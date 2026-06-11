package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javorszky/uk-energy-backtest/internal/config"
)

func TestOAuthConfigDisabled(t *testing.T) {
	e := echo.New()
	e.GET("/api/v1/oauth/config", oauthConfigHandler(""))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/oauth/config", http.NoBody)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp oauthConfigResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.False(t, resp.Enabled)
	assert.Empty(t, resp.ClientID)
}

func TestOAuthConfigEnabled(t *testing.T) {
	e := echo.New()
	e.GET("/api/v1/oauth/config", oauthConfigHandler("client-abc"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/oauth/config", http.NoBody)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp oauthConfigResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Enabled)
	assert.Equal(t, "client-abc", resp.ClientID)
	assert.Equal(t, "https://auth.octopus.energy/authorize/", resp.AuthorizeURL)
	assert.Contains(t, resp.Scopes, "view:detailed-usage")
}

// TestOAuthTokenRouteGatedByEnv proves the env-var gate: without
// OCTOPUS_OAUTH_CLIENT_ID the token route does not exist at all.
func TestOAuthTokenRouteGatedByEnv(t *testing.T) {
	body := `{"grant_type": "refresh_token", "refresh_token": "r"}`

	server := func(clientID string) http.Handler {
		cfg, err := config.LoadFrom(map[string]string{"OCTOPUS_OAUTH_CLIENT_ID": clientID})
		require.NoError(t, err)
		return New(cfg, "", "").Handler()
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/oauth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server("").ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code, "token route must not exist when the client id is unset")

	rec = httptest.NewRecorder()
	server("client-abc").ServeHTTP(rec, req)
	assert.NotEqual(t, http.StatusNotFound, rec.Code, "token route must exist when the client id is set")
}

// stubExchanger implements tokenExchanger.
type stubExchanger struct {
	err     error
	gotForm url.Values
	body    []byte
	status  int
}

func (s *stubExchanger) ExchangeToken(_ context.Context, form url.Values) (body []byte, status int, err error) {
	s.gotForm = form
	if s.err != nil {
		return nil, 0, s.err
	}
	return s.body, s.status, nil
}

func tokenTestHandler(exchanger tokenExchanger) http.Handler {
	e := echo.New()
	e.POST("/api/v1/oauth/token", oauthTokenHandler(exchanger, "client-abc"))
	return e
}

func postToken(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/oauth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestOAuthTokenCodeExchange(t *testing.T) {
	ex := &stubExchanger{body: []byte(`{"access_token": "at", "expires_in": 3600}`), status: http.StatusOK}
	rec := postToken(t, tokenTestHandler(ex), `{
		"grant_type": "authorization_code",
		"code": "abc",
		"code_verifier": "ver",
		"redirect_uri": "https://app.example/oauth/callback"
	}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.JSONEq(t, `{"access_token": "at", "expires_in": 3600}`, rec.Body.String())

	assert.Equal(t, "authorization_code", ex.gotForm.Get("grant_type"))
	assert.Equal(t, "client-abc", ex.gotForm.Get("client_id"))
	assert.Equal(t, "abc", ex.gotForm.Get("code"))
	assert.Equal(t, "ver", ex.gotForm.Get("code_verifier"))
	assert.Equal(t, "https://app.example/oauth/callback", ex.gotForm.Get("redirect_uri"))
}

func TestOAuthTokenRefresh(t *testing.T) {
	ex := &stubExchanger{body: []byte(`{"access_token": "at2"}`), status: http.StatusOK}
	rec := postToken(t, tokenTestHandler(ex), `{"grant_type": "refresh_token", "refresh_token": "r1"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "r1", ex.gotForm.Get("refresh_token"))
	assert.Empty(t, ex.gotForm.Get("code"))
}

func TestOAuthTokenRelaysUpstreamError(t *testing.T) {
	// A structured OAuth error from the auth server passes through verbatim
	// with its status, so the SPA can handle invalid_grant per spec.
	ex := &stubExchanger{body: []byte(`{"error": "invalid_grant"}`), status: http.StatusBadRequest}
	rec := postToken(t, tokenTestHandler(ex), `{"grant_type": "refresh_token", "refresh_token": "expired"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error": "invalid_grant"}`, rec.Body.String())
}

func TestOAuthTokenValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"unknown grant", `{"grant_type": "password"}`},
		{"code grant missing fields", `{"grant_type": "authorization_code", "code": "abc"}`},
		{"refresh grant missing token", `{"grant_type": "refresh_token"}`},
		{"malformed json", `{nope`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := postToken(t, tokenTestHandler(&stubExchanger{}), tc.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), "invalid_request")
		})
	}
}

func TestOAuthTokenTransportFailure(t *testing.T) {
	ex := &stubExchanger{err: fmt.Errorf("call auth server: connection refused")}
	rec := postToken(t, tokenTestHandler(ex), `{"grant_type": "refresh_token", "refresh_token": "r"}`)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "upstream_error")
}
