/**
 * Wire types shared with the Go backend. Property names mirror the Go JSON
 * tags exactly (snake_case) — see internal/costing/costing.go and
 * api/openapi.yaml. Do not rename fields here without changing both.
 */

export const BUCKETS_PER_DAY = 48

/**
 * Compact, date-stripped load profile: kWh per local (Europe/London)
 * half-hour of day. This is the only usage-derived data that ever crosses
 * the wire — raw readings stay on the device.
 */
export interface Profile {
  supplied_days: number
  import_hh: number[]
  export_hh?: number[]
  gas_kwh: number
}

/** Time-of-use rate window; "HH:MM" local clock times on :00/:30. */
export interface Band {
  from: string
  to: string
  rate: number
}

export interface ElectricityTariff {
  standing_charge: number
  import_default: number
  import_bands: Band[]
  export_default: number
  export_bands: Band[]
}

export interface GasTariff {
  standing_charge: number
  rate: number
}

/** All rates are VAT-inclusive pence, as a consumer quote shows them. */
export interface Tariff {
  name: string
  electricity: ElectricityTariff
  gas?: GasTariff
}

/** Costed outcome for one tariff; money values are pence. */
export interface CostResult {
  name: string
  import_p: number
  export_credit_p: number
  gas_p: number
  standing_p: number
  net_p: number
  import_rates: number[]
  export_rates: number[]
}

export interface CostResponse {
  results: CostResult[]
}

export interface OctopusCostRequest {
  account: string
  period_from: string
  period_to: string
  gas_unit?: 'kwh' | 'm3'
  calorific_value?: number
  tariffs: Tariff[]
}

export interface OctopusCostResponse {
  profile: Profile
  results: CostResult[]
}

/** One metered half-hour, after parsing: a UTC instant plus a kWh (or m³) value. */
export interface RawReading {
  /** UTC epoch milliseconds of the interval start. */
  ts: number
  /** Consumption for the period — kWh, or m³ for SMETS2 gas. */
  value: number
}
