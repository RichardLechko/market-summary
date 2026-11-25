package digest

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/RichardLechko/market-summary/internal/analysis"
	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/model"
)

func f(v float64) *float64 { return &v }

func sample() *Digest {
	start := time.Date(2026, 11, 17, 9, 0, 0, 0, time.Local)
	end := time.Date(2026, 11, 25, 9, 0, 0, 0, time.Local)
	row := analysis.NewEarningsRow(config.Company{Ticker: "NVDA", Sector: "Mega-cap tech"}, model.Filing{
		Ticker: "NVDA", Filed: end.AddDate(0, 0, -5),
		Facts: &model.EarningsFacts{IsEarningsRelease: true, Period: "Q3 FY2027", Unit: "USD millions",
			Revenue: f(60000), RevenueYearAgo: f(40000), EPSDiluted: f(1.5), EPSDilutedYearAgo: f(1),
			Guidance: "raised", GuidanceNote: "Q4 revenue outlook above consensus.", Headline: "Data center demand <strong>.",
		},
	})
	return &Digest{
		WindowStart: start, WindowEnd: end,
		Overview:  "First paragraph.\n\nSecond paragraph.",
		Market:    []analysis.MacroRow{{Series: config.Series{Name: "S&P 500"}, ValueText: "6,150.00", ChangeText: "+2.5%", HasChange: true, Flagged: true}},
		Earnings:  []EarningsSector{{Sector: "Mega-cap tech", Reports: []EarningsView{NewEarningsView(row, "https://www.sec.gov/x")}}},
		WeekAhead: []AheadItem{{Date: end.AddDate(0, 0, 2), Label: "Consumer Price Index"}},
		Notes:     []string{"A note."},
	}
}

func TestRender(t *testing.T) {
	d := sample()
	out, err := d.RenderPage()
	if err != nil {
		t.Fatal(err)
	}
	// html/template escapes '+' as &#43;; compare against what a mail client shows.
	text := html.UnescapeString(out)
	for _, want := range []string{
		"Market summary: Nov 17 – Nov 25",
		"Wednesday, November 25, 2026",
		"<p>Second paragraph.</p>",
		"S&P 500",
		"Revenue 60,000 USD millions (+50.0% y/y)",
		"Diluted EPS 1.50 vs 1.00 a year ago (+50.0% y/y)",
		"Guidance raised.</b> Q4 revenue outlook above consensus.",
		"Data center demand <strong>.",
		"Consumer Price Index",
		"A note.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered HTML missing %q", want)
		}
	}
	if strings.Contains(out, "<h2>Federal Reserve</h2>") {
		t.Error("empty Fed section should be omitted")
	}
	if strings.Contains(out, "<strong>") {
		t.Error("model text must be escaped")
	}
}

func TestBrief(t *testing.T) {
	b := sample().Brief()
	for _, want := range []string{"S&P 500: 6,150.00 (+2.5%) [notable move]", "NVDA Q3 FY2027", "Consumer Price Index"} {
		if !strings.Contains(b, want) {
			t.Errorf("brief missing %q:\n%s", want, b)
		}
	}
}

func TestMessage(t *testing.T) {
	msg, err := Message("a@example.com", []string{"b@example.com"}, "Market summary: Nov 17 – Nov 25", "<p>hi</p>", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s := string(msg)
	if !strings.Contains(s, "Subject: =?utf-8?q?") || !strings.Contains(s, "Content-Type: text/html; charset=UTF-8") {
		t.Fatalf("message headers wrong:\n%s", s)
	}
}

func TestMailValidate(t *testing.T) {
	if err := (Mail{}).Validate(); err == nil || !strings.Contains(err.Error(), "SMTP_PASSWORD") {
		t.Fatalf("Validate = %v", err)
	}
}

func TestRenderEmail(t *testing.T) {
	d := sample()
	with, err := d.RenderEmail(true, false)
	if err != nil {
		t.Fatal(err)
	}
	without, _ := d.RenderEmail(false, false)
	if !strings.Contains(with, "attached as a PDF") || !strings.Contains(without, "latest.html") {
		t.Fatal("email footer wrong")
	}
	if strings.Contains(with, "MP3") {
		t.Fatal("no podcast was attached")
	}
	if audio, _ := d.RenderEmail(true, true); !strings.Contains(audio, "podcast version is attached as an MP3") {
		t.Fatal("podcast not mentioned")
	}
	if strings.Contains(with, "NVDA") {
		t.Fatal("email should carry only the overview")
	}
}

func TestMessageWithAttachment(t *testing.T) {
	msg, err := Message("a@example.com", []string{"b@example.com"}, "s", "<p>hi</p>", time.Now(),
		Attachment{Name: "x.pdf", ContentType: "application/pdf", Data: []byte("%PDF")})
	if err != nil {
		t.Fatal(err)
	}
	s := string(msg)
	for _, want := range []string{"multipart/mixed", `filename="x.pdf"`, "JVBERg=="} {
		if !strings.Contains(s, want) {
			t.Errorf("message missing %q", want)
		}
	}
}
