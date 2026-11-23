# Market Summary — Design

Date: 2026-11-23
Status: Phases 0–5 implemented 2026-11-23. Changes made during the build are noted inline as "Implementation note".

## Purpose

A personal, internal tool that emails a digest roughly once a week answering "how is the market doing, and why?" It is not a trading tool. It optimizes for understanding: what changed since the last digest, what drove it, and what is coming next.

Earnings arrive in bursts (roughly six weeks per quarter), so the digest is built on a macro layer that has something to say every week. The earnings layer adds detail during reporting season.

## Non-goals

- Trading signals, alerts at filing time, or latency of any kind
- Covering every public company
- A product, web UI, or multi-user support
- Paid data sources in v1

## Constraints

- The repo is public. No secrets, personal holdings, or generated digests are committed. Secrets live in `.env`; personal watchlist additions live in `config/watchlist.local.yaml`; the database and digest output live in `data/`. All three are gitignored.
- Runs locally on a Windows PC that is not always on. Not GitHub Actions: logs on a public repo are public, which would expose the digests.
- Free data sources only: FRED, SEC EDGAR, Federal Reserve RSS, Alpha Vantage (free tier, transcripts).

## Architecture

Go, built as a single binary (`market-summary.exe`). SQLite via `modernc.org/sqlite` (pure Go, no cgo). `golangci-lint` for lint, `go test` for tests.

Key dependencies:

- `github.com/anthropics/anthropic-sdk-go` — Claude API
- `modernc.org/sqlite` — storage
- `github.com/PuerkitoBio/goquery` — HTML parsing of 8-K exhibits
- `github.com/mmcdole/gofeed` — Federal Reserve RSS
- `gopkg.in/yaml.v3` — config
- Standard library for HTTP, JSON, `html/template`, `net/smtp`

Layout:

```
cmd/market-summary/   CLI entry point
internal/collect/     fred, edgar, fed collectors
internal/store/       SQLite schema and typed queries
internal/stats/       period-over-period change, percent change, gap handling
internal/analysis/    macro and earnings fact computation
internal/summarize/   the only package that calls the Claude API
internal/digest/      section assembly, HTML template, SMTP send
internal/schedule/    the run gate (see Scheduling)
config/               series.yaml, watchlist.yaml
```

Pipeline:

```
collect  ->  store  ->  analysis  ->  summarize  ->  digest  ->  email
```

1. **Collectors** fetch raw data and write it to the store. Each is idempotent: re-running for the same period does not duplicate rows.
2. **Store** owns the schema. No other package issues SQL.
3. **Analysis** computes facts deterministically using `internal/stats`: changes since the last digest and year over year, flags for notable moves (thresholds per series in config), per-company comparisons against prior and year-ago quarters.
4. **Summarize** receives computed facts and source text and returns prose. The LLM never computes numbers; it explains numbers the code already computed.
5. **Digest** assembles sections into an HTML email, writes a copy to `data/digests/`, and sends via SMTP.

## Scheduling

The PC may be off at any fixed time, and the digest should go out roughly once a week, never on consecutive logins.

- **Triggers (Windows Task Scheduler):** "At log on" plus a daily trigger at 09:00 (covers a PC left on for days). Condition: start only if a network connection is available.
- **Run gate (in the binary):** on every start, read the date of the last successfully sent digest from SQLite. If fewer than 7 calendar days have passed, exit 0 within a second, without network calls. Otherwise run the full pipeline.
- **Calendar days, not hours,** so a login a few minutes earlier in the day than last week does not skip a week.
- **Only a successful send updates the last-sent date.** A failed run retries at the next trigger.
- **Coverage window:** each digest covers from the last sent digest's cutoff to now. A 10-day gap produces a 10-day digest; nothing is skipped or repeated. The first-ever run covers the previous 7 days.
- The gap threshold (7 days) is a config value.

CLI:

- `market-summary run` — gated run (what Task Scheduler calls)
- `market-summary run --force` — bypass the gate and send now
- `market-summary run --dry-run` — bypass the gate, build the digest, write it to disk, do not send, do not update the last-sent date
- `market-summary status` — print the last-sent date and when the next run becomes eligible

## Data sources

### Macro (FRED API, free key)

| Theme | Series |
|---|---|
| Market | S&P 500 (`SP500`), Nasdaq Composite (`NASDAQCOM`), Dow (`DJIA`), VIX (`VIXCLS`) |
| Rates | Fed funds (`DFF`), 2-year (`DGS2`), 10-year (`DGS10`), 10y-2y spread (`T10Y2Y`) |
| Inflation | CPI (`CPIAUCSL`), core PCE (`PCEPILFE`) |
| Labor | Payrolls (`PAYEMS`), unemployment (`UNRATE`), initial jobless claims (`ICSA`, weekly) |
| Credit and consumer | High-yield spread (`BAMLH0A0HYM2`), consumer sentiment (`UMCSENT`) |
| Commodities | WTI crude (`DCOILWTICO`) |

The series list lives in `config/series.yaml` so it can change without code edits. FRED's release-dates endpoint supplies the "week ahead" calendar.

### Fed (Federal Reserve RSS, free)

FOMC statements and minutes, and the Beige Book (eight times a year). Summarized when a new document falls inside the digest's coverage window.

Implementation note: the Beige Book is not in any Fed RSS feed. It is read from the Beige Book index page (latest report link) with the release date taken from the report's "Last Update" line. The monetary policy feed's discount-rate minutes are excluded by title.

### Earnings (SEC EDGAR, free, User-Agent with contact required)

Bellwether list in `config/watchlist.yaml`, each with a depth tier:

