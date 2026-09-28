---
layout: doc
title: Claims
description: Claims, grants and bindings, the access bits and their expansion, binding masks, composition, time windows, actor tokens, the mutation_access contract and scope grants.
tags: claim, user_claim, claim_object, sa_access, ACCESS_ALL, composition, ScopeAccess, mutation_access, P0403, P0404, sa_created_by
lastUpdated: true
---

# Claims

Access is three rows. A `claim` says what may be done, a `user_claim` says who holds it, a `claim_object` says what it covers. A claim on its own grants nothing, and neither does a claim bound to an object but held by nobody. One claim bound to a hundred rooms and granted to fifty people is 151 rows, not 5,000.

| Row | Key columns |
|---|---|
| `claim` | `name`, `sa_access`, `inherits`, `type` (`system`, `user`, `service`, `virtual`), `sa_owner_id`, `sa_root_id` |
| `user_claim` | `user_id`, `claim_id`, `starts_at`, `ends_at`, `reason` |
| `claim_object` | `claim_id`, `object_type`, `object_id`, `sa_access`, `inherits`, `scope`, `ref_id` |

## The access bits

`sa_access` is a bitmask. Each bit is its own permission, combined with OR, checked with `(stored & required) = required`. The Go constants are `shared.ClaimAccess`.

| Bit | Value | Constant | Means |
|---|---|---|---|
| owner | 1 | `ACCESS_OWNER` | Full control, including binding and deleting |
| write | 2 | `ACCESS_WRITE` | Create and modify |
| read | 4 | `ACCESS_READ` | View |
| execute | 8 | `ACCESS_EXECUTE` | Trigger actions, consume the resource |
| deny | 16 | `ACCESS_DENY` | The other bits are subtracted, see [Denials](./denials.md) |
| member | 32 | `ACCESS_MEMBER` | Participation, surfaced in member listings and notifications |

`ACCESS_ALL` is 15. Read only is 4, read with write 6, read with execute 12.

### Expansion

A permission that implies another carries it. `create_claim` and `update_claim` expand a grant mask, one with the deny bit clear, before storing it.

| Bit present | Adds |
|---|---|
| owner | write, read, execute |
| write | read, execute |
| execute | read |

Through `create_claim`: 1 stores 15, 2 stores 14, 8 stores 12, 4 stores 4, 32 stores 32, 33 stores 47. A deny mask is stored verbatim, so 17, 18, 20 and 24 store themselves. A mask with no bit other than deny is refused: a claim that grants nothing and denies nothing has no meaning.

The expansion is an invariant, not a convenience. Every mask claimius stores or computes is closed under it, so a check for read never has to ask whether owner implies read.

### The binding mask

`claim_object.sa_access` narrows the claim for one binding. NULL inherits the claim mask. The effective mask of a grant through a binding is

```
effective = claim.sa_access & expand(coalesce(claim_object.sa_access, claim.sa_access))
```

so a binding can remove bits from its claim and never add any. A writer can bind a claim only when the effective mask is covered by the writer's own access on the bound object; `_check_claim_composition` checks the effective mask, never the raw column, so a NULL binding mask on a strong claim is judged as the claim's full mask.

## Cascade

A binding reaches the rows below its object when both `claim.inherits` and `claim_object.inherits` are true. The claim flag is the switch for every binding the claim carries; the binding flag narrows it to one object. Either false stops the cascade, and that is how a broad claim is bound to one room without reaching anything under it. Which trees a cascade follows is in [Objects](./objects.md).

## Composition

A `claim_object` whose `object_type` is `claimius.claim` links claim A to claim B. Everyone holding A gains B's bindings, each capped:

```
composed = expand(coalesce(link.sa_access, A.sa_access)) & effective(B, binding)
```

1. A composed grant cascades exactly as B's binding cascades, when the link and both claims inherit.

2. A composed grant keeps its binding's class: a composed binding on the object itself is direct, one reached through a tree is inherited. The same holds for a composed deny.

3. A does not absorb B's grants. Holding A does not make you whoever holds B.

4. Holders of A see claim B itself at read, 4, so a graph can draw the link.

5. Composition is single hop. Links through links grant nothing, self links and reverse links are refused, and the writer must hold read on B. Holding B through a `user_claim` gives read on B.

The use is reusing a surface at a different strength. A Bookings Admin claim binds every meeting room of an organization; an External Vendor claim links to it with mask 4 and every vendor sees the same rooms at read without a binding copied.

Why single hop: with one hop the claims that can reach an object are the claims bound on its ancestors plus the claims linked to those, a set found with two index lookups. A transitive role hierarchy makes a grant's reach a graph search whose depth depends on the data, the case the NIST model leaves to implementations and Zanzibar bounds only by precomputing it ([Pang et al. 2019, section 3.2.4](https://www.usenix.org/system/files/atc19-pang.pdf)).

## Time windows

`user_claim.starts_at` and `ends_at` bound a grant. The read path filters on the current time, so a grant outside its window returns nothing, in both directions, on the next read. Nothing is revoked and no job runs, the way an [RFC 5280](https://www.rfc-editor.org/rfc/rfc5280) validity period works.

## Actor tokens

1. `sa_created_by` on every row is a `user_claim.id`: the grant the writer acted under, not the person. Authorship survives `merge_user`, and the record names the claim, the owning organization and the window that made the write legal.

2. `get_access`, `get_direct_access`, `mutation_access`, `get_objects` and `search` return a `user_claim_id`. When a row is returned it is populated. A follow on write stamps it into `sa_created_by`.

3. The creator of a row holds it through its own `sa_created_by`. The actor token for a creator's access is that same `user_claim.id`.

4. Listings and graphs are no authority to act on a row in them. To act, call `mutation_access` for that row.

5. There is no `sa_updated_by` or `sa_deleted_by`. Later changes are attributed in [Audit](./audit.md).

## The single object gate

`mutation_access(user, object_id, object_type, required)` is the gate every write and every read of one object passes. It resolves through `resolve_access`: `claimius.audit`, `claimius.user_relation` and `claimius.samna_user` go to their own lookups, every other type to `get_access` and then `get_direct_access`.

| Outcome | Answer |
|---|---|
| A grant covers `required` | One row, the strongest grant, with its `user_claim_id` |
| The object exists and no grant covers `required` | SQLSTATE `P0403` |
| The object does not exist, is soft deleted, has a malformed id or an unregistered type | SQLSTATE `P0404` |

`MutationAccess` in Go maps `P0403` to `ErrNoAccess` and `P0404` to `ErrNotFound`, so one round trip answers 200, 403 or 404. `MutationAccessMany` runs `resolve_access` per row in one statement and never raises: a denied or unknown row is a nil entry.

The strongest grant is chosen by, in order: the fewest hops from the object, the most bits set, the oldest claim.

## Scope grants

A `claim_object` with no object pair and a `scope` payload is a capability: a page, a feature, a public subset for a machine client. It is matched by jsonpath, carries the same effective mask rule as an object binding, and is covered in [Capabilities](./capabilities.md). A scope on an object binding is a different thing: its keys are field names of the object. See [Redaction](./redaction.md).
