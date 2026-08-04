import { describe, it, expect, vi, afterEach } from 'vitest'
import { createAuthFetch } from './fetch'
import { CSRF_TOKEN_HEADER } from './types'

function stubFetch() {
  const calls: { url: unknown; init: RequestInit | undefined }[] = []
  globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
    calls.push({ url, init })
    return new Response('{}', { status: 200 })
  }) as unknown as typeof fetch
  return calls
}

afterEach(() => {
  delete (globalThis as { document?: unknown }).document
})

describe('createAuthFetch csrf header', () => {
  it('attaches the csrf header on writes when the cookie is present', async () => {
    ;(globalThis as { document?: unknown }).document = { cookie: 'sauth_csrf_token=tok123' }
    const calls = stubFetch()
    const authFetch = createAuthFetch('https://api.example.com')
    await authFetch('/thing', { method: 'POST' })
    const headers = calls[0].init?.headers as Record<string, string>
    expect(headers[CSRF_TOKEN_HEADER]).toBe('tok123')
  })

  it('does not attach the csrf header on GET', async () => {
    ;(globalThis as { document?: unknown }).document = { cookie: 'sauth_csrf_token=tok123' }
    const calls = stubFetch()
    const authFetch = createAuthFetch('https://api.example.com')
    await authFetch('/thing')
    const headers = calls[0].init?.headers as Record<string, string>
    expect(headers[CSRF_TOKEN_HEADER]).toBeUndefined()
  })

  it('omits the header when the cookie is absent', async () => {
    ;(globalThis as { document?: unknown }).document = { cookie: '' }
    const calls = stubFetch()
    const authFetch = createAuthFetch('https://api.example.com')
    await authFetch('/thing', { method: 'DELETE' })
    const headers = calls[0].init?.headers as Record<string, string>
    expect(headers[CSRF_TOKEN_HEADER]).toBeUndefined()
  })
})
