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

function banner(): ShadowRoot {
  const host = document.getElementById('sauth-consent-host')
  if (!host?.shadowRoot) throw new Error('banner not mounted')
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
  it('mounts the banner when the visitor has not decided', async () => {
    stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    expect(document.getElementById('sauth-consent-host')).not.toBeNull()
    expect(banner().querySelector('h2')?.textContent).toContain('Cookies')
  })

  it('leaves the banner closed when a decision is already stored', async () => {
    stubFetch({ ...consentState, decided: true })
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    expect(document.getElementById('sauth-consent-host')).toBeNull()
  })

  it('offers one toggle per category and locks the necessary one', async () => {
    stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    const inputs = Array.from(banner().querySelectorAll('input[type=checkbox]')) as HTMLInputElement[]
    expect(inputs).toHaveLength(4)
    expect(inputs[0].disabled).toBe(true)
    expect(inputs[0].checked).toBe(true)
  })

  it('links to the policy page the backend resolved', async () => {
    stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    const link = banner().querySelector('a') as HTMLAnchorElement
    expect(link.href).toBe(consentState.policy_url)
    expect(link.target).toBe('_blank')
  })

  it('expands the detail panel when customize is pressed', async () => {
    stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    const root = banner()
    const customize = buttonNamed(root, 'Customize')
    expect(root.querySelector('.details')?.classList.contains('open')).toBe(false)
    customize.click()
    expect(root.querySelector('.details')?.classList.contains('open')).toBe(true)
    expect(customize.getAttribute('aria-expanded')).toBe('true')
  })
})

describe('a decision', () => {
  it('posts every category on accept and closes the banner', async () => {
    const calls = stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    buttonNamed(banner(), 'Accept all').click()
    await vi.waitFor(() => expect(calls.length).toBe(2))
    const body = JSON.parse(String(calls[1].init?.body))
    expect(body.categories).toEqual({
      necessary: true,
      preferences: true,
      analytics: true,
      marketing: true
    })
    expect(document.getElementById('sauth-consent-host')).toBeNull()
  })

  it('posts only the necessary category on reject', async () => {
    const calls = stubFetch()
    const { configureConsent } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    buttonNamed(banner(), 'Reject optional').click()
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
    await configureConsent({ endpoint: '/api' })
    const root = banner()
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

describe('openConsentBanner', () => {
  it('reopens the banner after a decision so settings stay reachable', async () => {
    stubFetch({ ...consentState, decided: true })
    const { configureConsent, openConsentBanner, closeConsentBanner } = await import('./consent')
    await configureConsent({ endpoint: '/api' })
    expect(document.getElementById('sauth-consent-host')).toBeNull()
    openConsentBanner()
    expect(document.getElementById('sauth-consent-host')).not.toBeNull()
    closeConsentBanner()
    expect(document.getElementById('sauth-consent-host')).toBeNull()
  })
})
