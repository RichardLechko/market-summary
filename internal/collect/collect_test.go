package collect

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RichardLechko/market-summary/internal/httpx"
)

func day(s string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02", s, time.Local)
	return t
}

func testClient() *httpx.Client {
	c := httpx.New("test-agent", 0)
	c.BaseBackoff = time.Millisecond
	return c
}

func TestFREDObservations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/series/observations" || r.URL.Query().Get("series_id") != "SP500" {
			t.Errorf("unexpected request %s", r.URL)
		}
		fmt.Fprint(w, `{"observations":[{"date":"2026-11-19","value":"6100.5"},{"date":"2026-11-20","value":"."},{"date":"2026-11-21","value":"6150"}]}`)
	}))
	defer srv.Close()
	f := &FRED{HTTP: testClient(), Key: "k", BaseURL: srv.URL}
	obs, err := f.Observations(context.Background(), "SP500", day("2026-11-01"))
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 2 || obs[1].Value != 6150 {
		t.Fatalf("obs = %+v", obs)
	}
}

func TestFREDKeyRedacted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	f := &FRED{HTTP: testClient(), Key: "secret-key", BaseURL: srv.URL}
	_, err := f.Observations(context.Background(), "X", day("2026-11-01"))
	if err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("err = %v", err)
	}
}

func TestFREDReleaseDates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"release_dates":[
			{"release_id":10,"release_name":"Consumer Price Index","date":"2026-11-25"},
			{"release_id":99,"release_name":"Some Regional Survey","date":"2026-11-25"},
			{"release_id":28,"release_name":"Consumer Price Index Excluding Energy Commodities","date":"2026-11-25"},
			{"release_id":50,"release_name":"Employment Situation","date":"2026-12-20"}]}`)
	}))
	defer srv.Close()
	f := &FRED{HTTP: testClient(), Key: "k", BaseURL: srv.URL}
	rs, err := f.ReleaseDates(context.Background(), day("2026-11-24"), day("2026-11-30"), []string{"consumer price index", "Employment Situation"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].ReleaseID != 10 {
		t.Fatalf("release dates = %+v", rs)
	}
}

func TestRetryOn5xx(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	body, err := testClient().Get(context.Background(), srv.URL)
	if err != nil || string(body) != "ok" || n != 3 {
		t.Fatalf("body=%q err=%v attempts=%d", body, err, n)
	}
}

func TestHTMLTextTables(t *testing.T) {
	doc := `<html><head><style>x{}</style></head><body><div id="article"><p>Results for the quarter</p>
<table><tr><td>Revenue</td><td>12,345</td><td>11,000</td></tr></table></div><p>footer</p></body></html>`
	got := HTMLText([]byte(doc), "article")
	if !strings.Contains(got, "Revenue | 12,345 | 11,000") {
		t.Fatalf("table cells not separated: %q", got)
	}
	if strings.Contains(got, "footer") || strings.Contains(got, "x{}") {
		t.Fatalf("selector or style not applied: %q", got)
	}
}

func TestPickExhibit(t *testing.T) {
	idx := `<table class="tableFile">
