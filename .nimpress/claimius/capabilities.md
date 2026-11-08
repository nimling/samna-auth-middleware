---
layout: doc
title: Capabilities
description: Scope grants, the claim_object bindings with no object pair that carry page and functional access, how get_scope_access matches them by jsonpath with deny subtraction and closure, what a match reads, and where claimius stops and the implementing side begins.
tags: scope, get_scope_access, ScopeAccess, jsonpath, page access, capability, claim_object, idx_claim_object_no_object, machine client
lastUpdated: true
---

# Capabilities

Not every permission has a row behind it. Opening a page, approving timesheets, exporting a report, a machine client reading the public subset of a resource: each is a capability, not access to an object. Claimius carries capabilities as scope grants, the relation `cap(c, s, b)` of [the model](./index.md#base-relations).

## The row

A scope grant is a `claim_object` whose `object_id` and `object_type` are both NULL and whose `scope` jsonb carries what is granted. The check constraint `claim_object_object_pair` admits exactly this shape and the object shape.

```json
{ "page": { "time-report": { "_access": "READ|EXECUTE", "export": { "_access": "READ" } } } }
```

It is written like any binding: `create_claim`, then `assign_claim_object` with a NULL object pair and the `scope` payload, then `assign_claim_user`. The partial index `idx_claim_object_no_object (claim_id) WHERE object_id IS NULL AND sa_deleted_at IS NULL` holds exactly these rows. A scope grant has no object, so it enters no tree, no `user_object` row and no settle share: the derivations read only bindings with an object.

## Matching

`get_scope_access(user, app, path, required)` is one `STABLE` statement:

1. The user's live `user_claim` rows in the app inside their window, one range of `idx_user_claim_user_app`, `C` rows, joined to their live claims, `C` primary key probes.

2. Each claim's live scope bindings, one probe of `idx_claim_object_no_object` per claim, `β` rows in all, kept when `jsonb_path_exists(scope, path)`. The argument is a jsonpath and starts with `$`: `$.page` matches any page grant, `$.page."time-report"` one page, `$.page.keyvalue() ? (@.key like_regex "report")` a pattern ([PostgreSQL, SQL/JSON path language](https://www.postgresql.org/docs/17/functions-json.html#FUNCTIONS-SQLJSON-PATH)).

3. Each matching binding carries `e = eff(c, b)`, the claim mask narrowed by the binding mask; a NULL binding mask is the claim mask.

4. The matching bindings whose `e` carries the deny bit subtract their bits from every other matching binding, and what remains is closed under implication:

```
X_p       = ⋁ { e & 15 : matching, e & 16 ≠ 0 }
a(binding) = close(e & 15 & ~X_p)          kept when a ≠ 0 and a & required = required
```

5. The answer is one row per surviving binding, with its grant, its claim, its settled mask, and the binding's own id, `scope`, mask and `inherits`, ordered by claim and binding. An empty answer is a normal no.

```
index entries read    = 1 + 2C + β
jsonpath evaluations  = β
rows written          = 0
```

It reads no `user_object`, runs no `reconcile_if_pending` and settles nothing, so a scope grant takes effect on the next call with no settle, costs the same whether the app has ten users or a million, and runs on a read only standby. `get_scope_access(user, path, required)` is the same in the deployment's own app, `get_app_id()`.

The scope key form `get_access(user, app, scope_key, required)` checks the user active and returns the one binding of `get_scope_access` at the path `$."scope_key"` with the most bits set, then the lowest claim id: `user_claim_id`, `claim_id`, `sa_access` and the binding's `scope`.

## The boundary

Claimius matches the path and the mask. It never interprets the keys. What lives under `page`, `file`, `stats` or any other top level key is defined by the implementing side, and two products use different shapes in one schema without agreeing on anything. That is also the limit: a capability answers what a caller may do in general, never which rows. Anything with an id is an object binding, where the trees and the deny classes apply.

## How an implementing side uses it

1. Page gating. Each page slug is a key under `page`, with `_access` at each node and sub functions nested below. One call with `$.page` returns every page grant of the user, which the implementing side merges into a slug map for navigation and routing.

2. Server side functional checks. A handler that performs a gated function asks for the specific path at the bits it needs, for example `$.page."time-approve"` at execute before approving a timesheet.

3. Machine clients. A client's capability, such as reading only the public subset of a resource, is a scope grant on a claim the client holds, matched the same way.

4. Denying a capability. A deny claim bound with a scope under the same path removes its bits from every grant that path matches for the users who hold it.

A nested grant, a page with sub functions each at their own strength, is a structured object matched by shape, the model [RFC 9396](https://www.rfc-editor.org/rfc/rfc9396) defines for rich authorization requests, and `jsonb_path_exists` evaluates it inside the query that already filters the grants.
