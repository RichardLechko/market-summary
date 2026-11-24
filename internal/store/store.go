// Package store owns the SQLite schema. No other package issues SQL.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/RichardLechko/market-summary/internal/model"
	"github.com/RichardLechko/market-summary/internal/stats"

	_ "modernc.org/sqlite" // driver
)

const dateFmt = "2006-01-02"

const schema = `
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS observations (
	series_id TEXT NOT NULL,
	date      TEXT NOT NULL,
	value     REAL NOT NULL,
	PRIMARY KEY (series_id, date)
);
CREATE TABLE IF NOT EXISTS release_dates (
	release_id INTEGER NOT NULL,
	name       TEXT NOT NULL,
	date       TEXT NOT NULL,
	PRIMARY KEY (release_id, date)
);
CREATE TABLE IF NOT EXISTS fed_docs (
	link      TEXT PRIMARY KEY,
	title     TEXT NOT NULL,
	kind      TEXT NOT NULL,
	published TEXT NOT NULL,
	body      TEXT NOT NULL DEFAULT '',
	summary   TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS filings (
	accession TEXT PRIMARY KEY,
	ticker    TEXT NOT NULL,
	cik       INTEGER NOT NULL,
	form      TEXT NOT NULL,
	filed     TEXT NOT NULL,
	items     TEXT NOT NULL DEFAULT '',
	doc_url   TEXT NOT NULL DEFAULT '',
	facts     TEXT,
	reported  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS filings_ticker_filed ON filings (ticker, filed);
CREATE TABLE IF NOT EXISTS call_notes (
	accession TEXT PRIMARY KEY,
	ticker    TEXT NOT NULL,
	quarter   TEXT NOT NULL,
	period    TEXT NOT NULL DEFAULT '',
	filed     TEXT NOT NULL,
	notes     TEXT NOT NULL,
	reported  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS company_events (
	accession TEXT PRIMARY KEY,
	ticker    TEXT NOT NULL,
	form      TEXT NOT NULL,
	filed     TEXT NOT NULL,
	items     TEXT NOT NULL DEFAULT '',
	doc_url   TEXT NOT NULL DEFAULT '',
	news      TEXT,
	reported  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS insider_filings (
	accession TEXT PRIMARY KEY,
	ticker    TEXT NOT NULL,
	filed     TEXT NOT NULL,
	trades    TEXT NOT NULL,
	reported  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS llm_cache (
	key     TEXT PRIMARY KEY,
	output  TEXT NOT NULL,
	created TEXT NOT NULL
);
`

// Store is a handle to the SQLite database.
type Store struct{ db *sql.DB }

