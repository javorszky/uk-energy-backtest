package octopus

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProductCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{in: "E-1R-VAR-19-04-12-N", want: "VAR-19-04-12"},
		{in: "E-1R-FLUX-IMPORT-23-02-14-C", want: "FLUX-IMPORT-23-02-14"},
		{in: "E-2R-VAR-19-04-12-A", want: "VAR-19-04-12"},
		{in: "G-1R-VAR-22-11-01-A", want: "VAR-22-11-01"},
		{in: "AGILE-FLEX", wantErr: true},
		{in: "X-1R-FOO-A", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tc := range tests {
		got, err := ProductCode(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ProductCode(%q) succeeded with %q, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ProductCode(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ProductCode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCurrentTariffCodes(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantBasicAuth(t, r)
		fmt.Fprint(w, `{
			"properties": [{
				"electricity_meter_points": [
					{"mpan": "1111", "is_export": false, "meters": [{"serial_number": "I1"}], "agreements": [
						{"tariff_code": "E-1R-OLD-18-01-01-C", "valid_from": "2018-01-01T00:00:00Z", "valid_to": "2023-03-01T00:00:00Z"},
						{"tariff_code": "E-1R-FLUX-IMPORT-23-02-14-C", "valid_from": "2023-03-01T00:00:00Z", "valid_to": null}
					]},
					{"mpan": "2222", "is_export": true, "meters": [{"serial_number": "E1"}], "agreements": [
						{"tariff_code": "E-1R-FLUX-EXPORT-23-02-14-C", "valid_from": "2023-03-01T00:00:00Z"}
					]}
				],
				"gas_meter_points": [
					{"mprn": "3333", "meters": [{"serial_number": "G1"}], "agreements": [
						{"tariff_code": "G-1R-VAR-22-11-01-C", "valid_from": "2022-12-01T00:00:00Z", "valid_to": null},
						{"tariff_code": "G-1R-FUTURE-27-01-01-C", "valid_from": "2027-01-01T00:00:00Z", "valid_to": null}
					]}
				]
			}]
		}`)
	}))
	defer srv.Close()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	c := newClientWithBase(srv.URL)
	codes, err := c.CurrentTariffCodes(t.Context(), testKey, "A-1", now)
	if err != nil {
		t.Fatalf("CurrentTariffCodes: %v", err)
	}
	want := TariffCodes{
		Import: "E-1R-FLUX-IMPORT-23-02-14-C",
		Export: "E-1R-FLUX-EXPORT-23-02-14-C",
		Gas:    "G-1R-VAR-22-11-01-C",
	}
	if codes != want {
		t.Errorf("codes = %+v, want %+v", codes, want)
	}
}

func TestCurrentTariffCodesNoLiveAgreement(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"properties": [{"electricity_meter_points": [
			{"mpan": "1111", "is_export": false, "meters": [{"serial_number": "I1"}], "agreements": [
				{"tariff_code": "E-1R-OLD-18-01-01-C", "valid_from": "2018-01-01T00:00:00Z", "valid_to": "2020-01-01T00:00:00Z"}
			]}
		]}]}`)
	}))
	defer srv.Close()

	c := newClientWithBase(srv.URL)
	if _, err := c.CurrentTariffCodes(t.Context(), testKey, "A-1", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Error("want error when no live import agreement exists")
	}
}

func TestCurrentStandingChargePrefersDirectDebit(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/products/FLUX-IMPORT-23-02-14/electricity-tariffs/E-1R-FLUX-IMPORT-23-02-14-C/standing-charges/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"results": [
			{"value_inc_vat": 60.0, "valid_from": "2023-01-01T00:00:00Z", "valid_to": null, "payment_method": "NON_DIRECT_DEBIT"},
			{"value_inc_vat": 47.85, "valid_from": "2023-01-01T00:00:00Z", "valid_to": null, "payment_method": "DIRECT_DEBIT"}
		]}`)
	}))
	defer srv.Close()

	c := newClientWithBase(srv.URL)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	got, err := c.CurrentStandingCharge(t.Context(), testKey, "E-1R-FLUX-IMPORT-23-02-14-C", false, now)
	if err != nil {
		t.Fatalf("CurrentStandingCharge: %v", err)
	}
	if got != 47.85 {
		t.Errorf("standing charge = %v, want 47.85 (direct debit preferred)", got)
	}
}

