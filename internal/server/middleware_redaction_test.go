package server_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestOctopusKeyNeverReachesLogsOrSpans runs a request carrying an Octopus
// API key through the full middleware stack with an in-memory span exporter
// and a captured slog handler, then asserts the key value appears in neither.
// This is the brief's redaction requirement made executable: the key lives in
// a request header, the logging middleware records method/URI/status only,
// and the OTel middleware records method/path/status attributes only — if
// anyone later adds header logging, this test fails.
//
// Not parallel: it swaps the global tracer provider and default logger.
func TestOctopusKeyNeverReachesLogsOrSpans(t *testing.T) {
	const (
		secretKey   = "sk_live_supersecret_2f9c"
		secretToken = "eyJhbGciOi_supersecret_token_91xq"
	)

	// Capture spans.
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prevTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prevTP) })

	// Capture logs.
	var logBuf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	// The handler must be constructed after the tracer provider swap so the
	// otel middleware picks up the test tracer.
	h := newHandler("")

	// The empty account fails validation before any upstream call is made,
	// so the test never touches the network — but the full middleware stack
	// (recover, otel, request logger) still processes the keyed request.
	body := `{"account": "", "period_from": "2024-06-01", "period_to": "2024-06-02", "tariffs": [{"name": "t", "electricity": {}}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/octopus/cost", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Octopus-Key", secretKey)
	req.Header.Set("X-Octopus-Token", secretToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.NoError(t, tp.ForceFlush(t.Context()))

	spans := exporter.GetSpans()
	require.NotEmpty(t, spans, "expected at least the server span")
	rawSpans, err := json.Marshal(spans)
	require.NoError(t, err)
	for name, secret := range map[string]string{"key": secretKey, "token": secretToken} {
		assert.NotContains(t, string(rawSpans), secret, "octopus %s leaked into a span", name)
		assert.NotContains(t, logBuf.String(), secret, "octopus %s leaked into a log line", name)
		// Neither credential may be echoed back in the response.
		assert.NotContains(t, rec.Body.String(), secret, "octopus %s leaked into the response body", name)
	}
}

// TestOctopusKeyNotAcceptedViaQuery documents that the key travels in the
// header only: a key passed as a query parameter is ignored (the endpoint
// responds 401 missing-key), so it can never end up in the URI that the
// request logger records.
func TestOctopusKeyNotAcceptedViaQuery(t *testing.T) {
	body := `{"account": "A-1", "period_from": "2024-06-01", "period_to": "2024-06-02", "tariffs": [{"name": "t", "electricity": {}}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/octopus/cost?key=sk_live_x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newHandler("").ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing_octopus_key")
}
