package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/digest"
	"github.com/RichardLechko/market-summary/internal/pipeline"
	"github.com/RichardLechko/market-summary/internal/store"
	"github.com/RichardLechko/market-summary/internal/summarize"
)

const (
	failureFile = "last-failure.txt"
	alertFile   = "last-alert.txt"
	// alertGap keeps a failure that repeats at every login or daily run
	// from sending more than one alert a day.
	alertGap = 20 * time.Hour
	// backupsKept is how many weekly database copies are kept.
	backupsKept  = 4
	avDailyLimit = 25
)

// recordFailure writes the failure where status can show it and, when email
// is configured, sends a short alert (at most one per alertGap). It never
// fails itself; problems go to stderr.
func recordFailure(dataDir string, mail pipeline.Sender, runErr error, logPath string, now time.Time) {
	msg := fmt.Sprintf("%s\n%v\n", now.Format(time.RFC3339), runErr)
	if logPath != "" {
		msg += "Log: " + logPath + "\n"
	}
	if err := os.MkdirAll(dataDir, 0o755); err == nil {
		_ = os.WriteFile(filepath.Join(dataDir, failureFile), []byte(msg), 0o644)
	}
	if mail == nil {
		return
	}
	alertPath := filepath.Join(dataDir, alertFile)
	if b, err := os.ReadFile(alertPath); err == nil {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b))); err == nil && now.Sub(t) < alertGap {
			return
		}
	}
	body := fmt.Sprintf(`<p style="font-family:Georgia,serif;font-size:16px;line-height:1.6;">The market summary could not be built on %s.</p>
<p style="font-family:Georgia,serif;font-size:16px;line-height:1.6;"><b>Error:</b> %s</p>
<p style="font-family:Georgia,serif;font-size:16px;line-height:1.6;">It will try again at the next scheduled run. To see details, run <code>market-summary status</code>%s.</p>`,
		now.Format("Monday, January 2 at 15:04"), html.EscapeString(runErr.Error()), logHint(logPath))
	if err := mail.Send("Market summary failed", body); err != nil {
		fmt.Fprintln(os.Stderr, "failure alert not sent:", err)
		return
	}
	_ = os.WriteFile(alertPath, []byte(now.Format(time.RFC3339)), 0o644)
}

func logHint(logPath string) string {
	if logPath == "" {
		return ""
	}
	return " or open the log at " + html.EscapeString(logPath)
}

// clearFailure removes the failure record after a successful send.
func clearFailure(dataDir string) {
	_ = os.Remove(filepath.Join(dataDir, failureFile))
	_ = os.Remove(filepath.Join(dataDir, alertFile))
}

func backupDir(cfg *config.Config) string {
	if d := cfg.Settings.BackupDir; d != "" {
		if filepath.IsAbs(d) {
			return d
		}
		return filepath.Join(cfg.Root, d)
	}
	return filepath.Join(cfg.DataDir(), "backups")
}

// backup copies the database into the backup folder and keeps the newest
// backupsKept copies.
func backup(cfg *config.Config, st *store.Store, now time.Time) (string, error) {
	dir := backupDir(cfg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "market-summary-"+now.Format("2006-01-02")+".db")
	_ = os.Remove(path) // a second send on the same day replaces the copy
	if err := st.Backup(path); err != nil {
		return "", err
	}
	old := backups(dir)
	for len(old) > backupsKept {
		_ = os.Remove(old[0])
		old = old[1:]
	}
	return path, nil
}

// backups lists backup files oldest first.
func backups(dir string) []string {
	m, _ := filepath.Glob(filepath.Join(dir, "market-summary-*.db"))
	slices.Sort(m) // names carry the date
	return m
}

// countAV records an Alpha Vantage request against today's count.
func countAV(st *store.Store) func() {
	return func() {
		key := "av_requests:" + time.Now().Format("2006-01-02")
		v, _ := st.Meta(key)
		n, _ := strconv.Atoi(v)
		_ = st.SetMeta(key, strconv.Itoa(n+1))
	}
}

