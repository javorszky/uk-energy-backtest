package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/javorszky/uk-energy-backtest/internal/costing"
	"github.com/javorszky/uk-energy-backtest/internal/octopus"
)

const (
	// octopusKeyHeader carries the user's Octopus API key. It exists for one
	// request only: it is forwarded as upstream Basic auth and never stored,
	// cached, or logged. The logging/tracing middleware records no request
	// headers (see middleware_redaction_test.go, which proves this).
	octopusKeyHeader = "X-Octopus-Key"

	// octopusTimeout bounds the whole fetch-aggregate-cost pipeline. Three
	// meters with pagination can take a while against a slow upstream.
	octopusTimeout = 90 * time.Second

	// maxPeriodDays caps the requested window at two years plus DST slack —
	// enough for any realistic backtest while bounding upstream load.
	maxPeriodDays = 732

	dateLayout = "2006-01-02"
)

// meterFetcher abstracts the Octopus client so handler tests can stub the
// upstream without a network.
type meterFetcher interface {
	DiscoverMeters(ctx context.Context, apiKey, account string) (octopus.MeterPoints, error)
	Consumption(ctx context.Context, apiKey, pointID, serial string, from, to time.Time, gas bool) ([]costing.Reading, error)
}

type octopusCostRequest struct {
	Account        string           `json:"account"`
	PeriodFrom     string           `json:"period_from"`
	PeriodTo       string           `json:"period_to"`
	GasUnit        string           `json:"gas_unit"`
	Tariffs        []costing.Tariff `json:"tariffs"`
	CalorificValue float64          `json:"calorific_value"`
}

type octopusCostResponse struct {
	Results []costing.Result `json:"results"`
	// Profile is returned so the frontend can draw the daily load-profile
	// chart without ever having seen the raw half-hourly data.
	Profile costing.Profile `json:"profile"`
}

// octopusCostHandler implements POST /api/v1/octopus/cost: fetch the user's
// half-hourly data from Octopus, aggregate it to the 48-bucket profile, cost
// the requested tariffs, and respond. The raw readings and the API key are
// discarded when the handler returns — nothing is persisted server-side.
func octopusCostHandler(fetcher meterFetcher, loc *time.Location) echo.HandlerFunc {
	return func(c *echo.Context) error {
		// The key must never be cached by any intermediary, and neither may
		// the (personal) consumption-derived response.
		c.Response().Header().Set("Cache-Control", "no-store")

		apiKey := c.Request().Header.Get(octopusKeyHeader)
		if apiKey == "" {
			return jsonError(c, http.StatusUnauthorized, codeMissingKey,
				fmt.Sprintf("provide your Octopus API key in the %s header", octopusKeyHeader))
		}

		var req octopusCostRequest
		if err := c.Bind(&req); err != nil {
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, "malformed request body")
		}

		from, to, msg := validateOctopusRequest(req)
		if msg != "" {
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, msg)
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), octopusTimeout)
		defer cancel()

		profile, err := fetchAndAggregate(ctx, fetcher, apiKey, req, from, to, loc)
		if err != nil {
			// Upstream failures (bad key, unknown account, Octopus downtime)
			// surface as 502: the request was well-formed, the upstream
			// leg failed. The error text never contains the key.
			return jsonError(c, http.StatusBadGateway, codeUpstreamError, err.Error())
		}

		results, err := costAll(&profile, req.Tariffs)
		if err != nil {
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, err.Error())
		}

		if err := c.JSON(http.StatusOK, octopusCostResponse{Profile: profile, Results: results}); err != nil {
			return fmt.Errorf("write octopus cost response: %w", err)
		}
		return nil
	}
}

func validateOctopusRequest(req octopusCostRequest) (from, to time.Time, msg string) {
	if msg := validateOctopusFields(req); msg != "" {
		return time.Time{}, time.Time{}, msg
	}
	return validatePeriod(req.PeriodFrom, req.PeriodTo)
}

func validateOctopusFields(req octopusCostRequest) string {
	if req.Account == "" {
		return "account is required"
	}
	if msg := validateTariffCount(req.Tariffs); msg != "" {
		return msg
	}
	if req.GasUnit != "" && req.GasUnit != "kwh" && req.GasUnit != "m3" {
		return `gas_unit must be "kwh" or "m3"`
	}
	if req.CalorificValue < 0 {
		return "calorific_value must not be negative"
	}
	return ""
}

func validatePeriod(periodFrom, periodTo string) (from, to time.Time, msg string) {
	from, err := time.ParseInLocation(dateLayout, periodFrom, time.UTC)
	if err != nil {
		return time.Time{}, time.Time{}, "period_from must be a YYYY-MM-DD date"
	}
	to, err = time.ParseInLocation(dateLayout, periodTo, time.UTC)
	if err != nil {
		return time.Time{}, time.Time{}, "period_to must be a YYYY-MM-DD date"
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, "period_from must be before period_to"
	}
	if to.Sub(from) > maxPeriodDays*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Sprintf("period must not exceed %d days", maxPeriodDays)
	}
	return from, to, ""
}

// fetchAndAggregate discovers the account's meters, pulls consumption for
// each stream that exists, and collapses everything into the load profile.
// The raw readings only ever live in this function's locals.
func fetchAndAggregate(
	ctx context.Context,
	fetcher meterFetcher,
	apiKey string,
	req octopusCostRequest,
	from, to time.Time,
	loc *time.Location,
) (costing.Profile, error) {
	mp, err := fetcher.DiscoverMeters(ctx, apiKey, req.Account)
	if err != nil {
		return costing.Profile{}, fmt.Errorf("discover meters: %w", err)
	}

	imp, err := fetcher.Consumption(ctx, apiKey, mp.ImportMPAN, mp.ImportSerial, from, to, false)
	if err != nil {
		return costing.Profile{}, fmt.Errorf("fetch import consumption: %w", err)
	}

	var exp []costing.Reading
	if mp.ExportMPAN != "" {
		if exp, err = fetcher.Consumption(ctx, apiKey, mp.ExportMPAN, mp.ExportSerial, from, to, false); err != nil {
			return costing.Profile{}, fmt.Errorf("fetch export consumption: %w", err)
		}
	}

	var gas []costing.Reading
	if mp.GasMPRN != "" {
		if gas, err = fetcher.Consumption(ctx, apiKey, mp.GasMPRN, mp.GasSerial, from, to, true); err != nil {
			return costing.Profile{}, fmt.Errorf("fetch gas consumption: %w", err)
		}
	}

	cv := req.CalorificValue
	if cv == 0 {
		cv = costing.DefaultCalorificValue
	}
	profile, err := costing.BuildProfile(imp, exp, gas, req.GasUnit == "m3", cv, loc)
	if err != nil {
		return costing.Profile{}, fmt.Errorf("build profile: %w", err)
	}
	return profile, nil
}
