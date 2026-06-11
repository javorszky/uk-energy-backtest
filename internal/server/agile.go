package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/javorszky/uk-energy-backtest/internal/octopus"
)

// rateSeriesFetcher abstracts the Octopus client's public-rates surface for
// handler tests.
type rateSeriesFetcher interface {
	RateSeries(ctx context.Context, product, region, leaf string, from, to time.Time) ([]octopus.RatePoint, error)
}

// agileRatesTimeout bounds the whole paginated fetch (a year of Agile is
// ~12 pages).
const agileRatesTimeout = 60 * time.Second

type agileRatesResponse struct {
	Results []octopus.RatePoint `json:"results"`
}

// agileRatesHandler implements GET /api/v1/agile/rates: relays the published
// half-hourly price history for a dynamic (Agile-style) electricity product
// so the frontend can cost raw readings against it on-device. The rates are
// public data — no credential is involved — which keeps the privacy
// invariant intact: usage flows nowhere, prices flow in.
func agileRatesHandler(fetcher rateSeriesFetcher) echo.HandlerFunc {
	return func(c *echo.Context) error {
		// Historical prices are immutable public data; let everything cache.
		c.Response().Header().Set("Cache-Control", "public, max-age=3600")

		product := c.QueryParam("product")
		region := c.QueryParam("region")
		kind := c.QueryParam("kind")
		if kind == "" {
			kind = "unit"
		}
		leaf := ""
		switch kind {
		case "unit":
			leaf = octopus.LeafUnitRates
		case "standing":
			leaf = octopus.LeafStandingCharges
		default:
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, `kind must be "unit" or "standing"`)
		}
		if !octopus.ValidProduct(product) {
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, "product must be a product code like AGILE-24-10-01")
		}
		if !octopus.ValidRegion(region) {
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, "region must be a single GSP letter A-P")
		}

		from, to, msg := validatePeriod(c.QueryParam("period_from"), c.QueryParam("period_to"))
		if msg != "" {
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, msg)
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), agileRatesTimeout)
		defer cancel()

		points, err := fetcher.RateSeries(ctx, product, region, leaf, from, to)
		if err != nil {
			return jsonError(c, http.StatusBadGateway, codeUpstreamError, err.Error())
		}

		if err := c.JSON(http.StatusOK, agileRatesResponse{Results: points}); err != nil {
			return fmt.Errorf("write agile rates response: %w", err)
		}
		return nil
	}
}
