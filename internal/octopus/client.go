// Package octopus is a minimal client for the Octopus Energy REST API,
// covering only what the backtester needs: account/meter discovery and
// half-hourly consumption. It is deliberately SSRF-locked: the upstream host
// is hardcoded, path segments are validated before interpolation, and
// pagination never follows a caller- or upstream-supplied absolute URL.
package octopus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/javorszky/uk-energy-backtest/internal/costing"
)

const (
	// defaultBaseURL is the only upstream this client will ever call. It must
	// not be configurable through any request-derived value (SSRF lockdown).
	defaultBaseURL = "https://api.octopus.energy/v1"

	// pageSize keeps pagination round-trips low; Octopus caps consumption
	// pages at 25000 rows.
	pageSize = "25000"

	// maxPages bounds the pagination loop. At 25000 rows/page this allows
	// ~14 years of half-hourly data — far beyond any sane request — while
	// guaranteeing termination if the upstream misbehaves.
	maxPages = 10

	// maxBodyBytes caps how much of an upstream response we will read.
	// A full 25000-row page is ~3 MB of JSON; 32 MB leaves ample headroom.
	maxBodyBytes = 32 * 1024 * 1024

	// timestampLayout formats period_from/period_to. The brief requires UTC
	// with a trailing Z; time.RFC3339 on a UTC time produces exactly that.
	timestampLayout = time.RFC3339
)

// segmentRe validates every path segment (account number, MPAN, MPRN, meter
// serial) before it is interpolated into an upstream URL. Octopus identifiers
// are alphanumeric with hyphens; anything else is rejected to keep
// caller-controlled input out of URL structure (gosec/CodeQL SSRF taint).
var segmentRe = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// Client calls the Octopus API. The zero value is not usable; construct with
// NewClient.
type Client struct {
	http *http.Client
	base string
}

// NewClient returns a Client with the production base URL. timeout applies
// per HTTP request (one page), not per logical operation.
func NewClient(timeout time.Duration) *Client {
	return &Client{
		http: &http.Client{Timeout: timeout},
		base: defaultBaseURL,
	}
}

// newClientWithBase exists for tests only, pointing the client at an
// httptest server. It is unexported so production code cannot redirect the
// client away from api.octopus.energy.
func newClientWithBase(base string) *Client {
	return &Client{
		http: &http.Client{Timeout: time.Second},
		base: base,
	}
}

// MeterPoints is the result of account discovery: the identifiers needed to
// fetch consumption for each stream. Export and gas fields are empty when the
// account has no such meter.
type MeterPoints struct {
	ImportMPAN   string
	ImportSerial string
	ExportMPAN   string
	ExportSerial string
	GasMPRN      string
	GasSerial    string
}

// accountResponse mirrors the subset of GET /accounts/{number}/ we need.
type accountResponse struct {
	Properties []struct {
		ElectricityMeterPoints []struct {
			MPAN   string `json:"mpan"`
			Meters []struct {
				SerialNumber string `json:"serial_number"`
			} `json:"meters"`
			IsExport bool `json:"is_export"`
		} `json:"electricity_meter_points"`
		GasMeterPoints []struct {
			MPRN   string `json:"mprn"`
			Meters []struct {
				SerialNumber string `json:"serial_number"`
			} `json:"meters"`
		} `json:"gas_meter_points"`
	} `json:"properties"`
}

// DiscoverMeters resolves an account number to its meter identifiers. The
// electricity point with is_export=true is the export meter. When a point has
// multiple meters (e.g. after a meter swap) the last serial is used, matching
// the most recently installed meter.
func (c *Client) DiscoverMeters(ctx context.Context, apiKey, account string) (MeterPoints, error) {
	if !segmentRe.MatchString(account) {
		return MeterPoints{}, fmt.Errorf("invalid account number %q", account)
	}

	u, err := url.JoinPath(c.base, "accounts", account)
	if err != nil {
		return MeterPoints{}, fmt.Errorf("build account url: %w", err)
	}

	var acc accountResponse
	if err := c.getJSON(ctx, apiKey, u+"/", &acc); err != nil {
		return MeterPoints{}, fmt.Errorf("fetch account: %w", err)
	}

	mp := acc.meterPoints()
	if mp.ImportMPAN == "" {
		return MeterPoints{}, fmt.Errorf("account %s has no import electricity meter", account)
	}
	return mp, nil
}

