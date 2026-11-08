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
export const OBJECT_TYPE_CODE = 'claimius.code'
export const OBJECT_TYPE_APP_COST = 'claimius.app_cost'

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
export const CODE_PARAM = 'samk'
export const CODE_HEADER = 'SAuth-Code'
export const CODE_COOKIE = '__Host-sauth_samk'

export type CodeKind = 'invite' | 'guest'

export const CODE_KIND_INVITE: CodeKind = 'invite'
export const CODE_KIND_GUEST: CodeKind = 'guest'

export type GuestMode = 'open' | 'code' | 'off'

export const GUEST_MODE_OPEN: GuestMode = 'open'
export const GUEST_MODE_CODE: GuestMode = 'code'
export const GUEST_MODE_OFF: GuestMode = 'off'

export interface Code {
  id: string
  kind: CodeKind
  claim_id: string
  object_id?: string
  object_type?: string
  data: Record<string, unknown>
  starts_at?: string
  ends_at?: string
  count: number
  used_count: number
  revoked_at?: string
  owner_id: string
  created_by: string
  created_at: string
  updated_at: string
}

export interface CodeCreated extends Code {
  value: string
  url?: string
}

export interface BodyCode {
  kind: CodeKind
  object_id?: string
  object_type?: string
  data?: Record<string, unknown>
  starts_at?: string
  ends_at?: string
  count: number
  path?: string
}

export interface ProfileSchema {
  type: 'string' | 'number' | 'integer' | 'boolean' | 'array' | 'object' | 'null'
  title?: string
  description?: string
  format?: 'multiline' | 'markdown' | 'uri' | 'email' | 'phone' | 'date' | 'time' | 'datetime' | 'image' | 'color' | 'country' | 'currency' | 'percent' | 'duration'
  component?: 'text' | 'textarea' | 'markdown' | 'link' | 'badge' | 'chip' | 'tag' | 'list' | 'image' | 'avatar' | 'progress' | 'rating' | 'table' | 'keyvalue' | 'date' | 'boolean' | 'number' | 'card' | 'hidden'
  icon?: string
  order?: number
  hidden?: boolean
  enum?: unknown[]
  default?: unknown
  minimum?: number
  maximum?: number
  minLength?: number
  maxLength?: number
  pattern?: string
  link?: string
  items?: ProfileSchema
  properties?: Record<string, ProfileSchema>
  required?: string[]
}

export interface ProfileItem {
  schema: ProfileSchema
  value: unknown
}

export type Profile = Record<string, ProfileItem>

export interface BodyClaimGuest {
  mode: GuestMode
}

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
  optional?: boolean
  guest?: boolean
}

export interface UserAuthOptions {
  clientSlug?: string
  clientSecret?: string
  redirectURI?: string
  pageRedirectURI?: string
  optional?: boolean
  guest?: boolean
}

