package analysis

import (
	"time"

	"github.com/RichardLechko/market-summary/internal/stats"
)

// trendPoints caps how many points a trend line carries; daily series are
// thinned to roughly weekly so the line reads as a shape, not noise.
const trendPoints = 60

// Trend returns the past year of the quantity a row displays, oldest first:
// the level for prices, rates and levels, the year-over-year rate for
// index_yoy series, and the month-over-month change for monthly_change series.
// obs must be sorted.
func Trend(kind string, obs []stats.Obs, end time.Time) []float64 {
	from := end.AddDate(-1, 0, 0)
	var vals []float64
	for i, o := range obs {
		if o.Date.Before(from) || o.Date.After(end) {
			continue
		}
		switch kind {
		case "index_yoy":
			if v, ok := stats.YoY(obs, i); ok {
				vals = append(vals, v)
			}
		case "monthly_change":
			if i > 0 {
				vals = append(vals, o.Value-obs[i-1].Value)
			}
		default:
			vals = append(vals, o.Value)
		}
	}
	return thin(vals, trendPoints)
}

// thin keeps at most n evenly spaced values, always including the last.
func thin(vals []float64, n int) []float64 {
	if len(vals) <= n || n < 2 {
		return vals
	}
	out := make([]float64, 0, n)
	step := float64(len(vals)-1) / float64(n-1)
	for k := 0; k < n; k++ {
		out = append(out, vals[int(float64(k)*step+0.5)])
	}
	out[n-1] = vals[len(vals)-1]
	return out
}
