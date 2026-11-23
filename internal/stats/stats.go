// Package stats holds small time-series helpers used by analysis.
package stats

import (
	"math"
	"sort"
	"time"
)

// Obs is one dated observation.
type Obs struct {
	Date  time.Time
	Value float64
}

// Sorted returns a copy of obs sorted by date ascending.
func Sorted(obs []Obs) []Obs {
	out := append([]Obs(nil), obs...)
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

// Latest returns the last observation of a sorted series.
func Latest(obs []Obs) (Obs, bool) {
	if len(obs) == 0 {
		return Obs{}, false
	}
	return obs[len(obs)-1], true
}

// OnOrBefore returns the last observation dated on or before t in a sorted
// series.
func OnOrBefore(obs []Obs, t time.Time) (Obs, bool) {
	i := sort.Search(len(obs), func(i int) bool { return obs[i].Date.After(t) })
	if i == 0 {
		return Obs{}, false
	}
	return obs[i-1], true
}

// Previous returns the observation immediately before index i.
func Previous(obs []Obs, i int) (Obs, bool) {
	if i <= 0 || i > len(obs) {
		return Obs{}, false
	}
	return obs[i-1], true
}

// PctChange returns (to/from - 1) * 100. ok is false when from is zero.
func PctChange(from, to float64) (float64, bool) {
	if from == 0 {
		return 0, false
	}
	return (to/from - 1) * 100, true
}

// YoY returns the year-over-year percent change for the observation at index
// i of a sorted monthly series, using the observation dated exactly one year
// earlier (same month).
func YoY(obs []Obs, i int) (float64, bool) {
	if i < 0 || i >= len(obs) {
		return 0, false
	}
	target := obs[i].Date.AddDate(-1, 0, 0)
	for j := i - 1; j >= 0; j-- {
		d := obs[j].Date
		if d.Year() == target.Year() && d.Month() == target.Month() {
			return PctChange(obs[j].Value, obs[i].Value)
		}
		if d.Before(target) {
			break
		}
	}
	return 0, false
}

// Median returns the median of xs, or NaN when empty.
func Median(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// Mean returns the arithmetic mean of xs, or NaN when empty.
func Mean(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}
