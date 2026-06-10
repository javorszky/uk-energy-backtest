# Build brief: energy tariff backtester (web app)

Supersedes the CLI brief. Same domain logic; now a multi-user web app that
keeps no server-side state, built on the `javorszky/go-vue-project-base`
template.

## Objective

A web app where anyone can (1) provide their half-hourly energy usage, (2)
define one or more tariffs, and (3) get back a cost calculation and
visualisation comparing them on their real usage. The anchor case: a solar +
battery + EV household on a time-of-use export tariff (e.g. Octopus Flux)
checking what a competitor quote (e.g. EDF) *would have* cost — import **and**
export **and** gas, because export earnings usually decide it and most
comparison tools ignore them.

## Architecture: server-side costing, process and discard

The **Go backend does the costing** and stores nothing — it processes each
request and discards. This fits the template's "Go does the logic" intent and
reuses the Go costing engine from the CLI brief rather than maintaining a second
copy in TypeScript.

The privacy story is kept intact by a small but important move: **the browser
never sends raw half-hourly data to the server.** Band matching depends only on
the local clock time of each half-hour, never on the calendar date, so the
frontend pre-aggregates usage into a compact **half-hourly load profile** — 48
buckets per stream (one per local half-hour of day), plus a supplied-day count —
and sends only that. A six-month dataset collapses to ~150 numbers, the raw
timestamps stay on the device, and the same profile is exactly what the daily
load-profile chart needs. See "Load profile" below for the contract.

Consequences:

- **CSV path:** parsed and aggregated to a profile **client-side**; the raw data
  never leaves the device. Only the anonymised profile + the tariffs are POSTed
  to the cost endpoint.
- **Octopus path:** the backend fetches the raw half-hourly data (browsers can't
  call Octopus directly — no permissive CORS headers), aggregates it to the same
  profile server-side, and discards the raw data and the key once it responds.
- **One costing engine, in Go**, fed by an identical profile from either path.
- Recalculating after a tariff tweak re-POSTs the profile (tiny) — no raw-data
  round-trips, so it stays snappy.

Precision note: a profile resolves at the data's native half-hour granularity,
so tariff band boundaries should fall on `:00` / `:30`. Sub-half-hour boundaries
can't be costed exactly from half-hourly metering under any architecture.

## Base template conventions to honour

Build inside a repo created from `javorszky/go-vue-project-base`. Honour its
existing contracts so the work merges cleanly:

- **Backend:** Go 1.26 + Echo, pure JSON API under `/api/v1/`. Entry
  `cmd/server/main.go`; routes/setup in `internal/server/`. Error envelope
  `{ "error": { "code": "…", "message": "…" } }`. OpenTelemetry is wired.
- **Frontend:** Vue 3 with `<script setup lang="ts">` only (Options API and
  `setup()` are ESLint-blocked). Reka UI headless primitives + Tailwind v4.
  **All HTTP goes through `src/api/`** — no raw `fetch` in components; that
  layer is the only reader of `VITE_API_URL`.
- **Embedded deploy:** SPA embedded via `//go:embed`, isolated in
  `internal/server/static.go`. Don't spread embed code elsewhere.
- **Lint/security gate:** golangci-lint (incl. `wrapcheck`, `gosec`, `cyclop`),
  gci import sections (stdlib / third-party / internal) + gofumpt; ESLint with
  Vue a11y (WCAG) rules; Trivy, govulncheck, CodeQL, Zizmor all run in CI. New
  code must pass all of them — notably: wrap every returned error, and charts
  need accessible fallbacks (see Visualisation).
- **No user accounts / no app auth.** The template ships Bearer-token auth; this
  app doesn't need it. The only credential anywhere is the user's Octopus API
  key, handled per-request (below). Drop or ignore the auth scaffolding.
- **Commits:** use the `Assisted-By` git trailer, not `Co-authored-By`.

## Data ingestion (both paths)

### A. CSV upload — supplier-agnostic, client-side parse + aggregate

