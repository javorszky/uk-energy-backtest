package costing

import "math"

const (
	// centsScale converts kWh to hundredths for 2-decimal-place rounding.
	centsScale = 100

	// m3VolumeCorrection is the standard volume correction factor applied to
	// metered gas volume per the UK Gas (Calculation of Thermal Energy)
	// Regulations.
	m3VolumeCorrection = 1.02264

	// joulesPerKWh converts megajoules to kWh (3.6 MJ per kWh).
	megajoulesPerKWh = 3.6

	// DefaultCalorificValue is the typical UK natural gas calorific value in
	// MJ/m³. Real values vary by region and day (~37.5–43.0); callers may
	// override.
	DefaultCalorificValue = 39.5
)

// RoundHalfEven2dp rounds v to 2 decimal places using round-half-to-even
// (banker's rounding), matching how Octopus rounds each half-hour slot before
// billing (costing rule 3). math.Round must not be used here: it rounds
// half-away-from-zero, which biases sums upward and diverges from the bill.
//
// math.Remainder implements IEEE 754 remainder: the quotient is rounded to
// the nearest integer with ties to even, so scaled-r is exactly the
// round-half-even integer, with no float drift from a division afterwards.
func RoundHalfEven2dp(v float64) float64 {
	scaled := v * centsScale
	r := math.Remainder(scaled, 1)
	return (scaled - r) / centsScale
}

// ConvertM3ToKWh converts a gas volume in cubic metres to kWh (costing rule
// 5): kWh = m³ × 1.02264 × calorific value / 3.6. SMETS1 meters already
// report kWh and must not pass through this.
func ConvertM3ToKWh(m3, calorificValue float64) float64 {
	return m3 * m3VolumeCorrection * calorificValue / megajoulesPerKWh
}
