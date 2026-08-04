import { describe, it, expect, vi } from 'vitest'
import { createServiceClient } from '../service'
import { ErrForbidden, ErrNotFound, ErrUnauthorized, USER_TOKEN_HEADER } from '../types'

function stubFetch(status = 200, payload: unknown = { data: { id: 'a1' } }) {
  const calls: { url: unknown; init: RequestInit | undefined }[] = []
  globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
    calls.push({ url, init })
    return new Response(status === 204 ? null : JSON.stringify(payload), { status })
  }) as unknown as typeof fetch
  return calls
}

const config = { baseUrl: 'https://api.example.com/', clientId: 'cid', clientSecret: 'csecret' }

describe('createServiceClient', () => {
  it('sends basic auth and unwraps the envelope', async () => {
    const calls = stubFetch()
    const client = createServiceClient(config)
    const out = await client.get<{ id: string }>('/employee')
    expect(out).toEqual({ id: 'a1' })
    expect(calls[0].url).toBe('https://api.example.com/employee')
    const headers = calls[0].init?.headers as Record<string, string>
    expect(headers.Authorization).toBe(`Basic ${btoa('cid:csecret')}`)
  })

  it('carries the user token when one is passed', async () => {
    const calls = stubFetch()
    const client = createServiceClient(config)
    await client.get('/employee', { userToken: 'tok' })
    const headers = calls[0].init?.headers as Record<string, string>
    expect(headers[USER_TOKEN_HEADER]).toBe('tok')
  })

  it('serializes a body and sets the content type on writes', async () => {
    const calls = stubFetch()
    const client = createServiceClient(config)
    await client.post('/employee', { name: 'Ada' })
    const headers = calls[0].init?.headers as Record<string, string>
    expect(headers['Content-Type']).toBe('application/json')
    expect(calls[0].init?.body).toBe(JSON.stringify({ name: 'Ada' }))
  })

  it('maps status codes onto the shared errors', async () => {
    const client = createServiceClient(config)
    stubFetch(401)
    await expect(client.get('/employee')).rejects.toBe(ErrUnauthorized)
    stubFetch(403)
    await expect(client.get('/employee')).rejects.toBe(ErrForbidden)
    stubFetch(404)
    await expect(client.get('/employee')).rejects.toBe(ErrNotFound)
  })

  it('returns undefined on no content', async () => {
    stubFetch(204)
    const client = createServiceClient(config)
    await expect(client.delete('/employee/a1')).resolves.toBeUndefined()
  })
})
