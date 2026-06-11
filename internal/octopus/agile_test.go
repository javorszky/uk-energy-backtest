package octopus

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const agileProduct = "AGILE-24-10-01"

func TestRateSeriesPaginatesAndIsUnauthenticated(t *testing.T) {
	t.Parallel()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want none on public rates", got)
		}
		if r.URL.Path != "/products/AGILE-24-10-01/electricity-tariffs/E-1R-AGILE-24-10-01-C/standard-unit-rates/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `{"next": "", "results": [
				{"value_inc_vat": 12.5, "valid_from": "2026-01-01T01:00:00Z", "valid_to": "2026-01-01T01:30:00Z"}
			]}`)
			return
		}
		fmt.Fprintf(w, `{"next": %q, "results": [
			{"value_inc_vat": 10.0, "valid_from": "2026-01-01T00:00:00Z", "valid_to": "2026-01-01T00:30:00Z"},
			{"value_inc_vat": 11.0, "valid_from": "2026-01-01T00:30:00Z", "valid_to": "2026-01-01T01:00:00Z"}
		]}`, srv.URL+r.URL.Path+"?"+r.URL.RawQuery+"&page=2")
	}))
	defer srv.Close()

	c := newClientWithBase(srv.URL)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	points, err := c.RateSeries(t.Context(), agileProduct, "C", LeafUnitRates, from, from.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("RateSeries: %v", err)
	}
	if len(points) != 3 {
		t.Fatalf("got %d points, want 3", len(points))
	}
	if points[2].Rate != 12.5 {
		t.Errorf("points[2].Rate = %v, want 12.5 (second page)", points[2].Rate)
	}
}

func TestRateSeriesPrefersDirectDebitDuplicates(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"next": "", "results": [
			{"value_inc_vat": 60.0, "valid_from": "2026-01-01T00:00:00Z", "valid_to": null, "payment_method": "NON_DIRECT_DEBIT"},
			{"value_inc_vat": 47.85, "valid_from": "2026-01-01T00:00:00Z", "valid_to": null, "payment_method": "DIRECT_DEBIT"}
		]}`)
	}))
	defer srv.Close()

	c := newClientWithBase(srv.URL)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	points, err := c.RateSeries(t.Context(), "VAR-22-11-01", "C", LeafStandingCharges, from, from.Add(time.Hour))
	if err != nil {
		t.Fatalf("RateSeries: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("got %d points, want 1 deduped", len(points))
	}
	if points[0].Rate != 47.85 {
		t.Errorf("rate = %v, want direct-debit 47.85", points[0].Rate)
	}
}

func TestRateSeriesValidation(t *testing.T) {
	t.Parallel()

	c := newClientWithBase("http://unused.invalid")
	from := time.Now()
	for _, tc := range []struct{ product, region, leaf string }{
		{"../evil", "C", LeafUnitRates},
		{agileProduct, "I", LeafUnitRates}, // I is not a GSP region
		{agileProduct, "CC", LeafUnitRates},
		{agileProduct, "C", "day-unit-rates"}, // not an allowed leaf
	} {
		if _, err := c.RateSeries(t.Context(), tc.product, tc.region, tc.leaf, from, from); err == nil {
			t.Errorf("RateSeries(%q, %q, %q) succeeded, want error", tc.product, tc.region, tc.leaf)
		}
	}
}

func TestRateSeriesNeverFollowsCrossHostNext(t *testing.T) {
	t.Parallel()

	calls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			fmt.Fprint(w, `{"next": "http://evil.invalid/steal?page=2", "results": []}`)
			return
		}
		if r.Host != strings.TrimPrefix(srv.URL, "http://") {
			t.Errorf("second request went to host %q", r.Host)
		}
		fmt.Fprint(w, `{"next": "", "results": []}`)
	}))
	defer srv.Close()

	c := newClientWithBase(srv.URL)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := c.RateSeries(t.Context(), agileProduct, "C", LeafUnitRates, from, from); err != nil {
		t.Fatalf("RateSeries: %v", err)
	}
	if calls != 2 {
		t.Errorf("upstream called %d times, want 2", calls)
	}
}