// Open opens (creating if needed) the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Meta returns a meta value, or "" if unset.
func (s *Store) Meta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetMeta sets a meta value.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// SetMetaMany sets several meta values in one transaction.
func (s *Store) SetMetaMany(kv map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for k, v := range kv {
		if _, err := tx.Exec(`INSERT INTO meta (key, value) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// UpsertObservations stores observations for a series.
func (s *Store) UpsertObservations(seriesID string, obs []stats.Obs) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO observations (series_id, date, value) VALUES (?, ?, ?)
		ON CONFLICT (series_id, date) DO UPDATE SET value = excluded.value`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, o := range obs {
		if _, err := stmt.Exec(seriesID, o.Date.Format(dateFmt), o.Value); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Observations returns a series sorted by date.
func (s *Store) Observations(seriesID string) ([]stats.Obs, error) {
	rows, err := s.db.Query(`SELECT date, value FROM observations WHERE series_id = ? ORDER BY date`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []stats.Obs
	for rows.Next() {
		var ds string
		var v float64
		if err := rows.Scan(&ds, &v); err != nil {
			return nil, err
		}
		t, err := time.ParseInLocation(dateFmt, ds, time.Local)
		if err != nil {
			return nil, err
		}
		out = append(out, stats.Obs{Date: t, Value: v})
	}
	return out, rows.Err()
}

// UpsertReleaseDates stores scheduled releases.
func (s *Store) UpsertReleaseDates(rs []model.ReleaseDate) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, r := range rs {
		if _, err := tx.Exec(`INSERT INTO release_dates (release_id, name, date) VALUES (?, ?, ?)
			ON CONFLICT (release_id, date) DO UPDATE SET name = excluded.name`,
			r.ReleaseID, r.Name, r.Date.Format(dateFmt)); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ReleaseDatesBetween returns releases with from < date <= to, sorted.
func (s *Store) ReleaseDatesBetween(from, to time.Time) ([]model.ReleaseDate, error) {
	rows, err := s.db.Query(`SELECT release_id, name, date FROM release_dates
		WHERE date > ? AND date <= ? ORDER BY date, name`, from.Format(dateFmt), to.Format(dateFmt))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ReleaseDate
	for rows.Next() {
		var r model.ReleaseDate
		var ds string
		if err := rows.Scan(&r.ReleaseID, &r.Name, &ds); err != nil {
			return nil, err
		}
		r.Date, _ = time.ParseInLocation(dateFmt, ds, time.Local)
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertFedDoc stores a Fed document, keeping any existing body and summary
// when the new values are empty.
func (s *Store) UpsertFedDoc(d model.FedDoc) error {
	_, err := s.db.Exec(`INSERT INTO fed_docs (link, title, kind, published, body, summary)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (link) DO UPDATE SET
			title = excluded.title,
			kind = excluded.kind,
			published = excluded.published,
			body = CASE WHEN excluded.body <> '' THEN excluded.body ELSE fed_docs.body END,
			summary = CASE WHEN excluded.summary <> '' THEN excluded.summary ELSE fed_docs.summary END`,
		d.Link, d.Title, d.Kind, d.Published.Format(time.RFC3339), d.Body, d.Summary)
	return err
}

// FedDocsBetween returns documents published in (from, to], oldest first.
func (s *Store) FedDocsBetween(from, to time.Time) ([]model.FedDoc, error) {
	rows, err := s.db.Query(`SELECT link, title, kind, published, body, summary FROM fed_docs ORDER BY published`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.FedDoc
	for rows.Next() {
		var d model.FedDoc
		var ps string
		if err := rows.Scan(&d.Link, &d.Title, &d.Kind, &ps, &d.Body, &d.Summary); err != nil {
			return nil, err
		}
		d.Published, _ = time.Parse(time.RFC3339, ps)
		if d.Published.After(from) && !d.Published.After(to) {
			out = append(out, d)
		}
	}
	return out, rows.Err()
}

// SaveFiling inserts or updates a filing. Existing facts are kept when f.Facts is nil.
func (s *Store) SaveFiling(f model.Filing) error {
	var facts any
	if f.Facts != nil {
		b, err := json.Marshal(f.Facts)
		if err != nil {
			return err
		}
		facts = string(b)
	}
	_, err := s.db.Exec(`INSERT INTO filings (accession, ticker, cik, form, filed, items, doc_url, facts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (accession) DO UPDATE SET
			doc_url = CASE WHEN excluded.doc_url <> '' THEN excluded.doc_url ELSE filings.doc_url END,
			facts = COALESCE(excluded.facts, filings.facts)`,
		f.Accession, f.Ticker, f.CIK, f.Form, f.Filed.Format(dateFmt), f.Items, f.DocURL, facts)
	return err
}

// Filing returns one filing by accession number, or nil if absent.
func (s *Store) Filing(accession string) (*model.Filing, error) {
	fs, err := s.queryFilings(`WHERE accession = ?`, accession)
	if err != nil || len(fs) == 0 {
		return nil, err
	}
	return &fs[0], nil
}

// FilingsForTicker returns a ticker's filings, oldest first.
func (s *Store) FilingsForTicker(ticker string) ([]model.Filing, error) {
	return s.queryFilings(`WHERE ticker = ? ORDER BY filed, accession`, ticker)
}

// FilingsFiledBetween returns filings with from < filed <= to, oldest first.
func (s *Store) FilingsFiledBetween(from, to time.Time) ([]model.Filing, error) {
	return s.queryFilings(`WHERE filed > ? AND filed <= ? ORDER BY filed, ticker`,
		from.Format(dateFmt), to.Format(dateFmt))
}

// UnreportedFilings returns filings not yet included in a sent digest, oldest first.
func (s *Store) UnreportedFilings() ([]model.Filing, error) {
	return s.queryFilings(`WHERE reported = 0 ORDER BY filed, ticker`)
}

// MarkFilingsReported flags filings as included in a sent digest.
func (s *Store) MarkFilingsReported(accessions []string) error {
	return s.markReported("filings", accessions)
}

// markReported sets reported = 1 on the given rows of table.
func (s *Store) markReported(table string, accessions []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, a := range accessions {
		if _, err := tx.Exec(`UPDATE `+table+` SET reported = 1 WHERE accession = ?`, a); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) queryFilings(where string, args ...any) ([]model.Filing, error) {
	rows, err := s.db.Query(`SELECT accession, ticker, cik, form, filed, items, doc_url, facts, reported FROM filings `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Filing
	for rows.Next() {
		var f model.Filing
		var filed string
		var facts sql.NullString
		if err := rows.Scan(&f.Accession, &f.Ticker, &f.CIK, &f.Form, &filed, &f.Items, &f.DocURL, &facts, &f.Reported); err != nil {
			return nil, err
		}
		f.Filed, _ = time.ParseInLocation(dateFmt, filed, time.Local)
		if facts.Valid {
			var ef model.EarningsFacts
			if err := json.Unmarshal([]byte(facts.String), &ef); err != nil {
				return nil, fmt.Errorf("filing %s facts: %w", f.Accession, err)
			}
			f.Facts = &ef
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CacheGet returns a cached LLM output.
func (s *Store) CacheGet(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT output FROM llm_cache WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// CachePut stores an LLM output.
func (s *Store) CachePut(key, output string) error {
	_, err := s.db.Exec(`INSERT INTO llm_cache (key, output, created) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET output = excluded.output`,
		key, output, time.Now().Format(time.RFC3339))
	return err
}

// SaveCallNote stores the summary of an earnings call, keeping its reported flag.
func (s *Store) SaveCallNote(c model.CallNote) error {
	b, err := json.Marshal(c.Notes)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO call_notes (accession, ticker, quarter, period, filed, notes)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (accession) DO UPDATE SET quarter = excluded.quarter, period = excluded.period, notes = excluded.notes`,
		c.Accession, c.Ticker, c.Quarter, c.Period, c.Filed.Format(dateFmt), string(b))
	return err
}

// HasCallNote reports whether a call summary exists for a release.
func (s *Store) HasCallNote(accession string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM call_notes WHERE accession = ?`, accession).Scan(&n)
	return n > 0, err
}

// UnreportedCallNotes returns call summaries not yet included in a sent digest, oldest first.
func (s *Store) UnreportedCallNotes() ([]model.CallNote, error) {
	return s.queryCallNotes(`WHERE reported = 0 ORDER BY filed, ticker`)
}

// CallNotesFiledBetween returns call summaries for releases with from < filed <= to, oldest first.
func (s *Store) CallNotesFiledBetween(from, to time.Time) ([]model.CallNote, error) {
	return s.queryCallNotes(`WHERE filed > ? AND filed <= ? ORDER BY filed, ticker`,
		from.Format(dateFmt), to.Format(dateFmt))
}

// MarkCallNotesReported flags call summaries as included in a sent digest.
func (s *Store) MarkCallNotesReported(accessions []string) error {
	return s.markReported("call_notes", accessions)
}

func (s *Store) queryCallNotes(where string, args ...any) ([]model.CallNote, error) {
	rows, err := s.db.Query(`SELECT accession, ticker, quarter, period, filed, notes, reported FROM call_notes `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.CallNote
	for rows.Next() {
		var c model.CallNote
		var filed, notes string
		if err := rows.Scan(&c.Accession, &c.Ticker, &c.Quarter, &c.Period, &filed, &notes, &c.Reported); err != nil {
			return nil, err
		}
		c.Filed, _ = time.ParseInLocation(dateFmt, filed, time.Local)
		if err := json.Unmarshal([]byte(notes), &c.Notes); err != nil {
			return nil, fmt.Errorf("call note %s: %w", c.Accession, err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveEvent stores a company event filing, keeping its reported flag.
func (s *Store) SaveEvent(e model.CompanyEvent) error {
	var news sql.NullString
	if e.News != nil {
		b, err := json.Marshal(e.News)
		if err != nil {
			return err
		}
		news = sql.NullString{String: string(b), Valid: true}
	}
	_, err := s.db.Exec(`INSERT INTO company_events (accession, ticker, form, filed, items, doc_url, news)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (accession) DO UPDATE SET items = excluded.items, doc_url = excluded.doc_url, news = excluded.news`,
		e.Accession, e.Ticker, e.Form, e.Filed.Format(dateFmt), e.Items, e.DocURL, news)
	return err
}

// Event returns one stored event filing, or nil if absent.
func (s *Store) Event(accession string) (*model.CompanyEvent, error) {
	es, err := s.queryEvents(`WHERE accession = ?`, accession)
	if err != nil || len(es) == 0 {
		return nil, err
	}
	return &es[0], nil
}

// UnreportedEvents returns event filings not yet included in a sent digest, oldest first.
func (s *Store) UnreportedEvents() ([]model.CompanyEvent, error) {
	return s.queryEvents(`WHERE reported = 0 ORDER BY filed, ticker`)
}

// EventsFiledBetween returns event filings with from < filed <= to, oldest first.
func (s *Store) EventsFiledBetween(from, to time.Time) ([]model.CompanyEvent, error) {
	return s.queryEvents(`WHERE filed > ? AND filed <= ? ORDER BY filed, ticker`,
		from.Format(dateFmt), to.Format(dateFmt))
}

// MarkEventsReported flags event filings as included in a sent digest.
func (s *Store) MarkEventsReported(accessions []string) error {
	return s.markReported("company_events", accessions)
}

func (s *Store) queryEvents(where string, args ...any) ([]model.CompanyEvent, error) {
	rows, err := s.db.Query(`SELECT accession, ticker, form, filed, items, doc_url, news, reported FROM company_events `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.CompanyEvent
	for rows.Next() {
		var e model.CompanyEvent
		var filed string
		var news sql.NullString
		if err := rows.Scan(&e.Accession, &e.Ticker, &e.Form, &filed, &e.Items, &e.DocURL, &news, &e.Reported); err != nil {
			return nil, err
		}
		e.Filed, _ = time.ParseInLocation(dateFmt, filed, time.Local)
		if news.Valid {
			var n model.EventNews
			if err := json.Unmarshal([]byte(news.String), &n); err != nil {
				return nil, fmt.Errorf("event %s: %w", e.Accession, err)
			}
			e.News = &n
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EventsForTicker returns a company's stored event filings, oldest first.
func (s *Store) EventsForTicker(ticker string) ([]model.CompanyEvent, error) {
	return s.queryEvents(`WHERE ticker = ? ORDER BY filed, accession`, ticker)
}

// SaveInsiderFiling stores a parsed Form 4, keeping its reported flag.
func (s *Store) SaveInsiderFiling(f model.InsiderFiling) error {
	b, err := json.Marshal(f.Trades)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO insider_filings (accession, ticker, filed, trades) VALUES (?, ?, ?, ?)
		ON CONFLICT (accession) DO UPDATE SET trades = excluded.trades`,
		f.Accession, f.Ticker, f.Filed.Format(dateFmt), string(b))
	return err
}

// HasInsiderFiling reports whether a Form 4 is stored.
func (s *Store) HasInsiderFiling(accession string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM insider_filings WHERE accession = ?`, accession).Scan(&n)
	return n > 0, err
}

// UnreportedInsiderFilings returns Form 4s not yet included in a sent digest, oldest first.
func (s *Store) UnreportedInsiderFilings() ([]model.InsiderFiling, error) {
	return s.queryInsider(`WHERE reported = 0 ORDER BY filed, accession`)
}

// InsiderFilingsBetween returns Form 4s with from < filed <= to, oldest first.
func (s *Store) InsiderFilingsBetween(from, to time.Time) ([]model.InsiderFiling, error) {
	return s.queryInsider(`WHERE filed > ? AND filed <= ? ORDER BY filed, accession`,
		from.Format(dateFmt), to.Format(dateFmt))
}

// MarkInsiderReported flags Form 4s as included in a sent digest.
func (s *Store) MarkInsiderReported(accessions []string) error {
	return s.markReported("insider_filings", accessions)
}

func (s *Store) queryInsider(where string, args ...any) ([]model.InsiderFiling, error) {
	rows, err := s.db.Query(`SELECT accession, ticker, filed, trades, reported FROM insider_filings `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.InsiderFiling
	for rows.Next() {
		var f model.InsiderFiling
		var filed, trades string
		if err := rows.Scan(&f.Accession, &f.Ticker, &filed, &trades, &f.Reported); err != nil {
			return nil, err
		}
		f.Filed, _ = time.ParseInLocation(dateFmt, filed, time.Local)
		if err := json.Unmarshal([]byte(trades), &f.Trades); err != nil {
			return nil, fmt.Errorf("insider filing %s: %w", f.Accession, err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Backup writes a consistent copy of the database to path, which must not exist.
func (s *Store) Backup(path string) error {
	_, err := s.db.Exec(`VACUUM INTO ?`, path)
	return err
}
