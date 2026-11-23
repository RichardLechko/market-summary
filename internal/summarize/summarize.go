// Package summarize is the only package that calls the Claude API. It turns
// computed facts and source text into prose, and extracts reported figures
// from earnings releases. It never computes numbers itself.
package summarize

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RichardLechko/market-summary/internal/model"
)

// Request is one model call.
type Request struct {
	Model  string
	Effort string
	System string
	User   string
	Schema json.RawMessage // nil for free text
}

// Caller performs a model call and returns the response text.
type Caller interface {
	Call(ctx context.Context, req Request) (string, error)
}

// Cache stores outputs keyed by a hash of the request.
type Cache interface {
	CacheGet(key string) (string, bool, error)
	CachePut(key, output string) error
}

// Summarizer wraps a Caller with caching and the prompts used by the digest.
type Summarizer struct {
	Caller          Caller
	Cache           Cache
	ExtractionModel string
	OverviewModel   string
	Effort          string
}

func (s *Summarizer) call(ctx context.Context, req Request) (string, error) {
	h := sha256.New()
	for _, part := range []string{req.Model, req.Effort, req.System, string(req.Schema), req.User} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	key := hex.EncodeToString(h.Sum(nil))
	if s.Cache != nil {
		if out, ok, err := s.Cache.CacheGet(key); err == nil && ok {
			return out, nil
		}
	}
	out, err := s.Caller.Call(ctx, req)
	if err != nil {
		return "", err
	}
	if s.Cache != nil {
		_ = s.Cache.CachePut(key, out)
	}
	return out, nil
}

// EarningsInput describes one earnings release to extract.
type EarningsInput struct {
	Ticker       string
	Sector       string
	Form         string
	Filed        string
	Depth        string // headline or standard
	Text         string
	PriorSummary string // notes from the prior quarter's release, if any
}

const extractSystem = `You read company press releases filed with the SEC and extract reported results for a personal market digest.

Rules:
- First decide whether the document reports quarterly or annual financial results. If it does not (for example a board resolution, a dividend notice, or an executive change), set is_earnings_release to false, set every figure to null, and keep text fields short.
- Copy figures exactly as reported for the most recent quarter. Take year-ago figures from the same table's comparison column. Use null for anything the release does not state.
- All figures must use the same unit, which you record in "unit" (for example "USD millions" or "TWD billions"). EPS is per share in the reporting currency.
- Revenue is total revenue; for banks use total net revenue. EPS is GAAP diluted EPS; use null if only an adjusted figure is given.
- Do not compute growth rates or percentages that the release does not state. Code computes comparisons from your figures.
- guidance: "raised", "maintained", or "lowered" only when the release states the direction or compares the new outlook with the previous one. Use "initiated" for a first outlook (including a next-quarter forecast given without a prior one to compare), "updated" when the outlook changed but the release does not say in which direction, "withdrawn", or "none" when the release gives no outlook. guidance_note states the outlook in one short sentence, or is empty.
- headline: one plain sentence, at most 30 words, on what mattered most this quarter.
- summary: when depth is "headline", an empty string. When depth is "standard", one paragraph of 80 to 140 words covering demand, margins, segments, and management tone. When prior quarter notes are provided, say what changed since then.
- Write for a reader who is learning how markets work. Plain language, no hype, no investment advice.`

var earningsSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["is_earnings_release","company","period","unit","revenue","revenue_year_ago","eps_diluted","eps_diluted_year_ago","net_income","net_income_year_ago","guidance","guidance_note","headline","summary"],
  "properties": {
    "is_earnings_release": {"type": "boolean"},
    "company": {"type": "string"},
    "period": {"type": "string", "description": "Reporting period, e.g. Q3 FY2026"},
    "unit": {"type": "string"},
    "revenue": {"anyOf": [{"type": "number"}, {"type": "null"}]},
    "revenue_year_ago": {"anyOf": [{"type": "number"}, {"type": "null"}]},
    "eps_diluted": {"anyOf": [{"type": "number"}, {"type": "null"}]},
    "eps_diluted_year_ago": {"anyOf": [{"type": "number"}, {"type": "null"}]},
    "net_income": {"anyOf": [{"type": "number"}, {"type": "null"}]},
    "net_income_year_ago": {"anyOf": [{"type": "number"}, {"type": "null"}]},
    "guidance": {"type": "string", "enum": ["raised","maintained","lowered","initiated","updated","withdrawn","none"]},
    "guidance_note": {"type": "string"},
    "headline": {"type": "string"},
    "summary": {"type": "string"}
  }
}`)

// ExtractEarnings extracts figures and prose from an earnings release.
func (s *Summarizer) ExtractEarnings(ctx context.Context, in EarningsInput) (model.EarningsFacts, error) {
	depth := in.Depth
	if depth == "deep" {
		depth = "standard"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Ticker: %s\nSector: %s\nForm: %s\nFiled: %s\nDepth: %s\n\n", in.Ticker, in.Sector, in.Form, in.Filed, depth)
	if in.PriorSummary != "" {
		fmt.Fprintf(&b, "<prior_quarter_notes>\n%s\n</prior_quarter_notes>\n\n", in.PriorSummary)
	}
	fmt.Fprintf(&b, "<document>\n%s\n</document>", in.Text)

	out, err := s.call(ctx, Request{
		Model: s.ExtractionModel, Effort: s.Effort, System: extractSystem, User: b.String(), Schema: earningsSchema,
	})
	if err != nil {
		return model.EarningsFacts{}, err
	}
	var facts model.EarningsFacts
	if err := json.Unmarshal([]byte(out), &facts); err != nil {
		return model.EarningsFacts{}, fmt.Errorf("parse extraction for %s: %w", in.Ticker, err)
	}
	return facts, nil
}

const fedSystem = `You summarize Federal Reserve press releases for a personal market digest read by someone learning how markets work.
Write one paragraph of 60 to 120 words: what the document says, what changed from before if the document says so, and why it matters for rates, inflation, jobs, or markets. Plain language. Use only facts in the document. No investment advice.`

// FedSummary summarizes one Federal Reserve document.
func (s *Summarizer) FedSummary(ctx context.Context, d model.FedDoc) (string, error) {
	user := fmt.Sprintf("Title: %s\nPublished: %s\n\n<document>\n%s\n</document>", d.Title, d.Published.Format("2006-01-02"), d.Body)
	return s.call(ctx, Request{Model: s.ExtractionModel, Effort: s.Effort, System: fedSystem, User: user})
}

const overviewSystem = `You write the opening paragraph of a personal market digest. The reader is learning how markets work and wants to know how the market is doing and why.
Use only the facts in the brief. Every number you mention must appear in the brief; do not calculate new ones.
Connect the pieces: what moved, the likely reasons given in the brief, and what to watch next. Prefer cause and effect over lists.
Write 120 to 200 words in one or two paragraphs. No headings, no bullet points, no investment advice.`

// Overview writes the opening summary from a plain-text brief of computed facts.
func (s *Summarizer) Overview(ctx context.Context, brief string) (string, error) {
	return s.call(ctx, Request{Model: s.OverviewModel, Effort: s.Effort, System: overviewSystem, User: "<brief>\n" + brief + "\n</brief>"})
}
