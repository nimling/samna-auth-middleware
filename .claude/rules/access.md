# Access functions

The middleware exposes seven access lookup entry points on `*DB`. Pick the right one for the call site; the differences matter.

Six of them answer "what may this actor do to this object" and return either a populated access struct or `ErrNoAccess` on miss. `ScopeAccess` is the seventh and answers a different question, "what has this actor been granted that is not bound to any object", and returns an empty slice rather than an error on miss. `ErrInternalServer` indicates a database level fault and should map to 500 in the handler.

| Function | Returns | SQL function called | Self case | Use when |
|----------|---------|---------------------|-----------|----------|
| `GetAccess` | `*AccessClaim` | `claimius.get_access` | YES — `userID == objectID && objectType == OBJECT_TYPE_USER` returns `selfAccessClaim(userID, requiredAccess)` without a DB round trip | You need the strongest single grant the actor has on a specific object. Read only check. |
| `GetDirectAccess` | `*AccessClaim` | `claimius.get_direct_access` | YES — same self case, returns `selfAccessClaim(userID, ACCESS_ALL)` | You need to find the closest ancestor with a direct `user_claim`. Fallback when `GetAccess` returns nothing but the actor still expects to act. |
| `MutationAccess` | `*AccessClaim` | `claimius.mutation_access` | YES — `userID == objectID && objectType == OBJECT_TYPE_USER` | Standard call site for any write side handler. One round trip that resolves the grant and raises when denied: SQLSTATE P0403 maps to `ErrNoAccess`, P0404 to `ErrNotFound`. |
| `MutationAccessMany` | `AccessClaimSet` | `claimius.resolve_access` per row | In SQL: a `samna_user` row resolves through `get_user_access`, which handles `actor == target` | Batched variant — single round trip for multiple checks. Each row runs the non raising dispatcher; a denied or unknown row comes back as a nil claim in the set, never an error. |
| `UserAccess` | `*AccessUser` | `claimius.get_user_access` | YES — the SQL function handles `actor == target` internally | The call is specifically "actor can act on this `samna_user` row". The target is a string accepting an external_id, a user_id, or a samna_user.id, resolved in that order. The return carries the canonical resolved `TargetUserID`. |
| `RootAccess` | `*AccessClaim` | `claimius.get_access` against the actor's root org | n/a (no object id parameter) | The action is at root organization level. Takes the lowest `sa_root_id` among the actor's live claims whose root organization is live, and checks `get_access` on that org with `OBJECT_TYPE_ORGANIZATION`. Use ONLY when the action targets the root org itself. |
| `ScopeAccess` | `[]AccessClaim` with `Objects` populated | `claimius.get_scope_access` | n/a (no object id parameter) | The grant is a capability, not an object. Matches `claim_object` rows with a NULL `object_id` whose `scope` jsonb satisfies the given jsonpath. Empty slice means no grant. |

## ScopeAccess

`ScopeAccess(ctx, userID, scopePath, requiredAccess)` is how a capability that has no row to point at is granted. The binding is a `claim_object` with both `object_id` and `object_type` NULL, carrying a `scope` jsonb payload. The consuming service defines what the keys inside `scope` mean; claimius only matches the path and the access mask.

`ScopeAccess` calls `claimius.get_scope_access(user, path, required)`. It keeps the bindings where the `user_claim` is live and inside its `starts_at`/`ends_at` window, the claim and the binding are not soft deleted, `object_id IS NULL` and `jsonb_path_exists(scope, path)`. Each binding carries the claim mask narrowed by its own, `claim & expand(coalesce(binding, claim))`, so a NULL binding mask means the claim mask. A deny binding matching the same path subtracts its bits from every other binding, and what remains is closed under implication. A row whose mask misses `requiredAccess` is dropped. It reads no `user_object` and settles nothing, so the Go side sends it to the replica when one is configured.

The path is a jsonpath, not a key or a regexp. `$.page` matches any page grant. `$.page."time-report"` narrows to one. `$.page.keyvalue() ? (@.key like_regex "...")` filters by pattern.

Results group by claim. Each returned `AccessClaim` carries `UserClaimID`, `ClaimID`, `ClaimName`, the unioned `Access`, `OwnerID`, `RootID`, and one `ClaimObject` per matching binding with its own `Scope`, `Access` and `Inherits`. A caller that needs the payload reads `Objects[i].Scope` and merges across claims itself; the middleware does no merging and imposes no shape on the json.

Two grants that ride this path in practice: page and navigation gating keyed by page slug, and machine client capabilities such as an api client that may read only the public subset of a resource.

## Why the self case matters

`GetAccess` and `GetDirectAccess` both short circuit when `objectType == OBJECT_TYPE_USER` and `userID == objectID`:

```go
if objectType == OBJECT_TYPE_USER && userID == objectID {
    return selfAccessClaim(userID, requiredAccess), nil
}
```

