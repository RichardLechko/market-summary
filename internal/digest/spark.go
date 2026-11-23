package digest

import (
	"fmt"
	"html/template"
	"math"
	"strings"

	"github.com/RichardLechko/market-summary/internal/analysis"
)

// Sparkline geometry, in viewBox units. CSS sets the rendered size.
const (
	sparkW   = 100.0
	sparkH   = 28.0
	sparkPad = 3.0
)

// sparkline renders a past-year trend as a small inline SVG line with a dot on
// the latest value. A faint zero line is drawn when the values cross zero.
// It returns "" when there are too few points to show a shape.
func sparkline(vals []float64, flagged bool) template.HTML {
	if len(vals) < 3 {
		return ""
	}
	lo, hi := vals[0], vals[0]
	for _, v := range vals {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	span := hi - lo
	y := func(v float64) float64 {
		if span == 0 {
			return sparkH / 2
		}
		return sparkPad + (hi-v)/span*(sparkH-2*sparkPad)
	}
	x := func(i int) float64 {
		return sparkPad + float64(i)/float64(len(vals)-1)*(sparkW-2*sparkPad)
	}

	var path strings.Builder
	for i, v := range vals {
		cmd := "L"
		if i == 0 {
			cmd = "M"
		}
		fmt.Fprintf(&path, "%s%.1f %.1f", cmd, x(i), y(v))
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="spark" viewBox="0 0 %.0f %.0f" role="img" aria-label="Past year: low %s, high %s">`,
		sparkW, sparkH, label(lo), label(hi))
	fmt.Fprintf(&b, `<title>Past year: low %s, high %s</title>`, label(lo), label(hi))
	if lo < 0 && hi > 0 {
		fmt.Fprintf(&b, `<line class="zero" x1="0" x2="%.0f" y1="%.1f" y2="%.1f"/>`, sparkW, y(0), y(0))
	}
	fmt.Fprintf(&b, `<path d="%s"/>`, path.String())
	dot := "end"
	if flagged {
		dot += " flag"
	}
	last := len(vals) - 1
	fmt.Fprintf(&b, `<circle class="%s" cx="%.1f" cy="%.1f" r="2.2"/>`, dot, x(last), y(vals[last]))
	b.WriteString(`</svg>`)
	return template.HTML(b.String()) //nolint:gosec // built only from numbers
}

func label(v float64) string {
	dec := 2
	if math.Abs(v) >= 100 {
		dec = 0
	}
	return analysis.FormatNumber(v, dec)
}
