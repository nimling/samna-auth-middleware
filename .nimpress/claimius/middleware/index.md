---
visibility: hidden
layout: doc
title: The middleware
description: samna_auth_middleware is the Go implementation of claimius for Samna Auth as prophet and for its disciples. What it carries, package by package, and where claimius ends and the middleware begins.
tags: samna auth middleware, go, sdb, smw, sam, client, shared, prophet, disciple, routed writes, pull sync, federation
lastUpdated: true
---

# The middleware

Claimius is the schema: its model, its functions and their guarantees, independent of any language or host. `samna_auth_middleware`, the Go module `github.com/nimling/samna-auth-middleware`, is a Samna Auth centric implementation of claimius in Go. It carries the claimius schema for Samna Auth as the prophet and for every product service as a disciple, and it adds what a schema cannot do on its own: authenticate a request, route a write to the prophet, pull the feed, count usage in process.

| Claimius | The middleware |
|---|---|
| The tables, the `get_*` and write functions, the triggers, the settle | The Go calls that reach them: `MutationAccess`, `ScopeAccess`, `ObjectWhere` and the rest |
| `sync_event`, `get_sync_events`, the `(txid, seq)` cursors | The `/sam/sync` routes, the pull loop `SyncStart`, the peer pull `PeerSyncStart` |
| `drain_access_diff` | The drain the pull loop runs between pulls, `PostAccessDiffDrain` |
| The actor model, `sa_created_by` | Stamping the actor on a routed write from the `AccessClaim` |
| `touch_object_usage` | Counting uses in process and flushing them |
| `get_app_home`, `check_sync_home`, `apply_peer_event` | `smw.AppHome`, the binding of a replica to one prophet, the peer pull |
| `prophet/database`, `disciple/database` | Shipping the two role sets as git includes for `smig` |

## Packages

| Package | Prophet | Disciple | Carries |
|---|---|---|---|
| `sdb` | `prophet/sdb` | `disciple/sdb` | The database handle, the access lookups, the write builders, the usage counter, the audit writer `Write` |
| `echo` | `prophet/echo` | `disciple/echo` | The HTTP handlers for the shared types |
| `sam` | `prophet/sam` | `disciple/sam` | Route registration; the pull loop, `Replica`, `LeadSync`, `WaitForSeq` and the peer pull |
| `smw` | `smw` | `smw` | Authentication, `AppMiddleware`, `SyncSeq`, `AppHome` |
| `client` | `client` | `client` | The HTTP client a replica reaches a prophet with, `SyncCursor`, `Bind` |
| `database` | `prophet/database` | `disciple/database` | The claimius role sets |
| `shared` | both | both | The implementation both roles use; never imported by a consumer |

## Files

| File | Covers |
|---|---|
| [Access](./access.md) | The Go access lookups, their SQL, error mapping, paged listings, graphs, the usage counter and replica routing |
| [Writes](./writes.md) | Routed and local writes, stamping the actor, audit, debug mode |
| [Sync](./sync.md) | The `/sam/sync` routes, the pull loop and the peer pull |

## Authentication

`smw.UserAuthMiddleware` accepts a bearer token, then a session cookie, then client credentials. Tokens are verified locally against the prophet's JWKS, checked for issuer and audience, and bound to a DPoP proof or an mTLS thumbprint when the token carries `cnf` ([RFC 9449](https://www.rfc-editor.org/rfc/rfc9449), [RFC 8705](https://www.rfc-editor.org/rfc/rfc8705)).

A machine client is verified with bcrypt against the local `samna_client` row once per credential. The process keeps, per `client_id`, the SHA-256 digest of the secret that passed and the `secret_hash` it passed against. A later request rereads the client row by `client_id`, live, active and unexpired, through `GetClient`, and compares its secret's digest to the kept one in constant time while the row's `secret_hash` is unchanged. So bcrypt runs once per credential, and a revoked client or a rotated secret stops on the next request.

`smw.AppMiddleware` authenticates a disciple, or a peer prophet, to the prophet's `/sam` group with `SAuth-App-Slug` and `SAuth-App-Secret`, and keeps the imported key pair of each app, imported again when the app row's key or seed changes. On the prophet's `/sam` group, `smw.SyncSeq` stamps the app's feed position on every successful write answer, and `smw.AppHome` refuses a write for an app another prophet decides with `409` and that prophet in `SAuth-Home`.

No request path calls the prophet to authenticate or authorize a read.
