package analysis

import (
	"math"
	"testing"
	"time"

	"github.com/RichardLechko/market-summary/internal/model"
)

func shortHist(vals ...float64) []model.ShortInterest {
	d := time.Date(2026, 1, 15, 0, 0, 0, 0, time.Local)
	var out []model.ShortInterest
	for i, v := range vals {
		out = append(out, model.ShortInterest{Settlement: d.AddDate(0, 0, 15*i), Short: v, DaysToCover: 3})
	}
	return out
}

func TestNewShortRow(t *testing.T) {
	r, ok := NewShortRow("AAA", shortHist(4e6, 5e6), 100e6)
	if !ok || math.Abs(r.Pct-5) > 1e-9 || math.Abs(r.Change-25) > 1e-9 || !r.Flagged {
		t.Errorf("rise from a 5%% base = %+v", r)
	}
	if r, _ := NewShortRow("TINY", shortHist(300e3, 400e3), 100e6); r.Flagged {
		t.Errorf("rise from a 0.4%% base flagged: %+v", r)
	}
	if r, _ := NewShortRow("HEAVY", shortHist(16e6, 16e6), 100e6); !r.Flagged {
		t.Errorf("16%% short not flagged: %+v", r)
	}
	r, _ = NewShortRow("GOOGL", shortHist(80e6, 100e6), 0)
	if !math.IsNaN(r.Pct) || !r.Flagged || len(r.Trend) != 2 || r.Trend[1] != 100e6 {
		t.Errorf("no share count: %+v", r)
	}
	if _, ok := NewShortRow("NONE", nil, 1); ok {
		t.Error("row without reports")
	}
}

func TestSortShortRows(t *testing.T) {
	rows := []ShortRow{
		{Ticker: "N1", Pct: math.NaN(), DaysToCover: 2},
		{Ticker: "P1", Pct: 3},
		{Ticker: "N2", Pct: math.NaN(), DaysToCover: 5},
		{Ticker: "P2", Pct: 9},
	}
	SortShortRows(rows)
	var got string
	for _, r := range rows {
		got += r.Ticker + " "
	}
	if got != "P2 P1 N2 N1 " {
		t.Errorf("order = %s", got)
	}
}
