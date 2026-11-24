package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RichardLechko/market-summary/internal/httpx"
)

func TestFINRAShortInterest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/data/group/otcMarket/name/consolidatedShortInterest" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		var req struct {
			DomainFilters []struct {
				Values []string `json:"values"`
			} `json:"domainFilters"`
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &req); err != nil || len(req.DomainFilters) != 1 || len(req.DomainFilters[0].Values) != 2 {
			t.Errorf("request = %s", b)
		}
		fmt.Fprint(w, `[{"symbolCode":"NVDA","settlementDate":"2026-11-13","currentShortPositionQuantity":305326286,"averageDailyVolumeQuantity":312384862,"daysToCoverQuantity":1},
			{"symbolCode":"ARM","settlementDate":"2026-11-13","currentShortPositionQuantity":1000,"averageDailyVolumeQuantity":500,"daysToCoverQuantity":null}]`)
	}))
	defer srv.Close()
	f := &FINRA{HTTP: httpx.New("t", 0), BaseURL: srv.URL}
	got, err := f.ShortInterest(context.Background(), []string{"NVDA", "ARM"}, time.Now().AddDate(-1, 0, 0), time.Now())
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v %v", got, err)
	}
	if got[0].Short != 305326286 || got[0].DaysToCover != 1 || got[0].Settlement.Format("2006-01-02") != "2026-11-13" {
		t.Errorf("NVDA = %+v", got[0])
	}
	if got[1].DaysToCover != 2 {
		t.Errorf("days to cover computed from volume = %v, want 2", got[1].DaysToCover)
	}
}

func TestSharesOutstanding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/xbrl/companyconcept/CIK0001652044/dei/EntityCommonStockSharesOutstanding.json" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"units":{"shares":[{"end":"2026-05-15","val":24200000000},{"end":"2026-08-21","val":24100000000},{"end":"2026-02-20","val":24300000000}]}}`)
	}))
	defer srv.Close()
	e := &EDGAR{HTTP: httpx.New("t", 0), DataBase: srv.URL}
	n, asOf, found, err := e.SharesOutstanding(context.Background(), 1045810)
	if err != nil || !found || n != 24.1e9 || asOf.Format("2006-01-02") != "2026-08-21" {
		t.Errorf("got %v %v %v %v", n, asOf, found, err)
	}
	if _, _, found, err := e.SharesOutstanding(context.Background(), 1652044); err != nil || found {
		t.Errorf("multi-class: found = %v, err = %v", found, err)
	}
}
