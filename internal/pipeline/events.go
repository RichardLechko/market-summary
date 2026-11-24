package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RichardLechko/market-summary/internal/analysis"
	"github.com/RichardLechko/market-summary/internal/collect"
	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/digest"
	"github.com/RichardLechko/market-summary/internal/model"
	"github.com/RichardLechko/market-summary/internal/summarize"
)

const (
	// eventLookbackDays is how far before the window news filings are
	// summarized, so a new filing can be read against recent news.
	eventLookbackDays = 60
	// earlierNewsDays is how much earlier news is given as context.
	earlierNewsDays = 120
)

// companyFilings stores a company's news filings and insider trades. subs
// is newest first; news is summarized oldest first so each filing can see
// the ones before it.
func (p *Pipeline) companyFilings(ctx context.Context, c config.Company, cik int, subs []collect.SubFiling, windowDay time.Time) {
	eventDay := windowDay.AddDate(0, 0, -eventLookbackDays)
	for i := len(subs) - 1; i >= 0; i-- {
		sf := subs[i]
		switch {
		case sf.Form == "4":
			// A month before the window too, to spot several insiders buying.
			if !sf.Filed.Before(windowDay.AddDate(0, 0, -analysis.ClusterDays)) {
				p.ensureInsider(ctx, c, cik, sf)
			}
		case !sf.Filed.Before(eventDay):
			p.ensureEvent(ctx, c, cik, sf)
		}
	}
}

// ensureInsider stores the open-market trades on a Form 4, once.
func (p *Pipeline) ensureInsider(ctx context.Context, c config.Company, cik int, sf collect.SubFiling) {
	if ok, err := p.Store.HasInsiderFiling(sf.Accession); err != nil || ok {
		return
	}
	body, err := p.EDGAR.HTTP.Get(ctx, p.EDGAR.RawDocURL(cik, sf.Accession, sf.PrimaryDoc))
	if err != nil {
		p.Log.Error("form 4 fetch failed", "ticker", c.Ticker, "accession", sf.Accession, "err", err)
		return
	}
	issuer, trades, err := collect.ParseForm4(body)
	if err != nil {
		p.Log.Error("form 4 parse failed", "ticker", c.Ticker, "accession", sf.Accession, "err", err)
		return
	}
	if issuer != cik {
		trades = nil // the company traded another company's shares
	}
	if err := p.Store.SaveInsiderFiling(model.InsiderFiling{Accession: sf.Accession, Ticker: c.Ticker, Filed: sf.Filed, Trades: trades}); err != nil {
		p.Log.Error("save form 4 failed", "ticker", c.Ticker, "err", err)
	}
}

// ensureEvent stores a news filing and summarizes it once. A failed summary
// is retried on the next run.
func (p *Pipeline) ensureEvent(ctx context.Context, c config.Company, cik int, sf collect.SubFiling) {
	kinds := collect.EventLabels(sf.Form, sf.Items)
	if len(kinds) == 0 || sf.PrimaryDoc == "" {
		return
	}
	if e, err := p.Store.Event(sf.Accession); err != nil || (e != nil && (e.News != nil || p.Sum == nil)) {
		return
	}
	if sf.Form == "6-K" {
		// Results releases are covered in Earnings; wait until that check has run.
		if f, err := p.Store.Filing(sf.Accession); err != nil || (f != nil && (f.Facts == nil || f.Facts.IsEarningsRelease)) {
			return
		}
	}
	ev := model.CompanyEvent{Accession: sf.Accession, Ticker: c.Ticker, Form: sf.Form, Filed: sf.Filed, Items: sf.Items,
		DocURL: p.EDGAR.FilingDir(cik, sf.Accession) + "/" + sf.PrimaryDoc}
	is13D := collect.Is13D(sf.Form)
	var filing string
	if is13D {
		body, err := p.EDGAR.HTTP.Get(ctx, p.EDGAR.RawDocURL(cik, sf.Accession, sf.PrimaryDoc))
		if err != nil {
			p.Log.Error("13D fetch failed", "ticker", c.Ticker, "accession", sf.Accession, "err", err)
			return
		}
		if issuer, err := collect.Schedule13DIssuer(body); err != nil || issuer != cik {
			// Filed by the company about another company, or unreadable.
			ev.News = &model.EventNews{Routine: true, Headline: "Not about this company."}
			p.saveEvent(ev)
			return
		}
		filing = string(body)
	}
	if p.Sum != nil {
		if filing == "" {
			var err error
			if filing, err = p.EDGAR.DocumentText(ctx, ev.DocURL); err != nil {
				p.Log.Error("event filing fetch failed", "ticker", c.Ticker, "accession", sf.Accession, "err", err)
				return
			}
		}
		var press string
		if !is13D {
			if url, err := p.EDGAR.ExhibitURL(ctx, cik, sf.Accession, sf.PrimaryDoc); err == nil && url != ev.DocURL {
				press, _ = p.EDGAR.DocumentText(ctx, url)
			}
		}
		n, err := p.Sum.CompanyEvent(ctx, summarize.EventInput{
			Ticker: c.Ticker, Kinds: kinds, Filing: filing, Press: press, Earlier: p.earlierNews(c.Ticker, sf),
		})
		if err != nil {
			p.Log.Error("event summary failed", "ticker", c.Ticker, "accession", sf.Accession, "err", err)
		} else {
			ev.News = &n
		}
	}
	p.saveEvent(ev)
}

