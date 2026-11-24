package collect

import (
	"slices"
	"strings"
)

// eventItems are the 8-K items reported as company news, with plain labels.
// Shareholder vote results (5.07) and Regulation FD notices (7.01), mostly
// investor slides, are left out.
var eventItems = []struct{ Item, Label string }{
	{"1.01", "Major agreement"},
	{"1.02", "Agreement ended"},
	{"1.03", "Bankruptcy"},
	{"2.01", "Acquisition or sale completed"},
	{"2.05", "Restructuring"},
	{"2.06", "Write-down"},
	{"3.01", "Delisting notice"},
	{"4.01", "Auditor change"},
	{"4.02", "Past results corrected"},
	{"5.01", "Change in control"},
	{"5.02", "Leadership change"},
	{"5.03", "Charter or bylaw change"},
	{"8.01", "Other news"},
}

// Is13D reports whether a form is a Schedule 13D: a stake above 5% held by
// an investor who may seek to influence the company.
func Is13D(form string) bool {
	return strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(form, "SCHEDULE "), "SC "), "/A") == "13D"
}

// EventLabels returns plain labels for the news a filing may carry, or nil
// if it is not a news filing.
func EventLabels(form, items string) []string {
	switch {
	case Is13D(form) && strings.HasSuffix(form, "/A"):
		return []string{"Activist stake update"}
	case Is13D(form):
		return []string{"Activist stake"}
	case form == "6-K":
		return []string{"Report from a foreign company"}
	case form != "8-K" && form != "8-K/A":
		return nil
	}
	have := strings.Split(items, ",")
	var out []string
	for _, e := range eventItems {
		if slices.Contains(have, e.Item) {
			out = append(out, e.Label)
		}
	}
	// An earnings 8-K's "other events" item is nearly always the dividend;
	// the earnings summary covers it.
	if slices.Contains(have, "2.02") && slices.Equal(out, []string{"Other news"}) {
		return nil
	}
	return out
}
