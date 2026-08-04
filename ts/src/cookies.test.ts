import { describe, it, expect } from 'vitest'
import { parseCookies } from './cookies'

describe('parseCookies', () => {
  it('parses a single cookie', () => {
    expect(parseCookies('sauth_access_token=abc')).toEqual({ sauth_access_token: 'abc' })
  })

  it('parses multiple cookies', () => {
    expect(parseCookies('a=1; b=2; c=3')).toEqual({ a: '1', b: '2', c: '3' })
  })

  it('decodes percent-encoded values', () => {
    expect(parseCookies('x=hello%20world')).toEqual({ x: 'hello world' })
  })

  it('handles empty input', () => {
    expect(parseCookies('')).toEqual({})
  })
})
