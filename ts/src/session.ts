import { getJWKS, getOpenIDConfiguration, getLogin } from './client'
import { validateToken } from './token'
import { readAccessTokenFromDocument } from './cookies'
import { ACCESS_TOKEN_COOKIE } from './types'
import type { ClaimUser, OpenIDConfiguration, UserType } from './types'

export type EdgeMode = 'edge' | 'bff' | 'static'

export const EDGE_MODE_EDGE: EdgeMode = 'edge'
export const EDGE_MODE_BFF: EdgeMode = 'bff'
export const EDGE_MODE_STATIC: EdgeMode = 'static'

export const PROPHET_ME_PATH = '/api/me/user'
export const PROPHET_CLAIM_PATH = '/api/me/claim'
export const PROPHET_LOGOUT_PATH = '/api/auth/logout'

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
  initiate?: string
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
  return edgeConfig.mode ?? EDGE_MODE_EDGE
}

export function getBffPath(): string {
  return (edgeConfig.bffPath ?? '/api/auth').replace(/\/$/, '')
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

type ProphetUser = {
  user_id: string
  first_name?: string
  last_name?: string
  user_name?: string
  phone?: string
  email?: string
  type?: UserType
}

async function readSessionFromProphet(): Promise<ClaimUser | null> {
  if (!edgeConfig.endpoint) return null
  try {
    const res = await fetch(`${getAuthEndpoint()}${PROPHET_ME_PATH}`, { credentials: 'include' })
    if (!res.ok) return null
    const body = (await res.json()) as { data?: ProphetUser }
    if (!body.data) return null
    return {
      id: body.data.user_id,
      firstName: body.data.first_name,
      lastName: body.data.last_name,
      userName: body.data.user_name,
      phone: body.data.phone,
      email: body.data.email,
      type: body.data.type
    }
  } catch {
    return null
  }
}

export type ViewerClaim = {
  id: string
  name: string
  access: number
  level?: number
  claim_objects?: Array<{ scope?: Record<string, unknown>; sa_access?: number | null }>
}

export const CLAIM_OBJECT_WITH = 'claim_object'

function claimsUrl(): string {
  if (getEdgeMode() === EDGE_MODE_BFF) return `${getBffPath()}/me/claim?with=${CLAIM_OBJECT_WITH}`
  return `${getAuthEndpoint()}${PROPHET_CLAIM_PATH}`
}

export async function readClaims(): Promise<ViewerClaim[]> {
  try {
    const res = await fetch(claimsUrl(), { credentials: 'include' })
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

async function readSession(): Promise<ClaimUser | null> {
  switch (getEdgeMode()) {
    case EDGE_MODE_BFF:
      return readSessionFromBff()
    case EDGE_MODE_STATIC:
      return readSessionFromProphet()
    default:
      return readSessionFromCookie()
  }
}

export async function readSessionFromDocument(): Promise<ClaimUser | null> {
  const user = await readSession()
  if (user && edgeConfig.callbacks?.onSession) {
    await edgeConfig.callbacks.onSession(user)
  }
  return user
}

export function getLoginUrl(provider?: string, returnTo?: string): string {
  if (getEdgeMode() === EDGE_MODE_BFF) {
    const base = edgeConfig.initiate ? `${getBffPath()}/${encodeURIComponent(edgeConfig.initiate)}/login` : `${getBffPath()}/login`
    return withNext(base, returnTo ?? window.location.href)
  }
  const base = getLogin(getAuthEndpoint(), edgeConfig.clientSlug, provider)
  if (!returnTo) return base
  const url = new URL(base)
  url.searchParams.set('next', getEdgeMode() === EDGE_MODE_STATIC ? new URL(returnTo, window.location.origin).toString() : returnTo)
  return url.toString()
}

export function getLogoutUrl(returnTo?: string): string {
  if (getEdgeMode() === EDGE_MODE_BFF) {
    if (edgeConfig.initiate) return `${getBffPath()}/${encodeURIComponent(edgeConfig.initiate)}/logout`
    return withNext(`${getBffPath()}/logout`, returnTo)
  }
  const url = new URL(`${getAuthEndpoint()}${PROPHET_LOGOUT_PATH}`)
  url.searchParams.set('client_slug', edgeConfig.clientSlug)
  if (returnTo) url.searchParams.set('next', returnTo)
  return url.toString()
}

export function startLogin(provider?: string, returnTo?: string): void {
  if (getEdgeMode() !== EDGE_MODE_BFF && !edgeConfig.endpoint) return
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
  if (getEdgeMode() !== EDGE_MODE_BFF && !edgeConfig.endpoint) return
  const url = getLogoutUrl(returnTo)
  try {
    if (getEdgeMode() === EDGE_MODE_EDGE) {
      await fetch(url, {
        method: 'POST',
        credentials: 'include'
      })
    } else {
      await fetch(url, { credentials: 'include' })
    }
  } catch {}
  if (edgeConfig.callbacks?.onLogout) {
    await edgeConfig.callbacks.onLogout(url, returnTo)
  }
}
