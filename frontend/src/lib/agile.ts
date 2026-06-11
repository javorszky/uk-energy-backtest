/**
 * Historical Agile backtesting. Dynamic tariffs price every half-hour
 * differently, so the 48-bucket profile cannot represent them — instead the
 * full raw reading history (which never leaves the device) is costed
 * directly against the published historical price series, which the backend
 * relays from the public Octopus product endpoints.
 */

import { roundHalfEven2dp } from './rounding'
import { localBucket } from './timezone'
import type { RawReading } from './types'

/** One published price interval, as served by GET /api/v1/agile/rates. */
export interface RateSlot {
  from: string
  to: string | null
  rate: number
}

export interface AgileStreamCost {
  /** Total pence across all matched readings. */
  costP: number
  /** kWh that found a published rate. */
  matchedKwh: number
  /** kWh excluded because no rate covered their interval. */
  unmatchedKwh: number
  unmatchedCount: number
}

interface Interval {
  fromMs: number
  toMs: number
  rate: number
}

function toIntervals(slots: RateSlot[]): Interval[] {
  return slots
    .map((s) => ({
      fromMs: Date.parse(s.from),
      toMs: s.to === null ? Number.POSITIVE_INFINITY : Date.parse(s.to),
      rate: s.rate,
    }))
    .filter((s) => !Number.isNaN(s.fromMs))
    .sort((a, b) => a.fromMs - b.fromMs)
}

/** Binary search for the interval covering ts; -1 when none does. */
function findInterval(intervals: Interval[], ts: number): number {
  let lo = 0
  let hi = intervals.length - 1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if (intervals[mid].fromMs > ts) {
      hi = mid - 1
    } else if (intervals[mid].toMs <= ts) {
      lo = mid + 1
    } else {
      return mid
    }
  }
  return -1
}

/**
 * Cost raw readings against a historical price series: each reading is
 * rounded to 0.01 kWh half-to-even (costing rule 3 — matching how Octopus
 * bills) and multiplied by the rate in force at its interval start. Readings
 * with no covering rate are tallied, not silently dropped — the caller
 * surfaces them as a coverage warning.
 */
export function costAgainstRates(readings: RawReading[], slots: RateSlot[]): AgileStreamCost {
  const intervals = toIntervals(slots)
  const result: AgileStreamCost = { costP: 0, matchedKwh: 0, unmatchedKwh: 0, unmatchedCount: 0 }
  for (const r of readings) {
    const kwh = roundHalfEven2dp(r.value)
    const i = findInterval(intervals, r.ts)
    if (i === -1) {
      result.unmatchedKwh += kwh
      result.unmatchedCount++
      continue
    }
    result.costP += kwh * intervals[i].rate
    result.matchedKwh += kwh
  }
  return result
}

/** Distinct Europe/London calendar dates with readings (rule 6). */
export function distinctLocalDates(readings: RawReading[]): string[] {
  const days = new Set<string>()
  for (const r of readings) {
    days.add(localBucket(r.ts).localDate)
  }
  return [...days].sort()
}

export interface StandingCost {
  costP: number
  /** Days that had no published standing charge. */
  uncoveredDays: number
}

/**
 * Standing charge summed per supplied day, using the charge in force at
 * local noon of each day (standing charges change rarely; noon avoids any
 * midnight boundary ambiguity).
 */
export function standingForDays(localDates: string[], slots: RateSlot[]): StandingCost {
  const intervals = toIntervals(slots)
  const result: StandingCost = { costP: 0, uncoveredDays: 0 }
  for (const date of localDates) {
    const noonUtc = Date.parse(`${date}T12:00:00Z`)
    const i = findInterval(intervals, noonUtc)
    if (i === -1) {
      result.uncoveredDays++
      continue
    }
    result.costP += intervals[i].rate
  }
  return result
}

/** The UTC date range (inclusive from, exclusive to) covering the readings. */
export function readingsDateRange(readings: RawReading[]): { from: string; to: string } | null {
  if (readings.length === 0) return null
  let min = Number.POSITIVE_INFINITY
  let max = Number.NEGATIVE_INFINITY
  for (const r of readings) {
    if (r.ts < min) min = r.ts
    if (r.ts > max) max = r.ts
  }
  const day = 24 * 3600 * 1000
  const iso = (ms: number): string => new Date(ms).toISOString().slice(0, 10)
  return { from: iso(min), to: iso(max + day) }
}
