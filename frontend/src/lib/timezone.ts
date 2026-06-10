/**
 * Timezone-aware conversions built on Intl.DateTimeFormat, so no timezone
 * library is shipped. The browser's own zone is never used — costing rule 1
 * requires explicit zones throughout (the CSV's declared source zone for
 * parsing, Europe/London for bucketing).
 *
 * Two distinct operations live here; do not conflate them:
 *  1. parse:  wall-clock time in some source zone → UTC instant
 *  2. bucket: UTC instant → Europe/London half-hour-of-day
 */

export const LONDON_TZ = 'Europe/London'

const MINUTES_PER_BUCKET = 30
const MS_PER_MINUTE = 60_000

interface WallTime {
  year: number
  month: number // 1-12
  day: number
  hour: number
  minute: number
  second: number
}

// One formatter per zone; constructing Intl.DateTimeFormat is expensive and
// a six-month dataset runs ~17k conversions through this.
const formatterCache = new Map<string, Intl.DateTimeFormat>()

function formatterFor(tz: string): Intl.DateTimeFormat {
  let fmt = formatterCache.get(tz)
  if (!fmt) {
    // en-GB + h23 gives unambiguous numeric parts; an invalid tz throws
    // RangeError here, which is the validation we want.
    fmt = new Intl.DateTimeFormat('en-GB', {
      timeZone: tz,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hourCycle: 'h23',
    })
    formatterCache.set(tz, fmt)
  }
  return fmt
}

/** What the wall clock in tz shows at the given UTC instant. */
export function wallTimeAt(utcMs: number, tz: string): WallTime {
  const parts = formatterFor(tz).formatToParts(utcMs)
  const get = (type: Intl.DateTimeFormatPartTypes): number => {
    const part = parts.find((p) => p.type === type)
    if (!part) throw new Error(`missing ${type} part formatting instant in ${tz}`)
    return Number(part.value)
  }
  return {
    year: get('year'),
    month: get('month'),
    day: get('day'),
    hour: get('hour'),
    minute: get('minute'),
    second: get('second'),
  }
}

function wallTimeAsUtcMs(w: WallTime): number {
  return Date.UTC(w.year, w.month - 1, w.day, w.hour, w.minute, w.second)
}

/**
 * Interpret a wall-clock time as a moment in tz and return the UTC instant.
 *
 * DST edges (relevant when the source zone observes DST):
 *  - Ambiguous times (autumn fall-back, the clock shows them twice) resolve
 *    to the EARLIER instant — the first time the clock showed that reading.
 *  - Skipped times (spring forward, the clock never shows them) resolve as
 *    if the clock had not jumped, landing just after the gap.
 */
export function wallTimeToUtcMs(w: WallTime, tz: string): number {
  const guess = wallTimeAsUtcMs(w)
  const day = 24 * 60 * MS_PER_MINUTE

  // Any DST transition near the wall time sits between the zone's offset a
  // day before and a day after, so probing those three instants surfaces
  // every plausible offset.
  const offsets = new Set<number>()
  for (const probe of [guess - day, guess, guess + day]) {
    offsets.add(wallTimeAsUtcMs(wallTimeAt(probe, tz)) - probe)
  }

  const valid: number[] = []
  for (const offset of offsets) {
    const t = guess - offset
    if (wallTimeAsUtcMs(wallTimeAt(t, tz)) === guess) valid.push(t)
  }
  if (valid.length > 0) return Math.min(...valid)

  // Skipped (spring-forward) time: apply the pre-transition offset so the
  // reading lands just after the gap.
  const preOffset = wallTimeAsUtcMs(wallTimeAt(guess - day, tz)) - (guess - day)
  return guess - preOffset
}

/**
 * Map a UTC instant to its Europe/London half-hour-of-day bucket and local
 * calendar date (costing rule 1: bucket in London local clock time, never
 * UTC — get this wrong and peak/off-peak shift by an hour for half the
 * year).
 */
export function localBucket(
  utcMs: number,
  tz: string = LONDON_TZ,
): { bucket: number; localDate: string } {
  const w = wallTimeAt(utcMs, tz)
  const pad = (n: number): string => String(n).padStart(2, '0')
  return {
    bucket: Math.floor((w.hour * 60 + w.minute) / MINUTES_PER_BUCKET),
    localDate: `${w.year}-${pad(w.month)}-${pad(w.day)}`,
  }
}

/**
 * Parse a CSV timestamp string to a UTC instant. If the string carries an
 * explicit offset or Z it wins and sourceTz is ignored; otherwise the string
 * is interpreted as wall time in sourceTz.
 *
 * Returns NaN for unparseable input (callers collect these as warnings).
 */
export function parseTimestamp(raw: string, sourceTz: string): number {
  const s = raw.trim()
  if (s === '') return NaN

  // Explicit offset or Z → the timestamp is absolute already.
  if (/(?:Z|[+-]\d{2}:?\d{2})$/.test(s)) {
    const ms = Date.parse(s)
    return Number.isNaN(ms) ? NaN : ms
  }

  // Accept "YYYY-MM-DD HH:MM[:SS]" and "YYYY-MM-DDTHH:MM[:SS]" and
  // "DD/MM/YYYY HH:MM[:SS]" (UK suppliers love the latter).
  const iso = /^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2})(?::(\d{2}))?$/.exec(s)
  const uk = /^(\d{2})\/(\d{2})\/(\d{4})[T ](\d{2}):(\d{2})(?::(\d{2}))?$/.exec(s)
  let w: WallTime
  if (iso) {
    w = {
      year: Number(iso[1]),
      month: Number(iso[2]),
      day: Number(iso[3]),
      hour: Number(iso[4]),
      minute: Number(iso[5]),
      second: Number(iso[6] ?? 0),
    }
  } else if (uk) {
    w = {
      year: Number(uk[3]),
      month: Number(uk[2]),
      day: Number(uk[1]),
      hour: Number(uk[4]),
      minute: Number(uk[5]),
      second: Number(uk[6] ?? 0),
    }
  } else {
    return NaN
  }
  if (w.month < 1 || w.month > 12 || w.day < 1 || w.day > 31 || w.hour > 23 || w.minute > 59) {
    return NaN
  }
  return wallTimeToUtcMs(w, sourceTz)
}
