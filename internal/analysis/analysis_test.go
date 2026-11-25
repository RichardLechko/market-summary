package analysis

import (
	"math"
	"testing"
	"time"

	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/model"
	"github.com/RichardLechko/market-summary/internal/stats"
)

func day(s string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02", s, time.Local)
	return t
}

func f(v float64) *float64 { return &v }

func TestFormatNumber(t *testing.T) {
	tests := map[float64]string{1234567.891: "1,234,567.89", -1234.5: "-1,234.50", 12: "12.00", 999: "999.00"}
	for in, want := range tests {
		if got := FormatNumber(in, 2); got != want {
			t.Errorf("FormatNumber(%v) = %q want %q", in, got, want)
		}
	}
}

func TestMacroPriceAndRate(t *testing.T) {
	series := []config.Series{
		{ID: "SP500", Kind: "price", Threshold: 2},
		{ID: "DGS10", Kind: "rate", Threshold: 15},
	}
	data := map[string][]stats.Obs{
		"SP500": {{Date: day("2026-11-13"), Value: 6000}, {Date: day("2026-11-20"), Value: 6150}},
		"DGS10": {{Date: day("2026-11-13"), Value: 4.10}, {Date: day("2026-11-20"), Value: 4.30}},
	}
	rows := Macro(series, data, day("2026-11-14"), nil)
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].ChangeText != "+2.5%" || !rows[0].Flagged || !rows[0].New {
		t.Errorf("price row = %+v", rows[0])
	}
	if rows[1].ChangeText != "+20 bp" || !rows[1].Flagged {
		t.Errorf("rate row = %+v", rows[1])
	}
}

func TestMacroIndexYoYNewness(t *testing.T) {
	series := []config.Series{{ID: "CPI", Kind: "index_yoy", Threshold: 0.3}}
	data := map[string][]stats.Obs{"CPI": {
		{Date: day("2025-09-01"), Value: 100}, {Date: day("2025-10-01"), Value: 100},
		{Date: day("2026-09-01"), Value: 103}, {Date: day("2026-10-01"), Value: 103.2},
	}}
	rows := Macro(series, data, day("2026-11-14"), map[string]time.Time{"CPI": day("2026-09-01")})
	r := rows[0]
	if r.ValueText != "3.2% y/y" || r.ChangeText != "+0.2 pp vs prior month" || !r.New || r.Flagged {
		t.Fatalf("cpi row = %+v", r)
	}
	rows = Macro(series, data, day("2026-11-14"), map[string]time.Time{"CPI": day("2026-10-01")})
	if rows[0].New {
		t.Fatal("already-reported observation marked new")
	}
}

func TestMacroMonthlyChange(t *testing.T) {
	series := []config.Series{{ID: "PAYEMS", Kind: "monthly_change", Unit: "K"}}
	data := map[string][]stats.Obs{"PAYEMS": {
		{Date: day("2026-08-01"), Value: 1000}, {Date: day("2026-09-01"), Value: 1120}, {Date: day("2026-10-01"), Value: 1270},
	}}
	r := Macro(series, data, day("2026-11-14"), nil)[0]
	if r.ValueText != "+150K" || r.ChangeText != "prior month +120K" {
		t.Fatalf("payrolls row = %+v", r)
	}
}

func TestGrowth(t *testing.T) {
	if g := Growth(f(110), f(100)); g == nil || *g != 10 {
		t.Fatalf("Growth = %v", g)
	}
	if Growth(f(1), f(-1)) != nil || Growth(nil, f(1)) != nil {
		t.Fatal("Growth should be nil for non-positive prior or missing value")
	}
}

func TestSummarize(t *testing.T) {
	mk := func(ticker, sector, filed string, rev, ago float64, guide string) EarningsRow {
		return NewEarningsRow(config.Company{Ticker: ticker, Sector: sector},
			model.Filing{Ticker: ticker, Filed: day(filed), Facts: &model.EarningsFacts{Revenue: f(rev), RevenueYearAgo: f(ago), Guidance: guide}})
	}
	rows := []EarningsRow{
		mk("JPM", "Banks", "2026-10-14", 110, 100, "none"),
		mk("BAC", "Banks", "2026-10-15", 95, 100, "maintained"),
		mk("NVDA", "Tech", "2026-11-19", 150, 100, "raised"),
		mk("NVDA", "Tech", "2026-08-20", 140, 100, "raised"), // older; ignored
	}
	agg := Summarize(rows, 15)
	if agg.Reported != 3 || agg.RevGrowthPositive != 2 || agg.GuidanceRaised != 1 || agg.GuidanceHeld != 1 {
		t.Fatalf("agg = %+v", agg)
	}
	if agg.MedianRevGrowth != 10 {
		t.Fatalf("median = %v", agg.MedianRevGrowth)
	}
	if len(agg.Sectors) != 2 || agg.Sectors[0].Sector != "Banks" || agg.Sectors[0].MedianRevGrowth != 2.5 {
		t.Fatalf("sectors = %+v", agg.Sectors)
	}
	if !math.IsNaN(Summarize(nil, 15).MedianRevGrowth) {
		t.Fatal("empty median should be NaN")
	}
}

func TestExpectedReports(t *testing.T) {
	cos := []config.Company{{Ticker: "JPM"}, {Ticker: "NVDA"}, {Ticker: "WMT"}}
	hist := map[string][]time.Time{
		"JPM":  {day("2026-01-13")},                    // +364 = 2027-01-12
		"NVDA": {day("2026-11-19"), day("2025-11-19")}, // reported recently
		"WMT":  {day("2025-11-20")},
	}
	got := ExpectedReports(cos, hist, day("2027-01-08"), 7)
	if len(got) != 1 || got[0].Company.Ticker != "JPM" || !got[0].Date.Equal(day("2027-01-12")) {
		t.Fatalf("expected = %+v", got)
	}
	got = ExpectedReports(cos, hist, day("2026-11-20"), 7)
	for _, e := range got {
		if e.Company.Ticker == "NVDA" {
			t.Fatal("NVDA already reported within 30 days")
		}
	}
}

func TestApplyInvestment(t *testing.T) {
	rows := []MacroRow{
		{Series: config.Series{ID: "SP500", Fund: true}, Change: -1.43, HasChange: true},
		{Series: config.Series{ID: "VIX"}, Change: 20, HasChange: true},
		{Series: config.Series{ID: "GLD", Fund: true}},
	}
	ApplyInvestment(rows, 10000)
	if got := rows[0].InvestedText; got != "$10,000 is now $9,860" {
		t.Errorf("fund row: %q", got)
	}
	if rows[1].InvestedText != "" || rows[2].InvestedText != "" {
		t.Errorf("non-fund or unchanged rows should have no dollar text: %+v", rows[1:])
	}
	ApplyInvestment(rows[:1], 500)
	if got := rows[0].InvestedText; got != "$500 is now $493" {
		t.Errorf("small amount: %q", got)
	}
}
