// Package digest assembles the summary, renders it as a web page and a short
// email, and sends it.
package digest

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"

	"github.com/RichardLechko/market-summary/internal/analysis"
	"github.com/RichardLechko/market-summary/internal/model"
)

var (
	//go:embed page.html
	pageHTML string
	//go:embed email.html
	emailHTML string
)

var funcs = template.FuncMap{
	"date":     func(t time.Time) string { return t.Format("Mon Jan 2") },
	"dateLong": func(t time.Time) string { return t.Format("Monday, January 2, 2006") },
	"paras":    paragraphs,
	"spark":    sparkline,
}

var (
	pageTmpl  = template.Must(template.New("page").Funcs(funcs).Parse(pageHTML))
	emailTmpl = template.Must(template.New("email").Funcs(funcs).Parse(emailHTML))
)

// Digest is everything the email shows.
type Digest struct {
	WindowStart time.Time
	WindowEnd   time.Time
	Overview    string
	Market      []analysis.MacroRow
	Macro       []MacroGroup
	Fed         []model.FedDoc
	Earnings    []EarningsSector
	Calls       []CallView    // earnings call notes
	News        []NewsView    // non-earnings company filings
	Insiders    []InsiderView // notable insider purchases and sales
	Clusters    []ClusterView // several insiders buying the same company
	Aggregate   *AggregateView
	WeekAhead   []AheadItem
	Notes       []string // sections that were unavailable and why
}

// MacroGroup is a titled block of macro rows.
type MacroGroup struct {
	Name string
	Rows []analysis.MacroRow
}

// EarningsSector groups reports by sector.
type EarningsSector struct {
	Sector  string
	Reports []EarningsView
}

// EarningsView is one rendered report.
type EarningsView struct {
	Ticker   string
	Company  string
	Period   string
	Filed    time.Time
	Figures  []string
	Guidance string // label and note combined, for the brief
	// GuidanceLabel is e.g. "Guidance raised"; GuidanceNote is the outlook sentence.
	GuidanceLabel string
	GuidanceNote  string
	Headline      string
	Summary       string
	Link          string
}

// AggregateView is the season-to-date summary across the watchlist.
type AggregateView struct {
	Lines   []string
	Sectors []string
}

// AheadItem is one upcoming event.
type AheadItem struct {
	Date  time.Time
	Label string
}

// Subject returns the email subject, naming the coverage window.
func (d *Digest) Subject() string {
	return fmt.Sprintf("Market summary: %s – %s", d.WindowStart.Format("Jan 2"), d.WindowEnd.Format("Jan 2"))
}

