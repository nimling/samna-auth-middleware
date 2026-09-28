---
layout: doc
title: The middleware
description: samna_auth_middleware is the Go implementation of claimius for Samna Auth as prophet and for its disciples. What it carries, package by package, and where claimius ends and the middleware begins.
tags: samna auth middleware, go, sdb, smw, sam, client, shared, prophet, disciple, routed writes, pull sync
lastUpdated: true
---

# The middleware

Claimius is the schema: its model, its functions and their guarantees, independent of any language or host. `samna_auth_middleware`, the Go module `github.com/nimling/samna-auth-middleware`, is a Samna Auth centric implementation of claimius in Go. It carries the claimius schema for Samna Auth as the prophet and for every product service as a disciple, and it adds what a schema cannot do on its own: authenticate a request, route a write to the prophet, pull the feed, batch usage.

| Claimius | The middleware |
|---|---|
| The tables, the `get_*` and write functions, the triggers, the settle | The Go calls that reach them: `MutationAccess`, `ScopeAccess`, `ObjectWhere` and the rest |
| `sync_event`, `get_sync_events`, the cursors | The `/sam/sync` routes and the pull loop `SyncStart` |
| The actor model, `sa_created_by` | Stamping the actor on a routed write from the `AccessClaim` |
| `touch_object_usage` | Counting uses in process and flushing them |
| `prophet/database`, `disciple/database` | Shipping the two role sets as git includes for `smig` |

## Packages

| Package | Prophet | Disciple | Carries |
|---|---|---|---|
| `sdb` | `prophet/sdb` | `disciple/sdb` | The database handle, the access lookups, the write builders, the usage counter |
| `echo` | `prophet/echo` | `disciple/echo` | The HTTP handlers for the shared types |
| `sam` | `prophet/sam` | `disciple/sam` | Route registration; on the disciple the pull loop |
| `smw` | `smw` | `smw` | Authentication: bearer token, session cookie, client credentials, app key |
| `client` | | `client` | The HTTP client a disciple reaches the prophet with |
| `database` | `prophet/database` | `disciple/database` | The claimius role sets |
| `shared` | both | both | The implementation both roles use; never imported by a consumer |

## Files

| File | Covers |
|---|---|
| [Access](./access.md) | The Go access lookups, their SQL, error mapping, listings and the usage counter |
| [Writes](./writes.md) | Routed and local writes, debug mode |
| [Sync](./sync.md) | The `/sam/sync` routes and the pull loop |

## Authentication

`smw.UserAuthMiddleware` accepts a bearer token, then a session cookie, then client credentials. Tokens are verified locally against the prophet's JWKS, checked for issuer and audience, and bound to a DPoP proof or an mTLS thumbprint when the token carries `cnf` ([RFC 9449](https://www.rfc-editor.org/rfc/rfc9449), [RFC 8705](https://www.rfc-editor.org/rfc/rfc8705)). A machine client is verified with bcrypt against the local `samna_client` replica once per credential: the process keeps the SHA-256 of the secret that passed, and a later request with the same `client_id` compares its secret's digest to it in constant time and rereads the `samna_client` row by primary key for status and expiry. A bcrypt compare at cost 10 takes 72 to 86 ms of a core, a user's whole request about 0.3 ms, so repeating the proof on every call would make a machine client cost 150 to 570 users; see [Scaling](../scaling.md). `smw.AppMiddleware` keeps the imported key pair of an app per slug in the same way, reloaded when the `samna_app` row changes. `smw.AppMiddleware` authenticates a disciple to the prophet's `/sam` group with `SAuth-App-Slug` and `SAuth-App-Secret`. No request path calls the prophet to authenticate or authorize a read.
