package digest

import (
	"fmt"
	"math"

	"github.com/RichardLechko/market-summary/internal/analysis"
)

// ShortView is one rendered short interest row.
type ShortView struct {
	Ticker  string
	Pct     string // share of shares outstanding, or "" when not comparable
	Shares  string // shares short, e.g. "305.3M"
	Change  string // since the previous report, e.g. "+9.2%"
	Days    string // days to cover
	Flagged bool
	Trend   []float64
}

// NewShortView renders a short interest row.
func NewShortView(r analysis.ShortRow) ShortView {
	v := ShortView{
		Ticker:  r.Ticker,
		Shares:  shares(r.Short),
		Days:    fmt.Sprintf("%.1f", r.DaysToCover),
		Flagged: r.Flagged,
		Trend:   r.Trend,
	}
	if !math.IsNaN(r.Pct) {
		v.Pct = fmt.Sprintf("%.1f%%", r.Pct)
	}
	if !math.IsNaN(r.Change) {
		v.Change = fmt.Sprintf("%+.1f%%", r.Change)
	}
	return v
}

func shares(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.2fB", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.0fK", v/1e3)
	}
	return fmt.Sprintf("%.0f", v)
}
