package analysis

import (
	"math"
	"testing"
	"time"

	"github.com/RichardLechko/market-summary/internal/model"
)

func TestInsiderRows(t *testing.T) {
	d1 := time.Date(2026, 11, 18, 0, 0, 0, 0, time.Local)
	d2 := d1.AddDate(0, 0, 1)
	filings := []model.InsiderFiling{
		{Ticker: "NVDA", Trades: []model.InsiderTrade{
			{Owner: "Huang", Code: "S", Date: d1, Shares: 50_000, Price: 150, OwnedAfter: 1_000_000, Account: "By Trust", Plan: true},
			{Owner: "Huang", Code: "S", Date: d2, Shares: 50_000, Price: 160, OwnedAfter: 950_000, Account: "By Trust", Plan: true},
			{Owner: "Huang", Code: "S", Date: d2, Shares: 10_000, Price: 160, OwnedAfter: 40_000, Account: "Direct", Plan: false},
		}},
		{Ticker: "SMCI", Trades: []model.InsiderTrade{
			{Owner: "Lee", Code: "P", Date: d1, Shares: 2_000, Price: 40, OwnedAfter: 10_000, Account: "Direct"},
			{Owner: "Kim", Code: "S", Date: d1, Shares: 1_000, Price: 40, OwnedAfter: 100_000, Account: "Direct"},
		}},
	}
	rows := InsiderRows(filings)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	buy := rows[0]
	if !buy.Buy || buy.Owner != "Lee" || buy.Value != 80_000 || buy.Notable() {
		t.Errorf("buy row = %+v; $80k purchase should not be notable", buy)
	}
	h := rows[1]
	if h.Owner != "Huang" || h.Trades != 3 || h.Shares != 110_000 || h.Value != 17_100_000 {
		t.Errorf("huang row = %+v", h)
	}
	if h.After != 990_000 || h.Plan {
		t.Errorf("after = %v plan = %v; want 990000 and false (one trade off-plan)", h.After, h.Plan)
	}
	if want := 110_000.0 / 1_100_000 * 100; math.Abs(h.PctOfHoldings-want) > 1e-9 || !h.Notable() {
		t.Errorf("pct = %v, notable = %v", h.PctOfHoldings, h.Notable())
	}
	if rows[2].Notable() {
		t.Error("small sale should not be notable")
	}
}
