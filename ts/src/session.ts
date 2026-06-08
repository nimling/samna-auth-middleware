import { getJWKS, getOpenIDConfiguration, getLogin } from './client'
import { validateToken } from './token'
import { readAccessTokenFromDocument } from './cookies'
import { ACCESS_TOKEN_COOKIE } from './types'
import type { ClaimUser, OpenIDConfiguration } from './types'

export interface EdgeConfig {
  endpoint: string
  clientSlug: string
  cookieNames?: {
    accessToken?: string
    refreshToken?: string
    csrfToken?: string
  }
}

let edgeConfig: EdgeConfig = { endpoint: '', clientSlug: '' }
let oidcCache: OpenIDConfiguration | null = null

export function configureEdge(config: EdgeConfig): void {
  edgeConfig = config
  oidcCache = null
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

async function loadOIDC(): Promise<OpenIDConfiguration> {
  if (oidcCache) return oidcCache
  const url = `${getAuthEndpoint()}/api/app/${edgeConfig.clientSlug}/.well-known/openid-configuration`
  oidcCache = await getOpenIDConfiguration(url)
  return oidcCache
}

export async function readSessionFromDocument(): Promise<ClaimUser | null> {
  if (!edgeConfig.endpoint || !edgeConfig.clientSlug) return null
  const cookieName = edgeConfig.cookieNames?.accessToken ?? ACCESS_TOKEN_COOKIE
  const token = readAccessTokenFromDocument(cookieName)
  if (!token) return null
  try {
    const oidc = await loadOIDC()
    const jwks = await getJWKS(oidc.jwks_uri)
    return await validateToken(token, oidc.issuer, jwks)
  } catch {
    return null
  }
}

export function getLoginUrl(provider?: string, returnTo?: string): string {
  const base = getLogin(getAuthEndpoint(), edgeConfig.clientSlug, provider)
  if (!returnTo) return base
  const url = new URL(base)
  url.searchParams.set('next', returnTo)
  return url.toString()
}

export function getLogoutUrl(returnTo?: string): string {
  const url = new URL(`${getAuthEndpoint()}/api/auth/logout`)
  url.searchParams.set('client_slug', edgeConfig.clientSlug)
  if (returnTo) url.searchParams.set('next', returnTo)
  return url.toString()
}

export function startLogin(provider?: string, returnTo?: string): void {
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
