package server

import (
	"encoding/json"
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

func usageTestHandler(fetcher meterFetcher) http.Handler {
	e := echo.New()
	e.POST("/api/v1/octopus/usage", octopusUsageHandler(fetcher))
	return e
}

func postUsage(t *testing.T, h http.Handler, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/octopus/usage", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set(octopusKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const validUsageBody = `{"account": "A-1", "period_from": "2024-06-01", "period_to": "2024-06-02"}`

func TestOctopusUsageRelaysRawReadings(t *testing.T) {
	ts1 := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2024, 6, 1, 10, 30, 0, 0, time.UTC)
	fetcher := &stubFetcher{
		meters: octopus.MeterPoints{
			ImportMPAN: "1111", ImportSerial: "I1",
			ExportMPAN: "2222", ExportSerial: "E1",
			GasMPRN: "3333", GasSerial: "G1",
		},
		consumption: map[string][]costing.Reading{
			"1111": {
				{IntervalStart: ts1, Consumption: 0.5},
				{IntervalStart: ts2, Consumption: 0.25},
			},
			"2222": {{IntervalStart: ts1, Consumption: 1.5}},
			"3333": {{IntervalStart: ts1, Consumption: 2.0}},
		},
	}

	rec := postUsage(t, usageTestHandler(fetcher), "sk_test", validUsageBody)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var resp struct {
		Import []struct {
			TS    int64   `json:"ts"`
			Value float64 `json:"value"`
		} `json:"import"`
		Export []struct {
			TS    int64   `json:"ts"`
			Value float64 `json:"value"`
		} `json:"export"`
		Gas []struct {
			TS    int64   `json:"ts"`
			Value float64 `json:"value"`
		} `json:"gas"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	require.Len(t, resp.Import, 2)
	assert.Equal(t, ts1.UnixMilli(), resp.Import[0].TS)
	assert.Equal(t, 0.5, resp.Import[0].Value)
	require.Len(t, resp.Export, 1)
	assert.Equal(t, 1.5, resp.Export[0].Value)
	require.Len(t, resp.Gas, 1)
	assert.Equal(t, 2.0, resp.Gas[0].Value)
}

func TestOctopusUsageOmitsAbsentStreams(t *testing.T) {
	fetcher := &stubFetcher{
		meters: octopus.MeterPoints{ImportMPAN: "1111", ImportSerial: "I1"},
		consumption: map[string][]costing.Reading{
			"1111": {{IntervalStart: time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC), Consumption: 1.0}},
		},
	}
	rec := postUsage(t, usageTestHandler(fetcher), "sk_test", validUsageBody)
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	assert.NotContains(t, body, `"export"`)
	assert.NotContains(t, body, `"gas"`)
}

func TestOctopusUsageValidation(t *testing.T) {
	h := usageTestHandler(&stubFetcher{})

	rec := postUsage(t, h, "", validUsageBody)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = postUsage(t, h, "sk_test", `{"account": "", "period_from": "2024-06-01", "period_to": "2024-06-02"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = postUsage(t, h, "sk_test", `{"account": "A-1", "period_from": "junk", "period_to": "2024-06-02"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
