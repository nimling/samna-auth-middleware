---
gate: auth-middleware
layout: doc
title: Access
description: The access lookups a handler calls before it reads or writes, what each one returns, which SQL function answers it, and how listings page.
tags: MutationAccess, UserAccess, RootAccess, ScopeAccess, AccessClaim, get_access, mutation_access
lastUpdated: true
---

# Access

A handler never decides access in its own code. It asks the database through one of the lookups on `sdb.Instance()`, and the answer is an `AccessClaim` carrying the grant the actor holds. That grant's `UserClaimID` is what a follow on write stamps into `sa_created_by`. A lookup on one object reads the actor's settled pair in `user_object`, after settling that one pair when a change the actor has not yet read covers it; see [Reads](./claimius/reads.md).

| Lookup | Returns | Answers through | Use for |
|---|---|---|---|
| `MutationAccess(ctx, actor, objectID, objectType, required)` | `*AccessClaim` | `claimius.mutation_access` | Every write, and every read of one object |
| `MutationAccessMany(ctx, requests)` | `AccessClaimSet` | `claimius.resolve_access` per row | Several objects in one round trip |
| `UserAccess(ctx, actor, target, required)` | `*AccessUser` | `claimius.get_user_access` | Acting on a user named by external id, user id or row id |
| `RootAccess(ctx, actor, required)` | `*AccessClaim` | `claimius.get_access` on the actor's root | Actions on the actor's own root organization |
| `GetAccess` | `*AccessClaim` | `claimius.get_access` | The grant on the object itself |
| `GetDirectAccess` | `*AccessClaim` | `claimius.get_direct_access` | The closest grant on any ancestor |
| `ScopeAccess(ctx, actor, jsonpath, required)` | `[]AccessClaim` | `claimius.get_scope_access` | Capabilities with no object, such as page grants |

## Status codes come from the database

`mutation_access` raises `P0403` when the object exists and the actor lacks the mask, and `P0404` when the object does not exist. The lookup maps them to `ErrNoAccess` and `ErrNotFound`, so one round trip answers 200, 403 or 404. `UserAccess` does the same for a user target.

`MutationAccessMany` never raises. A denied or unknown row comes back as a nil entry in the set.

`ScopeAccess` returns an empty slice when nothing matches. That is an answer, not an error.

## The self case

`MutationAccess(actor, actor, OBJECT_TYPE_USER, required)` succeeds without a database call. No `user_claim` binds a user to their own row, so the lookup synthesizes the grant.

## Delegation

A route that names a user acts for that user without becoming them. `UserAccess` resolves the actor's right to act on the target and returns the grant in the same call, so the row written carries the actor's grant in `sa_created_by` and the target's `user_id` as its subject. See [Users](./claimius/users.md).

## Listings

A listing filters through the materialized index. `ObjectWhere(q, actor, objectType, required, objectID, after, limit)` takes the handler's own `*bun.SelectQuery` and returns it restricted to the rows the actor reaches with `required`: a page of `limit` rows past the id `after` reads `limit` entries of the actor's range and is ordered by id, the order its cursor continues in, and `after` nil with `limit` 0 reads the whole range and keeps the order the handler gave. A query built with `Table` or `TableExpr` and no model names its table, or the alias after `AS`, in the filter. With `objectID` set it is the check of that one row. `GetOrganizations`, `GetLocations` and `GetSecrets` take the same `after` and `limit`, and `QueryPage(ctx)` reads them from the `cursor` and `limit` query parameters. `UserWhere` and `ClaimWhere` return a predicate for users and claims. A listing never returns a `UserClaimID`, because a listing is no authority to act on any row in it. To act on one row, call `MutationAccess` for that row.

## Graphs

`GetOwnerGraph(ctx, actor, startID, depth, required, window)` and `GetClaimGraph(ctx, actor, startID, depth, required, window)` return flat nodes, `object_id`, `object_type`, `label`, `parent_id` and a `data` payload with the access mask and depth. `window` is a `GraphWindow{Width, Sort, NextID}`: at most `Width` children of each type per parent, 0 meaning all, in name order or by the actor's decayed usage with `GRAPH_SORT_USAGE`, continuing after `NextID`. The graphs cover the shared claimius types and every table registered with `upsert_table_info`, read from `table_info`.

## Usage

Every granted single object lookup, `MutationAccess`, `GetAccess`, `UserAccess` and each granted entry of `MutationAccessMany`, counts one use of the object by the actor in process, keyed per app: `DB.Usage.Add(appID, actor, objectType, objectID)` with a nil `appID` for the deployment's own app, which is what the lookups pass. The counts flush to `claimius.touch_object_usage` every 10 s, at 1,000 distinct keys and on shutdown, in the order the pairs are locked, retrying a deadlock and keeping a failed batch for the next flush, and order the `usage` graph window and search. See [Access in Go](./claimius/middleware/access.md#usage).

## Where a lookup runs

The lookups that can settle a pair run on the primary. With `SAUTH_REPLICA_DSN` set, `ScopeAccess`, the client credential lookup and the child and parent walks run on the standby and are repeated on the primary when the standby refuses them. See [Access in Go](./claimius/middleware/access.md#where-a-call-runs).
