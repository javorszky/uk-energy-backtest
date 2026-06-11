package octopus

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestExchangeTokenForwardsFormAndRelaysResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token/" {
			t.Errorf("path = %q, want /token/", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("code") != "abc" {
			t.Errorf("form = %v", r.PostForm)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"access_token": "at"}`)
	}))
	defer srv.Close()

	c := newOAuthClientWithBase(srv.URL)
	body, status, err := c.ExchangeToken(t.Context(), url.Values{
		"grant_type": {"authorization_code"},
		"code":       {"abc"},
	})
	if err != nil {
		t.Fatalf("ExchangeToken: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d", status)
	}
	if string(body) != `{"access_token": "at"}` {
		t.Errorf("body = %s", body)
	}
}

func TestExchangeTokenRelaysOAuthErrorStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error": "invalid_grant"}`)
	}))
	defer srv.Close()

	c := newOAuthClientWithBase(srv.URL)
	body, status, err := c.ExchangeToken(t.Context(), url.Values{"grant_type": {"refresh_token"}})
	if err != nil {
		t.Fatalf("ExchangeToken: %v", err)
	}
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 relayed", status)
	}
	if string(body) != `{"error": "invalid_grant"}` {
		t.Errorf("body = %s", body)
	}
}
