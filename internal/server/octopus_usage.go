package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/javorszky/uk-energy-backtest/internal/costing"
)

type octopusUsageRequest struct {
	Account    string `json:"account"`
	PeriodFrom string `json:"period_from"`
	PeriodTo   string `json:"period_to"`
}

// wireReading is one half-hour on the wire, compact enough that a year of
// three streams stays a few MB. ts is the interval start in UTC epoch
// milliseconds — exactly the frontend's RawReading shape.
type wireReading struct {
	TS    int64   `json:"ts"`
	Value float64 `json:"value"`
}

type octopusUsageResponse struct {
	Import []wireReading `json:"import"`
	Export []wireReading `json:"export,omitempty"`
	Gas    []wireReading `json:"gas,omitempty"`
}

// octopusUsageHandler implements POST /api/v1/octopus/usage: fetch the
// account's raw half-hourly readings and relay them to the requester — the
// data's owner — so the browser can run the same on-device pipeline as the
// CSV path (profile build, Agile backtest, dataset save). Nothing is stored
// server-side; the readings and credential are discarded with the request.
func octopusUsageHandler(fetcher meterFetcher) echo.HandlerFunc {
	return func(c *echo.Context) error {
		// Personal consumption data: no intermediary may cache it.
		c.Response().Header().Set("Cache-Control", "no-store")

		apiKey, ok := octopusCredential(c)
		if !ok {
			return jsonError(c, http.StatusUnauthorized, codeMissingKey, missingCredentialMsg)
		}

		var req octopusUsageRequest
		if err := c.Bind(&req); err != nil {
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, "malformed request body")
		}
		if req.Account == "" {
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, "account is required")
		}
		from, to, msg := validatePeriod(req.PeriodFrom, req.PeriodTo)
		if msg != "" {
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, msg)
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), octopusTimeout)
		defer cancel()

		imp, exp, gas, err := fetchStreams(ctx, fetcher, apiKey, req.Account, from, to)
		if err != nil {
			return jsonError(c, http.StatusBadGateway, codeUpstreamError, err.Error())
		}

		resp := octopusUsageResponse{
			Import: toWire(imp),
			Export: toWire(exp),
			Gas:    toWire(gas),
		}
		if err := c.JSON(http.StatusOK, resp); err != nil {
			return fmt.Errorf("write octopus usage response: %w", err)
		}
		return nil
	}
}

func toWire(readings []costing.Reading) []wireReading {
	if len(readings) == 0 {
		return nil
	}
	out := make([]wireReading, len(readings))
	for i, r := range readings {
		out[i] = wireReading{TS: r.IntervalStart.UnixMilli(), Value: r.Consumption}
	}
	return out
}

// fetchStreams discovers the account's meters and pulls consumption for
// every stream that exists. Shared by the usage relay and the
// fetch-aggregate-cost path.
func fetchStreams(
	ctx context.Context,
	fetcher meterFetcher,
	apiKey, account string,
	from, to time.Time,
) (imp, exp, gas []costing.Reading, err error) {
	mp, err := fetcher.DiscoverMeters(ctx, apiKey, account)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("discover meters: %w", err)
	}

	if imp, err = fetcher.Consumption(ctx, apiKey, mp.ImportMPAN, mp.ImportSerial, from, to, false); err != nil {
		return nil, nil, nil, fmt.Errorf("fetch import consumption: %w", err)
	}
	if mp.ExportMPAN != "" {
		if exp, err = fetcher.Consumption(ctx, apiKey, mp.ExportMPAN, mp.ExportSerial, from, to, false); err != nil {
			return nil, nil, nil, fmt.Errorf("fetch export consumption: %w", err)
		}
	}
	if mp.GasMPRN != "" {
		if gas, err = fetcher.Consumption(ctx, apiKey, mp.GasMPRN, mp.GasSerial, from, to, true); err != nil {
			return nil, nil, nil, fmt.Errorf("fetch gas consumption: %w", err)
		}
	}
	return imp, exp, gas, nil
}
