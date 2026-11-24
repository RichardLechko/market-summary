package analysis

import (
	"math"
	"slices"
	"time"

	"github.com/RichardLechko/market-summary/internal/model"
)

// Thresholds for showing insider trades. Purchases with one's own money are
// rare and telling, so the bar is low. Sales are common (pay is often in
// stock), so only large ones are shown.
const (
	insiderBuyMin       = 100_000
	insiderSellMin      = 10_000_000
	insiderSellShareMin = 20 // percent of holdings
	insiderSellShareVal = 1_000_000
)

// InsiderRow is one person's purchases or sales in a company over the window.
type InsiderRow struct {
	Ticker   string
	Owner    string
	Role     string
	Buy      bool
	Trades   int
	Shares   float64
	Value    float64 // shares times price, summed
	AvgPrice float64
	After    float64 // shares held afterwards in the accounts used
	// PctOfHoldings is the share of those holdings bought or sold, or NaN.
	PctOfHoldings float64
	Plan          bool // every trade was under a pre-arranged 10b5-1 plan
	First, Last   time.Time
}

// Notable reports whether the row clears the thresholds for showing.
func (r InsiderRow) Notable() bool {
	if r.Buy {
		return r.Value >= insiderBuyMin
	}
	return r.Value >= insiderSellMin ||
		(!math.IsNaN(r.PctOfHoldings) && r.PctOfHoldings >= insiderSellShareMin && r.Value >= insiderSellShareVal)
}

// InsiderRows groups trades by company, person and direction, largest first.
func InsiderRows(filings []model.InsiderFiling) []InsiderRow {
	type key struct{ ticker, owner, code string }
	type last struct {
		date  time.Time
		after float64
	}
	rows := map[key]*InsiderRow{}
	accounts := map[key]map[string]last{}
	var order []key
	for _, f := range filings {
		for _, t := range f.Trades {
			k := key{f.Ticker, t.Owner, t.Code}
			r, ok := rows[k]
			if !ok {
				r = &InsiderRow{Ticker: f.Ticker, Owner: t.Owner, Role: t.Role, Buy: t.Code == "P", Plan: true, First: t.Date, Last: t.Date}
				rows[k] = r
				accounts[k] = map[string]last{}
				order = append(order, k)
			}
			r.Trades++
			r.Shares += t.Shares
			r.Value += t.Shares * t.Price
			r.Plan = r.Plan && t.Plan
			if t.Date.Before(r.First) {
				r.First = t.Date
			}
			if t.Date.After(r.Last) {
				r.Last = t.Date
			}
			if prev, ok := accounts[k][t.Account]; !ok || !t.Date.Before(prev.date) {
				accounts[k][t.Account] = last{t.Date, t.OwnedAfter}
			}
		}
	}
	out := make([]InsiderRow, 0, len(order))
	for _, k := range order {
		r := rows[k]
		for _, a := range accounts[k] {
			r.After += a.after
		}
		if r.Shares > 0 {
			r.AvgPrice = r.Value / r.Shares
		}
		r.PctOfHoldings = math.NaN()
		if r.Buy && r.After > 0 {
			r.PctOfHoldings = r.Shares / r.After * 100
		} else if !r.Buy && r.Shares+r.After > 0 {
			r.PctOfHoldings = r.Shares / (r.Shares + r.After) * 100
		}
		out = append(out, *r)
	}
	slices.SortStableFunc(out, func(a, b InsiderRow) int {
		if a.Buy != b.Buy {
			if a.Buy {
				return -1
			}
			return 1
		}
		switch {
		case a.Value > b.Value:
			return -1
		case a.Value < b.Value:
			return 1
		}
		return 0
	})
	return out
}

// ClusterDays and clusterBuyers define a buying cluster: at least
// clusterBuyers different insiders buying the same company's shares on the
// open market within ClusterDays.
const (
	ClusterDays   = 30
	clusterBuyers = 3
	// clusterMinBuy is the least a buyer must spend to count, so automatic
	// plan purchases and token buys do not make a cluster.
	clusterMinBuy = 25_000
)

// InsiderCluster is several insiders buying the same company's shares.
type InsiderCluster struct {
	Ticker      string
	Buyers      []string // in order of first purchase
	Value       float64
	First, Last time.Time
}

// InsiderClusters finds companies where at least three insiders bought
// shares in the given filings (the caller passes the last ClusterDays plus
// the window), and at least one of those purchases was filed on or after
// windowDay, so a cluster is reported when it forms or grows.
func InsiderClusters(filings []model.InsiderFiling, windowDay time.Time) []InsiderCluster {
	type acc struct {
		c      InsiderCluster
		spent  map[string]float64
		recent bool
	}
	by := map[string]*acc{}
	var order []string
	for _, f := range filings {
		for _, t := range f.Trades {
			if t.Code != "P" {
				continue
			}
			a, ok := by[f.Ticker]
			if !ok {
				a = &acc{c: InsiderCluster{Ticker: f.Ticker, First: t.Date, Last: t.Date}, spent: map[string]float64{}}
				by[f.Ticker] = a
				order = append(order, f.Ticker)
			}
			if _, ok := a.spent[t.Owner]; !ok {
				a.c.Buyers = append(a.c.Buyers, t.Owner)
			}
			a.spent[t.Owner] += t.Shares * t.Price
			a.c.Value += t.Shares * t.Price
			if t.Date.Before(a.c.First) {
				a.c.First = t.Date
			}
			if t.Date.After(a.c.Last) {
				a.c.Last = t.Date
			}
			if !f.Filed.Before(windowDay) {
				a.recent = true
			}
		}
	}
	var out []InsiderCluster
	for _, t := range order {
		a := by[t]
		var buyers []string
		for _, b := range a.c.Buyers {
			if a.spent[b] >= clusterMinBuy {
				buyers = append(buyers, b)
			}
		}
		if a.recent && len(buyers) >= clusterBuyers {
			a.c.Buyers = buyers
			out = append(out, a.c)
		}
	}
	return out
}
