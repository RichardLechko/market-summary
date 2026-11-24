package pipeline

import (
	"context"
	"strings"
	"time"

	"github.com/RichardLechko/market-summary/internal/collect"
	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/digest"
	"github.com/RichardLechko/market-summary/internal/model"
	"github.com/RichardLechko/market-summary/internal/summarize"
)

// ensureEvent stores a news 8-K (leadership change, acquisition and so on)
// and summarizes it once. A failed summary is retried on the next run.
func (p *Pipeline) ensureEvent(ctx context.Context, c config.Company, cik int, sf collect.SubFiling) {
	kinds := collect.EventLabels(sf.Form, sf.Items)
	if len(kinds) == 0 || sf.PrimaryDoc == "" {
		return
	}
	if e, err := p.Store.Event(sf.Accession); err != nil || (e != nil && (e.News != nil || p.Sum == nil)) {
		return
	}
	docURL := p.EDGAR.FilingDir(cik, sf.Accession) + "/" + sf.PrimaryDoc
	ev := model.CompanyEvent{Accession: sf.Accession, Ticker: c.Ticker, Form: sf.Form, Filed: sf.Filed, Items: sf.Items, DocURL: docURL}
	if p.Sum != nil {
		filing, err := p.EDGAR.DocumentText(ctx, docURL)
		if err != nil {
			p.Log.Error("event filing fetch failed", "ticker", c.Ticker, "accession", sf.Accession, "err", err)
			return
		}
		var press string
		if url, err := p.EDGAR.ExhibitURL(ctx, cik, sf.Accession, sf.PrimaryDoc); err == nil && url != docURL {
			press, _ = p.EDGAR.DocumentText(ctx, url)
		}
		n, err := p.Sum.CompanyEvent(ctx, summarize.EventInput{Ticker: c.Ticker, Kinds: kinds, Filing: filing, Press: press})
		if err != nil {
			p.Log.Error("event summary failed", "ticker", c.Ticker, "accession", sf.Accession, "err", err)
		} else {
			ev.News = &n
		}
	}
	if err := p.Store.SaveEvent(ev); err != nil {
		p.Log.Error("save event failed", "ticker", c.Ticker, "err", err)
	}
}

// news fills the Company news section and returns the accessions handled,
// including routine filings that are not shown.
func (p *Pipeline) news(d *digest.Digest, start, now time.Time, preview bool) []string {
	windowDay := dateOnly(start)
	events, err := p.Store.UnreportedEvents()
	if preview {
		events, err = p.Store.EventsFiledBetween(windowDay.AddDate(0, 0, -1), now)
	}
	if err != nil {
		p.Log.Error("load events failed", "err", err)
		return nil
	}
	watched := map[string]bool{}
	for _, c := range p.Cfg.Watchlist {
		watched[c.Ticker] = true
	}
	var shown []string
	for _, e := range events {
		if !watched[e.Ticker] || e.Filed.Before(windowDay) {
			continue
		}
		kind := strings.Join(collect.EventLabels(e.Form, e.Items), ", ")
		v := digest.NewsView{Ticker: e.Ticker, Kind: kind, Filed: e.Filed, Link: e.DocURL}
		switch {
		case e.News == nil && p.Sum != nil:
			continue // summary failed this run; retry next run
		case e.News == nil:
			v.Headline = "Filed an 8-K. A summary needs ANTHROPIC_API_KEY."
		case e.News.Routine:
			shown = append(shown, e.Accession)
			continue
		default:
			v.Company, v.Headline, v.Summary = e.News.Company, e.News.Headline, e.News.Summary
		}
		d.News = append(d.News, v)
		shown = append(shown, e.Accession)
	}
	return shown
}
