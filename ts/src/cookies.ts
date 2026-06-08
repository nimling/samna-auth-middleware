import { DEFAULT_COOKIE_NAMES } from './types'

export function parseCookies(header: string): Record<string, string> {
  const out: Record<string, string> = {}
  if (!header) return out
  for (const part of header.split(';')) {
    const trimmed = part.trim()
    if (!trimmed) continue
    const eq = trimmed.indexOf('=')
    if (eq < 0) continue
    const name = trimmed.slice(0, eq)
    const value = trimmed.slice(eq + 1)
    try {
      out[name] = decodeURIComponent(value)
    } catch {
      out[name] = value
    }
  }
  return out
}

export function readAccessTokenFromDocument(
  cookieName: string = DEFAULT_COOKIE_NAMES.accessToken
): string | null {
  if (typeof document === 'undefined') return null
  const cookies = parseCookies(document.cookie)
  return cookies[cookieName] ?? null
}
