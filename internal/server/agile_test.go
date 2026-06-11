package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javorszky/uk-energy-backtest/internal/octopus"
)

type stubRateSeries struct {
	err        error
	gotProduct string
	gotRegion  string
	gotLeaf    string
	points     []octopus.RatePoint
}

func (s *stubRateSeries) RateSeries(_ context.Context, product, region, leaf string, _, _ time.Time) ([]octopus.RatePoint, error) {
	s.gotProduct, s.gotRegion, s.gotLeaf = product, region, leaf
	if s.err != nil {
		return nil, s.err
	}
	return s.points, nil
}

func getAgile(t *testing.T, fetcher rateSeriesFetcher, query string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	e.GET("/api/v1/agile/rates", agileRatesHandler(fetcher))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agile/rates?"+query, http.NoBody)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestAgileRatesHappyPath(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC)
	stub := &stubRateSeries{points: []octopus.RatePoint{{From: from, Rate: 12.3}}}

	rec := getAgile(t, stub, "product=AGILE-24-10-01&region=C&period_from=2026-01-01&period_to=2026-02-01")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "public, max-age=3600", rec.Header().Get("Cache-Control"))

	assert.Equal(t, "AGILE-24-10-01", stub.gotProduct)
	assert.Equal(t, "C", stub.gotRegion)
	assert.Equal(t, "standard-unit-rates", stub.gotLeaf, "kind defaults to unit")

	var resp struct {
		Results []octopus.RatePoint `json:"results"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Results, 1)
	assert.Equal(t, 12.3, resp.Results[0].Rate)
}

func TestAgileRatesStandingKind(t *testing.T) {
	stub := &stubRateSeries{}
	rec := getAgile(t, stub, "product=AGILE-24-10-01&region=C&kind=standing&period_from=2026-01-01&period_to=2026-02-01")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "standing-charges", stub.gotLeaf)
}

func TestAgileRatesValidation(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"missing product", "region=C&period_from=2026-01-01&period_to=2026-02-01"},
		{"bad product", "product=..%2Fevil&region=C&period_from=2026-01-01&period_to=2026-02-01"},
		{"bad region", "product=AGILE-24-10-01&region=ZZ&period_from=2026-01-01&period_to=2026-02-01"},
		{"region I unused", "product=AGILE-24-10-01&region=I&period_from=2026-01-01&period_to=2026-02-01"},
		{"bad kind", "product=AGILE-24-10-01&region=C&kind=night&period_from=2026-01-01&period_to=2026-02-01"},
		{"bad dates", "product=AGILE-24-10-01&region=C&period_from=junk&period_to=2026-02-01"},
		{"range too long", "product=AGILE-24-10-01&region=C&period_from=2020-01-01&period_to=2026-02-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := getAgile(t, &stubRateSeries{}, tc.query)
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

func TestAgileRatesUpstreamFailure(t *testing.T) {
	stub := &stubRateSeries{err: fmt.Errorf("octopus returned status 503")}
	rec := getAgile(t, stub, "product=AGILE-24-10-01&region=C&period_from=2026-01-01&period_to=2026-02-01")
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "upstream_error")
}
