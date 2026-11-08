import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

vi.mock('rrweb-snapshot', () => ({ snapshot: () => ({ type: 0, childNodes: [], id: 1 }) }))

type Call = { url: unknown; init: RequestInit | undefined }

function stubFetch(responder: (url: string, init?: RequestInit) => Response): Call[] {
  const calls: Call[] = []
  globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
    calls.push({ url, init })
    return responder(String(url), init)
  }) as unknown as typeof fetch
  return calls
}

function envelope(data: unknown): Response {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' }
  })
}

const consentState = {
  anonymous_id: 'anon-1',
  policy_version: '1',
  policy_url: 'https://auth.example.com/legal/cookie-policy',
  decided: true,
  categories: { necessary: true, preferences: false, analytics: false, marketing: false }
}

function stubBrowser(): void {
  const store = new Map<string, string>()
  const shim = {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k)
  }
  const listeners = { addEventListener: () => {}, removeEventListener: () => {} }
  Object.assign(globalThis, {
    window: { localStorage: shim, sessionStorage: shim, innerWidth: 800, innerHeight: 600, ...listeners },
    localStorage: shim,
    sessionStorage: shim,
    document: { cookie: '', title: 'test', ...listeners },
    location: { origin: 'https://nim.io', pathname: '/', search: '' }
  })
}

function clearBrowser(): void {
  for (const key of ['window', 'localStorage', 'sessionStorage', 'document', 'location']) {
    delete (globalThis as Record<string, unknown>)[key]
  }
}

beforeEach(() => {
  vi.resetModules()
})

afterEach(() => {
  clearBrowser()
})

describe('configureAnalytics', () => {
  it('returns null when there is no window to record', async () => {
    const { configureAnalytics } = await import('./analytics')
    expect(await configureAnalytics()).toBeNull()
  })

  it('reads consent from the configured endpoint and carries the anonymous id', async () => {
    stubBrowser()
    const calls = stubFetch(() => envelope(consentState))
    const { configureAnalytics, getConsentState } = await import('./analytics')
    const state = await configureAnalytics({ endpoint: '/api' })
    expect(state?.anonymous_id).toBe('anon-1')
    expect(String(calls[0].url)).toContain('/api/analytics/consent')
    expect(getConsentState()?.policy_url).toBe(consentState.policy_url)
  })

  it('resolves to null and never throws when the consent read fails', async () => {
    stubBrowser()
    stubFetch(() => new Response('nope', { status: 500 }))
    const { configureAnalytics } = await import('./analytics')
    await expect(configureAnalytics({ endpoint: '/api' })).resolves.toBeNull()
  })

  it('resolves to null when fetch itself rejects', async () => {
    stubBrowser()
    globalThis.fetch = vi.fn(async () => {
      throw new Error('offline')
    }) as unknown as typeof fetch
    const { configureAnalytics } = await import('./analytics')
    await expect(configureAnalytics({ endpoint: '/api' })).resolves.toBeNull()
  })
})

describe('postConsent', () => {
  it('forces the necessary category on whatever the visitor chose', async () => {
    stubBrowser()
    const calls = stubFetch(() => envelope(consentState))
    const { configureAnalytics, postConsent } = await import('./analytics')
    await configureAnalytics({ endpoint: '/api' })
    await postConsent({ necessary: false, preferences: false, analytics: true, marketing: false })
    const body = JSON.parse(String(calls[1].init?.body))
    expect(body.categories.necessary).toBe(true)
    expect(body.categories.analytics).toBe(true)
    expect(body.anonymous_id).toBe('anon-1')
    expect(body.policy_version).toBe('1')
  })

  it('returns null on a rejected write rather than throwing', async () => {
    stubBrowser()
    let first = true
    stubFetch(() => {
      if (first) {
        first = false
        return envelope(consentState)
      }
      return new Response('no', { status: 502 })
    })
    const { configureAnalytics, postConsent } = await import('./analytics')
    await configureAnalytics({ endpoint: '/api' })
    await expect(
      postConsent({ necessary: true, preferences: false, analytics: true, marketing: false })
    ).resolves.toBeNull()
  })
})

