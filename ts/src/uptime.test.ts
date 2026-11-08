import { describe, it, expect, vi } from 'vitest'
import { appStatusUrl, getAppStatus } from './uptime'
import { ErrNotFound } from './types'

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

const status = {
  app_slug: 'nimployer',
  app_name: 'Nimployer',
  monitors: [
    {
      id: 'a1',
      app_id: 'b2',
      name: 'heartbeat',
      kind: 'heartbeat',
      interval_seconds: 60,
      timeout_seconds: 10,
      expected_status: 200,
      enabled: true,
      owner_id: 'c3',
      created_by: 'd4',
      created_at: '2026-09-24T00:00:00Z',
      updated_at: '2026-09-24T00:00:00Z',
      last_checked_at: '2026-09-24T10:00:00Z',
      last_ok: true,
      days: [{ monitor_id: 'a1', app_id: 'b2', day: '2026-09-24', ok_count: 600, failed_count: 0 }]
    }
  ],
  incidents: []
}

describe('appStatusUrl', () => {
  it('joins the endpoint and the public status route', () => {
    expect(appStatusUrl('https://auth.example.com/', 'nimployer')).toBe(
      'https://auth.example.com/api/app/nimployer/status'
    )
  })
})

describe('getAppStatus', () => {
  it('fetches the public status and unwraps the envelope', async () => {
    const calls = stubFetch(200, { data: status, count: 1 })
    const got = await getAppStatus('https://auth.example.com', 'nimployer')
    expect(calls).toHaveLength(1)
    expect(String(calls[0].url)).toBe('https://auth.example.com/api/app/nimployer/status')
    expect(calls[0].init?.credentials).toBeUndefined()
    expect(got.app_slug).toBe('nimployer')
    expect(got.monitors[0].kind).toBe('heartbeat')
    expect(got.monitors[0].days?.[0].ok_count).toBe(600)
  })

  it('throws ErrNotFound for an unknown app', async () => {
    stubFetch(404, { error: { message: 'App not found' } })
    await expect(getAppStatus('https://auth.example.com', 'nope')).rejects.toBe(ErrNotFound)
  })

  it('throws on a server error', async () => {
    stubFetch(500, { error: { message: 'status query failed' } })
    await expect(getAppStatus('https://auth.example.com', 'nimployer')).rejects.toThrow('status returned 500')
  })
})
