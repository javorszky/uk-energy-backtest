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

// stubFetcher implements meterFetcher without a network.
type stubFetcher struct {
	discoverErr error
	fetchErr    error
	consumption map[string][]costing.Reading
	meters      octopus.MeterPoints
}

func (s *stubFetcher) DiscoverMeters(_ context.Context, _, _ string) (octopus.MeterPoints, error) {
	if s.discoverErr != nil {
		return octopus.MeterPoints{}, s.discoverErr
	}
	return s.meters, nil
}

func (s *stubFetcher) Consumption(_ context.Context, _, pointID, _ string, _, _ time.Time, _ bool) ([]costing.Reading, error) {
	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	return s.consumption[pointID], nil
}

func mustLondon(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	require.NoError(t, err)
	return loc
}

func octopusTestHandler(t *testing.T, fetcher meterFetcher) http.Handler {
	t.Helper()
	e := echo.New()
	e.POST("/api/v1/octopus/cost", octopusCostHandler(fetcher, mustLondon(t)))
	return e
}

func postOctopus(t *testing.T, h http.Handler, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/octopus/cost", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set(octopusKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const validOctopusBody = `{
	"account": "A-1234ABCD",
	"period_from": "2024-06-01",
	"period_to": "2024-06-02",
	"gas_unit": "kwh",
	"tariffs": [{"name": "flat", "electricity": {"standing_charge": 50, "import_default": 25, "export_default": 15}}]
}`

func TestOctopusCostMissingKey(t *testing.T) {
	h := octopusTestHandler(t, &stubFetcher{})
	rec := postOctopus(t, h, "", validOctopusBody)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, "missing_octopus_key", env.Error.Code)
}

func TestOctopusCostFullPipeline(t *testing.T) {
	fetcher := &stubFetcher{
		meters: octopus.MeterPoints{
			ImportMPAN: "1111", ImportSerial: "I1",
			ExportMPAN: "2222", ExportSerial: "E1",
		},
		consumption: map[string][]costing.Reading{
			"1111": {
				{IntervalStart: time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC), Consumption: 1.0},
				{IntervalStart: time.Date(2024, 6, 1, 10, 30, 0, 0, time.UTC), Consumption: 2.0},
			},
			"2222": {
				{IntervalStart: time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC), Consumption: 1.0},
			},
		},
	}
	h := octopusTestHandler(t, fetcher)
	rec := postOctopus(t, h, "sk_test", validOctopusBody)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var resp struct {
		Results []costing.Result `json:"results"`
		Profile costing.Profile  `json:"profile"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// 10:00Z in June is 11:00 BST → bucket 22; 10:30Z → bucket 23.
	assert.InDelta(t, 1.0, resp.Profile.ImportHH[22], 1e-9)
	assert.InDelta(t, 2.0, resp.Profile.ImportHH[23], 1e-9)
	require.NotNil(t, resp.Profile.ExportHH)
	assert.InDelta(t, 1.0, resp.Profile.ExportHH[26], 1e-9)
	assert.Equal(t, 1, resp.Profile.SuppliedDays)

	require.Len(t, resp.Results, 1)
	// import 3 kWh × 25p + standing 50p − export 1 kWh × 15p = 110p.
	assert.InDelta(t, 110.0, resp.Results[0].NetPence, 1e-9)
}

func TestOctopusCostUpstreamFailure(t *testing.T) {
	h := octopusTestHandler(t, &stubFetcher{discoverErr: fmt.Errorf("octopus returned status 401")})
	rec := postOctopus(t, h, "sk_bad", validOctopusBody)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "upstream_error")
	assert.NotContains(t, rec.Body.String(), "sk_bad")
}

func TestOctopusCostValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"missing account", `{"period_from": "2024-06-01", "period_to": "2024-06-02", "tariffs": [{"name": "t", "electricity": {}}]}`, "account"},
		{"empty tariffs", `{"account": "A-1", "period_from": "2024-06-01", "period_to": "2024-06-02", "tariffs": []}`, "tariff"},
		{"bad date", `{"account": "A-1", "period_from": "junk", "period_to": "2024-06-02", "tariffs": [{"name": "t", "electricity": {}}]}`, "period_from"},
		{"from after to", `{"account": "A-1", "period_from": "2024-06-02", "period_to": "2024-06-01", "tariffs": [{"name": "t", "electricity": {}}]}`, "before"},
		{"range too long", `{"account": "A-1", "period_from": "2020-01-01", "period_to": "2024-06-01", "tariffs": [{"name": "t", "electricity": {}}]}`, "exceed"},
		{"bad gas unit", `{"account": "A-1", "period_from": "2024-06-01", "period_to": "2024-06-02", "gas_unit": "therms", "tariffs": [{"name": "t", "electricity": {}}]}`, "gas_unit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := octopusTestHandler(t, &stubFetcher{})
			rec := postOctopus(t, h, "sk_test", tc.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.want)
		})
	}
}

func TestOctopusCostNoGasMeterWithM3Unit(t *testing.T) {
	// gas_unit=m3 with an electricity-only account must not error: the gas
	// stream is simply absent.
	fetcher := &stubFetcher{
		meters: octopus.MeterPoints{ImportMPAN: "1111", ImportSerial: "I1"},
		consumption: map[string][]costing.Reading{
			"1111": {{IntervalStart: time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC), Consumption: 1.0}},
		},
	}
	h := octopusTestHandler(t, fetcher)
	body := strings.Replace(validOctopusBody, `"gas_unit": "kwh"`, `"gas_unit": "m3"`, 1)
	rec := postOctopus(t, h, "sk_test", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Profile costing.Profile `json:"profile"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Zero(t, resp.Profile.GasKWh)
	assert.Nil(t, resp.Profile.ExportHH)
}

// recordingFetcher captures the credential the handler resolved.
type recordingFetcher struct {
	stubFetcher
	gotCredential string
}

func (r *recordingFetcher) DiscoverMeters(ctx context.Context, apiKey, account string) (octopus.MeterPoints, error) {
	r.gotCredential = apiKey
	return r.stubFetcher.DiscoverMeters(ctx, apiKey, account)
}

func TestOctopusCostAcceptsOAuthToken(t *testing.T) {
	fetcher := &recordingFetcher{stubFetcher: stubFetcher{
		meters: octopus.MeterPoints{ImportMPAN: "1111", ImportSerial: "I1"},
		consumption: map[string][]costing.Reading{
			"1111": {{IntervalStart: time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC), Consumption: 1.0}},
		},
	}}
	h := octopusTestHandler(t, fetcher)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/octopus/cost", strings.NewReader(validOctopusBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(octopusTokenHeader, " tok123 ")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "Bearer tok123", fetcher.gotCredential,
		"token must reach the client bearer-prefixed and trimmed")
}
