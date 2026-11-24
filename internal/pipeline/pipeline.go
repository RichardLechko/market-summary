// Package pipeline runs collect -> store -> analysis -> summarize -> digest -> email.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/RichardLechko/market-summary/internal/analysis"
	"github.com/RichardLechko/market-summary/internal/collect"
	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/digest"
	"github.com/RichardLechko/market-summary/internal/model"
	"github.com/RichardLechko/market-summary/internal/schedule"
	"github.com/RichardLechko/market-summary/internal/stats"
	"github.com/RichardLechko/market-summary/internal/store"
	"github.com/RichardLechko/market-summary/internal/summarize"
)

const (
	metaLastSent   = "last_sent_date"
	metaLastCutoff = "last_cutoff"
	metaReported   = "reported:" // + series ID
	aheadDays      = 7
)

// Options control one run.
type Options struct {
	Force  bool      // bypass the gate and send
	DryRun bool      // bypass the gate, build, do not send or record
	Open   bool      // open the page in the browser when done
	Since  time.Time // with DryRun or Force: override the window start
	Now    time.Time
}

// Result describes what a run did.
type Result struct {
	NotDue       bool
	NextEligible time.Time
	Path         string
	PDFPath      string
	Sent         bool
	Subject      string
}

// Pipeline holds the run's dependencies.
type Pipeline struct {
	Cfg   *config.Config
	Store *store.Store
	Log   *slog.Logger
	FRED  *collect.FRED
	Fed   *collect.Fed
	EDGAR *collect.EDGAR
	AV    *collect.AlphaVantage // call transcripts and fund prices; nil or no key disables both
	Sum   *summarize.Summarizer // nil when no API key is configured
	Mail  Sender
	// PrintPDF renders the page to a PDF for the email; nil skips the PDF.
	PrintPDF func(ctx context.Context, htmlPath, pdfPath string) error
	// Open shows the page in the browser; nil disables it.
	Open func(path string) error
}

// Sender delivers the email.
type Sender interface {
	Send(subject, html string, atts ...digest.Attachment) error
}

// Gate reports whether a gated run is due.
func (p *Pipeline) Gate(now time.Time) (schedule.Decision, error) {
	v, err := p.Store.Meta(metaLastSent)
	if err != nil {
		return schedule.Decision{}, err
	}
	var last time.Time
	if v != "" {
		if last, err = config.ParseDate(v); err != nil {
			return schedule.Decision{}, fmt.Errorf("bad %s %q: %w", metaLastSent, v, err)
		}
	}
	return schedule.Decide(last, now, p.Cfg.Settings.MinGapDays), nil
}

// WindowStart returns the start of the coverage window.
func (p *Pipeline) WindowStart(now time.Time) (time.Time, error) {
	v, err := p.Store.Meta(metaLastCutoff)
	if err != nil {
		return time.Time{}, err
	}
	if v == "" {
		return now.AddDate(0, 0, -p.Cfg.Settings.FirstRunDays), nil
	}
	return time.Parse(time.RFC3339, v)
}

