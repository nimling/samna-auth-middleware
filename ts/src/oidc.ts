import type { OIDCConfiguration } from './types'

export async function fetchOIDCConfiguration(
  endpoint: string,
  clientSlug: string
): Promise<OIDCConfiguration> {
  const url = `${stripSlash(endpoint)}/api/app/${clientSlug}/.well-known/openid-configuration`
  const res = await fetch(url, { credentials: 'omit' })
  if (!res.ok) {
    throw new Error(`OIDC discovery failed: ${res.status} ${res.statusText}`)
  }
  return (await res.json()) as OIDCConfiguration
}

let jwksCache = new Map<string, { keys: unknown; fetchedAt: number }>()
const JWKS_TTL_MS = 10 * 60 * 1000

export async function getJWKS(jwksUri: string): Promise<{ keys: unknown[] }> {
  const cached = jwksCache.get(jwksUri)
  if (cached && Date.now() - cached.fetchedAt < JWKS_TTL_MS) {
    return cached.keys as { keys: unknown[] }
  }
  const res = await fetch(jwksUri, { credentials: 'omit' })
  if (!res.ok) {
    throw new Error(`JWKS fetch failed: ${res.status} ${res.statusText}`)
  }
  const keys = await res.json()
  jwksCache.set(jwksUri, { keys, fetchedAt: Date.now() })
  return keys
}

export function clearJWKSCache() {
  jwksCache.clear()
}

function stripSlash(url: string): string {
  return url.replace(/\/$/, '')
}
