import { CSRF_TOKEN_COOKIE, CSRF_TOKEN_HEADER } from './types'
import { parseCookies } from './cookies'

export interface ConsentCategories {
  necessary: boolean
  preferences: boolean
  analytics: boolean
  marketing: boolean
}

export interface ConsentState {
  anonymous_id: string
  policy_version: string
  policy_url: string
  decided: boolean
  categories: ConsentCategories
  granted_at?: string
}

export interface AnalyticsConfig {
  endpoint?: string
  consentPath?: string
  ingestPath?: string
  flushIntervalMs?: number
  maxBatch?: number
  pointerSampleMs?: number
  scrollSampleMs?: number
  captureText?: boolean
  worker?: boolean
  onConsent?: (state: ConsentState) => void
}

export interface AnalyticsEventInput {
  type: string
  path: string
  payload: Record<string, unknown>
  occurred_at: number
}

const SESSION_KEY_STORAGE = 'sauth_session_key'
const ANONYMOUS_ID_STORAGE = 'sauth_anonymous_id'

const defaults: Required<Omit<AnalyticsConfig, 'onConsent'>> = {
  endpoint: '',
  consentPath: '/analytics/consent',
  ingestPath: '/analytics',
  flushIntervalMs: 10000,
  maxBatch: 200,
  pointerSampleMs: 100,
  scrollSampleMs: 250,
  captureText: false,
  worker: true
}

let config = { ...defaults } as Required<Omit<AnalyticsConfig, 'onConsent'>> & Pick<AnalyticsConfig, 'onConsent'>
let consent: ConsentState | null = null
let queue: AnalyticsEventInput[] = []
let sessionKey = ''
let anonymousId = ''
let flushTimer: ReturnType<typeof setInterval> | null = null
let worker: Worker | null = null
let started = false
let detach: Array<() => void> = []

const workerSource = `self.onmessage=function(e){try{fetch(e.data.url,{method:'POST',body:e.data.body,headers:e.data.headers,credentials:'include',keepalive:true}).catch(function(){})}catch(x){}}`

function csrfHeaders(): Record<string, string> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (typeof document === 'undefined') return headers
  const token = parseCookies(document.cookie)[CSRF_TOKEN_COOKIE]
  if (token) headers[CSRF_TOKEN_HEADER] = token
  return headers
}

function ingestUrl(): string {
  const path = `${config.endpoint.replace(/\/$/, '')}${config.ingestPath}`
  if (typeof location === 'undefined') return path
  return new URL(path, location.origin).toString()
}

function consentUrl(): string {
  return `${config.endpoint.replace(/\/$/, '')}${config.consentPath}`
}

function readStore(key: string): string {
  try {
    return window.localStorage.getItem(key) ?? ''
  } catch {
    return ''
  }
}

function writeStore(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value)
  } catch {}
}

function newId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  return `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 12)}`
}

function ensureSessionKey(): string {
  try {
    const held = window.sessionStorage.getItem(SESSION_KEY_STORAGE)
    if (held) return held
    const fresh = newId()
    window.sessionStorage.setItem(SESSION_KEY_STORAGE, fresh)
    return fresh
  } catch {
    return sessionKey || newId()
  }
}

function sessionContext(): Record<string, unknown> {
  const nav = typeof navigator === 'undefined' ? undefined : navigator
  return {
    entry_path: location.pathname + location.search,
    referrer: document.referrer,
    language: nav?.language,
    languages: nav?.languages,
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    platform: (nav as unknown as { userAgentData?: { platform?: string } })?.userAgentData?.platform ?? nav?.platform,
    screen_width: window.screen?.width,
    screen_height: window.screen?.height,
    device_pixel_ratio: window.devicePixelRatio,
    viewport_width: window.innerWidth,
    viewport_height: window.innerHeight,
    hardware_concurrency: nav?.hardwareConcurrency,
    max_touch_points: nav?.maxTouchPoints,
    color_scheme: window.matchMedia?.('(prefers-color-scheme: dark)')?.matches ? 'dark' : 'light',
    reduced_motion: window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches === true
  }
}

