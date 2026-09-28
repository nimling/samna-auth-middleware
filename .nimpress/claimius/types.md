---
layout: doc
title: Types
description: Why organization, location, user and claim, joined by two bindings and three hierarchies, are the types authorization needs, and what that small set buys in index and query cost.
tags: types, organization, location, samna_user, claim, rbac, rebac, abac, multi tenancy, delegation
lastUpdated: true
---

# Types

Claimius carries four subject and structure types and two bindings.

| Type | Table | Is |
|---|---|---|
| Organization | `claimius.organization` | Who owns something. Nests into the ownership tree |
| Location | `claimius.location` | Where something is. Nests into the location tree |
| User | `claimius.samna_user` | Who acts. A person or a machine client, one row per app |
| Claim | `claimius.claim` | What may be done. A mask and an inherit flag |

| Binding | Table | Joins |
|---|---|---|
| Grant | `claimius.user_claim` | A user to a claim, inside an optional time window |
| Binding | `claimius.claim_object` | A claim to an object, to another claim, or to a scope payload |

Three hierarchies carry a binding downward: ownership through `sa_owner_id`, location through `sa_location_id` and the location tree, and parenthood through `sa_parent_id` with `sa_root_id` on a registered table that declares its own tree.

## The access relation

Written as relational algebra over those tables, the access a user `u` holds on an object `o` is

```
A(u, o) = π mask ( σ user_id = u (user_claim) ⋈ claim ⋈ claim_object ⋈ σ ancestor ∈ up(o) (object_info) )
```

with the deny subtraction of [Denials](./denials.md) applied to the result. Every question claimius answers is this relation or a projection of it. A check fixes `u` and `o`. A listing fixes `u` and `object_type`. A member listing fixes `o`. Claimius stores the relation materialized per user in `user_object`, so each of those is an index range on one table. See [Materialization](./materialization.md).

## Every common model reduces to these

1. Role based access control. A claim is a role, a `user_claim` is a role assignment, a `claim_object` is a permission assignment. A role hierarchy is claim composition, a `claim_object` whose `object_type` is `claimius.claim`. This is the vocabulary of the NIST model standardized as ANSI INCITS 359 ([NIST RBAC](https://csrc.nist.gov/projects/role-based-access-control)).

2. Relationship based access control. A relation tuple `object#relation@user` in the sense of the Zanzibar paper ([Pang et al. 2019](https://www.usenix.org/system/files/atc19-pang.pdf)) is a grant plus a binding: the relation is the claim, the tuple's object is the binding's object pair. A userset rewrite that inherits from a parent, `viewer of folder implies viewer of document`, is a hierarchy: the document carries `sa_parent_id` or `sa_owner_id` and the binding carries `inherits`.

3. Attribute based access control. The attribute decision that remains after relationships is almost always a capability with no row behind it, a page, a feature, a public subset. That is a scope grant: a `claim_object` with no object pair and a `scope` payload, matched by jsonpath through `ScopeAccess`. Field level attributes are redaction, a scope on an object binding whose keys are field names. See [Claims](./claims.md) and [Redaction](./redaction.md).

4. Multi tenancy. A tenant is a root organization, `sa_owner_id = sa_root_id = id`. Every row under it resolves to that root, cross root moves are refused, and an app reaches a tenant only through `app_object`. See [Organizations](./organizations.md) and [Apps](./apps.md).

5. Groups. A group is a claim held by its members. Nested groups are one level of composition. Membership without permission is the member bit, 32, with no other bit set.

6. Resource hierarchies. Folders, buildings, departments, product trees: the ownership, location and parenthood trees, precomputed as the up set on each `object_info` row.

7. Delegation. An actor acts for a target by holding a grant that covers the target's `samna_user` row. The row written carries the actor's `user_claim.id` in `sa_created_by` and the target's `user_id` as its subject. See [Users](./users.md).

8. Machine identities. A client is a `samna_user` of type `client` whose `user_id` is `samna_client.client_id`. It holds grants like a person.

## What the small type set buys

1. One shape for every grant. Every grant path is `user_claim → claim → claim_object → object_info`. Five indexes serve all of them: `idx_user_claim_user_app`, the claim primary key, `idx_claim_object_lookup`, `idx_claim_object_object` and the `object_info` primary key, with `idx_object_info_ancestors` for the objects under a node. A general tuple store needs an index per relation shape it rewrites through.

2. A bounded evaluation. With single hop composition and three trees, the ancestors of an object number at most `D_q + D_l + D_o (D_l + 2)` for the parenthood, location and ownership depths, and the bindings that can reach it are the bindings on those ancestors. Settling one pair is `O(D + C)` index probes for a user holding `C` claims. There is no recursive rewrite whose depth depends on the data, the case where the Zanzibar evaluation fans out and which Zanzibar bounds with Leopard, its precomputed index of group to group reachability ([Pang et al. 2019, section 3.2.4](https://www.usenix.org/system/files/atc19-pang.pdf)). The up set on `object_info` is that precomputed reachability for all three trees, and why its bound keeps claimius below the lower bound for dynamic reachability is Result 2 of [Theory](./theory.md).

3. A fixed read surface. The same `get_*` functions answer every consumer, because every consumer's objects enter through the same binding shape. Nothing about a consumer's schema reaches a claimius function body.
