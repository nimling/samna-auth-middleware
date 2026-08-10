import { configureAnalytics, postConsent, getConsentState } from './analytics'
import type { AnalyticsConfig, ConsentCategories, ConsentState } from './analytics'

export interface ConsentCopy {
  title: string
  body: string
  acceptAll: string
  rejectAll: string
  customize: string
  save: string
  readMore: string
  categories: Record<keyof ConsentCategories, { name: string; description: string }>
}

export interface ConsentBannerConfig extends AnalyticsConfig {
  copy?: Partial<ConsentCopy>
  mount?: HTMLElement
}

const HOST_ID = 'sauth-consent-host'

const defaultCopy: ConsentCopy = {
  title: 'Cookies on this service',
  body: 'We store a small amount of information in your browser to keep you signed in and, with your permission, to understand how the service is used. Your choice applies to every app that signs you in through this account.',
  acceptAll: 'Accept all',
  rejectAll: 'Reject optional',
  customize: 'Customize',
  save: 'Save choices',
  readMore: 'Read the full cookie policy',
  categories: {
    necessary: {
      name: 'Strictly necessary',
      description: 'Sign in, session continuity and security. These cannot be switched off and no analytics is stored for them.'
    },
    preferences: {
      name: 'Preferences',
      description: 'Remembers choices such as language and theme so the service looks the same on your next visit.'
    },
    analytics: {
      name: 'Analytics',
      description: 'Records which pages you open, where you click, how far you scroll and the size of your screen, so we can see which parts of the service work and which do not.'
    },
    marketing: {
      name: 'Marketing',
      description: 'Measures whether a campaign brought you here. Off unless you turn it on.'
    }
  }
}

const styles = `
:host { all: initial; }
.backdrop {
  position: fixed;
  inset: auto 0 0 0;
  display: flex;
  justify-content: center;
  padding: clamp(8px, 2vw, 24px);
  z-index: 2147483000;
  font-family: system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif;
  pointer-events: none;
}
.panel {
  pointer-events: auto;
  width: min(680px, 100%);
  max-height: min(80vh, 720px);
  overflow-y: auto;
  background: #ffffff;
  color: #14161a;
  border: 1px solid rgba(0, 0, 0, 0.12);
  border-radius: 14px;
  box-shadow: 0 18px 48px rgba(0, 0, 0, 0.24);
  padding: clamp(16px, 3vw, 24px);
  box-sizing: border-box;
  animation: rise 0.24s cubic-bezier(0.4, 0, 0.2, 1);
}
@keyframes rise {
  from { opacity: 0; transform: translateY(16px); }
  to { opacity: 1; transform: translateY(0); }
}
h2 { margin: 0 0 8px; font-size: 1.05rem; font-weight: 650; letter-spacing: -0.01em; }
p { margin: 0 0 14px; font-size: 0.875rem; line-height: 1.55; opacity: 0.82; }
a { color: inherit; font-size: 0.8125rem; text-decoration: underline; text-underline-offset: 3px; opacity: 0.7; }
a:hover { opacity: 1; }
.actions { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
button {
  appearance: none;
  font: inherit;
  font-size: 0.8125rem;
  font-weight: 550;
  border-radius: 8px;
  padding: 9px 16px;
  cursor: pointer;
  border: 1px solid transparent;
  transition: background 0.15s ease, border-color 0.15s ease;
}
.primary { background: #14161a; color: #ffffff; }
.primary:hover { background: #2b2f36; }
.secondary { background: transparent; color: inherit; border-color: rgba(0, 0, 0, 0.18); }
.secondary:hover { border-color: rgba(0, 0, 0, 0.4); }
.link { background: transparent; color: inherit; padding: 9px 4px; opacity: 0.7; text-decoration: underline; text-underline-offset: 3px; }
.link:hover { opacity: 1; }
.spacer { flex: 1 1 auto; }
.details { display: grid; grid-template-rows: 0fr; transition: grid-template-rows 0.28s cubic-bezier(0.4, 0, 0.2, 1); }
.details.open { grid-template-rows: 1fr; }
.details-inner { overflow: hidden; }
.category {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  padding: 14px 0;
  border-top: 1px solid rgba(0, 0, 0, 0.08);
}
.category-text { flex: 1 1 auto; }
.category-name { font-size: 0.8125rem; font-weight: 600; margin-bottom: 3px; }
.category-desc { font-size: 0.78125rem; line-height: 1.5; opacity: 0.72; }
.toggle { position: relative; flex: 0 0 auto; width: 40px; height: 23px; margin-top: 2px; }
.toggle input { position: absolute; opacity: 0; width: 100%; height: 100%; margin: 0; cursor: pointer; }
.track {
  position: absolute; inset: 0; border-radius: 999px;
  background: rgba(0, 0, 0, 0.22); transition: background 0.18s ease; pointer-events: none;
}
.thumb {
  position: absolute; top: 3px; left: 3px; width: 17px; height: 17px;
  border-radius: 50%; background: #ffffff; transition: transform 0.18s ease; pointer-events: none;
}
input:checked ~ .track { background: #1f8f4e; }
input:checked ~ .thumb { transform: translateX(17px); }
input:disabled ~ .track { background: rgba(0, 0, 0, 0.12); }
input:disabled ~ .thumb { background: rgba(255, 255, 255, 0.75); }
input:focus-visible ~ .track { outline: 2px solid #1f6feb; outline-offset: 2px; }
@media (prefers-color-scheme: dark) {
  .panel { background: #16181d; color: #f2f4f7; border-color: rgba(255, 255, 255, 0.14); }
  .secondary { border-color: rgba(255, 255, 255, 0.24); }
  .secondary:hover { border-color: rgba(255, 255, 255, 0.5); }
  .primary { background: #f2f4f7; color: #14161a; }
  .primary:hover { background: #ffffff; }
  .category { border-top-color: rgba(255, 255, 255, 0.1); }
  .track { background: rgba(255, 255, 255, 0.24); }
  .thumb { background: #16181d; }
  input:checked ~ .thumb { background: #ffffff; }
}
@media (max-width: 560px) {
  .backdrop { padding: 0; }
  .panel { border-radius: 14px 14px 0 0; max-height: 88vh; }
  .actions button { flex: 1 1 auto; }
}
`

