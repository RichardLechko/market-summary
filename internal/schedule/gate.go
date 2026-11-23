// Package schedule decides whether a gated run is due.
package schedule

import "time"

// Decision is the outcome of the run gate.
type Decision struct {
	Due bool
	// NextEligible is the first calendar date (local midnight) a run is due.
	// Zero when there has never been a successful send.
	NextEligible time.Time
}

// Decide reports whether a digest is due. lastSent is the local calendar date
// of the last successful send (zero if never). Days are counted by calendar
// date, not elapsed hours, so a login earlier in the day than last week's
// still counts as a full gap.
func Decide(lastSent, now time.Time, minGapDays int) Decision {
	if lastSent.IsZero() {
		return Decision{Due: true}
	}
	next := dateOf(lastSent).AddDate(0, 0, minGapDays)
	return Decision{Due: !dateOf(now).Before(next), NextEligible: next}
}

func dateOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
