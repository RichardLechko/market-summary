package digest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// browserCandidates are Chromium browsers that can print to PDF headlessly.
var browserCandidates = []string{
	`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
	`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	`C:\Program Files\Google\Chrome\Application\chrome.exe`,
	`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
}

// FindBrowser returns override if set, else the first installed Edge or Chrome.
func FindBrowser(override string) (string, error) {
	if override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("browser_path %q: %w", override, err)
		}
		return override, nil
	}
	for _, c := range browserCandidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", errors.New("no Edge or Chrome found; set browser_path in config/settings.yaml")
}

// PrintPDF renders htmlPath to pdfPath with a headless Chromium browser. A
// throwaway profile keeps it from attaching to a browser window already open.
func PrintPDF(ctx context.Context, browser, htmlPath, pdfPath string) error {
	abs, err := filepath.Abs(htmlPath)
	if err != nil {
		return err
	}
	profile, err := os.MkdirTemp("", "market-summary-browser-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(profile) }()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, browser,
		"--headless=new",
		"--disable-gpu",
		"--no-first-run",
		"--no-pdf-header-footer",
		"--virtual-time-budget=5000", // let web fonts load before printing
		"--user-data-dir="+profile,
		"--print-to-pdf="+pdfPath,
		"file:///"+strings.ReplaceAll(abs, `\`, "/"),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("print to PDF: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if st, err := os.Stat(pdfPath); err != nil || st.Size() == 0 {
		return fmt.Errorf("print to PDF produced no file at %s", pdfPath)
	}
	return nil
}

// OpenInBrowser opens a local file in the default browser.
func OpenInBrowser(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", abs).Start()
}
