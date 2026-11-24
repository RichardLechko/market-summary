package pipeline

import (
	"context"
	"encoding/json"
	"time"

	"github.com/RichardLechko/market-summary/internal/analysis"
	"github.com/RichardLechko/market-summary/internal/collect"
	"github.com/RichardLechko/market-summary/internal/digest"
)

const (
	metaCalendar   = "earnings_calendar"
	metaCalendarAt = "earnings_calendar_at"
	// calendarMaxAge is how long a stored calendar is used when a fresh one
	// cannot be fetched.
	calendarMaxAge = 14 * 24 * time.Hour
)

// earningsCalendar returns the next report date per watchlist ticker from
// the Alpha Vantage calendar, or nil when no calendar is available. A fetched
// calendar is stored and reused for two weeks if a later fetch fails.
func (p *Pipeline) earningsCalendar(ctx context.Context, now time.Time) map[string]collect.CalendarEntry {
	watched := map[string]bool{}
	for _, c := range p.Cfg.Watchlist {
		watched[c.Ticker] = true
	}
	var entries []collect.CalendarEntry
	if p.AV != nil && p.AV.Key != "" {
		all, err := p.AV.EarningsCalendar(ctx)
		if err != nil {
			p.Log.Warn("earnings calendar failed; using stored copy if recent", "err", err)
		} else {
			for _, e := range all {
				if watched[e.Symbol] {
					entries = append(entries, e)
				}
			}
			if b, err := json.Marshal(entries); err == nil {
				_ = p.Store.SetMetaMany(map[string]string{metaCalendar: string(b), metaCalendarAt: now.Format(time.RFC3339)})
			}
		}
	}
	if entries == nil {
		at, _ := p.Store.Meta(metaCalendarAt)
		t, err := time.Parse(time.RFC3339, at)
		if err != nil || now.Sub(t) > calendarMaxAge {
			return nil
		}
		raw, _ := p.Store.Meta(metaCalendar)
		if json.Unmarshal([]byte(raw), &entries) != nil {
			return nil
		}
	}
	out := map[string]collect.CalendarEntry{}
	for _, e := range entries {
		if prev, ok := out[e.Symbol]; !ok || e.Date.Before(prev.Date) {
			out[e.Symbol] = e
		}
	}
	return out
}

// aheadEarnings lists watchlist reports due in (today, today+aheadDays]:
// dates from the calendar where it lists the company, else estimates from
// last year's filing dates.
func (p *Pipeline) aheadEarnings(ctx context.Context, d *digest.Digest, history map[string][]time.Time, now time.Time) {
	today := dateOnly(now)
	end := today.AddDate(0, 0, aheadDays)
	cal := p.earningsCalendar(ctx, now)
	for _, c := range p.Cfg.Watchlist {
		e, ok := cal[c.Ticker]
		if !ok || !e.Date.After(today) || e.Date.After(end) {
			continue
		}
		label := c.Ticker + " earnings"
		switch e.Time {
		case "pre-market":
			label += ", before the market opens"
		case "post-market":
			label += ", after the market closes"
		}
		d.WeekAhead = append(d.WeekAhead, digest.AheadItem{Date: e.Date, Label: label})
	}
	for _, e := range analysis.ExpectedReports(p.Cfg.Watchlist, history, now, aheadDays) {
		if _, listed := cal[e.Company.Ticker]; listed {
			continue // the calendar has a date for it, outside this week
		}
		d.WeekAhead = append(d.WeekAhead, digest.AheadItem{Date: e.Date, Label: e.Company.Ticker + " earnings (estimated)"})
	}
}
