package costing_test

import (
	"math"
	"testing"
	"time"

	"github.com/javorszky/uk-energy-backtest/internal/costing"
)

func london(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load Europe/London: %v", err)
	}
	return loc
}

func reading(t *testing.T, iso string, kwh float64) costing.Reading {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		t.Fatalf("parse %q: %v", iso, err)
	}
	return costing.Reading{IntervalStart: ts, Consumption: kwh}
}

func TestBuildProfileAutumnDoubledHour(t *testing.T) {
	t.Parallel()

	// 2024-10-27: clocks fall back at 02:00 BST → 01:00 GMT. UTC 00:00Z is
	// local 01:00 BST and UTC 01:00Z is local 01:00 GMT — both must land in
	// bucket 2; likewise 00:30Z and 01:30Z in bucket 3.
	imp := []costing.Reading{
		reading(t, "2024-10-27T00:00:00Z", 0.5),
		reading(t, "2024-10-27T00:30:00Z", 0.6),
		reading(t, "2024-10-27T01:00:00Z", 0.7),
		reading(t, "2024-10-27T01:30:00Z", 0.8),
	}
	p, err := costing.BuildProfile(imp, nil, nil, false, costing.DefaultCalorificValue, london(t))
	if err != nil {
		t.Fatalf("BuildProfile: %v", err)
	}
	if math.Abs(p.ImportHH[2]-1.2) > moneyEps {
		t.Errorf("bucket 2 = %v, want 1.2 (doubled local hour)", p.ImportHH[2])
	}
	if math.Abs(p.ImportHH[3]-1.4) > moneyEps {
		t.Errorf("bucket 3 = %v, want 1.4 (doubled local hour)", p.ImportHH[3])
	}
	if p.SuppliedDays != 1 {
		t.Errorf("SuppliedDays = %d, want 1", p.SuppliedDays)
	}
}

func TestBuildProfileSpringSkippedHour(t *testing.T) {
	t.Parallel()

	// 2024-03-31: clocks spring forward at 01:00 GMT → 02:00 BST. Local
	// 01:00–01:59 never happens; UTC 01:00Z is local 02:00 BST (bucket 4).
	imp := []costing.Reading{
		reading(t, "2024-03-31T00:30:00Z", 0.3), // local 00:30 GMT → bucket 1
		reading(t, "2024-03-31T01:00:00Z", 0.4), // local 02:00 BST → bucket 4
	}
	p, err := costing.BuildProfile(imp, nil, nil, false, costing.DefaultCalorificValue, london(t))
	if err != nil {
		t.Fatalf("BuildProfile: %v", err)
	}
	if p.ImportHH[2] != 0 || p.ImportHH[3] != 0 {
		t.Errorf("buckets 2,3 = %v,%v, want 0,0 (skipped local hour)", p.ImportHH[2], p.ImportHH[3])
	}
	if math.Abs(p.ImportHH[1]-0.3) > moneyEps {
		t.Errorf("bucket 1 = %v, want 0.3", p.ImportHH[1])
	}
	if math.Abs(p.ImportHH[4]-0.4) > moneyEps {
		t.Errorf("bucket 4 = %v, want 0.4", p.ImportHH[4])
	}
}

func TestBuildProfileSummerBucketIsLocal(t *testing.T) {
	t.Parallel()

	// Mid-summer: UTC 17:00Z is local 18:00 BST → bucket 36, not 34. This is
	// costing rule 1 — get it wrong and peak/off-peak shift by an hour for
	// half the year.
	imp := []costing.Reading{reading(t, "2024-07-15T17:00:00Z", 1.0)}
	p, err := costing.BuildProfile(imp, nil, nil, false, costing.DefaultCalorificValue, london(t))
	if err != nil {
		t.Fatalf("BuildProfile: %v", err)
	}
	if p.ImportHH[36] != 1.0 {
		t.Errorf("bucket 36 = %v, want 1.0 (BST local bucketing)", p.ImportHH[36])
	}
	if p.ImportHH[34] != 0 {
		t.Errorf("bucket 34 = %v, want 0 (reading must not land at UTC position)", p.ImportHH[34])
	}
}