Suppliers and data brokers (n3rgy, Hildebrand/Bright, Loop, Octopus, individual
suppliers) all export different shapes, so don't hardcode one. Provide:

- A generic importer where the user maps columns: a timestamp column and a kWh
  column, with the period granularity detected (expect 30-min; warn on hourly /
  daily and cost them as-is).
- A couple of presets for common formats (at least n3rgy and Octopus CSV
  exports) that pre-fill the mapping.
- Separate uploads for **import**, **export**, and **gas** (a household may have
  one, two, or three files), each with its own mapping.
- A **timezone selector** for the CSV timestamps, default Europe/London. This is
  load-bearing for the local-time bucketing (see Load profile / Costing rule 1).
- Gas unit selector — kWh or m³ (m³ needs conversion, rule 5).

Parse with a streaming CSV parser (papaparse is the usual Vue choice); a
six-month half-hourly file is ~8,700 rows per stream, trivial in-browser. The
frontend then builds the load profile (below) and **sends only the profile** to
the cost endpoint — the parsed rows stay on the device.

### B. Connect Octopus — via the backend

The user pastes their Octopus API key and account number. The browser sends them
to a backend endpoint that fetches the half-hourly data, aggregates it into the
same load profile, costs the requested tariffs, and discards the raw data and
the key before responding.

Backend security (these are `gosec`/CodeQL-relevant, get them right):

- **Per-request only.** The key arrives in a request header (e.g.
  `X-Octopus-Key`), is used to build the upstream Basic auth, and is never
  stored, cached, or written to logs/OTel spans. Explicitly redact that header
  from tracing/logging middleware.
- **SSRF lockdown.** The backend hardcodes the upstream host
  (`api.octopus.energy`) and an allowlist of path prefixes (account +
  consumption only). It must not accept a caller-supplied URL.
- The browser may keep the key in memory for the session; persisting it to
  browser storage is opt-in only, with a clear warning. Default: not persisted.

## Load profile (the contract between front and back)