func TestCurrentGasUnitRate(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/products/VAR-22-11-01/gas-tariffs/G-1R-VAR-22-11-01-C/standard-unit-rates/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"results": [
			{"value_inc_vat": 6.1, "valid_from": "2024-01-01T00:00:00Z", "valid_to": null}
		]}`)
	}))
	defer srv.Close()

	c := newClientWithBase(srv.URL)
	got, err := c.CurrentGasUnitRate(t.Context(), testKey, "G-1R-VAR-22-11-01-C", time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CurrentGasUnitRate: %v", err)
	}
	if got != 6.1 {
		t.Errorf("gas rate = %v, want 6.1", got)
	}
}

func TestUnitRateBucketsTimeOfUse(t *testing.T) {
	t.Parallel()

	// A Go-style tariff: 9p between 00:30 and 04:30 UTC (winter = local),
	// 30p otherwise, published as long-lived overlapping windows the way
	// Octopus does: one entry per daily window in the requested range.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var entries []string
		// Generate per-day cheap windows covering the queried period.
		for _, day := range []string{"2026-01-14", "2026-01-15", "2026-01-16"} {
			entries = append(entries,
				fmt.Sprintf(`{"value_inc_vat": 9.0, "valid_from": "%sT00:30:00Z", "valid_to": "%sT04:30:00Z"}`, day, day))
		}
		// The default rate as one open-ended entry would overlap the cheap
		// windows; Octopus instead publishes the expensive stretches. For
		// the test, emit them explicitly.
		entries = append(entries,
			`{"value_inc_vat": 30.0, "valid_from": "2026-01-13T04:30:00Z", "valid_to": "2026-01-14T00:30:00Z"}`,
			`{"value_inc_vat": 30.0, "valid_from": "2026-01-14T04:30:00Z", "valid_to": "2026-01-15T00:30:00Z"}`,
			`{"value_inc_vat": 30.0, "valid_from": "2026-01-15T04:30:00Z", "valid_to": "2026-01-16T00:30:00Z"}`,
		)
		fmt.Fprint(w, `{"results": [`+strings.Join(entries, ",")+`]}`)
	}))
	defer srv.Close()

	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	c := newClientWithBase(srv.URL)
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	rates, err := c.UnitRateBuckets(t.Context(), testKey, "E-1R-GO-VAR-22-10-14-C", now, london)
	if err != nil {
		t.Fatalf("UnitRateBuckets: %v", err)
	}

	for i := range rates {
		want := 30.0
		if i >= 1 && i <= 8 { // local 00:30–04:30 in winter = UTC
			want = 9.0
		}
		if rates[i] != want {
			t.Errorf("rates[%d] = %v, want %v", i, rates[i], want)
		}
	}
}

func TestUnitRateBucketsFlat(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"results": [
			{"value_inc_vat": 24.5, "valid_from": "2024-01-01T00:00:00Z", "valid_to": null}
		]}`)
	}))
	defer srv.Close()

	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	c := newClientWithBase(srv.URL)
	rates, err := c.UnitRateBuckets(t.Context(), testKey, "E-1R-VAR-22-11-01-C", time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), london)
	if err != nil {
		t.Fatalf("UnitRateBuckets: %v", err)
	}
	for i, v := range rates {
		if v != 24.5 {
			t.Errorf("rates[%d] = %v, want 24.5", i, v)
		}
	}
}

func TestRatesURLRejectsInvalidTariffCode(t *testing.T) {
	t.Parallel()

	c := newClientWithBase("http://unused.invalid")
	if _, err := c.CurrentStandingCharge(t.Context(), testKey, "../evil", false, time.Now()); err == nil {
		t.Error("accepted invalid tariff code, want error")
	}
}