// Run executes the pipeline.
func (p *Pipeline) Run(ctx context.Context, opts Options) (Result, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if !opts.Force && !opts.DryRun {
		d, err := p.Gate(now)
		if err != nil {
			return Result{}, err
		}
		if !d.Due {
			return Result{NotDue: true, NextEligible: d.NextEligible}, nil
		}
	}
	start, err := p.WindowStart(now)
	if err != nil {
		return Result{}, err
	}
	if !opts.Since.IsZero() {
		start = opts.Since
	}
	p.Log.Info("building digest", "window_start", start.Format(time.RFC3339), "window_end", now.Format(time.RFC3339))

	d := &digest.Digest{WindowStart: start, WindowEnd: now}
	if p.Sum == nil {
		d.Notes = append(d.Notes, "Summaries unavailable: ANTHROPIC_API_KEY is not set.")
	}

	reported := p.macro(ctx, d, start, now)
	p.fed(ctx, d, start, now)
	filings := p.earnings(ctx, d, start, now, !opts.Since.IsZero())
	callNotes := p.calls(ctx, d, start, now, !opts.Since.IsZero())
	events := p.news(d, start, now, !opts.Since.IsZero())
	p.weekAhead(ctx, d, now)

	if p.Sum != nil {
		if ov, err := p.Sum.Overview(ctx, d.Brief()); err != nil {
			p.Log.Error("overview failed", "err", err)
			d.Notes = append(d.Notes, "Summary paragraph unavailable this run.")
		} else {
			d.Overview = ov
		}
	}

	page, err := d.RenderPage()
	if err != nil {
		return Result{}, fmt.Errorf("render: %w", err)
	}
	dir := filepath.Join(p.Cfg.DataDir(), "digests")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}
	suffix := ""
	if opts.DryRun {
		suffix = "-dry-run"
	}
	base := filepath.Join(dir, now.Format("2006-01-02-1504")+suffix)
	path := base + ".html"
	if err := os.WriteFile(path, []byte(page), 0o644); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.html"), []byte(page), 0o644); err != nil {
		p.Log.Warn("write latest.html failed", "err", err)
	}
	res := Result{Path: path, Subject: d.Subject()}
	p.Log.Info("digest written", "path", path)

	var atts []digest.Attachment
	if p.PrintPDF != nil {
		pdfPath := base + ".pdf"
		if err := p.PrintPDF(ctx, path, pdfPath); err != nil {
			p.Log.Error("pdf failed; sending without attachment", "err", err)
		} else if data, err := os.ReadFile(pdfPath); err == nil {
			res.PDFPath = pdfPath
			atts = append(atts, digest.Attachment{
				Name:        "market-summary-" + now.Format("2006-01-02") + ".pdf",
				ContentType: "application/pdf",
				Data:        data,
			})
		}
	}
	if opts.Open && p.Open != nil {
		if err := p.Open(path); err != nil {
			p.Log.Warn("open in browser failed", "err", err)
		}
	}
	if opts.DryRun {
		return res, nil
	}

	email, err := d.RenderEmail(len(atts) > 0)
	if err != nil {
		return res, fmt.Errorf("render email: %w", err)
	}
	if err := p.Mail.Send(d.Subject(), email, atts...); err != nil {
		return res, fmt.Errorf("send email (digest kept at %s): %w", path, err)
	}
	res.Sent = true
	p.Log.Info("digest sent")

	kv := map[string]string{
		metaLastSent:   now.Format("2006-01-02"),
		metaLastCutoff: now.Format(time.RFC3339),
	}
	for id, t := range reported {
		kv[metaReported+id] = t.Format("2006-01-02")
	}
	if err := p.Store.SetMetaMany(kv); err != nil {
		return res, fmt.Errorf("record send: %w", err)
	}
	if err := p.Store.MarkFilingsReported(filings); err != nil {
		return res, fmt.Errorf("mark filings reported: %w", err)
	}
	if err := p.Store.MarkCallNotesReported(callNotes); err != nil {
		return res, fmt.Errorf("mark call notes reported: %w", err)
	}
	if err := p.Store.MarkEventsReported(events); err != nil {
		return res, fmt.Errorf("mark events reported: %w", err)
	}
	return res, nil
}

