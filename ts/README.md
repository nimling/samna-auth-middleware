# @nimling/samna-auth-edge

Session auth edge client for `samna_auth`.

This package mirrors a strict subset of the Go middleware: session validation only. It is meant for browsers and edge contexts that hold no database and no client secret.

## In scope

1. Fetch OIDC configuration and JWKS from a samna_auth prophet

2. Validate access tokens signed by the prophet

3. Read the active session from `document.cookie` and validate it

4. Trigger login redirect and logout

5. Wrap `fetch` to include credentials and surface 401s as typed errors

## Out of scope

1. Schema sync (snapshot, incremental, replay). That is what the Go disciple does.

2. JWKS rotation polling. The cached JWKS is refreshed on validation failure only.

3. Auth code or refresh token exchange. Those flows hold `client_secret` and must run on the prophet, not on the edge.

4. Cookie writing. Cookies are issued by the prophet on successful login.

## API

1. `fetchOIDCConfiguration(endpoint, clientSlug)` returns `{ issuer, token_endpoint, jwks_uri }`

2. `getJWKS(jwksUri)` returns the parsed key set

3. `validateAccessToken(token, expectedIssuer, jwks)` returns a `SessionUser` or `null`

4. `readSessionFromDocument()` reads `sauth_access_token` from `document.cookie`, validates it, returns a `SessionUser` or `null`

5. `startLogin(provider?, returnTo?)` redirects the browser to the prophet's `/api/auth/login`

6. `endSession()` calls the prophet's `/api/auth/logout` with `credentials: 'include'`

7. `createAuthFetch(baseUrl)` returns a `fetch` wrapper that includes credentials and throws `AuthError` on 401
