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

/**
 * Costed outcome for one tariff; money values are pence. Electricity and
 * gas are separate entities, each with its own standing charge and
 * subtotal; total_p is the combined net (rule 7 regrouped by fuel).
 */
export interface CostResult {
  name: string
  import_p: number
  export_credit_p: number
  elec_standing_p: number
  /** import + elec standing − export credit */
  elec_net_p: number
  gas_p: number
  gas_standing_p: number
  /** gas usage + gas standing */
  gas_total_p: number
  total_p: number
  import_rates: number[]
  export_rates: number[]
}

export interface CostResponse {
  results: CostResult[]
}

/**
 * Response of POST /api/v1/octopus/usage — the account's raw half-hourly
 * readings, relayed to their owner so the browser can run the same
 * on-device pipeline as the CSV path (profile build, Agile backtest,
 * dataset save). ts is UTC epoch milliseconds.
 */
export interface OctopusUsageResponse {
  import: RawReading[]
  export?: RawReading[]
  gas?: RawReading[]
}

/** Response of POST /api/v1/octopus/tariff — a prefilled tariff from the
 * account's current agreements, plus provenance and any mapping warnings
 * (e.g. Agile collapsing to a flat average). */
export interface OctopusTariffResponse {
  tariff: Tariff
  codes: {
    import: string
    export?: string
    gas?: string
  }
  warnings: string[]
}

/** One metered half-hour, after parsing: a UTC instant plus a kWh (or m³) value. */
export interface RawReading {
  /** UTC epoch milliseconds of the interval start. */
  ts: number
  /** Consumption for the period — kWh, or m³ for SMETS2 gas. */
  value: number
}
