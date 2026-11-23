package schedule

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name     string
		lastSent time.Time
		now      time.Time
		want     bool
	}{
		{"never sent", time.Time{}, at("2026-11-24 09:00"), true},
		{"next day", at("2026-11-24 09:00"), at("2026-11-25 09:00"), false},
		{"six days", at("2026-11-24 09:00"), at("2026-11-30 23:59"), false},
		{"seven days, earlier in the day", at("2026-11-24 09:00"), at("2026-12-01 08:55"), true},
		{"eight days", at("2026-11-24 09:00"), at("2026-12-02 10:00"), true},
		{"same day", at("2026-11-24 09:00"), at("2026-11-24 18:00"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Decide(tt.lastSent, tt.now, 7)
			if got.Due != tt.want {
				t.Fatalf("Decide() due = %v, want %v (next eligible %s)", got.Due, tt.want, got.NextEligible)
			}
		})
	}
}

func TestNextEligible(t *testing.T) {
	d := Decide(at("2026-11-24 21:30"), at("2026-11-25 09:00"), 7)
	if want := at("2026-12-01 00:00"); !d.NextEligible.Equal(want) {
		t.Fatalf("NextEligible = %s, want %s", d.NextEligible, want)
	}
}
