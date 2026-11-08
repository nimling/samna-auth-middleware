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
import { ACCESS_DENY, ACCESS_EXECUTE, ACCESS_OWNER, ACCESS_READ, ACCESS_WRITE, type UserType } from './types'

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

function expandAccess(mask: number): number {
  let out = mask
  if (out & ACCESS_OWNER) out |= ACCESS_WRITE | ACCESS_READ | ACCESS_EXECUTE
  if (out & ACCESS_WRITE) out |= ACCESS_READ | ACCESS_EXECUTE
  if (out & ACCESS_EXECUTE) out |= ACCESS_READ
  return out
}

export function scopePages(claims: ViewerClaim[]): string[] {
  const grants = new Map<string, number>()
  const denies = new Map<string, number>()
  for (const claim of claims) {
    const level = claim.level ?? 0
    for (const object of claim.claim_objects ?? []) {
      const page = object.scope?.[PAGE_SCOPE_KEY]
      const pages = typeof page === 'string' ? [page] : page && typeof page === 'object' ? Object.keys(page) : []
      if (pages.length === 0) continue
      const binding = object.sa_access ?? null
      const effective = claim.access & expandAccess(binding ?? claim.access)
      const deny = (effective & ACCESS_DENY) !== 0
      if (!deny && (effective & ACCESS_READ) === 0) continue
      if (deny && (effective & ACCESS_READ) === 0) continue
      const into = deny ? denies : grants
      for (const key of pages) into.set(key, Math.max(into.get(key) ?? -1, level))
    }
  }
  const found: string[] = []
  for (const [key, level] of grants) {
    const denied = denies.get(key)
    if (denied === undefined || denied < level) found.push(key)
  }
  return found
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
