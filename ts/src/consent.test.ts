// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

type Call = { url: unknown; init: RequestInit | undefined }

const consentState = {
  anonymous_id: 'anon-1',
  policy_version: '1',
  policy_url: 'https://auth.example.com/legal/cookie-policy',
  decided: false,
  categories: { necessary: true, preferences: false, analytics: false, marketing: false }
}

function stubFetch(state: unknown = consentState): Call[] {
  const calls: Call[] = []
  globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
    calls.push({ url, init })
    return new Response(JSON.stringify({ data: state }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    })
  }) as unknown as typeof fetch
  return calls
}

function sheet(): ShadowRoot {
  const host = document.getElementById('sauth-consent-sheet')
  if (!host?.shadowRoot) throw new Error('sheet not mounted')
  return host.shadowRoot
}

function buttonNamed(root: ShadowRoot, label: string): HTMLButtonElement {
  const found = Array.from(root.querySelectorAll('button')).find((b) => b.textContent === label)
  if (!found) throw new Error(`no button labelled ${label}`)
  return found as HTMLButtonElement
}

beforeEach(() => {
  vi.resetModules()
  document.body.innerHTML = ''
})

afterEach(() => {
  document.body.innerHTML = ''
})

describe('configureConsent', () => {
  it('mounts the sheet when the visitor has not decided', async () => {
    stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    expect(document.getElementById('sauth-consent-sheet')).not.toBeNull()
    expect(sheet().querySelector('h2')?.textContent).toContain('Cookies')
  })

  it('leaves the sheet closed when a decision is already stored', async () => {
    stubFetch({ ...consentState, decided: true })
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    expect(document.getElementById('sauth-consent-sheet')).toBeNull()
  })

  it('offers one toggle per category and locks the necessary one', async () => {
    stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    const inputs = Array.from(sheet().querySelectorAll('input[type=checkbox]')) as HTMLInputElement[]
    expect(inputs).toHaveLength(4)
    expect(inputs[0].disabled).toBe(true)
    expect(inputs[0].checked).toBe(true)
  })

  it('links to the policy page the backend resolved', async () => {
    stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    const link = sheet().querySelector('a') as HTMLAnchorElement
    expect(link.href).toBe(consentState.policy_url)
    expect(link.target).toBe('_blank')
  })

  it('expands the detail panel when customize is pressed', async () => {
    stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    const root = sheet()
    const customize = buttonNamed(root, 'Customize')
    expect(root.querySelector('.details')?.classList.contains('open')).toBe(false)
    customize.click()
    expect(root.querySelector('.details')?.classList.contains('open')).toBe(true)
    expect(customize.getAttribute('aria-expanded')).toBe('true')
  })
})

describe('a decision', () => {
  it('posts every category on accept and closes the sheet', async () => {
    const calls = stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    buttonNamed(sheet(), 'Accept all').click()
    await vi.waitFor(() => expect(calls.length).toBe(2))
    const body = JSON.parse(String(calls[1].init?.body))
    expect(body.categories).toEqual({
      necessary: true,
      preferences: true,
      analytics: true,
      marketing: true
    })
    await vi.waitFor(() => expect(document.getElementById('sauth-consent-sheet')).toBeNull())
  })

  it('keeps the sheet open when the decision is not saved', async () => {
    const calls: Call[] = []
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      calls.push({ url, init })
      if (init?.method === 'POST') return new Response('{}', { status: 502 })
      return new Response(JSON.stringify({ data: consentState }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    }) as unknown as typeof fetch
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    buttonNamed(sheet(), 'Accept all').click()
    await vi.waitFor(() => expect(calls.length).toBe(2))
    await vi.waitFor(() => expect(buttonNamed(sheet(), 'Accept all').disabled).toBe(false))
    expect(document.getElementById('sauth-consent-sheet')).not.toBeNull()
  })

  it('shows no sheet when the consent service is unavailable', async () => {
    globalThis.fetch = vi.fn(async () => new Response('{}', { status: 503 })) as unknown as typeof fetch
    const { configureConsent } = await import('./consent')
    expect(await configureConsent({ endpoint: '/api' })).toBeNull()
    expect(document.getElementById('sauth-consent-sheet')).toBeNull()
  })

  it('posts only the necessary category on reject', async () => {
    const calls = stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    buttonNamed(sheet(), 'Reject optional').click()
    await vi.waitFor(() => expect(calls.length).toBe(2))
    const body = JSON.parse(String(calls[1].init?.body))
    expect(body.categories).toEqual({
      necessary: true,
      preferences: false,
      analytics: false,
      marketing: false
    })
  })

  it('carries the per category choices when saved from the detail panel', async () => {
    const calls = stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({
      endpoint: '/api',
      categories: { preferences: false, analytics: false, marketing: false }
    })
    const root = sheet()
    buttonNamed(root, 'Customize').click()
    const inputs = Array.from(root.querySelectorAll('input[type=checkbox]')) as HTMLInputElement[]
    inputs[2].checked = true
    inputs[2].dispatchEvent(new Event('change'))
    buttonNamed(root, 'Save choices').click()
    await vi.waitFor(() => expect(calls.length).toBe(2))
    const body = JSON.parse(String(calls[1].init?.body))
    expect(body.categories.analytics).toBe(true)
    expect(body.categories.marketing).toBe(false)
  })
})