- Banks: JPM, BAC — kick off each season; consumer credit and lending
- Mega-cap tech: AAPL, MSFT, NVDA, AMZN, GOOGL, META
- Consumer: WMT, COST, HD
- Industrial and shipping: CAT, FDX, UPS
- Semis: TSM (foreign filer: 6-K, not 8-K Item 2.02; handled by a separate parser)

Depth tiers:

- `headline` — one line: revenue and EPS vs prior year, guidance raised / held / cut
- `standard` — a paragraph comparing against the prior quarter, including tone and notable segment changes
- `deep` — reserved for transcripts (Phase 5); behaves as `standard` until then

Personal holdings go in the gitignored `config/watchlist.local.yaml` and are merged at load time. The SEC User-Agent contact string comes from `.env`.

The EDGAR rate limit (10 requests/second) is enforced in the client with a minimum interval between requests (120 ms).

Implementation note: XBRL company facts are not used. Quarterly XBRL data arrives with the 10-Q, weeks after the 8-K press release, so it cannot describe a report in the week it happens. Instead the model extracts the current and year-ago figures from the press release's own comparison table (structured output), and code computes growth from them. Standard-depth companies also get their previous earnings release extracted once, so the summary can compare against last quarter.

## Digest format

One email per run, sections in this order. A section with no new data is omitted, not padded.

1. **Summary** — one LLM-written paragraph across all sections below
2. **Market** — index levels and change over the window, VIX
3. **Macro** — releases inside the window, what changed, flagged moves
4. **Fed** — new statements, minutes, Beige Book
5. **Earnings** — bellwether reports inside the window, grouped by sector, at each company's depth tier
6. **Week ahead** — scheduled releases, FOMC meetings, bellwethers expected to report in the next 7 days

The subject line states the coverage window (for example "Market summary: Nov 17 – Nov 25"). Each section ends with a short source line.

Implementation note (2026-11-23): reading the full digest in an email body was rejected. The full summary is now a standalone web page (editorial layout, Newsreader and IBM Plex Sans, light and dark themes, print styles) written to `data/digests/` and opened in the default browser after each run. The email carries only the opening summary, with the page attached as a PDF printed by headless Edge or Chrome. Each series has a one-line plain-language description, the page ends with a glossary of terms, and the prompts explain jargon on first use.

## Claude usage

- Models are set in `config/settings.yaml` (`extraction_model`, `overview_model`, `effort`)
- Prompt caching on the static system prompt
- LLM outputs cached in SQLite keyed by input hash, so dry runs and retries do not pay twice

Implementation note: both default to `claude-opus-5-5` at `medium` effort, with server-side refusal fallback enabled. `extraction_model` can be switched to `claude-haiku-5-5` to cut per-filing cost if extraction quality holds. Earnings extraction uses structured output against a JSON schema.

## Error handling

- Each collector runs independently. A failed collector logs the error and marks its section "unavailable this run"; the digest still sends.
- HTTP calls retry with exponential backoff (3 attempts) on 429 and 5xx responses.
- If the Claude API fails, the digest sends with computed facts and no prose.
- If SMTP fails, the digest stays on disk in `data/digests/`, the last-sent date is not updated, and the exit code is non-zero.
- Logs go to `data/logs/`, one file per run.

## Testing

- Table-driven unit tests for `internal/stats`, `internal/analysis`, and `internal/schedule` (gate decisions across date gaps, failed sends, first run)
- Collector tests against recorded HTTP responses served by `httptest`
- `internal/summarize` tested against a fake client interface; one opt-in live test behind a build tag
- Digest rendering golden-file test
- `run --dry-run` as the manual end-to-end check

## Roadmap

**Phase 0 — Scaffold.** Go module, golangci-lint config, `.env.example`, config files, SQLite schema, CLI skeleton with `run`, `--force`, `--dry-run`, `status`, and the run gate.

**Phase 1 — Macro digest (first useful email).** FRED collector, stats and macro analysis, Market and Macro sections, summary paragraph, SMTP send, Task Scheduler setup script. Built first because the Q3 earnings season is ending; the next starts in mid-January with the bank reports.

**Phase 2 — Fed and week ahead.** Federal Reserve RSS collector, Fed section, FRED release calendar, Week ahead section.

**Phase 3 — Earnings layer.** EDGAR collector (8-K Item 2.02, Exhibit 99.1, XBRL company facts), prior-quarter comparison, depth tiers, Earnings section. Target: running before the January 2027 bank reports.

**Phase 4 — Earnings aggregate.** Across the bellwethers: share reporting revenue growth, share raising vs cutting guidance, growth by sector. Adds "Expected to report" to Week ahead, inferred from each company's Item 2.02 filing date for the same quarter a year earlier, labeled as an estimate.

**Phase 5 — Deep tier (optional).** Earnings call transcripts for `deep` companies, via Whisper on webcast audio or a paid transcript source. Decided after Phases 1–4 have run for a season.

Implementation note (2026-11-23): Phase 5 uses Alpha Vantage's free EARNINGS_CALL_TRANSCRIPT endpoint instead of Whisper or a paid source. For each deep-tier release filed in the last 30 days without call notes, the pipeline asks for the fiscal quarter named in the release (for example "Q3 FY2027" gives 2027Q3), then the calendar quarter before the filing date. The model confirms the transcript covers the release's period before its notes are used, so a year-old call under the same label is rejected. Notes (tone, two to four themes, analyst questions, outlook) are stored per release, shown once under "On the earnings calls", and retried on later runs until the transcript appears. The free tier allows 25 requests a day; on the limit the run stops asking and continues next time. JPM, AAPL, MSFT, NVDA and WMT are deep by default.

**Phase 6 — AWS Lambda (optional).** Run the same binary on Lambda with an EventBridge weekly schedule, SQLite file in S3, secrets in SSM Parameter Store. Only if local runs prove unreliable.
