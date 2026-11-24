package collect

import (
	"encoding/xml"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RichardLechko/market-summary/internal/model"
)

// RawDocURL returns the URL of a filing's raw primary document. For XML
// forms EDGAR lists a rendered view such as "xslF345X06/primary_doc.xml";
// the raw XML sits at the same name in the filing directory.
func (e *EDGAR) RawDocURL(cik int, accession, primaryDoc string) string {
	doc := primaryDoc
	if strings.HasPrefix(doc, "xsl") {
		doc = path.Base(doc)
	}
	return e.FilingDir(cik, accession) + "/" + doc
}

type xmlValue struct {
	Value string `xml:"value"`
}

type form4XML struct {
	Issuer struct {
		CIK string `xml:"issuerCik"`
	} `xml:"issuer"`
	Owners []struct {
		Name string `xml:"reportingOwnerId>rptOwnerName"`
		Rel  struct {
			Director     string `xml:"isDirector"`
			Officer      string `xml:"isOfficer"`
			TenPercent   string `xml:"isTenPercentOwner"`
			OfficerTitle string `xml:"officerTitle"`
		} `xml:"reportingOwnerRelationship"`
	} `xml:"reportingOwner"`
	Aff10b5One string `xml:"aff10b5One"`
	Trans      []struct {
		Date   xmlValue `xml:"transactionDate"`
		Code   string   `xml:"transactionCoding>transactionCode"`
		Shares xmlValue `xml:"transactionAmounts>transactionShares"`
		Price  xmlValue `xml:"transactionAmounts>transactionPricePerShare"`
		After  xmlValue `xml:"postTransactionAmounts>sharesOwnedFollowingTransaction"`
		Direct xmlValue `xml:"ownershipNature>directOrIndirectOwnership"`
		Nature xmlValue `xml:"ownershipNature>natureOfOwnership"`
	} `xml:"nonDerivativeTable>nonDerivativeTransaction"`
	Footnotes []string `xml:"footnotes>footnote"`
	Remarks   string   `xml:"remarks"`
}

// ParseForm4 returns the issuer CIK of a Form 4 and its open-market
// purchases (P) and sales (S). Grants, option exercises, tax withholding and
// gifts are left out.
func ParseForm4(b []byte) (int, []model.InsiderTrade, error) {
	var f form4XML
	if err := xml.Unmarshal(b, &f); err != nil {
		return 0, nil, fmt.Errorf("form 4: %w", err)
	}
	cik, err := ParseCIK(strings.TrimSpace(f.Issuer.CIK))
	if err != nil {
		return 0, nil, fmt.Errorf("form 4 issuer cik %q: %w", f.Issuer.CIK, err)
	}
	if len(f.Owners) == 0 {
		return cik, nil, nil
	}
	o := f.Owners[0]
	var roles []string
	title := strings.TrimSpace(o.Rel.OfficerTitle)
	if strings.EqualFold(title, "See Remarks") {
		title = remarksTitle(f.Remarks)
	}
	if title != "" && isTrue(o.Rel.Officer) {
		roles = append(roles, title)
	} else if isTrue(o.Rel.Officer) {
		roles = append(roles, "Officer")
	}
	if isTrue(o.Rel.Director) {
		roles = append(roles, "Director")
	}
	if isTrue(o.Rel.TenPercent) {
		roles = append(roles, "10% owner")
	}
	plan := isTrue(f.Aff10b5One)
	if !plan {
		for _, fn := range f.Footnotes {
			if strings.Contains(fn, "10b5-1") {
				plan = true
				break
			}
		}
	}
	var out []model.InsiderTrade
	for _, t := range f.Trans {
		code := strings.TrimSpace(t.Code)
		if code != "P" && code != "S" {
			continue
		}
		date, _ := time.ParseInLocation("2006-01-02", strings.TrimSpace(t.Date.Value), time.Local)
		account := "Direct"
		if strings.TrimSpace(t.Direct.Value) == "I" {
			account = strings.TrimSpace(t.Nature.Value)
			if account == "" {
				account = "Indirect"
			}
		}
		if isPlanPurchase(account) {
			continue // automatic employee stock purchase plan buys
		}
		out = append(out, model.InsiderTrade{
			Owner: strings.TrimSpace(o.Name), Role: strings.Join(roles, ", "), Date: date, Code: code,
			Shares: num(t.Shares.Value), Price: num(t.Price.Value), OwnedAfter: num(t.After.Value),
			Account: account, Plan: plan,
		})
	}
	return cik, out, nil
}

// Schedule13DIssuer returns the subject company CIK of a Schedule 13D.
func Schedule13DIssuer(b []byte) (int, error) {
	var f struct {
		CIK string `xml:"formData>coverPageHeader>issuerInfo>issuerCIK"`
	}
	if err := xml.Unmarshal(b, &f); err != nil {
		return 0, fmt.Errorf("schedule 13d: %w", err)
	}
	return ParseCIK(strings.TrimSpace(f.CIK))
}

func isTrue(s string) bool {
	s = strings.TrimSpace(s)
	return s == "1" || strings.EqualFold(s, "true")
}

func num(s string) float64 {
	v, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s), ",", ""), 64)
	return v
}

func isPlanPurchase(account string) bool {
	a := strings.ToLower(account)
	return strings.Contains(a, "espp") || strings.Contains(a, "stock purchase plan")
}

var officerTitleRe = regexp.MustCompile(`(?i)officer title:\s*([^.;]+)`)

// remarksTitle finds the officer title in a Form 4's remarks, used when the
// title field says "See Remarks".
func remarksTitle(remarks string) string {
	r := strings.TrimSpace(remarks)
	if m := officerTitleRe.FindStringSubmatch(r); m != nil {
		return strings.TrimSpace(m[1])
	}
	if len(r) <= 60 {
		return strings.TrimSuffix(r, ".")
	}
	return ""
}
