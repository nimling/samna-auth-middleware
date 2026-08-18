import { configureConsent, openConsentSheet, closeConsentSheet } from './consent'
import type { ConsentCategoryDefaults } from './consent'
import { postView, flush, stopAnalytics, getConsentState, postConsent } from './analytics'

export {
  configureConsent,
  openConsentSheet,
  closeConsentSheet,
  postView,
  flush,
  stopAnalytics,
  getConsentState,
  postConsent
}

/**
 * Reads the data-categories attribute, a comma separated list where a name on
 * its own starts on and a name suffixed with :off starts off. Absent means the
 * page takes every category.
 */
function bootCategories(raw?: string): ConsentCategoryDefaults | null {
  if (!raw) return null
  const out: ConsentCategoryDefaults = {}
  for (const entry of raw.split(',')) {
    const [name, state] = entry.trim().split(':')
    if (name === 'necessary' || name === 'preferences' || name === 'analytics' || name === 'marketing') {
      out[name] = state !== 'off'
    }
  }
  return out
}

function boot(): void {
  if (typeof document === 'undefined') return
  const script = document.currentScript as HTMLScriptElement | null
  if (!script || script.dataset.auto === 'false') return
  void configureConsent({
    endpoint: script.dataset.endpoint ?? '',
    consentPath: script.dataset.consentPath ?? '/analytics/consent',
    ingestPath: script.dataset.ingestPath ?? '/analytics',
    categories: bootCategories(script.dataset.categories),
    captureText: script.dataset.captureText === 'true',
    worker: script.dataset.worker !== 'false'
  })
}

boot()
