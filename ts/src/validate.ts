import { jwtVerify, createLocalJWKSet } from 'jose'
import type { JWTPayload } from 'jose'
import type { SessionUser, UserType } from './types'

interface TokenPayload extends JWTPayload {
  type?: UserType
  firstName?: string
  lastName?: string
  userName?: string
  phone?: string
  email?: string
  location?: string
  claims?: string[]
  scope?: string | string[]
  scopes?: string[]
}

function parseScopes(payload: TokenPayload): string[] {
  if (Array.isArray(payload.scopes)) return payload.scopes
  if (Array.isArray(payload.scope)) return payload.scope
  if (typeof payload.scope === 'string') {
    return payload.scope.split(/\s+/).filter(Boolean)
  }
  return []
}

export async function validateAccessToken(
  token: string,
  expectedIssuer: string,
  jwks: { keys: unknown[] }
): Promise<SessionUser | null> {
  try {
    const keyset = createLocalJWKSet(jwks as never)
    const { payload } = await jwtVerify<TokenPayload>(token, keyset, {
      issuer: expectedIssuer
    })
    if (!payload.sub) return null
    return {
      id: payload.sub,
      type: (payload.type ?? 'user') as UserType,
      firstName: payload.firstName,
      lastName: payload.lastName,
      userName: payload.userName,
      phone: payload.phone,
      email: payload.email,
      location: payload.location,
      claims: payload.claims ?? [],
      scopes: parseScopes(payload)
    }
  } catch {
    return null
  }
}
