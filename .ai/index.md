# Codebase index

Keep this file up to date after every code change. Update the relevant section
whenever a signature changes, a file is added or removed, or a responsibility
shifts. Do not let it drift from the actual code.

---

## Go packages

### `cmd/server` — process entry point
`main.go`, `otel.go`

| Symbol | Signature | Purpose |
|--------|-----------|---------|
| `main` | `func main()` | Calls `run()`; exits non-zero on error |
| `run` | `func run() error` | Loads config, sets up OTel, wires signal context, starts server |
| `setupOTel` | `func setupOTel(ctx context.Context, cfg config.Config) (func(), error)` | Initialises trace/metric/log providers, registers globals, bridges slog; returns flush-shutdown func |
| `exporterSet` | `struct{ tracer SpanExporter; reader Reader; logger Exporter }` | Groups the three signal exporters for a single transport |
| `buildExporters` | `func buildExporters(ctx, cfg) (exporterSet, error)` | Single dispatch: stdout / grpc / http based on cfg |
| `buildTracerProvider` | `func buildTracerProvider(exporter, res, ratio) *TracerProvider` | Wraps exporter in a TracerProvider; no transport logic |
| `buildMeterProvider` | `func buildMeterProvider(reader, res) *MeterProvider` | Wraps pre-built reader in a MeterProvider |
| `buildLoggerProvider` | `func buildLoggerProvider(exporter, res) *LoggerProvider` | Wraps exporter in a LoggerProvider |
| `checkOTelConnectivity` | `func checkOTelConnectivity(endpoint, transport string) error` | Dispatches to gRPC or HTTP probe |
| `buildStdoutExporters` | `func buildStdoutExporters(cfg) (exporterSet, error)` | `otel_exporters_stdout.go` — stdout exporters for dev |
| `buildGRPCExporters` | `func buildGRPCExporters(ctx, cfg) (exporterSet, error)` | `otel_exporters_grpc.go` — OTLP gRPC exporters |
| `checkOTelGRPC` | `func checkOTelGRPC(endpoint string) error` | `otel_exporters_grpc.go` — gRPC protocol-level connectivity probe |
| `buildHTTPExporters` | `func buildHTTPExporters(ctx, cfg) (exporterSet, error)` | `otel_exporters_http.go` — OTLP HTTP exporters |
| `checkOTelHTTP` | `func checkOTelHTTP(endpoint string) error` | `otel_exporters_http.go` — HTTP HEAD connectivity probe |

**To change:** startup/shutdown sequence → `run()`. OTel provider config → `otel.go`. Exporter construction → `otel_exporters_*.go`. Process exit code → `main()`.

---

### `internal/config` — runtime configuration
`config.go`, `config_test.go`

| Symbol | Signature | Purpose |
|--------|-----------|---------|
| `Config` | `struct{ Domain string; FrontendOrigin string; OTelEndpoint string; OTelTransport string; ServiceName string; OTelExportInterval time.Duration; OTelSamplingRatio float64; Port int }` | All runtime config; parsed from env vars |
| `Load` | `func Load() (Config, error)` | Parses OS environment; call once at startup |
| `LoadFrom` | `func LoadFrom(vars map[string]string) (Config, error)` | Parses from an in-memory map; use in tests instead of `os.Setenv` |

Env vars: `PORT` (default `8080`), `DOMAIN` (default `localhost`), `FRONTEND_ORIGIN` (optional), `OTEL_EXPORTER_OTLP_ENDPOINT` (empty → stdout exporters), `OTEL_EXPORTER_OTLP_PROTOCOL` (`grpc` or `http`, default `grpc`), `OTEL_SERVICE_NAME` (default `uk-energy-backtest`), `OTEL_SAMPLING_RATIO` (float 0–1, default `1.0`), `OTEL_METRIC_EXPORT_INTERVAL` (Go duration, default `15s`).

**To change:** add/remove a config variable → `Config` struct + this table.  
**Rule:** never call `os.Getenv` outside this package (enforced by golangci-lint `forbidigo`).

---

### `internal/costing` — tariff costing engine (pure, no HTTP imports)
`costing.go`, `profile.go`, `round.go` + tests incl. `fixture_test.go` (shared cross-language fixture)

