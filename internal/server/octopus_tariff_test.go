package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javorszky/uk-energy-backtest/internal/costing"
	"github.com/javorszky/uk-energy-backtest/internal/octopus"
)

// stubTariffFetcher implements tariffFetcher without a network.
type stubTariffFetcher struct {
	codesErr    error
	standing    map[string]float64
	rateBuckets map[string]*[costing.BucketsPerDay]float64
	codes       octopus.TariffCodes
	gasRate     float64
}

func (s *stubTariffFetcher) CurrentTariffCodes(_ context.Context, _, _ string, _ time.Time) (octopus.TariffCodes, error) {
	if s.codesErr != nil {
		return octopus.TariffCodes{}, s.codesErr
	}
	return s.codes, nil
}

func (s *stubTariffFetcher) CurrentStandingCharge(_ context.Context, _, tariffCode string, _ bool, _ time.Time) (float64, error) {
	v, ok := s.standing[tariffCode]
	if !ok {
		return 0, fmt.Errorf("no standing charge stubbed for %s", tariffCode)
	}
	return v, nil
}

func (s *stubTariffFetcher) CurrentGasUnitRate(_ context.Context, _, _ string, _ time.Time) (float64, error) {
	return s.gasRate, nil
}

func (s *stubTariffFetcher) UnitRateBuckets(_ context.Context, _, tariffCode string, _ time.Time, _ *time.Location) (*[costing.BucketsPerDay]float64, error) {
	r, ok := s.rateBuckets[tariffCode]
	if !ok {
		return nil, fmt.Errorf("no rates stubbed for %s", tariffCode)
	}
	return r, nil
}

// Tariff codes reused across stubs; named to satisfy goconst.
const (
	fluxImportCode = "E-1R-FLUX-IMPORT-23-02-14-C"
	agileCode      = "E-1R-AGILE-24-10-01-C"
	flatVarCode    = "E-1R-VAR-22-11-01-C"
)

func flatRates(v float64) *[costing.BucketsPerDay]float64 {
	var r [costing.BucketsPerDay]float64
	for i := range r {
		r[i] = v
	}
	return &r
}

func tariffTestHandler(t *testing.T, fetcher tariffFetcher) http.Handler {
	t.Helper()
	e := echo.New()
	e.POST("/api/v1/octopus/tariff", octopusTariffHandler(fetcher, mustLondon(t)))
	return e
}

func postTariff(t *testing.T, h http.Handler, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/octopus/tariff", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set(octopusKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestOctopusTariffFullPrefill(t *testing.T) {
	importRates := flatRates(30)
	for i := 4; i <= 9; i++ {
		importRates[i] = 16 // 02:00–05:00 cheap window
	}
	fetcher := &stubTariffFetcher{
		codes: octopus.TariffCodes{
			Import: fluxImportCode,
			Export: "E-1R-FLUX-EXPORT-23-02-14-C",
			Gas:    "G-1R-VAR-22-11-01-C",
		},
		standing: map[string]float64{
			fluxImportCode:        47.85,
			"G-1R-VAR-22-11-01-C": 29.6,
		},
		gasRate: 6.1,
		rateBuckets: map[string]*[costing.BucketsPerDay]float64{
			fluxImportCode:                importRates,
			"E-1R-FLUX-EXPORT-23-02-14-C": flatRates(15),
		},
	}

	rec := postTariff(t, tariffTestHandler(t, fetcher), "sk_test", `{"account": "A-1"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var resp struct {
		Codes struct {
			Import string `json:"import"`
		} `json:"codes"`
		Warnings []string       `json:"warnings"`
		Tariff   costing.Tariff `json:"tariff"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 47.85, resp.Tariff.Electricity.StandingCharge)
	assert.Equal(t, 30.0, resp.Tariff.Electricity.ImportDefault)
	require.Len(t, resp.Tariff.Electricity.ImportBands, 1)
	assert.Equal(t, costing.Band{From: "02:00", To: "05:00", Rate: 16}, resp.Tariff.Electricity.ImportBands[0])
	assert.Equal(t, 15.0, resp.Tariff.Electricity.ExportDefault)
	assert.Empty(t, resp.Tariff.Electricity.ExportBands)
	require.NotNil(t, resp.Tariff.Gas)
	assert.Equal(t, 29.6, resp.Tariff.Gas.StandingCharge)
	assert.Equal(t, 6.1, resp.Tariff.Gas.Rate)
	assert.Empty(t, resp.Warnings)
	assert.Equal(t, fluxImportCode, resp.Codes.Import)
	assert.Contains(t, resp.Tariff.Name, "FLUX-IMPORT")
}

func TestOctopusTariffAgileFallback(t *testing.T) {
	// 48 distinct rates → cannot be expressed as bands; average + warning.
	var agile [costing.BucketsPerDay]float64
	for i := range agile {
		agile[i] = float64(i) + 1
	}
	fetcher := &stubTariffFetcher{
		codes:    octopus.TariffCodes{Import: agileCode},
		standing: map[string]float64{agileCode: 50},
		rateBuckets: map[string]*[costing.BucketsPerDay]float64{
			agileCode: &agile,
		},
	}

	rec := postTariff(t, tariffTestHandler(t, fetcher), "sk_test", `{"account": "A-1"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Warnings []string       `json:"warnings"`
		Tariff   costing.Tariff `json:"tariff"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Mean of 1..48 = 24.5.
	assert.InDelta(t, 24.5, resp.Tariff.Electricity.ImportDefault, 1e-9)
	assert.Empty(t, resp.Tariff.Electricity.ImportBands)
	require.Len(t, resp.Warnings, 1)
	assert.Contains(t, resp.Warnings[0], "Agile")
	assert.Nil(t, resp.Tariff.Gas)
}

func TestOctopusTariffElectricityOnly(t *testing.T) {
	fetcher := &stubTariffFetcher{
		codes:    octopus.TariffCodes{Import: flatVarCode},
		standing: map[string]float64{flatVarCode: 53.8},
		rateBuckets: map[string]*[costing.BucketsPerDay]float64{
			flatVarCode: flatRates(24.5),
		},
	}
	rec := postTariff(t, tariffTestHandler(t, fetcher), "sk_test", `{"account": "A-1"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Tariff costing.Tariff `json:"tariff"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Nil(t, resp.Tariff.Gas)
	assert.Equal(t, 24.5, resp.Tariff.Electricity.ImportDefault)
	assert.Zero(t, resp.Tariff.Electricity.ExportDefault)
}

func TestOctopusTariffMissingKey(t *testing.T) {
	rec := postTariff(t, tariffTestHandler(t, &stubTariffFetcher{}), "", `{"account": "A-1"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing_octopus_key")
}

func TestOctopusTariffValidationAndUpstream(t *testing.T) {
	h := tariffTestHandler(t, &stubTariffFetcher{codesErr: fmt.Errorf("octopus returned status 401")})

	rec := postTariff(t, h, "sk_test", `{"account": ""}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = postTariff(t, h, "sk_test", `{"account": "A-1"}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "upstream_error")
}
