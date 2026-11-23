# market-summary

A personal tool that emails a market digest about once a week: what the market did, what the economy data said, what the Federal Reserve said, which bellwether companies reported earnings, and what is coming next week.

It runs on a Windows PC that is not always on. It triggers at login and daily at 09:00, and sends only when 7 or more calendar days have passed since the last successful send. Each digest covers everything since the previous one.

## Data sources

All free:

- **FRED** (St. Louis Fed): stock indexes, VIX, Treasury yields, Fed funds rate, CPI, core PCE, payrolls, unemployment, jobless claims, credit spreads, consumer sentiment, oil, and the economic release calendar.
- **Federal Reserve**: FOMC statements, minutes, and projections (RSS), and the Beige Book.
- **SEC EDGAR**: earnings press releases (8-K Item 2.02, Exhibit 99.1; 6-K for foreign filers).

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
| `SEC_USER_AGENT` | Your own text with a contact email, e.g. `market-summary you@example.com`. SEC rejects requests without one. |
| `SMTP_USER`, `SMTP_PASSWORD` | A Gmail address and an app password (needs 2-Step Verification; myaccount.google.com/apppasswords) |
| `MAIL_TO` | Where to send the digest |

Personal holdings go in `config/watchlist.local.yaml` (gitignored), same format as `config/watchlist.yaml`:

```yaml
companies:
  - {ticker: COST, sector: Consumer, depth: standard}
```

## Usage

```powershell
go run ./cmd/market-summary run --dry-run   # build the digest into data\digests\ without sending
go run ./cmd/market-summary run --force     # send now
go run ./cmd/market-summary run             # send only if 7+ days since the last send
go run ./cmd/market-summary status          # last send, next due date
```

Schedule it:

```powershell
pwsh -File scripts\install-task.ps1
```

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
