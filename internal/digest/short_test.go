package digest

import (
	"math"
	"testing"

	"github.com/RichardLechko/market-summary/internal/analysis"
)

func TestNewShortView(t *testing.T) {
	v := NewShortView(analysis.ShortRow{Ticker: "NVDA", Short: 305326286, DaysToCover: 1, Pct: 1.267, Change: 9.21})
	if v.Pct != "1.3%" || v.Shares != "305.3M" || v.Change != "+9.2%" || v.Days != "1.0" {
		t.Errorf("view = %+v", v)
	}
	v = NewShortView(analysis.ShortRow{Ticker: "GOOGL", Short: 78649008, DaysToCover: 2.09, Pct: math.NaN(), Change: math.NaN()})
	if v.Pct != "" || v.Change != "" || v.Shares != "78.6M" {
		t.Errorf("view without share count = %+v", v)
	}
}
