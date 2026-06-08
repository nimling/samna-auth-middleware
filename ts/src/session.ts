import { fetchOIDCConfiguration, getJWKS } from './oidc'
import { validateAccessToken } from './validate'
import { readAccessTokenFromDocument } from './cookies'
import { DEFAULT_COOKIE_NAMES } from './types'
import type { EdgeConfig, SessionUser } from './types'

let oidcCache: { issuer: string; jwksUri: string } | null = null

async function getOIDC(config: EdgeConfig) {
  if (oidcCache) return oidcCache
  const oidc = await fetchOIDCConfiguration(config.endpoint, config.clientSlug)
  oidcCache = { issuer: oidc.issuer, jwksUri: oidc.jwks_uri }
  return oidcCache
}

export function configureEdge(config: EdgeConfig) {
  edgeConfig = config
  oidcCache = null
}

let edgeConfig: EdgeConfig = {
  endpoint: '',
  clientSlug: ''
}

export async function readSessionFromDocument(): Promise<SessionUser | null> {
  if (!edgeConfig.endpoint || !edgeConfig.clientSlug) return null
  const cookieName = edgeConfig.cookieNames?.accessToken ?? DEFAULT_COOKIE_NAMES.accessToken
  const token = readAccessTokenFromDocument(cookieName)
  if (!token) return null
  try {
    const { issuer, jwksUri } = await getOIDC(edgeConfig)
    const jwks = await getJWKS(jwksUri)
    return await validateAccessToken(token, issuer, jwks)
  } catch {
    return null
  }
}

export function startLogin(provider?: string, returnTo?: string) {
  if (!edgeConfig.endpoint) return
  const params = new URLSearchParams()
  if (provider) params.set('provider', provider)
  if (returnTo) params.set('next', returnTo)
  const qs = params.toString()
  window.location.href = `${edgeConfig.endpoint.replace(/\/$/, '')}/api/auth/login${qs ? '?' + qs : ''}`
}

export async function endSession(): Promise<void> {
  if (!edgeConfig.endpoint) return
  try {
    await fetch(`${edgeConfig.endpoint.replace(/\/$/, '')}/api/auth/logout`, {
      method: 'POST',
      credentials: 'include'
    })
  } catch {}
}
