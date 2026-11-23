// Command market-summary builds and emails a periodic market digest.
//
// Usage:
//
//	market-summary run [--force] [--dry-run] [--no-open] [--root DIR]
//	market-summary open [--root DIR]
//	market-summary status [--root DIR]
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/RichardLechko/market-summary/internal/collect"
	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/digest"
	"github.com/RichardLechko/market-summary/internal/httpx"
	"github.com/RichardLechko/market-summary/internal/pipeline"
	"github.com/RichardLechko/market-summary/internal/store"
	"github.com/RichardLechko/market-summary/internal/summarize"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  market-summary run [--force] [--dry-run [--since DATE]] [--no-open] [--root DIR]
  market-summary open [--root DIR]
  market-summary status [--root DIR]

run       build the summary, open it in the browser, and email it with a PDF,
          if 7+ days have passed since the last one
--force   send now regardless of the last send date
--dry-run build the summary without emailing or recording it
--no-open do not open the summary in the browser
--since   with --dry-run, preview the period from a date (YYYY-MM-DD)
open      open the latest summary in the browser
status    show the last send date and when the next summary is due`)
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	root := fs.String("root", ".", "project directory containing config/ and .env")
	force := fs.Bool("force", false, "send now regardless of the last send date")
	dry := fs.Bool("dry-run", false, "build the digest without sending or recording it")
	noOpen := fs.Bool("no-open", false, "do not open the summary in the browser")
	since := fs.String("since", "", "with --dry-run: preview the period starting at this date (YYYY-MM-DD)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cfg, err := config.Load(absRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 1
	}
	if err := os.MkdirAll(cfg.DataDir(), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	st, err := store.Open(filepath.Join(cfg.DataDir(), "market-summary.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		return 1
	}
	defer st.Close()

	p := newPipeline(cfg, st, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	switch cmd {
	case "status":
		return status(p)
	case "open":
		latest := filepath.Join(cfg.DataDir(), "digests", "latest.html")
		if _, err := os.Stat(latest); err != nil {
			fmt.Fprintln(os.Stderr, "no summary yet; run: market-summary run --dry-run")
			return 1
		}
		if err := digest.OpenInBrowser(latest); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "run":
	default:
		usage()
		return 2
	}

	now := time.Now()
	if !*force && !*dry {
		d, err := p.Gate(now)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if !d.Due {
			// Quiet exit: this runs at every login.
			return 0
		}
	}

	logFile, err := openLog(cfg, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer logFile.Close()
	p.Log = slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, logFile), nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	opts := pipeline.Options{Force: *force, DryRun: *dry, Open: cfg.Settings.OpenInBrowser && !*noOpen, Now: now}
	if *since != "" {
		if !*dry {
			fmt.Fprintln(os.Stderr, "--since works only with --dry-run")
			return 2
		}
		if opts.Since, err = config.ParseDate(*since); err != nil {
			fmt.Fprintln(os.Stderr, "--since:", err)
			return 2
		}
	}
	res, err := p.Run(ctx, opts)
	if err != nil {
		p.Log.Error("run failed", "err", err)
		return 1
	}
	switch {
	case res.NotDue:
	case res.Sent:
		fmt.Printf("Sent %q. Page at %s\n", res.Subject, res.Path)
	default:
		fmt.Printf("Dry run: page at %s\n", res.Path)
		if res.PDFPath != "" {
			fmt.Printf("PDF at %s\n", res.PDFPath)
		}
	}
	return 0
}

func newPipeline(cfg *config.Config, st *store.Store, log *slog.Logger) *pipeline.Pipeline {
	sec := cfg.Secrets
	general := httpx.New("market-summary/1.0", 0)
	p := &pipeline.Pipeline{
		Cfg:   cfg,
		Store: st,
		Log:   log,
		FRED:  &collect.FRED{HTTP: general, Key: sec.FREDKey},
		Fed:   &collect.Fed{HTTP: general},
		// SEC allows 10 requests/second; stay under it.
		EDGAR: &collect.EDGAR{HTTP: httpx.New(sec.SECUserAgent, 120*time.Millisecond)},
		Mail: digest.Mail{
			Host: sec.SMTPHost, Port: sec.SMTPPort, User: sec.SMTPUser,
			Password: sec.SMTPPassword, From: sec.MailFrom, To: sec.MailTo,
		},
	}
	if browser, err := digest.FindBrowser(cfg.Settings.BrowserPath); err != nil {
		log.Warn("PDF disabled", "err", err)
	} else {
		p.PrintPDF = func(ctx context.Context, htmlPath, pdfPath string) error {
			return digest.PrintPDF(ctx, browser, htmlPath, pdfPath)
		}
	}
	p.Open = digest.OpenInBrowser
	if sec.AnthropicKey != "" {
		p.Sum = &summarize.Summarizer{
			Caller:          summarize.NewClaude(sec.AnthropicKey),
			Cache:           st,
			ExtractionModel: cfg.Settings.ExtractionModel,
			OverviewModel:   cfg.Settings.OverviewModel,
			Effort:          cfg.Settings.Effort,
		}
	}
	return p
}

func status(p *pipeline.Pipeline) int {
	now := time.Now()
	d, err := p.Gate(now)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	last, _ := p.Store.Meta("last_sent_date")
	start, _ := p.WindowStart(now)
	if last == "" {
		fmt.Println("Last sent:   never")
	} else {
		fmt.Println("Last sent:  ", last)
	}
	if d.Due {
		fmt.Println("Next digest: due now")
	} else {
		fmt.Println("Next digest:", d.NextEligible.Format("Mon 2006-01-02"))
	}
	fmt.Printf("Would cover: %s to now\n", start.Format("Mon 2006-01-02 15:04"))
	return 0
}

func openLog(cfg *config.Config, now time.Time) (*os.File, error) {
	dir := filepath.Join(cfg.DataDir(), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "run-"+now.Format("2006-01-02-150405")+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}
