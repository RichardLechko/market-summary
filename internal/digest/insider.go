package digest

import (
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/RichardLechko/market-summary/internal/analysis"
)

// InsiderView is one person's notable purchases or sales.
type InsiderView struct {
	Ticker string
	Who    string
	Role   string
	What   string // e.g. "Sold 110,000 shares for $17.1M"
	Detail string
}

// NewInsiderView renders an insider row.
func NewInsiderView(r analysis.InsiderRow) InsiderView {
	verb := "Sold"
	if r.Buy {
		verb = "Bought"
	}
	v := InsiderView{
		Ticker: r.Ticker,
		Who:    personName(r.Owner),
		Role:   roleText(r.Role),
		What:   fmt.Sprintf("%s %s shares for %s", verb, analysis.FormatNumber(r.Shares, 0), money(r.Value)),
	}
	var d []string
	when := r.First.Format("Jan 2")
	if !r.Last.Equal(r.First) {
		when += "–" + r.Last.Format("Jan 2")
	}
	trades := "1 trade"
	if r.Trades > 1 {
		trades = fmt.Sprintf("%d trades", r.Trades)
	}
	d = append(d, fmt.Sprintf("%s, %s, average $%s.", trades, when, analysis.FormatNumber(r.AvgPrice, 2)))
	if !math.IsNaN(r.PctOfHoldings) {
		if r.Buy {
			d = append(d, fmt.Sprintf("Adds about %.0f%% to the shares held in those accounts.", r.PctOfHoldings))
		} else {
			d = append(d, fmt.Sprintf("About %s of the shares held in those accounts.", pct(r.PctOfHoldings)))
		}
	}
	if r.Plan {
		d = append(d, "Under a pre-arranged trading plan.")
	} else if !r.Buy {
		d = append(d, "Not under a pre-arranged trading plan.")
	}
	v.Detail = strings.Join(d, " ")
	return v
}

func pct(v float64) string {
	if v < 1 {
		return "less than 1%"
	}
	return fmt.Sprintf("%.0f%%", v)
}

func money(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("$%.1fB", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("$%.1fM", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("$%.0fK", v/1e3)
	}
	return fmt.Sprintf("$%.0f", v)
}

var entityWords = []string{"INC", "LLC", "CORP", "TRUST", "FUND", "LP", "L.P.", "CO", "&", "HOLDINGS", "PARTNERS", "CAPITAL", "GROUP", "FOUNDATION", "LTD", "BANK"}

// personName turns SEC "Last First Middle" order into "First Middle Last"
// and fixes all-caps names. Entities are left as filed.
func personName(s string) string {
	parts := strings.Fields(s)
	for _, p := range parts {
		up := strings.ToUpper(strings.Trim(p, ",./"))
		for _, w := range entityWords {
			if up == strings.Trim(w, ".") {
				return s
			}
		}
	}
	if len(parts) < 2 || len(parts) > 4 {
		return s
	}
	if s == strings.ToUpper(s) {
		for i, p := range parts {
			parts[i] = titleWord(p)
		}
	}
	return strings.Join(append(parts[1:], parts[0]), " ")
}

func titleWord(w string) string {
	r := []rune(strings.ToLower(w))
	for i := range r {
		if i == 0 || r[i-1] == '-' || r[i-1] == '\'' {
			r[i] = unicode.ToUpper(r[i])
		}
	}
	return string(r)
}

var acronyms = map[string]bool{"CEO": true, "CFO": true, "COO": true, "CTO": true, "CAO": true, "CIO": true, "CLO": true,
	"EVP": true, "SVP": true, "VP": true, "GC": true, "US": true, "AI": true}

// roleText softens all-caps titles: "PRESIDENT AND CEO" to "President and CEO".
func roleText(s string) string {
	parts := strings.Split(s, ", ")
	for i, p := range parts {
		parts[i] = rolePart(p)
	}
	return strings.Join(parts, ", ")
}

func rolePart(s string) string {
	if s != strings.ToUpper(s) || len(s) <= 4 {
		return s
	}
	words := strings.Fields(s)
	for i, w := range words {
		core := strings.Trim(w, ",.&()")
		switch {
		case acronyms[core]:
		case core == "AND" || core == "OF" || core == "THE":
			words[i] = strings.ToLower(w)
		default:
			words[i] = titleWord(w)
		}
	}
	return strings.Join(words, " ")
}
