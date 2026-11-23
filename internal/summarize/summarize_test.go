package summarize

import (
	"context"
	"strings"
	"testing"
)

type fakeCaller struct {
	calls []Request
	reply string
}

func (f *fakeCaller) Call(_ context.Context, req Request) (string, error) {
	f.calls = append(f.calls, req)
	return f.reply, nil
}

type mapCache map[string]string

func (m mapCache) CacheGet(k string) (string, bool, error) { v, ok := m[k]; return v, ok, nil }
func (m mapCache) CachePut(k, v string) error              { m[k] = v; return nil }

func TestExtractEarningsParsesAndCaches(t *testing.T) {
	fc := &fakeCaller{reply: `{"is_earnings_release":true,"company":"JPMorgan Chase","period":"Q3 2026","unit":"USD millions",
		"revenue":47000,"revenue_year_ago":43000,"eps_diluted":5.1,"eps_diluted_year_ago":4.4,"net_income":null,"net_income_year_ago":null,
		"guidance":"maintained","guidance_note":"","headline":"Strong quarter.","summary":"Paragraph."}`}
	s := &Summarizer{Caller: fc, Cache: mapCache{}, ExtractionModel: "m", OverviewModel: "m", Effort: "medium"}
	in := EarningsInput{Ticker: "JPM", Depth: "deep", Text: "release text", PriorSummary: "last quarter notes"}
	for range 2 {
		facts, err := s.ExtractEarnings(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		if *facts.Revenue != 47000 || facts.NetIncome != nil || facts.Guidance != "maintained" {
			t.Fatalf("facts = %+v", facts)
		}
	}
	if len(fc.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (second should hit cache)", len(fc.calls))
	}
	req := fc.calls[0]
	if req.Schema == nil || !strings.Contains(req.User, "Depth: standard") || !strings.Contains(req.User, "last quarter notes") {
		t.Fatalf("request = %+v", req)
	}
}

func TestExtractEarningsBadJSON(t *testing.T) {
	s := &Summarizer{Caller: &fakeCaller{reply: "not json"}}
	if _, err := s.ExtractEarnings(context.Background(), EarningsInput{Ticker: "X"}); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestOverviewFreeText(t *testing.T) {
	fc := &fakeCaller{reply: "The market rose."}
	s := &Summarizer{Caller: fc, OverviewModel: "big", ExtractionModel: "small"}
	out, err := s.Overview(context.Background(), "facts")
	if err != nil || out != "The market rose." {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if fc.calls[0].Model != "big" || fc.calls[0].Schema != nil {
		t.Fatalf("request = %+v", fc.calls[0])
	}
}
