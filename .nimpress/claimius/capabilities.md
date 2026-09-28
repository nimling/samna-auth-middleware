---
layout: doc
title: Capabilities
description: Scope grants, the claim_object bindings with no object pair that carry page and functional access, how they are stored and matched, what a match reads, and where claimius stops and the implementing side begins.
tags: scope, ScopeAccess, jsonpath, page access, capability, claim_object, idx_claim_object_no_object, machine client
lastUpdated: true
---

# Capabilities

Not every permission has a row behind it. Opening a page, approving timesheets, exporting a report, a machine client reading the public subset of a resource: each is a capability, not access to an object. Claimius carries capabilities as scope grants.

## The row

A scope grant is a `claim_object` whose `object_id` and `object_type` are both NULL and whose `scope` jsonb carries what is granted. The check constraint `claim_object_object_pair` admits exactly this shape and the object shape.

```json
{ "page": { "time-report": { "_access": "READ|EXECUTE", "export": { "_access": "READ" } } } }
```

It is written like any binding: `create_claim`, then `assign_claim_object` with a NULL object pair and the `scope` payload, then `assign_claim_user`. The partial index `idx_claim_object_no_object (claim_id) WHERE object_id IS NULL AND sa_deleted_at IS NULL` holds exactly these rows. A scope grant has no object, so it enters no tree, no `user_object` row and no area: the recompute reads only bindings with an object.

## Matching

The match is one statement, issued by `ScopeAccess` in the middleware:

1. The actor's live `user_claim` rows inside their window, one range of `idx_user_claim_user_app`, `C` rows, joined to their live claims, `C` primary key probes.

2. Each claim's scope bindings, one probe of `idx_claim_object_no_object` per claim, `b` rows in all.

3. `jsonb_path_exists(scope, path)` per binding. The argument is a jsonpath and starts with `$`: `$.page` matches any page grant, `$.page."time-report"` one page, `$.page.keyvalue() ? (@.key like_regex "report")` a pattern ([PostgreSQL, SQL/JSON path language](https://www.postgresql.org/docs/17/functions-json.html#FUNCTIONS-SQLJSON-PATH)).

4. The mask is `(claim.sa_access | claim_object.sa_access) & 15` and must cover the required bits. The binding mask is part of the sum, so a scope binding with a NULL mask matches nothing. A binding whose mask carries the deny bit is excluded from the answer; it does not subtract from other bindings.

5. The answer groups by claim, with one entry per matching binding carrying its own `scope`, mask and `inherits`. An empty answer is a normal no.

```
index entries read = 1 + 2C + b          jsonpath evaluations = b
```

It reads no materialized table, so a scope grant takes effect on the next call with no settle, and costs the same whether the app has ten users or a million.

In SQL the same match by top level key is `get_access(user, app, scope_key, required)`: after the read preamble of [Reads](./reads.md#what-every-read-pays-first), the user's live grants in the app joined to their scope bindings whose `scope` has the key, `scope ? key`, with the same mask and deny rule, and the binding with the most bits set as the one row.

## The boundary

Claimius matches the path and the mask. It never interprets the keys. What lives under `page`, `file`, `stats` or any other top level key is defined by the implementing side, and two products use different shapes in one schema without agreeing on anything. That is also the limit: a capability answers what a caller may do in general, never which rows. Anything with an id is an object binding, where the trees and the deny rules apply.

## How an implementing side uses it

1. Page gating. Each page slug is a key under `page`, with `_access` at each node and sub functions nested below. One call with `$.page` returns every page grant of the user, which the implementing side merges into a slug map for navigation and routing.

2. Server side functional checks. A handler that performs a gated function asks for the specific path at the bits it needs, for example `$.page."time-approve"` at execute before approving a timesheet.

3. Machine clients. A client's capability, such as reading only the public subset of a resource, is a scope grant on a claim the client holds, matched the same way.

Why jsonb matched by jsonpath: a nested grant, a page with sub functions each at their own strength, cannot be expressed in a flat scope string without a naming convention inside the string. A structured object matched by shape is the model [RFC 9396](https://www.rfc-editor.org/rfc/rfc9396) defines for rich authorization requests, and `jsonb_path_exists` evaluates it inside the query that already filters the grants.
