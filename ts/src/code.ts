import { CODE_PARAM } from './types'
import type { Code } from './types'

export function getCodeURL(base: string, path: string, value: string): string {
  const url = new URL(path || '', base)
  url.searchParams.set(CODE_PARAM, value)
  return url.toString()
}

export async function getCode(url: string): Promise<Code | null> {
  try {
    const res = await fetch(url, { credentials: 'include' })
    if (!res.ok) return null
    const body = (await res.json()) as { data?: Code }
    return body.data ?? null
  } catch {
    return null
  }
}
