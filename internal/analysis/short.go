package analysis

import (
	"math"
	"slices"
	"time"

	"github.com/RichardLechko/market-summary/internal/model"
)

// Flags for short interest. A rise is flagged only from a meaningful base,
// so a stock going from 0.3% to 0.4% short is not marked.
const (
	shortRiseFlag    = 25 // percent rise since the previous report
	shortRiseMinPct  = 2  // ... when at least this share of stock is short
	shortRiseMinDays = 2  // ... or, without a share count, this many days to cover
	shortHeavyPct    = 15 // share of stock short that counts as heavily shorted
)

// ShortRow is the latest short interest for one stock.
type ShortRow struct {
	Ticker      string
	AsOf        time.Time
	Short       float64
	DaysToCover float64
	// Pct is the share of shares outstanding sold short, or NaN when the
	// company reports no single share count it can be compared with.
	Pct float64
	// Change is the percent change in shares short since the previous
	// report, or NaN.
	Change  float64
	Flagged bool
	Trend   []float64 // the past year: Pct when known, else shares short
}

// NewShortRow builds a row from a stock's reports (oldest first). sharesOut
// is 0 when unknown. ok is false when there are no reports.
func NewShortRow(ticker string, hist []model.ShortInterest, sharesOut float64) (ShortRow, bool) {
	if len(hist) == 0 {
		return ShortRow{}, false
	}
	last := hist[len(hist)-1]
	r := ShortRow{Ticker: ticker, AsOf: last.Settlement, Short: last.Short, DaysToCover: last.DaysToCover, Pct: math.NaN(), Change: math.NaN()}
	if sharesOut > 0 {
		r.Pct = last.Short / sharesOut * 100
	}
	if len(hist) > 1 {
		if prev := hist[len(hist)-2].Short; prev > 0 {
			r.Change = (last.Short - prev) / prev * 100
		}
	}
	base := r.DaysToCover >= shortRiseMinDays
	if !math.IsNaN(r.Pct) {
		base = r.Pct >= shortRiseMinPct
	}
	r.Flagged = (!math.IsNaN(r.Change) && r.Change >= shortRiseFlag && base) || (!math.IsNaN(r.Pct) && r.Pct >= shortHeavyPct)

	from := last.Settlement.AddDate(-1, 0, 0)
	var vals []float64
	for _, h := range hist {
		if h.Settlement.Before(from) {
			continue
		}
		v := h.Short
		if sharesOut > 0 {
			v = h.Short / sharesOut * 100
		}
		vals = append(vals, v)
	}
	r.Trend = thin(vals, trendPoints)
	return r, true
}

// SortShortRows orders rows by share of stock short, highest first; rows
// without a share count follow, by days to cover.
func SortShortRows(rows []ShortRow) {
	slices.SortStableFunc(rows, func(a, b ShortRow) int {
		an, bn := math.IsNaN(a.Pct), math.IsNaN(b.Pct)
		switch {
		case an != bn:
			if an {
				return 1
			}
			return -1
		case !an && a.Pct != b.Pct:
			if a.Pct > b.Pct {
				return -1
			}
			return 1
		case a.DaysToCover > b.DaysToCover:
			return -1
		case a.DaysToCover < b.DaysToCover:
			return 1
		}
		return 0
	})
}
