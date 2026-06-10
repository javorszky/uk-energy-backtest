package octopus

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/javorszky/uk-energy-backtest/internal/costing"
)

// slotDuration is the metering and rate-publication granularity.
const (
	slotDuration   = 30 * time.Minute
	slotMinutes    = 30
	minutesPerHour = 60
)

// TariffCodes holds the user's current tariff code per stream. Export and
// Gas are empty when the account has no such meter or no live agreement.
type TariffCodes struct {
	Import string
	Export string
	Gas    string
}

// CurrentTariffCodes resolves the account's live agreements. "Current" means
// valid_from <= now and (valid_to absent or in the future); with several
// candidates the most recently started wins.
func (c *Client) CurrentTariffCodes(ctx context.Context, apiKey, account string, now time.Time) (TariffCodes, error) {
	if !segmentRe.MatchString(account) {
		return TariffCodes{}, fmt.Errorf("invalid account number %q", account)
	}
	u, err := url.JoinPath(c.base, "accounts", account)
	if err != nil {
		return TariffCodes{}, fmt.Errorf("build account url: %w", err)
	}
	var acc accountResponse
	if err := c.getJSON(ctx, apiKey, u+"/", &acc); err != nil {
		return TariffCodes{}, fmt.Errorf("fetch account: %w", err)
	}

	codes := acc.currentTariffCodes(now)
	if codes.Import == "" {
		return TariffCodes{}, fmt.Errorf("account %s has no current import tariff agreement", account)
	}
	return codes, nil
}

func (acc accountResponse) currentTariffCodes(now time.Time) TariffCodes {
	var codes TariffCodes
	for _, prop := range acc.Properties {
		for _, emp := range prop.ElectricityMeterPoints {
			code := currentAgreement(emp.Agreements, now)
			if code == "" {
				continue
			}
			if emp.IsExport {
				codes.Export = code
			} else {
				codes.Import = code
			}
		}
		for _, gmp := range prop.GasMeterPoints {
			if code := currentAgreement(gmp.Agreements, now); code != "" {
				codes.Gas = code
			}
		}
	}
	return codes
}

func currentAgreement(agreements []agreement, now time.Time) string {
	code := ""
	var bestFrom time.Time
	for _, a := range agreements {
		if a.ValidFrom.After(now) {
			continue
		}
		if a.ValidTo != nil && !a.ValidTo.After(now) {
			continue
		}
		if code == "" || a.ValidFrom.After(bestFrom) {
			code, bestFrom = a.TariffCode, a.ValidFrom
		}
	}
	return code
}

// minTariffCodeParts: <fuel>-<meterType>-<product…>-<region>, e.g.
// E-1R-FLUX-IMPORT-23-02-14-C.
const minTariffCodeParts = 4

// ProductCode derives the product code from a tariff code by stripping the
// fuel/meter-type prefix (E-1R-, E-2R-, G-1R-) and the trailing region
// letter: E-1R-VAR-19-04-12-N → VAR-19-04-12.
func ProductCode(tariffCode string) (string, error) {
	parts := strings.Split(tariffCode, "-")
	if len(parts) < minTariffCodeParts || (parts[0] != "E" && parts[0] != "G") {
		return "", fmt.Errorf("unrecognised tariff code %q", tariffCode)
	}
	return strings.Join(parts[2:len(parts)-1], "-"), nil
}

// rateEntry is one priced interval from a standing-charges or unit-rates
// endpoint. ValidTo nil = open-ended. PaymentMethod nil = applies to all.
type rateEntry struct {
	ValidFrom     time.Time  `json:"valid_from"`
	ValidTo       *time.Time `json:"valid_to"`
	PaymentMethod *string    `json:"payment_method"`
	ValueIncVAT   float64    `json:"value_inc_vat"`
}

type ratesResponse struct {
	Results []rateEntry `json:"results"`
}

// ratesURL builds a product-rates endpoint URL. These endpoints are public,
// but the same hardcoded base and segment validation apply (SSRF lockdown).
func (c *Client) ratesURL(tariffCode, leaf string, gas bool) (string, error) {
	if !segmentRe.MatchString(tariffCode) {
		return "", fmt.Errorf("invalid tariff code %q", tariffCode)
	}
	product, err := ProductCode(tariffCode)
	if err != nil {
		return "", err
	}
	kind := "electricity-tariffs"
	if gas {
		kind = "gas-tariffs"
	}
	u, err := url.JoinPath(c.base, "products", product, kind, tariffCode, leaf)
	if err != nil {
		return "", fmt.Errorf("build rates url: %w", err)
	}
	return u + "/", nil
}

