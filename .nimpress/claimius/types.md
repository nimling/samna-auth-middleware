---
layout: doc
title: Types
description: Why organization, location, user and claim, joined by two bindings and three trees, are the types authorization needs, how every common model reduces to them, and why that set keeps a check constant in the size of the data.
tags: types, organization, location, samna_user, claim, rbac, rebac, abac, multi tenancy, delegation, reachability
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

Three trees carry a binding downward: ownership through `sa_owner_id`, location through `sa_location_id` and the location tree, and parenthood through `sa_parent_id` with `sa_root_id` on a registered table that declares its own tree. Each is stored as a closure in `inheritance_info`; see [Objects](./objects.md).

## The access relation

Over those tables the access a user `u` holds on an object `x` is

```
A(u, x) = π mask ( σ user_id = u (user_claim) ⋈ claim ⋈ claim_object ⋈ σ descendant = x (inheritance_info) )
```

with the composition term and the deny subtraction of [the model](./index.md#the-model) applied to the result. Every question claimius answers is this relation or a projection of it: a check fixes `u` and `x`, a listing fixes `u` and `object_type`, a member listing fixes `x`. Claimius stores it per pair in `user_object`, so each of those is an index read on one table; see [Materialization](./materialization.md).

## Every common model reduces to these

1. Role based access control. A claim is a role, a `user_claim` a role assignment, a `claim_object` a permission assignment. A role hierarchy of one level is claim composition, a `claim_object` whose `object_type` is `claimius.claim`. This is the vocabulary of the NIST model standardized as ANSI INCITS 359 ([NIST RBAC](https://csrc.nist.gov/projects/role-based-access-control)).

2. Relationship based access control. A relation tuple `object#relation@user` in the sense of the Zanzibar paper ([Pang et al. 2019](https://www.usenix.org/system/files/atc19-pang.pdf)) is a grant plus a binding: the relation is the claim, the tuple's object is the binding's object pair. A rewrite that inherits from a parent, viewer of a folder implies viewer of its documents, is a tree: the document carries `sa_parent_id` or `sa_owner_id` and the binding carries `inherits`.

3. Attribute based access control. The attribute decision left after relationships is almost always a capability with no row behind it, a page, a feature, a public subset. That is a scope grant, a `claim_object` with no object pair and a `scope` payload; see [Capabilities](./capabilities.md). A field level attribute is a scope on an object binding; see [Redaction](./redaction.md).

4. Multi tenancy. A tenant is a root organization, `sa_owner_id = sa_root_id = id`. Every row under it resolves to that root, a cross root move is refused, and an app reaches a tenant only through `app_object`. See [Organizations](./organizations.md) and [Apps](./apps.md).

5. Groups. A group is a claim held by its members. A group of groups is one level of composition.

6. Resource hierarchies. Folders, buildings, departments, product trees: the ownership, location and parenthood closures.

7. Delegation. An actor acts for a target by holding a grant that covers the target's `samna_user` row. The row written carries the actor's `user_claim.id` in `sa_created_by`. See [Users](./users.md).

8. Machine identities. A client is a `samna_user` of type `client` whose `user_id` is its `samna_client.client_id`. It holds grants like a person.

## What the small type set buys

1. One shape for every grant. Every derivation is `user_claim → claim → claim_object → inheritance_info`. The indexes that serve all of them are `idx_user_claim_user_app`, the `claim` primary key, `idx_claim_object_object`, `idx_claim_object_lookup` and `idx_inheritance_info_descendant`. A general tuple store needs an index per relation shape it rewrites through.

2. A bounded derivation. With one composition hop and three trees, the ancestors of `x` number `a_x = d_o(x) + d_l(x) + d_q(x) ≤ D_o + D_l + D_q`, and the bindings that can reach `x` are the `B_x` bindings on `x` and those ancestors, plus the `L` bindings reached through one link. Deriving one pair reads `O(C + L + a_x + B_x)` index entries, whatever `U` and `O` are.

3. A fixed read surface. The same `get_*` functions answer every consumer, because every consumer's objects enter through the same binding shape.

## Why the model stays below the reachability bound

**Claim.** A model whose group nesting or composition is transitive over data must solve dynamic reachability, for which no algorithm has both update and query time polynomially below the edge count `m`. Claimius's recursion is bounded by the depth of three trees, and its check reads a number of index entries independent of `m`.

**Proof.**

1. A Zanzibar rewrite that follows a group into its subgroups asks whether a path of membership edges leads from a user to a group. The paper states it: "Group membership can be considered as a reachability problem in a graph". Fong's ReBAC evaluates a policy by local model checking whose cost is that of a depth first search bounded by the nesting of its modal operators.

2. Henzinger, Krinninger, Nanongkai and Saranurak show that, under the OMv conjecture, no fully dynamic s-t subgraph connectivity algorithm has polynomial preprocessing, `m^{α−ε}` amortized update time and `m^{1−α−ε}` amortized query time for any `0 ≤ α ≤ 1`, and that the bound carries to every reachability problem, transitive closure included.

3. In claimius a composition link is followed once: the composition rule consumes a binding of the linked claim on the object itself and never another link. The only recursion is `anc_t`, and each tree is acyclic by the types of its edges: an organization goes up only to organizations, a location to locations in the location tree and to its owner in the ownership tree, a registered row to rows of its own type, a location or an organization, and every root points at itself. So `|anc(x)| ≤ D_o + D_l + D_q`.

4. The cost of storing `anc` is paid on a move: moving a subtree `S` changes `anc` for exactly the `n_S` nodes of `S` and the `n_S × d` pairs between them and the ancestors above the move; any representation that answers `anc(x)` without a walk must write those.

**Sources.** R. Pang et al., [Zanzibar: Google's Consistent, Global Authorization System](https://www.usenix.org/system/files/atc19-pang.pdf), USENIX ATC 2019, sections 2 and 3.2.4. M. Henzinger, S. Krinninger, D. Nanongkai, T. Saranurak, [Unifying and Strengthening Hardness for Dynamic Problems via the Online Matrix-Vector Multiplication Conjecture](https://arxiv.org/abs/1511.06773), STOC 2015. P. W. L. Fong, [Relationship-Based Access Control: Protection Model and Policy Language](https://pages.cpsc.ucalgary.ca/~pwlfong/Pub/codaspy2011.pdf), CODASPY 2011, section 4.3.

**For claimius.** Zanzibar pays the bound with Leopard, a precomputed group to group index of which the paper says "a single Zanzibar tuple addition or deletion may yield potentially tens of thousands of discrete Leopard tuple events". Claimius does not pay it, because its only recursion is three trees of bounded depth. Three trees and one hop are the condition under which a derivation is constant in the size of the data.
