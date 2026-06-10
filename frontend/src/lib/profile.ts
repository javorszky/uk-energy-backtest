/**
 * Client-side profile builder: collapses raw half-hourly readings into the
 * 48-bucket load profile (costing rules 1, 3, 5, 6). This is the TS twin of
 * Go's costing.BuildProfile — the shared fixture in
 * testdata/shared/profile_fixture.json keeps the two in agreement.
 *
 * This module is pure: no network, no DOM, no storage. The raw readings it
 * consumes never leave the device; only the returned profile is ever POSTed.
 */

import { BUCKETS_PER_DAY, type Profile, type RawReading } from './types'
import { convertM3ToKWh, roundHalfEven2dp, DEFAULT_CALORIFIC_VALUE } from './rounding'
import { localBucket } from './timezone'

export interface ProfileBuildInput {
  importReadings: RawReading[]
  exportReadings?: RawReading[]
  gasReadings?: RawReading[]
  /** How gas reading values are interpreted; m³ triggers rule-5 conversion. */
  gasUnit?: 'kwh' | 'm3'
  /** MJ/m³, used only when gasUnit is m3. */
  calorificValue?: number
}

export interface ProfileBuildResult {
  profile: Profile
  /** Human-readable data-quality notes (granularity, duplicates, …). */
  warnings: string[]
}

const EXPECTED_PERIOD_MINUTES = 30

/**
 * Build the load profile. Each reading is bucketed by its Europe/London
 * local clock time (rule 1) — on DST-change days two UTC half-hours can land
 * in one local bucket (autumn) or a local hour can receive none (spring);
 * both are correct because tariff bands are defined in local clock time.
 * Each slot value is rounded to 0.01 kWh half-to-even BEFORE summing into
 * its bucket (rule 3). supplied_days counts distinct local calendar dates
 * with import readings (rule 6), so gaps don't mis-charge standing.
 */
export function buildProfile(input: ProfileBuildInput): ProfileBuildResult {
  const warnings: string[] = []

  const importHH = new Array<number>(BUCKETS_PER_DAY).fill(0)
  const days = new Set<string>()
  for (const r of input.importReadings) {
    const { bucket, localDate } = localBucket(r.ts)
    importHH[bucket] += roundHalfEven2dp(r.value)
    days.add(localDate)
  }

  let exportHH: number[] | undefined
  if (input.exportReadings && input.exportReadings.length > 0) {
    exportHH = new Array<number>(BUCKETS_PER_DAY).fill(0)
    for (const r of input.exportReadings) {
      exportHH[localBucket(r.ts).bucket] += roundHalfEven2dp(r.value)
    }
  }

  let gasKwh = 0
  const cv = input.calorificValue ?? DEFAULT_CALORIFIC_VALUE
  for (const r of input.gasReadings ?? []) {
    const kwh = input.gasUnit === 'm3' ? convertM3ToKWh(r.value, cv) : r.value
    gasKwh += roundHalfEven2dp(kwh)
  }

  const granularity = detectGranularityMinutes(input.importReadings)
  if (granularity !== null && granularity !== EXPECTED_PERIOD_MINUTES) {
    warnings.push(
      `Import readings look ${granularity}-minute, not half-hourly; ` +
        `costs are computed as-is, attributing each reading to the half-hour it starts in.`,
    )
  }

  return {
    profile: {
      supplied_days: days.size,
      import_hh: importHH,
      ...(exportHH ? { export_hh: exportHH } : {}),
      gas_kwh: gasKwh,
    },
    warnings,
  }
}

/**
 * Median gap between consecutive readings, in minutes — null when there are
 * fewer than two readings. The median shrugs off gaps and DST days.
 */
export function detectGranularityMinutes(readings: RawReading[]): number | null {
  if (readings.length < 2) return null
  const sorted = [...readings].sort((a, b) => a.ts - b.ts)
  const deltas: number[] = []
  for (let i = 1; i < sorted.length; i++) {
    const d = sorted[i].ts - sorted[i - 1].ts
    if (d > 0) deltas.push(d)
  }
  if (deltas.length === 0) return null
  deltas.sort((a, b) => a - b)
  return deltas[Math.floor(deltas.length / 2)] / 60_000
}