// RenderPage returns the full summary as a standalone web page.
func (d *Digest) RenderPage() (string, error) {
	var buf bytes.Buffer
	if err := pageTmpl.Execute(&buf, struct {
		*Digest
		Subject string
	}{d, d.Subject()}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderEmail returns the short email body: the opening summary and a pointer
// to the attached PDF.
func (d *Digest) RenderEmail(pdfAttached bool) (string, error) {
	var buf bytes.Buffer
	if err := emailTmpl.Execute(&buf, struct {
		*Digest
		Subject     string
		PDFAttached bool
	}{d, d.Subject(), pdfAttached}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func paragraphs(s string) []string {
	var out []string
	for _, p := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// NewEarningsView renders an earnings row's figures into display strings.
func NewEarningsView(r analysis.EarningsRow, link string) EarningsView {
	f := r.Facts
	v := EarningsView{
		Ticker:   r.Company.Ticker,
		Company:  f.Company,
		Period:   f.Period,
		Filed:    r.Filing.Filed,
		Headline: f.Headline,
		Summary:  f.Summary,
		Link:     link,
	}
	unit := ""
	if f.Unit != "" {
		unit = " " + f.Unit
	}
	if f.Revenue != nil {
		v.Figures = append(v.Figures, "Revenue "+analysis.FormatNumber(*f.Revenue, decimals(*f.Revenue))+unit+growthText(r.RevGrowth))
	}
	if f.EPSDiluted != nil {
		s := "Diluted EPS " + analysis.FormatNumber(*f.EPSDiluted, 2)
		if f.EPSDilutedYearAgo != nil {
			s += " vs " + analysis.FormatNumber(*f.EPSDilutedYearAgo, 2) + " a year ago"
		}
		v.Figures = append(v.Figures, s+growthText(r.EPSGrowth))
	}
	if f.NetIncome != nil {
		v.Figures = append(v.Figures, "Net income "+analysis.FormatNumber(*f.NetIncome, decimals(*f.NetIncome))+unit+growthText(r.NIGrowth))
	}
	switch f.Guidance {
	case "", "none":
	default:
		v.GuidanceLabel = "Guidance " + f.Guidance
		v.GuidanceNote = f.GuidanceNote
		v.Guidance = v.GuidanceLabel
		if f.GuidanceNote != "" {
			v.Guidance += ": " + f.GuidanceNote
		}
	}
	return v
}

func decimals(v float64) int {
	if math.Abs(v) >= 100 {
		return 0
	}
	return 2
}

func growthText(g *float64) string {
	if g == nil {
		return ""
	}
	sign := ""
	if *g > 0 {
		sign = "+"
	}
	return fmt.Sprintf(" (%s%.1f%% y/y)", sign, *g)
}

// NewAggregateView renders the season aggregate.
func NewAggregateView(a analysis.Aggregate, seasonDays int) *AggregateView {
	v := &AggregateView{}
	v.Lines = append(v.Lines, fmt.Sprintf("%d of %d watchlist companies have reported in the last %d days.", a.Reported, a.Total, seasonDays))
	if a.WithRevenue > 0 {
		v.Lines = append(v.Lines, fmt.Sprintf("%d of %d grew revenue from a year ago; median revenue growth %.1f%%.",
			a.RevGrowthPositive, a.WithRevenue, a.MedianRevGrowth))
	}
	if n := a.GuidanceRaised + a.GuidanceHeld + a.GuidanceLowered; n > 0 {
		v.Lines = append(v.Lines, fmt.Sprintf("Guidance: %d raised, %d held, %d lowered.", a.GuidanceRaised, a.GuidanceHeld, a.GuidanceLowered))
	}
	for _, s := range a.Sectors {
		line := fmt.Sprintf("%s: %d reported", s.Sector, s.Count)
		if !math.IsNaN(s.MedianRevGrowth) {
			line += fmt.Sprintf(", median revenue growth %.1f%%", s.MedianRevGrowth)
		}
		v.Sectors = append(v.Sectors, line)
	}
	return v
}

// Brief renders the computed facts as plain text for the overview prompt.
func (d *Digest) Brief() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Coverage: %s to %s\n", d.WindowStart.Format("2006-01-02"), d.WindowEnd.Format("2006-01-02"))
	writeRows := func(title string, rows []analysis.MacroRow) {
		if len(rows) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s:\n", title)
		for _, r := range rows {
			fmt.Fprintf(&b, "- %s: %s", r.Series.Name, r.ValueText)
			if r.HasChange {
				fmt.Fprintf(&b, " (%s)", r.ChangeText)
			}
			if r.Flagged {
				b.WriteString(" [notable move]")
			}
			fmt.Fprintf(&b, ", as of %s\n", r.AsOf.Format("2006-01-02"))
		}
	}
	writeRows("Market (change since last digest)", d.Market)
	for _, g := range d.Macro {
		writeRows(g.Name, g.Rows)
	}
	if len(d.Fed) > 0 {
		b.WriteString("\nFederal Reserve:\n")
		for _, f := range d.Fed {
			fmt.Fprintf(&b, "- %s (%s): %s\n", f.Title, f.Published.Format("2006-01-02"), f.Summary)
		}
	}
	for _, s := range d.Earnings {
		fmt.Fprintf(&b, "\nEarnings, %s:\n", s.Sector)
		for _, r := range s.Reports {
			fmt.Fprintf(&b, "- %s %s: %s %s %s\n", r.Ticker, r.Period, r.Headline, strings.Join(r.Figures, "; "), r.Guidance)
		}
	}
	if len(d.Calls) > 0 {
		b.WriteString("\nEarnings calls:\n")
		for _, c := range d.Calls {
			fmt.Fprintf(&b, "- %s %s: %s", c.Ticker, c.Period, c.Tone)
			for _, t := range c.Themes {
				fmt.Fprintf(&b, " %s: %s.", t.Topic, t.Detail)
			}
			if c.Outlook != "" {
				fmt.Fprintf(&b, " Outlook: %s", c.Outlook)
			}
			b.WriteString("\n")
		}
	}
	if len(d.News) > 0 {
		b.WriteString("\nCompany news (SEC filings):\n")
		for _, n := range d.News {
			fmt.Fprintf(&b, "- %s %s (%s, filed %s): %s %s\n", n.Ticker, n.Kind, n.Company, n.Filed.Format("2006-01-02"), n.Headline, n.Summary)
		}
	}
	for _, c := range d.Clusters {
		fmt.Fprintf(&b, "\nSeveral insiders buying %s: %s\n", c.Ticker, c.Text)
	}
	if len(d.Insiders) > 0 {
		b.WriteString("\nLarge insider trades (Form 4):\n")
		for _, in := range d.Insiders {
			fmt.Fprintf(&b, "- %s: %s (%s) %s. %s\n", in.Ticker, in.Who, in.Role, in.What, in.Detail)
		}
	}
	if d.Aggregate != nil {
		b.WriteString("\nEarnings season so far:\n")
		for _, l := range append(append([]string{}, d.Aggregate.Lines...), d.Aggregate.Sectors...) {
			fmt.Fprintf(&b, "- %s\n", l)
		}
	}
	if len(d.WeekAhead) > 0 {
		b.WriteString("\nComing up:\n")
		for _, a := range d.WeekAhead {
			fmt.Fprintf(&b, "- %s: %s\n", a.Date.Format("Mon 2006-01-02"), a.Label)
		}
	}
	return b.String()
}

// CallView is one rendered earnings call summary.
type CallView struct {
	Ticker       string
	Company      string
	Period       string
	Tone         string
	Themes       []model.CallTheme
	AnalystFocus []string
	Outlook      string
}

// NewCallView renders stored call notes.
func NewCallView(n model.CallNote, company string) CallView {
	return CallView{
		Ticker: n.Ticker, Company: company, Period: n.Period,
		Tone: n.Notes.Tone, Themes: n.Notes.Themes, AnalystFocus: n.Notes.AnalystFocus, Outlook: n.Notes.Outlook,
	}
}

// NewsView is one rendered company event, such as a leadership change.
type NewsView struct {
	Ticker   string
	Company  string
	Kind     string // e.g. "Leadership change"
	Filed    time.Time
	Headline string
	Summary  string
	Link     string
}
