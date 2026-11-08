import { CSRF_TOKEN_COOKIE, CSRF_TOKEN_HEADER, ErrUnauthorized } from './types'
import { parseCookies } from './cookies'
import { handleUnauthenticated } from './session'

function csrfHeader(method: string): Record<string, string> {
  const upper = method.toUpperCase()
  if (upper === 'GET' || upper === 'HEAD' || upper === 'OPTIONS') return {}
  if (typeof document === 'undefined') return {}
  const token = parseCookies(document.cookie)[CSRF_TOKEN_COOKIE]
  if (!token) return {}
  return { [CSRF_TOKEN_HEADER]: token }
}

export function createAuthFetch(baseUrl: string): typeof fetch {
  const base = baseUrl.replace(/\/$/, '')
  return async (input, init) => {
    const url = typeof input === 'string'
      ? (input.startsWith('http') ? input : `${base}/${input.replace(/^\//, '')}`)
      : input
    const res = await fetch(url, {
      ...init,
      credentials: 'include',
      headers: {
        Accept: 'application/json',
        ...csrfHeader(init?.method ?? 'GET'),
        ...((init?.headers as Record<string, string>) ?? {})
      }
    })
    if (res.status === 401) throw ErrUnauthorized
    return res
  }
}

export async function guardedFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const res = await fetch(input, {
    ...init,
    credentials: 'include',
    headers: {
      ...csrfHeader(init?.method ?? 'GET'),
      ...((init?.headers as Record<string, string>) ?? {})
    }
  })
  if (res.status === 401) {
    const returnTo = typeof window !== 'undefined' ? window.location.pathname + window.location.search : undefined
    handleUnauthenticated(returnTo)
    throw ErrUnauthorized
  }
  return res
}
