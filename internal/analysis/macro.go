// Package analysis computes facts deterministically. It never calls the LLM.
package analysis

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/stats"
)

// MacroRow is one line of the Market or Macro section.
type MacroRow struct {
	Series     config.Series
	Value      float64
	ValueText  string
	Change     float64
	ChangeText string
	HasChange  bool
	AsOf       time.Time
	New        bool // has data newer than the last digest
	Flagged    bool
}

// Macro computes a row per series. windowStart is the cutoff of the previous
// digest; lastReported holds, per series, the latest observation date shown
// in the previous digest (used for monthly and weekly series).
func Macro(series []config.Series, data map[string][]stats.Obs, windowStart time.Time, lastReported map[string]time.Time) []MacroRow {
	var out []MacroRow
	for _, s := range series {
		obs := stats.Sorted(data[s.ID])
		row, ok := macroRow(s, obs, windowStart, lastReported[s.ID])
		if ok {
			out = append(out, row)
		}
	}
	return out
}

func macroRow(s config.Series, obs []stats.Obs, windowStart, reported time.Time) (MacroRow, bool) {
	last, ok := stats.Latest(obs)
	if !ok {
		return MacroRow{}, false
	}
	i := len(obs) - 1
	row := MacroRow{Series: s, AsOf: last.Date}
	dec := 2

	switch s.Kind {
	case "price":
		row.Value = last.Value
		row.ValueText = FormatNumber(last.Value, dec)
		row.New = last.Date.After(windowStart)
		if ref, ok := stats.OnOrBefore(obs, windowStart); ok {
			if pc, ok := stats.PctChange(ref.Value, last.Value); ok {
				row.Change, row.HasChange = pc, true
				row.ChangeText = signed(pc, 1) + "%"
			}
		}
	case "rate":
		row.Value = last.Value
		row.ValueText = fmt.Sprintf("%.2f%%", last.Value)
		row.New = last.Date.After(windowStart)
		if ref, ok := stats.OnOrBefore(obs, windowStart); ok {
			bp := math.Round((last.Value - ref.Value) * 100)
			row.Change, row.HasChange = bp, true
			row.ChangeText = signed(bp, 0) + " bp"
		}
	case "index_yoy":
		yoy, ok := stats.YoY(obs, i)
		if !ok {
			return MacroRow{}, false
		}
		row.Value = yoy
		row.ValueText = fmt.Sprintf("%.1f%% y/y", yoy)
		row.New = last.Date.After(reported)
		if prev, ok := stats.YoY(obs, i-1); ok {
			pp := round(yoy-prev, 1)
			row.Change, row.HasChange = pp, true
			row.ChangeText = signed(pp, 1) + " pp vs prior month"
		}
	case "monthly_change":
		if i < 1 {
			return MacroRow{}, false
		}
		chg := last.Value - obs[i-1].Value
		row.Value = chg
		row.ValueText = signed(chg, 0) + s.Unit
		row.New = last.Date.After(reported)
		if i >= 2 {
			prior := obs[i-1].Value - obs[i-2].Value
			row.Change, row.HasChange = chg-prior, true
			row.ChangeText = "prior month " + signed(prior, 0) + s.Unit
		}
	case "level":
		row.Value = last.Value
		if s.Unit == "%" {
			row.ValueText = fmt.Sprintf("%.1f%%", last.Value)
		} else {
			if math.Abs(last.Value) >= 1000 {
				dec = 0
			}
			row.ValueText = FormatNumber(last.Value, dec) + s.Unit
		}
		row.New = last.Date.After(reported)
		if prev, ok := stats.Previous(obs, i); ok {
			if s.Delta == "pct" {
				if pc, ok := stats.PctChange(prev.Value, last.Value); ok {
					row.Change, row.HasChange = pc, true
					row.ChangeText = signed(pc, 1) + "% vs prior"
				}
			} else {
				d := round(last.Value-prev.Value, 2)
				row.Change, row.HasChange = d, true
				unit := s.Unit
				if unit == "%" {
					unit = " pp"
				}
				row.ChangeText = signed(d, 1) + unit + " vs prior"
			}
		}
	}
	row.Flagged = row.HasChange && s.Threshold > 0 && math.Abs(row.Change) >= s.Threshold
	return row, true
}

func round(v float64, dec int) float64 {
	p := math.Pow(10, float64(dec))
	return math.Round(v*p) / p
}

func signed(v float64, dec int) string {
	s := FormatNumber(math.Abs(v), dec)
	switch {
	case round(v, dec) > 0:
		return "+" + s
	case round(v, dec) < 0:
		return "-" + s
	default:
		return s
	}
}

// FormatNumber formats v with thousands separators and dec decimals.
func FormatNumber(v float64, dec int) string {
	s := fmt.Sprintf("%.*f", dec, v)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := b.String()
	if hasFrac {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}
