import { ErrUnauthorized } from './types'

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
        ...((init?.headers as Record<string, string>) ?? {})
      }
    })
    if (res.status === 401) throw ErrUnauthorized
    return res
  }
}