describe('flush', () => {
  it('sends nothing while capture has not started', async () => {
    stubBrowser()
    const calls = stubFetch(() => envelope(consentState))
    const { configureAnalytics, flush } = await import('./analytics')
    await configureAnalytics({ endpoint: '/api' })
    const before = calls.length
    flush()
    expect(calls.length).toBe(before)
  })

  it('posts the batch to an absolute url so a worker can resolve it', async () => {
    stubBrowser()
    const granted = { ...consentState, categories: { ...consentState.categories, analytics: true } }
    const calls = stubFetch(() => envelope(granted))
    const { configureAnalytics, postView, flush } = await import('./analytics')
    await configureAnalytics({ endpoint: '/api', worker: false })
    postView('/service/cloud')
    flush()
    const ingest = calls.map((c) => String(c.url)).filter((u) => u.endsWith('/api/analytics'))
    expect(ingest).toContain('https://nim.io/api/analytics')
  })
})

describe('capture', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('posts one snapshot of a viewed path under an analytics grant', async () => {
    stubBrowser()
    const granted = { ...consentState, categories: { ...consentState.categories, analytics: true }, captured: [] }
    const calls = stubFetch(() => envelope(granted))
    const { configureAnalytics, postView } = await import('./analytics')
    await configureAnalytics({ endpoint: '/api', worker: false })
    postView('/service/snap-once')
    postView('/service/snap-once')
    await vi.advanceTimersByTimeAsync(2500)
    await vi.dynamicImportSettled()
    await vi.advanceTimersByTimeAsync(0)
    const posts = calls.filter((c) => String(c.url).includes('/analytics/capture?path=%2Fservice%2Fsnap-once'))
    expect(posts.length).toBe(1)
    expect(posts[0]?.init?.method).toBe('POST')
    expect(JSON.parse(String(posts[0]?.init?.body))).toEqual({ type: 0, childNodes: [], id: 1 })
  })

  it('skips a path the consent answer already holds a snapshot for', async () => {
    stubBrowser()
    const granted = { ...consentState, categories: { ...consentState.categories, analytics: true }, captured: ['/service/held'] }
    const calls = stubFetch(() => envelope(granted))
    const { configureAnalytics, postView } = await import('./analytics')
    await configureAnalytics({ endpoint: '/api', worker: false })
    postView('/service/held')
    await vi.advanceTimersByTimeAsync(2500)
    const posts = calls.filter((c) => String(c.url).includes('/analytics/capture?path=%2Fservice%2Fheld'))
    expect(posts).toEqual([])
  })

  it('never asks while the analytics category is withheld', async () => {
    stubBrowser()
    const calls = stubFetch(() => envelope(consentState))
    const { configureAnalytics, postView } = await import('./analytics')
    await configureAnalytics({ endpoint: '/api', worker: false })
    postView('/service/withheld')
    await vi.advanceTimersByTimeAsync(2500)
    const asked = calls
      .map((c) => String(c.url))
      .filter((u) => u.includes('/analytics/capture') && u.includes('withheld'))
    expect(asked).toEqual([])
  })
})

describe('heat', () => {
  it('folds pointer samples into one heat event per page instead of one event per sample', async () => {
    stubBrowser()
    const handlers = new Map<string, (ev: unknown) => void>()
    const doc = globalThis.document as unknown as Record<string, unknown>
    doc.addEventListener = (type: string, handler: (ev: unknown) => void) => void handlers.set(type, handler)
    doc.visibilityState = 'visible'
    const granted = { ...consentState, categories: { ...consentState.categories, analytics: true } }
    const calls = stubFetch(() => envelope(granted))
    const { configureAnalytics, flush } = await import('./analytics')
    await configureAnalytics({ endpoint: '/api', worker: false, pointerSampleMs: 0, heatCellPx: 16 })
    const move = handlers.get('pointermove')
    expect(move).toBeTypeOf('function')
    move!({ pageX: 20, pageY: 40 })
    move!({ pageX: 25, pageY: 47 })
    move!({ pageX: 100, pageY: 40 })
    flush()
    const ingest = calls.filter((c) => String(c.url).endsWith('/api/analytics'))
    const body = JSON.parse(String(ingest[ingest.length - 1]?.init?.body)) as { events: { type: string; payload: { cells?: number[][] } }[] }
    const heat = body.events.filter((e) => e.type === 'heat')
    expect(heat.length).toBe(1)
    expect(heat[0]?.payload.cells).toEqual([[1, 2, 2], [6, 2, 1]])
    expect(body.events.filter((e) => e.type === 'pointer')).toEqual([])
  })
})
