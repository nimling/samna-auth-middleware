# Access functions

The middleware exposes six access lookup entry points on `*DB`. Pick the right one for the call site; the differences matter.

All return either a populated access struct or `ErrNoAccess` on miss. `ErrInternalServer` indicates a DB-level fault and should map to 500 in the handler.

| Function | Returns | SQL function called | Self case | Use when |
|----------|---------|---------------------|-----------|----------|
| `GetAccess` | `*AccessClaim` | `claimius.get_access` | YES — `userID == objectID && objectType == OBJECT_TYPE_USER` returns `selfAccessClaim(userID, requiredAccess)` without a DB round-trip | You need the strongest single grant the actor has on a specific object. Read-only check. |
| `GetDirectAccess` | `*AccessClaim` | `claimius.get_direct_access` | YES — same self-case, returns `selfAccessClaim(userID, ACCESS_ALL)` | You need to find the closest ancestor with a direct `user_claim`. Fallback when `GetAccess` returns nothing but the actor still expects to act. |
| `MutationAccess` | `*AccessClaim` | `claimius.mutation_access` | YES — the object id string parses to the actor's own uuid | Standard call site for any write-side handler. One round trip that resolves the grant and raises when denied: SQLSTATE P0403 maps to `ErrNoAccess`, P0404 to `ErrNotFound`. The object id is a string; for `OBJECT_TYPE_USER` it accepts an external_id, a user_id, or a samna_user.id, resolved in that order. Other object types expect a uuid string. |
| `MutationAccessMany` | `AccessClaimSet` | `claimius.resolve_access` per row | YES per-entry | Batched variant — single round-trip for multiple checks. Each row runs the non raising dispatcher; a denied or unknown row comes back as a nil claim in the set, never an error. Request object ids are strings with the same per type rules as `MutationAccess`. |
| `UserAccess` | `*AccessUser` | `claimius.get_user_access` | YES — the SQL function handles `actor == target` internally | The call is specifically "actor can act on this `samna_user` row". The target is a string accepting an external_id, a user_id, or a samna_user.id, resolved in that order. The return carries the canonical resolved `TargetUserID`. |
| `RootAccess` | `*AccessClaim` | `claimius.get_access` against the actor's root org | n/a (no object id parameter) | The action is at root-organization level. Looks up the actor's first non-deleted `user_claim`, takes its `sa_root_id`, and checks `get_access` on that org with `OBJECT_TYPE_ORGANIZATION`. Use ONLY when the action targets the root org itself. |

## Why the self case matters

`GetAccess` and `GetDirectAccess` both short-circuit when `objectType == OBJECT_TYPE_USER` and `userID == objectID`:

```go
if objectType == OBJECT_TYPE_USER && userID == objectID {
    return selfAccessClaim(userID, requiredAccess), nil
}
```

The synthesized `*AccessClaim` carries `UserObjectID = userID`, `Access = int(requiredAccess)`, `OwnerID = userID`, `RootID = userID`, `DirectGrant = true`. This is why `MutationAccess(actor, actor.String(), OBJECT_TYPE_USER, ACCESS_WRITE)` succeeds for self-writes even though no `user_claim` row binds a user to their own `samna_user` row in the database. `MutationAccess` takes the object id as a string, so its self shortcut parses the string and compares the parsed uuid to the actor.

## SQL gate semantics

`claimius.mutation_access` is the raising single object gate. It resolves through `claimius.resolve_access` and, on a miss, raises P0403 when the object exists and P0404 when it does not, so the Go side maps one round trip to 200, 403, or 404.

`claimius.resolve_access` is the non raising dispatcher. It routes audit, user_relation, and samna_user to their bespoke lookups and every other type through `claimius.get_access` then `claimius.get_direct_access`. `MutationAccessMany` runs it per row in a lateral join, which is why the batch never aborts on a denied row. Do not call it for a single object gate; that is what `mutation_access` is for.

`claimius.get_direct_access` carries a required access mask and a raise flag, both defaulted off, so plain callers get the silent ancestor walk while `mutation_access` delegates its raise to it.

`claimius.get_user_access` owns samna_user identifier resolution: the target arrives as text and resolves across external_id, user_id, and samna_user.id in that order, scoped to the app, non deleted, active. Every samna_user verdict, single or batched, flows through it.

## Picking the call site

- Action targets some object the user has been granted access to (org, location, bookable, claim, asset, etc.) → `MutationAccess(actor, objectID, objectType, requiredAccess)` with the object id as a uuid string.
- Action targets the actor's own user row (PutMe, PatchMe, settings, relations) → `MutationAccess(actor, actor.String(), OBJECT_TYPE_USER, requiredAccess)`. The self-case shortcut returns a valid synthesized claim.
- Action targets a samna_user named by the caller → pass the raw identifier string through; external_id, user_id, and samna_user.id all resolve. Do not uuid parse it first.
- Action needs the matching `*AccessUser` (because the handler later uses user-shaped fields, not claim-shaped) → `UserAccess(actor, target, requiredAccess)` with the same three identifier forms.
- Action targets the root org and you do not want to require the caller to also have access at any sub-org → `RootAccess(actor, requiredAccess)`. Verify this is really what you want; `MutationAccess` on the specific root id is usually the better choice.
- Multiple object checks in one handler → `MutationAccessMany(requests)`.

## Anti-patterns

- Using `RootAccess` for per-user operations (bookmarks, settings, relations). Over-broad — `MutationAccess` with `OBJECT_TYPE_USER` is correct.
- Calling `GetAccess` alone on a write path without falling through to `GetDirectAccess`. Use `MutationAccess` so the fallthrough is automatic.
- Using `UserAccess` when you only need to write the row and never read the user-shaped return fields — `MutationAccess` is simpler and returns the same access mask.
- Treating `ErrNoAccess` as 500 — it must surface as 403 on writes and as a filter on reads.
- Stamping `sa_created_by` from anywhere other than the returned access struct's `UserClaimID`.
- Assuming `sa_updated_by` or `sa_deleted_by` columns exist. They do not. The claimius schema carries `sa_created_by` only; updates and deletes attribute the actor via the `audit` row written by `audit.Write`, never via a per-row column.
