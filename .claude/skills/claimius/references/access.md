# Access lookups on `*DB`

The Go access calls on `shared.DB`, reached through `sdb.Instance()` on a disciple: `GetAccess` and `GetDirectAccess` in `samna_auth_middleware/shared/claims.go`, `MutationAccess`, `MutationAccessMany`, `ScopeAccess`, `UserAccess`, `RootAccess` and `PostAccessDiffDrain` in `samna_auth_middleware/shared/access.go`.

`ErrNoAccess` is 403 on writes and a filter on reads, never 500. `ErrInternalServer` is 500.

| Call | SQL | Use when |
|---|---|---|
| `MutationAccess(actor, id, type, req)` | `mutation_access` | Default for every write. One round trip; P0403 maps to `ErrNoAccess`, P0404 to `ErrNotFound` |
| `MutationAccessMany(reqs)` | `resolve_access` per row | Several checks in one handler. A denied row is a nil claim in the set, never an error |
| `GetAccess(actor, id, type, req)` | `get_access` | Read only check of the strongest grant |
| `GetDirectAccess(actor, id, type)` | `get_direct_access` | Closest ancestor grant when `GetAccess` finds none; takes no required access and answers the mask it finds |
| `UserAccess(actor, target, req)` | `get_user_access` | Target is a `samna_user` named by the caller, or the handler needs `*AccessUser` fields. Pass the raw string: external_id, user_id and row id all resolve. Returns the canonical `TargetUserID` |
| `RootAccess(actor, req)` | `get_access` on each root org the actor holds a claim under, in id order; the first that grants answers | Only when the action targets the root org itself. Never for per user data |
| `ScopeAccess(actor, path, req)` | `get_scope_access` | A capability with no object: page gating, a machine client's public read. Empty slice means no grant, not an error |
| `PostAccessDiffDrain(ctx, limitPairs int) (int, error)` | `drain_access_diff` | The settle of frequent readers off the request path, up to `limitPairs` pairs; the disciple's sync leader runs it between pulls in `disciple/sam/main.go`, never a handler. Answers the pairs settled and an error naming the failures |

The other exported methods in this subject: `HasAccess(ctx, userID, objectID, objectType, req) bool`, `GetAccess` as a yes or no; `GetPublicClaims(ctx, appID) (PublicClaims, error)`, the anonymous user's live grants from the in process cache, `uuid.Nil` for the disciple's own app; `GetCodeAccess(ctx, value) (*CodeAccess, error)`, the live invite code behind a value or `ErrCodeInvalid`; `GetUserClaims(ctx, userID)`, `GetClientClaims(ctx, clientID)` and `GetObjectClaims(ctx, userID, objectID, objectType)`, the grants of a user, a machine client and the claims bound to an object; `Read(ctx, fn)`, a pure read on the replica repeated on the primary; `WaitForSeq(ctx, target, timeout)`, the wait for the disciple's cursor. Every remaining exported method on `*DB` is a model read or a writer and lives outside this file.

## Self

`GetAccess`, `GetDirectAccess` and `MutationAccess` short circuit `userID == objectID` with `OBJECT_TYPE_USER` to `selfAccessClaim`, `GetDirectAccess` with `ACCESS_ALL`, so `MutationAccess(actor, actor, OBJECT_TYPE_USER, req)` works for PutMe, settings and relations with no `user_claim` row.

## Level

Every `AccessClaim` carries `Level`, the level of the claim its grant came through. A composed grant carries the greater of the link's level and the bound level; `selfAccessClaim` carries 0. `Level` is the claim level that decides between a grant and a deny, the higher one winning and equal levels falling to the classes, never an access mask.

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
