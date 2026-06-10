package costing_test

import (
	"math"
	"strings"
	"testing"

	"github.com/javorszky/uk-energy-backtest/internal/costing"
)

const moneyEps = 1e-9

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= moneyEps
}

// Band fixture times appear repeatedly; named to satisfy goconst.
const (
	twoAM  = "02:00"
	fiveAM = "05:00"
)

// emptyProfile returns a pointer to a zero-usage profile for rate-table tests.
func emptyProfile() *costing.Profile { return &costing.Profile{} }

// uniformProfile returns a profile with every import bucket holding kwh.
func uniformProfile(kwh float64, days int) costing.Profile {
	p := costing.Profile{SuppliedDays: days}
	for i := range p.ImportHH {
		p.ImportHH[i] = kwh
	}
	return p
}

func TestCostEmptyBandsUsesDefaultEverywhere(t *testing.T) {
	t.Parallel()

	// Acceptance criterion: a tariff with import_bands: [] prices every
	// bucket at import_default.
	p := uniformProfile(1.0, 1)
	tariff := costing.Tariff{
		Name: "flat",
		Electricity: costing.Electricity{
			StandingCharge: 10,
			ImportDefault:  25,
			ImportBands:    []costing.Band{},
		},
	}

	r, err := costing.Cost(&p, tariff)
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if !almostEqual(r.ImportPence, 48*25.0) {
		t.Errorf("ImportPence = %v, want %v", r.ImportPence, 48*25.0)
	}
	for i, rate := range r.ImportRates {
		if rate != 25 {
			t.Errorf("ImportRates[%d] = %v, want 25", i, rate)
		}
	}
	if !almostEqual(r.StandingPence, 10) {
		t.Errorf("StandingPence = %v, want 10", r.StandingPence)
	}
	if !almostEqual(r.NetPence, 48*25.0+10) {
		t.Errorf("NetPence = %v, want %v", r.NetPence, 48*25.0+10)
	}
}

func TestCostBandMatching(t *testing.T) {
	t.Parallel()

	tariff := costing.Tariff{
		Name: "banded",
		Electricity: costing.Electricity{
			ImportDefault: 30,
			ImportBands: []costing.Band{
				{From: twoAM, To: fiveAM, Rate: 15},
				// Overlaps the first band's tail: first match must win.
				{From: "04:00", To: "06:00", Rate: 99},
			},
		},
	}

	r, err := costing.Cost(emptyProfile(), tariff)
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}

	// Bucket 4 = 02:00 — band start is inclusive.
	if r.ImportRates[4] != 15 {
		t.Errorf("rate at 02:00 = %v, want 15", r.ImportRates[4])
	}
	// Bucket 9 = 04:30 — covered by the first band; overlap loses.
	if r.ImportRates[9] != 15 {
		t.Errorf("rate at 04:30 = %v, want 15 (first match wins)", r.ImportRates[9])
	}
	// Bucket 10 = 05:00 — band end is exclusive; second band covers it.
	if r.ImportRates[10] != 99 {
		t.Errorf("rate at 05:00 = %v, want 99", r.ImportRates[10])
	}
	// Bucket 12 = 06:00 — outside all bands.
	if r.ImportRates[12] != 30 {
		t.Errorf("rate at 06:00 = %v, want default 30", r.ImportRates[12])
	}
	// Bucket 3 = 01:30 — before any band.
	if r.ImportRates[3] != 30 {
		t.Errorf("rate at 01:30 = %v, want default 30", r.ImportRates[3])
	}
}

func TestCostWrapMidnightBand(t *testing.T) {
	t.Parallel()

	tariff := costing.Tariff{
		Name: "wrap",
		Electricity: costing.Electricity{
			ImportDefault: 30,
			ImportBands:   []costing.Band{{From: "23:00", To: "01:00", Rate: 12}},
		},
	}

	r, err := costing.Cost(emptyProfile(), tariff)
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}

	wantBand := map[int]bool{46: true, 47: true, 0: true, 1: true}
	for i, rate := range r.ImportRates {
		want := 30.0
		if wantBand[i] {
			want = 12.0
		}
		if rate != want {
			t.Errorf("ImportRates[%d] = %v, want %v", i, rate, want)
		}
	}
}

func TestCostZeroWidthBandNeverMatches(t *testing.T) {
	t.Parallel()

	tariff := costing.Tariff{
		Name: "zero-width",
		Electricity: costing.Electricity{
			ImportDefault: 30,
			ImportBands:   []costing.Band{{From: twoAM, To: twoAM, Rate: 1}},
		},
	}
	r, err := costing.Cost(emptyProfile(), tariff)
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	for i, rate := range r.ImportRates {
		if rate != 30 {
			t.Errorf("ImportRates[%d] = %v, want default 30 (zero-width band)", i, rate)
		}
	}
}

