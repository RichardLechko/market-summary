// Package collect fetches raw data from FRED, the Federal Reserve, and SEC EDGAR.
package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/RichardLechko/market-summary/internal/httpx"
	"github.com/RichardLechko/market-summary/internal/model"
	"github.com/RichardLechko/market-summary/internal/stats"
)

// FRED is a client for the St. Louis Fed FRED API.
type FRED struct {
	HTTP    *httpx.Client
	Key     string
	BaseURL string // default https://api.stlouisfed.org/fred
}

func (f *FRED) base() string {
	if f.BaseURL != "" {
		return f.BaseURL
	}
	return "https://api.stlouisfed.org/fred"
}

// Observations returns observations for a series starting at start. Missing
// values (FRED's ".") are skipped.
func (f *FRED) Observations(ctx context.Context, seriesID string, start time.Time) ([]stats.Obs, error) {
	q := url.Values{
		"series_id":         {seriesID},
		"api_key":           {f.Key},
		"file_type":         {"json"},
		"observation_start": {start.Format("2006-01-02")},
	}
	body, err := f.HTTP.Get(ctx, f.base()+"/series/observations?"+q.Encode())
	if err != nil {
		return nil, redactKey(err, f.Key)
	}
	var resp struct {
		Observations []struct {
			Date  string `json:"date"`
			Value string `json:"value"`
		} `json:"observations"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("fred %s: %w", seriesID, err)
	}
	out := make([]stats.Obs, 0, len(resp.Observations))
	for _, o := range resp.Observations {
		v, err := strconv.ParseFloat(o.Value, 64)
		if err != nil {
			continue // "." means no value
		}
		d, err := time.ParseInLocation("2006-01-02", o.Date, time.Local)
		if err != nil {
			continue
		}
		out = append(out, stats.Obs{Date: d, Value: v})
	}
	return out, nil
}

// ReleaseDates returns scheduled release dates in [from, to] whose release
// name equals one of names (case-insensitive). An empty names list keeps all.
func (f *FRED) ReleaseDates(ctx context.Context, from, to time.Time, names []string) ([]model.ReleaseDate, error) {
	q := url.Values{
		"api_key":                            {f.Key},
		"file_type":                          {"json"},
		"realtime_start":                     {from.Format("2006-01-02")},
		"realtime_end":                       {to.Format("2006-01-02")},
		"include_release_dates_with_no_data": {"true"},
		"limit":                              {"1000"},
		"sort_order":                         {"asc"},
	}
	body, err := f.HTTP.Get(ctx, f.base()+"/releases/dates?"+q.Encode())
	if err != nil {
		return nil, redactKey(err, f.Key)
	}
	var resp struct {
		ReleaseDates []struct {
			ReleaseID   int    `json:"release_id"`
			ReleaseName string `json:"release_name"`
			Date        string `json:"date"`
		} `json:"release_dates"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("fred release dates: %w", err)
	}
	var out []model.ReleaseDate
	for _, r := range resp.ReleaseDates {
		d, err := time.ParseInLocation("2006-01-02", r.Date, time.Local)
		if err != nil || d.Before(from) || d.After(to) {
			continue
		}
		if !matchesAny(r.ReleaseName, names) {
			continue
		}
		out = append(out, model.ReleaseDate{ReleaseID: r.ReleaseID, Name: r.ReleaseName, Date: d})
	}
	return out, nil
}

func matchesAny(name string, names []string) bool {
	if len(names) == 0 {
		return true
	}
	for _, n := range names {
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(n)) {
			return true
		}
	}
	return false
}

// redactKey keeps the API key out of logs and error messages.
func redactKey(err error, key string) error {
	if key == "" {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), key, "REDACTED"))
}
