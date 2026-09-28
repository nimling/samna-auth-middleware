---
layout: doc
title: Access
description: The access lookups a handler calls before it reads or writes, what each one returns, and which SQL function answers it.
tags: MutationAccess, UserAccess, RootAccess, ScopeAccess, AccessClaim, get_access, mutation_access
lastUpdated: true
---

# Access

A handler never decides access in its own code. It asks the database through one of the lookups on `sdb.Instance()`, and the answer is an `AccessClaim` carrying the grant the actor holds. That grant's `UserClaimID` is what a follow on write stamps into `sa_created_by`.

| Lookup | Returns | Answers through | Use for |
|---|---|---|---|
| `MutationAccess(ctx, actor, objectID, objectType, required)` | `*AccessClaim` | `claimius.mutation_access` | Every write, and every read of one object |
| `MutationAccessMany(ctx, requests)` | `AccessClaimSet` | `claimius.resolve_access` per row | Several objects in one round trip |
| `UserAccess(ctx, actor, target, required)` | `*AccessUser` | `claimius.get_user_access` | Acting on a user named by external id, user id or row id |
| `RootAccess(ctx, actor, required)` | `*AccessClaim` | `claimius.get_access` on the actor's root | Actions on the actor's own root organization |
| `GetAccess` | `*AccessClaim` | `claimius.get_access` | The grant on the object itself, read only |
| `GetDirectAccess` | `*AccessClaim` | `claimius.get_direct_access` | The closest grant on any ancestor |
| `ScopeAccess(ctx, actor, jsonpath, required)` | `[]AccessClaim` | A match over scope bindings | Capabilities with no object, such as page grants |

## Status codes come from the database

`mutation_access` raises `P0403` when the object exists and the actor lacks the mask, and `P0404` when the object does not exist. The lookup maps them to `ErrNoAccess` and `ErrNotFound`, so one round trip answers 200, 403 or 404. `UserAccess` does the same for a user target.

`MutationAccessMany` never raises. A denied or unknown row comes back as a nil entry in the set.

`ScopeAccess` returns an empty slice when nothing matches. That is an answer, not an error.

## The self case

`MutationAccess(actor, actor, OBJECT_TYPE_USER, required)` succeeds without a database call. No `user_claim` binds a user to their own row, so the lookup synthesizes the grant.

## Delegation

A route that names a user acts for that user without becoming them. `UserAccess` resolves the actor's right to act on the target and returns the grant in the same call, so the row written carries the actor's grant in `sa_created_by` and the target's `user_id` as its subject. See [Delegation](/libraries/claimius/concepts/delegation).

## Listings

A listing filters through the materialized index with `ObjectWhere(actor, objectType, required)`, `UserWhere` or `ClaimWhere`. A listing never returns a `UserClaimID`, because a listing is no authority to act on any row in it. To act on one row, call `MutationAccess` for that row.

## Graphs

`GetOwnerGraph(ctx, actor, startID, depth, required)` and `GetClaimGraph(ctx, actor, startID, depth, required)` return flat nodes, `object_id`, `object_type`, `label`, `parent_id` and a `data` payload with the access mask and depth. The graphs cover the shared claimius types and every table registered with `init_claimius_tables`, read from `table_info`. A service that wants its own shapes on top wraps `claimius.get_owner_graph`, `claimius.get_claim_graph` and `claimius.search` in its own SQL; the middleware carries no paging window and no usage record for them.
