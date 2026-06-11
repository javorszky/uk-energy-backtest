import { beforeEach, describe, expect, it } from 'vitest'

import { buildAuthorizeRedirect, codeChallengeS256, consumeCallback } from '../oauth'

const config = {
  enabled: true,
  client_id: 'client-abc',
  authorize_url: 'https://auth.octopus.energy/authorize/',
  scopes: ['openid', 'view:detailed-usage'],
}

beforeEach(() => {
  sessionStorage.clear()
})

describe('codeChallengeS256', () => {
  it('matches the RFC 7636 appendix B vector', async () => {
    // Verifier and expected challenge from the PKCE spec.
    const challenge = await codeChallengeS256('dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk')
    expect(challenge).toBe('E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM')
  })
})

describe('buildAuthorizeRedirect', () => {
  it('builds a complete authorize URL and stashes verifier + state', async () => {
    const url = new URL(await buildAuthorizeRedirect(config))
    expect(url.origin + url.pathname).toBe('https://auth.octopus.energy/authorize/')
    expect(url.searchParams.get('response_type')).toBe('code')
    expect(url.searchParams.get('client_id')).toBe('client-abc')
    expect(url.searchParams.get('scope')).toBe('openid view:detailed-usage')
    expect(url.searchParams.get('code_challenge_method')).toBe('S256')
    expect(url.searchParams.get('code_challenge')).toBeTruthy()
    expect(url.searchParams.get('redirect_uri')).toContain('/oauth/callback')

    const state = url.searchParams.get('state')!
    expect(sessionStorage.getItem('ukeb.oauth.state')).toBe(state)
    expect(sessionStorage.getItem('ukeb.oauth.verifier')).toBeTruthy()
  })

  it('throws when oauth is disabled', async () => {
    await expect(buildAuthorizeRedirect({ enabled: false })).rejects.toThrow('not enabled')
  })
})

describe('consumeCallback', () => {
  it('returns code + verifier when state matches, and clears the stash', async () => {
    const url = new URL(await buildAuthorizeRedirect(config))
    const state = url.searchParams.get('state')!
    const verifier = sessionStorage.getItem('ukeb.oauth.verifier')!

    const result = consumeCallback(new URLSearchParams({ code: 'auth-code-1', state }))
    expect(result).toEqual({ code: 'auth-code-1', codeVerifier: verifier })
    expect(sessionStorage.getItem('ukeb.oauth.verifier')).toBeNull()
    expect(sessionStorage.getItem('ukeb.oauth.state')).toBeNull()
  })

  it('rejects a mismatched state and still clears the stash (no replay)', async () => {
    await buildAuthorizeRedirect(config)
    const result = consumeCallback(new URLSearchParams({ code: 'c', state: 'forged' }))
    expect(result).toBeNull()
    expect(sessionStorage.getItem('ukeb.oauth.verifier')).toBeNull()
  })

  it('returns null with no stashed verifier', () => {
    expect(consumeCallback(new URLSearchParams({ code: 'c', state: 's' }))).toBeNull()
  })
})
