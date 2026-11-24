package pipeline

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RichardLechko/market-summary/internal/collect"
	"github.com/RichardLechko/market-summary/internal/httpx"
)

// transcriptServer serves last year's call for the fiscal label and this
// quarter's call for the calendar label, so the period check must reject the
// first and accept the second.
func transcriptServer(t *testing.T, limit bool, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if limit {
			fmt.Fprint(w, `{"Information":"rate limit"}`)
			return
		}
		content := "LAST YEAR CALL"
		if r.URL.Query().Get("quarter") == "2026Q3" {
			content = "RIGHT CALL"
		}
		fmt.Fprintf(w, `{"symbol":"JPM","quarter":%q,"transcript":[{"speaker":"Jeremy","title":"CFO","content":%q,"sentiment":"0.5"}]}`,
			r.URL.Query().Get("quarter"), content)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCallNotes(t *testing.T) {
	srv := fixtureServer(t)
	var hits atomic.Int32
	av := transcriptServer(t, false, &hits)
	sender := &fakeSender{}
	p := newTestPipeline(t, srv, &fakeCaller{}, sender)
	p.Cfg.Watchlist[0].Depth = "deep"
	p.AV = &collect.AlphaVantage{HTTP: httpx.New("test", 0), Key: "k", BaseURL: av.URL}
	ctx := context.Background()

	res, err := p.Run(ctx, Options{Now: now0})
	if err != nil || !res.Sent {
		t.Fatalf("run: %+v %v", res, err)
	}
	page := readPage(t, res.Path)
	for _, want := range []string{"On the earnings calls", "Management sounded confident.", "Deposits rose.", "Analysts asked about credit losses.", "Expects steady lending."} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(page, "LAST YEAR CALL") {
		t.Error("wrong-period transcript used")
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("transcript requests = %d, want 2 (fiscal label, then calendar label)", n)
	}

	// A week later: notes already sent are not repeated or fetched again.
	res, err = p.Run(ctx, Options{Now: now0.AddDate(0, 0, 8)})
	if err != nil || !res.Sent {
		t.Fatalf("second run: %+v %v", res, err)
	}
	if strings.Contains(readPage(t, res.Path), "Management sounded confident.") {
		t.Error("call notes repeated")
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("transcript requests after second run = %d, want 2", n)
	}
}

func TestCallNotesLimitAndMissingKey(t *testing.T) {
	srv := fixtureServer(t)
	var hits atomic.Int32
	av := transcriptServer(t, true, &hits)
	p := newTestPipeline(t, srv, &fakeCaller{}, &fakeSender{})
	p.Cfg.Watchlist[0].Depth = "deep"
	p.AV = &collect.AlphaVantage{HTTP: httpx.New("test", 0), Key: "k", BaseURL: av.URL}

	res, err := p.Run(context.Background(), Options{Now: now0, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readPage(t, res.Path), "On the earnings calls") || hits.Load() != 1 {
		t.Errorf("limit: want no notes and one request, got %d requests", hits.Load())
	}

	p.AV = nil
	res, err = p.Run(context.Background(), Options{Now: now0, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readPage(t, res.Path), "ALPHAVANTAGE_API_KEY is not set") {
		t.Error("missing-key note not shown")
	}
}
