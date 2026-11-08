---
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
| `claim` | `name`, `sa_access`, `inherits`, `type` (`system`, `user`, `service`, `virtual`), `sa_owner_id`, `sa_root_id` |
| `user_claim` | `user_id`, `claim_id`, `starts_at`, `ends_at`, `reason`, `sa_owner_id` |
| `claim_object` | `claim_id`, `object_type`, `object_id`, `sa_access`, `inherits`, `scope`, `ref_id` |

The rows grow with the grants and the bindings, never with their product: one claim bound to `n` objects and granted to `k` users is `1 + n + k` rows, where the access they give is up to `k |T_c|` pairs. `idx_user_claim_unique` holds at most one live grant per claim, user and app.

## The access bits

`sa_access` is a bitmask, tested with `(stored & required) = required`. The Go constants are `shared.ClaimAccess`.

| Bit | Value | Constant | Means |
|---|---|---|---|
| owner | 1 | `ACCESS_OWNER` | Full control, including binding and deleting |
| write | 2 | `ACCESS_WRITE` | Create and modify |
| read | 4 | `ACCESS_READ` | View |
| execute | 8 | `ACCESS_EXECUTE` | Trigger actions, consume the resource |
| deny | 16 | `ACCESS_DENY` | The other bits are subtracted; see [Denials](./denials.md) |
| member | 32 | `ACCESS_MEMBER` | Participation, read by `get_user_access` and scope grants |

`ACCESS_ALL` is 15. Read only is 4, read with write 6, read with execute 12.

### Expansion

