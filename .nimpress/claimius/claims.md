---
layout: doc
title: Claims
description: Claims, grants and bindings, the access bits and their expansion, the effective mask of a binding, composition, time windows, actor tokens and the single object gate, with what each costs in rows.
tags: claim, user_claim, claim_object, sa_access, ACCESS_ALL, expansion, composition, mutation_access, P0403, P0404, sa_created_by
lastUpdated: true
---

# Claims

Access is three rows. A `claim` says what may be done, a `user_claim` says who holds it, a `claim_object` says what it covers. A claim on its own grants nothing, and neither does a claim bound to an object and held by nobody.

| Row | Key columns |
|---|---|
| `claim` | `name`, `sa_access`, `inherits`, `type` (`system`, `user`, `service`, `virtual`), `sa_owner_id`, `sa_root_id` |
| `user_claim` | `user_id`, `claim_id`, `starts_at`, `ends_at`, `reason`, `sa_owner_id` |
| `claim_object` | `claim_id`, `object_type`, `object_id`, `sa_access`, `inherits`, `scope`, `ref_id` |

The rows grow with the grants and the bindings, never with their product: one claim bound to `n` objects and granted to `k` users is `1 + n + k` rows, where storing each user's access to each object is `n k` pairs. `idx_user_claim_unique` holds at most one live grant per claim, user and app.

## The access bits

`sa_access` is a bitmask, checked with `(stored & required) = required`. The Go constants are `shared.ClaimAccess`.

| Bit | Value | Constant | Means |
|---|---|---|---|
| owner | 1 | `ACCESS_OWNER` | Full control, including binding and deleting |
| write | 2 | `ACCESS_WRITE` | Create and modify |
| read | 4 | `ACCESS_READ` | View |
| execute | 8 | `ACCESS_EXECUTE` | Trigger actions, consume the resource |
| deny | 16 | `ACCESS_DENY` | The other bits are subtracted, see [Denials](./denials.md) |
| member | 32 | `ACCESS_MEMBER` | Participation, read by `get_user_access` and scope grants |

`ACCESS_ALL` is 15. Read only is 4, read with write 6, read with execute 12.

### Expansion

`create_claim` and `update_claim` expand a mask with the deny bit clear before storing it:

| Bit present | Adds |
|---|---|
| owner | write, read, execute: `| 14` |
| write | read, execute: `| 12` |
| execute | read: `| 4` |

So 1 stores 15, 2 stores 14, 8 stores 12, 4 stores 4, 33 stores 47. A deny mask is stored as given. The expansion runs once per claim write, so every grant mask read afterwards already carries the bits it implies, and no read asks whether owner implies read.

## The effective mask of a binding

`claim_object.sa_access` may be NULL, which means the claim's mask. The mask a grant carries through one binding is

```
e = claim.sa_access | coalesce(claim_object.sa_access, claim.sa_access)
```

and the grant bits are `e & 15`; `e & 16` makes the derivation a deny. A binding mask therefore adds to its claim's mask. What a writer may add is bounded by `_check_claim_composition`, a before trigger on `claim_object`: a binding with a non NULL, non deny mask on an object is refused when it carries a bit the writer's own `user_object` row on that object does not, `(b & ~A(writer, y) & 15) ≠ 0`. It reads one range of the writer's pairs on the object.

The binding mask is stored as given; `create_claim`'s expansion does not apply to it. A binding of mask 2 on a read claim yields `4 | 2 = 6`.

## Cascade

A binding reaches the rows below its object when both `claim.inherits` and `claim_object.inherits` are true. The claim flag is the switch for every binding the claim carries; the binding flag narrows it to one object. Either false stops the cascade, which is how a broad claim is bound to one room without reaching anything under it. The objects a claim reaches are

```
T_c = { y : bind(c, y) }  ∪  { x : bind(c, y, inherits), claim(c, inherits), anc_t(x, y, h), h > 0 }
|T_c| = n_c + Σ_{inheriting bindings y} |desc(y)|
```

read as one range of `idx_claim_object_claim_id` and one range of `idx_inheritance_info_ancestor` per inheriting binding.

