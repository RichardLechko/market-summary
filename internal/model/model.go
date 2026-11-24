// Package model holds data types shared across packages.
package model

import "time"

// ReleaseDate is a scheduled FRED release.
type ReleaseDate struct {
	ReleaseID int
	Name      string
	Date      time.Time
}

// FedDoc is a Federal Reserve press release.
type FedDoc struct {
	Link      string
	Title     string
	Kind      string // statement, minutes, beige_book, other
	Published time.Time
	Body      string
	Summary   string
}

// Filing is an SEC filing for a watchlist company.
type Filing struct {
	Accession string
	Ticker    string
	CIK       int
	Form      string
	Filed     time.Time
	Items     string
	DocURL    string
	Facts     *EarningsFacts // nil until extracted
	Reported  bool           // included in a sent digest
}

// EarningsFacts are figures and prose extracted from an earnings release.
// Figures are as reported in the release; growth rates are computed in code.
type EarningsFacts struct {
	IsEarningsRelease bool     `json:"is_earnings_release"`
	Company           string   `json:"company"`
	Period            string   `json:"period"`
	Unit              string   `json:"unit"`
	Revenue           *float64 `json:"revenue"`
	RevenueYearAgo    *float64 `json:"revenue_year_ago"`
	EPSDiluted        *float64 `json:"eps_diluted"`
	EPSDilutedYearAgo *float64 `json:"eps_diluted_year_ago"`
	NetIncome         *float64 `json:"net_income"`
	NetIncomeYearAgo  *float64 `json:"net_income_year_ago"`
	Guidance          string   `json:"guidance"`
	GuidanceNote      string   `json:"guidance_note"`
	Headline          string   `json:"headline"`
	Summary           string   `json:"summary"`
}

// CallNote is the summary of one earnings call, tied to the earnings release
// it discusses.
type CallNote struct {
	Accession string // the earnings release's accession number
	Ticker    string
	Quarter   string // provider quarter label, e.g. 2026Q3
	Period    string // the release's period label, e.g. Q3 FY2027
	Filed     time.Time
	Notes     CallNotes
	Reported  bool // included in a sent digest
}

// CallNotes is what the model takes from an earnings call transcript.
type CallNotes struct {
	MatchesPeriod bool        `json:"matches_period"`
	Tone          string      `json:"tone"`
	Themes        []CallTheme `json:"themes"`
	AnalystFocus  []string    `json:"analyst_focus"`
	Outlook       string      `json:"outlook"`
}

// CallTheme is one topic management spent time on.
type CallTheme struct {
	Topic  string `json:"topic"`
	Detail string `json:"detail"`
}

// CompanyEvent is a non-earnings 8-K, such as a leadership change or a
// completed acquisition.
type CompanyEvent struct {
	Accession string
	Ticker    string
	Form      string
	Filed     time.Time
	Items     string // 8-K item numbers, e.g. "5.02,9.01"
	DocURL    string
	News      *EventNews // nil until summarized
	Reported  bool       // included in a sent digest
}

// EventNews is what the model takes from an event filing.
type EventNews struct {
	Company  string `json:"company"`
	Kind     string `json:"kind"`    // short label, e.g. "Share buyback"
	Routine  bool   `json:"routine"` // not worth showing, e.g. a routine pay plan
	Headline string `json:"headline"`
	Summary  string `json:"summary"`
}

// InsiderFiling is one Form 4 for a watchlist company. Trades holds only
// open-market purchases and sales; it is empty for filings with neither or
// for filings about another company.
type InsiderFiling struct {
	Accession string
	Ticker    string
	Filed     time.Time
	Trades    []InsiderTrade
	Reported  bool
}

// InsiderTrade is one open-market purchase or sale reported on Form 4.
type InsiderTrade struct {
	Owner      string    `json:"owner"`
	Role       string    `json:"role"` // e.g. "President and CEO", "Director"
	Date       time.Time `json:"date"`
	Code       string    `json:"code"` // P purchase, S sale
	Shares     float64   `json:"shares"`
	Price      float64   `json:"price"`
	OwnedAfter float64   `json:"owned_after"`
	Account    string    `json:"account"` // direct, or the indirect holder, e.g. "By Trust"
	Plan       bool      `json:"plan"`    // made under a pre-arranged Rule 10b5-1 plan
}

// ShortInterest is one twice-monthly FINRA short interest report for a stock.
type ShortInterest struct {
	Symbol      string
	Settlement  time.Time // the date the position was measured
	Short       float64   // shares sold short and not yet bought back
	AvgVolume   float64   // average daily shares traded
	DaysToCover float64   // Short / AvgVolume, as FINRA reports it
}
