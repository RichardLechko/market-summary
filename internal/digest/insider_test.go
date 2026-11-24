package digest

import (
	"strings"
	"testing"
	"time"

	"github.com/RichardLechko/market-summary/internal/analysis"
)

func TestPersonName(t *testing.T) {
	for in, want := range map[string]string{
		"Teter Timothy S.":             "Timothy S. Teter",
		"HUANG JEN HSUN":               "Jen Hsun Huang",
		"Walton Family Holdings Trust": "Walton Family Holdings Trust",
		"BANK OF AMERICA CORP /DE/":    "BANK OF AMERICA CORP /DE/",
		"Cher":                         "Cher",
	} {
		if got := personName(in); got != want {
			t.Errorf("personName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewInsiderView(t *testing.T) {
	d := time.Date(2026, 11, 18, 0, 0, 0, 0, time.Local)
	v := NewInsiderView(analysis.InsiderRow{
		Ticker: "NVDA", Owner: "Teter Timothy S.", Role: "EVP", Trades: 3, Shares: 110000, Value: 17_100_000,
		AvgPrice: 155.45, PctOfHoldings: 10, Plan: true, First: d, Last: d.AddDate(0, 0, 1),
	})
	if v.What != "Sold 110,000 shares for $17.1M" {
		t.Errorf("What = %q", v.What)
	}
	for _, want := range []string{"3 trades, Nov 18–Nov 19, average $155.45.", "About 10%", "pre-arranged"} {
		if !strings.Contains(v.Detail, want) {
			t.Errorf("Detail %q missing %q", v.Detail, want)
		}
	}
}

func TestRoleText(t *testing.T) {
	for in, want := range map[string]string{
		"PRESIDENT AND CEO, Director": "President and CEO, Director",
		"PRESIDENT AND CEO":           "President and CEO",
		"EVP CHIEF FINANCIAL OFFICER": "EVP Chief Financial Officer",
		"VP":                          "VP",
	} {
		if got := roleText(in); got != want {
			t.Errorf("roleText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewClusterView(t *testing.T) {
	d := time.Date(2026, 11, 3, 0, 0, 0, 0, time.Local)
	v := NewClusterView(analysis.InsiderCluster{Ticker: "AAA", Buyers: []string{"Lee Ann", "Kim Bob", "Park Cy"}, Value: 1_200_000, First: d, Last: d.AddDate(0, 0, 17)})
	if want := "3 insiders bought $1.2M of shares on the open market between Nov 3 and Nov 20: Ann Lee, Bob Kim and Cy Park."; v.Text != want {
		t.Errorf("Text = %q", v.Text)
	}
}
