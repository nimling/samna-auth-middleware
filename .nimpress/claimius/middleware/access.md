---
layout: doc
title: Access in Go
description: The access lookups on sdb.Instance(), the claimius function each one calls, how errors map to status codes, the listing filters, the self case, and the usage counter.
tags: MutationAccess, MutationAccessMany, UserAccess, RootAccess, GetAccess, GetDirectAccess, ScopeAccess, ObjectWhere, AccessClaim, usage
lastUpdated: true
---

# Access in Go

A handler never decides access in its own code. It asks the database through a lookup on `sdb.Instance()`, and the answer is an `AccessClaim` carrying the grant the actor holds.

| Lookup | Calls | Returns | Use for |
|---|---|---|---|
| `MutationAccess(ctx, actor, objectID, objectType, required)` | `claimius.mutation_access` | `*AccessClaim`, `ErrNoAccess`, `ErrNotFound` | Every write, and every read of one object |
| `MutationAccessMany(ctx, requests)` | `claimius.resolve_access` per row, one statement | `AccessClaimSet`, nil entries for denied rows | Several objects in one round trip |
| `UserAccess(ctx, actor, target, required)` | `claimius.get_user_access` | `*AccessUser` with the resolved `TargetUserID` | Acting on a user named by external id, user id or row id |
| `RootAccess(ctx, actor, required)` | `claimius.get_access` on the actor's root organization | `*AccessClaim` | Actions on the root itself |
| `GetAccess`, `GetDirectAccess` | `claimius.get_access`, `claimius.get_direct_access` | `*AccessClaim` | The grant on the object, the closest grant above it |
| `ScopeAccess(ctx, actor, jsonpath, required)` | The scope match of [Capabilities](../capabilities.md), parameterized by the actor's app | `[]AccessClaim`, empty for no | Capabilities, page grants |

## Status codes come from the database

`mutation_access` raises `P0403` when the object exists and the actor lacks the mask and `P0404` when it does not exist, so one round trip answers 200, 403 or 404. `ErrInternalServer` is a database fault and maps to 500. `ErrNoAccess` is never a 500: on a write it is a 403, on a read it filters.

## The self case

`MutationAccess(actor, actor, OBJECT_TYPE_USER, required)` and `GetAccess` on the actor's own user row return a synthesized grant without a database call: `UserObjectID`, `OwnerID` and `RootID` are the actor, `DirectGrant` is true. No `user_claim` binds a user to their own row, and none is needed to edit it.

## One round trip for many

`MutationAccessMany` sends four arrays and runs `resolve_access` in a lateral join over `unnest`. A page that shows fifty rows with an edit button checks fifty grants in one statement: `50 × (O(C + D) probes)` on the server, one round trip on the wire.

## Listings

`ObjectWhere(query, actor, objectType, required, after, limit)` puts `table.id IN (SELECT object_id FROM claimius.get_objects(actor, objectType, required, after, limit))` on the implementing side's query, every value a bound parameter, and orders it by `table.id`, so a page of `limit` rows past `after` reads `limit` entries of the actor's range; `after` nil and `limit` 0 pass NULL and read the whole range. `GetOrganizations`, `GetLocations` and `GetSecrets` take the same `after` and `limit`, and `QueryPage` reads them from the `cursor` and `limit` query parameters, so `/me/organization?cursor=<last id>&limit=50` is the next page. `UserWhere`, `ClaimWhere` and `AuditWhere` return a predicate the implementing side puts on its own query. The implementing side's table is joined to the actor's own index range, so a listing is one query and the planner sees both sides. A listing carries no `UserClaimID`; acting on a row in it goes through `MutationAccess`. See [Reads](../reads.md).

## Usage

Every lookup that returns a granted single object, `MutationAccess`, `GetAccess`, `UserAccess` and each granted entry of `MutationAccessMany`, adds one use of `(app, actor, object_type, object_id)` to an in process counter on the `sdb` handle, with the time. The counter flushes through `claimius.touch_object_usage` every 10 seconds, at 1,000 distinct keys, and when the process stops. A crash loses at most the uses since the last flush, a loss usage tolerates and access never sees. With several replicas each flushes its own counts, and the database adds them. See [Usage](../usage.md).

## Where a call runs

Every access call is one statement, sent as a named prepared statement. A service configured with a standby pool sends reads to it. A check never writes and always answers there; a listing that meets an area to settle is refused by the standby with SQLSTATE `25006` and repeated once on the primary. Writes, `WaitForSeq` reads and the usage flush go to the primary. See [Reads](../reads.md) and [Scaling](../scaling.md).
