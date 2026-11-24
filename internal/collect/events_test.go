package collect

import (
	"slices"
	"testing"
)

func TestEventLabels(t *testing.T) {
	cases := []struct {
		form, items string
		want        []string
	}{
		{"8-K", "5.02,9.01", []string{"Leadership change"}},
		{"8-K", "1.01,2.03,7.01", []string{"Major agreement"}},
		{"8-K", "2.02,9.01", nil},
		{"8-K", "5.07", nil},
		{"8-K/A", "4.02", []string{"Past results corrected"}},
		{"6-K", "5.02", nil},
	}
	for _, c := range cases {
		if got := EventLabels(c.form, c.items); !slices.Equal(got, c.want) {
			t.Errorf("EventLabels(%q, %q) = %v, want %v", c.form, c.items, got, c.want)
		}
	}
}
