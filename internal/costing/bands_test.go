package costing_test

import (
	"testing"

	"github.com/javorszky/uk-energy-backtest/internal/costing"
)

func ratesWith(def float64, set map[int]float64) *[costing.BucketsPerDay]float64 {
	var r [costing.BucketsPerDay]float64
	for i := range r {
		r[i] = def
	}
	for i, v := range set {
		r[i] = v
	}
	return &r
}

func TestBandsFromRatesFlat(t *testing.T) {
	t.Parallel()

	def, bands := costing.BandsFromRates(ratesWith(25, nil))
	if def != 25 {
		t.Errorf("def = %v, want 25", def)
	}
	if len(bands) != 0 {
		t.Errorf("bands = %v, want none", bands)
	}
}

func TestBandsFromRatesSingleBand(t *testing.T) {
	t.Parallel()

	// Cheap rate 02:00–05:00 = buckets 4..9.
	set := map[int]float64{}
	for i := 4; i <= 9; i++ {
		set[i] = 7
	}
	def, bands := costing.BandsFromRates(ratesWith(28, set))
	if def != 28 {
		t.Errorf("def = %v, want 28", def)
	}
	if len(bands) != 1 {
		t.Fatalf("bands = %v, want exactly one", bands)
	}
	want := costing.Band{From: "02:00", To: "05:00", Rate: 7}
	if bands[0] != want {
		t.Errorf("band = %+v, want %+v", bands[0], want)
	}
}

func TestBandsFromRatesFluxShape(t *testing.T) {
	t.Parallel()

	// Flux: cheap 02:00–05:00, peak 16:00–19:00, default elsewhere.
	set := map[int]float64{}
	for i := 4; i <= 9; i++ {
		set[i] = 16
	}
	for i := 32; i <= 37; i++ {
		set[i] = 45
	}
	def, bands := costing.BandsFromRates(ratesWith(30, set))
	if def != 30 {
		t.Errorf("def = %v, want 30", def)
	}
	if len(bands) != 2 {
		t.Fatalf("bands = %v, want two", bands)
	}
	if bands[0] != (costing.Band{From: "02:00", To: "05:00", Rate: 16}) {
		t.Errorf("bands[0] = %+v", bands[0])
	}
	if bands[1] != (costing.Band{From: "16:00", To: "19:00", Rate: 45}) {
		t.Errorf("bands[1] = %+v", bands[1])
	}
}

func TestBandsFromRatesWrapMidnight(t *testing.T) {
	t.Parallel()

	// EV overnight 23:30–05:30: buckets 47, 0..10.
	set := map[int]float64{47: 7}
	for i := 0; i <= 10; i++ {
		set[i] = 7
	}
	def, bands := costing.BandsFromRates(ratesWith(28, set))
	if def != 28 {
		t.Errorf("def = %v, want 28", def)
	}
	if len(bands) != 1 {
		t.Fatalf("bands = %v, want one wrap band", bands)
	}
	want := costing.Band{From: "23:30", To: "05:30", Rate: 7}
	if bands[0] != want {
		t.Errorf("band = %+v, want %+v", bands[0], want)
	}
}

func TestBandsFromRatesRoundTripThroughCost(t *testing.T) {
	t.Parallel()

	// Derived bands fed back through the costing engine must reproduce the
	// original rate table exactly.
	set := map[int]float64{47: 7, 0: 7, 1: 7, 32: 45, 33: 45}
	original := ratesWith(30, set)
	def, bands := costing.BandsFromRates(original)

	tariff := costing.Tariff{
		Name:        "roundtrip",
		Electricity: costing.Electricity{ImportDefault: def, ImportBands: bands},
	}
	r, err := costing.Cost(&costing.Profile{}, tariff)
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if r.ImportRates != *original {
		t.Errorf("round-trip mismatch:\n got %v\nwant %v", r.ImportRates, *original)
	}
}

func TestDistinctAndMeanRates(t *testing.T) {
	t.Parallel()

	r := ratesWith(10, map[int]float64{0: 20, 1: 30})
	if got := costing.DistinctRates(r); got != 3 {
		t.Errorf("DistinctRates = %d, want 3", got)
	}
	// 46×10 + 20 + 30 = 510; /48 = 10.625.
	if got := costing.MeanRate(r); got != 510.0/48 {
		t.Errorf("MeanRate = %v, want %v", got, 510.0/48)
	}
}
