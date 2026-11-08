# Access lookups on `*DB`

`ErrNoAccess` is 403 on writes and a filter on reads, never 500. `ErrInternalServer` is 500.

| Call | SQL | Use when |
|---|---|---|
| `MutationAccess(actor, id, type, req)` | `mutation_access` | Default for every write. One round trip; P0403 maps to `ErrNoAccess`, P0404 to `ErrNotFound` |
| `MutationAccessMany(reqs)` | `resolve_access` per row | Several checks in one handler. A denied row is a nil claim in the set, never an error |
| `GetAccess` | `get_access` | Read only check of the strongest grant |
| `GetDirectAccess` | `get_direct_access` | Closest ancestor grant when `GetAccess` finds none |
| `UserAccess(actor, target, req)` | `get_user_access` | Target is a `samna_user` named by the caller, or the handler needs `*AccessUser` fields. Pass the raw string: external_id, user_id and row id all resolve. Returns the canonical `TargetUserID` |
| `RootAccess(actor, req)` | `get_access` on each root org the actor holds a claim under, in id order; the first that grants answers | Only when the action targets the root org itself. Never for per user data |
| `ScopeAccess(actor, path, req)` | `get_scope_access` | A capability with no object: page gating, a machine client's public read. Empty slice means no grant, not an error |

## Self

`GetAccess`, `GetDirectAccess` and `MutationAccess` short circuit `userID == objectID` with `OBJECT_TYPE_USER` to `selfAccessClaim`, so `MutationAccess(actor, actor, OBJECT_TYPE_USER, req)` works for PutMe, settings and relations with no `user_claim` row.

## Level

Every `AccessClaim` carries `Level`, the level of the claim its grant came through. A composed grant carries the higher level of its two claims; `selfAccessClaim` carries 0. `Level` is precedence, never an access mask.

## ScopeAccess

1. The path is a jsonpath starting with `$`: `$.page`, `$.page."time-report"`, `$.page.keyvalue() ? (@.key like_regex "...")`. A bare key is wrong.
2. Matches `claim_object` rows with NULL `object_id` and `object_type` whose `scope` satisfies the path; denies on the same path subtract.
3. Results group per claim; each `ClaimObject` carries its own `Scope`. The caller merges payloads; the middleware imposes no shape.
4. Never read `claim_object` directly for a scope grant.

## Where a call runs

1. The object gates (`get_access`, `get_direct_access`, `mutation_access`, `resolve_access`) may settle a pair, so they run on the primary.
2. Pure reads, `ScopeAccess` included, go through `DB.Read`: replica from `SAUTH_REPLICA_DSN`, repeated on the primary on SQLSTATE `25006`.
3. On a disciple, a gate inside a request that made a routed write first waits until the disciple has applied that write's `SAuth-Seq`, so the handler reads its own write; on timeout it reads anyway.
4. Granted gates add a use to `DB.Usage` (`*ObjectUsage`), flushed through `touch_object_usage` every 10 s, at 1000 keys and on stop. Never write usage columns yourself.

## Actor stamping

1. `sa_created_by` comes only from the returned struct's `UserClaimID`.
2. There is no `sa_updated_by` or `sa_deleted_by`. Updates and deletes are attributed by the `audit` row `sdb.Write` writes in the same transaction.