describe('openConsentSheet', () => {
  it('reopens the sheet after a decision so settings stay reachable', async () => {
    stubFetch({ ...consentState, decided: true })
    const { configureConsent, openConsentSheet, closeConsentSheet } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    expect(document.getElementById('sauth-consent-sheet')).toBeNull()
    openConsentSheet()
    expect(document.getElementById('sauth-consent-sheet')).not.toBeNull()
    closeConsentSheet()
    expect(document.getElementById('sauth-consent-sheet')).toBeNull()
  })
})

describe('the categories declaration', () => {
  it('carries every category toggled on when none is declared', async () => {
    stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    const root = sheet()
    buttonNamed(root, 'Customize').click()
    const inputs = Array.from(root.querySelectorAll('input[type=checkbox]')) as HTMLInputElement[]
    expect(inputs.length).toBe(4)
    expect(inputs.map((i) => i.checked)).toEqual([true, true, true, true])
  })

  it('treats null the same as absent', async () => {
    stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api', categories: null })
    const root = sheet()
    buttonNamed(root, 'Customize').click()
    const inputs = Array.from(root.querySelectorAll('input[type=checkbox]')) as HTMLInputElement[]
    expect(inputs.length).toBe(4)
  })

  it('renders only the declared categories and always carries necessary', async () => {
    stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api', categories: { analytics: true } })
    const root = sheet()
    buttonNamed(root, 'Customize').click()
    const names = Array.from(root.querySelectorAll('.category-name')).map((n) => n.textContent)
    expect(names).toEqual(['Strictly necessary', 'Analytics'])
  })

  it('starts a declared category off when it is declared false', async () => {
    stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api', categories: { analytics: true, marketing: false } })
    const root = sheet()
    buttonNamed(root, 'Customize').click()
    const inputs = Array.from(root.querySelectorAll('input[type=checkbox]')) as HTMLInputElement[]
    expect(inputs.map((i) => i.checked)).toEqual([true, true, false])
  })

  it('grants only the declared categories on accept', async () => {
    const calls = stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api', categories: { analytics: true } })
    buttonNamed(sheet(), 'Accept all').click()
    await vi.waitFor(() => expect(calls.length).toBe(2))
    const body = JSON.parse(String(calls[1].init?.body))
    expect(body.categories).toEqual({
      necessary: true,
      preferences: false,
      analytics: true,
      marketing: false
    })
  })

  it('keeps a stored decision over the declared default when reopened', async () => {
    stubFetch({ ...consentState, decided: true, categories: { ...consentState.categories, analytics: false } })
    const { configureConsent, openConsentSheet } = await import('./consent')
    await configureConsent({ endpoint: '/api', categories: { analytics: true } })
    openConsentSheet({ categories: { analytics: true } })
    const root = sheet()
    buttonNamed(root, 'Customize').click()
    const inputs = Array.from(root.querySelectorAll('input[type=checkbox]')) as HTMLInputElement[]
    expect(inputs.map((i) => i.checked)).toEqual([true, false])
  })
})
