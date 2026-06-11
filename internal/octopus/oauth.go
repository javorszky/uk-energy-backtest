package octopus

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// defaultAuthBaseURL is the Octopus OAuth 2.0 server. Hardcoded for the
	// same reason as the API base: no request-derived value may choose the
	// upstream host.
	defaultAuthBaseURL = "https://auth.octopus.energy"

	// AuthorizeURL is where the browser is sent to start the PKCE flow. The
	// frontend appends client_id, redirect_uri, scope, state, and the code
	// challenge.
	AuthorizeURL = defaultAuthBaseURL + "/authorize/"

	// maxTokenBodyBytes caps the token endpoint response we will read; token
	// payloads are tiny.
	maxTokenBodyBytes = 1 << 20
)

// OAuthClient exchanges authorization codes (and refresh tokens) at the
// Octopus auth server on behalf of the SPA. The app is a public client, so
// there is no client secret anywhere — PKCE protects the exchange. The
// backend only forwards the exchange because the auth server's CORS policy
// for browser-direct token requests is not guaranteed; nothing from the
// response is retained.
type OAuthClient struct {
	http *http.Client
	base string
}

// NewOAuthClient returns an OAuthClient against the production auth server.
func NewOAuthClient(timeout time.Duration) *OAuthClient {
	return &OAuthClient{
		http: &http.Client{Timeout: timeout},
		base: defaultAuthBaseURL,
	}
}

// newOAuthClientWithBase exists for tests only.
func newOAuthClientWithBase(base string) *OAuthClient {
	return &OAuthClient{
		http: &http.Client{Timeout: time.Second},
		base: base,
	}
}

// ExchangeToken forwards a token-grant form to the auth server and returns
// the raw response body and status verbatim, so the SPA sees exactly what
// the auth server said (including structured OAuth error bodies). The
// caller has already validated the grant type and injected the client id.
func (c *OAuthClient) ExchangeToken(ctx context.Context, form url.Values) (body []byte, status int, err error) {
	u, err := url.JoinPath(c.base, "token")
	if err != nil {
		return nil, 0, fmt.Errorf("build token url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u+"/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("call auth server: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body close

	body, err = io.ReadAll(io.LimitReader(resp.Body, maxTokenBodyBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("read token response: %w", err)
	}
	return body, resp.StatusCode, nil
}
