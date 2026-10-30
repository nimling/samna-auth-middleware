import { describe, it, expect, vi } from 'vitest'
import { appCostUrl, getAppCost } from './client'
import { ErrForbidden, ErrNotFound } from './types'

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

const buckets = [
  { module: 'bookable', period_start: '2026-10-30T00:00:00Z', unit: 'request', currency: 'NOK', quantity: 30, amount: 3.5, hours: 2 },
  { module: 'uptime', period_start: '2026-10-30T00:00:00Z', unit: 'check', currency: 'NOK', quantity: 60, amount: 0.06, hours: 1 }
]

describe('appCostUrl', () => {
  it('keeps a relative route of the service and adds the query', () => {
    expect(appCostUrl('/api/cost', { module: 'uptime', granularity: 'day' })).toBe('/api/cost?module=uptime&granularity=day')
  })

  it('keeps an absolute route and adds the window', () => {
    expect(appCostUrl('https://book.example.com/api/cost', { from: '2026-10-01T00:00:00Z', to: '2026-11-01T00:00:00Z' })).toBe(
      'https://book.example.com/api/cost?from=2026-10-01T00%3A00%3A00Z&to=2026-11-01T00%3A00%3A00Z'
    )
  })

  it('adds nothing without a query', () => {
    expect(appCostUrl('/api/cost')).toBe('/api/cost')
  })
})

describe('getAppCost', () => {
  it('fetches the buckets with the session and unwraps the envelope', async () => {
    const calls = stubFetch(200, { data: buckets, count: 2 })
    const got = await getAppCost('/api/cost', { granularity: 'day' })
    expect(calls).toHaveLength(1)
    expect(String(calls[0].url)).toBe('/api/cost?granularity=day')
    expect(calls[0].init?.credentials).toBe('include')
    expect(got).toHaveLength(2)
    expect(got[0].module).toBe('bookable')
    expect(got[0].hours).toBe(2)
  })

  it('answers an empty list when the envelope carries no data', async () => {
    stubFetch(200, { data: null, count: 0 })
    await expect(getAppCost('/api/cost')).resolves.toEqual([])
  })

  it('throws ErrForbidden for a reader without read', async () => {
    stubFetch(403, { error: { message: 'forbidden' } })
    await expect(getAppCost('/api/cost')).rejects.toBe(ErrForbidden)
  })

  it('throws ErrNotFound when the service has no cost route', async () => {
    stubFetch(404, { error: { message: 'not found' } })
    await expect(getAppCost('/api/cost')).rejects.toBe(ErrNotFound)
  })

  it('throws on a server error', async () => {
    stubFetch(500, { error: { message: 'cost query failed' } })
    await expect(getAppCost('/api/cost')).rejects.toThrow('cost returned status 500')
  })
})
