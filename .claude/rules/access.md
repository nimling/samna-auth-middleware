# Access functions

The middleware exposes seven access lookup entry points on `*DB`. Pick the right one for the call site; the differences matter.

Six of them answer "what may this actor do to this object" and return either a populated access struct or `ErrNoAccess` on miss. `ScopeAccess` is the seventh and answers a different question, "what has this actor been granted that is not bound to any object", and returns an empty slice rather than an error on miss. `ErrInternalServer` indicates a DB-level fault and should map to 500 in the handler.

| Function | Returns | SQL function called | Self case | Use when |
|----------|---------|---------------------|-----------|----------|
| `GetAccess` | `*AccessClaim` | `claimius.get_access` | YES — `userID == objectID && objectType == OBJECT_TYPE_USER` returns `selfAccessClaim(userID, requiredAccess)` without a DB round-trip | You need the strongest single grant the actor has on a specific object. Read-only check. |
| `GetDirectAccess` | `*AccessClaim` | `claimius.get_direct_access` | YES — same self-case, returns `selfAccessClaim(userID, ACCESS_ALL)` | You need to find the closest ancestor with a direct `user_claim`. Fallback when `GetAccess` returns nothing but the actor still expects to act. |
| `MutationAccess` | `*AccessClaim` | `claimius.get_access` then `claimius.get_direct_access` | YES via either branch | Standard call site for any write-side handler. Tries `GetAccess` first; on miss, falls through to `GetDirectAccess` and verifies the returned access mask covers `requiredAccess`. |
| `MutationAccessMany` | `AccessClaimSet` | batched | YES per-entry | Batched variant of `MutationAccess` — single round-trip for multiple `(objectID, objectType)` checks. Use when the handler needs more than one access verdict and would otherwise issue N sequential `MutationAccess` calls. |
| `UserAccess` | `*AccessUser` | `claimius.get_user_access` | YES — the SQL function handles `actor == target` internally | The call is specifically "actor can act on this `samna_user` row". The return shape is user-specific (carries user fields, not claim fields). |
| `RootAccess` | `*AccessClaim` | `claimius.get_access` against the actor's root org | n/a (no object id parameter) | The action is at root-organization level. Looks up the actor's first non-deleted `user_claim`, takes its `sa_root_id`, and checks `get_access` on that org with `OBJECT_TYPE_ORGANIZATION`. Use ONLY when the action targets the root org itself. |
| `ScopeAccess` | `[]AccessClaim` with `Objects` populated | none — a raw join over `user_claim`, `claim`, `claim_object` | n/a (no object id parameter) | The grant is a capability, not an object. Matches `claim_object` rows with a NULL `object_id` whose `scope` jsonb satisfies the given jsonpath. Empty slice means no grant. |

## ScopeAccess

`ScopeAccess(ctx, userID, scopePath, requiredAccess)` is how a capability that has no row to point at is granted. The binding is a `claim_object` with both `object_id` and `object_type` NULL, carrying a `scope` jsonb payload. The consuming service defines what the keys inside `scope` mean; claimius only matches the path and the access mask.

The SQL filters on all of: the `user_claim` is live and inside its `starts_at`/`ends_at` window, the claim and the binding are not soft deleted, `co.object_id IS NULL`, `jsonb_path_exists(co.scope, $path)`, the deny bit `0x10` is clear on the union of `claim.sa_access | claim_object.sa_access`, and that union covers `requiredAccess`.

The path is a jsonpath, not a key or a regexp. `$.page` matches any page grant. `$.page."time-report"` narrows to one. `$.page.keyvalue() ? (@.key like_regex "...")` filters by pattern.

Results group by claim. Each returned `AccessClaim` carries `UserClaimID`, `ClaimID`, `ClaimName`, the unioned `Access`, `OwnerID`, `RootID`, and one `ClaimObject` per matching binding with its own `Scope`, `Access` and `Inherits`. A caller that needs the payload reads `Objects[i].Scope` and merges across claims itself; the middleware does no merging and imposes no shape on the json.

Two grants that ride this path in practice: page and navigation gating keyed by page slug, and machine client capabilities such as an api client that may read only the public subset of a resource.

## Why the self case matters

`GetAccess` and `GetDirectAccess` both short-circuit when `objectType == OBJECT_TYPE_USER` and `userID == objectID`:

```go
if objectType == OBJECT_TYPE_USER && userID == objectID {
    return selfAccessClaim(userID, requiredAccess), nil
}
```

The synthesized `*AccessClaim` carries `UserObjectID = userID`, `Access = int(requiredAccess)`, `OwnerID = userID`, `RootID = userID`, `DirectGrant = true`. This is why `MutationAccess(actor, actor, OBJECT_TYPE_USER, ACCESS_WRITE)` succeeds for self-writes even though no `user_claim` row binds a user to their own `samna_user` row in the database.

## Picking the call site

- Action targets some object the user has been granted access to (org, location, bookable, claim, asset, etc.) → `MutationAccess(actor, objectID, objectType, requiredAccess)`.
- Action targets the actor's own user row (PutMe, PatchMe, settings, relations) → `MutationAccess(actor, actor, OBJECT_TYPE_USER, requiredAccess)`. The self-case shortcut returns a valid synthesized claim.
- Action needs the matching `*AccessUser` (because the handler later uses user-shaped fields, not claim-shaped) → `UserAccess(actor, targetUserID, requiredAccess)`.
- Action targets the root org and you do not want to require the caller to also have access at any sub-org → `RootAccess(actor, requiredAccess)`. Verify this is really what you want; `MutationAccess` on the specific root id is usually the better choice.
- Multiple object checks in one handler → `MutationAccessMany(requests)`.
- Action is gated by a capability with no object behind it, such as a page grant or a machine client's public read scope → `ScopeAccess(actor, jsonpath, requiredAccess)`.

## Anti-patterns

- Using `RootAccess` for per-user operations (bookmarks, settings, relations). Over-broad — `MutationAccess` with `OBJECT_TYPE_USER` is correct.
- Calling `GetAccess` alone on a write path without falling through to `GetDirectAccess`. Use `MutationAccess` so the fallthrough is automatic.
- Using `UserAccess` when you only need to write the row and never read the user-shaped return fields — `MutationAccess` is simpler and returns the same access mask.
- Treating `ErrNoAccess` as 500 — it must surface as 403 on writes and as a filter on reads.
- Treating an empty `ScopeAccess` slice as an error. It is the normal "no such grant" answer and belongs in an `if len(access) > 0` branch.
- Passing a bare key such as `page` to `ScopeAccess`. The argument is a jsonpath and must start with `$`.
- Reading `claimius.claim_object` directly to find a scope grant. `ScopeAccess` is the only supported path.
- Stamping `sa_created_by` from anywhere other than the returned access struct's `UserClaimID`.
- Assuming `sa_updated_by` or `sa_deleted_by` columns exist. They do not. The claimius schema carries `sa_created_by` only; updates and deletes attribute the actor via the `audit` row written by `audit.Write`, never via a per-row column.