<tr><th>Seq</th><th>Description</th><th>Document</th><th>Type</th></tr>
<tr><td>1</td><td>8-K</td><td><a href="/ix?doc=/Archives/edgar/data/1/0001/main.htm">main.htm</a></td><td>8-K</td></tr>
<tr><td>2</td><td>CFO commentary</td><td><a href="/Archives/edgar/data/1/0001/ex992.htm">ex992.htm</a></td><td>EX-99.2</td></tr>
<tr><td>3</td><td>Press release</td><td><a href="/Archives/edgar/data/1/0001/ex991.htm">ex991.htm</a></td><td>EX-99.1</td></tr>
</table>`
	if got := PickExhibit([]byte(idx)); got != "/Archives/edgar/data/1/0001/ex991.htm" {
		t.Fatalf("PickExhibit = %q", got)
	}
}

func TestSubmissionsPaging(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/submissions/CIK0000019617.json":
			fmt.Fprint(w, `{"filings":{"recent":{
				"accessionNumber":["a-3","a-2"],"filingDate":["2026-10-14","2026-10-01"],
				"form":["8-K","424B2"],"items":["2.02,9.01",""],"primaryDocument":["p3.htm","p2.htm"]},
				"files":[{"name":"page-001.json","filingFrom":"2025-06-01","filingTo":"2026-09-30"},
				         {"name":"page-002.json","filingFrom":"2020-01-01","filingTo":"2025-05-31"}]}}`)
		case "/submissions/page-001.json":
			fmt.Fprint(w, `{"accessionNumber":["a-1"],"filingDate":["2026-07-15"],"form":["8-K"],"items":["2.02"],"primaryDocument":["p1.htm"]}`)
		default:
			t.Errorf("unexpected fetch %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	e := &EDGAR{HTTP: testClient(), DataBase: srv.URL}
	subs, err := e.Submissions(context.Background(), 19617, day("2025-10-01"))
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 3 || subs[0].Accession != "a-3" || subs[2].Accession != "a-1" {
		t.Fatalf("subs = %+v", subs)
	}
	if !subs[0].IsEarnings8K() || subs[1].IsEarnings8K() {
		t.Fatal("IsEarnings8K wrong")
	}
}

func TestFedDocs(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed.xml":
			fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Fed</title>
<item><title>Federal Reserve issues FOMC statement</title><link>%[1]s/stmt</link><pubDate>Wed, 18 Nov 2026 19:00:00 GMT</pubDate></item>
<item><title>Old item</title><link>%[1]s/old</link><pubDate>Wed, 01 Oct 2026 19:00:00 GMT</pubDate></item>
<item><title>Minutes of the Federal Open Market Committee, October 27-28, 2026</title><link>%[1]s/minutes-pr</link><pubDate>Wed, 18 Nov 2026 19:00:00 GMT</pubDate></item>
<item><title>Minutes of the Board's discount rate meetings</title><link>%[1]s/dr</link><pubDate>Thu, 19 Nov 2026 19:00:00 GMT</pubDate></item>
</channel></rss>`, srv.URL)
		case "/minutes-pr":
			fmt.Fprint(w, `<div id="article"><p>Minutes released.</p><a href="/monetarypolicy/fomcminutes20261028.htm">HTML</a></div>`)
		case "/monetarypolicy/fomcminutes20261028.htm":
			fmt.Fprint(w, `<div id="article"><p>Participants judged inflation elevated.</p></div>`)
		case "/stmt":
			fmt.Fprint(w, `<html><body><div id="article"><p>The Committee decided to maintain the target range.</p></div></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := &Fed{HTTP: testClient()}
	docs, err := f.Docs(context.Background(), srv.URL+"/feed.xml", []string{"discount rate"}, day("2026-11-10"))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].Kind != "statement" || !strings.Contains(docs[0].Body, "maintain the target range") {
		t.Fatalf("docs = %+v", docs)
	}
	if docs[1].Kind != "minutes" || !strings.Contains(docs[1].Body, "inflation elevated") {
		t.Fatalf("minutes not followed: %+v", docs[1])
	}
}

func TestLatestBeigeBook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.htm":
			fmt.Fprint(w, `<!--<ul><li>/monetarypolicy/beigebook202610.htm</li><li>/monetarypolicy/beigebook202608.htm</li></ul>-->`)
		case "/monetarypolicy/beigebook202610.htm":
			fmt.Fprint(w, `<div id="article"><p>Economic activity was little changed.</p></div><div id="lastUpdate">Last Update: October 14, 2026 </div>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	doc, err := (&Fed{HTTP: testClient()}).LatestBeigeBook(context.Background(), srv.URL+"/index.htm")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Beige Book, October 2026" || doc.Published.Format("2006-01-02") != "2026-10-14" ||
		!strings.Contains(doc.Body, "little changed") || !strings.HasSuffix(doc.Link, "/monetarypolicy/beigebook202610.htm") {
		t.Fatalf("doc = %+v", doc)
	}
}
