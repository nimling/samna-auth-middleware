---
layout: doc
title: Capabilities
description: Scope grants, the claim_object bindings with no object pair that carry page and functional access, how they are written, stored and matched by ScopeAccess, and where claimius stops and the implementing side begins.
tags: scope, ScopeAccess, jsonpath, page access, capability, claim_object, machine client, feature
lastUpdated: true
---

# Capabilities

Not every permission has a row behind it. Opening a page, approving timesheets, exporting a report, a machine client reading the public subset of a resource: each is a capability, not access to an object. Claimius carries capabilities as scope grants.

## The row

A scope grant is a `claim_object` whose `object_id` and `object_type` are both NULL and whose `scope` jsonb carries what is granted. The pair constraint admits exactly this shape and the object shape.

```json
{ "page": { "time-report": { "_access": "READ|EXECUTE", "export": { "_access": "READ" } } } }
```

It is written like any binding, in order: `create_claim`, then `assign_claim_object` with a NULL object pair and the `scope` payload, then `assign_claim_user`. From a disciple the three writes go to the prophet through `sdb.NewInsert` with `.Access(ac)` and come back by sync. The index `idx_claim_object_no_object (claim_id) WHERE object_id IS NULL AND sa_deleted_at IS NULL` holds exactly these rows.

## Matching

`ScopeAccess(ctx, actor, jsonpath, required)` answers one question: which of the actor's live grants carry a scope binding that matches this path with at least these bits.

1. The actor's `user_claim` rows in the actor's app, inside their window, not soft deleted, joined to their claims and to the claims' scope bindings through `idx_claim_object_no_object`.

2. The binding's effective mask is `claim.sa_access & expand(coalesce(claim_object.sa_access, claim.sa_access))`, the same rule as an object binding. A NULL binding mask inherits the claim.

3. `jsonb_path_exists(scope, path)` matches. The argument is a jsonpath and must start with `$`: `$.page` matches any page grant, `$.page."time-report"` one page, `$.page.keyvalue() ? (@.key like_regex "report")` a pattern.

4. A deny binding whose path matches subtracts its bits from the grants matching the same path.

5. The answer groups by claim: `UserClaimID`, `ClaimID`, `ClaimName`, the unioned `Access`, `OwnerID`, `RootID`, and one entry in `Objects` per matching binding with its own `Scope`, `Access` and `Inherits`. An empty answer is a normal no, handled in `if len(access) > 0`, never an error.

The cost is `O(C + b)` index probes for a user holding `C` grants with `b` scope bindings, and one jsonpath evaluation per binding. It reads no materialized table, so a scope grant takes effect on the next call with no settle.

## The boundary

Claimius matches the path and the mask. It never interprets the keys. What lives under `page`, `file`, `stats` or any other top level key is defined by the implementing side, and two products use different shapes in one schema without agreeing on anything. That is also the limit: a capability answers what a caller may do in general, never which rows. Anything with an id is an object binding, where the trees and the deny rules apply.

## How an implementing side uses it

Nimployer is the reference user of capabilities.

1. Page gating. Each page slug is a key under `page`, with `_access` at each node as the `ClaimAccess` pipe string and sub functionality nested below. `GET /me/claim/page` calls `ScopeAccess(ctx, user, "$.page", ACCESS_NONE)`, deep merges every returned `scope.page` tree, unions `_access` with `DENY` subtracting, prunes nodes left empty, and returns the slug map. The frontend loads the map once and asks `can(slug)` for navigation and routing.

2. Server side functional checks. A handler that performs a gated function asks for the specific path at the bits it needs, for example `ScopeAccess(ctx, user, $.page."time-approve", ACCESS_EXECUTE)` before approving a timesheet. The frontend gate decides what is shown; this check decides what is permitted.

3. Anchors. A `file` key carries where the caller's file listing starts. It is not access: every listed row still passes `ObjectWhere` on the asset table.

4. Machine clients. A client's capability, such as reading only the public subset of a resource, is a scope grant on a claim the client holds, matched the same way.

Why the payload is jsonb matched by jsonpath: a nested grant, a page with sub functions each at their own strength, cannot be expressed in a flat scope string without inventing a naming convention inside the string. A structured object matched by shape is the model [RFC 9396](https://www.rfc-editor.org/rfc/rfc9396) defines for rich authorization requests, and `jsonb_path_exists` evaluates it inside the query that already filters the grants ([PostgreSQL, SQL/JSON path language](https://www.postgresql.org/docs/17/functions-json.html#FUNCTIONS-SQLJSON-PATH)).
