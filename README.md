# market-summary

A personal tool that emails a market digest about once a week: what the market did, what the economy data said, what the Federal Reserve said, which bellwether companies reported earnings, and what is coming next week.

It runs on a Windows PC that is not always on. It triggers at login and daily at 09:00, and sends only when 7 or more calendar days have passed since the last successful send. Each digest covers everything since the previous one.

## Data sources

All free:

- **FRED** (St. Louis Fed): stock indexes, VIX, Treasury yields, Fed funds rate, CPI, core PCE, payrolls, unemployment, jobless claims, credit spreads, consumer sentiment, oil, and the economic release calendar.
- **Federal Reserve**: FOMC statements, minutes, and projections (RSS), and the Beige Book.
- **FINRA**: short interest for watchlist stocks, twice a month (no account needed), shown as a share of shares outstanding from SEC filings where the company has a single share class.
- **SEC EDGAR**: earnings press releases (8-K Item 2.02, Exhibit 99.1; 6-K for foreign filers); company news on Form 8-K (leadership changes, major agreements, acquisitions, restructurings, write-downs, restatements, auditor changes, delisting notices, charter or bylaw changes, other events), 6-K reports from foreign companies, Schedule 13D activist stakes, and proxy fights (PREC14A, DEFC14A), with routine filings left out and each item read against the company's earlier news; and large insider purchases and sales from Form 4 (purchases of $100K or more; sales of $10M or more, or of a fifth of a holding worth $1M or more; employee stock purchase plan buys, awards, option exercises and gifts left out), flagging three or more insiders buying the same stock within 30 days.
- **Alpha Vantage** (free key): the earnings calendar for "Week ahead" (one request per run; the last copy is reused for two weeks if a request fails), earnings call transcripts for companies set to `depth: deep`, and daily prices for funds FRED lacks (Russell 2000 via IWM, long-term Treasuries via TLT, gold via GLD).

Claude reads each earnings release and returns the reported figures plus a short summary. Growth rates, the season aggregate, and every other number are computed in code.

## Setup

Requires Go 1.26+.

```powershell
Copy-Item .env.example .env
notepad .env
```

Fill in:

| Variable | Where to get it |
|---|---|
| `ANTHROPIC_API_KEY` | console.anthropic.com |
| `FRED_API_KEY` | fredaccount.stlouisfed.org/apikeys (free) |
| `ALPHAVANTAGE_API_KEY` | alphavantage.co/support/#api-key (free, 25 requests a day). Optional: without it, call notes are skipped. |
| `SEC_USER_AGENT` | Your own text with a contact email, e.g. `market-summary you@example.com`. SEC rejects requests without one. |
| `SMTP_USER`, `SMTP_PASSWORD` | A Gmail address and an app password (needs 2-Step Verification; myaccount.google.com/apppasswords) |
| `MAIL_TO` | Where to send the digest |

Personal holdings go in `config/watchlist.local.yaml` (gitignored), same format as `config/watchlist.yaml`:

```yaml
companies:
  - {ticker: COST, sector: Consumer, depth: deep}
```

`depth` is `headline` (one line), `standard` (a paragraph compared with last quarter), or `deep` (standard plus notes from the earnings call: tone, main topics, what analysts asked, outlook).

## Usage

Each run writes a web page to `data\digests\` (also copied to `latest.html`), opens it in your browser, and emails a short note with the same page attached as a PDF.

```powershell
go run ./cmd/market-summary run --dry-run                    # build and open the page, no email
go run ./cmd/market-summary run --dry-run --since 2026-11-16 # preview a past period
go run ./cmd/market-summary run --force                      # send now
go run ./cmd/market-summary run --force --since 2026-11-16   # send now, covering a chosen period
go run ./cmd/market-summary run                              # send only if 7+ days since the last send
go run ./cmd/market-summary open                             # reopen the latest summary
go run ./cmd/market-summary status                           # last send, next due date, failures, backups, service checks
```

The PDF is printed by Edge or Chrome in headless mode; set `browser_path` in `config/settings.yaml` if neither is in the default location. Set `open_in_browser: false` to stop the page opening after each run.

Schedule it:

```powershell
pwsh -File scripts\install-task.ps1
```

If a scheduled or forced run fails, you get a short "Market summary failed" email with the error (at most one a day), and `status` shows it until the next successful send. After each send the database is copied to `data\backups\` (or `backup_dir` in `config/settings.yaml`); the newest 4 copies are kept. To restore one, copy it over `data\market-summary.db`.

Remove the schedule:

```powershell
Unregister-ScheduledTask -TaskName MarketSummary -Confirm:$false
```

## Development

```powershell
go test ./...
golangci-lint run ./...
```

Local state lives in `data\` (gitignored): the SQLite database, rendered digests, and run logs.