func status(cfg *config.Config, p *pipeline.Pipeline) int {
	now := time.Now()
	d, err := p.Gate(now)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	row := func(label, value string) { fmt.Printf("%-16s %s\n", label+":", value) }

	if last, _ := p.Store.Meta("last_sent_date"); last == "" {
		row("Last sent", "never")
	} else {
		row("Last sent", last)
	}
	if d.Due {
		row("Next summary", "due now; sent at the next scheduled run")
	} else {
		row("Next summary", "on or after "+d.NextEligible.Format("Mon 2006-01-02"))
	}
	if start, err := p.WindowStart(now); err == nil {
		row("Would cover", start.Format("Mon 2006-01-02 15:04")+" to then")
	}
	if b, err := os.ReadFile(filepath.Join(cfg.DataDir(), failureFile)); err == nil {
		row("Last failure", strings.ReplaceAll(strings.TrimSpace(string(b)), "\n", "\n"+strings.Repeat(" ", 17)))
	} else {
		row("Last failure", "none since the last send")
	}
	if bs := backups(backupDir(cfg)); len(bs) > 0 {
		row("Last backup", fmt.Sprintf("%s (%d kept)", bs[len(bs)-1], len(bs)))
	} else {
		row("Last backup", "none yet; made after each send")
	}
	n, _ := p.Store.Meta("av_requests:" + now.Format("2006-01-02"))
	if n == "" {
		n = "0"
	}
	row("Alpha Vantage", fmt.Sprintf("%s of %d requests used today (as counted by this program)", n, avDailyLimit))

	fmt.Println("\nChecks:")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	check := func(label string, err error) {
		v := "ok"
		if err != nil {
			v = "FAILED: " + err.Error()
		}
		fmt.Printf("  %-15s %s\n", label, v)
	}
	sec := cfg.Secrets
	if sec.FREDKey == "" {
		check("FRED", errors.New("FRED_API_KEY is not set"))
	} else {
		_, err := p.FRED.Observations(ctx, "DGS10", now.AddDate(0, 0, -10))
		check("FRED", err)
	}
	if sec.SECUserAgent == "" {
		check("SEC EDGAR", errors.New("SEC_USER_AGENT is not set"))
	} else {
		_, err := p.EDGAR.Submissions(ctx, 320193, now.AddDate(0, 0, -7))
		check("SEC EDGAR", err)
	}
	if sec.AnthropicKey == "" {
		check("Anthropic", errors.New("ANTHROPIC_API_KEY is not set; summaries are skipped"))
	} else {
		check("Anthropic", summarize.NewClaude(sec.AnthropicKey).Check(ctx))
	}
	if mail, ok := p.Mail.(digest.Mail); ok {
		check("Email", mail.Check())
	}
	if sec.AlphaVantageKey == "" {
		check("Alpha Vantage", errors.New("ALPHAVANTAGE_API_KEY is not set; call notes, fund prices and the earnings calendar are skipped"))
	} else {
		fmt.Printf("  %-15s key set (not tested, to save the daily requests)\n", "Alpha Vantage")
	}
	if runtime.GOOS == "windows" {
		if next, err := scheduledTask(); err != nil {
			check("Scheduled task", err)
		} else {
			fmt.Printf("  %-15s ok, next run %s\n", "Scheduled task", next)
		}
	}
	return 0
}

// scheduledTask confirms the MarketSummary task exists and returns its next
// run time as Task Scheduler prints it.
func scheduledTask() (string, error) {
	out, err := exec.Command("schtasks", "/query", "/tn", "MarketSummary", "/fo", "list").CombinedOutput()
	if err != nil {
		return "", errors.New("task MarketSummary not found; run install-task.ps1")
	}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "Next Run Time" {
			return strings.TrimSpace(v), nil
		}
	}
	return "unknown", nil
}
