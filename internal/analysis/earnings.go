package analysis

import (
	"math"
	"slices"
	"time"

	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/model"
	"github.com/RichardLechko/market-summary/internal/stats"
)

// EarningsRow is one company's report with computed comparisons.
type EarningsRow struct {
	Company   config.Company
	Filing    model.Filing
	Facts     model.EarningsFacts
	RevGrowth *float64 // percent vs. year-ago quarter
	EPSGrowth *float64
	NIGrowth  *float64
}

// Growth returns the percent change from prior to cur. It is nil when either
// value is missing or prior is not positive (growth off a loss is not
// meaningful as a percentage).
func Growth(cur, prior *float64) *float64 {
	if cur == nil || prior == nil || *prior <= 0 {
		return nil
	}
	g, ok := stats.PctChange(*prior, *cur)
	if !ok {
		return nil
	}
	g = round(g, 1)
	return &g
}

// NewEarningsRow builds a row from a filing with extracted facts.
func NewEarningsRow(c config.Company, f model.Filing) EarningsRow {
	facts := *f.Facts
	return EarningsRow{
		Company:   c,
		Filing:    f,
		Facts:     facts,
		RevGrowth: Growth(facts.Revenue, facts.RevenueYearAgo),
		EPSGrowth: Growth(facts.EPSDiluted, facts.EPSDilutedYearAgo),
		NIGrowth:  Growth(facts.NetIncome, facts.NetIncomeYearAgo),
	}
}

// SectorAgg summarizes one sector in the current season.
type SectorAgg struct {
	Sector          string
	Count           int
	MedianRevGrowth float64 // NaN when no company has a revenue comparison
}

// Aggregate summarizes the current earnings season across the watchlist.
type Aggregate struct {
	Reported          int
	Total             int
	WithRevenue       int
	RevGrowthPositive int
	MedianRevGrowth   float64
	GuidanceRaised    int
	GuidanceHeld      int
	GuidanceLowered   int
	Sectors           []SectorAgg
}

// Summarize aggregates the latest report per company in rows. total is the
// watchlist size.
func Summarize(rows []EarningsRow, total int) Aggregate {
	latest := map[string]EarningsRow{}
	for _, r := range rows {
		cur, ok := latest[r.Company.Ticker]
		if !ok || r.Filing.Filed.After(cur.Filing.Filed) {
			latest[r.Company.Ticker] = r
		}
	}
	agg := Aggregate{Reported: len(latest), Total: total, MedianRevGrowth: math.NaN()}
	var growths []float64
	bySector := map[string][]float64{}
	counts := map[string]int{}
	for _, r := range latest {
		counts[r.Company.Sector]++
		if r.RevGrowth != nil {
			growths = append(growths, *r.RevGrowth)
			bySector[r.Company.Sector] = append(bySector[r.Company.Sector], *r.RevGrowth)
			if *r.RevGrowth > 0 {
				agg.RevGrowthPositive++
			}
		}
		switch r.Facts.Guidance {
		case "raised", "initiated":
			agg.GuidanceRaised++
		case "maintained":
			agg.GuidanceHeld++
		case "lowered", "withdrawn":
			agg.GuidanceLowered++
		}
	}
	agg.WithRevenue = len(growths)
	agg.MedianRevGrowth = stats.Median(growths)
	for sector, n := range counts {
		agg.Sectors = append(agg.Sectors, SectorAgg{Sector: sector, Count: n, MedianRevGrowth: stats.Median(bySector[sector])})
	}
	slices.SortFunc(agg.Sectors, func(a, b SectorAgg) int {
		if a.Sector < b.Sector {
			return -1
		}
		if a.Sector > b.Sector {
			return 1
		}
		return 0
	})
	return agg
}

// Expected is a company estimated to report soon.
type Expected struct {
	Company config.Company
	Date    time.Time
}

// ExpectedReports estimates which companies report in (today, today+days],
// from each company's earnings release dates (history). A company is expected
// when a release fell 52 weeks before a date in that range, unless it already
// released within the last 30 days.
func ExpectedReports(companies []config.Company, history map[string][]time.Time, today time.Time, days int) []Expected {
	today = dateOnly(today)
	end := today.AddDate(0, 0, days)
	var out []Expected
	for _, c := range companies {
		dates := history[c.Ticker]
		recent := false
		for _, d := range dates {
			if !d.Before(today.AddDate(0, 0, -30)) && !d.After(today) {
				recent = true
			}
		}
		if recent {
			continue
		}
		for _, d := range dates {
			e := dateOnly(d).AddDate(0, 0, 364)
			if e.After(today) && !e.After(end) {
				out = append(out, Expected{Company: c, Date: e})
				break
			}
		}
	}
	slices.SortFunc(out, func(a, b Expected) int { return a.Date.Compare(b.Date) })
	return out
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
