/**
 * Cross-language contract test: the same fixture is consumed by Go
 * (internal/costing/fixture_test.go). If this spec and the Go test both
 * pass, the TS profile-build phase and the Go costing phase provably agree
 * on DST bucketing, per-slot rounding, and gas conversion.
 */
import { describe, expect, it } from 'vitest'

import fixture from '../../../../testdata/shared/profile_fixture.json'
import { buildProfile } from '../profile'
import type { RawReading } from '../types'

interface FixtureReading {
  interval_start: string
  consumption: number
}

const toReadings = (rs: FixtureReading[]): RawReading[] =>
  rs.map((r) => ({ ts: Date.parse(r.interval_start), value: r.consumption }))

describe('shared fixture', () => {
  const { profile } = buildProfile({
    importReadings: toReadings(fixture.readings.import),
    exportReadings: toReadings(fixture.readings.export),
    gasReadings: toReadings(fixture.readings.gas),
    gasUnit: fixture.gas_unit as 'kwh' | 'm3',
    calorificValue: fixture.calorific_value,
  })

  it('reproduces supplied_days', () => {
    expect(profile.supplied_days).toBe(fixture.expected_profile.supplied_days)
  })

  it('reproduces every import bucket', () => {
    for (let i = 0; i < 48; i++) {
      expect(profile.import_hh[i], `import_hh[${i}]`).toBeCloseTo(
        fixture.expected_profile.import_hh[i],
        9,
      )
    }
  })

  it('reproduces every export bucket', () => {
    expect(profile.export_hh).toBeDefined()
    for (let i = 0; i < 48; i++) {
      expect(profile.export_hh![i], `export_hh[${i}]`).toBeCloseTo(
        fixture.expected_profile.export_hh[i],
        9,
      )
    }
  })

  it('reproduces gas_kwh', () => {
    expect(profile.gas_kwh).toBeCloseTo(fixture.expected_profile.gas_kwh, 9)
  })
})
