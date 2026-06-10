import { describe, expect, it } from 'vitest'

import { convertM3ToKWh, roundHalfEven2dp, DEFAULT_CALORIFIC_VALUE } from '../rounding'

describe('roundHalfEven2dp', () => {
  // Tie inputs are exactly representable in float64 (eighths), so the
  // half-even behaviour is genuinely exercised. They mirror the Go tests in
  // internal/costing/round_test.go.
  it.each([
    [0.125, 0.12],
    [0.375, 0.38],
    [0.625, 0.62],
    [0.875, 0.88],
    [0.876, 0.88],
    [0.874, 0.87],
    [1.23, 1.23],
    [0, 0],
    [5, 5],
    [0.07499999999, 0.07],
    [0.07500000001, 0.08],
    [-0.125, -0.12],
  ])('rounds %f to %f', (input, want) => {
    expect(roundHalfEven2dp(input)).toBeCloseTo(want, 12)
  })
})

describe('convertM3ToKWh', () => {
  it('applies the rule-5 formula', () => {
    expect(convertM3ToKWh(1, DEFAULT_CALORIFIC_VALUE)).toBeCloseTo((1.02264 * 39.5) / 3.6, 12)
  })

  it('handles zero', () => {
    expect(convertM3ToKWh(0)).toBe(0)
  })
})
