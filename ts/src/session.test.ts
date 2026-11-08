// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest'
import {
  CLAIM_OBJECT_WITH,
  EDGE_MODE_STATIC,
  PROPHET_CLAIM_PATH,
  PROPHET_LOGOUT_PATH,
  PROPHET_ME_PATH,
  configureEdge,
  endSession,
  getLoginUrl,
  readClaims,
  readSessionFromDocument
} from './session'

type Call = { url: unknown; init: RequestInit | undefined }

function stubFetch(status: number, body: unknown): Call[] {
  const calls: Call[] = []
  globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
    calls.push({ url, init })
    return new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' }
    })
  }) as unknown as typeof fetch
  return calls
}

describe('readClaims', () => {
  it('asks the bff for the bindings and reads their scope', async () => {
    configureEdge({ endpoint: '', clientSlug: 'docs', mode: 'bff' })
    const calls = stubFetch(200, {
      data: [
        {
          id: 'c1',
          name: 'Test page',
          access: 4,
          claim_objects: [{ scope: { page: { test: { _access: 'READ' } } } }]
        }
      ]
    })

    const claims = await readClaims()

    expect(calls[0].url).toBe(`/api/auth/me/claim?with=${CLAIM_OBJECT_WITH}`)
    expect(calls[0].init?.credentials).toBe('include')
    expect(claims[0].claim_objects?.[0].scope).toEqual({ page: { test: { _access: 'READ' } } })
  })

  it('answers an empty list when the bff refuses', async () => {
    configureEdge({ endpoint: '', clientSlug: 'docs', mode: 'bff' })
    stubFetch(401, { error: 'unauthorized' })

    expect(await readClaims()).toEqual([])
  })
})

describe('static mode', () => {
  it('reads the viewer from the prophet with credentials', async () => {
    configureEdge({ endpoint: 'https://auth.example.io/', clientSlug: 'docs', mode: EDGE_MODE_STATIC })
    const calls = stubFetch(200, {
      data: { user_id: 'u1', first_name: 'Ada', last_name: 'Lovelace', email: 'ada@example.io', type: 'user' }
    })

    const user = await readSessionFromDocument()

    expect(calls[0].url).toBe(`https://auth.example.io${PROPHET_ME_PATH}`)
    expect(calls[0].init?.credentials).toBe('include')
    expect(user).toEqual({
      id: 'u1',
      firstName: 'Ada',
      lastName: 'Lovelace',
      userName: undefined,
      phone: undefined,
      email: 'ada@example.io',
      type: 'user'
    })
  })

  it('answers no viewer when the prophet refuses', async () => {
    configureEdge({ endpoint: 'https://auth.example.io', clientSlug: 'docs', mode: EDGE_MODE_STATIC })
    stubFetch(401, {})

    expect(await readSessionFromDocument()).toBeNull()
  })

  it('reads the claims with their bindings from the prophet', async () => {
    configureEdge({ endpoint: 'https://auth.example.io', clientSlug: 'docs', mode: EDGE_MODE_STATIC })
    const calls = stubFetch(200, { data: [{ id: 'c1', name: 'Test page', access: 4, claim_objects: [{ scope: { page: { test: {} } } }] }] })

    const claims = await readClaims()

    expect(calls[0].url).toBe(`https://auth.example.io${PROPHET_CLAIM_PATH}`)
    expect(calls[0].init?.credentials).toBe('include')
    expect(claims[0].claim_objects?.[0].scope).toEqual({ page: { test: {} } })
  })

  it('sends the login to the prophet with the absolute page to return to', () => {
    configureEdge({ endpoint: 'https://auth.example.io', clientSlug: 'docs', mode: EDGE_MODE_STATIC })

    const url = new URL(getLoginUrl(undefined, '/test'))

    expect(url.origin + url.pathname).toBe('https://auth.example.io/api/auth/login')
    expect(url.searchParams.get('client_slug')).toBe('docs')
    expect(url.searchParams.get('next')).toBe(`${window.location.origin}/test`)
  })

  it('sends a child app login to the bff with its slug and the page to return to', () => {
    configureEdge({ endpoint: '', clientSlug: 'bookable', mode: 'bff', bffPath: 'https://api.example.io/api/auth', app: 'bookable-web' })

    const url = new URL(getLoginUrl(undefined, '/bookings'))

    expect(url.origin + url.pathname).toBe('https://api.example.io/api/auth/login')
    expect(url.searchParams.get('app')).toBe('bookable-web')
    expect(url.searchParams.get('next')).toBe('/bookings')
  })

  it('ends the session on the prophet with a credentialed get', async () => {
    configureEdge({ endpoint: 'https://auth.example.io', clientSlug: 'docs', mode: EDGE_MODE_STATIC })
    const calls = stubFetch(200, {})

    await endSession('/')

    expect(String(calls[0].url).startsWith(`https://auth.example.io${PROPHET_LOGOUT_PATH}?`)).toBe(true)
    expect(calls[0].init?.method).toBeUndefined()
    expect(calls[0].init?.credentials).toBe('include')
  })
})
