import { describe, expect, it } from 'vitest'

import { buildProfile, detectGranularityMinutes } from '../profile'
import type { RawReading } from '../types'

const r = (iso: string, value: number): RawReading => ({ ts: Date.parse(iso), value })

describe('buildProfile', () => {
  it('sums the autumn doubled local hour into one bucket', () => {
    const { profile } = buildProfile({
      importReadings: [
        r('2024-10-27T00:00:00Z', 0.5), // 01:00 BST → bucket 2
        r('2024-10-27T00:30:00Z', 0.6), // 01:30 BST → bucket 3
        r('2024-10-27T01:00:00Z', 0.7), // 01:00 GMT → bucket 2 again
        r('2024-10-27T01:30:00Z', 0.8), // 01:30 GMT → bucket 3 again
      ],
    })
    expect(profile.import_hh[2]).toBeCloseTo(1.2, 9)
    expect(profile.import_hh[3]).toBeCloseTo(1.4, 9)
    expect(profile.supplied_days).toBe(1)
  })

  it('leaves the skipped spring hour empty', () => {
    const { profile } = buildProfile({
      importReadings: [
        r('2024-03-31T00:30:00Z', 0.3), // 00:30 GMT → bucket 1
        r('2024-03-31T01:00:00Z', 0.4), // 02:00 BST → bucket 4
      ],
    })
    expect(profile.import_hh[1]).toBeCloseTo(0.3, 9)
    expect(profile.import_hh[2]).toBe(0)
    expect(profile.import_hh[3]).toBe(0)
    expect(profile.import_hh[4]).toBeCloseTo(0.4, 9)
  })

  it('rounds each slot half-to-even before summing (rule 3)', () => {
    // 0.875 → 0.88 per slot; two slots = 1.76. Sum-then-round would give 1.75.
    const { profile } = buildProfile({
      importReadings: [r('2024-06-01T10:00:00Z', 0.875), r('2024-06-01T10:00:10Z', 0.875)],
    })
    expect(profile.import_hh[22]).toBeCloseTo(1.76, 9)
  })

  it('counts supplied days from distinct local dates with gaps excluded', () => {
    const { profile } = buildProfile({
      importReadings: [
        r('2024-06-01T10:00:00Z', 1),
        r('2024-06-01T11:00:00Z', 1),
        // 2024-06-02 missing entirely.
        r('2024-06-03T10:00:00Z', 1),
        // 23:30Z is 00:30 BST on the 4th — a fourth... no, third distinct new day.
        r('2024-06-03T23:30:00Z', 1),
      ],
    })
    expect(profile.supplied_days).toBe(3)
  })

  it('converts m³ gas per reading then rounds then sums', () => {
    const { profile } = buildProfile({
      importReadings: [],
      gasReadings: [r('2024-06-01T06:00:00Z', 1), r('2024-06-01T06:30:00Z', 2)],
      gasUnit: 'm3',
    })
    // 1 m³ → 11.220633… → 11.22; 2 m³ → 22.441266… → 22.44.
    expect(profile.gas_kwh).toBeCloseTo(33.66, 9)
  })

  it('passes kWh gas through unconverted', () => {
    const { profile } = buildProfile({
      importReadings: [],
      gasReadings: [r('2024-06-01T06:00:00Z', 1), r('2024-06-01T06:30:00Z', 2)],
      gasUnit: 'kwh',
    })
    expect(profile.gas_kwh).toBeCloseTo(3, 9)
  })

  it('omits export_hh when there is no export stream', () => {
    const { profile } = buildProfile({ importReadings: [r('2024-06-01T10:00:00Z', 1)] })
    expect(profile.export_hh).toBeUndefined()
    expect(profile.gas_kwh).toBe(0)
  })

  it('warns on hourly granularity but still costs as-is', () => {
    const { profile, warnings } = buildProfile({
      importReadings: [
        r('2024-06-01T10:00:00Z', 1),
        r('2024-06-01T11:00:00Z', 1),
        r('2024-06-01T12:00:00Z', 1),
      ],
    })
    expect(warnings).toHaveLength(1)
    expect(warnings[0]).toContain('60-minute')
    expect(profile.import_hh[22]).toBeCloseTo(1, 9)
  })

  it('does not warn on half-hourly data', () => {
    const { warnings } = buildProfile({
      importReadings: [
        r('2024-06-01T10:00:00Z', 1),
        r('2024-06-01T10:30:00Z', 1),
        r('2024-06-01T11:00:00Z', 1),
      ],
    })
    expect(warnings).toHaveLength(0)
  })
})

describe('detectGranularityMinutes', () => {
  it('returns null for fewer than two readings', () => {
    expect(detectGranularityMinutes([])).toBeNull()
    expect(detectGranularityMinutes([r('2024-06-01T10:00:00Z', 1)])).toBeNull()
  })

  it('ignores gaps via the median', () => {
    const readings = [
      r('2024-06-01T10:00:00Z', 1),
      r('2024-06-01T10:30:00Z', 1),
      r('2024-06-01T11:00:00Z', 1),
      // big gap
      r('2024-06-05T10:00:00Z', 1),
      r('2024-06-05T10:30:00Z', 1),
    ]
    expect(detectGranularityMinutes(readings)).toBe(30)
  })
})
