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
  captured?: string[]
}

export interface AnalyticsConfig {
  endpoint?: string
  consentPath?: string
  ingestPath?: string
  capturePath?: string
  captureImage?: boolean
  flushIntervalMs?: number
  maxBatch?: number
  pointerSampleMs?: number
  scrollSampleMs?: number
  heatCellPx?: number
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
  capturePath: '/analytics/capture',
  flushIntervalMs: 10000,
  maxBatch: 200,
  pointerSampleMs: 100,
  scrollSampleMs: 250,
  heatCellPx: 16,
  captureText: false,
  captureImage: true,
  worker: true
}

class Batch {
  events: AnalyticsEventInput[] = []
  heat = new Map<string, { path: string; width: number; at: number; cells: Map<number, number> }>()
  cell: number

  constructor(cell: number) {
    this.cell = cell
  }

  event(e: AnalyticsEventInput): void {
    this.events.push(e)
  }

  pointer(path: string, width: number, x: number, y: number, at: number): void {
    const key = `${path}|${width}`
    let page = this.heat.get(key)
    if (!page) {
      page = { path, width, at, cells: new Map<number, number>() }
      this.heat.set(key, page)
    }
    const bx = Math.floor(x / this.cell)
    const by = Math.floor(y / this.cell)
    const id = bx * 1000000 + by
    page.cells.set(id, (page.cells.get(id) ?? 0) + 1)
  }

  size(): number {
    return this.events.length + this.heat.size
  }

  drain(): AnalyticsEventInput[] {
    const out = this.events
    this.events = []
    for (const page of this.heat.values()) {
      const cells: number[][] = []
      for (const [id, count] of page.cells) {
        cells.push([Math.floor(id / 1000000), id % 1000000, count])
      }
      out.push({
        type: 'heat',
        path: page.path,
        payload: { viewport_width: page.width, source: 'pointer', cells },
        occurred_at: page.at
      })
    }
    this.heat.clear()
    return out
  }
}

const workerSource = `const Batch=(${Batch.toString()});
let batch=null,cfg=null,timer=null;
function flush(headers){if(!batch||!cfg)return;const events=batch.drain();if(!events.length)return;if(headers)cfg.headers=headers;let body;try{body=JSON.stringify({anonymous_id:cfg.anonymousId,session_key:cfg.sessionKey,context:cfg.context,events})}catch(x){return}try{fetch(cfg.url,{method:'POST',body:body,headers:cfg.headers,credentials:'include',keepalive:true}).catch(function(){})}catch(x){}}
self.onmessage=function(e){const m=e.data;switch(m.k){case'start':cfg=m;batch=new Batch(m.cell);if(timer)clearInterval(timer);timer=setInterval(function(){flush()},m.interval);break;case'ctx':if(cfg){cfg.context=m.context;cfg.headers=m.headers}break;case'e':if(batch){batch.event(m.e);if(batch.size()>=cfg.maxBatch)flush()}break;case'p':if(batch)batch.pointer(m.path,m.width,m.x,m.y,m.at);break;case'flush':flush(m.headers);break;case'stop':flush(m.headers);if(timer)clearInterval(timer);timer=null;break}}`

let config = { ...defaults } as Required<Omit<AnalyticsConfig, 'onConsent'>> & Pick<AnalyticsConfig, 'onConsent'>
let consent: ConsentState | null = null
let batch: Batch | null = null
let sessionKey = ''
let anonymousId = ''
let context: Record<string, unknown> = {}
let flushTimer: ReturnType<typeof setInterval> | null = null
const captured = new Set<string>()
const captureSettleMs = 2000
let worker: Worker | null = null
let started = false
let detach: Array<() => void> = []

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

function refreshContext(): void {
  context = sessionContext()
  worker?.postMessage({ k: 'ctx', context, headers: csrfHeaders() })
}

function lowEnd(): boolean {
  const nav = typeof navigator === 'undefined' ? undefined : (navigator as Navigator & { connection?: { saveData?: boolean } })
  return nav?.connection?.saveData === true || (nav?.hardwareConcurrency ?? 4) <= 2
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
  const event: AnalyticsEventInput = { type, path: location.pathname, payload, occurred_at: Date.now() }
  if (worker) {
    worker.postMessage({ k: 'e', e: event })
    return
  }
  batch?.event(event)
  if (batch && batch.size() >= config.maxBatch) flush()
}

function pushPointer(x: number, y: number): void {
  if (!started) return
  const at = Date.now()
  if (worker) {
    worker.postMessage({ k: 'p', path: location.pathname, width: window.innerWidth, x, y, at })
    return
  }
  batch?.pointer(location.pathname, window.innerWidth, x, y, at)
}

