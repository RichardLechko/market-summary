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
