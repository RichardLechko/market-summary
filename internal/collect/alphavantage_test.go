package collect

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RichardLechko/market-summary/internal/httpx"
)

func TestTranscriptQuarters(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse("2006-01-02", s); return v }
	cases := []struct {
		period string
		filed  time.Time
		want   []string
	}{
		{"Q3 FY2027", d("2026-11-19"), []string{"2027Q3", "2026Q3"}},
		{"Q3 FY2026", d("2026-10-13"), []string{"2026Q3"}},
		{"Q1 FY2027 (quarter ended September 30, 2026)", d("2026-10-28"), []string{"2027Q1", "2026Q3"}},
		{"Q4 2026", d("2027-01-15"), []string{"2026Q4"}},
		{"", d("2026-02-03"), []string{"2025Q4"}},
	}
	for _, c := range cases {
		if got := TranscriptQuarters(c.period, c.filed); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q %s: got %v, want %v", c.period, c.filed.Format("2006-01-02"), got, c.want)
		}
	}
}

func TestTranscript(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("function") != "EARNINGS_CALL_TRANSCRIPT" {
			t.Errorf("function = %q", r.URL.Query().Get("function"))
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	tr := &AlphaVantage{HTTP: httpx.New("test", 0), Key: "k", BaseURL: srv.URL}
	ctx := context.Background()

	body = `{"symbol":"IBM","quarter":"2024Q1","transcript":[{"speaker":"Arvind","title":"CEO","content":" Thanks. ","sentiment":"0.6"}]}`
	parts, err := tr.Transcript(ctx, "IBM", "2024Q1")
	if err != nil || len(parts) != 1 {
		t.Fatalf("got %v %v", parts, err)
	}
	if got := TranscriptText(parts); !strings.HasPrefix(got, "Arvind (CEO): Thanks.") {
		t.Errorf("text = %q", got)
	}

	body = `{"symbol":"IBM","quarter":"2030Q1","transcript":[]}`
	if parts, err := tr.Transcript(ctx, "IBM", "2030Q1"); err != nil || len(parts) != 0 {
		t.Errorf("missing transcript: %v %v", parts, err)
	}

	body = `{"Information":"Our standard API rate limit is 25 requests per day."}`
	if _, err := tr.Transcript(ctx, "IBM", "2024Q1"); !errors.Is(err, ErrAVLimit) {
		t.Errorf("limit: err = %v", err)
	}
}

func TestDaily(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("function") != "TIME_SERIES_DAILY" || r.URL.Query().Get("symbol") != "GLD" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	av := &AlphaVantage{HTTP: httpx.New("test", 0), Key: "k", BaseURL: srv.URL}

	body = `{"Meta Data":{"2. Symbol":"GLD"},"Time Series (Daily)":{
		"2026-11-20":{"1. open":"380.0","4. close":"381.25","5. volume":"1"},
		"2026-11-19":{"1. open":"379.0","4. close":"379.50","5. volume":"1"}}}`
	obs, err := av.Daily(context.Background(), "GLD")
	if err != nil || len(obs) != 2 || obs[0].Value != 379.5 || obs[1].Value != 381.25 || !obs[0].Date.Before(obs[1].Date) {
		t.Fatalf("got %+v %v", obs, err)
	}

	body = `{"Note":"Thank you for using Alpha Vantage! Our standard API call frequency is 25 requests per day."}`
	if _, err := av.Daily(context.Background(), "GLD"); !errors.Is(err, ErrAVLimit) {
		t.Errorf("limit: err = %v", err)
	}
}

func TestEarningsCalendar(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("function") != "EARNINGS_CALENDAR" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	av := &AlphaVantage{HTTP: httpx.New("test", 0), Key: "k", BaseURL: srv.URL}

	body = "symbol,name,reportDate,fiscalDateEnding,estimate,currency,timeOfTheDay\r\n" +
		"CRWD,CROWDSTRIKE HOLDINGS INCORPORATED,2026-12-01,2026-10-31,0.0,USD,post-market\r\n" +
		"ABC,\"ABC, INC\",2026-12-02,2026-10-31,,USD,\r\n" +
		"BAD,BAD ROW,not-a-date,,,USD,\r\n"
	es, err := av.EarningsCalendar(context.Background())
	if err != nil || len(es) != 2 {
		t.Fatalf("got %+v %v", es, err)
	}
	if es[0].Symbol != "CRWD" || es[0].Date.Format("2006-01-02") != "2026-12-01" || es[0].Time != "post-market" || es[1].Time != "" {
		t.Errorf("entries = %+v", es)
	}

	body = `{"Information":"Thank you for using Alpha Vantage! Our standard API rate limit is 25 requests per day."}`
	if _, err := av.EarningsCalendar(context.Background()); !errors.Is(err, ErrAVLimit) {
		t.Errorf("limit: err = %v", err)
	}
}

func TestEarningsCalendarLimitAsCSV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "symbol,name,reportDate,fiscalDateEnding,estimate,currency,timeOfTheDay\r\nI,n,f,o,r,m,a\r\n")
	}))
	defer srv.Close()
	av := &AlphaVantage{HTTP: httpx.New("test", 0), Key: "k", BaseURL: srv.URL}
	if _, err := av.EarningsCalendar(context.Background()); !errors.Is(err, ErrAVLimit) {
		t.Errorf("err = %v, want ErrAVLimit", err)
	}
}
