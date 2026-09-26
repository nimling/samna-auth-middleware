export type UserType = 'user' | 'client' | 'system' | 'guest'

export const USER_TYPE_USER: UserType = 'user'
export const USER_TYPE_CLIENT: UserType = 'client'
export const USER_TYPE_SYSTEM: UserType = 'system'
export const USER_TYPE_GUEST: UserType = 'guest'

export type ClaimType = 'system' | 'user' | 'service' | 'virtual'

export const CLAIM_TYPE_SYSTEM: ClaimType = 'system'
export const CLAIM_TYPE_USER: ClaimType = 'user'
export const CLAIM_TYPE_SERVICE: ClaimType = 'service'
export const CLAIM_TYPE_VIRTUAL: ClaimType = 'virtual'

export type UserStatus = 'active' | 'pending' | 'suspended' | 'inactive'

export const USER_STATUS_ACTIVE: UserStatus = 'active'
export const USER_STATUS_PENDING: UserStatus = 'pending'
export const USER_STATUS_SUSPENDED: UserStatus = 'suspended'
export const USER_STATUS_INACTIVE: UserStatus = 'inactive'

export const ACCESS_NONE = 0
export const ACCESS_OWNER = 1 << 0
export const ACCESS_WRITE = 1 << 1
export const ACCESS_READ = 1 << 2
export const ACCESS_EXECUTE = 1 << 3
export const ACCESS_DENY = 1 << 4
export const ACCESS_MEMBER = 1 << 5
export const ACCESS_ALL = ACCESS_OWNER | ACCESS_WRITE | ACCESS_READ | ACCESS_EXECUTE

export const OBJECT_TYPE_USER = 'claimius.samna_user'
export const OBJECT_TYPE_ORGANIZATION = 'claimius.organization'
export const OBJECT_TYPE_LOCATION = 'claimius.location'
export const OBJECT_TYPE_CLAIM = 'claimius.claim'
export const OBJECT_TYPE_USER_CLAIM = 'claimius.user_claim'
export const OBJECT_TYPE_CLAIM_OBJECT = 'claimius.claim_object'
export const OBJECT_TYPE_USER_RELATION = 'claimius.user_relation'
export const OBJECT_TYPE_SAMNA_APP = 'claimius.samna_app'
export const OBJECT_TYPE_SAMNA_CLIENT = 'claimius.samna_client'
export const OBJECT_TYPE_SAMNA_SECRET = 'claimius.samna_secret'

export const USER_ID_KEY = 'user_id'
export const ACCESS_TOKEN_KEY = 'access_token'

export const ACCESS_TOKEN_COOKIE = 'sauth_access_token'
export const REFRESH_TOKEN_COOKIE = 'sauth_refresh_token'
export const CSRF_TOKEN_COOKIE = 'sauth_csrf_token'
export const CSRF_TOKEN_HEADER = 'SAuth-CSRF-Token'

export const APP_SLUG_HEADER = 'SAuth-App-Slug'
export const APP_SECRET_HEADER = 'SAuth-App-Secret'
export const USER_TOKEN_HEADER = 'SAuth-User-Token'
export const USER_EXTERNAL_HEADER = 'SAuth-User-ID'

export interface ResponseEnvelope<T> {
  data: T
  error?: string
  request_id?: string
  count?: number
}

export interface ServiceClientConfig {
  baseUrl: string
  clientId: string
  clientSecret: string
  timeoutMs?: number
}

export interface ServiceCallOptions {
  userToken?: string
  userExternalId?: string
  headers?: Record<string, string>
  signal?: AbortSignal
}

export interface JWKSLike {
  keys: unknown[]
}

export interface UserDetail {
  key: string
  value: string
}

export interface ClaimUser {
  id: string
  firstName?: string
  lastName?: string
  userName?: string
  phone?: string
  email?: string
  location?: string
  type?: UserType
  details?: UserDetail[]
}

export interface DebugClient {
  name: string
  client_id: string
  client_secret: string
  description?: string
  status?: string
  expires_at?: string
  owner_id?: string
}

export interface AuthMiddlewareConfig {
  clientSlug: string
  clientSecret: string
  tokenURL: string
  jwksURL: string
  expectedTokenIssuer: string
  redirectURI: string
  pageRedirectURI: string
  openIDConfigURL: string
  debugUserID?: string
  debugClients?: Record<string, DebugClient>
}

export interface UserAuthOptions {
  clientSlug?: string
  clientSecret?: string
  redirectURI?: string
  pageRedirectURI?: string
}

export interface ResponseToken {
  access_token: string
  refresh_token: string
  access_expires_in: number
  refresh_expires_in: number
  token_type: string
}

export interface OpenIDConfiguration {
  issuer: string
  token_endpoint: string
  jwks_uri: string
}

export class AuthError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'AuthError'
  }
}

export const ErrorTokenExpired = new AuthError('Token is expired')
export const ErrorUnauthorized = new AuthError('Unauthorized')
export const ErrorCredentialMissing = new AuthError('Credential missing')
export const ErrorUserTokenNotRecognized = new AuthError('User token not recognized')
export const ErrorUserExternalNotRecognized = new AuthError('User external id not recognized')

export const ErrBadRequest = new AuthError('bad request')
export const ErrForbidden = new AuthError('forbidden')
export const ErrNotFound = new AuthError('not found')
export const ErrUnauthorized = new AuthError('unauthorized')