The synthesized `*AccessClaim` carries `UserObjectID = userID`, `Access = int(requiredAccess)`, `OwnerID = userID`, `RootID = userID`, `DirectGrant = true`. This is why `MutationAccess(actor, actor, OBJECT_TYPE_USER, ACCESS_WRITE)` succeeds for self writes even though no `user_claim` row binds a user to their own `samna_user` row in the database.

## SQL gate semantics

`claimius.mutation_access` is the raising single object gate. It resolves through `claimius.resolve_access` and, on a miss, raises P0403 when the object exists and P0404 when it does not, so the Go side maps one round trip to 200, 403, or 404.

`claimius.resolve_access` is the non raising dispatcher. It routes audit, user_relation, and samna_user to their bespoke lookups and every other type through `claimius.get_access` then `claimius.get_direct_access`. `MutationAccessMany` runs it per row in a lateral join, which is why the batch never aborts on a denied row. Do not call it for a single object gate; that is what `mutation_access` is for.

`claimius.get_direct_access` carries a required access mask and a raise flag, both defaulted off, so plain callers get the silent ancestor walk while `mutation_access` delegates its raise to it. The walk honours `inherits` on both the claim and the binding: a binding above the object counts only when `claim.inherits` and `claim_object.inherits` are both set, each binding narrows its claim mask, and the user's denies subtract by class: a deny bound on the object itself from every candidate, a deny on an ancestor from the candidates found on an ancestor, the remainder closed under implication.

`claimius.get_user_access` owns samna_user identifier resolution: the target arrives as text and resolves across external_id, user_id, and samna_user.id in that order, scoped to the app, non deleted, active. Every samna_user verdict, single or batched, flows through it.

## Usage and where a call runs

`MutationAccess`, `GetAccess`, `UserAccess` and every granted entry of `MutationAccessMany` add one use of `(actor, object_type, object_id)` to the in process counter `DB.Usage`. `Usage.Add(appID *uuid.UUID, userID, objectType, objectID)` keys the use per app; a nil `appID` is the deployment's own app, which is what the lookups pass. It flushes through `claimius.touch_object_usage` every 10 seconds, at 1,000 distinct keys, and on stop, with the keys sorted in lock order, a deadlock or serialization failure retried, and a failed batch kept for the next flush. Never write `used_count`, `last_used_at` or `user_activity` yourself.

The object gates run on the primary: `get_access`, `get_direct_access`, `mutation_access` and `resolve_access` may settle a pair, which writes. Pure reads, `ScopeAccess` among them, go through `DB.Read`, which uses the replica from `SAUTH_REPLICA_DSN` and repeats a read on the primary when the replica refuses it with SQLSTATE `25006`.

## Picking the call site

- Action targets some object the user has been granted access to (org, location, bookable, claim, asset, etc.) → `MutationAccess(actor, objectID, objectType, requiredAccess)`.
- Action targets the actor's own user row (PutMe, PatchMe, settings, relations) → `MutationAccess(actor, actor, OBJECT_TYPE_USER, requiredAccess)`. The self case shortcut returns a valid synthesized claim.
- Action targets a samna_user named by the caller → `UserAccess(actor, target, requiredAccess)` with the raw identifier string; external_id, user_id, and samna_user.id all resolve. Do not uuid parse it first.
- Action needs the matching `*AccessUser` (because the handler later uses user shaped fields, not claim shaped) → `UserAccess(actor, target, requiredAccess)` with the same three identifier forms.
- Action targets the root org and you do not want to require the caller to also have access at any sub organization → `RootAccess(actor, requiredAccess)`. Verify this is really what you want; `MutationAccess` on the specific root id is usually the better choice.
- Multiple object checks in one handler → `MutationAccessMany(requests)`.
- Action is gated by a capability with no object behind it, such as a page grant or a machine client's public read scope → `ScopeAccess(actor, jsonpath, requiredAccess)`.

## Antipatterns

- Using `RootAccess` for per user operations (bookmarks, settings, relations). Overly broad — `MutationAccess` with `OBJECT_TYPE_USER` is correct.
- Calling `GetAccess` alone on a write path without falling through to `GetDirectAccess`. Use `MutationAccess` so the fallthrough is automatic.
- Using `UserAccess` when you only need to write the row and never read the user shaped return fields — `MutationAccess` is simpler and returns the same access mask.
- Treating `ErrNoAccess` as 500 — it must surface as 403 on writes and as a filter on reads.
- Treating an empty `ScopeAccess` slice as an error. It is the normal "no such grant" answer and belongs in an `if len(access) > 0` branch.
- Passing a bare key such as `page` to `ScopeAccess`. The argument is a jsonpath and must start with `$`.
- Reading `claimius.claim_object` directly to find a scope grant. `ScopeAccess` is the only supported path.
- Stamping `sa_created_by` from anywhere other than the returned access struct's `UserClaimID`.
- Assuming `sa_updated_by` or `sa_deleted_by` columns exist. They do not. The claimius schema carries `sa_created_by` only; updates and deletes attribute the actor via the `audit` row written by `sdb.Write` in the transaction of the change, never via a per row column.
