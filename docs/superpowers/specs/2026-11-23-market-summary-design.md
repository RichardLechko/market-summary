# Market Summary — Design

Date: 2026-11-23
Status: Draft, pending review

## Purpose

A personal, internal tool that emails a weekly digest answering "how is the market doing, and why?" It is not a trading tool. It optimizes for understanding: what changed this week, what drove it, and what is coming next week.

Earnings arrive in bursts (roughly six weeks per quarter), so the digest is built on a macro layer that has something to say every week. The earnings layer adds detail during reporting season.

## Non-goals

- Trading signals, alerts at filing time, or latency of any kind
- Covering every public company
- A product, web UI, or multi-user support
- Paid data sources in v1

## Constraints

- The repo is public. No secrets, personal holdings, or generated digests are committed. Secrets live in `.env`; personal watchlist additions live in `config/watchlist.local.yaml`; the database and digest output live in `data/`. All three are gitignored.
- Runs locally on Windows via Task Scheduler. Not GitHub Actions: logs on a public repo are public, which would expose the digests.
- Free data sources only: FRED, SEC EDGAR, Federal Reserve RSS.

## Architecture

Python 3.13+, managed with `uv`. Ruff for lint and format, pytest for tests. SQLite for storage (a single file, no server to run).

The pipeline has four stages, each a separate module with a narrow interface:

```
collectors/  ->  store (SQLite)  ->  analysis/  ->  digest/  ->  email
```

1. **Collectors** fetch raw data and write it to the store. Each collector is idempotent: re-running for the same period does not duplicate rows.
   - `fred.py` — macro series and upcoming release dates
   - `edgar.py` — 8-K Item 2.02 filings, Exhibit 99.1 text, XBRL company facts
   - `fed.py` — FOMC statements, minutes, Beige Book from Federal Reserve RSS feeds
2. **Store** (`store.py`) — SQLite schema plus typed read/write helpers. No other module issues SQL.
3. **Analysis** — deterministic computation first, LLM second.
   - `macro.py` — week-over-week and year-over-year changes, flags for notable moves (thresholds per series in config)
   - `earnings.py` — per-company facts vs prior quarter and year-ago quarter (revenue, EPS, margins, guidance language)
   - `summarize.py` — the only module that calls the Claude API. It receives computed facts and source text and returns prose. The LLM never computes numbers; it explains numbers the code already computed.
4. **Digest** (`digest/`) — assembles sections into an HTML email from a Jinja2 template, writes a copy to `data/digests/`, sends via SMTP.

Entry point: `market-summary run --week YYYY-MM-DD` plus `--dry-run` (build the digest, write it to disk, skip sending). Task Scheduler runs `market-summary run` Saturday at 08:00.

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

Series list lives in `config/series.yaml` so it can change without code edits. FRED's release-dates endpoint supplies the "week ahead" calendar (CPI day, jobs report day, and so on).

### Fed (Federal Reserve RSS, free)

FOMC statements and minutes, and the Beige Book (eight times a year, a qualitative read on each regional economy). Summarized when a new document appears in the digest week.

### Earnings (SEC EDGAR, free, User-Agent with contact required)

Bellwether list in `config/watchlist.yaml`, each with a depth tier:

- Banks: JPM, BAC — kick off each season; consumer credit and lending
- Mega-cap tech: AAPL, MSFT, NVDA, AMZN, GOOGL, META
- Consumer: WMT, COST, HD
- Industrial and shipping: CAT, FDX, UPS
- Semis: TSM (files 6-K, not 8-K; monthly revenue)

Depth tiers:

- `headline` — one line: revenue and EPS vs prior year, guidance raised / held / cut
- `standard` — a paragraph comparing against the prior quarter, including tone and notable segment changes
- `deep` — reserved for transcripts (Phase 5); behaves as `standard` until then

Personal holdings go in the gitignored `config/watchlist.local.yaml` and are merged at load time.

EDGAR rate limit (10 requests/second) is enforced in the client with a token bucket.

## Digest format

One email per week, sections in this order. A section with no new data is omitted, not padded.

1. **The week in one paragraph** — LLM synthesis across all sections below
2. **Market** — index levels and weekly change, VIX
3. **Macro** — releases that came out this week, what changed, flagged moves
4. **Fed** — new statements, minutes, Beige Book
5. **Earnings** — bellwether reports this week, grouped by sector, at each company's depth tier
6. **Week ahead** — scheduled releases, FOMC meetings, and bellwethers expected to report

Every number in the email is traceable to a stored row; the email includes a small source line per section.

## Claude usage

- Per-filing summaries: a smaller, cheaper model (Haiku class)
- Weekly synthesis paragraph: a stronger model (Sonnet class)
- Prompt caching on the static system prompt
- LLM outputs are cached in SQLite keyed by input hash, so re-runs and dry runs do not pay twice

Exact model IDs and parameters are fixed in the implementation plan.

## Error handling

- Each collector runs independently. A failed collector logs the error and marks its section "unavailable this week" in the digest; the digest still sends.
- HTTP calls retry with exponential backoff (3 attempts) on 429 and 5xx responses.
- If the Claude API fails, the digest sends with computed facts and no prose.
- If SMTP fails, the digest is still on disk in `data/digests/` and the exit code is non-zero so Task Scheduler records the failure.

## Testing

- Unit tests for analysis modules on fixture data (no network)
- Collector tests against recorded HTTP responses (saved JSON/HTML fixtures)
- `summarize.py` tested with a fake client; one opt-in live test marked `@pytest.mark.live`
- Digest rendering snapshot test
- `--dry-run` serves as the manual end-to-end check

## Roadmap

**Phase 0 — Scaffold.** uv project, Ruff, pytest, `.env.example`, `.gitignore`, config files, SQLite schema, CLI skeleton with `--dry-run`.

**Phase 1 — Macro digest (first useful email).** FRED collector, macro analysis, Market and Macro sections, weekly synthesis paragraph, SMTP send, Task Scheduler setup. Built first because the Q3 earnings season is ending and the next one starts in mid-January with the bank reports.

**Phase 2 — Fed and week ahead.** Federal Reserve RSS collector, Fed section, FRED release calendar, Week Ahead section.

**Phase 3 — Earnings layer.** EDGAR collector (8-K Item 2.02, Exhibit 99.1, XBRL company facts), prior-quarter comparison, depth tiers, Earnings section. Target: running before the January 2027 bank reports.

**Phase 4 — Earnings aggregate.** Across the bellwethers: share reporting revenue growth, share raising vs cutting guidance, growth by sector. Adds "Expected to report" to Week Ahead, inferred from each company's Item 2.02 filing date for the same quarter a year earlier (companies report in nearly the same week each year), labeled as an estimate.

**Phase 5 — Deep tier (optional).** Earnings call transcripts for `deep` companies, via Whisper on webcast audio or a paid transcript source. Decided after Phases 1–4 have run for a season.
