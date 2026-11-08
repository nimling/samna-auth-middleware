// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest'
import { nimpressAuth, scopePages } from './nimpress'
import { PROPHET_CLAIM_PATH, PROPHET_ME_PATH, getEdgeMode } from './session'

type Route = { status: number; body: unknown }

function stubRoutes(routes: Record<string, Route>): string[] {
  const urls: string[] = []
  globalThis.fetch = vi.fn(async (url: unknown) => {
    const key = String(url)
    urls.push(key)
    const route = routes[key] ?? { status: 404, body: {} }
    return new Response(JSON.stringify(route.body), {
      status: route.status,
      headers: { 'Content-Type': 'application/json' }
    })
  }) as unknown as typeof fetch
  return urls
}

const endpoint = 'https://auth.example.io'

describe('scopePages', () => {
  it('collects the page keys and page names of every binding', () => {
    expect(
      scopePages([
        { id: 'c1', name: 'a', access: 4, claim_objects: [{ scope: { page: { test: { _access: 'READ' }, beta: {} } } }] },
        { id: 'c2', name: 'b', access: 4, claim_objects: [{ scope: { page: 'guide' } }, {}] }
      ])
    ).toEqual(['test', 'beta', 'guide'])
  })
})

describe('nimpressAuth', () => {
  it('configures the static mode by default', () => {
    nimpressAuth({ endpoint, appSlug: 'docs' })
    expect(getEdgeMode()).toBe('static')
  })

  it('resolves the viewer and opens the pages its claims carry', async () => {
    const auth = nimpressAuth({ endpoint, appSlug: 'docs' })
    stubRoutes({
      [`${endpoint}${PROPHET_ME_PATH}`]: { status: 200, body: { data: { user_id: 'u1', first_name: 'Ada', email: 'ada@example.io', type: 'user' } } },
      [`${endpoint}${PROPHET_CLAIM_PATH}`]: {
        status: 200,
        body: { data: [{ id: 'c1', name: 'Test', access: 4, claim_objects: [{ scope: { page: { test: { _access: 'READ' } } } }] }] }
      }
    })

    const viewer = await auth.resolveViewer(undefined)

    expect(viewer?.authenticated).toBe(true)
    expect(viewer?.id).toBe('u1')
    expect(viewer?.firstName).toBe('Ada')
    expect(auth.checkAccess({ gate: 'test' }, viewer!)).toBe(true)
    expect(auth.checkAccess({ gate: 'other' }, viewer!)).toBe(false)
    expect(auth.checkAccess({}, viewer!)).toBe(true)
  })

  it('answers no viewer and closes every gate when the prophet refuses', async () => {
    const auth = nimpressAuth({ endpoint, appSlug: 'docs' })
    stubRoutes({})

    expect(await auth.resolveViewer(undefined)).toBeNull()
    expect(auth.checkAccess({ gate: 'test' }, { authenticated: false })).toBe(false)
  })

  it('closes the gates when the session ends', async () => {
    const auth = nimpressAuth({ endpoint, appSlug: 'docs' })
    stubRoutes({
      [`${endpoint}${PROPHET_ME_PATH}`]: { status: 200, body: { data: { user_id: 'u1' } } },
      [`${endpoint}${PROPHET_CLAIM_PATH}`]: { status: 200, body: { data: [{ id: 'c1', name: 'Test', access: 4, claim_objects: [{ scope: { page: { test: {} } } }] }] } }
    })
    const viewer = await auth.resolveViewer(undefined)

    await auth.endSession(undefined, '/')

    expect(auth.checkAccess({ gate: 'test' }, viewer!)).toBe(false)
  })
})
