package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/digest"
	"github.com/RichardLechko/market-summary/internal/store"
)

type fakeMail struct{ subjects, bodies []string }

func (f *fakeMail) Send(subject, html string, _ ...digest.Attachment) error {
	f.subjects = append(f.subjects, subject)
	f.bodies = append(f.bodies, html)
	return nil
}

func TestRecordFailure(t *testing.T) {
	dir := t.TempDir()
	m := &fakeMail{}
	now := time.Date(2026, 11, 30, 9, 0, 0, 0, time.Local)
	recordFailure(dir, m, errors.New("fred: HTTP 500 <bad>"), `D:\logs\run.log`, now)
	if len(m.subjects) != 1 || m.subjects[0] != "Market summary failed" {
		t.Fatalf("alerts = %v", m.subjects)
	}
	if !strings.Contains(m.bodies[0], "fred: HTTP 500 &lt;bad&gt;") || !strings.Contains(m.bodies[0], `D:\logs\run.log`) {
		t.Errorf("body = %s", m.bodies[0])
	}
	b, err := os.ReadFile(filepath.Join(dir, failureFile))
	if err != nil || !strings.Contains(string(b), "fred: HTTP 500") {
		t.Errorf("failure file = %q, %v", b, err)
	}

	// The 9:00 run fails again at a login an hour later: no second alert.
	recordFailure(dir, m, errors.New("again"), "", now.Add(time.Hour))
	if len(m.subjects) != 1 {
		t.Errorf("alert repeated within the gap: %v", m.subjects)
	}
	// Next day: alert again.
	recordFailure(dir, m, errors.New("again"), "", now.Add(24*time.Hour))
	if len(m.subjects) != 2 {
		t.Errorf("no alert the next day: %v", m.subjects)
	}

	clearFailure(dir)
	if _, err := os.Stat(filepath.Join(dir, failureFile)); !os.IsNotExist(err) {
		t.Error("failure record not cleared")
	}
	recordFailure(dir, m, errors.New("new failure"), "", now.Add(25*time.Hour))
	if len(m.subjects) != 3 {
		t.Error("alert throttle not reset by a successful send")
	}
}

func TestBackupKeepsFour(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Root: root}
	st, err := store.Open(filepath.Join(root, "db.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	start := time.Date(2026, 11, 1, 9, 0, 0, 0, time.Local)
	for i := 0; i < 6; i++ {
		if _, err := backup(cfg, st, start.AddDate(0, 0, 7*i)); err != nil {
			t.Fatal(err)
		}
	}
	// A second send on the same day replaces that day's copy.
	if _, err := backup(cfg, st, start.AddDate(0, 0, 35)); err != nil {
		t.Fatal(err)
	}
	got := backups(backupDir(cfg))
	if len(got) != 4 || !strings.HasSuffix(got[0], "2026-11-15.db") || !strings.HasSuffix(got[3], "2026-12-06.db") {
		t.Errorf("backups = %v", got)
	}
}
