// Package config loads settings, series, watchlist, and calendar files plus
// secrets from the environment (optionally via a .env file).
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Series describes one FRED series and how to present it.
//
// Kinds:
//   - price:          daily level; change is percent vs. the start of the digest window
//   - rate:           daily percent; change is basis points vs. the start of the window
//   - index_yoy:      monthly index shown as year-over-year percent; change in percentage points vs. prior month
//   - monthly_change: monthly level shown as change from prior month (e.g. payrolls)
//   - level:          shown as-is; change vs. prior observation, as Delta ("pct" or "diff")
type Series struct {
	ID        string  `yaml:"id"`
	Name      string  `yaml:"name"`
	Group     string  `yaml:"group"`
	Kind      string  `yaml:"kind"`
	Delta     string  `yaml:"delta"`
	Unit      string  `yaml:"unit"`
	About     string  `yaml:"about"`
	Threshold float64 `yaml:"flag_threshold"`
	Source    string  `yaml:"source"` // "" or fred (default), alphavantage (fund and stock prices)
	// Fund marks a price series you can buy a fund for; its row shows what
	// example_investment would be worth after the move.
	Fund bool `yaml:"fund"`
}

// Company is one watchlist entry.
type Company struct {
	Ticker string   `yaml:"ticker"`
	Sector string   `yaml:"sector"`
	Depth  string   `yaml:"depth"`
	Forms  []string `yaml:"forms"`
}

// Meeting is a scheduled FOMC meeting.
type Meeting struct {
	Start string `yaml:"start"`
	End   string `yaml:"end"`
}

// Feed is a Federal Reserve RSS feed. Items whose title contains any phrase
// in Exclude (case-insensitive) are skipped.
type Feed struct {
	URL     string   `yaml:"url"`
	Exclude []string `yaml:"exclude"`
}

// Settings holds tunable behavior.
type Settings struct {
	MinGapDays      int      `yaml:"min_gap_days"`
	FirstRunDays    int      `yaml:"first_run_days"`
	SeasonDays      int      `yaml:"season_days"`
	ExtractionModel string   `yaml:"extraction_model"`
	OverviewModel   string   `yaml:"overview_model"`
	Effort          string   `yaml:"effort"`
	FedFeeds        []Feed   `yaml:"fed_feeds"`
	BeigeBookIndex  string   `yaml:"beige_book_index"`
	OpenInBrowser   bool     `yaml:"open_in_browser"`
	BrowserPath     string   `yaml:"browser_path"`
	Releases        []string `yaml:"releases"`
	// BackupDir holds weekly database copies; empty means data/backups.
	BackupDir string `yaml:"backup_dir"`
	// ExampleInvestment is the dollar amount used to translate fund moves
	// into money; 0 means 10,000.
	ExampleInvestment float64 `yaml:"example_investment"`
	Podcast           Podcast `yaml:"podcast"`
}

// Podcast configures the two-host audio version of the summary.
type Podcast struct {
	Model        string  `yaml:"model"`         // writes the script; empty means overview_model
	Minutes      int     `yaml:"minutes"`       // target length
	SpeakingRate float64 `yaml:"speaking_rate"` // 1.0 is normal speed
	Hosts        []Host  `yaml:"hosts"`         // exactly two
}

// Host is one podcast voice.
type Host struct {
	Name  string `yaml:"name"`
	Voice string `yaml:"voice"` // Google Cloud voice name
}

// Secrets come from the environment.
type Secrets struct {
	AnthropicKey    string
	FREDKey         string
	AlphaVantageKey string
	GoogleTTSKey    string
	SECUserAgent    string
	SMTPHost        string
	SMTPPort        string
	SMTPUser        string
	SMTPPassword    string
	MailFrom        string
	MailTo          []string
}

// Config is everything the pipeline needs.
type Config struct {
	Root      string
	Settings  Settings
	Series    []Series
	Watchlist []Company
	FOMC      []Meeting
	Secrets   Secrets
}

// DataDir returns the gitignored local data directory.
func (c *Config) DataDir() string { return filepath.Join(c.Root, "data") }

// Load reads config files under root/config and secrets from root/.env and the
// process environment (process environment wins).
func Load(root string) (*Config, error) {
	if err := loadDotEnv(filepath.Join(root, ".env")); err != nil {
		return nil, err
	}
	cfg := &Config{Root: root}
	dir := filepath.Join(root, "config")

	if err := readYAML(filepath.Join(dir, "settings.yaml"), &cfg.Settings); err != nil {
		return nil, err
	}
	var series struct {
		Series []Series `yaml:"series"`
	}
	if err := readYAML(filepath.Join(dir, "series.yaml"), &series); err != nil {
		return nil, err
	}
	cfg.Series = series.Series

	var wl struct {
		Companies []Company `yaml:"companies"`
	}
	if err := readYAML(filepath.Join(dir, "watchlist.yaml"), &wl); err != nil {
		return nil, err
	}
	var local struct {
		Companies []Company `yaml:"companies"`
	}
	if err := readYAML(filepath.Join(dir, "watchlist.local.yaml"), &local); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	cfg.Watchlist = mergeWatchlist(wl.Companies, local.Companies)

	var cal struct {
		FOMC []Meeting `yaml:"fomc"`
	}
	if err := readYAML(filepath.Join(dir, "calendar.yaml"), &cal); err != nil {
		return nil, err
	}
	cfg.FOMC = cal.FOMC

	applyDefaults(&cfg.Settings)
	cfg.Secrets = secretsFromEnv()
	return cfg, cfg.validate()
}

