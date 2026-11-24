package collect

import (
	"slices"
	"strings"
)

// eventItems are the 8-K items reported as company news, with plain labels.
// Routine items (shareholder votes, Regulation FD notices, "other events")
// are left out.
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
}

// EventLabels returns plain labels for the news items an 8-K reports, or nil
// if it reports none.
func EventLabels(form, items string) []string {
	if form != "8-K" && form != "8-K/A" {
		return nil
	}
	have := strings.Split(items, ",")
	var out []string
	for _, e := range eventItems {
		if slices.Contains(have, e.Item) {
			out = append(out, e.Label)
		}
	}
	return out
}
