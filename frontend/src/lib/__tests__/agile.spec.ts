import { describe, expect, it } from 'vitest'

import {
  costAgainstRates,
  distinctLocalDates,
  readingsDateRange,
  standingForDays,
  type RateSlot,
} from '../agile'
import type { RawReading } from '../types'

const r = (iso: string, value: number): RawReading => ({ ts: Date.parse(iso), value })

const halfHourSlots: RateSlot[] = [
  { from: '2026-01-01T00:00:00Z', to: '2026-01-01T00:30:00Z', rate: 10 },
  { from: '2026-01-01T00:30:00Z', to: '2026-01-01T01:00:00Z', rate: 20 },
  { from: '2026-01-01T01:00:00Z', to: '2026-01-01T01:30:00Z', rate: -2 }, // plunge pricing
]

describe('costAgainstRates', () => {
  it('matches each reading to the rate in force at its start', () => {
    const result = costAgainstRates(
      [r('2026-01-01T00:00:00Z', 1), r('2026-01-01T00:30:00Z', 2), r('2026-01-01T01:00:00Z', 1)],
      halfHourSlots,
    )
    // 1×10 + 2×20 + 1×(−2) = 48p — negative Agile prices reduce the bill.
    expect(result.costP).toBeCloseTo(48, 9)
    expect(result.matchedKwh).toBeCloseTo(4, 9)
    expect(result.unmatchedCount).toBe(0)
  })

  it('rounds each reading half-to-even before pricing (rule 3)', () => {
    const result = costAgainstRates([r('2026-01-01T00:00:00Z', 0.125)], halfHourSlots)
    // 0.125 → 0.12 kWh × 10p = 1.2p.
    expect(result.costP).toBeCloseTo(1.2, 9)
  })

  it('tallies readings outside the published series instead of dropping them silently', () => {
    const result = costAgainstRates(
      [r('2026-01-01T00:00:00Z', 1), r('2025-06-01T00:00:00Z', 3)],
      halfHourSlots,
    )
    expect(result.costP).toBeCloseTo(10, 9)
    expect(result.unmatchedKwh).toBeCloseTo(3, 9)
    expect(result.unmatchedCount).toBe(1)
  })

  it('handles an open-ended final slot', () => {
    const slots: RateSlot[] = [{ from: '2026-01-01T00:00:00Z', to: null, rate: 5 }]
    const result = costAgainstRates([r('2030-12-31T23:30:00Z', 2)], slots)
    expect(result.costP).toBeCloseTo(10, 9)
  })

  it('handles unsorted slot input', () => {
    const shuffled = [halfHourSlots[2], halfHourSlots[0], halfHourSlots[1]]
    const result = costAgainstRates([r('2026-01-01T00:30:00Z', 1)], shuffled)
    expect(result.costP).toBeCloseTo(20, 9)
  })
})

describe('distinctLocalDates', () => {
  it('counts London-local dates, crossing midnight correctly in summer', () => {
    const dates = distinctLocalDates([
      r('2024-06-01T10:00:00Z', 1),
      r('2024-06-01T23:30:00Z', 1), // 00:30 BST on the 2nd
    ])
    expect(dates).toEqual(['2024-06-01', '2024-06-02'])
  })
})

describe('standingForDays', () => {
  it('applies the historical charge in force on each day', () => {
    const slots: RateSlot[] = [
      { from: '2025-12-01T00:00:00Z', to: '2026-01-02T00:00:00Z', rate: 40 },
      { from: '2026-01-02T00:00:00Z', to: null, rate: 50 },
    ]
    const result = standingForDays(['2026-01-01', '2026-01-02', '2026-01-03'], slots)
    expect(result.costP).toBeCloseTo(40 + 50 + 50, 9)
    expect(result.uncoveredDays).toBe(0)
  })

  it('tallies days with no published charge', () => {
    const slots: RateSlot[] = [{ from: '2026-01-02T00:00:00Z', to: null, rate: 50 }]
    const result = standingForDays(['2026-01-01', '2026-01-02'], slots)
    expect(result.costP).toBeCloseTo(50, 9)
    expect(result.uncoveredDays).toBe(1)
  })
})

describe('readingsDateRange', () => {
  it('spans min to max plus a day, as dates', () => {
    const range = readingsDateRange([r('2026-01-05T10:00:00Z', 1), r('2026-01-02T23:30:00Z', 1)])
    expect(range).toEqual({ from: '2026-01-02', to: '2026-01-06' })
  })

  it('returns null for no readings', () => {
    expect(readingsDateRange([])).toBeNull()
  })
})
