package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/RichardLechko/market-summary/internal/model"
	"github.com/RichardLechko/market-summary/internal/stats"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func day(s string) time.Time {
	t, _ := time.ParseInLocation(dateFmt, s, time.Local)
	return t
}

func TestMeta(t *testing.T) {
	s := open(t)
	if v, err := s.Meta("x"); err != nil || v != "" {
		t.Fatalf("unset meta = %q, %v", v, err)
	}
	if err := s.SetMetaMany(map[string]string{"x": "1", "y": "2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("x", "3"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Meta("x"); v != "3" {
		t.Fatalf("meta x = %q", v)
	}
}

func TestObservationsIdempotent(t *testing.T) {
	s := open(t)
	obs := []stats.Obs{{Date: day("2026-11-20"), Value: 1}, {Date: day("2026-11-19"), Value: 2}}
	for range 2 {
		if err := s.UpsertObservations("SP500", obs); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Observations("SP500")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Value != 2 {
		t.Fatalf("observations = %+v", got)
	}
}

func TestFilingFactsPreserved(t *testing.T) {
	s := open(t)
	rev := 100.0
	f := model.Filing{Accession: "a-1", Ticker: "JPM", CIK: 19617, Form: "8-K", Filed: day("2026-10-14"),
		Facts: &model.EarningsFacts{IsEarningsRelease: true, Revenue: &rev, Headline: "h"}}
	if err := s.SaveFiling(f); err != nil {
		t.Fatal(err)
	}
	f.Facts = nil
	if err := s.SaveFiling(f); err != nil {
		t.Fatal(err)
	}
	got, err := s.Filing("a-1")
	if err != nil || got == nil || got.Facts == nil || *got.Facts.Revenue != 100 {
		t.Fatalf("facts not preserved: %+v %v", got, err)
	}
	between, _ := s.FilingsFiledBetween(day("2026-10-13"), day("2026-10-14"))
	if len(between) != 1 {
		t.Fatalf("FilingsFiledBetween = %d", len(between))
	}
}

func TestCache(t *testing.T) {
	s := open(t)
	if _, ok, _ := s.CacheGet("k"); ok {
		t.Fatal("unexpected hit")
	}
	_ = s.CachePut("k", "v")
	if v, ok, _ := s.CacheGet("k"); !ok || v != "v" {
		t.Fatalf("cache = %q %v", v, ok)
	}
}

func TestCallNotes(t *testing.T) {
	s := open(t)
	n := model.CallNote{Accession: "a1", Ticker: "JPM", Quarter: "2026Q3", Period: "Q3 FY2026", Filed: day("2026-10-14"),
		Notes: model.CallNotes{MatchesPeriod: true, Tone: "Calm.", Themes: []model.CallTheme{{Topic: "Loans", Detail: "Up."}}}}
	if err := s.SaveCallNote(n); err != nil {
		t.Fatal(err)
	}
	if has, _ := s.HasCallNote("a1"); !has {
		t.Fatal("HasCallNote false after save")
	}
	got, err := s.UnreportedCallNotes()
	if err != nil || len(got) != 1 || got[0].Notes.Themes[0].Topic != "Loans" || !got[0].Filed.Equal(n.Filed) {
		t.Fatalf("got %+v %v", got, err)
	}
	if err := s.MarkCallNotesReported([]string{"a1"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.UnreportedCallNotes(); len(got) != 0 {
		t.Error("still unreported")
	}
	if err := s.SaveCallNote(n); err != nil { // re-saving keeps the reported flag
		t.Fatal(err)
	}
	if got, _ := s.UnreportedCallNotes(); len(got) != 0 {
		t.Error("re-save reset reported flag")
	}
	if got, _ := s.CallNotesFiledBetween(day("2026-10-13"), day("2026-10-20")); len(got) != 1 {
		t.Errorf("between: got %d", len(got))
	}
}

func TestBackup(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetMeta("k", "v"); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "copy.db")
	if err := s.Backup(dst); err != nil {
		t.Fatal(err)
	}
	c, err := Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if v, _ := c.Meta("k"); v != "v" {
		t.Errorf("backup meta = %q", v)
	}
}
