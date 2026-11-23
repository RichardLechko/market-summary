package digest

import (
	"strings"
	"testing"
)

func TestSparkline(t *testing.T) {
	if got := sparkline([]float64{1, 2}, false); got != "" {
		t.Fatalf("two points should render nothing, got %q", got)
	}
	s := string(sparkline([]float64{-1, 2, 3, 1}, true))
	for _, want := range []string{`<svg class="spark"`, `<path d="M3.0 `, `class="zero"`, `class="end flag"`, "low -1.00, high 3.00"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
	if s := string(sparkline([]float64{5, 5, 5}, false)); strings.Contains(s, "NaN") || strings.Contains(s, "zero") {
		t.Errorf("flat line rendered badly: %s", s)
	}
}
