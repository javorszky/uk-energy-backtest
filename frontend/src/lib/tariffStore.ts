/**
 * Browser-local tariff persistence. Tariffs are small and structured, so
 * localStorage is the right home (usage datasets go to IndexedDB instead —
 * see datasetStore.ts). The payload is versioned with a schema key so future
 * shape changes can migrate rather than discard.
 */
import type { Tariff } from './types'

export const TARIFFS_STORAGE_KEY = 'ukeb.tariffs'

/** Bump when the serialised shape changes; add a migration in migrate(). */
const CURRENT_SCHEMA_VERSION = 1

interface StoredTariffs {
  version: number
  tariffs: Tariff[]
}

/**
 * Starter presets the user can load and edit. Rates are realistic shapes,
 * not live quotes — the user is expected to overwrite them with the numbers
 * from their own tariff/quote.
 */
export const starterPresets: Tariff[] = [
  {
    name: 'Octopus Flux (edit rates to match your quote)',
    electricity: {
      standing_charge: 47.85,
      import_default: 26.8,
      import_bands: [
        { from: '02:00', to: '05:00', rate: 16.08 },
        { from: '16:00', to: '19:00', rate: 37.52 },
      ],
      export_default: 15.74,
      export_bands: [
        { from: '02:00', to: '05:00', rate: 5.02 },
        { from: '16:00', to: '19:00', rate: 26.46 },
      ],
    },
    gas: { standing_charge: 29.6, rate: 6.1 },
  },
  {
    name: 'Flat single rate (edit to match your quote)',
    electricity: {
      standing_charge: 53.8,
      import_default: 24.5,
      import_bands: [],
      export_default: 15.0,
      export_bands: [],
    },
    gas: { standing_charge: 29.6, rate: 6.2 },
  },
  {
    name: 'EV overnight (edit to match your quote)',
    electricity: {
      standing_charge: 47.85,
      import_default: 27.7,
      import_bands: [{ from: '23:30', to: '05:30', rate: 7.0 }],
      export_default: 15.0,
      export_bands: [],
    },
  },
]

function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T
}

/** A deep copy of the presets, safe to hand to a mutable editor. */
export function presetTariffs(): Tariff[] {
  return clone(starterPresets)
}

function isBandArray(v: unknown): boolean {
  return (
    Array.isArray(v) &&
    v.every(
      (b) =>
        typeof b === 'object' &&
        b !== null &&
        typeof (b as Record<string, unknown>).from === 'string' &&
        typeof (b as Record<string, unknown>).to === 'string' &&
        typeof (b as Record<string, unknown>).rate === 'number',
    )
  )
}

function isTariff(v: unknown): v is Tariff {
  if (typeof v !== 'object' || v === null) return false
  const t = v as Record<string, unknown>
  if (typeof t.name !== 'string') return false
  const e = t.electricity as Record<string, unknown> | undefined
  if (typeof e !== 'object' || e === null) return false
  return (
    typeof e.standing_charge === 'number' &&
    typeof e.import_default === 'number' &&
    isBandArray(e.import_bands) &&
    isBandArray(e.export_bands)
  )
}

/**
 * Validate and migrate a raw parsed payload to the current schema. Returns
 * null when the payload is unusable (corrupt JSON shape, unknown future
 * version) — the caller falls back to presets.
 */
export function migrate(raw: unknown): Tariff[] | null {
  if (typeof raw !== 'object' || raw === null) return null
  const stored = raw as Partial<StoredTariffs>
  if (stored.version !== CURRENT_SCHEMA_VERSION) return null
  if (!Array.isArray(stored.tariffs) || !stored.tariffs.every(isTariff)) return null
  return stored.tariffs
}

/** Load saved tariffs; presets when nothing valid is stored. */
export function loadTariffs(): Tariff[] {
  try {
    const raw = localStorage.getItem(TARIFFS_STORAGE_KEY)
    if (raw === null) return presetTariffs()
    const migrated = migrate(JSON.parse(raw))
    return migrated && migrated.length > 0 ? migrated : presetTariffs()
  } catch {
    // Corrupt JSON or storage unavailable (private mode quota) — start fresh.
    return presetTariffs()
  }
}

export function saveTariffs(tariffs: Tariff[]): void {
  const payload: StoredTariffs = { version: CURRENT_SCHEMA_VERSION, tariffs }
  localStorage.setItem(TARIFFS_STORAGE_KEY, JSON.stringify(payload))
}

/** Part of the "delete my data" control. */
export function clearTariffs(): void {
  localStorage.removeItem(TARIFFS_STORAGE_KEY)
}
