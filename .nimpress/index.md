---
visibility: hidden
layout: doc
title: Samna Auth Middleware
description: The Go module every Samna service links to reach Samna Auth, in one of two roles, prophet or disciple, over one shared claimius schema.
tags: samna auth middleware, sauth, claimius, prophet, disciple, smw, sdb, access control
lastUpdated: true
---

# Samna Auth Middleware

Samna Auth Middleware is the Go module every service in the Samna stack links to reach [Samna Auth](/solutions/auth/). It authenticates requests, keeps a local copy of the access state in the service's own Postgres, and answers access questions from that copy without a network call. The TypeScript package `@nimling/samna-auth-middleware` ships beside it for the browser half.

**Repository**: [nimling/samna-auth-middleware](https://github.com/nimling/samna-auth-middleware)

## One schema, two roles

Every consuming service runs the [claimius](./claimius/index.md) schema in its own database. A binary links exactly one role.

| Role | Is | Holds | Writes |
|---|---|---|---|
| Prophet | Samna Auth itself | Every app, with key and seed material | Owns every grant and serves the sync feed |
| Disciple | A product service such as Bookable or Nimployer | One app, `app_id` stripped | Sends grant writes to the prophet, owns its own tables |
| Hybrid | A prophet set up with `upsert_prophet_peer` | Its own apps, and one app mirrored from a peer prophet | Owns its own apps; the mirrored app's writes belong to the peer |

The prophet is the authority, and each app has one home prophet that decides its writes. A disciple is a mirror that answers the same reads from its own database, relies on exactly one prophet, and settles access for the tables only it carries. See [Disciple](./disciple.md) for how writes and sync move between the two, and [Prophets and disciples](./claimius/prophets.md) for federation.

## Packages

| Package | Prophet import | Disciple import | Holds |
|---|---|---|---|
| `sdb` | `prophet/sdb` | `disciple/sdb` | The database layer, the access lookups, the usage counter and the audit writer |
| `echo` | `prophet/echo` | `disciple/echo` | The HTTP handlers |
| `sam` | `prophet/sam` | `disciple/sam` | Route registration; the pull loop `SyncStart`, and on a prophet the peer pull `PeerSyncStart` |
| `smw` | `smw` | `smw` | Authentication middleware for both roles |
| `client` | `client` | `client` | The HTTP client a disciple reaches the prophet with |
| `database` | `prophet/database` | `disciple/database` | The canonical claimius SQL set for the role |
| `uptime` | | `uptime` | The heartbeat a disciple reports to the prophet |

`shared` carries the implementation both roles use. A consumer never imports it; each role package reexports what it needs.

`cmd/bff` builds `sauth-bff`, a disciple with no tables that fronts a static site with the login and the session; see [BFF](./bff.md). A site with no server of its own logs in against the prophet directly; see [Static sites](./static.md). Every disciple mounts the dependency free `GET /api/status/health` the status probe reads through `sam.RegisterStatus`; see [Health](./health.md).

## Authentication

| Middleware | Accepts | Mount on |
|---|---|---|
| `smw.UserAuthMiddleware(opts)` | Bearer token, then session cookie, then client credentials | Routes browsers and services both reach |
| `smw.ClientAuthMiddleware()` | Client credentials only | Routes only machines reach |
| `smw.AppMiddleware` | `SAuth-App-Slug` and `SAuth-App-Secret` | The prophet's `/sam` group, where disciples reach the prophet |
| `smw.TelemetryMiddleware()` | Nothing, it opens the server span | The echo instance, before auth |

Tokens are verified locally against the prophet's JWKS, checked for issuer and audience, and bound to a DPoP proof or an mTLS thumbprint when the token carries `cnf`. A machine client is verified against the local `samna_client` row with bcrypt once per credential, and by the digest of the verified secret after, so serving a call never asks the prophet. The actor lands in `ctx.Get(smw.UserIDKey)`.

## Access

A handler asks the database, never its own code, whether an actor may act. See [Access](./access.md).

## Schema delivery

The SQL is not copied into consumers, and a consumer holds none of it on disk. The middleware ships two base sets, `prophet/database` and `disciple/database`, and nothing else for the schema. A consumer's `database/migrate.yml` pulls its role's set as one git include at the same ref its `go.mod` pins, and `smig up` applies it.

```yaml
- name: Claimius Schema
  type: base
  slug: claimius
  git:
    repo: nimling/samna-auth-middleware
    ref: <tag>
    path: disciple/database
```

A prophet uses `prophet/database` in the same step. Moving the ref replays the changed base files through smig's preflight. The base sets bring the claimius tables to the new shape themselves. What a consumer made of its own for the earlier line and must drop is the consumer's own migration, `V2.0.0__claimius_unified.sql` in its own `database/migrations/`; the middleware carries no `migrations/` folder under either role.

## Configuration

`sdb.Init` reads every `SAUTH_` variable once. Consumer code never reads one.

| Variable | Role | Holds |
|---|---|---|
| `SAUTH_ENDPOINT` | both | Base url of Samna Auth; a disciple bound to another prophet under a handover routes there and asks this one to take it over |
| `SAUTH_APP_SLUG` | both | The app slug |
| `SAUTH_APP_SECRET` | disciple | The app secret |
| `SAUTH_REDIRECT_URI` | disciple | The OAuth redirect for the login flow |
| `SAUTH_PAGE_REDIRECT` | disciple | Where the callback lands the browser |
| `SAUTH_CLIENT_TIMEOUT` | disciple | Seconds before a call to the prophet gives up, default 5 |
| `SAUTH_DEBUG_USER_ID` | both | Pins one user and skips authentication |
| `SAUTH_DEBUG_CLIENTS` | both | A static map of client credentials |
| `SAUTH_DEBUG_PRIVATE_SEED` | disciple | The app seed a debug disciple encrypts local secrets with |
| `SAUTH_REPLICA_DSN` | both | A standby the pure reads run on; see [Access in Go](./claimius/middleware/access.md) |

## Debug mode

`SAUTH_DEBUG_USER_ID` or `SAUTH_DEBUG_CLIENTS` turns a disciple into a self contained service. Authentication is bypassed, the pull loop does not start, and every write runs through the local claimius functions instead of the prophet. It exists for local development and hurl runs. It never proves the prophet path, and it is never set in a deployed environment.
