import {
  EDGE_MODE_STATIC,
  configureEdge,
  endSession,
  readClaims,
  readSessionFromDocument,
  startLogin,
  type EdgeMode,
  type ViewerClaim
} from './session'
import type { UserType } from './types'

export const PAGE_SCOPE_KEY = 'page'

export interface NimpressViewer {
  authenticated: boolean
  id?: string
  firstName?: string
  lastName?: string
  userName?: string
  phone?: string
  email?: string
  location?: string
  type?: UserType
}

export interface NimpressAccessRequirement {
  gate?: string
}

export interface NimpressAuthFunctions {
  resolveViewer: (rp: unknown) => Promise<NimpressViewer | null>
  startLogin: (rp: unknown, returnTo: string) => void
  endSession: (rp: unknown, returnTo: string) => Promise<void>
  checkAccess: (required: NimpressAccessRequirement, viewer: NimpressViewer) => boolean
}

export interface NimpressAuthConfig {
  endpoint: string
  appSlug: string
  mode?: EdgeMode
}

export function scopePages(claims: ViewerClaim[]): string[] {
  const found = new Set<string>()
  for (const claim of claims) {
    for (const object of claim.claim_objects ?? []) {
      const page = object.scope?.[PAGE_SCOPE_KEY]
      if (typeof page === 'string') found.add(page)
      else if (page && typeof page === 'object') {
        for (const key of Object.keys(page)) found.add(key)
      }
    }
  }
  return [...found]
}

export function nimpressAuth(config: NimpressAuthConfig): NimpressAuthFunctions {
  configureEdge({
    endpoint: config.endpoint,
    clientSlug: config.appSlug,
    mode: config.mode ?? EDGE_MODE_STATIC
  })

  let pages: string[] = []

  return {
    async resolveViewer() {
      const user = await readSessionFromDocument()
      if (!user) {
        pages = []
        return null
      }
      pages = scopePages(await readClaims())
      return {
        authenticated: true,
        id: user.id,
        firstName: user.firstName,
        lastName: user.lastName,
        userName: user.userName,
        phone: user.phone,
        email: user.email,
        type: user.type
      }
    },

    startLogin(_rp, returnTo) {
      startLogin(undefined, returnTo)
    },

    async endSession(_rp, returnTo) {
      pages = []
      await endSession(returnTo)
    },

    checkAccess(required, viewer) {
      if (!required.gate) return true
      if (!viewer.authenticated) return false
      return pages.includes(required.gate)
    }
  }
}
