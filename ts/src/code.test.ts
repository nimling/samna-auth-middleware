import { afterEach, describe, expect, it, vi } from 'vitest'
import { getCode, getCodeURL } from './code'
import { CODE_PARAM } from './types'

describe('getCodeURL', () => {
  it('puts the value in samk on the path under the base', () => {
    expect(getCodeURL('https://svc.example.test', '/board?view=1', 'abc')).toBe('https://svc.example.test/board?view=1&samk=abc')
  })

  it('keeps the base when no path is given', () => {
    expect(getCodeURL('https://svc.example.test/page', '', 'abc')).toBe(`https://svc.example.test/page?${CODE_PARAM}=abc`)
  })
})

describe('getCode', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('reads the code the request carries', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ data: { id: 'c1', kind: 'guest', claim_id: 'k1', data: { view: 'board' } } })
    })
    vi.stubGlobal('fetch', fetchMock)
    const code = await getCode('/api/auth/code')
    expect(code?.data).toEqual({ view: 'board' })
    expect(fetchMock).toHaveBeenCalledWith('/api/auth/code', { credentials: 'include' })
  })

  it('answers null when the request carries no code', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 404 }))
    expect(await getCode('/api/auth/code')).toBeNull()
  })
})
