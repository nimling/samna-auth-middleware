---
layout: doc
title: BFF
description: The sauth-bff binary that sits in front of a static site, runs the login against Samna Auth, holds the session in cookies and gates the pages the site marks guarded.
tags: bff, sauth-bff, login, session, handoff, cookies, nimpress guard, static site
visibility: hidden
lastUpdated: true
---

# BFF

`sauth-bff` is a disciple with no tables of its own. It serves a static build, or proxies a dev server, and puts the login, the session cookies and the access check in front of it, so a site built from markdown can gate pages without a line of server code. It is `cmd/bff` in this repository, built from `bff/`, and every release attaches it to the GitHub release as `sauth-bff-<os>-<arch>`.

## Configuration

The binary reads the disciple variables `sdb.Init` reads, listed under [Configuration](./index.md#configuration), and four of its own.

| Variable | Default | Holds |
|---|---|---|
| `SAUTH_BFF_PORT` | `4300` | The port it listens on |
| `SAUTH_BFF_BASE_PATH` | `/api/auth` | The prefix the auth routes mount under; `SAUTH_REDIRECT_URI` points at `<origin><base path>/callback` |
| `SAUTH_BFF_STATIC_DIR` | | The folder of the built site; an unknown path answers `index.html` so the site's own router takes over |
| `SAUTH_BFF_PROXY_TARGET` | | A dev server to reverse proxy instead of serving `SAUTH_BFF_STATIC_DIR` |

The disciple variables it needs are `SAUTH_ENDPOINT`, `SAUTH_APP_SLUG`, `SAUTH_APP_SECRET`, `SAUTH_REDIRECT_URI`, the `PG*` set for the database its claimius copy lives in, and `SAUTH_CLIENT_ID` with `SAUTH_CLIENT_SECRET` for the subscription forward. `SAUTH_PAGE_REDIRECT` names where a failed login sends the visitor; it defaults to `/`.

## Routes

Every auth route sits under `SAUTH_BFF_BASE_PATH`. The status route sits beside it, under the same parent, so the default base path gives `/api/auth/*` and `/api/status/*`.

| Route | Auth | Does |
|---|---|---|
| `GET <base>/login?next=<path>` | none | Starts the PKCE flow against the prophet and remembers `next` for the return, or the same origin referring page when `next` is absent |
| `GET <base>/callback` | none | Exchanges the code for tokens, writes the session cookies and redirects to `next` |
| `GET <base>/session?handoff=<value>` | none | Proxies the handoff to the prophet with the browser's host in `X-Forwarded-Host`, so the prophet writes the session cookies for the site's own domain; see below |
| `GET <base>/logout` | none | Clears the cookies, revokes the token on the prophet and redirects to the end session page |
| `GET <base>/me` | session | The signed in user |
| `GET <base>/me/claim?with=claim_object` | session | The claims the user holds; `with=claim_object` adds each claim's bindings as `claim_objects`, whose `scope` carries the page gates |
| `POST <base>/app/:app_slug/subscription/subscribe` | none, 5 per hour per address | Forwards an email subscription to the prophet with the feed the site serves |
| `DELETE <base>/app/:app_slug/subscription/subscribe` | none, 5 per hour per address | Removes it |
| `GET /api/status/health` | none | The health answer the status probe reads; see [Health](./health.md) |
| `GET /*` | optional | The site, from `SAUTH_BFF_STATIC_DIR` or `SAUTH_BFF_PROXY_TARGET`; a session on the request lands the viewer in the context, nothing is refused here |

## The login shapes

An app on the prophet carries `connection`, and the value decides what the prophet does once the provider has signed the user in.

| `connection` | The provider callback | The session lands |
|---|---|---|
| `code` | Mints an auth code and redirects to the app's redirect URI with it; the app's `GET <base>/callback` exchanges it with the app secret | In cookies the app writes itself, for its own host |
| `bff` | Writes the session cookies itself and redirects the browser to `next` | In cookies the prophet writes, for the host the request came in on |
| `static` | Writes the session cookies on its own host and redirects the browser to the page the login started from | In cookies on the prophet host; the site reads them through the prophet, see [Static sites](./static.md) |

With `code` the token exchange is server to server and the cookies are the disciple's own; this is the shape every product service uses. With `bff` the prophet is the one holding the session, so the site and the prophet share a cookie domain and the site carries no secret in the exchange.

### The handoff

`bff` has one wrinkle: a cookie the prophet writes while answering on `auth.example.com` is a cookie for `auth.example.com`, and the site on `docs.example.com` never sees it. So when the app's primary origin is on another host than the prophet, the provider callback does not write cookies. It stores a `session_handoff` row that lives two minutes, and redirects the browser to `<app origin><base>/session?handoff=<value>`.

The bff's `GET <base>/session` reverse proxies that request to the prophet at the same path and query, addressed to the prophet's own host so an ingress that routes by host still matches, with the browser's host and scheme in `X-Forwarded-Host` and `X-Forwarded-Proto`. The prophet redeems the value once, derives the cookie domain from the forwarded host against the app's redirect URIs, writes `sauth_access_token`, `sauth_refresh_token`, `sauth_csrf_token` and `sauth_app_slug`, and answers a `307` to the stored `next` path. The bff passes the `Set-Cookie` headers back untouched and, when the prophet sends the visitor to `/` and the login was started with `<base>/login?next=<path>`, points the redirect at that path from the `sauth_login_next` cookie and clears it, so the cookies land on the site's own domain and the next request carries a session.

On the same host there is no handoff; the prophet sets the cookies in the callback and the site reads them. The handoff only happens when the hosts differ, and only for a `bff` app.

## The docs site

[developer.samna.io](https://developer.samna.io) is a nimpress build on GitHub Pages, which logs in through the prophet in the [static](./static.md) mode. Locally it runs behind this binary. The site marks a page guarded with `gate:` in its front matter, `nimpress build` moves those pages under `_guarded/` and writes `access.json`, and `nimpress guard map` records every guarded file with its gates. The deploy uploads the guarded bundle to the asset host, feeds the returned mapping to `nimpress guard apply`, which writes the base url and the published list back into `access.json` and removes the guarded folder from the public build. Locally the public build is what `SAUTH_BFF_STATIC_DIR` serves.

Behind the binary the `@nimling/samna-auth-middleware` session runs in `bff` mode against `<base>`: it reads `<base>/me` and `<base>/me/claim?with=claim_object`, and takes the page gates from `claim_objects[].scope` for the viewer, and sends the viewer to `<base>/login?next=<page>` when a gated page needs one. The nimpress runtime checks the viewer's claims against the gate and reads the gated page from where `access.json` says it was published. The binary never reads the guarded files; it only decides who is signed in.

Locally, `just bff` in the docs repository downloads the pinned release and runs it with `SAUTH_BFF_PROXY_TARGET` pointed at the dev server, with `SAUTH_EDGE_MODE=bff` in the docs `.env` so `nimpressAuth` runs in `bff` mode.
