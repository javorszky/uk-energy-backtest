/**
 * Rounding and unit conversion for profile building. These must agree
 * exactly with the Go twins in internal/costing/round.go — JS numbers are
 * IEEE 754 float64, the same representation Go uses, so identical algorithms
 * yield identical results. The shared fixture proves it.
 */

/** Typical UK natural gas calorific value in MJ/m³; regionally variable. */
export const DEFAULT_CALORIFIC_VALUE = 39.5

const M3_VOLUME_CORRECTION = 1.02264
const MEGAJOULES_PER_KWH = 3.6

/**
 * Round to 2 decimal places with round-half-to-even (banker's rounding),
 * matching how Octopus rounds each half-hour slot before billing (costing
 * rule 3). Math.round is half-away-from-zero and must not be used here.
 */
export function roundHalfEven2dp(v: number): number {
  const scaled = v * 100
  const floor = Math.floor(scaled)
  const diff = scaled - floor
  if (diff > 0.5) return (floor + 1) / 100
  if (diff < 0.5) return floor / 100
  return (floor % 2 === 0 ? floor : floor + 1) / 100
}

/**
 * Gas m³ → kWh (costing rule 5): kWh = m³ × 1.02264 × calorific value / 3.6.
 * SMETS1 meters already report kWh and must not pass through this.
 */
export function convertM3ToKWh(
  m3: number,
  calorificValue: number = DEFAULT_CALORIFIC_VALUE,
): number {
  return (m3 * M3_VOLUME_CORRECTION * calorificValue) / MEGAJOULES_PER_KWH
}