function send(body: string, beacon: boolean): void {
  const url = ingestUrl()
  if (beacon && typeof navigator !== 'undefined' && typeof navigator.sendBeacon === 'function') {
    try {
      navigator.sendBeacon(url, new Blob([body], { type: 'application/json' }))
      return
    } catch {}
  }
  try {
    fetch(url, { method: 'POST', body, headers: csrfHeaders(), credentials: 'include', keepalive: true }).catch(() => {})
  } catch {}
}

export function flush(beacon = false): void {
  if (!started) return
  if (worker) {
    worker.postMessage({ k: 'flush', headers: csrfHeaders() })
    return
  }
  if (!batch || batch.size() === 0) return
  const events = batch.drain()
  let body: string
  try {
    body = JSON.stringify({ anonymous_id: anonymousId, session_key: sessionKey, context, events })
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
  listen(document, 'pointermove', (ev) => {
    const now = Date.now()
    if (now - lastPointer < config.pointerSampleMs) return
    lastPointer = now
    pushPointer(ev.pageX, ev.pageY)
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
    refreshContext()
  }, { passive: true })
}

function captureUrl(path: string): string {
  const url = `${config.endpoint.replace(/\/$/, '')}${config.capturePath}?path=${encodeURIComponent(path)}`
  if (typeof location === 'undefined') return url
  return new URL(url, location.origin).toString()
}

function whenIdle(): Promise<void> {
  return new Promise((resolve) => {
    const idle = (window as Window & { requestIdleCallback?: (cb: () => void, options?: { timeout: number }) => number })
      .requestIdleCallback
    if (typeof idle === 'function') idle(() => resolve(), { timeout: 10000 })
    else setTimeout(resolve, 0)
  })
}

async function postCapture(path: string): Promise<void> {
  await whenIdle()
  if (document.visibilityState === 'hidden') {
    captured.delete(path)
    return
  }
  const { snapshot } = await import('rrweb-snapshot')
  const tree = snapshot(document, {
    maskAllInputs: true,
    maskTextSelector: config.captureText ? null : '*',
    inlineStylesheet: true
  })
  if (!tree) {
    captured.delete(path)
    return
  }
  await fetch(captureUrl(path), {
    method: 'POST',
    body: JSON.stringify(tree),
    headers: csrfHeaders(),
    credentials: 'include'
  })
}

function scheduleCapture(path: string): void {
  if (!config.captureImage || captured.has(path) || lowEnd()) return
  if (consent?.categories?.analytics !== true) return
  if (consent.captured?.includes(path)) return
  captured.add(path)
  setTimeout(() => {
    postCapture(path).catch(() => captured.delete(path))
  }, captureSettleMs)
}

export function postView(path?: string, title?: string): void {
  const here = path ?? location.pathname + location.search
  push('view', { path: here, title: title ?? document.title })
  scheduleCapture(here)
}

function start(): void {
  if (started || typeof window === 'undefined') return
  started = true
  sessionKey = ensureSessionKey()
  context = sessionContext()
  if (config.worker && typeof Worker !== 'undefined') {
    try {
      worker = new Worker(URL.createObjectURL(new Blob([workerSource], { type: 'text/javascript' })))
      worker.postMessage({
        k: 'start',
        url: ingestUrl(),
        headers: csrfHeaders(),
        anonymousId,
        sessionKey,
        context,
        cell: config.heatCellPx,
        interval: config.flushIntervalMs,
        maxBatch: config.maxBatch
      })
    } catch {
      worker = null
    }
  }
  if (!worker) {
    batch = new Batch(config.heatCellPx)
    flushTimer = setInterval(() => flush(), config.flushIntervalMs)
  }
  attach()
  postView()
}

export function stopAnalytics(): void {
  if (!started) return
  if (worker) {
    worker.postMessage({ k: 'stop', headers: csrfHeaders() })
    const held = worker
    setTimeout(() => held.terminate(), 1000)
    worker = null
  } else {
    flush(true)
  }
  started = false
  detach.forEach((off) => off())
  detach = []
  if (flushTimer) clearInterval(flushTimer)
  flushTimer = null
  batch = null
}

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

export function getConsentState(): ConsentState | null {
  return consent
}

export async function configureAnalytics(options: AnalyticsConfig = {}): Promise<ConsentState | null> {
  if (typeof window === 'undefined') return null
  config = { ...defaults, ...options }
  anonymousId = readStore(ANONYMOUS_ID_STORAGE)
  const state = await getConsent()
  if (!state) return null
  applyConsent(state)
  return state
}