export interface ResponseToken {
  access_token: string
  refresh_token: string
  expires_in: number
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

export type CostGranularity = 'hour' | 'day' | 'month'
export type CostKind = 'usage' | 'fee' | 'period' | 'credit'
export type CostStatus = 'estimated' | 'settled' | 'invoiced'
export type CostAccumulateMethod = 'sum' | 'max' | 'last' | 'count'
export type CostProration = 'overlap' | 'start'

export const COST_GRANULARITY_HOUR: CostGranularity = 'hour'
export const COST_GRANULARITY_DAY: CostGranularity = 'day'
export const COST_GRANULARITY_MONTH: CostGranularity = 'month'
export const COST_SCHEMA_ID = 'https://samna.io/schema/cost/1'

export interface CostAccumulate {
  method?: CostAccumulateMethod
  proration?: CostProration
  group?: string[]
}

export interface CostPresentation {
  title?: string
  columns?: string[]
  unit_label?: string
  amount_label?: string
  accumulate?: CostAccumulate
}

export interface CostSchemaDocument {
  $schema?: string
  $id?: string
  'x-samna'?: CostPresentation
  [keyword: string]: unknown
}

export const COST_SCHEMA: CostSchemaDocument = {
  $schema: 'https://json-schema.org/draft/2020-12/schema',
  $id: COST_SCHEMA_ID,
  title: 'Cost evidence',
  type: 'object',
  additionalProperties: false,
  properties: {
    subject: {
      type: 'object',
      required: ['type', 'id'],
      additionalProperties: false,
      properties: {
        type: { type: 'string', pattern: '^[a-z_][a-z0-9_]*\\.[a-z_][a-z0-9_]*$' },
        id: { type: 'string', format: 'uuid' },
        name: { type: 'string' }
      }
    },
    source: {
      type: 'object',
      additionalProperties: false,
      properties: {
        vendor: { type: 'string' },
        ids: { type: 'array', items: { type: 'string' } },
        refs: {
          type: 'array',
          items: {
            type: 'object',
            required: ['table', 'id'],
            additionalProperties: false,
            properties: { table: { type: 'string' }, id: { type: 'string' } }
          }
        },
        run_id: { type: 'string' }
      }
    },
    items: {
      type: 'array',
      items: {
        type: 'object',
        required: ['description', 'quantity', 'unit', 'amount'],
        additionalProperties: false,
        properties: {
          description: { type: 'string' },
          quantity: { type: 'number' },
          unit: { type: 'string', minLength: 1 },
          unit_price: { type: 'number' },
          amount: { type: 'number' },
          ref: { type: 'string' },
          at: { type: 'string', format: 'date-time' }
        }
      }
    },
    pricing: {
      type: 'object',
      additionalProperties: false,
      properties: {
        list: { type: 'string' },
        tier: { type: 'string' },
        prorated: {
          type: 'object',
          required: ['from', 'to', 'fraction'],
          additionalProperties: false,
          properties: {
            from: { type: 'string', format: 'date-time' },
            to: { type: 'string', format: 'date-time' },
            fraction: { type: 'number', minimum: 0, maximum: 1 }
          }
        }
      }
    },
    notes: { type: 'string' }
  },
  'x-samna': {
    title: 'Cost',
    columns: ['subject.name', 'items', 'pricing.tier', 'notes'],
    unit_label: 'Unit',
    amount_label: 'Amount',
    accumulate: { method: 'sum', proration: 'overlap', group: ['subject.id'] }
  }
}

export interface CostSubject {
  type: string
  id: string
  name?: string
}

export interface CostSource {
  vendor?: string
  ids?: string[]
  refs?: { table: string; id: string }[]
  run_id?: string
}

export interface CostItem {
  description: string
  quantity: number
  unit: string
  unit_price?: number
  amount: number
  ref?: string
  at?: string
}

export interface CostPricing {
  list?: string
  tier?: string
  prorated?: { from: string; to: string; fraction: number }
}

export interface CostEvidence {
  subject?: CostSubject
  source?: CostSource
  items?: CostItem[]
  pricing?: CostPricing
  notes?: string
}

export interface AppCost {
  id: string
  module: string
  kind: CostKind
  period_start: string
  period_end: string
  quantity: number
  unit: string
  unit_price: number | null
  amount: number
  currency: string
  ref: string
  supersedes_id: string | null
  status: CostStatus
  schema: CostSchemaDocument
  detail: Record<string, unknown>
  owner_id: string
  created_by: string
  deleted_at?: string
  created_at: string
  updated_at: string
}

export interface AppCostItemInput {
  description: string
  quantity: number
  unit: string
  unit_price?: number
  amount?: number
  ref?: string
  at?: string
}

export interface AppCostInput {
  module: string
  ref: string
  currency: string
  kind?: CostKind
  period_start?: string
  period_end?: string
  at?: string
  quantity?: number
  unit?: string
  unit_price?: number
  amount?: number
  status?: CostStatus
  supersedes_id?: string
  items?: AppCostItemInput[]
  schema?: CostSchemaDocument
  detail?: Record<string, unknown>
}

export interface AppCostBucket {
  app_id?: string
  module: string
  kind: CostKind
  schema_id: string | null
  group_key: Record<string, unknown> | null
  period_start: string
  unit: string
  currency: string
  quantity: number
  amount: number
  hours: number
  rows: number
}

export interface AppCostQuery {
  module?: string
  status?: CostStatus
  subject_type?: string
  subject_id?: string
  from?: string
  to?: string
  granularity?: CostGranularity
}

export interface SyncConsumer {
  consumer_id: string
  peer: boolean
  txid: number
  seq: number
  last_contact_at: string
  in_contact: boolean
  head_txid: number
  head_seq: number
  trimmed_txid: number
  trimmed_seq: number
  behind: number
  next_txid?: number
  next_seq?: number
  next_operation?: 'upsert' | 'delete'
  next_event_type?: string
  next_payload_id?: string
}
