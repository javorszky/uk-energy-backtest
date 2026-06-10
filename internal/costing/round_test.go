package costing_test

import (
	"math"
	"testing"

	"github.com/javorszky/uk-energy-backtest/internal/costing"
)

func TestRoundHalfEven2dp(t *testing.T) {
	t.Parallel()

	// Tie inputs are chosen to be exactly representable in float64 (eighths)
	// so the half-even behaviour is actually exercised rather than decided by
	// binary representation noise.
	tests := []struct {
		name string
		in   float64
		want float64
	}{
		{"tie rounds down to even", 0.125, 0.12},
		{"tie rounds up to even", 0.375, 0.38},
		{"tie rounds down to even high", 0.625, 0.62},
		{"tie rounds up to even high", 0.875, 0.88},
		{"non-tie rounds up", 0.876, 0.88},
		{"non-tie rounds down", 0.874, 0.87},
		{"already 2dp unchanged", 1.23, 1.23},
		{"zero", 0, 0},
		{"integer unchanged", 5, 5},
		{"just below tie", 0.07499999999, 0.07},
		{"just above tie", 0.07500000001, 0.08},
		{"negative tie to even", -0.125, -0.12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := costing.RoundHalfEven2dp(tt.in)
			if math.Abs(got-tt.want) > 1e-12 {
				t.Errorf("RoundHalfEven2dp(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestConvertM3ToKWh(t *testing.T) {
	t.Parallel()

	// 1 m³ × 1.02264 × 39.5 / 3.6 = 11.220633...
	got := costing.ConvertM3ToKWh(1.0, costing.DefaultCalorificValue)
	want := 1.02264 * 39.5 / 3.6
	if math.Abs(got-want) > 1e-12 {
		t.Errorf("ConvertM3ToKWh(1.0, 39.5) = %v, want %v", got, want)
	}

	if got := costing.ConvertM3ToKWh(0, costing.DefaultCalorificValue); got != 0 {
		t.Errorf("ConvertM3ToKWh(0, 39.5) = %v, want 0", got)
	}
}
