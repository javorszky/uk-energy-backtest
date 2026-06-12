package server_test

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixtureJSON struct {
	profile, tariffs string
}

func readFixture() (fixtureJSON, error) {
	raw, err := os.ReadFile("../../testdata/shared/profile_fixture.json")
	if err != nil {
		return fixtureJSON{}, err //nolint:wrapcheck // test helper
	}
	var f struct {
		ExpectedProfile json.RawMessage `json:"expected_profile"`
		Tariffs         json.RawMessage `json:"tariffs"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return fixtureJSON{}, err //nolint:wrapcheck // test helper
	}
	return fixtureJSON{profile: string(f.ExpectedProfile), tariffs: string(f.Tariffs)}, nil
}

func TestLargeJSONResponsesAreGzipped(t *testing.T) {
	// Ten tariffs produce a ~5 KB response — well past the gzip threshold; a
	// client advertising gzip must get a compressed body that still decodes
	// to the same JSON. (The two-tariff fixture response is ~1 KB and stays
	// below the threshold, see the test below.)
	raw, err := readFixture()
	require.NoError(t, err)

	var tariffs []json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw.tariffs), &tariffs))
	const copies = 5
	many := make([]json.RawMessage, 0, len(tariffs)*copies)
	for range copies {
		many = append(many, tariffs...)
	}
	manyJSON, err := json.Marshal(many)
	require.NoError(t, err)

	body := `{"profile": ` + raw.profile + `, "tariffs": ` + string(manyJSON) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cost", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	newHandler("").ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))

	zr, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)
	decompressed, err := io.ReadAll(zr)
	require.NoError(t, err)

	var resp struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(decompressed, &resp))
	assert.Len(t, resp.Results, 2*copies)
}

func TestTinyResponsesAreNotGzipped(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", http.NoBody)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	newHandler("").ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Content-Encoding"), "sub-threshold response must stay uncompressed")
}

func TestNoGzipWithoutAcceptEncoding(t *testing.T) {
	raw, err := readFixture()
	require.NoError(t, err)

	body := `{"profile": ` + raw.profile + `, "tariffs": ` + raw.tariffs + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cost", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newHandler("").ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.True(t, json.Valid(rec.Body.Bytes()), "plain JSON expected")
}
