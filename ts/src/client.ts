import { ErrForbidden, ErrNotFound, ErrUnauthorized } from './types'
import type { AppCostBucket, AppCostQuery, OpenIDConfiguration, ResponseToken, JWKSLike } from './types'

function joinURL(base: string, path: string): string {
  const cleanBase = base.replace(/\/$/, '')
  const cleanPath = path.replace(/^\//, '')
  return `${cleanBase}/${cleanPath}`
}

let jwksCache = new Map<string, { keys: JWKSLike; fetchedAt: number }>()
const JWKS_TTL_MS = 10 * 60 * 1000

export async function getJWKS(jwksUri: string): Promise<JWKSLike> {
  const cached = jwksCache.get(jwksUri)
  if (cached && Date.now() - cached.fetchedAt < JWKS_TTL_MS) return cached.keys
  const res = await fetch(jwksUri, { credentials: 'omit' })
  if (!res.ok) throw new Error(`JWKS fetch failed: ${res.status}`)
  const keys = (await res.json()) as JWKSLike
  jwksCache.set(jwksUri, { keys, fetchedAt: Date.now() })
  return keys
}

export function clearJWKSCache(): void {
  jwksCache.clear()
}

export async function getOpenIDConfiguration(
  configURL: string
): Promise<OpenIDConfiguration> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 3000)
  let resp: Response
  try {
    resp = await fetch(configURL, { method: 'GET', signal: controller.signal })
  } finally {
    clearTimeout(timer)
  }
  if (resp.status === 404) throw ErrNotFound
  if (!resp.ok) {
    throw new Error(
      `openid configuration request to ${configURL} returned status ${resp.status}, check that the client slug exists`
    )
  }
  const contentType = resp.headers.get('Content-Type') ?? ''
  if (!contentType.includes('application/json')) {
    const body = await resp.text()
    throw new Error(
      `openid configuration at ${configURL} returned ${contentType} instead of JSON: ${body.slice(0, 200)}`
    )
  }
  return (await resp.json()) as OpenIDConfiguration
}

export function getLogin(
  endpoint: string,
  clientSlug: string,
  provider?: string
): string {
  const path = provider && provider.length > 0
    ? `api/auth/p/${provider}/login`
    : 'api/auth/login'
  const url = new URL(joinURL(endpoint, path))
  url.searchParams.set('client_slug', clientSlug)
  return url.toString()
}

export async function getLogout(
  endpoint: string,
  clientSlug: string,
  clientSecret: string
): Promise<number> {
  const url = joinURL(endpoint, 'sam/logout')
  const resp = await fetch(url, {
    method: 'GET',
    headers: {
      'SAuth-App-Slug': clientSlug,
      'SAuth-App-Secret': clientSecret
    }
  })
  if (!resp.ok) {
    if (resp.status === 401) throw ErrUnauthorized
    throw new Error(`logout returned status ${resp.status}`)
  }
  return 200
}

async function postToken(
  tokenURL: string,
  slug: string,
  secret: string,
  form: URLSearchParams
): Promise<ResponseToken> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 3000)
  let resp: Response
  try {
    resp = await fetch(tokenURL, {
      method: 'POST',
      body: form.toString(),
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded',
        Accept: 'application/json',
        'SAuth-App-Slug': slug,
        'SAuth-App-Secret': secret
      },
      signal: controller.signal
    })
  } finally {
    clearTimeout(timer)
  }
  if (!resp.ok) {
    if (resp.status === 401) throw ErrUnauthorized
    throw new Error(`auth server returned status ${resp.status}`)
  }
  return (await resp.json()) as ResponseToken
}

export async function postAuthCode(
  tokenURL: string,
  slug: string,
  secret: string,
  code: string,
  redirectURI: string
): Promise<ResponseToken> {
  const form = new URLSearchParams()
  form.set('grant_type', 'authorization_code')
  form.set('code', code)
  form.set('redirect_uri', redirectURI)
  return postToken(tokenURL, slug, secret, form)
}

export async function postRefreshToken(
  tokenURL: string,
  slug: string,
  secret: string,
  refreshToken: string
): Promise<ResponseToken> {
  const form = new URLSearchParams()
  form.set('grant_type', 'refresh_token')
  form.set('refresh_token', refreshToken)
  return postToken(tokenURL, slug, secret, form)
}

export function appCostUrl(url: string, query?: AppCostQuery): string {
  const target = new URL(url, typeof location === 'undefined' ? 'http://localhost' : location.origin)
  if (query?.module) target.searchParams.set('module', query.module)
  if (query?.from) target.searchParams.set('from', query.from)
  if (query?.to) target.searchParams.set('to', query.to)
  if (query?.granularity) target.searchParams.set('granularity', query.granularity)
  return url.startsWith('/') ? `${target.pathname}${target.search}` : target.toString()
}

export async function getAppCost(url: string, query?: AppCostQuery): Promise<AppCostBucket[]> {
  const res = await fetch(appCostUrl(url, query), { credentials: 'include', headers: { Accept: 'application/json' } })
  if (res.status === 401) throw ErrUnauthorized
  if (res.status === 403) throw ErrForbidden
  if (res.status === 404) throw ErrNotFound
  if (!res.ok) throw new Error(`cost returned status ${res.status}`)
  const body = (await res.json()) as { data?: AppCostBucket[] }
  return body.data ?? []
}
