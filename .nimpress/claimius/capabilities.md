---
gate: samna-developer
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

It is written like any binding: `insert_claim`, then `insert_claim_object` with a NULL object pair and the `scope` payload, then `insert_user_claim`. The partial index `idx_claim_object_no_object` holds exactly these rows. A scope grant has no object, so it enters no tree, no `user_object` row and no settle share.

## Matching

`get_scope_access(p_user_id, p_path, p_required_access)` is one `STABLE` statement:

1. It takes the user's live grants inside their window, joined to their live claims.
2. It takes each claim's live scope bindings and keeps those where `jsonb_path_exists(scope, path)`. The argument is a jsonpath and starts with `$`: `$.page` matches any page grant, `$.page."time-report"` one page, `$.page.keyvalue() ? (@.key like_regex "report")` a pattern ([PostgreSQL, SQL/JSON path language](https://www.postgresql.org/docs/17/functions-json.html#FUNCTIONS-SQLJSON-PATH)).
3. Each matching binding carries the claim mask narrowed by the binding mask; a NULL binding mask is the claim mask.
4. Each matching deny binding subtracts its bits from every matching grant binding whose claim level is not higher than the deny's. What remains is closed under implication. A scope binding on a claim reached through one inheriting link matches too, with the composed mask and the higher of the two levels.
5. A binding left with no bit, or whose bits do not cover `required`, is dropped.

The answer is one row per surviving binding, with its grant, its claim, its settled mask and level, and the binding's own id, `scope`, mask and `inherits`, ordered by claim and binding. Deny bindings are never returned. An empty answer is a normal no.

It reads no `user_object`, runs no `reconcile_if_pending` and settles nothing, so a scope grant takes effect on the next call, and the call runs on a read only standby. It has one form, the same on both roles.

The scope key form `get_access(p_user_id, p_scope_key, p_required_access)` checks the user active and returns the one binding of `get_scope_access` at the path `$."scope_key"` with the most bits set, then the lowest claim id: `user_claim_id`, `claim_id`, `sa_access` and the binding's `scope`.

## The boundary

Claimius matches the path and the mask. It never interprets the keys. What lives under `page`, `file`, `stats` or any other top level key is defined by the implementing side, and two products use different shapes in one schema without agreeing on anything. That is also the limit: a capability answers what a caller may do in general, never which rows. Anything with an id is an object binding, where the trees and the deny classes apply.

## How an implementing side uses it

1. Page gating. Each page slug is a key under `page`, with `_access` at each node and sub functions nested below. One call with `$.page` returns every page grant of the user, which the implementing side merges into a slug map for navigation and routing.
2. Server side functional checks. A handler that performs a gated function asks for the specific path at the bits it needs, for example `$.page."time-approve"` at execute before approving a timesheet.
3. Machine clients. A client's capability, such as reading only the public subset of a resource, is a scope grant on a claim the client holds, matched the same way.
4. Denying a capability. A deny claim bound with a scope under the same path removes its bits from every grant that path matches whose level is not higher than the deny's, for the users who hold it.

## Math

For one call, over the matching bindings:

```
e          = eff(c, b)
X_p(l)     = ⋁ { e' & 15 : matching deny binding, e' & 16 ≠ 0, level' ≥ l }
a(binding) = close(e & 15 & ~X_p(level))      kept when a ≠ 0 and a & required = required
```

## Cost

With `C` live grants of the user and `β` scope bindings on their claims:

```
index entries read    = 1 + 2C + β + L + 2L'
jsonpath evaluations  = β
rows written          = 0
```

one range of `idx_user_claim_user_app`, `C` claim probes, one probe of `idx_claim_object_no_object` per claim, and for the links out of the held claims one range of `idx_claim_object_claim_id` per held claim, `L` links, with one claim probe and one `idx_claim_object_no_object` probe per linked claim, `L'` of them. The cost is the same whether the app has ten users or a million.

## Proofs and measurements

**Sources.** A nested grant, a page with sub functions each at their own strength, is a structured object matched by shape, the model [RFC 9396](https://www.rfc-editor.org/rfc/rfc9396) defines for rich authorization requests; `jsonb_path_exists` evaluates it inside the query that already filters the grants.