// macro collects FRED series and fills the Market and Economy sections. It
// returns the latest observation date shown per series.
func (p *Pipeline) macro(ctx context.Context, d *digest.Digest, start, now time.Time) map[string]time.Time {
	shown := map[string]time.Time{}
	if p.Cfg.Secrets.FREDKey == "" {
		d.Notes = append(d.Notes, "Market and economy sections unavailable: FRED_API_KEY is not set.")
		return shown
	}
	since := now.AddDate(-2, -1, 0)
	failed := 0
	avLimited := false
	for _, s := range p.Cfg.Series {
		var obs []stats.Obs
		var err error
		switch {
		case s.Source != "alphavantage":
			obs, err = p.FRED.Observations(ctx, s.ID, since)
		case p.AV == nil || p.AV.Key == "" || avLimited:
			continue // shown from stored prices if any; dropped otherwise
		default:
			obs, err = p.AV.Daily(ctx, s.ID)
			if errors.Is(err, collect.ErrAVLimit) {
				avLimited = true
			}
		}
		if err != nil {
			failed++
			p.Log.Error("series refresh failed", "series", s.ID, "err", err)
			continue
		}
		if err := p.Store.UpsertObservations(s.ID, obs); err != nil {
			p.Log.Error("store series failed", "series", s.ID, "err", err)
		}
	}
	if failed > 0 {
		d.Notes = append(d.Notes, fmt.Sprintf("%d market series could not be refreshed; stored values were used.", failed))
	}

	data := map[string][]stats.Obs{}
	reported := map[string]time.Time{}
	for _, s := range p.Cfg.Series {
		obs, err := p.Store.Observations(s.ID)
		if err != nil {
			p.Log.Error("load series failed", "series", s.ID, "err", err)
			continue
		}
		data[s.ID] = obs
		if v, _ := p.Store.Meta(metaReported + s.ID); v != "" {
			if t, err := config.ParseDate(v); err == nil {
				reported[s.ID] = t
			}
		}
	}
	rows := analysis.Macro(p.Cfg.Series, data, start, reported)

	groups := map[string][]analysis.MacroRow{}
	var order []string
	for _, r := range rows {
		shown[r.Series.ID] = r.AsOf
		if r.Series.Group == "Market" {
			d.Market = append(d.Market, r)
			continue
		}
		daily := r.Series.Kind == "price" || r.Series.Kind == "rate"
		if !daily && !r.New {
			continue
		}
		if daily {
			r.New = false // daily series always update; the tag is for releases
		}
		if _, ok := groups[r.Series.Group]; !ok {
			order = append(order, r.Series.Group)
		}
		groups[r.Series.Group] = append(groups[r.Series.Group], r)
	}
	for _, g := range order {
		d.Macro = append(d.Macro, digest.MacroGroup{Name: g, Rows: groups[g]})
	}
	return shown
}

// fed collects Federal Reserve press releases and fills the Fed section.
func (p *Pipeline) fed(ctx context.Context, d *digest.Digest, start, now time.Time) {
	for _, f := range p.Cfg.Settings.FedFeeds {
		docs, err := p.Fed.Docs(ctx, f.URL, f.Exclude, start)
		if err != nil {
			p.Log.Error("fed feed failed", "feed", f.URL, "err", err)
			d.Notes = append(d.Notes, "A Federal Reserve feed could not be read this run.")
			continue
		}
		for _, doc := range docs {
			if err := p.Store.UpsertFedDoc(doc); err != nil {
				p.Log.Error("store fed doc failed", "link", doc.Link, "err", err)
			}
		}
	}
	if idx := p.Cfg.Settings.BeigeBookIndex; idx != "" {
		bb, err := p.Fed.LatestBeigeBook(ctx, idx)
		switch {
		case err != nil:
			p.Log.Error("beige book failed", "err", err)
		case bb.Published.After(start):
			if err := p.Store.UpsertFedDoc(bb); err != nil {
				p.Log.Error("store beige book failed", "err", err)
			}
		}
	}
	docs, err := p.Store.FedDocsBetween(start, now)
	if err != nil {
		p.Log.Error("load fed docs failed", "err", err)
		return
	}
	for i := range docs {
		if docs[i].Summary == "" && p.Sum != nil && docs[i].Body != "" {
			s, err := p.Sum.FedSummary(ctx, docs[i])
			if err != nil {
				p.Log.Error("fed summary failed", "title", docs[i].Title, "err", err)
				continue
			}
			docs[i].Summary = s
			_ = p.Store.UpsertFedDoc(docs[i])
		}
	}
	d.Fed = docs
}

