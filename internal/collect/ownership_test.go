package collect

import "testing"

const form4Sample = `<ownershipDocument>
<issuer><issuerCik>0001321655</issuerCik></issuer>
<reportingOwner>
  <reportingOwnerId><rptOwnerName>Sankar Shyam</rptOwnerName></reportingOwnerId>
  <reportingOwnerRelationship><isOfficer>1</isOfficer><officerTitle>See Remarks</officerTitle></reportingOwnerRelationship>
</reportingOwner>
<nonDerivativeTable>
  <nonDerivativeTransaction>
    <transactionDate><value>2026-11-10</value></transactionDate>
    <transactionCoding><transactionCode>S</transactionCode></transactionCoding>
    <transactionAmounts><transactionShares><value>1,000</value></transactionShares><transactionPricePerShare><value>160.5</value></transactionPricePerShare></transactionAmounts>
    <postTransactionAmounts><sharesOwnedFollowingTransaction><value>9000</value></sharesOwnedFollowingTransaction></postTransactionAmounts>
    <ownershipNature><directOrIndirectOwnership><value>I</value></directOrIndirectOwnership><natureOfOwnership><value>By Trust</value></natureOfOwnership></ownershipNature>
  </nonDerivativeTransaction>
  <nonDerivativeTransaction>
    <transactionDate><value>2026-11-10</value></transactionDate>
    <transactionCoding><transactionCode>P</transactionCode></transactionCoding>
    <transactionAmounts><transactionShares><value>50</value></transactionShares><transactionPricePerShare><value>70</value></transactionPricePerShare></transactionAmounts>
    <ownershipNature><directOrIndirectOwnership><value>I</value></directOrIndirectOwnership><natureOfOwnership><value>By ESPP Trust</value></natureOfOwnership></ownershipNature>
  </nonDerivativeTransaction>
  <nonDerivativeTransaction>
    <transactionDate><value>2026-11-10</value></transactionDate>
    <transactionCoding><transactionCode>F</transactionCode></transactionCoding>
  </nonDerivativeTransaction>
</nonDerivativeTable>
<footnotes><footnote id="F1">Sold under a Rule 10b5-1 trading plan adopted May 1, 2026.</footnote></footnotes>
<remarks>Officer title: Chief Technology Officer. This Form 4 does not list all holdings.</remarks>
</ownershipDocument>`

func TestParseForm4(t *testing.T) {
	cik, trades, err := ParseForm4([]byte(form4Sample))
	if err != nil {
		t.Fatal(err)
	}
	if cik != 1321655 || len(trades) != 1 {
		t.Fatalf("cik = %d, trades = %+v", cik, trades)
	}
	tr := trades[0]
	if tr.Role != "Chief Technology Officer" || tr.Code != "S" || tr.Shares != 1000 || tr.Price != 160.5 ||
		tr.OwnedAfter != 9000 || tr.Account != "By Trust" || !tr.Plan {
		t.Errorf("trade = %+v", tr)
	}
}

func TestSchedule13DIssuer(t *testing.T) {
	cik, err := Schedule13DIssuer([]byte(`<edgarSubmission xmlns="http://www.sec.gov/edgar/schedule13D"><formData><coverPageHeader><issuerInfo><issuerCIK>0001774342</issuerCIK></issuerInfo></coverPageHeader></formData></edgarSubmission>`))
	if err != nil || cik != 1774342 {
		t.Errorf("cik = %d, err = %v", cik, err)
	}
}

func TestRawDocURL(t *testing.T) {
	e := &EDGAR{WWWBase: "https://x"}
	if got := e.RawDocURL(1, "0001-26-000002", "xslF345X06/primary_doc.xml"); got != "https://x/Archives/edgar/data/1/000126000002/primary_doc.xml" {
		t.Errorf("RawDocURL = %q", got)
	}
}