// covering returns the entry in force at instant t, preferring the
// direct-debit price when both payment-method variants are published (the
// direct-debit rate is what the overwhelming majority of customers pay).
func covering(entries []rateEntry, t time.Time) (rateEntry, bool) {
	var found rateEntry
	ok := false
	for _, e := range entries {
		if e.ValidFrom.After(t) {
			continue
		}
		if e.ValidTo != nil && !e.ValidTo.After(t) {
			continue
		}
		dd := e.PaymentMethod != nil && *e.PaymentMethod == "DIRECT_DEBIT"
		if !ok || dd {
			found, ok = e, true
		}
	}
	return found, ok
}

func (c *Client) fetchRates(ctx context.Context, apiKey, tariffCode, leaf string, gas bool, from, to time.Time) ([]rateEntry, error) {
	base, err := c.ratesURL(tariffCode, leaf, gas)
	if err != nil {
		return nil, err
	}
	query := url.Values{
		"period_from": {from.UTC().Format(timestampLayout)},
		"period_to":   {to.UTC().Format(timestampLayout)},
		"page_size":   {"1500"},
	}
	var resp ratesResponse
	if err := c.getJSON(ctx, apiKey, base+"?"+query.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("fetch %s for %s: %w", leaf, tariffCode, err)
	}
	return resp.Results, nil
}

// CurrentStandingCharge returns the standing charge (pence/day, VAT
// inclusive) in force now for the tariff.
func (c *Client) CurrentStandingCharge(ctx context.Context, apiKey, tariffCode string, gas bool, now time.Time) (float64, error) {
	entries, err := c.fetchRates(ctx, apiKey, tariffCode, "standing-charges", gas, now.Add(-24*time.Hour), now)
	if err != nil {
		return 0, err
	}
	e, ok := covering(entries, now.Add(-time.Minute))
	if !ok {
		return 0, fmt.Errorf("no current standing charge published for %s", tariffCode)
	}
	return e.ValueIncVAT, nil
}

// CurrentGasUnitRate returns the flat gas unit rate (pence/kWh) in force now.
func (c *Client) CurrentGasUnitRate(ctx context.Context, apiKey, tariffCode string, now time.Time) (float64, error) {
	entries, err := c.fetchRates(ctx, apiKey, tariffCode, "standard-unit-rates", true, now.Add(-24*time.Hour), now)
	if err != nil {
		return 0, err
	}
	e, ok := covering(entries, now.Add(-time.Minute))
	if !ok {
		return 0, fmt.Errorf("no current unit rate published for %s", tariffCode)
	}
	return e.ValueIncVAT, nil
}

// UnitRateBuckets resolves an electricity tariff's unit rates over the last
// 24 hours into the 48 local half-hour buckets. For flat and time-of-use
// tariffs (Flux, Go, Economy 7 single-register codes) the published rate
// intervals repeat daily, so one day's sweep reconstructs the band shape;
// for dynamic tariffs (Agile) the caller detects the many distinct values
// and falls back to an average.
func (c *Client) UnitRateBuckets(ctx context.Context, apiKey, tariffCode string, now time.Time, loc *time.Location) (*[costing.BucketsPerDay]float64, error) {
	// Sweep 26 hours, not 24: on a DST-change day a plain 24h window leaves
	// up to two local buckets unvisited (the spring-forward gap shifts which
	// local half-hours the UTC steps land in).
	const sweepSteps = 52
	from := now.Add(-time.Duration(sweepSteps+1) * slotDuration)
	entries, err := c.fetchRates(ctx, apiKey, tariffCode, "standard-unit-rates", false, from, now)
	if err != nil {
		return nil, err
	}

	var rates [costing.BucketsPerDay]float64
	filled := [costing.BucketsPerDay]bool{}
	// Each half-hour step lands in exactly one local bucket; the first
	// (most recent) hit wins.
	slot := now.Truncate(slotDuration)
	for i := 0; i < sweepSteps; i++ {
		t := slot.Add(time.Duration(-i) * slotDuration)
		local := t.In(loc)
		b := (local.Hour()*minutesPerHour + local.Minute()) / slotMinutes
		if filled[b] {
			continue
		}
		e, ok := covering(entries, t)
		if !ok {
			return nil, fmt.Errorf("no published unit rate covering %s for %s", t.UTC().Format(time.RFC3339), tariffCode)
		}
		rates[b] = e.ValueIncVAT
		filled[b] = true
	}
	for b, ok := range filled {
		if !ok {
			return nil, fmt.Errorf("could not resolve a rate for local bucket %d of %s", b, tariffCode)
		}
	}
	return &rates, nil
}
