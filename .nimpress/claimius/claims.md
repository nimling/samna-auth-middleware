---
gate: samna-developer
layout: doc
title: Claims
description: Claims, grants and bindings, the access bits and their expansion, the effective mask of a binding, cascade, composition and its checks, time windows, actor tokens, choosing a grant and the single object gate, with what each stores and reads.
tags: claim, user_claim, claim_object, sa_access, _expand_access, _effective_access, composition, _check_claim_composition, _best_grant, mutation_access, resolve_access, get_direct_access, P0403, P0404, sa_created_by
lastUpdated: true
---

# Claims

Access is three rows. A `claim` says what may be done, a `user_claim` says who holds it, a `claim_object` says what it covers. A claim on its own grants nothing, and neither does a claim bound to an object and held by nobody.

| Row | Key columns |
|---|---|
| `claim` | `name`, `sa_access`, `inherits`, `level`, `type` (`system`, `user`, `service`, `virtual`), `sa_owner_id`, `sa_root_id` |
| `user_claim` | `user_id`, `claim_id`, `starts_at`, `ends_at`, `reason`, `sa_owner_id` |
| `claim_object` | `claim_id`, `object_type`, `object_id`, `sa_access`, `inherits`, `scope`, `reason`, `ref_id` |

A user holds at most one live grant per claim and app, enforced by `idx_user_claim_unique`.

A grant and a binding live in the app of their claim. On the prophet `user_claim_app_claim_fkey` and `claim_object_app_claim_fkey` reference `claim (app_id, id)`, so a writer acting through one app that grants or binds a claim of another stamps the claim's app. Each row then travels to the app's consumers with its claim, and a disciple, which holds only its app's claims, never receives a grant or a binding whose claim it lacks; see [Sync](./sync.md).

## The access bits

`sa_access` is a bitmask, tested with `(stored & required) = required`. The Go constants are `shared.ClaimAccess`.

| Bit | Value | Constant | Means |
|---|---|---|---|
| owner | 1 | `ACCESS_OWNER` | Full control, including binding and deleting |
| write | 2 | `ACCESS_WRITE` | Create and modify |
| read | 4 | `ACCESS_READ` | View |
| execute | 8 | `ACCESS_EXECUTE` | Trigger actions, consume the resource |
| deny | 16 | `ACCESS_DENY` | The other bits are subtracted; see [Denials](./denials.md) |
| member | 32 | `ACCESS_MEMBER` | Participation. `get_user_access` sets it on the actor's own row and on users who share an object with the actor; `get_scope_access` returns the low four bits only |

`ACCESS_ALL` is 15. Read only is 4, read with write 6, read with execute 12.

### Expansion

`insert_claim` and `update_claim` expand a mask whose deny bit is clear before storing it: owner adds write, read and execute, write adds read and execute, execute adds read. Member stands alone. So 1 stores 15, 2 stores 14, 8 stores 12, 4 stores 4, 33 stores 47. Every claim mask read afterwards already carries the bits it implies.

1. A deny mask is stored as given.
2. A mask with no bit besides deny is refused with SQLSTATE `22023`.
3. `update_claim` treats a NULL or 0 mask and an empty name as unchanged, so a caller patching one field does not wipe the others.

## The effective mask of a binding

`claim_object.sa_access` may be NULL, which means the claim's mask. A binding mask narrows its claim and never widens it: the grant carries the claim mask intersected with the expansion of the binding mask. An owner claim, 15, bound with mask 4 gives 4; a read claim, 4, bound with mask 2 gives 4. The grant bits are the low four bits of the result, and the deny bit in it makes the derivation a deny.
## What a writer may bind

`_check_claim_composition`, a before trigger on `claim_object`, bounds what a writer may hand out. It runs outside replay on an insert, or on an update of the mask, claim or object, of a live binding on an object that is not a claim. It resolves the writer from `sa_created_by` and settles the writer's pair on the object first.