## Composition

A `claim_object` whose `object_type` is `claimius.claim` links claim A to claim B. A holder of A gains every binding B carries on an object, with the mask

```
e = (coalesce(link.sa_access, A.sa_access) & coalesce(binding.sa_access, B.sa_access))
    | ((coalesce(link.sa_access, A.sa_access) | coalesce(binding.sa_access, B.sa_access)) & 16)
```

so the grant bits are capped by the link and by the binding, and a deny on either side makes the composed derivation a deny.

1. The link must inherit and B must inherit, `link.inherits` and `B.inherits`.

2. A composed grant reaches only the objects B is bound to, not their descendants: the composition rule reads B's bindings on the object itself. Its class is `composition`, which an inherited deny does not subtract from; see [Denials](./denials.md).

3. A does not absorb B's grants. Holding A does not make you whoever holds B.

4. Composition is single hop. A link on B is not followed, a self link and a link whose reverse already exists are refused, and the writer must hold read on B in `user_object`.

The use is reusing a surface at another strength. A Bookings Admin claim binds every meeting room of an organization; an External Vendor claim links to it with mask 4, and every vendor reads the same rooms without a binding copied: one link row where copying costs `n_B` binding rows and stays in step with B only by hand.

Why single hop: with one hop the claims that can reach an object are the claims bound on it and its ancestors plus the claims linking to those bound on it, found with `C` probes of the reader's links. A transitive hierarchy makes a grant's reach a graph search whose depth depends on the data; see [Types](./types.md#why-the-model-stays-below-the-reachability-bound).

## Time windows

`user_claim.starts_at` and `ends_at` bound a grant. The derivation that fills `user_object` ignores the window; the reads filter it. `_best_grant` joins every derivation in a pair's `grants` to its live `user_claim` inside its window, so a pair whose only grants are outside their window returns no grant, and `get_objects` and `search` drop it. Nothing is revoked and no job runs, the way an [RFC 5280](https://www.rfc-editor.org/rfc/rfc5280) validity period works; the cost is two probes per derivation per read, `2 G_x`.

## Actor tokens

1. `sa_created_by` on every row is a `user_claim.id`: the grant the writer acted under, not the person. Authorship survives `merge_user`, and the record names the claim, the owning organization and the window that made the write legal.

2. `get_access`, `get_direct_access`, `mutation_access`, `get_objects` and `search` return a `user_claim_id`, the grant `_best_grant` chose. A follow on write stamps it into `sa_created_by`.

3. The creator of a row reaches it through its own `sa_created_by`, `direct_grant` on the pair.

4. Listings and graphs are no authority to act on a row in them. To act, call `mutation_access` for that row.

5. There is no `sa_updated_by` or `sa_deleted_by`.

## Choosing the grant

`_best_grant(grants, user, app, required)` returns, among the derivations of a pair whose mask covers `required` and whose grant is live and in its window, the one with the most bits set, then the highest mask, then the oldest claim. `get_direct_access` orders its rows by the fewest hops from the object, then the most bits, then the grant id.

## The single object gate

`mutation_access(user, object_id, object_type, required)` is the gate every write and every read of one object passes. It resolves through `resolve_access`: `claimius.audit`, `claimius.user_relation` and `claimius.samna_user` go to their own lookups, every other type to `get_access` and then `get_direct_access`.

| Outcome | Answer |
|---|---|
| A grant covers `required` | One row, the chosen grant, with its `user_claim_id` |
| The object exists and no grant covers `required` | SQLSTATE `P0403` |
| The object does not exist or is soft deleted | SQLSTATE `P0404` |

On a hit it reads what `get_access` reads; on a miss it adds two walks of `get_direct_access` and one existence probe; see [Reads](./reads.md). One round trip answers 200, 403 or 404.

## Scope grants

A `claim_object` with no object pair and a `scope` payload is a capability: a page, a feature, a public subset for a machine client; see [Capabilities](./capabilities.md). A scope on an object binding is a different thing: its keys are field names of the object; see [Redaction](./redaction.md).