| Symbol | Signature | Purpose |
|--------|-----------|---------|
| `BucketsPerDay` | `const = 48` | Half-hour buckets per local day |
| `Profile` | `struct{ ExportHH *[48]float64; ImportHH [48]float64; SuppliedDays int; GasKWh float64 }` | Date-stripped load profile; the front/back contract |
| `Band` | `struct{ From, To string; Rate float64 }` | "HH:MM" local-time rate window; `From > To` wraps midnight |
| `Electricity`, `Gas`, `Tariff` | structs | Tariff model, VAT-inclusive pence; `Tariff.Gas` optional |
| `Result` | `struct{ Name string; ImportPence, ExportCreditP, GasPence, StandingPence, NetPence float64; ImportRates, ExportRates [48]float64 }` | Costed outcome; rate arrays feed the chart overlay |
| `Cost` | `func Cost(p *Profile, t Tariff) (Result, error)` | Rules 2/4/6/7: band match, sums, standing, net = import+gas+standing−export |
| `Reading` | `struct{ IntervalStart time.Time; Consumption float64 }` | One metered half-hour |
| `BuildProfile` | `func BuildProfile(imp, exp, gas []Reading, gasIsM3 bool, cv float64, loc *time.Location) (Profile, error)` | Rules 1/3/5/6: local-time bucketing, per-slot half-even rounding, m³ conversion, supplied-day count |
| `RoundHalfEven2dp` | `func RoundHalfEven2dp(v float64) float64` | Banker's rounding to 0.01 kWh (rule 3) |
| `ConvertM3ToKWh` | `func ConvertM3ToKWh(m3, calorificValue float64) float64` | Rule 5; `DefaultCalorificValue = 39.5` |

The TS twin lives in `frontend/src/lib/profile.ts`; `testdata/shared/profile_fixture.json` keeps the two phases in agreement (both test suites consume it).

**To change costing rules:** this package + the TS twin + the fixture.

---

### `internal/octopus` — SSRF-locked Octopus Energy client
`client.go`, `client_test.go` (httptest only, never the network)

| Symbol | Signature | Purpose |
|--------|-----------|---------|
| `Client` | `struct{ http *http.Client; base string }` | Base URL hardcoded to `https://api.octopus.energy/v1`; test-only unexported override |
| `NewClient` | `func NewClient(timeout time.Duration) *Client` | Per-request (per page) timeout |
| `MeterPoints` | `struct{ ImportMPAN, ImportSerial, ExportMPAN, ExportSerial, GasMPRN, GasSerial string }` | Discovery result; export/gas empty when absent |
| `(*Client).DiscoverMeters` | `func (c *Client) DiscoverMeters(ctx, apiKey, account string) (MeterPoints, error)` | `GET /accounts/{n}/`; `is_export:true` point is the export meter |
| `(*Client).Consumption` | `func (c *Client) Consumption(ctx, apiKey, pointID, serial string, from, to time.Time, gas bool) ([]costing.Reading, error)` | Paginated consumption fetch; UTC `Z` params; never follows cross-host `next` |

Security invariants: path segments regex-validated (`^[A-Za-z0-9-]+$`), Basic auth with key as username/empty password, key never in URLs or error strings.

---

### `internal/server` — HTTP server
`server.go`, `middleware.go`, `status.go`, `static.go`, `errors.go`, `cost.go`, `octopus.go` + tests incl. `middleware_redaction_test.go`