`insert_claim` and `update_claim` store `X(m)` of [the masks](./index.md#masks) for a mask with the deny bit clear: owner adds write, read and execute, write adds read and execute, execute adds read. So 1 stores 15, 2 stores 14, 8 stores 12, 4 stores 4, 33 stores 47. A deny mask is stored as given, and a mask with no bit besides deny is refused with SQLSTATE `22023`. `update_claim` treats a NULL or 0 mask and an empty name as unchanged. The expansion runs once per claim write, so every claim mask read afterwards already carries the bits it implies.

## The effective mask of a binding

`claim_object.sa_access` may be NULL, which means the claim's mask. The mask a grant carries through one binding is

```
eff(c, b) = m_c & X(coalesce(b, m_c))
```

A binding mask narrows its claim and never widens it: an owner claim, 15, bound with mask 4 gives 4, and a read claim, 4, bound with mask 2 gives `4 & X(2) = 4 & 14 = 4`. The grant bits are `eff & 15`, and `eff & 16` makes the derivation a deny.

`_check_claim_composition`, a before trigger on `claim_object`, bounds what a writer may hand out. On an insert, or an update of the mask, claim or object, of a live binding on an object that is not a claim and not a deny, it resolves the writer from `sa_created_by`, settles the writer's pair on the object with `reconcile_if_pending(app, writer, type, id)`, and refuses the binding when

```
eff(c, b) & ~A(writer, y) & 15 ≠ 0
```

with `A(writer, y) = 0` when the writer holds no pair. It reads the writer's grant, the settle test of one pair and one range of the writer's pairs on the object. An actor who cannot reach an object cannot grant it away, and one who reads it cannot grant write on it.

Two first bindings are judged on the writer's owner bit on an organization instead, `A = 15` when it holds owner there, because no one can hold a pair on what has no binding yet:

1. On a prophet, the first live binding of an app that carries none, from a writer whose grant belongs to another app, when the writer holds owner, in its own app, on the organization that owns the `samna_app` row. This is how `upsert_app` opens a new app; see [Apps](./apps.md#creating-an-app).

2. The first live binding on a `samna_user` who holds a claim assigned under the binding's owning organization, when the writer holds owner on that organization in the binding's app. This is how `insert_organization_user` binds a new member's claim.

Each costs one pair settle and one probe of the writer's pair on the organization, and is tested only when the plain rule would refuse.

## Cascade

A binding reaches the rows below its object when both `claim.inherits` and `claim_object.inherits` are true. The claim flag is the switch for every binding the claim carries; the binding flag narrows it to one object. Either false stops the cascade, which is how a broad claim is bound to one room without reaching anything under it. The objects a claim reaches are

```
T_c = { y : bind(c, y) }  ∪  { x : bind(c, y, b, true), claim(c, _, true), anc_t(x, y, h), h > 0 }
|T_c| ≤ n_c + Σ_{inheriting bindings y} |desc(y)|
```

read as one range of `idx_claim_object_claim_id` and one range of `idx_inheritance_info_ancestor` per inheriting binding.

## Composition

A `claim_object` whose `object_type` is `claimius.claim` links claim A to claim B. A holder of A gains every binding B carries on an object, with the mask

```
comp(A, l, B, b) = (eff(A, l) & eff(B, b) & ~16) | ((eff(A, l) | eff(B, b)) & 16)
```

so the grant bits are what both the link and the binding carry, and a deny on either side makes the composed derivation a deny.

1. The link must inherit and B must inherit: `link.inherits` and `B.inherits`.

2. A composed grant keeps the class of the binding's position. On B's bound object it is class `direct`; where B's binding inherits, it cascades to the descendants with the class of the tree, exactly as B's own grant does. So a deny treats a composed grant as it treats a grant of B; see [Denials](./denials.md).

3. A does not absorb B's grants. Holding A does not make you whoever holds B.

4. Composition is single hop. The composition rules consume a binding of B, never a link of B, so a link on B is not followed.

`_check_claim_composition` enforces the link, outside replay: a link to its own claim and a link whose reverse is live are refused with one probe of `idx_claim_object_object`, and the writer must hold read on B: after settling the writer's pair on B, one probe of the writer's `user_object` row on `(claimius.claim, B)` with bit 4. A holder of B reads B, and so does a holder of a claim bound on B; see [the derivations](./index.md#derivations).

The use is reusing a surface at another strength. A Bookings Admin claim binds every meeting room of an organization; an External Vendor claim links to it with mask 4, and every vendor reads the same rooms without a binding copied: one link row where copying costs `n_B` binding rows and stays in step with B only by hand.

Single hop is what keeps the access relation nonrecursive: the claims that can reach an object are the claims bound on it and its ancestors plus the claims linking to those, found with one probe per binding. A transitive hierarchy makes a grant's reach a graph search whose depth depends on the data; see [Types](./types.md#why-the-model-stays-below-the-reachability-bound).

## Time windows

`user_claim.starts_at` and `ends_at` bound a grant. The derivations stored in `user_object` ignore the window; the reads apply it:

1. `_best_grant` joins every derivation in a pair's `grants` to its live `user_claim` inside its window, so a pair whose only grants are outside their window returns no grant from `get_access`, and `get_objects` and `search` drop it unless the reader created the object.

2. `get_direct_access`, `get_scope_access`, `get_user_access` and `get_claims` filter the window in their own joins.

Nothing is revoked and no job runs, the way an [RFC 5280](https://www.rfc-editor.org/rfc/rfc5280) validity period works; the cost is two probes per derivation per read, `2 G_x`.

## Actor tokens

1. `sa_created_by` on every row is a `user_claim.id`: the grant the writer acted under, not the person. Authorship survives `merge_user`, and the record names the claim, the owning organization and the window that made the write legal.

2. `get_access`, `get_direct_access`, `mutation_access`, `get_objects` and `search` return a `user_claim_id`, the grant chosen for the row. A follow on write stamps it into `sa_created_by`.

3. The creator of a row reaches it through its own `sa_created_by`, `direct_grant` on the pair, even with no binding at all.

4. Listings and graphs are no authority to act on a row in them. To act, call `mutation_access` for that row.

5. There is no `sa_updated_by` or `sa_deleted_by`. Updates and deletes are attributed in `audit`.

## Choosing the grant

`_best_grant(grants, user, app, required)` returns, among the derivations of a pair whose mask covers `required` and whose grant is live and in its window, the one with the most bits set, then the highest mask, then the oldest claim. It reads per derivation one probe of `idx_user_claim_unique` and one `claim` primary key probe, `2 G_x`. `get_direct_access` orders its candidates by the fewest hops from the object, then the most bits, then the grant id.

## The single object gate

`mutation_access(user, object_id, object_type, required)` is the gate every write and every read of one object passes. It takes the object id as text, refuses with `P0404` a malformed id or a type absent from `table_info`, and resolves through `resolve_access`: `claimius.audit`, `claimius.user_relation` and `claimius.samna_user` go to their own lookups, every other type to `get_access` and then `get_direct_access`.

| Outcome | Answer |
|---|---|
| A grant covers `required` | One row, the chosen grant, with its `user_claim_id` |
| The object exists and no grant covers `required` | SQLSTATE `P0403` |
| The object does not exist or is soft deleted | SQLSTATE `P0404` |

### What it reads

`get_access` settles the one pair it asks about when an open area covers it, and reads the pair; see [Reads](./reads.md). With no open area in the app:

```
entries(get_access) = 3 + 1 + 1 + 1 + 2 G_x + 1 = 7 + 2 G_x
```

three probes for the settle test, one `check_user_active`, one pair on the `user_object` primary key, one `object_info` row, the grant choice, and the owner's root.

`get_direct_access` answers without `user_object`, for an object whose pair gives no grant. It walks `get_ancestors`, the ownership, location and parenthood chains and, for each location on the chain, its owner's ownership chain, `a'_x` ancestors; reads the reader's `C` live grants in their window; finds the bindings on each ancestor held directly, where a binding above hop 0 counts only when claim and binding both inherit, and through one link; subtracts the unscoped denies, those on the object from every candidate and all of them from candidates found on an ancestor; closes the remainder; and takes the first candidate by hops, bits and grant id. With `B'_x` bindings on those ancestors:

```
entries(get_direct_access) ≈ 1 + 2C + 3 a'_x + 3 B'_x + O(1)
```

A hit of the gate is one check. A miss is one check, two walks, the second with `p_raise` set, and one probe of the object's live row that decides between `P0403` and `P0404`. One round trip answers 200, 403 or 404.

## Scope grants

A `claim_object` with no object pair and a `scope` payload is a capability: a page, a feature, a public subset for a machine client; see [Capabilities](./capabilities.md). A scope on an object binding is a different thing: its keys are field names of the object; see [Redaction](./redaction.md).

## Removing a claim

`delete_claim(claim, actor)` soft deletes the links pointing at the claim, then its grants, then its bindings, then the claim: four updates. Each soft delete fires the triggers of its table, which raise the claim area of each row, once per claim and once per assignment, and settle the actor's pairs on the claim and on each binding's object. The holders settle on their own next reads; see [Materialization](./materialization.md).
