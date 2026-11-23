package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RichardLechko/market-summary/internal/httpx"
)

// ErrTranscriptLimit means Alpha Vantage refused the request because the
// daily or per-minute quota is used up. Stop asking for this run.
var ErrTranscriptLimit = errors.New("alpha vantage request limit reached")

// Transcripts fetches earnings call transcripts from Alpha Vantage.
type Transcripts struct {
	HTTP    *httpx.Client
	Key     string
	BaseURL string // default https://www.alphavantage.co/query
}

// TranscriptPart is one speaker turn.
type TranscriptPart struct {
	Speaker string `json:"speaker"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Transcript returns the call transcript for ticker and quarter ("2026Q3").
// It returns nil parts, no error, when the transcript is not available.
func (t *Transcripts) Transcript(ctx context.Context, ticker, quarter string) ([]TranscriptPart, error) {
	base := t.BaseURL
	if base == "" {
		base = "https://www.alphavantage.co/query"
	}
	q := url.Values{
		"function": {"EARNINGS_CALL_TRANSCRIPT"},
		"symbol":   {ticker},
		"quarter":  {quarter},
		"apikey":   {t.Key},
	}
	body, err := t.HTTP.Get(ctx, base+"?"+q.Encode())
	if err != nil {
		return nil, redactKey(err, t.Key)
	}
	var resp struct {
		Transcript  []TranscriptPart `json:"transcript"`
		Information string           `json:"Information"`
		Note        string           `json:"Note"`
		Error       string           `json:"Error Message"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("transcript %s %s: %w", ticker, quarter, err)
	}
	if resp.Information != "" || resp.Note != "" {
		return nil, ErrTranscriptLimit
	}
	return resp.Transcript, nil
}

// TranscriptText joins speaker turns into plain text.
func TranscriptText(parts []TranscriptPart) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Speaker)
		if p.Title != "" {
			b.WriteString(" (" + p.Title + ")")
		}
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(p.Content))
		b.WriteString("\n\n")
	}
	return b.String()
}

var fiscalPeriod = regexp.MustCompile(`(?i)\bQ([1-4])\b[^0-9]{0,12}?(\d{4})`)

// TranscriptQuarters returns the quarter labels to try for a release, best
// guess first: the fiscal quarter named in the release's period (for example
// "Q3 FY2027" gives "2027Q3"), then the calendar quarter that ended before
// the filing date, for providers that label by calendar quarter.
func TranscriptQuarters(period string, filed time.Time) []string {
	var out []string
	if m := fiscalPeriod.FindStringSubmatch(period); m != nil {
		out = append(out, m[2]+"Q"+m[1])
	}
	// The last calendar quarter that ended before the filing date.
	y, q := filed.Year(), (int(filed.Month())-1)/3 // q is the previous quarter, 0 means Q4 of last year
	if q == 0 {
		y, q = y-1, 4
	}
	cal := strconv.Itoa(y) + "Q" + strconv.Itoa(q)
	if len(out) == 0 || out[0] != cal {
		out = append(out, cal)
	}
	return out
}
