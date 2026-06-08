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
      claims: payload.claims ?? []
    }
  } catch {
    return null
  }
}
