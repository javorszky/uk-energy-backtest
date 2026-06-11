import type { RateSlot } from '../lib/agile'
import type { OAuthConfig, TokenResponse } from '../lib/oauth'
import type {
  CostResponse,
  OctopusCostRequest,
  OctopusCostResponse,
  OctopusTariffResponse,
  Profile,
  Tariff,
} from '../lib/types'

/**
 * Per-request Octopus credential: an API key or an OAuth access token. The
 * proxy endpoints accept either; the value travels in a header for this one
 * request and is never persisted server-side.
 */
export type OctopusAuth = { kind: 'key'; value: string } | { kind: 'token'; value: string }

function octopusAuthHeader(auth: OctopusAuth): Record<string, string> {
  return auth.kind === 'token' ? { 'X-Octopus-Token': auth.value } : { 'X-Octopus-Key': auth.value }
}

const base = import.meta.env.VITE_API_URL ?? ''

const DEFAULT_TIMEOUT_MS = 10_000

interface RequestOptions extends RequestInit {
  /** Per-call timeout; the Octopus proxy can take far longer than 10s. */
  timeoutMs?: number
}

/** Thrown for non-2xx responses; carries the server's error envelope code. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function request<T>(path: string, init?: RequestOptions): Promise<T> {
  const timeout = AbortSignal.timeout(init?.timeoutMs ?? DEFAULT_TIMEOUT_MS)
  const signal = init?.signal ? AbortSignal.any([timeout, init.signal]) : timeout
  const res = await fetch(`${base}${path}`, { ...init, signal })
  if (!res.ok) {
    // Prefer the standard error envelope so the UI can show the real reason.
    let code = 'http_error'
    let message = `HTTP ${res.status}: ${path}`
    try {
      const body = (await res.json()) as { error?: { code?: string; message?: string } }
      if (body.error?.message) {
        code = body.error.code ?? code
        message = body.error.message
      }
    } catch {
      // Non-JSON error body; keep the generic message.
    }
    throw new ApiError(res.status, code, message)
  }
  const ct = res.headers.get('content-type')
  if (!ct?.includes('application/json')) {
    throw new Error(`Expected JSON from ${path}, got: ${ct ?? 'no content-type'}`)
  }
  return res.json() as Promise<T>
}

function postJson<T>(path: string, body: unknown, init?: RequestOptions): Promise<T> {
  return request<T>(path, {
    ...init,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    body: JSON.stringify(body),
  })
}

/**
 * Cost a pre-aggregated load profile (the CSV path). Note the signature:
 * only a Profile can cross this boundary — there is deliberately no API that
 * accepts raw half-hourly rows.
 */
export function postCost(profile: Profile, tariffs: Tariff[]): Promise<CostResponse> {
  return postJson<CostResponse>('/api/v1/cost', { profile, tariffs })
}

const OCTOPUS_TIMEOUT_MS = 95_000

/**
 * Fetch-aggregate-cost via the backend Octopus proxy. The API key travels in
 * the X-Octopus-Key header for this one request and is never persisted by
 * the server.
 */
export function postOctopusCost(
  req: OctopusCostRequest,
  auth: OctopusAuth,
): Promise<OctopusCostResponse> {
  return postJson<OctopusCostResponse>('/api/v1/octopus/cost', req, {
    headers: octopusAuthHeader(auth),
    timeoutMs: OCTOPUS_TIMEOUT_MS,
  })
}

/**
 * Fetch the account's current tariff (import, export, gas) as a prefilled
 * tariff in the app's model, built from the rates Octopus publishes for the
 * account's live agreements.
 */
export function postOctopusTariff(
  account: string,
  auth: OctopusAuth,
): Promise<OctopusTariffResponse> {
  return postJson<OctopusTariffResponse>(
    '/api/v1/octopus/tariff',
    { account },
    {
      headers: octopusAuthHeader(auth),
      timeoutMs: OCTOPUS_TIMEOUT_MS,
    },
  )
}

const AGILE_RATES_TIMEOUT_MS = 65_000

/**
 * Fetch the published historical price series for a dynamic (Agile-style)
 * product. Public data relayed by the backend — no credential involved.
 */
export function getAgileRates(params: {
  product: string
  region: string
  kind: 'unit' | 'standing'
  periodFrom: string
  periodTo: string
}): Promise<{ results: RateSlot[] }> {
  const query = new URLSearchParams({
    product: params.product,
    region: params.region,
    kind: params.kind,
    period_from: params.periodFrom,
    period_to: params.periodTo,
  })
  return request<{ results: RateSlot[] }>(`/api/v1/agile/rates?${query.toString()}`, {
    timeoutMs: AGILE_RATES_TIMEOUT_MS,
  })
}

/** Whether the Octopus OAuth connect flow is configured server-side. */
export function getOAuthConfig(): Promise<OAuthConfig> {
  return request<OAuthConfig>('/api/v1/oauth/config')
}

/** Exchange a PKCE authorization code for tokens via the backend relay. */
export function postOAuthToken(body: {
  grant_type: 'authorization_code' | 'refresh_token'
  code?: string
  code_verifier?: string
  redirect_uri?: string
  refresh_token?: string
}): Promise<TokenResponse> {
  return postJson<TokenResponse>('/api/v1/oauth/token', body)
}

export interface HealthResponse {
  status: string
}

export function checkHealth(): Promise<HealthResponse> {
  return request<HealthResponse>('/api/v1/health')
}

export interface StatusResponse {
  status: string
  git_sha: string
  build_time: string
}

export function getStatus(): Promise<StatusResponse> {
  return request<StatusResponse>('/api/v1/status')
}
