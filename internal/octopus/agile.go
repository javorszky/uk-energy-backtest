package octopus

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"time"
)

// RatePoint is one published price interval, flattened for the frontend.
// To is nil for an open-ended interval (the rate currently in force).
type RatePoint struct {
	From time.Time  `json:"from"`
	To   *time.Time `json:"to"`
	Rate float64    `json:"rate"`
}

// regionRe matches the GSP region letter at the end of a tariff code.
// Regions run A–P with I and O unused.
var regionRe = regexp.MustCompile(`^[A-HJ-NP]$`)

// ValidRegion reports whether s is a single GSP region letter, so handlers
// can reject bad input as 400 before any upstream call.
func ValidRegion(s string) bool { return regionRe.MatchString(s) }

// ValidProduct reports whether s is a plausible product code segment.
func ValidProduct(s string) bool { return segmentRe.MatchString(s) }

// maxRatePages bounds the pagination loop: at 1500 rows/page, 32 pages is
// ~2.7 years of half-hourly Agile prices — beyond the 732-day request cap.
const maxRatePages = 32

// ratePageSize is the documented maximum page size for price endpoints.
const ratePageSize = "1500"

// Rate endpoint leaves this client supports.
const (
	LeafUnitRates       = "standard-unit-rates"
	LeafStandingCharges = "standing-charges"
)

// RateSeries fetches the published rate intervals for one electricity
// tariff (built as E-1R-{product}-{region}) between from and to, following
// pagination. leaf selects "standard-unit-rates" or "standing-charges".
// These endpoints are public — no credential is sent — but the same
// hardcoded base, segment validation, and same-host pagination rules apply.
// Where both payment-method variants exist for an interval, the
// direct-debit price is kept.
func (c *Client) RateSeries(ctx context.Context, product, region, leaf string, from, to time.Time) ([]RatePoint, error) {
	base, err := rateSeriesURL(c.base, product, region, leaf)
	if err != nil {
		return nil, err
	}

	query := url.Values{
		paramPeriodFrom: {from.UTC().Format(timestampLayout)},
		paramPeriodTo:   {to.UTC().Format(timestampLayout)},
		paramPageSize:   {ratePageSize},
	}

	var entries []rateEntry
	for page := 0; ; page++ {
		if page >= maxRatePages {
			return nil, fmt.Errorf("rate pagination exceeded %d pages", maxRatePages)
		}
		var resp paginatedRatesResponse
		if err := c.getJSON(ctx, "", base+"?"+query.Encode(), &resp); err != nil {
			return nil, fmt.Errorf("fetch %s page %d for %s-%s: %w", leaf, page, product, region, err)
		}
		entries = append(entries, resp.Results...)
		if resp.Next == "" {
			break
		}
		// Same SSRF guard as consumption: never follow the upstream URL,
		// reuse only its query parameters against our own base.
		nextURL, err := url.Parse(resp.Next)
		if err != nil {
			return nil, fmt.Errorf("parse next page url: %w", err)
		}
		query = nextURL.Query()
	}

	return dedupeRatePoints(entries), nil
}

// rateSeriesURL validates the inputs and builds the endpoint URL.
func rateSeriesURL(apiBase, product, region, leaf string) (string, error) {
	if !segmentRe.MatchString(product) {
		return "", fmt.Errorf("invalid product code %q", product)
	}
	if !regionRe.MatchString(region) {
		return "", fmt.Errorf("invalid region %q: want a single GSP letter A-P", region)
	}
	if leaf != LeafUnitRates && leaf != LeafStandingCharges {
		return "", fmt.Errorf("invalid rate kind %q", leaf)
	}

	tariffCode := "E-1R-" + product + "-" + region
	u, err := url.JoinPath(apiBase, "products", product, "electricity-tariffs", tariffCode, leaf)
	if err != nil {
		return "", fmt.Errorf("build rates url: %w", err)
	}
	return u + "/", nil
}

// paginatedRatesResponse is ratesResponse plus the pagination cursor.
type paginatedRatesResponse struct {
	Next    string      `json:"next"`
	Results []rateEntry `json:"results"`
}

// dedupeRatePoints flattens entries to RatePoints, preferring the
// direct-debit variant when two entries share an interval start.
func dedupeRatePoints(entries []rateEntry) []RatePoint {
	type slot struct {
		entry rateEntry
		dd    bool
	}
	byStart := make(map[int64]slot, len(entries))
	order := make([]int64, 0, len(entries))
	for _, e := range entries {
		key := e.ValidFrom.UnixMilli()
		dd := e.PaymentMethod != nil && *e.PaymentMethod == "DIRECT_DEBIT"
		existing, seen := byStart[key]
		if !seen {
			order = append(order, key)
		}
		if !seen || (dd && !existing.dd) {
			byStart[key] = slot{entry: e, dd: dd}
		}
	}

	points := make([]RatePoint, 0, len(order))
	for _, key := range order {
		e := byStart[key].entry
		points = append(points, RatePoint{From: e.ValidFrom, To: e.ValidTo, Rate: e.ValueIncVAT})
	}
	return points
}