func TestCostRejectsSubHalfHourBoundary(t *testing.T) {
	t.Parallel()

	tariff := costing.Tariff{
		Name: "bad",
		Electricity: costing.Electricity{
			ImportBands: []costing.Band{{From: "02:15", To: fiveAM, Rate: 15}},
		},
	}
	_, err := costing.Cost(&costing.Profile{}, tariff)
	if err == nil {
		t.Fatal("Cost accepted a :15 band boundary, want error")
	}
	if !strings.Contains(err.Error(), ":00 or :30") {
		t.Errorf("error %q does not mention the :00/:30 constraint", err)
	}
}

func TestCostRejectsMalformedTimes(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"", "2:00", "25:00", "02:60", "ab:cd", "02-00", "02:00:00"} {
		tariff := costing.Tariff{
			Electricity: costing.Electricity{
				ExportBands: []costing.Band{{From: bad, To: fiveAM, Rate: 1}},
			},
		}
		if _, err := costing.Cost(&costing.Profile{}, tariff); err == nil {
			t.Errorf("Cost accepted band time %q, want error", bad)
		}
	}
}

func TestCostExportIsACredit(t *testing.T) {
	t.Parallel()

	var exportHH [costing.BucketsPerDay]float64
	exportHH[20] = 2.0
	p := costing.Profile{SuppliedDays: 1, ExportHH: &exportHH}
	p.ImportHH[20] = 1.0

	tariff := costing.Tariff{
		Name: "credit",
		Electricity: costing.Electricity{
			StandingCharge: 40,
			ImportDefault:  30,
			ExportDefault:  15,
		},
	}

	r, err := costing.Cost(&p, tariff)
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if !almostEqual(r.ExportCreditP, 30) {
		t.Errorf("ExportCreditP = %v, want 30 (positive)", r.ExportCreditP)
	}
	// Net = 30 (import) + 40 (standing) − 30 (export) = 40. Subtracted once.
	if !almostEqual(r.NetPence, 40) {
		t.Errorf("NetPence = %v, want 40", r.NetPence)
	}
}

func TestCostNoExportMeter(t *testing.T) {
	t.Parallel()

	p := costing.Profile{SuppliedDays: 1}
	p.ImportHH[0] = 1.0
	tariff := costing.Tariff{
		Electricity: costing.Electricity{ImportDefault: 10, ExportDefault: 15},
	}
	r, err := costing.Cost(&p, tariff)
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if r.ExportCreditP != 0 {
		t.Errorf("ExportCreditP = %v, want 0 for dataset with no export", r.ExportCreditP)
	}
}

func TestCostGasHandling(t *testing.T) {
	t.Parallel()

	t.Run("tariff without gas block prices gas at zero", func(t *testing.T) {
		t.Parallel()
		p := costing.Profile{SuppliedDays: 2, GasKWh: 100}
		tariff := costing.Tariff{
			Electricity: costing.Electricity{StandingCharge: 50},
		}
		r, err := costing.Cost(&p, tariff)
		if err != nil {
			t.Fatalf("Cost: %v", err)
		}
		if r.GasPence != 0 {
			t.Errorf("GasPence = %v, want 0", r.GasPence)
		}
		if !almostEqual(r.StandingPence, 100) {
			t.Errorf("StandingPence = %v, want 100 (no gas standing)", r.StandingPence)
		}
	})

	t.Run("gas standing skipped when dataset has no gas", func(t *testing.T) {
		t.Parallel()
		p := costing.Profile{SuppliedDays: 2}
		tariff := costing.Tariff{
			Electricity: costing.Electricity{StandingCharge: 50},
			Gas:         &costing.Gas{StandingCharge: 30, Rate: 6},
		}
		r, err := costing.Cost(&p, tariff)
		if err != nil {
			t.Fatalf("Cost: %v", err)
		}
		if r.GasPence != 0 {
			t.Errorf("GasPence = %v, want 0", r.GasPence)
		}
		if !almostEqual(r.StandingPence, 100) {
			t.Errorf("StandingPence = %v, want 100 (gas standing skipped)", r.StandingPence)
		}
	})

	t.Run("dual fuel charges both standings", func(t *testing.T) {
		t.Parallel()
		p := costing.Profile{SuppliedDays: 2, GasKWh: 10}
		tariff := costing.Tariff{
			Electricity: costing.Electricity{StandingCharge: 50},
			Gas:         &costing.Gas{StandingCharge: 30, Rate: 6},
		}
		r, err := costing.Cost(&p, tariff)
		if err != nil {
			t.Fatalf("Cost: %v", err)
		}
		if !almostEqual(r.GasPence, 60) {
			t.Errorf("GasPence = %v, want 60", r.GasPence)
		}
		if !almostEqual(r.StandingPence, 160) {
			t.Errorf("StandingPence = %v, want 160", r.StandingPence)
		}
	})
}
