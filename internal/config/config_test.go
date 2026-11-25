package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadRepoConfig validates the committed config files.
func TestLoadRepoConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Series) == 0 || len(cfg.Watchlist) == 0 || len(cfg.FOMC) == 0 || len(cfg.Settings.FedFeeds) == 0 {
		t.Fatalf("config incomplete: %d series, %d companies, %d meetings", len(cfg.Series), len(cfg.Watchlist), len(cfg.FOMC))
	}
	for _, m := range cfg.FOMC {
		if _, err := ParseDate(m.Start); err != nil {
			t.Errorf("bad FOMC date %q", m.Start)
		}
	}
}

func TestMergeWatchlist(t *testing.T) {
	got := mergeWatchlist(
		[]Company{{Ticker: "JPM", Sector: "Banks", Depth: "headline"}},
		[]Company{{Ticker: "jpm", Sector: "Banks", Depth: "standard"}, {Ticker: "VTI", Sector: "Mine"}},
	)
	if len(got) != 2 || got[0].Depth != "standard" || got[1].Depth != "headline" || got[1].Forms[0] != "8-K" {
		t.Fatalf("merged = %+v", got)
	}
}

func TestDotEnvDoesNotOverride(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte("# c\nMS_TEST_A=\"from-file\"\nMS_TEST_B=file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MS_TEST_B", "from-env")
	t.Setenv("MS_TEST_A", "")
	os.Unsetenv("MS_TEST_A")
	if err := loadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("MS_TEST_A") != "from-file" || os.Getenv("MS_TEST_B") != "from-env" {
		t.Fatalf("A=%q B=%q", os.Getenv("MS_TEST_A"), os.Getenv("MS_TEST_B"))
	}
}

func TestPodcastSettings(t *testing.T) {
	c := &Config{}
	if err := c.validate(); err != nil || c.Settings.Podcast.Minutes != 20 || c.Settings.Podcast.SpeakingRate != 1 {
		t.Fatalf("defaults: %+v, %v", c.Settings.Podcast, err)
	}
	for _, p := range []Podcast{
		{Hosts: []Host{{Name: "A", Voice: "v"}}},
		{Hosts: []Host{{Name: "A", Voice: "v"}, {Name: "B"}}},
		{SpeakingRate: 3},
	} {
		c := &Config{Settings: Settings{Podcast: p}}
		if c.validate() == nil {
			t.Errorf("accepted %+v", p)
		}
	}
}
