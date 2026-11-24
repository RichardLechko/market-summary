package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/RichardLechko/market-summary/internal/httpx"
	"github.com/RichardLechko/market-summary/internal/model"
)

// FINRA reads FINRA's public short interest data. No account is needed.
type FINRA struct {
	HTTP    *httpx.Client
	BaseURL string // default https://api.finra.org
}

const finraPage = 5000

// ShortInterest returns the reports for symbols with settlement dates in
// [from, to], in one request per 5,000 rows.
func (f *FINRA) ShortInterest(ctx context.Context, symbols []string, from, to time.Time) ([]model.ShortInterest, error) {
	base := f.BaseURL
	if base == "" {
		base = "https://api.finra.org"
	}
	url := base + "/data/group/otcMarket/name/consolidatedShortInterest"
	var out []model.ShortInterest
	for offset := 0; ; offset += finraPage {
		req, err := json.Marshal(map[string]any{
			"limit":         finraPage,
			"offset":        offset,
			"fields":        []string{"symbolCode", "settlementDate", "currentShortPositionQuantity", "averageDailyVolumeQuantity", "daysToCoverQuantity"},
			"domainFilters": []map[string]any{{"fieldName": "symbolCode", "values": symbols}},
			"dateRangeFilters": []map[string]any{{
				"fieldName": "settlementDate", "startDate": from.Format("2006-01-02"), "endDate": to.Format("2006-01-02"),
			}},
		})
		if err != nil {
			return nil, err
		}
		body, err := f.HTTP.PostJSON(ctx, url, req)
		if err != nil {
			return nil, err
		}
		var rows []struct {
			Symbol string   `json:"symbolCode"`
			Date   string   `json:"settlementDate"`
			Short  float64  `json:"currentShortPositionQuantity"`
			Volume float64  `json:"averageDailyVolumeQuantity"`
			Days   *float64 `json:"daysToCoverQuantity"`
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &rows); err != nil {
				return nil, fmt.Errorf("finra short interest: %w", err)
			}
		}
		for _, r := range rows {
			d, err := time.ParseInLocation("2006-01-02", r.Date, time.Local)
			if err != nil {
				continue
			}
			s := model.ShortInterest{Symbol: r.Symbol, Settlement: d, Short: r.Short, AvgVolume: r.Volume}
			switch {
			case r.Days != nil:
				s.DaysToCover = *r.Days
			case r.Volume > 0:
				s.DaysToCover = r.Short / r.Volume
			}
			out = append(out, s)
		}
		if len(rows) < finraPage {
			return out, nil
		}
	}
}
