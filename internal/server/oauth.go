package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/javorszky/uk-energy-backtest/internal/octopus"
)

// octopusTokenHeader carries an OAuth access token instead of an API key.
// Like the key, it lives for one request only and is never stored or logged.
const octopusTokenHeader = "X-Octopus-Token"

// oauthScopes is the minimal scope set the app requests: account/meter
// discovery, agreements (tariff prefill), and consumption.
var oauthScopes = []string{
	"openid",
	"view:account-number",
	"query:property-meters",
	"query:agreements",
	"view:detailed-usage",
	"request:consumption-data",
}

type oauthConfigResponse struct {
	ClientID     string   `json:"client_id,omitempty"`
	AuthorizeURL string   `json:"authorize_url,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
	Enabled      bool     `json:"enabled"`
}

// oauthConfigHandler implements GET /api/v1/oauth/config: tells the SPA
// whether the Octopus OAuth flow is available and, if so, what to send to
// the authorize endpoint. All values are public — the client id of a public
// OAuth client is not a secret.
func oauthConfigHandler(clientID string) echo.HandlerFunc {
	resp := oauthConfigResponse{Enabled: false}
	if clientID != "" {
		resp = oauthConfigResponse{
			Enabled:      true,
			ClientID:     clientID,
			AuthorizeURL: octopus.AuthorizeURL,
			Scopes:       oauthScopes,
		}
	}
	return func(c *echo.Context) error {
		if err := c.JSON(http.StatusOK, resp); err != nil {
			return fmt.Errorf("write oauth config response: %w", err)
		}
		return nil
	}
}

// tokenExchanger abstracts the auth-server client for handler tests.
type tokenExchanger interface {
	ExchangeToken(ctx context.Context, form url.Values) ([]byte, int, error)
}

type oauthTokenRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
	RefreshToken string `json:"refresh_token"`
}

// oauthTokenTimeout bounds one exchange round-trip.
const oauthTokenTimeout = 15 * time.Second

// oauthTokenHandler implements POST /api/v1/oauth/token: forwards a PKCE
// code (or refresh-token) exchange to auth.octopus.energy with the
// configured client id and relays the auth server's response verbatim. The
// backend keeps nothing — tokens pass straight through to the browser.
// Registered only when OCTOPUS_OAUTH_CLIENT_ID is set.
func oauthTokenHandler(exchanger tokenExchanger, clientID string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		// Token responses are credentials; no intermediary may cache them.
		c.Response().Header().Set("Cache-Control", "no-store")

		var req oauthTokenRequest
		if err := c.Bind(&req); err != nil {
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, "malformed request body")
		}

		form, msg := tokenForm(req, clientID)
		if msg != "" {
			return jsonError(c, http.StatusBadRequest, codeInvalidRequest, msg)
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), oauthTokenTimeout)
		defer cancel()

		body, status, err := exchanger.ExchangeToken(ctx, form)
		if err != nil {
			// The error never contains the code or any token — only
			// transport-level context from the auth client.
			return jsonError(c, http.StatusBadGateway, codeUpstreamError, err.Error())
		}

		// Relay the auth server's JSON (success or structured OAuth error)
		// with its original status so the SPA can handle it per spec.
		if err := c.JSONBlob(status, body); err != nil {
			return fmt.Errorf("write token response: %w", err)
		}
		return nil
	}
}

// tokenForm validates the request and builds the upstream form. Only the
// two grant types the SPA uses are accepted.
func tokenForm(req oauthTokenRequest, clientID string) (form url.Values, msg string) {
	switch req.GrantType {
	case "authorization_code":
		if req.Code == "" || req.CodeVerifier == "" || req.RedirectURI == "" {
			return nil, "authorization_code grant requires code, code_verifier, and redirect_uri"
		}
		return url.Values{
			"grant_type":    {req.GrantType},
			"client_id":     {clientID},
			"code":          {req.Code},
			"code_verifier": {req.CodeVerifier},
			"redirect_uri":  {req.RedirectURI},
		}, ""
	case "refresh_token":
		if req.RefreshToken == "" {
			return nil, "refresh_token grant requires refresh_token"
		}
		return url.Values{
			"grant_type":    {req.GrantType},
			"client_id":     {clientID},
			"refresh_token": {req.RefreshToken},
		}, ""
	default:
		return nil, `grant_type must be "authorization_code" or "refresh_token"`
	}
}