const order: Array<keyof ConsentCategories> = ['necessary', 'preferences', 'analytics', 'marketing']

let host: HTMLElement | null = null

function removeBanner(): void {
  host?.remove()
  host = null
}

function render(state: ConsentState, copy: ConsentCopy, mount: HTMLElement): void {
  removeBanner()
  host = document.createElement('div')
  host.id = HOST_ID
  const root = host.attachShadow({ mode: 'open' })

  const chosen: ConsentCategories = { ...state.categories, necessary: true }

  const sheet = document.createElement('style')
  sheet.textContent = styles

  const backdrop = document.createElement('div')
  backdrop.className = 'backdrop'

  const panel = document.createElement('div')
  panel.className = 'panel'
  panel.setAttribute('role', 'dialog')
  panel.setAttribute('aria-modal', 'false')
  panel.setAttribute('aria-label', copy.title)

  const title = document.createElement('h2')
  title.textContent = copy.title

  const body = document.createElement('p')
  body.textContent = copy.body

  const details = document.createElement('div')
  details.className = 'details'
  const detailsInner = document.createElement('div')
  detailsInner.className = 'details-inner'
  details.appendChild(detailsInner)

  order.forEach((key) => {
    const row = document.createElement('div')
    row.className = 'category'

    const toggle = document.createElement('label')
    toggle.className = 'toggle'
    const input = document.createElement('input')
    input.type = 'checkbox'
    input.checked = key === 'necessary' ? true : chosen[key] === true
    input.disabled = key === 'necessary'
    input.setAttribute('aria-label', copy.categories[key].name)
    input.addEventListener('change', () => {
      chosen[key] = input.checked
    })
    const track = document.createElement('span')
    track.className = 'track'
    const thumb = document.createElement('span')
    thumb.className = 'thumb'
    toggle.append(input, track, thumb)

    const text = document.createElement('div')
    text.className = 'category-text'
    const name = document.createElement('div')
    name.className = 'category-name'
    name.textContent = copy.categories[key].name
    const desc = document.createElement('div')
    desc.className = 'category-desc'
    desc.textContent = copy.categories[key].description
    text.append(name, desc)

    row.append(text, toggle)
    detailsInner.appendChild(row)
  })

  if (state.policy_url) {
    const linkRow = document.createElement('div')
    linkRow.className = 'category'
    const link = document.createElement('a')
    link.href = state.policy_url
    link.target = '_blank'
    link.rel = 'noreferrer noopener'
    link.textContent = copy.readMore
    linkRow.appendChild(link)
    detailsInner.appendChild(linkRow)
  }

  const actions = document.createElement('div')
  actions.className = 'actions'

  const customize = document.createElement('button')
  customize.type = 'button'
  customize.className = 'link'
  customize.textContent = copy.customize
  customize.setAttribute('aria-expanded', 'false')

  const save = document.createElement('button')
  save.type = 'button'
  save.className = 'secondary'
  save.textContent = copy.save
  save.hidden = true

  customize.addEventListener('click', () => {
    const open = details.classList.toggle('open')
    customize.setAttribute('aria-expanded', open ? 'true' : 'false')
    save.hidden = !open
  })

  const reject = document.createElement('button')
  reject.type = 'button'
  reject.className = 'secondary'
  reject.textContent = copy.rejectAll

  const accept = document.createElement('button')
  accept.type = 'button'
  accept.className = 'primary'
  accept.textContent = copy.acceptAll

  const decide = (categories: ConsentCategories) => {
    removeBanner()
    void postConsent(categories)
  }

  reject.addEventListener('click', () =>
    decide({ necessary: true, preferences: false, analytics: false, marketing: false })
  )
  accept.addEventListener('click', () =>
    decide({ necessary: true, preferences: true, analytics: true, marketing: true })
  )
  save.addEventListener('click', () => decide({ ...chosen, necessary: true }))

  const spacer = document.createElement('div')
  spacer.className = 'spacer'

  actions.append(customize, spacer, save, reject, accept)
  panel.append(title, body, details, actions)
  backdrop.appendChild(panel)
  root.append(sheet, backdrop)
  mount.appendChild(host)
}

/**
 * Opens the consent banner regardless of whether a decision is already stored,
 * so an app can offer a "cookie settings" control that reopens it.
 */
export function openConsentBanner(options: ConsentBannerConfig = {}): void {
  const state = getConsentState()
  if (!state) return
  const copy: ConsentCopy = {
    ...defaultCopy,
    ...options.copy,
    categories: { ...defaultCopy.categories, ...options.copy?.categories }
  }
  render(state, copy, options.mount ?? document.body)
}

/**
 * Reads consent, starts capture when it is already granted, and shows the
 * banner when the visitor has not decided under the current policy. This is the
 * single call an app needs.
 */
export async function configureConsent(options: ConsentBannerConfig = {}): Promise<ConsentState | null> {
  const state = await configureAnalytics(options)
  if (!state) return null
  if (!state.decided) openConsentBanner(options)
  return state
}

/**
 * Removes the banner without recording a decision.
 */
export function closeConsentBanner(): void {
  removeBanner()
}