// earnings collects watchlist filings, extracts figures, and fills the
// Earnings section and season aggregate. It returns accessions shown.
func (p *Pipeline) earnings(ctx context.Context, d *digest.Digest, start, now time.Time, preview bool) []string {
	if p.Cfg.Secrets.SECUserAgent == "" {
		d.Notes = append(d.Notes, "Earnings section unavailable: SEC_USER_AGENT is not set.")
		return nil
	}
	tickers, err := p.EDGAR.TickerMap(ctx)
	if err != nil {
		p.Log.Error("edgar ticker map failed", "err", err)
		d.Notes = append(d.Notes, "Earnings section unavailable: SEC EDGAR could not be reached.")
		return nil
	}
	history := map[string][]time.Time{}
	windowDay := dateOnly(start)
	seasonDay := dateOnly(now.AddDate(0, 0, -p.Cfg.Settings.SeasonDays))
	for _, c := range p.Cfg.Watchlist {
		cik, ok := tickers[c.Ticker]
		if !ok {
			p.Log.Warn("ticker not found in EDGAR", "ticker", c.Ticker)
			continue
		}
		subs, err := p.EDGAR.Submissions(ctx, cik, now.AddDate(0, 0, -400))
		if err != nil {
			p.Log.Error("edgar submissions failed", "ticker", c.Ticker, "err", err)
			continue
		}
		for _, sf := range subs {
			if !sf.Filed.Before(windowDay) {
				p.ensureEvent(ctx, c, cik, sf)
			}
		}
		cands := candidates(c, subs)
		for i, sf := range cands {
			if sf.Form == "8-K" {
				history[c.Ticker] = append(history[c.Ticker], sf.Filed)
			}
			if sf.Filed.Before(windowDay) {
				if !sf.Filed.Before(seasonDay) {
					p.backfill(ctx, c, cik, sf)
				}
				continue
			}
			var prior *collect.SubFiling
			if i+1 < len(cands) {
				prior = &cands[i+1]
			}
			p.ensureFiling(ctx, c, cik, sf, prior)
		}
		if slices.Contains(c.Forms, "6-K") {
			stored, _ := p.Store.FilingsForTicker(c.Ticker)
			for _, f := range stored {
				if f.Facts != nil && f.Facts.IsEarningsRelease {
					history[c.Ticker] = append(history[c.Ticker], f.Filed)
				}
			}
		}
	}

	byTicker := map[string]config.Company{}
	for _, c := range p.Cfg.Watchlist {
		byTicker[c.Ticker] = c
	}

	// Reports not yet emailed.
	unreported, err := p.Store.UnreportedFilings()
	if preview {
		// Previewing a past period: include filings already emailed.
		unreported, err = p.Store.FilingsFiledBetween(windowDay.AddDate(0, 0, -1), now)
	}
	if err != nil {
		p.Log.Error("load filings failed", "err", err)
		return nil
	}
	sectors := map[string][]digest.EarningsView{}
	var sectorOrder, shown []string
	for _, f := range unreported {
		c, ok := byTicker[f.Ticker]
		if !ok || f.Filed.Before(windowDay) {
			continue
		}
		var view digest.EarningsView
		switch {
		case f.Facts == nil:
			if p.Sum != nil {
				continue // extraction failed this run; retry next run
			}
			view = digest.EarningsView{Ticker: f.Ticker, Filed: f.Filed, Link: f.DocURL,
				Headline: fmt.Sprintf("Filed a %s earnings release. Figures need ANTHROPIC_API_KEY.", f.Form)}
		case !f.Facts.IsEarningsRelease:
			shown = append(shown, f.Accession) // not an earnings release; never show
			continue
		default:
			view = digest.NewEarningsView(analysis.NewEarningsRow(c, f), f.DocURL)
		}
		if _, ok := sectors[c.Sector]; !ok {
			sectorOrder = append(sectorOrder, c.Sector)
		}
		sectors[c.Sector] = append(sectors[c.Sector], view)
		shown = append(shown, f.Accession)
	}
	for _, s := range sectorOrder {
		d.Earnings = append(d.Earnings, digest.EarningsSector{Sector: s, Reports: sectors[s]})
	}

	// Season aggregate.
	seasonStart := now.AddDate(0, 0, -p.Cfg.Settings.SeasonDays)
	season, err := p.Store.FilingsFiledBetween(seasonStart, now)
	if err == nil {
		var rows []analysis.EarningsRow
		for _, f := range season {
			c, ok := byTicker[f.Ticker]
			if ok && f.Facts != nil && f.Facts.IsEarningsRelease {
				rows = append(rows, analysis.NewEarningsRow(c, f))
			}
		}
		if len(rows) >= 3 {
			d.Aggregate = digest.NewAggregateView(analysis.Summarize(rows, len(p.Cfg.Watchlist)), p.Cfg.Settings.SeasonDays)
		}
	}

	for _, e := range analysis.ExpectedReports(p.Cfg.Watchlist, history, now, aheadDays) {
		d.WeekAhead = append(d.WeekAhead, digest.AheadItem{Date: e.Date, Label: e.Company.Ticker + " earnings (estimated)"})
	}
	return shown
}

