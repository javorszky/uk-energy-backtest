import { describe, expect, it } from 'vitest'

import { localBucket, parseTimestamp, LONDON_TZ } from '../timezone'

const utc = (iso: string): number => Date.parse(iso)

describe('localBucket', () => {
  it('buckets winter (GMT) instants at their UTC clock position', () => {
    expect(localBucket(utc('2024-01-15T17:00:00Z')).bucket).toBe(34)
    expect(localBucket(utc('2024-01-15T00:00:00Z')).bucket).toBe(0)
  })

  it('buckets summer (BST) instants one hour ahead of UTC — costing rule 1', () => {
    // 17:00Z is 18:00 local in July: bucket 36, not 34.
    expect(localBucket(utc('2024-07-15T17:00:00Z')).bucket).toBe(36)
    // 23:30Z is 00:30 local the NEXT day.
    const late = localBucket(utc('2024-07-15T23:30:00Z'))
    expect(late.bucket).toBe(1)
    expect(late.localDate).toBe('2024-07-16')
  })

  it('maps both autumn-overlap UTC half-hours into the same local bucket', () => {
    // 2024-10-27: 00:00Z is 01:00 BST, 01:00Z is 01:00 GMT — both bucket 2.
    expect(localBucket(utc('2024-10-27T00:00:00Z')).bucket).toBe(2)
    expect(localBucket(utc('2024-10-27T01:00:00Z')).bucket).toBe(2)
    expect(localBucket(utc('2024-10-27T00:30:00Z')).bucket).toBe(3)
    expect(localBucket(utc('2024-10-27T01:30:00Z')).bucket).toBe(3)
  })

  it('never produces buckets 2-3 from UTC instants on the spring-forward morning', () => {
    // 2024-03-31: local 01:00-01:59 never exists; 01:00Z is already 02:00 BST.
    expect(localBucket(utc('2024-03-31T00:30:00Z')).bucket).toBe(1)
    expect(localBucket(utc('2024-03-31T01:00:00Z')).bucket).toBe(4)
  })
})

describe('parseTimestamp', () => {
  it('respects an explicit Z, ignoring the source zone', () => {
    expect(parseTimestamp('2024-07-15T12:00:00Z', LONDON_TZ)).toBe(utc('2024-07-15T12:00:00Z'))
  })

  it('respects an explicit offset (Octopus CSV exports carry +01:00 in summer)', () => {
    expect(parseTimestamp('2024-07-15T13:00:00+01:00', LONDON_TZ)).toBe(utc('2024-07-15T12:00:00Z'))
  })

  it('interprets bare timestamps in the source zone (summer wall time)', () => {
    // 13:00 London wall time in July is 12:00Z.
    expect(parseTimestamp('2024-07-15 13:00', LONDON_TZ)).toBe(utc('2024-07-15T12:00:00Z'))
    expect(parseTimestamp('2024-07-15T13:00:00', LONDON_TZ)).toBe(utc('2024-07-15T12:00:00Z'))
  })

  it('interprets bare timestamps as UTC when the source zone is UTC (n3rgy)', () => {
    expect(parseTimestamp('2024-07-15 13:00', 'UTC')).toBe(utc('2024-07-15T13:00:00Z'))
  })

  it('parses UK-style DD/MM/YYYY', () => {
    expect(parseTimestamp('15/01/2024 17:30', LONDON_TZ)).toBe(utc('2024-01-15T17:30:00Z'))
  })

  it('resolves ambiguous autumn wall times to the earlier (BST) instant', () => {
    // 01:30 on 2024-10-27 happens twice in London; the first occurrence is
    // 00:30Z (still BST).
    expect(parseTimestamp('2024-10-27 01:30', LONDON_TZ)).toBe(utc('2024-10-27T00:30:00Z'))
  })

  it('shifts skipped spring wall times past the gap', () => {
    // 01:30 on 2024-03-31 never happens; pre-gap GMT offset applies, landing
    // at 01:30Z (= 02:30 BST).
    expect(parseTimestamp('2024-03-31 01:30', LONDON_TZ)).toBe(utc('2024-03-31T01:30:00Z'))
  })

  it('returns NaN for junk', () => {
    expect(parseTimestamp('not a date', LONDON_TZ)).toBeNaN()
    expect(parseTimestamp('', LONDON_TZ)).toBeNaN()
    expect(parseTimestamp('2024-13-40 99:99', LONDON_TZ)).toBeNaN()
  })
})