function selectorFor(el: Element | null): string {
  if (!el) return ''
  const parts: string[] = []
  let node: Element | null = el
  let depth = 0
  while (node && node.nodeType === 1 && depth < 5) {
    let part = node.tagName.toLowerCase()
    if (node.id) {
      parts.unshift(`${part}#${node.id}`)
      break
    }
    const cls = typeof node.className === 'string' ? node.className.trim().split(/\s+/).slice(0, 2).join('.') : ''
    if (cls) part += `.${cls}`
    const parent: Element | null = node.parentElement
    if (parent) {
      const siblings = Array.from(parent.children).filter((c) => c.tagName === node!.tagName)
      if (siblings.length > 1) part += `:nth-of-type(${siblings.indexOf(node) + 1})`
    }
    parts.unshift(part)
    node = parent
    depth += 1
  }
  return parts.join(' > ')
}

function labelFor(el: Element | null): string | undefined {
  if (!el || !config.captureText) return undefined
  const text = (el as HTMLElement).innerText ?? ''
  const trimmed = text.trim().slice(0, 80)
  return trimmed.length > 0 ? trimmed : undefined
}

function push(type: string, payload: Record<string, unknown>): void {
  if (!started) return
  queue.push({ type, path: location.pathname, payload, occurred_at: Date.now() })
  if (queue.length >= config.maxBatch) flush()
}

function send(body: string, beacon: boolean): void {
  const url = ingestUrl()
  if (beacon && typeof navigator !== 'undefined' && typeof navigator.sendBeacon === 'function') {
    try {
      navigator.sendBeacon(url, new Blob([body], { type: 'application/json' }))
      return
    } catch {}
  }
  if (worker) {
    try {
      worker.postMessage({ url, body, headers: csrfHeaders() })
      return
    } catch {}
  }
  try {
    fetch(url, { method: 'POST', body, headers: csrfHeaders(), credentials: 'include', keepalive: true }).catch(() => {})
  } catch {}
}

/**
 * Flushes the queue. Failures are swallowed on purpose: capture must never
 * surface an error into the page it is measuring.
 */
export function flush(beacon = false): void {
  if (!started || queue.length === 0) return
  const events = queue
  queue = []
  let body: string
  try {
    body = JSON.stringify({ anonymous_id: anonymousId, session_key: sessionKey, context: sessionContext(), events })
  } catch {
    return
  }
  send(body, beacon)
}

type CaptureEventMap = DocumentEventMap & WindowEventMap

function listen<K extends keyof CaptureEventMap>(
  target: Document | Window,
  type: K,
  handler: (ev: CaptureEventMap[K]) => void,
  options?: AddEventListenerOptions
): void {
  target.addEventListener(type, handler as EventListener, options)
  detach.push(() => target.removeEventListener(type, handler as EventListener, options))
}

function attach(): void {
  listen(document, 'click', (ev) => {
    const target = ev.target as Element | null
    push('click', {
      x: ev.clientX,
      y: ev.clientY,
      page_x: ev.pageX,
      page_y: ev.pageY,
      viewport_width: window.innerWidth,
      viewport_height: window.innerHeight,
      selector: selectorFor(target),
      tag: target?.tagName?.toLowerCase(),
      label: labelFor(target),
      button: ev.button
    })
  }, { capture: true, passive: true })

  let lastPointer = 0
  let pending: PointerEvent | null = null
  let frame = 0
  listen(document, 'pointermove', (ev) => {
    const now = Date.now()
    if (now - lastPointer < config.pointerSampleMs) return
    lastPointer = now
    pending = ev
    if (frame) return
    frame = requestAnimationFrame(() => {
      frame = 0
      if (!pending) return
      push('pointer', {
        x: pending.clientX,
        y: pending.clientY,
        page_x: pending.pageX,
        page_y: pending.pageY,
        viewport_width: window.innerWidth,
        viewport_height: window.innerHeight,
        pointer_type: pending.pointerType
      })
      pending = null
    })
  }, { capture: true, passive: true })

  let lastScroll = 0
  listen(window, 'scroll', () => {
    const now = Date.now()
    if (now - lastScroll < config.scrollSampleMs) return
    lastScroll = now
    const doc = document.documentElement
    const max = doc.scrollHeight - window.innerHeight
    push('scroll', {
      scroll_y: window.scrollY,
      depth: max > 0 ? Math.round((window.scrollY / max) * 100) : 0,
      viewport_height: window.innerHeight
    })
  }, { capture: true, passive: true })

  listen(document, 'input', (ev) => {
    const target = ev.target as HTMLInputElement | null
    push('input', {
      selector: selectorFor(target),
      tag: target?.tagName?.toLowerCase(),
      input_type: target?.type,
      name: target?.name,
      length: target?.value?.length ?? 0
    })
  }, { capture: true, passive: true })

  listen(document, 'submit', (ev) => {
    const target = ev.target as Element | null
    push('submit', { selector: selectorFor(target) })
  }, { capture: true, passive: true })

  listen(document, 'visibilitychange', () => {
    push('visibility', { state: document.visibilityState })
    if (document.visibilityState === 'hidden') flush(true)
  })

  listen(window, 'pagehide', () => {
    push('unload', {})
    flush(true)
  })

  listen(window, 'resize', () => {
    push('resize', { viewport_width: window.innerWidth, viewport_height: window.innerHeight })
  }, { passive: true })
}

