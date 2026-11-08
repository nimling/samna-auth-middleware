import {
  ErrBadRequest,
  ErrForbidden,
  ErrNotFound,
  ErrUnauthorized,
  USER_EXTERNAL_HEADER,
  USER_TOKEN_HEADER
} from './types'
import type {
  ResponseEnvelope,
  ServiceCallOptions,
  ServiceClientConfig
} from './types'

const DEFAULT_TIMEOUT_MS = 5000

function basicAuth(clientId: string, clientSecret: string): string {
  const raw = `${clientId}:${clientSecret}`
  if (typeof btoa === 'function') return `Basic ${btoa(raw)}`
  return `Basic ${Buffer.from(raw, 'utf8').toString('base64')}`
}

function callHeaders(auth: string, method: string, options?: ServiceCallOptions, hasBody = false): Record<string, string> {
  const headers: Record<string, string> = {
    Authorization: auth,
    Accept: 'application/json',
    ...(options?.headers ?? {})
  }
  if (hasBody) headers['Content-Type'] = 'application/json'
  if (options?.userToken) headers[USER_TOKEN_HEADER] = options.userToken
  if (options?.userExternalId) headers[USER_EXTERNAL_HEADER] = options.userExternalId
  return headers
}

function raise(status: number): never {
  if (status === 401) throw ErrUnauthorized
  if (status === 403) throw ErrForbidden
  if (status === 404) throw ErrNotFound
  if (status === 400 || status === 409) throw ErrBadRequest
  throw new Error(`service returned status ${status}`)
}

export interface ServiceClient {
  get<T>(path: string, options?: ServiceCallOptions): Promise<T>
  post<T>(path: string, body?: unknown, options?: ServiceCallOptions): Promise<T>
  put<T>(path: string, body?: unknown, options?: ServiceCallOptions): Promise<T>
  patch<T>(path: string, body?: unknown, options?: ServiceCallOptions): Promise<T>
  delete<T>(path: string, options?: ServiceCallOptions): Promise<T>
}

export function createServiceClient(config: ServiceClientConfig): ServiceClient {
  const base = config.baseUrl.replace(/\/$/, '')
  const auth = basicAuth(config.clientId, config.clientSecret)
  const timeoutMs = config.timeoutMs ?? DEFAULT_TIMEOUT_MS

  async function execute<T>(method: string, path: string, body: unknown, options?: ServiceCallOptions): Promise<T> {
    const url = `${base}/${path.replace(/^\//, '')}`
    const hasBody = body !== undefined && body !== null
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), timeoutMs)
    if (options?.signal) {
      options.signal.addEventListener('abort', () => controller.abort(), { once: true })
    }

    let res: Response
    try {
      res = await fetch(url, {
        method,
        headers: callHeaders(auth, method, options, hasBody),
        body: hasBody ? JSON.stringify(body) : undefined,
        signal: controller.signal
      })
    } finally {
      clearTimeout(timer)
    }

    if (!res.ok) raise(res.status)
    if (res.status === 204) return undefined as T

    const envelope = (await res.json()) as ResponseEnvelope<T>
    return envelope.data
  }

  return {
    get: <T>(path: string, options?: ServiceCallOptions) => execute<T>('GET', path, undefined, options),
    post: <T>(path: string, body?: unknown, options?: ServiceCallOptions) => execute<T>('POST', path, body, options),
    put: <T>(path: string, body?: unknown, options?: ServiceCallOptions) => execute<T>('PUT', path, body, options),
    patch: <T>(path: string, body?: unknown, options?: ServiceCallOptions) => execute<T>('PATCH', path, body, options),
    delete: <T>(path: string, options?: ServiceCallOptions) => execute<T>('DELETE', path, undefined, options)
  }
}