1. The binding is refused with `P0403` when its grant bits exceed what the writer holds on the object. A writer who cannot reach an object cannot grant it away, and one who reads it cannot grant write on it. A deny binding is exempt from this test.
2. The binding is refused with `P0403` when the claim's level is higher than every level the writer holds on the object. This holds for a deny whose level is not 0 as well.

Handing a claim out, `insert_user_claim`, and increasing a claim's level run `_check_claim_level` for the actor in `sa_created_by`: a claim whose level is not 0 needs write on the claim, and the level must not be higher than a deny the actor faces on an object the claim is bound to, so an actor never lifts a deny it meets by handing out a stronger claim.

Where the writer holds less than the binding hands out, these holds are tried in order before refusing:

1. A binding on an object outside the `claimius` schema that the database holds no `object_info` row for, a row of a disciple's own table bound on the prophet, is judged on the writer's pair on the binding's owning organization when the writer holds no pair on the object, and as the row's creator when the calling app vouches the row was created under a live grant of the writer in the binding's app and the owning organization is live: owner, write, read and execute, as the disciple counts its creator. Claimius never resolves such a type: the prophet knows the organization, `_check_app_scope` keeps it linked to the app, and the disciple, which holds the row, computes access on it when the binding arrives by sync.
2. On a prophet, the first live binding of an app that carries none, from a writer whose grant belongs to another app, passes when the writer holds owner, in its own app, on the organization that owns the `samna_app` row. This is how `upsert_app` opens a new app; see [Apps](./apps.md#creating-an-app).
3. The first live binding on a `samna_user` who holds a claim assigned under the binding's owning organization passes when the writer holds owner on that organization in the binding's app. This is how `insert_organization_user` binds a new member's claim.
4. Any other binding in an app other than the system app may still pass on the access the writer holds on the same object in the system app, capped at that access.

## Cascade

A binding reaches the rows below its object when both `claim.inherits` and `claim_object.inherits` are true. The claim flag is the switch for every binding the claim carries; the binding flag narrows it to one object. Either false stops the cascade, which is how a broad claim is bound to one room without reaching anything under it.

## Composition

A `claim_object` whose `object_type` is `claimius.claim` links claim A to claim B. A holder of A gains every binding B carries on an object.

1. The link must inherit and B must inherit.
2. When A and B are at the same level, the composed grant carries the bits both the link and B's binding carry, and a deny on either side makes it a deny.
3. When one claim has the higher level, the composed grant takes that level and that claim decides its bits: the link's mask when A is higher, B's binding mask when B is higher.
4. A composed grant keeps the class of the binding's position. On B's bound object it is class `direct`; where B's binding inherits, it cascades to the descendants with the class of the tree, exactly as B's own grant does. So a deny treats a composed grant as it treats a grant of B; see [Denials](./denials.md).
5. A does not absorb B's grants. Holding A does not make you whoever holds B.
6. Composition is single hop. A link on B is not followed.

`_check_claim_composition` enforces the link outside replay:

1. A link to its own claim is refused.
2. A link whose reverse is live is refused.
3. The writer must hold read on B, after its pair on B is settled, or the link is refused with `P0403`. A holder of B reads B, and so does a holder of a claim bound on B; see [the derivations](./index.md#derivations).
4. When the higher of A's and B's levels is not 0, the writer must hold that level on B, or the link is refused with `P0403`.

The use is reusing a surface at another strength. A Bookings Admin claim binds every meeting room of an organization; an External Vendor claim links to it with mask 4, and every vendor reads the same rooms without a binding copied: one link row, where copying needs a binding row per object of B and stays in step with B only by hand.

## Time windows

`user_claim.starts_at` and `ends_at` bound a grant. Nothing is revoked and no job runs; the reads apply the window.

1. `_best_grant` tests the window stored on every derivation in a pair's `grants`, copied from the grant when the pair settled. A pair whose only grants are outside their window returns no grant from `get_access`, and `get_objects` and `search` drop it unless the reader created the object.
2. A change to the window raises the assignee's assignment area, so the pair is recomputed on its next read.
3. `get_direct_access`, `get_scope_access`, `get_user_access` and `get_claims` filter the window in their own joins.

## Actor tokens

1. `sa_created_by` on every row is a `user_claim.id`: the grant the writer acted under, not the person. Authorship survives `merge_user`, and the record names the claim, the owning organization and the window that made the write legal.
2. `get_access`, `get_direct_access`, `mutation_access`, `get_objects` and `search` return a `user_claim_id`, the grant chosen for the row. A follow on write stamps it into `sa_created_by`.
3. The creator of a row reaches it through its own `sa_created_by`, `direct_grant` on the pair, even with no binding at all, while its owning organization is in the ownership closure. A claim owned under a soft deleted organization is then reached only by its holders and its bindings, in `get_objects`, `get_access` and `mutation_access` alike.
4. Listings and graphs are no authority to act on a row in them. To act, call `mutation_access` for that row.
5. There is no `sa_updated_by` or `sa_deleted_by`. Updates and deletes are attributed in `audit`.

## Choosing the grant

`_best_grant(grants, required)` returns, among the derivations of a pair whose mask covers `required` and whose stored window holds at `now()`, the one with the most bits set, then the highest mask, then the highest level, then the oldest claim by its stored `claim_created_at`. Every access row carries the level and the claim name of the grant it chose. It reads nothing beyond the pair: the grant id, the window, the claim name and its creation time are on the derivation, and every write that can change them raises an area covering the pair.

`get_direct_access` orders its candidates by the fewest hops from the object, then the most bits, then the grant id.

## The single object gate

`mutation_access(user, object_id, object_type, required)` is the gate every write and every read of one object passes. It answers in one round trip.

1. It takes the object id as text and refuses with `P0404` a malformed id, or a type absent from `table_info` other than `claimius.audit` and `claimius.user_relation`. A `claimius.samna_user` target is exempt from both tests: it may be named by external id, user id or row id.
2. It resolves through `resolve_access`: `claimius.audit`, `claimius.user_relation` and `claimius.samna_user` go to their own lookups, every other type to `get_access`, which settles and reads the pair in one pass and walks the ancestors through `get_direct_access` only when the settle was skipped on a lock.
3. On a miss it probes the object's live row to tell the two refusals apart.

| Outcome | Answer |
|---|---|
| A grant covers `required` | One row, the chosen grant, with its `user_claim_id` |
| The object exists and no grant covers `required` | SQLSTATE `P0403` |
| The object does not exist or is soft deleted | SQLSTATE `P0404` |

`get_direct_access` answers without `user_object`, for an object whose pair gives no grant after a settle that could not run. It walks the ownership, location and parenthood chains of the object and, for each location on them, its owner's ownership chain; finds the reader's live grants in their window bound on each ancestor, held directly or through one link, where a binding above the object itself counts only when claim and binding both inherit; subtracts the unscoped denies by level and by class, those on the object from every candidate and those on an ancestor from candidates found on an ancestor; closes the remainder; and takes the first candidate by hops, bits and grant id.

## Scope grants

A `claim_object` with no object pair and a `scope` payload is a capability: a page, a feature, a public subset for a machine client; see [Capabilities](./capabilities.md). A scope on an object binding is a different thing: its keys are field names of the object; see [Redaction](./redaction.md).

## Removing a claim

`delete_claim(claim, actor)` soft deletes, in order, the links pointing at the claim, its grants, its bindings, and the claim, and returns the deleted claim row. Each soft delete fires the triggers of its table, which raise the object area of each binding's object, the assignment area of each grant's user and, for the claim itself, one object area per binding and one on the claim object, and settle the actor's pairs on the claim and on each binding's object. The holders settle on their own next reads; see [Materialization](./materialization.md).

## Math

### Rows against reach

The rows grow with the grants and the bindings, never with their product: one claim bound to `n` objects and granted to `k` users is `1 + n + k` rows, where the access they give is up to `k |T_c|` pairs.

### Masks

Expansion is `X(m)` of [the masks](./index.md#masks), applied when the deny bit is clear. The mask a grant carries through one binding is

```
eff(c, b) = m_c & X(coalesce(b, m_c))
```

so `4 & X(2) = 4 & 14 = 4`. The grant bits are `eff & 15`, and `eff & 16` makes the derivation a deny.

A binding is refused when

```
eff(c, b) & ~A(writer, y) & 15 ≠ 0
```

with `A(writer, y) = 0` when the writer holds no pair, and `A = 15` when one of the first binding holds on an organization applies.

### Cascade reach

```
T_c = { y : bind(c, y) }  ∪  { x : bind(c, y, b, true), claim(c, _, true), anc_t(x, y, h), h > 0 }
|T_c| ≤ n_c + Σ_{inheriting bindings y} |desc(y)|
```

### Composition

At the same level:

```
comp(A, l, B, b) = (eff(A, l) & eff(B, b) & ~16) | ((eff(A, l) | eff(B, b)) & 16)
```

At different levels the higher claim's `eff` alone: `eff(A, l)` when A is higher, `eff(B, b)` when B is higher. A link replaces `n_B` copied binding rows with one.

## Cost

| Operation | Reads |
|---|---|
| Expansion | once per claim write |
| Cascade reach of a claim | one range of `idx_claim_object_claim_id` and one range of `idx_inheritance_info_ancestor` per inheriting binding |
| The bind check | the writer's grant, the point settle of one pair, one range of the writer's pairs on the object, read once more for the highest level the writer holds there |
| `_check_claim_level` on a grant or a level increase | the settle of the actor's pair on the claim, one probe for write on it, and one range of the claim's bindings for the denies the actor faces, only for a claim whose level is not 0 |
| A first binding hold, or the system app hold | one pair settle and one probe of the writer's pair, tested only when the plain rule would refuse |
| A binding on a foreign row | one extra probe of `object_info` and the settle of one pair |
| A link | one probe for the reverse link, one settle and one probe of the writer's `user_object` row on `(claimius.claim, B)` |
| `_best_grant` | nothing beyond the pair |
| A time window | nothing beyond the pair |
| `delete_claim` | four updates |

### What the gate reads

With nothing raised in the object's root since the mark:

```
entries(get_access) = 1 + 1 + 1 + 1 + 1 + 2 + 0 = 8
```

one `check_user_active`, one probe of the reader's `samna_user` row for the guest type that caps a guest at read, one pair on the `user_object` primary key with its mark, one `object_info` row with the owner, location and owner root, the trimmed position, the two empty ranges of the areas of the root and of the reader's assignments since the mark, and the grant chosen from the pair with no probe. With something raised in the root since the mark the check also reads the areas and the object's closure range, and moves the mark; only an area on the object, with cascade on an ancestor, or an assignment of the reader recomputes the pair.

With `a'_x` ancestors on the chains of the walk and `B'_x` bindings on them:

```
entries(get_direct_access) ≈ 1 + 2C + 3 a'_x + 3 B'_x + O(1)
```

A hit of the gate is one check. A miss is one check and one probe of the object's live row, the walk added only when the settle was skipped.

## Proofs and measurements

### Why single hop

Single hop keeps the access relation nonrecursive: the claims that can reach an object are the claims bound on it and its ancestors plus the claims linking to those, found with one probe per binding. A transitive hierarchy makes a grant's reach a graph search whose depth depends on the data; see [Types](./types.md#why-the-model-stays-below-the-reachability-bound).

### Measurements

1. `check_mark` suite: a check with nothing raised since the mark reads 7 index entries on the prophet and 4 on a disciple; after an unrelated raise in the same root it reads 15 and 10 and moves the mark without a recompute.
2. `grant_fields` suite: closing the window on a stored pair makes the check answer nothing and the listing return nothing, with no row written and no probe of the grant or the claim.

**Sources.** Validity periods with no revocation: [RFC 5280](https://www.rfc-editor.org/rfc/rfc5280).
