# @nimling/samna-auth-middleware

TypeScript caller for `samna_auth`. Reads OIDC discovery, builds login and logout redirect URLs, reads the active session cookie, and validates access tokens with `jose`. The Go middleware in this same repo remains the canonical implementation; this package surfaces a focused subset that fits browser and edge runtimes.

## Roles by runtime

Capabilities split on whether the runtime can hold a confidential client secret. Browsers cannot. Node servers and edge runtimes can.

### Always available, browser and server

1. Read the OIDC discovery document and JWKS from a samna_auth host.

2. Validate an access token signed by samna_auth.

3. Read the `sauth_access_token` cookie from `document.cookie` and validate it.

4. Build redirect URLs:

4.1. `getLoginUrl(provider?, returnTo?)` returns `${endpoint}/api/auth/login?app=${slug}&...`.

4.2. `getLogoutUrl(returnTo?)` returns the matching logout URL.

5. Drive the redirect: `startLogin` and `endSession` call those URLs from the browser.

6. Wrap `fetch` to include credentials and raise a typed `AuthError` on 401.

### Requires `client_id` and `client_secret`, server only

These flows hold a confidential secret and must run on a first-party backend.

1. Authorization code exchange at `/oauth/token`, through `postAuthCode`.

2. Refresh token exchange at `/oauth/token`, through `postRefreshToken`.

3. Service to service calls with client credentials, through `createServiceClient`.

4. Local schema sync. The Go middleware's `sync.PullSnapshot` is the only implementation; a browser cannot hold the resulting database.

### Service to service

`createServiceClient` calls another Samna service with a machine client's credentials. It sends HTTP basic auth, unwraps the response envelope and returns `data`, and maps 401, 403, 404, 400 and 409 onto the exported `Err*` sentinels. 204 resolves to `undefined`.

```ts
import { createServiceClient } from '@nimling/samna-auth-middleware'

const api = createServiceClient({
  baseUrl: process.env.CONTENT_BASE_URL!,
  clientId: process.env.CONTENT_CLIENT_ID!,
  clientSecret: process.env.CONTENT_CLIENT_SECRET!
})

const articles = await api.get<Article[]>('/api/article')
```

The client id becomes the actor on the serving side, so what comes back is decided by the claims bound to that client. To act on behalf of a signed in human on the same call, pass `userToken` or `userExternalId` in the per call options; they travel as `SAuth-User-Token` and `SAuth-User-ID`.

Creating the client and granting it a claim happens on the prophet. See `docs/clients.md` in the repo root.

## Best practice: never ship client credentials to a plain frontend

This matches what the Go middleware in this repo documents and what `samna_auth` enforces. A `client_id` and `client_secret` represent the application, not the user. A frontend bundle is public by construction, so any secret embedded in it is the same as no secret at all.

When a flow needs the secret:

1. A first-party backend holds the secret in an environment variable.

2. The browser starts the login by being redirected to `samna_auth` through `getLoginUrl` or `startLogin`.

3. `samna_auth` redirects back to the first-party backend's callback URL with an authorization code.

4. The backend exchanges that code with its secret to obtain tokens, then sets the `sauth_access_token` cookie on the user's response.

5. From that point the browser only carries the cookie. The session is read with `readSessionFromDocument`. The browser never sees the secret.

The pattern matches the existing Go middleware's authentication flow. See the repo's top-level `README.md` for the canonical sequence.

## API

```ts
import {
  configureEdge,
  getLoginUrl,
  getLogoutUrl,
  getAppSlug,
  getAuthEndpoint,
  readSessionFromDocument,
  startLogin,
  endSession,
  createAuthFetch,
  validateToken,
  getOpenIDConfiguration,
  getJWKS,
  type ClaimUser
} from '@nimling/samna-auth-middleware'

configureEdge({
  endpoint: 'https://auth.example.com',
  clientSlug: 'docs'
})

const user: ClaimUser | null = await readSessionFromDocument()

startLogin('microsoft', window.location.pathname)

const url = getLoginUrl(undefined, '/protected')
```

## Consent and analytics

One call reads the visitor's consent, starts capture when the analytics category
is granted, and shows the banner when no decision is stored under the current
policy. Nothing it does can throw into the host app.

```ts
import { configureConsent, postView } from '@nimling/samna-auth-middleware'

await configureConsent({ endpoint: '/api' })

router.afterEach((to) => postView(to.fullPath))
```

The decision is stored against the account on the prophet, so one answer covers
every app the person signs in to. Reopen the banner from a settings control with
`openConsentBanner()`.

The backend half mounts the same way every other saecho handler does. Put it
outside any group that requires a session, because an anonymous visitor reaches
consent before they have one:

```go
sauth := e.Group("/api/sauth")
sauth.GET("/consent", saecho.GetConsent)
sauth.POST("/consent", saecho.PostConsent)
sauth.POST("/analytics", saecho.PostAnalytics)
```

The read routes answer with what was recorded about other people, so they belong
on a group that requires a session, behind whatever page grant gates the
consumer's admin surface:

```go
analytics := api.Group("/analytics")
analytics.GET("/event", saecho.GetAnalyticsEvents)
analytics.GET("/session", saecho.GetAnalyticsSessions)
```

## Build and publish

```bash
pnpm install
pnpm build
pnpm test
```

The repo's GitHub Actions workflow `typescript-publish.yml` builds and ships this package to GitHub Packages when a `v*` tag is pushed. The Go release workflow handles the Go module independently.
