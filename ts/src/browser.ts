import { configureConsent, openConsentBanner, closeConsentBanner } from './consent'
import { postView, flush, stopAnalytics, getConsentState, postConsent } from './analytics'

export {
  configureConsent,
  openConsentBanner,
  closeConsentBanner,
  postView,
  flush,
  stopAnalytics,
  getConsentState,
  postConsent
}

function boot(): void {
  if (typeof document === 'undefined') return
  const script = document.currentScript as HTMLScriptElement | null
  if (!script || script.dataset.auto === 'false') return
  void configureConsent({
    endpoint: script.dataset.endpoint ?? '',
    consentPath: script.dataset.consentPath ?? '/consent',
    ingestPath: script.dataset.ingestPath ?? '/analytics',
    captureText: script.dataset.captureText === 'true',
    worker: script.dataset.worker !== 'false'
  })
}

boot()