// meterPoints collapses the account payload to the identifiers we fetch with.
func (acc accountResponse) meterPoints() MeterPoints {
	var mp MeterPoints
	for _, prop := range acc.Properties {
		for _, emp := range prop.ElectricityMeterPoints {
			if len(emp.Meters) == 0 {
				continue
			}
			serial := emp.Meters[len(emp.Meters)-1].SerialNumber
			if emp.IsExport {
				mp.ExportMPAN, mp.ExportSerial = emp.MPAN, serial
			} else {
				mp.ImportMPAN, mp.ImportSerial = emp.MPAN, serial
			}
		}
		for _, gmp := range prop.GasMeterPoints {
			if len(gmp.Meters) == 0 {
				continue
			}
			mp.GasMPRN, mp.GasSerial = gmp.MPRN, gmp.Meters[len(gmp.Meters)-1].SerialNumber
		}
	}
	return mp
}

// consumptionResponse mirrors one page of a consumption endpoint.
type consumptionResponse struct {
	Next    string `json:"next"`
	Results []struct {
		IntervalStart time.Time `json:"interval_start"`
		Consumption   float64   `json:"consumption"`
	} `json:"results"`
}

// Consumption fetches half-hourly readings for one meter between from and to.
// gas selects the gas-meter-points endpoint (pointID is then an MPRN). The
// export MPAN reports exported energy under the same consumption field.
func (c *Client) Consumption(ctx context.Context, apiKey, pointID, serial string, from, to time.Time, gas bool) ([]costing.Reading, error) {
	base, err := c.consumptionURL(pointID, serial, gas)
	if err != nil {
		return nil, err
	}

	query := url.Values{
		"period_from": {from.UTC().Format(timestampLayout)},
		"period_to":   {to.UTC().Format(timestampLayout)},
		"page_size":   {pageSize},
		"order_by":    {"period"},
	}

	var readings []costing.Reading
	for page := 0; ; page++ {
		if page >= maxPages {
			return nil, fmt.Errorf("consumption pagination exceeded %d pages", maxPages)
		}

		var resp consumptionResponse
		if err := c.getJSON(ctx, apiKey, base+"?"+query.Encode(), &resp); err != nil {
			return nil, fmt.Errorf("fetch consumption page %d: %w", page, err)
		}
		for _, r := range resp.Results {
			readings = append(readings, costing.Reading{
				IntervalStart: r.IntervalStart,
				Consumption:   r.Consumption,
			})
		}

		if resp.Next == "" {
			return readings, nil
		}
		// Never follow the upstream-supplied URL itself: reuse only its
		// query parameters against our own validated base, so a hostile or
		// compromised response cannot redirect the client (SSRF guard).
		nextURL, err := url.Parse(resp.Next)
		if err != nil {
			return nil, fmt.Errorf("parse next page url: %w", err)
		}
		query = nextURL.Query()
	}
}

// consumptionURL validates the identifiers and builds the endpoint URL.
func (c *Client) consumptionURL(pointID, serial string, gas bool) (string, error) {
	if !segmentRe.MatchString(pointID) {
		return "", fmt.Errorf("invalid meter point id %q", pointID)
	}
	if !segmentRe.MatchString(serial) {
		return "", fmt.Errorf("invalid meter serial %q", serial)
	}

	kind := "electricity-meter-points"
	if gas {
		kind = "gas-meter-points"
	}
	base, err := url.JoinPath(c.base, kind, pointID, "meters", serial, "consumption")
	if err != nil {
		return "", fmt.Errorf("build consumption url: %w", err)
	}
	return base + "/", nil
}

// getJSON performs an authenticated GET and decodes the JSON body into out.
// Octopus auth is HTTP Basic with the API key as username and an empty
// password. The key lives only in the Authorization header of the outbound
// request — it must never appear in URLs or error messages.
func (c *Client) getJSON(ctx context.Context, apiKey, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(apiKey, "")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call octopus: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body close

	if resp.StatusCode != http.StatusOK {
		// Drain a little of the body for context but never echo it fully —
		// upstream errors are not under our control.
		return fmt.Errorf("octopus returned status %d for %s %s", resp.StatusCode, http.MethodGet, req.URL.Path)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(out); err != nil {
		return fmt.Errorf("decode octopus response: %w", err)
	}
	return nil
}