func applyDefaults(s *Settings) {
	if s.MinGapDays <= 0 {
		s.MinGapDays = 7
	}
	if s.FirstRunDays <= 0 {
		s.FirstRunDays = 7
	}
	if s.SeasonDays <= 0 {
		s.SeasonDays = 100
	}
	if s.ExtractionModel == "" {
		s.ExtractionModel = "claude-opus-5-5"
	}
	if s.OverviewModel == "" {
		s.OverviewModel = "claude-opus-5-5"
	}
	if s.Effort == "" {
		s.Effort = "medium"
	}
}

func (c *Config) validate() error {
	for _, s := range c.Series {
		switch s.Kind {
		case "price", "rate", "index_yoy", "monthly_change", "level":
		default:
			return fmt.Errorf("series %s: unknown kind %q", s.ID, s.Kind)
		}
		switch s.Source {
		case "", "fred", "alphavantage":
		default:
			return fmt.Errorf("series %s: unknown source %q", s.ID, s.Source)
		}
	}
	for _, co := range c.Watchlist {
		switch co.Depth {
		case "headline", "standard", "deep":
		default:
			return fmt.Errorf("watchlist %s: unknown depth %q", co.Ticker, co.Depth)
		}
	}
	for _, s := range c.Series {
		if s.Fund && s.Kind != "price" {
			return fmt.Errorf("series %s: fund applies only to kind price", s.ID)
		}
	}
	if c.Settings.ExampleInvestment < 0 {
		return errors.New("example_investment must not be negative")
	}
	if c.Settings.ExampleInvestment == 0 {
		c.Settings.ExampleInvestment = 10000
	}
	pc := &c.Settings.Podcast
	if len(pc.Hosts) != 0 && len(pc.Hosts) != 2 {
		return fmt.Errorf("podcast: need exactly two hosts, got %d", len(pc.Hosts))
	}
	for _, h := range pc.Hosts {
		if h.Name == "" || h.Voice == "" {
			return errors.New("podcast: each host needs a name and a voice")
		}
	}
	if pc.Minutes <= 0 {
		pc.Minutes = 20
	}
	if pc.SpeakingRate == 0 {
		pc.SpeakingRate = 1
	}
	if pc.SpeakingRate < 0.25 || pc.SpeakingRate > 2 {
		return fmt.Errorf("podcast: speaking_rate %.2f is outside 0.25 to 2", pc.SpeakingRate)
	}
	return nil
}

// mergeWatchlist appends local entries; a local entry with the same ticker
// replaces the shared one.
func mergeWatchlist(base, local []Company) []Company {
	idx := map[string]int{}
	out := make([]Company, 0, len(base)+len(local))
	for _, c := range append(append([]Company{}, base...), local...) {
		c.Ticker = strings.ToUpper(strings.TrimSpace(c.Ticker))
		if c.Depth == "" {
			c.Depth = "headline"
		}
		if len(c.Forms) == 0 {
			c.Forms = []string{"8-K"}
		}
		if i, ok := idx[c.Ticker]; ok {
			out[i] = c
			continue
		}
		idx[c.Ticker] = len(out)
		out = append(out, c)
	}
	return out
}

func readYAML(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func secretsFromEnv() Secrets {
	var to []string
	for _, a := range strings.Split(os.Getenv("MAIL_TO"), ",") {
		if a = strings.TrimSpace(a); a != "" {
			to = append(to, a)
		}
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		host = "smtp.gmail.com"
	}
	from := os.Getenv("MAIL_FROM")
	if from == "" {
		from = os.Getenv("SMTP_USER")
	}
	return Secrets{
		AnthropicKey:    os.Getenv("ANTHROPIC_API_KEY"),
		FREDKey:         os.Getenv("FRED_API_KEY"),
		AlphaVantageKey: os.Getenv("ALPHAVANTAGE_API_KEY"),
		GoogleTTSKey:    os.Getenv("GOOGLE_TTS_API_KEY"),
		SECUserAgent:    os.Getenv("SEC_USER_AGENT"),
		SMTPHost:        host,
		SMTPPort:        port,
		SMTPUser:        os.Getenv("SMTP_USER"),
		SMTPPassword:    os.Getenv("SMTP_PASSWORD"),
		MailFrom:        from,
		MailTo:          to,
	}
}

// loadDotEnv sets variables from a .env file without overriding ones already
// present in the process environment. A missing file is not an error.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, set := os.LookupEnv(k); !set {
			_ = os.Setenv(k, v)
		}
	}
	return sc.Err()
}

// ParseDate parses a YYYY-MM-DD date in the local time zone.
func ParseDate(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, time.Local)
}
