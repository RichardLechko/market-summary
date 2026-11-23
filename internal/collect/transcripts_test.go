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
	tr := &Transcripts{HTTP: httpx.New("test", 0), Key: "k", BaseURL: srv.URL}
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
	if _, err := tr.Transcript(ctx, "IBM", "2024Q1"); !errors.Is(err, ErrTranscriptLimit) {
		t.Errorf("limit: err = %v", err)
	}
}
