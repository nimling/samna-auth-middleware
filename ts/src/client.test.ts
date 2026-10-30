import { describe, it, expect, vi } from 'vitest'
import { appCostUrl, getAppCost } from './client'
import { COST_SCHEMA, COST_SCHEMA_ID, ErrForbidden, ErrNotFound } from './types'
import type { AppCostBucket, AppCostInput, CostEvidence } from './types'

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

const buckets: AppCostBucket[] = [
  { module: 'bookable', kind: 'usage', schema_id: COST_SCHEMA_ID, group_key: { 'subject.id': '10000000-0000-4000-8000-000000000001' }, period_start: '2026-10-30T00:00:00Z', unit: 'request', currency: 'NOK', quantity: 30, amount: 3.5, hours: 2, rows: 2 },
  { module: 'uptime', kind: 'fee', schema_id: null, group_key: null, period_start: '2026-10-30T00:00:00Z', unit: 'event', currency: 'NOK', quantity: 1, amount: 0.06, hours: 0, rows: 1 }
]

describe('appCostUrl', () => {
  it('keeps a relative route of the service and adds the query', () => {
    expect(appCostUrl('/api/cost', { module: 'uptime', granularity: 'month' })).toBe('/api/cost?module=uptime&granularity=month')
  })

  it('keeps an absolute route and adds the window', () => {
    expect(appCostUrl('https://book.example.com/api/cost', { from: '2026-10-01T00:00:00Z', to: '2026-11-01T00:00:00Z' })).toBe(
      'https://book.example.com/api/cost?from=2026-10-01T00%3A00%3A00Z&to=2026-11-01T00%3A00%3A00Z'
    )
  })

  it('adds the status and the subject filters', () => {
    expect(appCostUrl('/api/cost', { status: 'invoiced', subject_type: 'claimius.organization', subject_id: '10000000-0000-4000-8000-000000000001' })).toBe(
      '/api/cost?status=invoiced&subject_type=claimius.organization&subject_id=10000000-0000-4000-8000-000000000001'
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
    expect(got[0].kind).toBe('usage')
    expect(got[0].schema_id).toBe(COST_SCHEMA_ID)
    expect(got[0].group_key).toEqual({ 'subject.id': '10000000-0000-4000-8000-000000000001' })
    expect(got[0].rows).toBe(2)
    expect(got[1].group_key).toBeNull()
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

describe('COST_SCHEMA', () => {
  it('names itself and accumulates per subject out of the box', () => {
    expect(COST_SCHEMA.$id).toBe(COST_SCHEMA_ID)
    expect(COST_SCHEMA['x-samna']?.accumulate).toEqual({ method: 'sum', proration: 'overlap', group: ['subject.id'] })
    const properties = COST_SCHEMA.properties as Record<string, unknown>
    expect(Object.keys(properties)).toEqual(['subject', 'source', 'items', 'pricing', 'notes'])
  })

  it('types the three input forms and the evidence', () => {
    const row: AppCostInput = { module: 'bookable', kind: 'usage', period_start: '2026-10-30T09:00:00Z', period_end: '2026-10-30T10:00:00Z', quantity: 10, unit: 'request', unit_price: 0.15, currency: 'NOK', ref: '2026-10-30T09' }
    const items: AppCostInput = { module: 'bookable', period_start: '2026-10-30T09:00:00Z', period_end: '2026-10-30T10:00:00Z', currency: 'NOK', ref: 'batch-7', items: [{ description: 'mails', quantity: 2, unit: 'mail', unit_price: 0.5 }] }
    const event: AppCostInput = { module: 'uptime', at: '2026-10-30T09:30:00Z', amount: 0.06, currency: 'NOK', ref: 'check-1' }
    const evidence: CostEvidence = { subject: { type: 'claimius.organization', id: '10000000-0000-4000-8000-000000000001' }, items: [{ description: 'mails', quantity: 2, unit: 'mail', amount: 1 }] }
    expect([row, items, event].map((input) => input.ref)).toEqual(['2026-10-30T09', 'batch-7', 'check-1'])
    expect(evidence.items?.[0].amount).toBe(1)
  })
})
