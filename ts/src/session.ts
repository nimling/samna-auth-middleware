import { fetchOIDCConfiguration, getJWKS } from './oidc'
import { validateAccessToken } from './validate'
import { readAccessTokenFromDocument } from './cookies'
import { DEFAULT_COOKIE_NAMES } from './types'
import type { EdgeConfig, SessionUser } from './types'

let oidcCache: { issuer: string; jwksUri: string; authorizationEndpoint?: string } | null = null

async function getOIDC(config: EdgeConfig) {
  if (oidcCache) return oidcCache
  const oidc = await fetchOIDCConfiguration(config.endpoint, config.clientSlug)
  oidcCache = {
    issuer: oidc.issuer,
    jwksUri: oidc.jwks_uri,
    authorizationEndpoint: oidc.authorization_endpoint
  }
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

export function getEdgeConfig(): EdgeConfig {
  return edgeConfig
}

export function getAppSlug(): string {
  return edgeConfig.clientSlug
}

export function getAuthEndpoint(): string {
  return edgeConfig.endpoint.replace(/\/$/, '')
}

export function getLoginUrl(provider?: string, returnTo?: string): string {
  const params = new URLSearchParams()
  params.set('app', edgeConfig.clientSlug)
  if (provider) params.set('provider', provider)
  if (returnTo) params.set('next', returnTo)
  return `${getAuthEndpoint()}/api/auth/login?${params.toString()}`
}

export function getLogoutUrl(returnTo?: string): string {
  const params = new URLSearchParams()
  params.set('app', edgeConfig.clientSlug)
  if (returnTo) params.set('next', returnTo)
  return `${getAuthEndpoint()}/api/auth/logout?${params.toString()}`
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
  window.location.href = getLoginUrl(provider, returnTo)
}

export async function endSession(returnTo?: string): Promise<void> {
  if (!edgeConfig.endpoint) return
  try {
    await fetch(getLogoutUrl(returnTo), {
      method: 'POST',
      credentials: 'include'
    })
  } catch {}
}