| Symbol | Signature | Purpose |
|--------|-----------|---------|
| `Server` | `struct{ echo *echo.Echo; addr string }` | Wraps Echo and the listen address |
| `New` | `func New(cfg config.Config, gitSHA, buildTime string) *Server` | Creates Echo instance, registers middleware and routes |
| `(*Server).Start` | `func (s *Server) Start(ctx context.Context) error` | Runs server until `ctx` is cancelled, then shuts down gracefully (10 s timeout) |
| `(*Server).Handler` | `func (s *Server) Handler() http.Handler` | Returns the Echo instance as `http.Handler`; use in tests with `httptest` |
| `otelMiddleware` | `func otelMiddleware(serviceName string) echo.MiddlewareFunc` | Custom Echo v5 OTel middleware: extracts W3C trace context, creates server span, records HTTP method/path/status |
| `healthHandler` | `func healthHandler(c *echo.Context) error` | `GET /api/v1/health` → `{"status":"ok"}` |
| `statusHandler` | `func statusHandler(gitSHA, buildTime string) echo.HandlerFunc` | `GET /api/v1/status` → `{"status":"ok","git_sha":"…","build_time":"…"}` |
| `registerStatic` | `func registerStatic(e *echo.Echo)` | Serves embedded Vue SPA (Mode 1 only; delete this file to move to Mode 2) |
| `jsonError` | `func jsonError(c *echo.Context, status int, code, msg string) error` | `errors.go` — standard `{"error":{code,message}}` envelope |
| `costHandler` | `func costHandler(c *echo.Context) error` | `cost.go` — `POST /api/v1/cost`: stateless profile+tariffs → results |
| `profilePayload` | wire struct with slice buckets | `cost.go` — rejects non-48-bucket arrays (fixed arrays would silently truncate) |
| `octopusCostHandler` | `func octopusCostHandler(fetcher meterFetcher, loc *time.Location) echo.HandlerFunc` | `octopus.go` — `POST /api/v1/octopus/cost`: fetch→aggregate→cost→discard; key via `X-Octopus-Key` header only; `Cache-Control: no-store` |
| `meterFetcher` | interface over the octopus client | `octopus.go` — lets handler tests stub the upstream |

Note: `otelecho` (the contrib package) targets Echo v4 and cannot be used here. `otelMiddleware` is the Echo v5 replacement.

Security: the middleware records method/path/status only — no request headers — so the Octopus key cannot leak into logs or spans. `middleware_redaction_test.go` proves this with an in-memory span exporter and a captured slog handler; it must keep passing.

**To add a route:** `New()` in `server.go`.  
**To change graceful timeout:** `Start()` in `server.go`.  
**To migrate to decoupled deployment:** delete `static.go` and remove its call in `New()`.

---

### `internal/ui` — embedded frontend assets
`embed.go`

| Symbol | Signature | Purpose |
|--------|-----------|---------|
| `FS` | `var FS embed.FS` | Compiled Vue SPA embedded at build time via `//go:embed all:dist` |

**To populate:** `npm run build` in `frontend/`; output goes to `internal/ui/dist/`.

---

## Frontend (`frontend/src/`)

### `main.ts` — app entry point
Mounts the Vue app onto `#app`. No exports.

---

### `App.vue` — root component
Single-page flow: usage data (CSV/Octopus/saved datasets) → tariff editors → results charts. Owns the streams/tariffs/results state; the privacy invariant (only profiles cross the wire) is enforced here and by the API client signatures.

---

### `api/client.ts` — typed API client
All `fetch` calls live here. No raw `fetch` elsewhere.

| Export | Signature | Purpose |
|--------|-----------|---------|
| `HealthResponse` | `interface{ status: string }` | Response shape for `/api/v1/health` |
| `checkHealth` | `function checkHealth(): Promise<HealthResponse>` | `GET /api/v1/health` |
| `StatusResponse` | `interface{ status: string; git_sha: string; build_time: string }` | Response shape for `/api/v1/status` |
| `getStatus` | `function getStatus(): Promise<StatusResponse>` | `GET /api/v1/status` |
| `ApiError` | `class extends Error { status: number; code: string }` | Carries the server error envelope |
| `postCost` | `function postCost(profile: Profile, tariffs: Tariff[]): Promise<CostResponse>` | `POST /api/v1/cost` — deliberately only accepts a Profile, never raw rows |
| `postOctopusCost` | `function postOctopusCost(req: OctopusCostRequest, apiKey: string): Promise<OctopusCostResponse>` | `POST /api/v1/octopus/cost`; key in `X-Octopus-Key` header; 95 s timeout |

**To add an API call:** add a function here, typed against the OpenAPI contract.

---

### `lib/` — pure modules (no network, no DOM)