/**
 * Records a page view. Single page apps call this on every route change,
 * because a history navigation fires no load event.
 */
export function postView(path?: string, title?: string): void {
  push('view', { path: path ?? location.pathname + location.search, title: title ?? document.title })
}

function start(): void {
  if (started || typeof window === 'undefined') return
  started = true
  sessionKey = ensureSessionKey()
  if (config.worker && typeof Worker !== 'undefined') {
    try {
      worker = new Worker(URL.createObjectURL(new Blob([workerSource], { type: 'text/javascript' })))
    } catch {
      worker = null
    }
  }
  attach()
  postView()
  flushTimer = setInterval(() => flush(), config.flushIntervalMs)
}

/**
 * Stops capture, releases every listener and drops the queue. Called when the
 * visitor withdraws the analytics category.
 */
export function stopAnalytics(): void {
  if (!started) return
  flush(true)
  started = false
  detach.forEach((off) => off())
  detach = []
  if (flushTimer) clearInterval(flushTimer)
  flushTimer = null
  if (worker) worker.terminate()
  worker = null
  queue = []
}

/**
 * Reads the visitor's consent from the backend. The answer resolves across every
 * samna_auth app, so a decision made elsewhere is already known here.
 */
export async function getConsent(): Promise<ConsentState | null> {
  try {
    const res = await fetch(`${consentUrl()}?anonymous_id=${encodeURIComponent(anonymousId)}`, {
      credentials: 'include',
      headers: { Accept: 'application/json' }
    })
    if (!res.ok) return null
    const body = (await res.json()) as { data?: ConsentState }
    return body.data ?? null
  } catch {
    return null
  }
}

/**
 * Records the visitor's decision and starts or stops capture to match it.
 */
export async function postConsent(categories: ConsentCategories): Promise<ConsentState | null> {
  const policyVersion = consent?.policy_version ?? ''
  try {
    const res = await fetch(consentUrl(), {
      method: 'POST',
      credentials: 'include',
      headers: csrfHeaders(),
      body: JSON.stringify({
        anonymous_id: anonymousId,
        policy_version: policyVersion,
        categories: { ...categories, necessary: true }
      })
    })
    if (!res.ok) return null
    const body = (await res.json()) as { data?: ConsentState }
    const state = body.data ?? null
    if (state) applyConsent(state)
    return state
  } catch {
    return null
  }
}

function applyConsent(state: ConsentState): void {
  consent = state
  if (state.anonymous_id) {
    anonymousId = state.anonymous_id
    writeStore(ANONYMOUS_ID_STORAGE, anonymousId)
  }
  if (state.categories?.analytics) {
    start()
  } else {
    stopAnalytics()
  }
  config.onConsent?.(state)
}

/**
 * The current consent state, or null before the first read resolves.
 */
export function getConsentState(): ConsentState | null {
  return consent
}

/**
 * Wires capture into the page. It reads the visitor's consent first and starts
 * recording only when the analytics category is granted. Nothing it does can
 * throw into the host app.
 */
export async function configureAnalytics(options: AnalyticsConfig = {}): Promise<ConsentState | null> {
  if (typeof window === 'undefined') return null
  config = { ...defaults, ...options }
  anonymousId = readStore(ANONYMOUS_ID_STORAGE)
  const state = await getConsent()
  if (!state) return null
  applyConsent(state)
  return state
}
