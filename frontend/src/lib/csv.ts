/**
 * Supplier-agnostic CSV ingestion. Parsing happens entirely client-side
 * (papaparse, streaming) — raw rows never leave the device; the caller feeds
 * the readings into buildProfile and POSTs only the profile.
 */
import Papa from 'papaparse'

import { parseTimestamp, LONDON_TZ } from './timezone'
import type { RawReading } from './types'

export interface ColumnMapping {
  /** Header name of the timestamp (period start) column. */
  timestampCol: string
  /** Header name of the consumption (kWh or m³) column. */
  valueCol: string
}

export interface CsvPreset {
  id: 'octopus' | 'n3rgy' | 'generic'
  label: string
  /** True when the headers look like this preset's format. */
  detect: (headers: string[]) => boolean
  /** Pre-filled mapping; null when detection cannot identify the columns. */
  mapping: (headers: string[]) => ColumnMapping | null
  /**
   * Forced source timezone, when the format documents one (n3rgy exports
   * UTC). Null means the user's timezone selector applies.
   */
  forcedTz: string | null
}

/**
 * Header lookup that shrugs off case and stray whitespace — several
 * suppliers pad header cells (" Consumption (kwh)").
 */
function findHeader(headers: string[], ...candidates: string[]): string | null {
  for (const c of candidates) {
    const hit = headers.find((h) => h.trim().toLowerCase() === c.toLowerCase())
    if (hit !== undefined) return hit
  }
  return null
}

export const presets: CsvPreset[] = [
  {
    id: 'octopus',
    label: 'Octopus Energy export',
    detect: (headers) =>
      findHeader(headers, 'Consumption (kwh)', 'Consumption (kWh)') !== null &&
      findHeader(headers, 'Start', 'interval_start') !== null,
    mapping: (headers) => {
      const ts = findHeader(headers, 'Start', 'interval_start')
      const v = findHeader(headers, 'Consumption (kwh)', 'Consumption (kWh)')
      return ts && v ? { timestampCol: ts, valueCol: v } : null
    },
    // Octopus timestamps carry explicit offsets, which win over any selector.
    forcedTz: null,
  },
  {
    id: 'n3rgy',
    label: 'n3rgy export',
    detect: (headers) =>
      findHeader(headers, 'timestamp (UTC)') !== null &&
      findHeader(headers, 'energyConsumption (kWh)', 'energyConsumption (m3)') !== null,
    mapping: (headers) => {
      const ts = findHeader(headers, 'timestamp (UTC)')
      const v = findHeader(headers, 'energyConsumption (kWh)', 'energyConsumption (m3)')
      return ts && v ? { timestampCol: ts, valueCol: v } : null
    },
    // n3rgy documents its bare timestamps as UTC.
    forcedTz: 'UTC',
  },
  {
    id: 'generic',
    label: 'Generic CSV (map columns manually)',
    detect: () => true,
    mapping: () => null,
    forcedTz: null,
  },
]

/** First preset whose detect() matches, generic as the fallback. */
export function detectPreset(headers: string[]): CsvPreset {
  return presets.find((p) => p.detect(headers)) ?? presets[presets.length - 1]
}

export interface ParsedCsv {
  readings: RawReading[]
  warnings: string[]
}

const MAX_REPORTED_BAD_ROWS = 5

/**
 * Parse a CSV file (or raw string, handy in tests) into readings using the
 * given column mapping. Bare timestamps are interpreted in sourceTz;
 * timestamps with explicit offsets/Z keep their own offset.
 */
export function parseCsv(
  file: File | string,
  mapping: ColumnMapping,
  sourceTz: string = LONDON_TZ,
): Promise<ParsedCsv> {
  return new Promise((resolve, reject) => {
    const readings: RawReading[] = []
    const warnings: string[] = []
    let badRows = 0
    let rowNum = 1 // header row

    Papa.parse<Record<string, string>>(file, {
      header: true,
      skipEmptyLines: 'greedy',
      transformHeader: (h) => h.trim(),
      step: (row) => {
        rowNum++
        const tsRaw = row.data[mapping.timestampCol.trim()]
        const valueRaw = row.data[mapping.valueCol.trim()]
        const ts = parseTimestamp(tsRaw ?? '', sourceTz)
        const value = Number(valueRaw)
        if (
          Number.isNaN(ts) ||
          valueRaw === undefined ||
          valueRaw.trim() === '' ||
          Number.isNaN(value)
        ) {
          badRows++
          if (badRows <= MAX_REPORTED_BAD_ROWS) {
            warnings.push(
              `Row ${rowNum}: could not parse timestamp "${tsRaw}" / value "${valueRaw}"; skipped.`,
            )
          }
          return
        }
        readings.push({ ts, value })
      },
      complete: () => {
        if (badRows > MAX_REPORTED_BAD_ROWS) {
          warnings.push(`…and ${badRows - MAX_REPORTED_BAD_ROWS} more unparseable rows skipped.`)
        }
        if (readings.length === 0) {
          warnings.push('No usable rows found — check the column mapping and timezone.')
        }
        resolve({ readings, warnings })
      },
      error: (err: Error) => reject(new Error(`CSV parse failed: ${err.message}`)),
    })
  })
}
