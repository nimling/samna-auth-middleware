export type UserType = 'user' | 'client' | 'system' | 'guest'

export interface SessionUser {
  id: string
  type: UserType
  firstName?: string
  lastName?: string
  userName?: string
  phone?: string
  email?: string
  location?: string
  claims: string[]
}

export interface OIDCConfiguration {
  issuer: string
  token_endpoint: string
  jwks_uri: string
  authorization_endpoint?: string
  userinfo_endpoint?: string
}

export interface EdgeConfig {
  endpoint: string
  clientSlug: string
  cookieNames?: {
    accessToken?: string
    refreshToken?: string
    csrfToken?: string
  }
}

export const DEFAULT_COOKIE_NAMES = {
  accessToken: 'sauth_access_token',
  refreshToken: 'sauth_refresh_token',
  csrfToken: 'sauth_csrf_token'
} as const

export class AuthError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
    this.name = 'AuthError'
  }
}
