---
layout: doc
title: Static sites
description: The static edge mode and the nimpress adapter, for a site with no server of its own that logs in against Samna Auth and opens gated pages from the prophet.
tags: static, static site, nimpress, nimpressAuth, edge mode, login, session, gated pages, github pages
visibility: hidden
lastUpdated: true
---

# Static sites

A site served from a static host, such as GitHub Pages, has no `/api/auth/*` of its own and cannot run [sauth-bff](./bff.md). The `static` mode keeps the whole session on the prophet: the app on the prophet carries `connection: static`, and the browser half of `@nimling/samna-auth-middleware` reads the viewer, its claims and the login and logout routes from the prophet with `credentials: 'include'`.

## The app on the prophet

| `connection` | After the provider signs the user in | The session lands |
|---|---|---|
| `static` | Writes the session cookies on the prophet's own host and redirects to the page the login started from | In cookies for the prophet host, `SameSite=None; Secure`, sent with every credentialed request the site makes to the prophet |

There is no handoff and no secret on the site. The login start takes `next` as the absolute URL of the page to return to; the prophet accepts it only when its origin is the origin of one of the app's redirect URIs, and otherwise returns to the origin of the first one.

## Routes the site calls

All on the prophet, `<endpoint>`, with credentials. The prophet's CORS answers the site's origin with `Access-Control-Allow-Credentials: true`.

| Route | Does |
|---|---|
| `GET <endpoint>/api/auth/login?client_slug=<app>&next=<url>` | Starts the login on the prophet's login page for the app |
| `GET <endpoint>/api/me/user` | The signed in user of the app, `401` without a session |
| `GET <endpoint>/api/me/claim` | The claims the user holds in the app, each with its bindings as `claim_objects`, whose `scope` carries the page gates |
| `GET <endpoint>/api/auth/logout` | Ends the session and clears the cookies |
| `GET <endpoint>/api/asset/<id>/data` | A gated bundle the nimpress build published |

## The browser half

```ts
import { configureEdge, readSessionFromDocument, readClaims, startLogin, endSession } from '@nimling/samna-auth-middleware'

configureEdge({ mode: 'static', endpoint: 'https://auth.example.io', clientSlug: 'docs' })
```

`readSessionFromDocument` reads `/api/me/user`, `readClaims` reads `/api/me/claim`, `startLogin(undefined, '/test')` sends the browser to the prophet with `next` set to the absolute URL of `/test` on the current origin, and `endSession` calls the logout route with a credentialed `GET`.

## nimpress

`nimpressAuth` returns the `AuthFunctions` a nimpress client module exports. It configures the edge in `static` mode unless `mode` says otherwise, resolves the viewer, takes the page gates from the keys under `scope.page` of every binding the viewer holds, and answers `checkAccess` from them. The types are structural, so the package carries no dependency on nimpress.

```ts
import type { AuthFunctions } from '@nimtech/nimpress'
import { nimpressAuth } from '@nimling/samna-auth-middleware'

export const authFunctions: AuthFunctions = nimpressAuth({
  endpoint: import.meta.env.SAUTH_ENDPOINT,
  appSlug: import.meta.env.SAUTH_APP_SLUG
})
```

A page is readable through a claim on the app whose scope binding carries `{"page": {"<gate>": {"_access": "READ"}}}`, granted to the user.

## Limits

1. The cookies are third party cookies on the site's pages. A browser that blocks them, such as Safari with tracking prevention, signs in on the prophet but the site never sees the session.
2. The cookies sit on the prophet host, so a second app signing in on the same prophet in the same browser replaces them.
