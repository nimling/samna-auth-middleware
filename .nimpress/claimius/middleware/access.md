---
gate: auth-middleware
layout: doc
title: Access in Go
description: The access lookups on sdb.Instance(), the claimius function each one calls, how errors map to status codes, the paged listings, the graph window, the usage counter and where each call runs.
tags: MutationAccess, MutationAccessMany, UserAccess, RootAccess, GetAccess, GetDirectAccess, ScopeAccess, ObjectWhere, QueryPage, GraphWindow, AccessClaim, usage, replica
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
| `ScopeAccess(ctx, actor, jsonpath, required)` | `claimius.get_scope_access` | `[]AccessClaim`, empty for no | Capabilities, page grants; see [Capabilities](../capabilities.md) |

## Status codes come from the database

`mutation_access` raises `P0403` when the object exists and the actor lacks the mask and `P0404` when it does not exist, when its id is malformed or its type is not registered, so one round trip answers 200, 403 or 404. `ErrInternalServer` is a database fault and maps to 500. `ErrNoAccess` is never a 500: on a write it is a 403, on a read it filters. `UserAccess` answers `ErrNotFound` for a target that resolves to no user in the app.

## What a lookup reads

A lookup on one object reads the settled pair of `user_object`. `get_access` first runs `reconcile_if_pending` for that one pair, which recomputes it only when an area still open for the actor covers it, then reads the pair and chooses a grant with `_best_grant`. No lookup evaluates access from the base relations; see [Reads](../reads.md) and [Materialization](../materialization.md).

## The self case

`MutationAccess(actor, actor, OBJECT_TYPE_USER, required)` and `GetAccess` on the actor's own user row return a synthesized grant without a database call: `UserObjectID`, `OwnerID` and `RootID` are the actor, `DirectGrant` is true. `GetDirectAccess` does the same with `ACCESS_ALL`. No `user_claim` binds a user to their own row, and none is needed to edit it.

## One round trip for many

`MutationAccessMany` sends four arrays and runs `resolve_access` in a lateral join over `unnest`. A page that shows fifty rows with an edit button checks fifty grants in one statement and one round trip. A denied or unknown row is a nil entry in the set, never an error.

## Listings

`ObjectWhere(q, actor, objectType, required, objectID, after, limit)` takes the implementing side's `*bun.SelectQuery` and returns it filtered:

The table is `?TableAlias` when the query has a model; a query built with `Table` or `TableExpr` and no model has its table resolved from the query, the alias after `AS` when there is one, so `db.NewSelect().TableExpr("public.asset AS a")` filters on `a.id`.

1. With `objectID` set it adds `<table>.id = objectID` and `EXISTS (SELECT 1 FROM claimius.get_access(...))`, the check of that one object.

2. Otherwise it adds `<table>.id IN (SELECT object_id FROM claimius.get_objects(actor, objectType, required, after, limit))`, every value a bound parameter, and orders by `<table>.id` when `after` or `limit` is set: a page is ordered on the id its cursor continues from, and an unpaged listing keeps the order the caller gave. `GetOrganizationsQuery` and `GetLocationsQuery` order an unpaged listing by id themselves. `get_objects` reads the actor's range of the `user_object` primary key in `object_id` order from the first key past `after`, so a page of `limit` rows reads `limit` entries of that range; `after` nil and `limit` 0 pass NULL and read the whole range.

`GetOrganizations`, `GetLocations` and `GetSecrets`, with their `Query` forms, take the same `after` and `limit`. `QueryPage(ctx)` reads them from the `cursor` and `limit` query parameters, a uuid and a positive integer, so `/me/organization?cursor=<last id>&limit=50` is the next page. `UserWhere`, `ClaimWhere` and `AuditWhere` return a predicate the implementing side puts on its own query. The implementing side's table is joined to the actor's own index range, so a listing is one query and the planner sees both sides. A listing carries no `UserClaimID`; acting on a row in it goes through `MutationAccess`. See [Reads](../reads.md).

## Graphs

`GetOwnerGraph(ctx, actor, startID, depth, required, window)` and `GetClaimGraph(ctx, actor, startID, depth, required, window)`, with their `Query` forms, call `claimius.get_owner_graph` and `claimius.get_claim_graph` and return flat `GraphNode` rows. `window` is a `GraphWindow{Width, Sort, NextID}`: at most `Width` children of each type per parent, 0 meaning all, in `GRAPH_SORT_NAME` order or by decayed usage with `GRAPH_SORT_USAGE`, continuing after `NextID`. An empty `Sort` is name order. `GraphWindow` is exported from `disciple/sdb` beside `GraphNode`, so a disciple builds the window without importing `shared`. See [Graphs](../graphs.md).

## Object types

`ObjectType` carries a type to SQL and back. `ResolveObjectType` validates claimius's own types only: a bare claimius name resolves to `claimius.X`, and a `claimius.X` the schema does not register is refused. Every other type passes through untouched, because a consumer's type is its own metadata. `ObjectType.Validate(known...)` is the opt in check a consumer runs on its own types: schema qualified, and one of `known` when given; the middleware never calls it. The middleware reads a model's type from its `bun` table tag as given, through `ObjectTypeOf`, so a consumer's model carries a qualified tag such as `table:public.bookable`.

## Usage

Every granted single object lookup adds one use of `(actor, object_type, object_id)` to an in process counter on the handle, `DB.Usage`: `MutationAccess`, `GetAccess`, `UserAccess` on the resolved target, and each granted entry of `MutationAccessMany`. `Usage.Add(appID *uuid.UUID, userID uuid.UUID, objectType string, objectID uuid.UUID)` keys the use per app; the lookups pass a nil `appID`, the deployment's own app, and a prophet service that checks for another app passes that app, so the use lands on that app's pair. The counter flushes through `claimius.touch_object_usage` every 10 s, when it holds 1,000 distinct keys, and on `Stop`:

1. The keys go sorted by user, app, type and object, the own app keys through the form without apps and the others through the form with `app_ids[]`, in one statement. The flush records `usage_diff` rows and locks no pair, so it never waits on a read; the user's next listing folds them into the pairs.

2. A flush refused with SQLSTATE `40P01` or `40001` is sent again, up to three attempts.

3. A batch that still fails is added back to the counter and reported, so its counts go with the next flush.

A crash loses at most the uses since the last flush, a loss usage tolerates and access never sees. With several replicas each flushes its own counts, and the database adds them. A use counts only on a settled pair; see [Usage](../usage.md).

## Where a call runs

Every access call is one statement, sent as a named prepared statement through `DB.prepared`, parsed and planned once per connection.

`SAUTH_REPLICA_DSN` opens a second pool, `DB.Replica`. `DB.Read` runs a pure read there and runs it once more on the primary when the replica refuses it with SQLSTATE `25006`. The calls that use it are `ScopeAccess`, the client credential lookup `GetClient`, `GetApp`, and the child and parent walks `GetChildOrganizations`, `GetParentOrganizations`, `GetChildLocations` and `GetParentLocations`. The lookups that can settle, `MutationAccess`, `MutationAccessMany`, `UserAccess`, `RootAccess`, `GetAccess` and `GetDirectAccess`, always run on the primary, because `reconcile_if_pending` writes when an area covers the pair. Writes, `WaitForSeq` reads and the usage flush go to the primary.