// candidates returns the company's earnings-release candidates, newest first:
// 8-K Item 2.02 filings, plus every 6-K for foreign filers that use them.
func candidates(c config.Company, subs []collect.SubFiling) []collect.SubFiling {
	var out []collect.SubFiling
	for _, s := range subs {
		switch {
		case s.Form == "8-K" && slices.Contains(c.Forms, "8-K") && s.IsEarnings8K():
			out = append(out, s)
		case s.Form == "6-K" && slices.Contains(c.Forms, "6-K"):
			out = append(out, s)
		}
	}
	return out
}

// ensureFiling stores a filing and extracts its facts if not done yet. For
// standard-depth companies the prior earnings release is extracted too, so
// the summary can compare against last quarter.
func (p *Pipeline) ensureFiling(ctx context.Context, c config.Company, cik int, sf collect.SubFiling, prior *collect.SubFiling) {
	existing, err := p.Store.Filing(sf.Accession)
	if err != nil {
		p.Log.Error("load filing failed", "accession", sf.Accession, "err", err)
		return
	}
	if existing != nil && existing.Facts != nil {
		return
	}
	f := model.Filing{Accession: sf.Accession, Ticker: c.Ticker, CIK: cik, Form: sf.Form, Filed: sf.Filed, Items: sf.Items}
	url, err := p.EDGAR.ExhibitURL(ctx, cik, sf.Accession, sf.PrimaryDoc)
	if err != nil {
		p.Log.Error("find exhibit failed", "ticker", c.Ticker, "accession", sf.Accession, "err", err)
		f.DocURL = p.EDGAR.FilingDir(cik, sf.Accession)
		_ = p.Store.SaveFiling(f)
		return
	}
	f.DocURL = url
	if p.Sum == nil {
		_ = p.Store.SaveFiling(f)
		return
	}

	priorSummary := ""
	if c.Depth != "headline" && prior != nil && sf.Form == "8-K" {
		priorSummary = p.priorSummary(ctx, c, cik, *prior)
	}
	facts, err := p.extract(ctx, c, sf, url, c.Depth, priorSummary)
	if err != nil {
		p.Log.Error("extract failed", "ticker", c.Ticker, "accession", sf.Accession, "err", err)
		_ = p.Store.SaveFiling(f)
		return
	}
	f.Facts = &facts
	if err := p.Store.SaveFiling(f); err != nil {
		p.Log.Error("save filing failed", "accession", sf.Accession, "err", err)
	}
	p.Log.Info("extracted filing", "ticker", c.Ticker, "accession", sf.Accession, "earnings", facts.IsEarningsRelease)
}

