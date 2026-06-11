package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// postJSON sends body to path through the full middleware stack.
func postJSON(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cost", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newHandler("").ServeHTTP(rec, req)
	return rec
}

type errEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errEnvelope {
	t.Helper()
	var env errEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	return env
}

func TestCostEndpointWithSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/shared/profile_fixture.json")
	require.NoError(t, err)

	var fixture struct {
		ExpectedProfile json.RawMessage `json:"expected_profile"`
		Tariffs         json.RawMessage `json:"tariffs"`
		ExpectedResults []struct {
			Name       string  `json:"name"`
			TotalPence float64 `json:"total_p"`
		} `json:"expected_results"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))

	body := `{"profile": ` + string(fixture.ExpectedProfile) + `, "tariffs": ` + string(fixture.Tariffs) + `}`
	rec := postJSON(t, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Results []struct {
			Name       string  `json:"name"`
			TotalPence float64 `json:"total_p"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Results, len(fixture.ExpectedResults))
	for i, want := range fixture.ExpectedResults {
		assert.Equal(t, want.Name, resp.Results[i].Name)
		assert.InDelta(t, want.TotalPence, resp.Results[i].TotalPence, 1e-9)
	}
}

func TestCostEndpointRejectsMalformedJSON(t *testing.T) {
	rec := postJSON(t, `{not json`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_request", decodeError(t, rec).Error.Code)
}

func TestCostEndpointRejectsEmptyTariffs(t *testing.T) {
	rec := postJSON(t, `{"profile": {"supplied_days": 1, "import_hh": []}, "tariffs": []}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decodeError(t, rec).Error.Message, "tariff")
}

// buckets48 renders a JSON array of n copies of v — handy for building
// (in)valid bucket arrays.
func bucketsJSON(v string, n int) string {
	return "[" + strings.TrimSuffix(strings.Repeat(v+",", n), ",") + "]"
}

func TestCostEndpointRejectsWrongBucketCount(t *testing.T) {
	for _, n := range []int{0, 1, 47, 49} {
		body := `{"profile": {"supplied_days": 1, "import_hh": ` + bucketsJSON("0.1", n) + `}, "tariffs": [{"name": "t", "electricity": {"import_default": 1}}]}`
		rec := postJSON(t, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "import_hh with %d buckets must be rejected", n)
		assert.Contains(t, decodeError(t, rec).Error.Message, "exactly 48")
	}
}

func TestCostEndpointRejectsBadBandBoundary(t *testing.T) {
	body := `{
		"profile": {"supplied_days": 1, "import_hh": ` + bucketsJSON("0.5", 48) + `},
		"tariffs": [{"name": "t", "electricity": {"import_default": 1, "import_bands": [{"from": "02:15", "to": "05:00", "rate": 1}]}}]
	}`
	rec := postJSON(t, body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decodeError(t, rec).Error.Message, ":00 or :30")
}

func TestCostEndpointRejectsNegativeUsage(t *testing.T) {
	body := `{
		"profile": {"supplied_days": 1, "import_hh": ` + bucketsJSON("-0.5", 48) + `},
		"tariffs": [{"name": "t", "electricity": {"import_default": 1}}]
	}`
	rec := postJSON(t, body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decodeError(t, rec).Error.Message, "negative")
}
