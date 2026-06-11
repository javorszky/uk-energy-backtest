/**
 * PKCE helpers for the Octopus OAuth connect flow (public client — no
 * secret; the S256 challenge protects the code exchange). The verifier and
 * state live in sessionStorage only for the duration of the redirect
 * round-trip; the access token itself is held in component memory and never
 * persisted.
 */

export interface OAuthConfig {
  enabled: boolean
  client_id?: string
  authorize_url?: string
  scopes?: string[]
}

export interface TokenResponse {
  access_token: string
  token_type?: string
  expires_in?: number
  refresh_token?: string
  scope?: string
}

const VERIFIER_KEY = 'ukeb.oauth.verifier'
const STATE_KEY = 'ukeb.oauth.state'

export const OAUTH_CALLBACK_PATH = '/oauth/callback'

function randomUrlSafe(bytes: number): string {
  const buf = new Uint8Array(bytes)
  crypto.getRandomValues(buf)
  return base64Url(buf)
}

function base64Url(bytes: Uint8Array): string {
  let bin = ''
  for (const b of bytes) bin += String.fromCharCode(b)
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

/** SHA-256 → base64url, the S256 code challenge transform. */
export async function codeChallengeS256(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier))
  return base64Url(new Uint8Array(digest))
}

/** The redirect URI registered with Octopus: this origin + the callback path. */
export function redirectUri(): string {
  return `${window.location.origin}${OAUTH_CALLBACK_PATH}`
}

/**
 * Generate verifier + state, stash them for the callback, and return the
 * full authorize URL to navigate to.
 */
export async function buildAuthorizeRedirect(config: OAuthConfig): Promise<string> {
  if (!config.enabled || !config.client_id || !config.authorize_url) {
    throw new Error('OAuth is not enabled')
  }
  const verifier = randomUrlSafe(48)
  const state = randomUrlSafe(24)
  sessionStorage.setItem(VERIFIER_KEY, verifier)
  sessionStorage.setItem(STATE_KEY, state)

  const params = new URLSearchParams({
    response_type: 'code',
    client_id: config.client_id,
    redirect_uri: redirectUri(),
    scope: (config.scopes ?? []).join(' '),
    state,
    code_challenge: await codeChallengeS256(verifier),
    code_challenge_method: 'S256',
  })
  return `${config.authorize_url}?${params.toString()}`
}

export interface CallbackParams {
  code: string
  codeVerifier: string
}

/**
 * Validate the callback query against the stashed state and return what the
 * token exchange needs. Returns null when this is not a (valid) callback.
 * The stash is cleared either way — a verifier must never be usable twice.
 */
export function consumeCallback(query: URLSearchParams): CallbackParams | null {
  const verifier = sessionStorage.getItem(VERIFIER_KEY)
  const expectedState = sessionStorage.getItem(STATE_KEY)
  sessionStorage.removeItem(VERIFIER_KEY)
  sessionStorage.removeItem(STATE_KEY)

  const code = query.get('code')
  const state = query.get('state')
  if (!code || !verifier || !expectedState || state !== expectedState) {
    return null
  }
  return { code, codeVerifier: verifier }
}
