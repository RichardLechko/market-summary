package pipeline

import (
	"context"
	"errors"
	"time"

	"github.com/RichardLechko/market-summary/internal/collect"
	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/digest"
	"github.com/RichardLechko/market-summary/internal/model"
	"github.com/RichardLechko/market-summary/internal/summarize"
)

// callLookbackDays is how long after an earnings release the pipeline keeps
// looking for its call transcript. Transcripts usually appear within days.
const callLookbackDays = 30

// calls fetches and summarizes earnings call transcripts for deep-tier
// companies and fills the call notes in the Earnings section. It returns the
// accessions of the call notes shown.
func (p *Pipeline) calls(ctx context.Context, d *digest.Digest, start, now time.Time, preview bool) []string {
	deep := map[string]config.Company{}
	for _, c := range p.Cfg.Watchlist {
		if c.Depth == "deep" {
			deep[c.Ticker] = c
		}
	}
	if len(deep) == 0 {
		return nil
	}

	recent, err := p.Store.FilingsFiledBetween(dateOnly(now.AddDate(0, 0, -callLookbackDays)), now)
	if err != nil {
		p.Log.Error("load recent filings failed", "err", err)
		return nil
	}
	var pending []model.Filing
	for _, f := range recent {
		if _, ok := deep[f.Ticker]; !ok || f.Facts == nil || !f.Facts.IsEarningsRelease {
			continue
		}
		if has, err := p.Store.HasCallNote(f.Accession); err == nil && !has {
			pending = append(pending, f)
		}
	}

	switch {
	case len(pending) == 0:
	case p.Calls == nil || p.Calls.Key == "":
		d.Notes = append(d.Notes, "Earnings call notes unavailable: ALPHAVANTAGE_API_KEY is not set.")
	case p.Sum == nil:
	default:
		p.fetchCalls(ctx, pending)
	}

	var notes []model.CallNote
	if preview {
		notes, err = p.Store.CallNotesFiledBetween(dateOnly(start).AddDate(0, 0, -1), now)
	} else {
		notes, err = p.Store.UnreportedCallNotes()
	}
	if err != nil {
		p.Log.Error("load call notes failed", "err", err)
		return nil
	}
	var shown []string
	for _, n := range notes {
		company := n.Ticker
		if f, err := p.Store.Filing(n.Accession); err == nil && f != nil && f.Facts != nil && f.Facts.Company != "" {
			company = f.Facts.Company
		}
		d.Calls = append(d.Calls, digest.NewCallView(n, company))
		shown = append(shown, n.Accession)
	}
	return shown
}

func (p *Pipeline) fetchCalls(ctx context.Context, pending []model.Filing) {
	for _, f := range pending {
		for _, q := range collect.TranscriptQuarters(f.Facts.Period, f.Filed) {
			parts, err := p.Calls.Transcript(ctx, f.Ticker, q)
			if errors.Is(err, collect.ErrTranscriptLimit) {
				p.Log.Warn("alpha vantage limit reached; remaining transcripts next run")
				return
			}
			if err != nil {
				p.Log.Error("transcript fetch failed", "ticker", f.Ticker, "quarter", q, "err", err)
				break
			}
			if len(parts) == 0 {
				continue
			}
			notes, err := p.Sum.CallNotes(ctx, summarize.CallInput{
				Ticker: f.Ticker, Company: f.Facts.Company, Period: f.Facts.Period,
				ReleaseSummary: f.Facts.Headline + " " + f.Facts.Summary,
				Text:           collect.TranscriptText(parts),
			})
			if err != nil {
				p.Log.Error("call notes failed", "ticker", f.Ticker, "err", err)
				break
			}
			if !notes.MatchesPeriod {
				p.Log.Info("transcript is for a different period", "ticker", f.Ticker, "quarter", q)
				continue
			}
			if err := p.Store.SaveCallNote(model.CallNote{
				Accession: f.Accession, Ticker: f.Ticker, Quarter: q, Period: f.Facts.Period, Filed: f.Filed, Notes: notes,
			}); err != nil {
				p.Log.Error("save call notes failed", "ticker", f.Ticker, "err", err)
			}
			p.Log.Info("call notes added", "ticker", f.Ticker, "quarter", q)
			break
		}
	}
}
