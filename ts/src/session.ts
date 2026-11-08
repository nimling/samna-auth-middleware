import { getJWKS, getOpenIDConfiguration, getLogin } from './client'
import { validateToken } from './token'
import { readAccessTokenFromDocument } from './cookies'
import { ACCESS_TOKEN_COOKIE } from './types'
import type { ClaimUser, OpenIDConfiguration } from './types'

export type EdgeMode = 'edge' | 'bff'

export interface EdgeCallbacks {
  onSession?: (user: ClaimUser) => void | Promise<void>
  onLogin?: (url: string, returnTo?: string) => void
  onLogout?: (url: string, returnTo?: string) => void | Promise<void>
  onUnauthenticated?: (returnTo?: string) => void
}

export interface EdgeConfig {
  endpoint: string
  clientSlug: string
  mode?: EdgeMode
  bffPath?: string
  cookieNames?: {
    accessToken?: string
    refreshToken?: string
    csrfToken?: string
  }
  callbacks?: EdgeCallbacks
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

export function getEdgeMode(): EdgeMode {
  return edgeConfig.mode ?? 'edge'
}

export function getBffPath(): string {
  return (edgeConfig.bffPath ?? '/api/sauth').replace(/\/$/, '')
}

function withNext(base: string, returnTo?: string): string {
  if (!returnTo) return base
  const sep = base.includes('?') ? '&' : '?'
  return `${base}${sep}next=${encodeURIComponent(returnTo)}`
}

async function loadOIDC(): Promise<OpenIDConfiguration> {
  if (oidcCache) return oidcCache
  const url = `${getAuthEndpoint()}/api/app/${edgeConfig.clientSlug}/.well-known/openid-configuration`
  oidcCache = await getOpenIDConfiguration(url)
  return oidcCache
}

async function readSessionFromBff(): Promise<ClaimUser | null> {
  try {
    const res = await fetch(`${getBffPath()}/me`, { credentials: 'include' })
    if (!res.ok) return null
    const body = (await res.json()) as { data?: ClaimUser }
    return body.data ?? null
  } catch {
    return null
  }
}

export type ViewerClaim = {
  id: string
  name: string
  access: number
  objects?: Array<{ scope?: Record<string, unknown> }>
}

export async function readClaimsFromBff(): Promise<ViewerClaim[]> {
  try {
    const res = await fetch(`${getBffPath()}/me/claim`, { credentials: 'include' })
    if (!res.ok) return []
    const body = (await res.json()) as { data?: ViewerClaim[] }
    return body.data ?? []
  } catch {
    return []
  }
}

async function readSessionFromCookie(): Promise<ClaimUser | null> {
  if (!edgeConfig.endpoint || !edgeConfig.clientSlug) return null
  const cookieName = edgeConfig.cookieNames?.accessToken ?? ACCESS_TOKEN_COOKIE
  const token = readAccessTokenFromDocument(cookieName)
  if (!token) return null
  try {
    const oidc = await loadOIDC()
    const jwks = await getJWKS(oidc.jwks_uri)
    return await validateToken(token, oidc.issuer, edgeConfig.clientSlug, jwks)
  } catch {
    return null
  }
}

export async function readSessionFromDocument(): Promise<ClaimUser | null> {
  const user = getEdgeMode() === 'bff' ? await readSessionFromBff() : await readSessionFromCookie()
  if (user && edgeConfig.callbacks?.onSession) {
    await edgeConfig.callbacks.onSession(user)
  }
  return user
}

export function getLoginUrl(provider?: string, returnTo?: string): string {
  if (getEdgeMode() === 'bff') {
    return withNext(`${getBffPath()}/login`, returnTo)
  }
  const base = getLogin(getAuthEndpoint(), edgeConfig.clientSlug, provider)
  if (!returnTo) return base
  const url = new URL(base)
  url.searchParams.set('next', returnTo)
  return url.toString()
}

export function getLogoutUrl(returnTo?: string): string {
  if (getEdgeMode() === 'bff') {
    return withNext(`${getBffPath()}/logout`, returnTo)
  }
  const url = new URL(`${getAuthEndpoint()}/api/auth/logout`)
  url.searchParams.set('client_slug', edgeConfig.clientSlug)
  if (returnTo) url.searchParams.set('next', returnTo)
  return url.toString()
}

export function startLogin(provider?: string, returnTo?: string): void {
  if (getEdgeMode() === 'edge' && !edgeConfig.endpoint) return
  const url = getLoginUrl(provider, returnTo)
  if (edgeConfig.callbacks?.onLogin) {
    edgeConfig.callbacks.onLogin(url, returnTo)
    return
  }
  window.location.href = url
}

export function handleUnauthenticated(returnTo?: string): void {
  if (edgeConfig.callbacks?.onUnauthenticated) {
    edgeConfig.callbacks.onUnauthenticated(returnTo)
    return
  }
  startLogin(undefined, returnTo)
}

export async function endSession(returnTo?: string): Promise<void> {
  if (getEdgeMode() === 'edge' && !edgeConfig.endpoint) return
  const url = getLogoutUrl(returnTo)
  try {
    if (getEdgeMode() === 'bff') {
      await fetch(url, { credentials: 'include' })
    } else {
      await fetch(url, {
        method: 'POST',
        credentials: 'include'
      })
    }
  } catch {}
  if (edgeConfig.callbacks?.onLogout) {
    await edgeConfig.callbacks.onLogout(url, returnTo)
  }
}
