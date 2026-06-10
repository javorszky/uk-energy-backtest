package costing_test

import (
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	"github.com/javorszky/uk-energy-backtest/internal/costing"
)

// fixturePath is the shared cross-language contract fixture; the Vitest suite
// (frontend/src/lib/__tests__/fixture.spec.ts) consumes the same file so the
// TS profile-build and the Go costing phases provably agree.
const fixturePath = "../../testdata/shared/profile_fixture.json"

type fixtureReading struct {
	IntervalStart time.Time `json:"interval_start"`
	Consumption   float64   `json:"consumption"`
}

type fixture struct {
	GasUnit  string `json:"gas_unit"`
	Readings struct {
		Import []fixtureReading `json:"import"`
		Export []fixtureReading `json:"export"`
		Gas    []fixtureReading `json:"gas"`
	} `json:"readings"`
	Tariffs         []costing.Tariff `json:"tariffs"`
	ExpectedResults []struct {
		Name          string  `json:"name"`
		ImportPence   float64 `json:"import_p"`
		ExportCreditP float64 `json:"export_credit_p"`
		GasPence      float64 `json:"gas_p"`
		StandingPence float64 `json:"standing_p"`
		NetPence      float64 `json:"net_p"`
	} `json:"expected_results"`
	ExpectedProfile costing.Profile `json:"expected_profile"`
	CalorificValue  float64         `json:"calorific_value"`
}

func loadFixture(t *testing.T) fixture {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return f
}

func toReadings(frs []fixtureReading) []costing.Reading {
	out := make([]costing.Reading, len(frs))
	for i, fr := range frs {
		out[i] = costing.Reading{IntervalStart: fr.IntervalStart, Consumption: fr.Consumption}
	}
	return out
}

func TestFixtureBuildProfile(t *testing.T) {
	t.Parallel()

	f := loadFixture(t)
	p, err := costing.BuildProfile(
		toReadings(f.Readings.Import),
		toReadings(f.Readings.Export),
		toReadings(f.Readings.Gas),
		f.GasUnit == "m3",
		f.CalorificValue,
		london(t),
	)
	if err != nil {
		t.Fatalf("BuildProfile: %v", err)
	}

	if p.SuppliedDays != f.ExpectedProfile.SuppliedDays {
		t.Errorf("SuppliedDays = %d, want %d", p.SuppliedDays, f.ExpectedProfile.SuppliedDays)
	}
	for i := range costing.BucketsPerDay {
		if math.Abs(p.ImportHH[i]-f.ExpectedProfile.ImportHH[i]) > moneyEps {
			t.Errorf("ImportHH[%d] = %v, want %v", i, p.ImportHH[i], f.ExpectedProfile.ImportHH[i])
		}
	}
	if p.ExportHH == nil || f.ExpectedProfile.ExportHH == nil {
		t.Fatalf("ExportHH nil: got %v, want %v", p.ExportHH == nil, f.ExpectedProfile.ExportHH == nil)
	}
	for i := range costing.BucketsPerDay {
		if math.Abs(p.ExportHH[i]-f.ExpectedProfile.ExportHH[i]) > moneyEps {
			t.Errorf("ExportHH[%d] = %v, want %v", i, p.ExportHH[i], f.ExpectedProfile.ExportHH[i])
		}
	}
	if math.Abs(p.GasKWh-f.ExpectedProfile.GasKWh) > moneyEps {
		t.Errorf("GasKWh = %v, want %v", p.GasKWh, f.ExpectedProfile.GasKWh)
	}
}

func TestFixtureCost(t *testing.T) {
	t.Parallel()

	f := loadFixture(t)
	if len(f.Tariffs) != len(f.ExpectedResults) {
		t.Fatalf("fixture has %d tariffs but %d expected results", len(f.Tariffs), len(f.ExpectedResults))
	}

	for i, tariff := range f.Tariffs {
		want := f.ExpectedResults[i]
		r, err := costing.Cost(&f.ExpectedProfile, tariff)
		if err != nil {
			t.Fatalf("Cost(%q): %v", tariff.Name, err)
		}
		if r.Name != want.Name {
			t.Errorf("Name = %q, want %q", r.Name, want.Name)
		}
		checks := []struct {
			field     string
			got, want float64
		}{
			{"import_p", r.ImportPence, want.ImportPence},
			{"export_credit_p", r.ExportCreditP, want.ExportCreditP},
			{"gas_p", r.GasPence, want.GasPence},
			{"standing_p", r.StandingPence, want.StandingPence},
			{"net_p", r.NetPence, want.NetPence},
		}
		for _, c := range checks {
			if math.Abs(c.got-c.want) > moneyEps {
				t.Errorf("tariff %q %s = %v, want %v", tariff.Name, c.field, c.got, c.want)
			}
		}
	}
}
