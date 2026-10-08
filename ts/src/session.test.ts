import { describe, it, expect, vi } from 'vitest'
import { CLAIM_OBJECT_WITH, configureEdge, readClaimsFromBff } from './session'

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

describe('readClaimsFromBff', () => {
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

    const claims = await readClaimsFromBff()

    expect(calls[0].url).toBe(`/api/auth/me/claim?with=${CLAIM_OBJECT_WITH}`)
    expect(calls[0].init?.credentials).toBe('include')
    expect(claims[0].claim_objects?.[0].scope).toEqual({ page: { test: { _access: 'READ' } } })
  })

  it('answers an empty list when the bff refuses', async () => {
    configureEdge({ endpoint: '', clientSlug: 'docs', mode: 'bff' })
    stubFetch(401, { error: 'unauthorized' })

    expect(await readClaimsFromBff()).toEqual([])
  })
})
