package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/javorszky/uk-energy-backtest/internal/costing"
	"github.com/javorszky/uk-energy-backtest/internal/octopus"
)

// maxBandableRates is the most distinct unit rates a 24h day can show and
// still be a band-structured tariff (Flux has three). More than this means
// dynamic (Agile-like) pricing, which cannot be expressed as static bands.
const maxBandableRates = 4

// tariffFetcher abstracts the Octopus client's tariff-discovery surface so
// handler tests can stub the upstream.
type tariffFetcher interface {
	CurrentTariffCodes(ctx context.Context, apiKey, account string, now time.Time) (octopus.TariffCodes, error)
	CurrentStandingCharge(ctx context.Context, apiKey, tariffCode string, gas bool, now time.Time) (float64, error)
	CurrentGasUnitRate(ctx context.Context, apiKey, tariffCode string, now time.Time) (float64, error)
	UnitRateBuckets(ctx context.Context, apiKey, tariffCode string, now time.Time, loc *time.Location) (*[costing.BucketsPerDay]float64, error)
}

type octopusTariffRequest struct {
	Account string `json:"account"`
}

type octopusTariffResponse struct {
	Codes    tariffCodes    `json:"codes"`
	Warnings []string       `json:"warnings"`
	Tariff   costing.Tariff `json:"tariff"`
}

type tariffCodes struct {
	Import string `json:"import"`
	Export string `json:"export,omitempty"`
	Gas    string `json:"gas,omitempty"`
}

// octopusTariffHandler implements POST /api/v1/octopus/tariff: look up the
// account's current agreements, pull the published rates for each, and
// return a prefilled tariff in the app's own model. Like the cost endpoint,
// it is stateless and the key is discarded with the request.
func octopusTariffHandler(fetcher tariffFetcher, loc *time.Location) echo.HandlerFunc {
	return func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")

		apiKey := strings.TrimSpace(c.Request().Header.Get(octopusKeyHeader))
		if apiKey == "" {
			return jsonError(c, http.StatusUnauthorized, codeMissingKey,
				fmt.Sprintf("provide your Octopus API key in the %s header", octopusKeyHeader))
		}

		var req octopusTariffRequest
		if err := c.Bind(&req); err != nil {
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, "malformed request body")
		}
		if req.Account == "" {
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, "account is required")
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), octopusTimeout)
		defer cancel()

		resp, err := buildPrefilledTariff(ctx, fetcher, apiKey, req.Account, time.Now(), loc)
		if err != nil {
			return jsonError(c, http.StatusBadGateway, codeUpstreamError, err.Error())
		}

		if err := c.JSON(http.StatusOK, resp); err != nil {
			return fmt.Errorf("write octopus tariff response: %w", err)
		}
		return nil
	}
}

func buildPrefilledTariff(
	ctx context.Context,
	fetcher tariffFetcher,
	apiKey, account string,
	now time.Time,
	loc *time.Location,
) (octopusTariffResponse, error) {
	codes, err := fetcher.CurrentTariffCodes(ctx, apiKey, account, now)
	if err != nil {
		return octopusTariffResponse{}, fmt.Errorf("discover tariffs: %w", err)
	}

	resp := octopusTariffResponse{
		Codes:    tariffCodes{Import: codes.Import, Export: codes.Export, Gas: codes.Gas},
		Warnings: []string{},
	}
	tariff := costing.Tariff{
		Name: "My Octopus tariff (" + codes.Import + ")",
		Electricity: costing.Electricity{
			ImportBands: []costing.Band{},
			ExportBands: []costing.Band{},
		},
	}

	if err := fillElectricity(ctx, fetcher, apiKey, codes, now, loc, &tariff, &resp); err != nil {
		return octopusTariffResponse{}, err
	}

	if codes.Gas != "" {
		gasStanding, err := fetcher.CurrentStandingCharge(ctx, apiKey, codes.Gas, true, now)
		if err != nil {
			return octopusTariffResponse{}, fmt.Errorf("gas standing charge: %w", err)
		}
		gasRate, err := fetcher.CurrentGasUnitRate(ctx, apiKey, codes.Gas, now)
		if err != nil {
			return octopusTariffResponse{}, fmt.Errorf("gas unit rate: %w", err)
		}
		tariff.Gas = &costing.Gas{StandingCharge: gasStanding, Rate: gasRate}
	}

	resp.Tariff = tariff
	return resp, nil
}

func fillElectricity(
	ctx context.Context,
	fetcher tariffFetcher,
	apiKey string,
	codes octopus.TariffCodes,
	now time.Time,
	loc *time.Location,
	tariff *costing.Tariff,
	resp *octopusTariffResponse,
) error {
	standing, err := fetcher.CurrentStandingCharge(ctx, apiKey, codes.Import, false, now)
	if err != nil {
		return fmt.Errorf("import standing charge: %w", err)
	}
	tariff.Electricity.StandingCharge = standing

	def, bands, warning, err := streamRates(ctx, fetcher, apiKey, codes.Import, now, loc)
	if err != nil {
		return fmt.Errorf("import rates: %w", err)
	}
	tariff.Electricity.ImportDefault = def
	tariff.Electricity.ImportBands = bands
	if warning != "" {
		resp.Warnings = append(resp.Warnings, "import: "+warning)
	}

	if codes.Export == "" {
		return nil
	}
	def, bands, warning, err = streamRates(ctx, fetcher, apiKey, codes.Export, now, loc)
	if err != nil {
		return fmt.Errorf("export rates: %w", err)
	}
	tariff.Electricity.ExportDefault = def
	tariff.Electricity.ExportBands = bands
	if warning != "" {
		resp.Warnings = append(resp.Warnings, "export: "+warning)
	}
	return nil
}

// streamRates resolves one electricity tariff code into a default rate plus
// bands. Dynamic tariffs (more distinct daily rates than bands can express,
// i.e. Agile) collapse to a 24h time-weighted average with a warning.
func streamRates(
	ctx context.Context,
	fetcher tariffFetcher,
	apiKey, tariffCode string,
	now time.Time,
	loc *time.Location,
) (def float64, bands []costing.Band, warning string, err error) {
	rates, err := fetcher.UnitRateBuckets(ctx, apiKey, tariffCode, now, loc)
	if err != nil {
		return 0, nil, "", fmt.Errorf("unit rates for %s: %w", tariffCode, err)
	}
	if costing.DistinctRates(rates) > maxBandableRates {
		avg := costing.RoundHalfEven2dp(costing.MeanRate(rates))
		return avg, []costing.Band{}, fmt.Sprintf(
			"%s prices change every half hour (Agile-style); used yesterday's average %.2fp/kWh as a flat rate — adjust to taste",
			tariffCode, avg), nil
	}
	def, bands = costing.BandsFromRates(rates)
	if bands == nil {
		bands = []costing.Band{}
	}
	return def, bands, "", nil
}
