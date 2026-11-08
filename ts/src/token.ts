import { jwtVerify, decodeJwt, createLocalJWKSet, errors as joseErrors } from 'jose'
import type { JWTPayload } from 'jose'
import { ErrorTokenExpired } from './types'
import type { ClaimUser } from './types'

export interface JWKSLike {
  keys: unknown[]
}

export async function validateToken(
  tokenString: string,
  expectedIssuer: string,
  audience: string,
  jwks: JWKSLike
): Promise<ClaimUser> {
  const keyset = createLocalJWKSet(jwks as never)
  try {
    const { payload } = await jwtVerify<JWTPayload>(tokenString, keyset, {
      issuer: expectedIssuer,
      audience
    })
    if (!payload.sub) {
      throw new Error('claims subject is missing')
    }
    return { id: payload.sub }
  } catch (err) {
    if (err instanceof joseErrors.JWTExpired) {
      throw ErrorTokenExpired
    }
    throw err
  }
}

export function claimsFromTokenUnsafe(tokenString: string): ClaimUser {
  const payload = decodeJwt(tokenString)
  if (!payload.sub) {
    throw new Error('parse user id: subject missing')
  }
  return { id: payload.sub }
}
