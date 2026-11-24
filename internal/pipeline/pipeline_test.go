package pipeline

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RichardLechko/market-summary/internal/collect"
	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/digest"
	"github.com/RichardLechko/market-summary/internal/httpx"
	"github.com/RichardLechko/market-summary/internal/store"
	"github.com/RichardLechko/market-summary/internal/summarize"
)

var now0 = time.Date(2026, 11, 24, 9, 0, 0, 0, time.Local)

type fakeSender struct {
	sent []string
	atts [][]digest.Attachment
	err  error
}

func (f *fakeSender) Send(subject, body string, atts ...digest.Attachment) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, subject+"\n"+body)
	f.atts = append(f.atts, atts)
	return nil
}

func readPage(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(string(b))
}

type fakeCaller struct {
	extractions  atomic.Int32
	eventPrompts []string
}

func (f *fakeCaller) Call(_ context.Context, req summarize.Request) (string, error) {
	if strings.Contains(req.System, "earnings call transcripts") {
		if strings.Contains(req.User, "RIGHT CALL") {
			return `{"matches_period":true,"tone":"Management sounded confident.","themes":[{"topic":"Deposits","detail":"Deposits rose."}],"analyst_focus":["Analysts asked about credit losses."],"outlook":"Expects steady lending."}`, nil
		}
		return `{"matches_period":false,"tone":"","themes":[],"analyst_focus":[],"outlook":""}`, nil
	}
	if strings.Contains(req.System, "company news other than earnings") {
		f.eventPrompts = append(f.eventPrompts, req.User)
		if strings.Contains(req.User, "schedule13D") {
			return `{"company":"JPMorgan Chase","kind":"Activist stake","routine":false,"headline":"An investor took a 6% stake.","summary":"It plans to talk to the board."}`, nil
		}
		if strings.Contains(req.User, "PAY PLAN") {
			return `{"company":"JPMorgan Chase","kind":"Pay plan","routine":true,"headline":"Pay plan amended.","summary":"Routine."}`, nil
		}
		return `{"company":"JPMorgan Chase","kind":"Leadership change","routine":false,"headline":"The CEO resigned.","summary":"No reason was given."}`, nil
	}
	if req.Schema != nil {
		n := f.extractions.Add(1)
		return fmt.Sprintf(`{"is_earnings_release":true,"company":"JPMorgan Chase","period":"Q%d 2026","unit":"USD millions",
			"revenue":110,"revenue_year_ago":100,"eps_diluted":2,"eps_diluted_year_ago":1.6,"net_income":null,"net_income_year_ago":null,
			"guidance":"none","guidance_note":"","headline":"Lending grew.","summary":"Summary %d."}`, n, n), nil
	}
	if strings.Contains(req.System, "Federal Reserve") {
		return "The Fed held rates.", nil
	}
	return "Overview paragraph.", nil
}