func (p *Pipeline) saveEvent(ev model.CompanyEvent) {
	if err := p.Store.SaveEvent(ev); err != nil {
		p.Log.Error("save event failed", "ticker", ev.Ticker, "err", err)
	}
}

// earlierNews lists the company's non-routine news filed before sf.
func (p *Pipeline) earlierNews(ticker string, sf collect.SubFiling) []string {
	events, err := p.Store.EventsForTicker(ticker)
	if err != nil {
		return nil
	}
	from := sf.Filed.AddDate(0, 0, -earlierNewsDays)
	var out []string
	for _, e := range events {
		if e.Accession == sf.Accession || e.News == nil || e.News.Routine || e.Filed.After(sf.Filed) || e.Filed.Before(from) {
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s %s", e.Filed.Format("2006-01-02"), e.News.Headline, e.News.Summary))
	}
	return out
}

// news fills the Company news section and returns the event and Form 4
// accessions handled, including those not shown.
func (p *Pipeline) news(d *digest.Digest, start, now time.Time, preview bool) (events, insiders []string) {
	windowDay := dateOnly(start)
	watched := map[string]bool{}
	for _, c := range p.Cfg.Watchlist {
		watched[c.Ticker] = true
	}

	evs, err := p.Store.UnreportedEvents()
	if preview {
		evs, err = p.Store.EventsFiledBetween(windowDay.AddDate(0, 0, -1), now)
	}
	if err != nil {
		p.Log.Error("load events failed", "err", err)
	}
	for _, e := range evs {
		if !watched[e.Ticker] || e.Filed.Before(windowDay) {
			continue
		}
		v := digest.NewsView{Ticker: e.Ticker, Kind: strings.Join(collect.EventLabels(e.Form, e.Items), ", "), Filed: e.Filed, Link: e.DocURL}
		switch {
		case e.News == nil && p.Sum != nil:
			continue // summary failed this run; retry next run
		case e.News == nil:
			v.Headline = "Filed a news report. A summary needs ANTHROPIC_API_KEY."
		case e.News.Routine:
			events = append(events, e.Accession)
			continue
		default:
			v.Company, v.Headline, v.Summary = e.News.Company, e.News.Headline, e.News.Summary
			if e.News.Kind != "" {
				v.Kind = e.News.Kind
			}
		}
		d.News = append(d.News, v)
		events = append(events, e.Accession)
	}

	fs, err := p.Store.UnreportedInsiderFilings()
	if preview {
		fs, err = p.Store.InsiderFilingsBetween(windowDay.AddDate(0, 0, -1), now)
	}
	if err != nil {
		p.Log.Error("load insider filings failed", "err", err)
		return events, nil
	}
	var inWindow []model.InsiderFiling
	for _, f := range fs {
		if watched[f.Ticker] && !f.Filed.Before(windowDay) {
			inWindow = append(inWindow, f)
			insiders = append(insiders, f.Accession)
		}
	}
	for _, r := range analysis.InsiderRows(inWindow) {
		if r.Notable() {
			d.Insiders = append(d.Insiders, digest.NewInsiderView(r))
		}
	}
	recent, err := p.Store.InsiderFilingsBetween(windowDay.AddDate(0, 0, -analysis.ClusterDays-1), now)
	if err != nil {
		p.Log.Error("load insider filings failed", "err", err)
		return events, insiders
	}
	var watchedRecent []model.InsiderFiling
	for _, f := range recent {
		if watched[f.Ticker] {
			watchedRecent = append(watchedRecent, f)
		}
	}
	for _, c := range analysis.InsiderClusters(watchedRecent, windowDay) {
		d.Clusters = append(d.Clusters, digest.NewClusterView(c))
	}
	return events, insiders
}