func TestBuildProfileSuppliedDaysWithGaps(t *testing.T) {
	t.Parallel()

	imp := []costing.Reading{
		reading(t, "2024-06-01T10:00:00Z", 1),
		reading(t, "2024-06-01T11:00:00Z", 1),
		// 2024-06-02 missing entirely.
		reading(t, "2024-06-03T10:00:00Z", 1),
		// 23:30Z on the 3rd is local 00:30 BST on the 4th — counts as day 4.
		reading(t, "2024-06-03T23:30:00Z", 1),
	}
	p, err := costing.BuildProfile(imp, nil, nil, false, costing.DefaultCalorificValue, london(t))
	if err != nil {
		t.Fatalf("BuildProfile: %v", err)
	}
	if p.SuppliedDays != 3 {
		t.Errorf("SuppliedDays = %d, want 3 (gap day excluded, local-midnight crossover counted)", p.SuppliedDays)
	}
}

func TestBuildProfilePerSlotRoundingBeforeBucketing(t *testing.T) {
	t.Parallel()

	// Two readings in the same bucket, each a .005 tie: rounding must happen
	// per slot (0.125→0.12, 0.375→0.38, sum 0.50), not on the sum
	// (0.125+0.375=0.5 either way here, so also assert a case that differs:
	// 0.875→0.88 and 0.875→0.88 sum 1.76, whereas sum-then-round gives 1.75).
	imp := []costing.Reading{
		reading(t, "2024-06-01T10:00:00Z", 0.875),
		reading(t, "2024-06-01T10:00:10Z", 0.875),
	}
	p, err := costing.BuildProfile(imp, nil, nil, false, costing.DefaultCalorificValue, london(t))
	if err != nil {
		t.Fatalf("BuildProfile: %v", err)
	}
	if math.Abs(p.ImportHH[22]-1.76) > moneyEps {
		t.Errorf("bucket 22 = %v, want 1.76 (round per slot, then sum)", p.ImportHH[22])
	}
}

func TestBuildProfileGasConversion(t *testing.T) {
	t.Parallel()

	gas := []costing.Reading{
		reading(t, "2024-06-01T10:00:00Z", 1.0),
		reading(t, "2024-06-01T10:30:00Z", 2.0),
	}

	t.Run("m3 converts then rounds per reading", func(t *testing.T) {
		t.Parallel()
		p, err := costing.BuildProfile(nil, nil, gas, true, costing.DefaultCalorificValue, london(t))
		if err != nil {
			t.Fatalf("BuildProfile: %v", err)
		}
		// 1 m³ → 11.220633… → 11.22; 2 m³ → 22.441266… → 22.44.
		if math.Abs(p.GasKWh-33.66) > moneyEps {
			t.Errorf("GasKWh = %v, want 33.66", p.GasKWh)
		}
	})

	t.Run("kwh passes through", func(t *testing.T) {
		t.Parallel()
		p, err := costing.BuildProfile(nil, nil, gas, false, costing.DefaultCalorificValue, london(t))
		if err != nil {
			t.Fatalf("BuildProfile: %v", err)
		}
		if math.Abs(p.GasKWh-3.0) > moneyEps {
			t.Errorf("GasKWh = %v, want 3.0", p.GasKWh)
		}
	})
}

func TestBuildProfileMissingStreams(t *testing.T) {
	t.Parallel()

	imp := []costing.Reading{reading(t, "2024-06-01T10:00:00Z", 1)}
	p, err := costing.BuildProfile(imp, nil, nil, false, costing.DefaultCalorificValue, london(t))
	if err != nil {
		t.Fatalf("BuildProfile: %v", err)
	}
	if p.ExportHH != nil {
		t.Error("ExportHH should be nil when there are no export readings")
	}
	if p.GasKWh != 0 {
		t.Errorf("GasKWh = %v, want 0", p.GasKWh)
	}
}

func TestBuildProfileNilLocation(t *testing.T) {
	t.Parallel()

	if _, err := costing.BuildProfile(nil, nil, nil, false, costing.DefaultCalorificValue, nil); err == nil {
		t.Error("BuildProfile accepted nil location, want error")
	}
}