func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/fred/series/observations", func(w http.ResponseWriter, r *http.Request) {
		var parts []string
		switch r.URL.Query().Get("series_id") {
		case "SP500":
			for i := 30; i >= 0; i-- {
				d := now0.AddDate(0, 0, -i)
				parts = append(parts, fmt.Sprintf(`{"date":"%s","value":"%d"}`, d.Format("2006-01-02"), 6000+(30-i)*10))
			}
		case "CPIAUCSL":
			for i := 25; i >= 1; i-- {
				d := time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local).AddDate(0, -i, 0)
				parts = append(parts, fmt.Sprintf(`{"date":"%s","value":"%.2f"}`, d.Format("2006-01-02"), 300+float64(25-i)*0.8))
			}
		}
		fmt.Fprintf(w, `{"observations":[%s]}`, strings.Join(parts, ","))
	})
	mux.HandleFunc("/fred/releases/dates", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"release_dates":[{"release_id":10,"release_name":"Consumer Price Index","date":"2026-11-26"}]}`)
	})
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Fed</title>
<item><title>Minutes of the Federal Open Market Committee</title><link>%s/minutes</link><pubDate>Thu, 19 Nov 2026 19:00:00 GMT</pubDate></item>
</channel></rss>`, srv.URL)
	})
	mux.HandleFunc("/minutes", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<html><body><div id="article"><p>Participants agreed policy was well positioned.</p></div></body></html>`)
	})
	mux.HandleFunc("/data/group/otcMarket/name/consolidatedShortInterest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"symbolCode":"JPM","settlementDate":"2026-10-30","currentShortPositionQuantity":20000000,"averageDailyVolumeQuantity":10000000,"daysToCoverQuantity":2},
			{"symbolCode":"JPM","settlementDate":"2026-11-13","currentShortPositionQuantity":26000000,"averageDailyVolumeQuantity":10000000,"daysToCoverQuantity":2.6}]`)
	})
	mux.HandleFunc("/api/xbrl/companyconcept/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"units":{"shares":[{"end":"2026-10-31","val":1000000000}]}}`)
	})
	mux.HandleFunc("/files/company_tickers.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"0":{"cik_str":19617,"ticker":"JPM","title":"JPMorgan Chase"}}`)
	})
	mux.HandleFunc("/submissions/CIK0000019617.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"filings":{"recent":{
			"accessionNumber":["0000019617-26-000330","0000019617-26-000320","0000019617-26-000315","0000019617-26-000310","0000019617-26-000305","0000019617-26-000300","0000019617-26-000200","0000019617-26-000250","0000019617-26-000100"],
			"filingDate":["2026-11-23","2026-11-23","2026-11-23","2026-11-22","2026-11-21","2026-11-20","2026-11-19","2026-10-14","2026-08-14"],
			"form":["SCHEDULE 13D","4","SCHEDULE 13D","8-K","8-K","8-K","8-K","8-K","8-K"],
			"items":["","","","5.02,9.01","5.02","2.02,9.01","8.01","2.02,9.01","2.02,9.01"],
			"primaryDocument":["xslSCHEDULE_13D_X02/d13.xml","xslF345X06/f4.xml","xslSCHEDULE_13D_X02/other13.xml","f.htm","g.htm","a.htm","b.htm","e.htm","c.htm"]},"files":[]}}`)
	})
	mux.HandleFunc("/Archives/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "-index.htm") {
			dir := r.URL.Path[:strings.LastIndex(r.URL.Path, "/")]
			fmt.Fprintf(w, `<table class="tableFile"><tr><td>1</td><td>PR</td><td><a href="%s/ex991.htm">ex991.htm</a></td><td>EX-99.1</td></tr></table>`, dir)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/f.htm"):
			fmt.Fprint(w, `<html><body><p>Item 5.02. CEO RESIGNS effective immediately.</p></body></html>`)
			return
		case strings.HasSuffix(r.URL.Path, "/d13.xml"):
			fmt.Fprint(w, `<edgarSubmission xmlns="http://www.sec.gov/edgar/schedule13D"><formData><coverPageHeader><issuerInfo><issuerCIK>0000019617</issuerCIK></issuerInfo></coverPageHeader></formData></edgarSubmission>`)
			return
		case strings.HasSuffix(r.URL.Path, "/other13.xml"):
			fmt.Fprint(w, `<edgarSubmission xmlns="http://www.sec.gov/edgar/schedule13D"><formData><coverPageHeader><issuerInfo><issuerCIK>0000099999</issuerCIK></issuerInfo></coverPageHeader></formData></edgarSubmission>`)
			return
		case strings.HasSuffix(r.URL.Path, "/f4.xml"):
			fmt.Fprint(w, `<ownershipDocument><issuer><issuerCik>0000019617</issuerCik></issuer><reportingOwner><reportingOwnerId><rptOwnerName>Dimon James</rptOwnerName></reportingOwnerId><reportingOwnerRelationship><isOfficer>1</isOfficer><officerTitle>Chairman and CEO</officerTitle></reportingOwnerRelationship></reportingOwner><aff10b5One>1</aff10b5One><nonDerivativeTable><nonDerivativeTransaction><transactionDate><value>2026-11-20</value></transactionDate><transactionCoding><transactionCode>S</transactionCode></transactionCoding><transactionAmounts><transactionShares><value>100000</value></transactionShares><transactionPricePerShare><value>300</value></transactionPricePerShare></transactionAmounts><postTransactionAmounts><sharesOwnedFollowingTransaction><value>900000</value></sharesOwnedFollowingTransaction></postTransactionAmounts><ownershipNature><directOrIndirectOwnership><value>D</value></directOrIndirectOwnership></ownershipNature></nonDerivativeTransaction><nonDerivativeTransaction><transactionDate><value>2026-11-20</value></transactionDate><transactionCoding><transactionCode>A</transactionCode></transactionCoding><transactionAmounts><transactionShares><value>5000</value></transactionShares></transactionAmounts></nonDerivativeTransaction></nonDerivativeTable></ownershipDocument>`)
			return
		case strings.HasSuffix(r.URL.Path, "/g.htm"):
			fmt.Fprint(w, `<html><body><p>Item 5.02. PAY PLAN amended.</p></body></html>`)
			return
		}
		fmt.Fprint(w, `<html><body><p>JPMorgan reports results</p><table><tr><td>Revenue</td><td>110</td><td>100</td></tr></table></body></html>`)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestPipeline(t *testing.T, srv *httptest.Server, caller summarize.Caller, sender Sender) *Pipeline {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{
		Root: root,
		Settings: config.Settings{
			MinGapDays: 7, FirstRunDays: 7, SeasonDays: 100, Effort: "medium",
			FedFeeds: []config.Feed{{URL: srv.URL + "/feed.xml"}},
			Releases: []string{"Consumer Price Index"},
		},
		Series: []config.Series{
			{ID: "SP500", Name: "S&P 500", Group: "Market", Kind: "price", Threshold: 1},
			{ID: "CPIAUCSL", Name: "CPI inflation", Group: "Inflation", Kind: "index_yoy"},
		},
		Watchlist: []config.Company{{Ticker: "JPM", Sector: "Banks", Depth: "standard", Forms: []string{"8-K"}}},
		FOMC:      []config.Meeting{{Start: "2026-11-25", End: "2026-11-26"}},
		Secrets:   config.Secrets{FREDKey: "k", SECUserAgent: "test"},
	}
	hc := httpx.New("test", 0)
	hc.BaseBackoff = time.Millisecond
	p := &Pipeline{
		Cfg: cfg, Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		FRED:  &collect.FRED{HTTP: hc, Key: "k", BaseURL: srv.URL + "/fred"},
		Fed:   &collect.Fed{HTTP: hc},
		EDGAR: &collect.EDGAR{HTTP: hc, WWWBase: srv.URL, DataBase: srv.URL},
		FINRA: &collect.FINRA{HTTP: hc, BaseURL: srv.URL},
		Mail:  sender,
	}
	if caller != nil {
		p.Sum = &summarize.Summarizer{Caller: caller, Cache: st, ExtractionModel: "m", OverviewModel: "m", Effort: "medium"}
	}
	return p
}

func TestRunEndToEnd(t *testing.T) {
	srv := fixtureServer(t)
	caller := &fakeCaller{}
	sender := &fakeSender{}
	p := newTestPipeline(t, srv, caller, sender)
	ctx := context.Background()

	res, err := p.Run(ctx, Options{Now: now0})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Sent || len(sender.sent) != 1 {
		t.Fatalf("first run not sent: %+v", res)
	}
	body := readPage(t, res.Path)
	for _, want := range []string{
		"Market summary: Nov 17 – Nov 24",
		"Overview paragraph.",
		"S&P 500",
		"CPI inflation",
		"Minutes of the Federal Open Market Committee",
		"The Fed held rates.",
		"Lending grew.",
		"Company news",
		"The CEO resigned.",
		"Leadership change",
		"An investor took a 6% stake.",
		"Short interest",
		"As of Friday, November 13, 2026",
		"2.6%",
		"26.0M",
		"+30.0%",
		"Insider trades",
		"James Dimon",
		"Sold 100,000 shares for $30.0M",
		"About 10% of the shares held in those accounts. Under a pre-arranged trading plan.",
		"Revenue 110 USD millions (+10.0% y/y)",
		"Consumer Price Index",
		"FOMC meeting, decision Thu Nov 26",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("digest missing %q", want)
		}
	}
	if strings.Contains(body, "Pay plan amended.") {
		t.Error("routine event shown")
	}
	if strings.Contains(body, "Not about this company.") {
		t.Error("13D about another company shown")
	}
	var sawEarlier bool
	for _, pr := range caller.eventPrompts {
		if strings.Contains(pr, "schedule13D") && strings.Contains(pr, "2026-11-22: The CEO resigned.") {
			sawEarlier = true
		}
	}
	if !sawEarlier {
		t.Error("13D prompt did not include the earlier leadership news")
	}
	// Current release plus the prior quarter for the standard-depth comparison.
	if n := caller.extractions.Load(); n != 2 {
		t.Errorf("extractions = %d, want 2", n)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Errorf("digest file not written: %v", err)
	}

	// Next day: gate holds.
	res, err = p.Run(ctx, Options{Now: now0.AddDate(0, 0, 1)})
	if err != nil || !res.NotDue || len(sender.sent) != 1 {
		t.Fatalf("second run should not be due: %+v %v", res, err)
	}

	// Eight days later: due; the filing already emailed is not repeated and not re-extracted.
	res, err = p.Run(ctx, Options{Now: now0.AddDate(0, 0, 8)})
	if err != nil || !res.Sent {
		t.Fatalf("third run: %+v %v", res, err)
	}
	third := readPage(t, res.Path)
	if strings.Contains(third, "Lending grew.") {
		t.Error("already-reported filing repeated")
	}
	if strings.Contains(third, "The CEO resigned.") {
		t.Error("already-reported event repeated")
	}
	if strings.Contains(third, "Shares sold short and not yet bought back") {
		t.Error("unchanged short interest report repeated")
	}
	if !strings.Contains(third, "Market summary: Nov 24 – Dec 2") {
		t.Error("window should start at previous cutoff")
	}
	if n := caller.extractions.Load(); n != 2 {
		t.Errorf("extractions after third run = %d, want 2", n)
	}
}

func TestSendFailureDoesNotRecord(t *testing.T) {
	srv := fixtureServer(t)
	p := newTestPipeline(t, srv, &fakeCaller{}, &fakeSender{err: errors.New("smtp down")})
	if _, err := p.Run(context.Background(), Options{Now: now0}); err == nil {
		t.Fatal("expected send error")
	}
	if v, _ := p.Store.Meta(metaLastSent); v != "" {
		t.Fatalf("last sent recorded despite failure: %q", v)
	}
	d, _ := p.Gate(now0.AddDate(0, 0, 1))
	if !d.Due {
		t.Fatal("failed send should leave the next run due")
	}
}

func TestDryRunWithoutKeys(t *testing.T) {
	srv := fixtureServer(t)
	sender := &fakeSender{}
	p := newTestPipeline(t, srv, nil, sender)
	p.Cfg.Secrets.FREDKey = ""
	res, err := p.Run(context.Background(), Options{DryRun: true, Now: now0})
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent || len(sender.sent) != 0 {
		t.Fatal("dry run must not send")
	}
	b, _ := os.ReadFile(res.Path)
	page := html.UnescapeString(string(b))
	for _, want := range []string{"FRED_API_KEY is not set", "ANTHROPIC_API_KEY is not set", "Figures need ANTHROPIC_API_KEY"} {
		if !strings.Contains(page, want) {
			t.Errorf("dry run page missing %q", want)
		}
	}
	if v, _ := p.Store.Meta(metaLastSent); v != "" {
		t.Fatal("dry run recorded a send")
	}
}

func TestBackfillSeasonNotShown(t *testing.T) {
	srv := fixtureServer(t)
	caller := &fakeCaller{}
	sender := &fakeSender{}
	p := newTestPipeline(t, srv, caller, sender)
	p.Cfg.Watchlist[0].Depth = "headline" // no prior-quarter extraction
	res, err := p.Run(context.Background(), Options{Now: now0})
	if err != nil {
		t.Fatal(err)
	}
	// Current release (Nov 20) plus the Oct 14 release backfilled for the season; Aug 14 is outside 100 days.
	if n := caller.extractions.Load(); n != 2 {
		t.Fatalf("extractions = %d, want 2", n)
	}
	body := readPage(t, res.Path)
	if got := strings.Count(body, "Lending grew."); got != 1 {
		t.Fatalf("headline shown %d times, want 1 (backfill must not appear as new)", got)
	}
	f, err := p.Store.Filing("0000019617-26-000250")
	if err != nil || f == nil || f.Facts == nil || !f.Reported {
		t.Fatalf("backfilled filing = %+v, %v", f, err)
	}
}

func TestEmailAttachesPDF(t *testing.T) {
	srv := fixtureServer(t)
	sender := &fakeSender{}
	p := newTestPipeline(t, srv, &fakeCaller{}, sender)
	opened := ""
	p.PrintPDF = func(_ context.Context, _, pdfPath string) error {
		return os.WriteFile(pdfPath, []byte("%PDF-1.7 fake"), 0o644)
	}
	p.Open = func(path string) error { opened = path; return nil }
	res, err := p.Run(context.Background(), Options{Now: now0, Open: true})
	if err != nil {
		t.Fatal(err)
	}
	if opened != res.Path {
		t.Errorf("opened %q, want %q", opened, res.Path)
	}
	if len(sender.atts[0]) != 1 || sender.atts[0][0].ContentType != "application/pdf" || string(sender.atts[0][0].Data) != "%PDF-1.7 fake" {
		t.Fatalf("attachments = %+v", sender.atts[0])
	}
	email := html.UnescapeString(sender.sent[0])
	if !strings.Contains(email, "Overview paragraph.") || !strings.Contains(email, "attached as a PDF") || strings.Contains(email, "Lending grew.") {
		t.Fatalf("email body should be the short version:\n%s", email)
	}
}

// failFirstCaller fails every extraction on its first pass, then behaves
// like fakeCaller.
type failFirstCaller struct {
	fakeCaller
	failing atomic.Bool
}

func (f *failFirstCaller) Call(ctx context.Context, req summarize.Request) (string, error) {
	if f.failing.Load() && req.Schema != nil {
		return "", errors.New("claude: response hit max_tokens")
	}
	return f.fakeCaller.Call(ctx, req)
}

func TestBackfillRetriesFailedExtraction(t *testing.T) {
	srv := fixtureServer(t)
	caller := &failFirstCaller{}
	caller.failing.Store(true)
	p := newTestPipeline(t, srv, caller, &fakeSender{})
	p.Cfg.Watchlist[0].Depth = "headline"
	if _, err := p.Run(context.Background(), Options{Now: now0, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if f, _ := p.Store.Filing("0000019617-26-000250"); f != nil && f.Facts != nil {
		t.Fatal("extraction should have failed on the first run")
	}
	caller.failing.Store(false)
	if _, err := p.Run(context.Background(), Options{Now: now0, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if f, _ := p.Store.Filing("0000019617-26-000250"); f == nil || f.Facts == nil {
		t.Fatal("failed backfill was not retried")
	}
}
