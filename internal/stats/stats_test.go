package stats

import (
	"math"
	"testing"
	"time"
)

func d(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func TestOnOrBefore(t *testing.T) {
	obs := []Obs{{d("2026-11-16"), 1}, {d("2026-11-17"), 2}, {d("2026-11-19"), 3}}
	tests := []struct {
		at   string
		want float64
		ok   bool
	}{
		{"2026-11-15", 0, false},
		{"2026-11-16", 1, true},
		{"2026-11-18", 2, true},
		{"2026-12-01", 3, true},
	}
	for _, tt := range tests {
		got, ok := OnOrBefore(obs, d(tt.at))
		if ok != tt.ok || got.Value != tt.want {
			t.Errorf("OnOrBefore(%s) = %v,%v want %v,%v", tt.at, got.Value, ok, tt.want, tt.ok)
		}
	}
}

func TestPctChange(t *testing.T) {
	if got, ok := PctChange(200, 210); !ok || math.Abs(got-5) > 1e-9 {
		t.Fatalf("PctChange = %v,%v", got, ok)
	}
	if _, ok := PctChange(0, 1); ok {
		t.Fatal("PctChange from zero should not be ok")
	}
}

func TestYoY(t *testing.T) {
	obs := []Obs{
		{d("2025-09-01"), 100},
		{d("2025-10-01"), 101},
		{d("2026-09-01"), 103},
		{d("2026-10-01"), 104.03},
	}
	got, ok := YoY(obs, 3)
	if !ok || math.Abs(got-3) > 1e-9 {
		t.Fatalf("YoY = %v,%v want 3", got, ok)
	}
	if _, ok := YoY(obs, 1); ok {
		t.Fatal("YoY with no year-ago observation should not be ok")
	}
}

func TestMedianMean(t *testing.T) {
	if got := Median([]float64{3, 1, 2}); got != 2 {
		t.Fatalf("Median odd = %v", got)
	}
	if got := Median([]float64{4, 1, 2, 3}); got != 2.5 {
		t.Fatalf("Median even = %v", got)
	}
	if !math.IsNaN(Median(nil)) {
		t.Fatal("Median(nil) should be NaN")
	}
	if got := Mean([]float64{1, 2, 3}); got != 2 {
		t.Fatalf("Mean = %v", got)
	}
}
