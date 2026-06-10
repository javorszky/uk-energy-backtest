import type {
  CostResponse,
  OctopusCostRequest,
  OctopusCostResponse,
  OctopusTariffResponse,
  Profile,
  Tariff,
} from '../lib/types'

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
  apiKey: string,
): Promise<OctopusCostResponse> {
  return postJson<OctopusCostResponse>('/api/v1/octopus/cost', req, {
    headers: { 'X-Octopus-Key': apiKey },
    timeoutMs: OCTOPUS_TIMEOUT_MS,
  })
}

/**
 * Fetch the account's current tariff (import, export, gas) as a prefilled
 * tariff in the app's model, built from the rates Octopus publishes for the
 * account's live agreements.
 */
export function postOctopusTariff(account: string, apiKey: string): Promise<OctopusTariffResponse> {
  return postJson<OctopusTariffResponse>(
    '/api/v1/octopus/tariff',
    { account },
    {
      headers: { 'X-Octopus-Key': apiKey },
      timeoutMs: OCTOPUS_TIMEOUT_MS,
    },
  )
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
