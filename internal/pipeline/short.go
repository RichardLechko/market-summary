package pipeline

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/RichardLechko/market-summary/internal/analysis"
	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/digest"
)

const (
	// metaShortReported is the settlement date of the last short interest
	// report included in a sent summary.
	metaShortReported = "short_reported"
	metaSharesOut     = "shares_out:" // + ticker
	sharesOutMaxAge   = 30 * 24 * time.Hour
)

// shortInterest fills the Short interest section when FINRA has published a
// report since the last summary (always when previewing). It returns the
// report's settlement date, to record after sending, or "".
func (p *Pipeline) shortInterest(ctx context.Context, d *digest.Digest, now time.Time, preview bool) string {
	if p.FINRA == nil {
		return ""
	}
	var symbols []string
	for _, c := range p.Cfg.Watchlist {
		symbols = append(symbols, c.Ticker)
	}
	rows, err := p.FINRA.ShortInterest(ctx, symbols, now.AddDate(-1, -1, 0), now)
	if err != nil {
		p.Log.Error("finra short interest failed; using stored reports", "err", err)
	} else if err := p.Store.UpsertShortInterest(rows); err != nil {
		p.Log.Error("store short interest failed", "err", err)
	}

	var latest time.Time
	for _, c := range p.Cfg.Watchlist {
		h, err := p.Store.ShortInterest(c.Ticker)
		if err == nil && len(h) > 0 && h[len(h)-1].Settlement.After(latest) {
			latest = h[len(h)-1].Settlement
		}
	}
	if latest.IsZero() {
		return ""
	}
	asOf := latest.Format("2006-01-02")
	if last, _ := p.Store.Meta(metaShortReported); !preview && last >= asOf {
		return "" // no new report since the last summary
	}

	var out []analysis.ShortRow
	for _, c := range p.Cfg.Watchlist {
		h, err := p.Store.ShortInterest(c.Ticker)
		if err != nil || len(h) == 0 || !h[len(h)-1].Settlement.Equal(latest) {
			continue // no report for this stock in the latest release
		}
		if r, ok := analysis.NewShortRow(c.Ticker, h, p.sharesOutstanding(ctx, c, now)); ok {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return ""
	}
	analysis.SortShortRows(out)
	for _, r := range out {
		d.Short = append(d.Short, digest.NewShortView(r))
	}
	d.ShortAsOf = latest
	return asOf
}

type sharesCache struct {
	Shares  float64   `json:"shares"` // 0 when the company reports no single figure
	Fetched time.Time `json:"fetched"`
}

// sharesOutstanding returns a company's shares outstanding from its SEC
// filings, refreshed monthly, or 0 when it cannot be compared with short
// interest: foreign companies (US shares are depositary receipts that may
// stand for several local shares) and companies with several share classes.
func (p *Pipeline) sharesOutstanding(ctx context.Context, c config.Company, now time.Time) float64 {
	if slices.Contains(c.Forms, "6-K") {
		return 0
	}
	key := metaSharesOut + c.Ticker
	var cached sharesCache
	if raw, _ := p.Store.Meta(key); raw != "" && json.Unmarshal([]byte(raw), &cached) == nil && now.Sub(cached.Fetched) < sharesOutMaxAge {
		return cached.Shares
	}
	cik, ok := p.ciks[c.Ticker]
	if !ok || p.EDGAR == nil {
		return cached.Shares
	}
	n, _, found, err := p.EDGAR.SharesOutstanding(ctx, cik)
	if err != nil {
		p.Log.Warn("shares outstanding failed", "ticker", c.Ticker, "err", err)
		return cached.Shares
	}
	if !found {
		n = 0
	}
	if b, err := json.Marshal(sharesCache{Shares: n, Fetched: now}); err == nil {
		_ = p.Store.SetMeta(key, string(b))
	}
	return n
}
