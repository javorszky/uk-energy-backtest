package octopus

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testKey = "sk_test_abc123"

func wantBasicAuth(t *testing.T, r *http.Request) {
	t.Helper()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(testKey+":"))
	if got := r.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want %q (key as username, empty password)", got, want)
	}
}

func TestDiscoverMeters(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantBasicAuth(t, r)
		if r.URL.Path != "/accounts/A-1234ABCD/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{
			"properties": [{
				"electricity_meter_points": [
					{"mpan": "1111", "is_export": false, "meters": [{"serial_number": "OLD1"}, {"serial_number": "IMP1"}]},
					{"mpan": "2222", "is_export": true, "meters": [{"serial_number": "EXP1"}]}
				],
				"gas_meter_points": [
					{"mprn": "3333", "meters": [{"serial_number": "GAS1"}]}
				]
			}]
		}`)
	}))
	defer srv.Close()

	c := newClientWithBase(srv.URL)
	mp, err := c.DiscoverMeters(t.Context(), testKey, "A-1234ABCD")
	if err != nil {
		t.Fatalf("DiscoverMeters: %v", err)
	}

	want := MeterPoints{
		ImportMPAN: "1111", ImportSerial: "IMP1",
		ExportMPAN: "2222", ExportSerial: "EXP1",
		GasMPRN: "3333", GasSerial: "GAS1",
	}
	if mp != want {
		t.Errorf("MeterPoints = %+v, want %+v", mp, want)
	}
}

func TestDiscoverMetersRejectsInvalidAccount(t *testing.T) {
	t.Parallel()

	c := newClientWithBase("http://unused.invalid")
	for _, bad := range []string{"", "../etc", "a/b", "a?x=1", "a b", "a%2f"} {
		if _, err := c.DiscoverMeters(t.Context(), testKey, bad); err == nil {
			t.Errorf("DiscoverMeters accepted account %q, want error", bad)
		}
	}
}

func TestDiscoverMetersNoImportMeter(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"properties": []}`)
	}))
	defer srv.Close()

	c := newClientWithBase(srv.URL)
	if _, err := c.DiscoverMeters(t.Context(), testKey, "A-1"); err == nil {
		t.Error("DiscoverMeters succeeded on account with no meters, want error")
	}
}

func TestConsumptionPagination(t *testing.T) {
	t.Parallel()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantBasicAuth(t, r)
		if r.URL.Path != "/electricity-meter-points/1111/meters/IMP1/consumption/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("order_by") != "period" || q.Get("page_size") != "25000" {
			t.Errorf("query = %v", q)
		}
		if q.Get("period_from") != "2024-01-01T00:00:00Z" {
			t.Errorf("period_from = %q, want UTC Z format", q.Get("period_from"))
		}

		if q.Get("page") == "2" {
			fmt.Fprint(w, `{"next": "", "results": [
				{"consumption": 0.3, "interval_start": "2024-01-01T01:00:00Z"}
			]}`)
			return
		}
		// First page points at page 2 via an absolute next URL.
		fmt.Fprintf(w, `{"next": %q, "results": [
			{"consumption": 0.1, "interval_start": "2024-01-01T00:00:00Z"},
			{"consumption": 0.2, "interval_start": "2024-01-01T00:30:00Z"}
		]}`, srv.URL+r.URL.Path+"?"+q.Encode()+"&page=2")
	}))
	defer srv.Close()

	c := newClientWithBase(srv.URL)
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	readings, err := c.Consumption(t.Context(), testKey, "1111", "IMP1", from, to, false)
	if err != nil {
		t.Fatalf("Consumption: %v", err)
	}
	if len(readings) != 3 {
		t.Fatalf("got %d readings, want 3", len(readings))
	}
	if readings[2].Consumption != 0.3 {
		t.Errorf("readings[2].Consumption = %v, want 0.3", readings[2].Consumption)
	}
}

func TestConsumptionNeverFollowsCrossHostNext(t *testing.T) {
	t.Parallel()

	// The upstream responds with a `next` pointing at an attacker host. The
	// client must keep calling its own base (it reuses only query params), so
	// the second request still arrives at our test server.
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
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := c.Consumption(t.Context(), testKey, "1111", "IMP1", from, from, false); err != nil {
		t.Fatalf("Consumption: %v", err)
	}
	if calls != 2 {
		t.Errorf("upstream called %d times, want 2", calls)
	}
}

func TestConsumptionGasEndpoint(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gas-meter-points/3333/meters/GAS1/consumption/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"next": "", "results": []}`)
	}))
	defer srv.Close()

	c := newClientWithBase(srv.URL)
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := c.Consumption(t.Context(), testKey, "3333", "GAS1", from, from, true); err != nil {
		t.Fatalf("Consumption: %v", err)
	}
}

func TestConsumptionRejectsInvalidIDs(t *testing.T) {
	t.Parallel()

	c := newClientWithBase("http://unused.invalid")
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := c.Consumption(t.Context(), testKey, "../x", "S1", from, from, false); err == nil {
		t.Error("accepted invalid point id, want error")
	}
	if _, err := c.Consumption(t.Context(), testKey, "1111", "a/b", from, from, false); err == nil {
		t.Error("accepted invalid serial, want error")
	}
}

func TestUpstreamErrorIsWrappedWithoutKey(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newClientWithBase(srv.URL)
	_, err := c.DiscoverMeters(t.Context(), testKey, "A-1")
	if err == nil {
		t.Fatal("want error on 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q does not mention upstream status", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("error %q leaks the API key", err)
	}
}

func TestBearerCredentialUsesAuthorizationHeader(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok123" {
			t.Errorf("Authorization = %q, want bearer token passthrough", got)
		}
		fmt.Fprint(w, `{"properties": [{"electricity_meter_points": [
			{"mpan": "1111", "is_export": false, "meters": [{"serial_number": "I1"}]}
		]}]}`)
	}))
	defer srv.Close()

	c := newClientWithBase(srv.URL)
	if _, err := c.DiscoverMeters(t.Context(), "Bearer tok123", "A-1"); err != nil {
		t.Fatalf("DiscoverMeters: %v", err)
	}
}
