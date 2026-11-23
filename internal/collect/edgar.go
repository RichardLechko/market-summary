package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/RichardLechko/market-summary/internal/httpx"
)

// EDGAR is a client for SEC EDGAR. SEC requires a descriptive User-Agent with
// contact details and allows at most 10 requests per second; set both on HTTP.
type EDGAR struct {
	HTTP     *httpx.Client
	WWWBase  string // default https://www.sec.gov
	DataBase string // default https://data.sec.gov
}

func (e *EDGAR) www() string {
	if e.WWWBase != "" {
		return e.WWWBase
	}
	return "https://www.sec.gov"
}

func (e *EDGAR) data() string {
	if e.DataBase != "" {
		return e.DataBase
	}
	return "https://data.sec.gov"
}

// SubFiling is one row of a company's filing history.
type SubFiling struct {
	Accession   string
	Form        string
	Filed       time.Time
	Items       string
	PrimaryDoc  string
	Description string
}

// IsEarnings8K reports whether an 8-K row reports results (Item 2.02).
func (s SubFiling) IsEarnings8K() bool {
	return s.Form == "8-K" && slices.Contains(strings.Split(s.Items, ","), "2.02")
}

// TickerMap returns ticker -> CIK for all SEC-registered companies.
func (e *EDGAR) TickerMap(ctx context.Context) (map[string]int, error) {
	body, err := e.HTTP.Get(ctx, e.www()+"/files/company_tickers.json")
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		CIK    int    `json:"cik_str"`
		Ticker string `json:"ticker"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("company_tickers: %w", err)
	}
	out := make(map[string]int, len(raw))
	for _, r := range raw {
		out[strings.ToUpper(r.Ticker)] = r.CIK
	}
	return out, nil
}

type columns struct {
	AccessionNumber []string `json:"accessionNumber"`
	FilingDate      []string `json:"filingDate"`
	Form            []string `json:"form"`
	Items           []string `json:"items"`
	PrimaryDocument []string `json:"primaryDocument"`
	PrimaryDocDesc  []string `json:"primaryDocDescription"`
}

func (c columns) rows() []SubFiling {
	out := make([]SubFiling, 0, len(c.AccessionNumber))
	at := func(s []string, i int) string {
		if i < len(s) {
			return s[i]
		}
		return ""
	}
	for i, acc := range c.AccessionNumber {
		d, err := time.ParseInLocation("2006-01-02", at(c.FilingDate, i), time.Local)
		if err != nil {
			continue
		}
		out = append(out, SubFiling{
			Accession:   acc,
			Form:        at(c.Form, i),
			Filed:       d,
			Items:       at(c.Items, i),
			PrimaryDoc:  at(c.PrimaryDocument, i),
			Description: at(c.PrimaryDocDesc, i),
		})
	}
	return out
}

// Submissions returns the company's filings filed on or after since, newest
// first. Older history pages are fetched only as far back as needed (at most
// maxPages extra pages).
func (e *EDGAR) Submissions(ctx context.Context, cik int, since time.Time) ([]SubFiling, error) {
	const maxPages = 10
	body, err := e.HTTP.Get(ctx, fmt.Sprintf("%s/submissions/CIK%010d.json", e.data(), cik))
	if err != nil {
		return nil, err
	}
	var resp struct {
		Filings struct {
			Recent columns `json:"recent"`
			Files  []struct {
				Name       string `json:"name"`
				FilingFrom string `json:"filingFrom"`
				FilingTo   string `json:"filingTo"`
			} `json:"files"`
		} `json:"filings"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("submissions %d: %w", cik, err)
	}
	all := resp.Filings.Recent.rows()
	for i, f := range resp.Filings.Files {
		if i >= maxPages {
			break
		}
		to, err := time.ParseInLocation("2006-01-02", f.FilingTo, time.Local)
		if err != nil || to.Before(since) {
			continue
		}
		page, err := e.HTTP.Get(ctx, e.data()+"/submissions/"+f.Name)
		if err != nil {
			return nil, err
		}
		var cols columns
		if err := json.Unmarshal(page, &cols); err != nil {
			return nil, fmt.Errorf("submissions page %s: %w", f.Name, err)
		}
		all = append(all, cols.rows()...)
	}
	var out []SubFiling
	for _, f := range all {
		if !f.Filed.Before(since) {
			out = append(out, f)
		}
	}
	slices.SortFunc(out, func(a, b SubFiling) int { return b.Filed.Compare(a.Filed) })
	return out, nil
}

// FilingDir returns the archive directory URL for a filing.
func (e *EDGAR) FilingDir(cik int, accession string) string {
	return fmt.Sprintf("%s/Archives/edgar/data/%d/%s", e.www(), cik, strings.ReplaceAll(accession, "-", ""))
}

// ExhibitURL finds the earnings press release in a filing: the EX-99.1
// exhibit, else the first EX-99.x, else the primary document.
func (e *EDGAR) ExhibitURL(ctx context.Context, cik int, accession, primaryDoc string) (string, error) {
	dir := e.FilingDir(cik, accession)
	body, err := e.HTTP.Get(ctx, dir+"/"+accession+"-index.htm")
	if err != nil {
		return "", err
	}
	href := PickExhibit(body)
	if href == "" {
		if primaryDoc == "" {
			return "", fmt.Errorf("filing %s: no exhibit or primary document", accession)
		}
		return dir + "/" + primaryDoc, nil
	}
	href = strings.TrimPrefix(href, "/ix?doc=")
	if strings.HasPrefix(href, "/") {
		return e.www() + href, nil
	}
	if strings.HasPrefix(href, "http") {
		return href, nil
	}
	return dir + "/" + href, nil
}

// PickExhibit returns the href of the best press-release exhibit in a filing
// index page, or "".
func PickExhibit(indexHTML []byte) string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(indexHTML))
	if err != nil {
		return ""
	}
	var first99, exact string
	doc.Find("table.tableFile tr").Each(func(_ int, tr *goquery.Selection) {
		cells := tr.Find("td")
		if cells.Length() < 4 {
			return
		}
		typ := strings.ToUpper(strings.TrimSpace(cells.Eq(3).Text()))
		href, _ := cells.Eq(2).Find("a").Attr("href")
		if href == "" {
			return
		}
		switch {
		case typ == "EX-99.1" && exact == "":
			exact = href
		case strings.HasPrefix(typ, "EX-99") && first99 == "":
			first99 = href
		}
	})
	if exact != "" {
		return exact
	}
	return first99
}

// DocumentText fetches a filing document and returns its plain text.
func (e *EDGAR) DocumentText(ctx context.Context, url string) (string, error) {
	body, err := e.HTTP.Get(ctx, url)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(strings.ToLower(url), ".txt") {
		return string(body), nil
	}
	return HTMLText(body, ""), nil
}

// ParseCIK parses a CIK string such as "0000019617".
func ParseCIK(s string) (int, error) { return strconv.Atoi(strings.TrimLeft(s, "0")) }
