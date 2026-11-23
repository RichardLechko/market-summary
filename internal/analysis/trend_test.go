package analysis

import (
	"math"
	"testing"

	"github.com/RichardLechko/market-summary/internal/stats"
)

func monthly(start string, vals ...float64) []stats.Obs {
	t := day(start)
	out := make([]stats.Obs, len(vals))
	for i, v := range vals {
		out[i] = stats.Obs{Date: t.AddDate(0, i, 0), Value: v}
	}
	return out
}

func TestTrendLevelKeepsPastYear(t *testing.T) {
	obs := monthly("2025-01-01", 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23)
	got := Trend("level", obs, obs[len(obs)-1].Date) // 2026-11-01
	if len(got) != 13 || got[0] != 11 || got[12] != 23 {
		t.Fatalf("got %v, want 11..23", got)
	}
}

func TestTrendMonthlyChangeAndYoY(t *testing.T) {
	obs := monthly("2025-01-01", 100, 110, 105, 120)
	if got := Trend("monthly_change", obs, obs[3].Date); len(got) != 3 || got[0] != 10 || got[1] != -5 || got[2] != 15 {
		t.Fatalf("monthly_change: got %v", got)
	}
	idx := monthly("2025-01-01", 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 103, 104)
	got := Trend("index_yoy", idx, idx[13].Date)
	if len(got) != 2 || math.Abs(got[0]-3) > 1e-9 || math.Abs(got[1]-4) > 1e-9 {
		t.Fatalf("index_yoy: got %v, want [3 4]", got)
	}
}

func TestThinKeepsEnds(t *testing.T) {
	vals := make([]float64, 250)
	for i := range vals {
		vals[i] = float64(i)
	}
	got := thin(vals, 60)
	if len(got) != 60 || got[0] != 0 || got[59] != 249 {
		t.Fatalf("len %d first %v last %v", len(got), got[0], got[len(got)-1])
	}
}