| Module | Key exports | Purpose |
|--------|------------|---------|
| `types.ts` | `Profile`, `Band`, `Tariff`, `CostResult`, `RawReading`, `BUCKETS_PER_DAY` | Wire types mirroring Go JSON tags exactly (snake_case) |
| `rounding.ts` | `roundHalfEven2dp`, `convertM3ToKWh`, `DEFAULT_CALORIFIC_VALUE` | TS twins of `internal/costing/round.go` |
| `timezone.ts` | `localBucket`, `parseTimestamp`, `wallTimeToUtcMs`, `LONDON_TZ` | Intl-based tz conversion (no tz library); ambiguous autumn wall times → earlier instant |
| `profile.ts` | `buildProfile`, `detectGranularityMinutes` | TS twin of `costing.BuildProfile`; fixture-verified |
| `csv.ts` | `parseCsv`, `presets`, `detectPreset`, `ColumnMapping` | papaparse wrapper; Octopus/n3rgy presets + generic mapper |
| `tariffStore.ts` | `loadTariffs`, `saveTariffs`, `migrate`, `starterPresets`, `clearTariffs` | localStorage, versioned schema key `ukeb.tariffs` |
| `datasetStore.ts` | `saveDataset`, `listDatasets`, `loadDataset`, `deleteDataset`, `clearDatasets` | IndexedDB (`idb`), raw readings stay on-device |
| `echarts.ts` | `VChart`, `BUCKET_LABELS`, `pounds` | Modular ECharts registration — import VChart only from here |

Tests in `lib/__tests__/`; `fixture.spec.ts` consumes `testdata/shared/profile_fixture.json` (same file as the Go suite).

---

### `components/`

| Component | Purpose |
|-----------|---------|
| `CsvImport.vue` | Three-stream upload, preset/column mapping, tz + gas-unit selectors |
| `OctopusConnect.vue` | Key/account/period form; key memory-only by default, opt-in persistence behind a warning |
| `TariffEditor.vue` | One tariff; local copy + `update:modelValue` (parent re-keys on list restructure); band rows constrained to :00/:30 |
| `CostComparisonChart.vue` | Stacked bars, export as negative credit, net labelled |
| `LoadProfileChart.vue` | 48-bucket profile with selected tariff's rate overlay |
| `ChartDataTable.vue` | Toggleable table fallback per chart (a11y gate) |
| `DatasetManager.vue` | Save/load/delete named datasets in IndexedDB |
| `DeleteMyData.vue` | Reka AlertDialog; wipes localStorage + IndexedDB + remembered key |

---

### `env.d.ts` — environment variable types
Declares `ImportMetaEnv` so `import.meta.env.VITE_*` variables are typed.  
**To add a frontend env var:** add a `readonly VITE_FOO?: string` entry here.

---

## Navigation guide

| I want to… | Go to… |
|------------|--------|
| Read the API contract | `api/openapi.yaml` |
| Add a new API route | `internal/server/server.go` → `New()` |
| Add a config variable | `internal/config/config.go` → `Config` struct; update this index |
| Change startup / shutdown logic | `cmd/server/main.go` → `run()` |
| Change graceful shutdown timeout | `internal/server/server.go` → `Start()` |
| Test config parsing | Use `config.LoadFrom(map[string]string{...})` |
| Add a frontend API call | `frontend/src/api/client.ts` |
| Add a frontend env var | `frontend/src/env.d.ts` → `ImportMetaEnv` |
| Change the landing page UI | `frontend/src/App.vue` |
| Change CORS origin | Set `FRONTEND_ORIGIN` env var — no code change needed |
| Migrate to decoupled deployment | Delete `internal/server/static.go`; remove its call in `New()` |
| Add / change a golangci-lint rule | `.golangci.yml` |
| Add a CI job | `.github/workflows/ci.yml` |
| Add a security scan | `.github/workflows/security.yml` |
| Change costing rules | `internal/costing/` + `frontend/src/lib/profile.ts` + shared fixture |
| Change the shared fixture | `testdata/shared/profile_fixture.json` (recompute expectations on both sides) |
| Add a CSV preset | `frontend/src/lib/csv.ts` → `presets` |
| Add a tariff starter preset | `frontend/src/lib/tariffStore.ts` → `starterPresets` |
| Change Octopus upstream behaviour | `internal/octopus/client.go` (keep SSRF invariants) |
| Run local dev environment | `compose.yaml` (`docker compose up`) |
| Deploy | `fly.toml` (`fly deploy`); embedded single binary |