Both ingest paths converge on this compact, date-stripped structure. Band
matching depends only on local half-hour-of-day, so this is exact, not lossy
(to the data's native resolution), and it is also the daily-profile chart data.

```json
{
  "supplied_days": 184,
  "import_hh": [/* 48 floats: kWh in each local half-hour 00:00..23:30 */],
  "export_hh": [/* 48 floats, or omitted if no export meter */],
  "gas_kwh": 1234.5
}
```

Build rule: convert each reading's timestamp to **Europe/London local time**,
round the slot to 0.01 kWh (round-half-to-even, rule 3), and add it to bucket
`floor(localMinuteOfDay / 30)` for its stream. Gas is flat, so just sum it.
`supplied_days` = count of distinct local calendar dates with import readings.

`POST /api/v1/cost` takes `{ profile, tariffs: [...] }` and returns a costed
result per tariff. It is fully stateless.

## Octopus API reference (for the proxy)

Base `https://api.octopus.energy/v1`. Auth is HTTP Basic with the API key as
**username and an empty password** (`SetBasicAuth(key, "")`).

- **Discover meters:** `GET /accounts/{account-number}/` →
  `properties[].electricity_meter_points[]` (`mpan`, `is_export` bool,
  `meters[].serial_number`) and `gas_meter_points[]` (`mprn`, serials). The
  point with `is_export: true` is the export meter.
- **Consumption:**
  `GET /electricity-meter-points/{mpan}/meters/{serial}/consumption/` and the
  `gas-meter-points/{mprn}/...` equivalent. Params: `period_from`, `period_to`
  (ISO 8601, **UTC with trailing Z**), `page_size` (large, e.g. 25000),
  `order_by=period`. Results: `{ consumption, interval_start, interval_end }`.
  Follow `next` to paginate.
- **Quirk:** the export MPAN returns its data under the same `consumption`
  field — it is the amount *exported*.
- **Gas units:** SMETS1 returns kWh; SMETS2 returns m³ (convert, rule 5).

## Tariff model

A tariff is entered through a form and persisted browser-local. All rates are
**VAT-inclusive pence**, exactly as a consumer quote shows them — the app
applies no VAT. Shape (also the localStorage serialisation):

```json
{
  "name": "Octopus Flux (current)",
  "electricity": {
    "standing_charge": 47.85,
    "import_default": 32.0,
    "import_bands": [
      { "from": "02:00", "to": "05:00", "rate": 16.0 },
      { "from": "16:00", "to": "19:00", "rate": 48.0 }
    ],
    "export_default": 15.0,
    "export_bands": [
      { "from": "02:00", "to": "05:00", "rate": 7.0 },
      { "from": "16:00", "to": "19:00", "rate": 45.0 }
    ]
  },
  "gas": { "standing_charge": 28.0, "rate": 6.2 }
}
```

- `*_default` = fallback rate for any half-hour not covered by a band (the
  "everything else" / peak-day rate).
- `*_bands` is ordered; first match wins; a band with `from` > `to` wraps past
  midnight.
- Gas block and export bands are optional.

The form should let users add/remove bands visually and ship 2–3 starter
presets (Flux, a flat single-rate, a generic EV overnight tariff) they can edit.

## Costing rules

Split into two phases. **Profile build** (rules 1, 3, 5, 6) runs at aggregation
time — client-side for CSV, server-side in Go for the Octopus path. **Costing**
(rules 2, 4, 7) runs in the Go cost endpoint against the 48-bucket profile.

1. **Bucket in Europe/London local clock time, not UTC.** Convert each reading's
   timestamp to London local before bucketing, so a bucket index maps to a fixed
   local half-hour (`bucket i` ⇒ local minute `i*30`). This is why the CSV
   timezone selector and the UTC `Z` query params matter — get it wrong and
   peak/off-peak shift by an hour for half the year (BST). On the client use
   `Temporal` or a tz-aware lib (don't trust the browser's local zone); in Go use
   `time.LoadLocation("Europe/London")`.
2. **Match bands by bucket.** For bucket `i`, the rate is the first band whose
   window contains local minute `i*30`, else the default. Bands may wrap
   midnight: `from > to` ⇒ covers `t >= from || t < to`; else `from <= t < to`.
3. **Round each half-hour to 0.01 kWh before bucketing**, round-half-to-even, to
   match how Octopus bills (round per slot, then sum into the bucket, then price).
4. **Import** = Σ over 48 buckets (`import_hh[i] × rate(i)`). **Export** = Σ
   (`export_hh[i] × rate(i)`), subtracted as a credit. **Gas** = `gas_kwh × gas
   rate` (flat, no bands).
5. **Gas m³→kWh** (at profile build): `kWh = m3 × 1.02264 × calorific_value /
   3.6`, calorific value default 39.5 MJ/m³, configurable. Skip when already kWh.
6. **Standing charges** = `supplied_days × (elec standing + gas standing)`, where
   `supplied_days` = count of distinct local calendar dates with import readings
   (so data gaps don't mis-charge). Computed at profile build, carried in the
   profile.
7. **Net** = import + gas + standing − export credit.

## Visualisation

At minimum:

- **Cost comparison** — one stacked bar per tariff: import, gas, standing
  (positive) and export credit (negative), with the net labelled. This is the
  headline answer.
- **Daily load profile vs rate bands** — the profile's 48 buckets, overlaid with
  the selected tariff's rate bands, so the user sees *where* their usage lands
  relative to peak/off-peak. This is what explains the comparison, and it's the
  profile you already computed — no extra data needed.
- Optional: cumulative net cost over time, one line per tariff. This needs
  day-level resolution, which the 48-bucket profile doesn't carry — compute it
  client-side from the raw data still held in IndexedDB, or skip it.

Charting lib is your call; ECharts handles bars + heatmap + lines in one
dependency and copes with thousands of points. If the time series feels heavy,
uPlot is the fast fallback. **Accessibility:** the ESLint a11y rules will flag
chart-only data — provide a toggleable data table alongside each chart and
proper labels/aria, don't rely on colour alone.

## Storage (browser-local)

- **Tariffs:** small and structured → `localStorage` (JSON, versioned with a
  schema key so future shape changes can migrate).
- **Usage datasets:** potentially tens of thousands of rows → `IndexedDB`, keyed
  by a user-named import (e.g. "2025 full year"). Don't put these in
  localStorage.
- A clear "delete my data" control that wipes both, surfaced prominently — it
  reinforces that nothing is server-side.

## Backend specifics on this template

- Add two endpoints in `internal/server/`: `POST /api/v1/cost` (stateless,
  profile + tariffs → results) and `POST /api/v1/octopus/cost` (key header +
  account + period → fetch, aggregate, cost, discard). Keep handlers thin,
  return the standard error envelope, wrap all errors (`wrapcheck`).
- The costing engine ports directly from the CLI brief's Go code — same band
  matching, round-half-to-even, gas conversion. Put it in `internal/costing/`
  as a pure package with no Echo/HTTP imports, so it unit-tests cleanly.
- **tzdata is required.** The binary runs as UTC in the scratch image and the
  Octopus path buckets in London local time, so add `import _ "time/tzdata"` (a
  one-line blank import) so `time.LoadLocation("Europe/London")` works without
  touching the Dockerfile.
- Ensure the OTel/logging middleware redacts the Octopus key header; verify no
  raw consumption or key ends up in a span or log line.

## Frontend specifics on this template

- New views as Vue 3 `<script setup lang="ts">` SFCs; Reka primitives + Tailwind
  v4 for the tariff form, upload, and results.
- All backend calls go through `src/api/`. CSV parsing and profile-building are
  pure TS modules (no network) so they're unit-testable under Vitest.
- The TS side owns **profile build**, so test it hard: BST/GMT boundary days
  (a slot's local bucket across the clock change), round-half-to-even at the
  .005 boundary, m³ conversion, supplied-day counting with gaps, and a
  missing-export / missing-gas dataset.
- The authoritative cost numbers come from the Go engine; mirror the same
  boundary cases in the Go `internal/costing` tests so both phases agree.

## Gotchas (where this goes wrong)

- London-local bucketing across BST/GMT (rule 1) — the single biggest risk, and
  it must give the same answer on both the TS (CSV) and Go (Octopus) sides.
- Treating export as usage instead of a credit, or sign-flipping it twice.
- Per-slot 0.01 kWh round-half-to-even *before* bucketing (rule 3).
- SMETS1 kWh vs SMETS2 m³ gas.
- Octopus key or raw consumption leaking into logs/traces, or the backend
  accepting a caller-supplied URL (SSRF).
- Accidentally sending raw half-hourly rows from the CSV path — only the profile
  should ever cross the wire.
- Heterogeneous CSV formats — don't assume one supplier's column layout.

## Acceptance criteria

- A user can upload import/export/gas CSVs (or connect Octopus), define two
  tariffs, and see a correct comparison with nothing persisted server-side.
- On the CSV path the server receives only the profile — never raw rows. On the
  Octopus path the key is never logged and only `api.octopus.energy` is called.
- Total import kWh shown matches the user's supplier dashboard for the period.
- TS profile-build and Go costing tests cover the rule-1 BST boundary and rule-3
  rounding, and the two phases agree on a shared fixture.
- A tariff with `import_bands: []` prices every bucket at `import_default`; a
  tariff with no gas block and a dataset with no export both work.
- New code passes the template's full lint + security CI gate.

## Decisions left for you

- CSV preset list — which suppliers/brokers to ship mappings for first.
- Charting library (ECharts vs uPlot vs other).
- Whether to offer an n3rgy/Hildebrand API connector later as a third ingest
  path (CSV covers them for now).
- For the EDF comparison specifically: confirm what export/SEG rate the quote
  assumes — if it's silent on export, that figure is the one to chase, because
  it's usually what decides a Flux-vs-EDF comparison.