// backfill extracts a release from earlier in the season that predates this
// digest's window (for example on the first run), so the season aggregate is
// complete. It is stored as already reported and never shown as new.
func (p *Pipeline) backfill(ctx context.Context, c config.Company, cik int, sf collect.SubFiling) {
	if p.Sum == nil {
		return
	}
	if f, err := p.Store.Filing(sf.Accession); err != nil || (f != nil && f.Facts != nil) {
		return
	}
	headline := c
	headline.Depth = "headline"
	p.ensureFiling(ctx, headline, cik, sf, nil)
	if err := p.Store.MarkFilingsReported([]string{sf.Accession}); err != nil {
		p.Log.Error("mark backfill reported failed", "accession", sf.Accession, "err", err)
	}
}

func (p *Pipeline) priorSummary(ctx context.Context, c config.Company, cik int, sf collect.SubFiling) string {
	if f, err := p.Store.Filing(sf.Accession); err == nil && f != nil && f.Facts != nil {
		return f.Facts.Summary
	}
	url, err := p.EDGAR.ExhibitURL(ctx, cik, sf.Accession, sf.PrimaryDoc)
	if err != nil {
		return ""
	}
	facts, err := p.extract(ctx, c, sf, url, "standard", "")
	if err != nil {
		p.Log.Warn("prior quarter extraction failed", "ticker", c.Ticker, "err", err)
		return ""
	}
	// Stored as already reported: it predates this digest's window.
	_ = p.Store.SaveFiling(model.Filing{Accession: sf.Accession, Ticker: c.Ticker, CIK: cik, Form: sf.Form,
		Filed: sf.Filed, Items: sf.Items, DocURL: url, Facts: &facts})
	_ = p.Store.MarkFilingsReported([]string{sf.Accession})
	return facts.Summary
}

func (p *Pipeline) extract(ctx context.Context, c config.Company, sf collect.SubFiling, url, depth, priorSummary string) (model.EarningsFacts, error) {
	text, err := p.EDGAR.DocumentText(ctx, url)
	if err != nil {
		return model.EarningsFacts{}, err
	}
	if strings.TrimSpace(text) == "" {
		return model.EarningsFacts{}, fmt.Errorf("empty document %s", url)
	}
	return p.Sum.ExtractEarnings(ctx, summarize.EarningsInput{
		Ticker: c.Ticker, Sector: c.Sector, Form: sf.Form, Filed: sf.Filed.Format("2006-01-02"),
		Depth: depth, Text: text, PriorSummary: priorSummary,
	})
}

// weekAhead adds scheduled releases and FOMC meetings in the next week.
func (p *Pipeline) weekAhead(ctx context.Context, d *digest.Digest, now time.Time) {
	today := dateOnly(now)
	end := today.AddDate(0, 0, aheadDays)
	if p.Cfg.Secrets.FREDKey != "" {
		rs, err := p.FRED.ReleaseDates(ctx, today.AddDate(0, 0, 1), end, p.Cfg.Settings.Releases)
		if err != nil {
			p.Log.Error("fred release dates failed", "err", err)
		} else if err := p.Store.UpsertReleaseDates(rs); err != nil {
			p.Log.Error("store release dates failed", "err", err)
		}
	}
	if rs, err := p.Store.ReleaseDatesBetween(today, end); err == nil {
		for _, r := range rs {
			d.WeekAhead = append(d.WeekAhead, digest.AheadItem{Date: r.Date, Label: r.Name})
		}
	}
	for _, m := range p.Cfg.FOMC {
		s, err := config.ParseDate(m.Start)
		if err != nil {
			continue
		}
		if s.After(today) && !s.After(end) {
			label := "FOMC meeting"
			if e, err := config.ParseDate(m.End); err == nil {
				label += ", decision " + e.Format("Mon Jan 2")
			}
			d.WeekAhead = append(d.WeekAhead, digest.AheadItem{Date: s, Label: label})
		}
	}
	slices.SortStableFunc(d.WeekAhead, func(a, b digest.AheadItem) int { return a.Date.Compare(b.Date) })
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
