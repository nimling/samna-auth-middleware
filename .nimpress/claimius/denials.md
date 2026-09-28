---
layout: doc
title: Denials
description: Deny claims, the subtraction formula _apply_denies computes over the derivations of one pair, what a creator keeps, why a deny is a claim, and a worked scenario of an organization admin with a denied branch.
tags: deny, ACCESS_DENY, direct deny, inherited deny, _apply_denies, _build_grants_for_object, creator, direct_grant, scenario
lastUpdated: true
---

# Denials

A deny is a derivation whose effective mask carries the deny bit, 16: a claim stored with bit 16, or a binding whose mask carries it. Its other bits, `e & 15`, name what it takes away. A deny mask is stored as given, never expanded, and a deny claim is bound and granted like any claim: `assign_claim_object` puts it on an object, `assign_claim_user` gives it to the users it applies to.

## How a deny subtracts

`_build_grants_for_object` collects every derivation reaching a user on an object and tags it with its class.

| Class | `tree_type` in `grants` | Reaches the object through |
|---|---|---|
| Direct | `direct` | A binding on the object itself |
| Inherited | `ownership`, `location`, `parenthood` | A binding on an ancestor, through one tree |
| Composed | `composition` | A binding of a linked claim on the object itself |

`_apply_denies` then subtracts, for every grant and every deny reaching the same pair:

1. A deny of class direct subtracts its bits from every grant, of every class.

2. A deny of any other class, inherited or composed, subtracts its bits from inherited grants only. A grant bound on the object itself, or composed onto it, survives it. That is how one object is shared out of a denied subtree.

3. A grant left with no bit is dropped.

With `X_d` the OR of the direct denies and `X_o` the OR of every other deny:

```
A(u, x) = 15·[direct_grant(u, x)]
        | ⋁_{g direct or composed} (e_g & 15 & ~X_d)
        | ⋁_{g inherited}          (e_g & 15 & ~(X_d | X_o))
```

The bits are subtracted independently and the result is not expanded again: denying write to a holder of 15 leaves 13, owner, read and execute, and denying read leaves 11, owner, write and execute. A check tests the bits it needs, so a pair of 11 passes a write check and fails a read check. The member bit is masked off every derivation, `& 15`, so it never reaches `user_object`.

The cost is `G × X` mask operations for `G` grants and `X` denies on one pair, in memory, paid once per recompute of the pair; the read of a pair is the same with or without denies. This is the deny overrides combining rule of [XACML 3.0, appendix C](https://docs.oasis-open.org/xacml/3.0/xacml-3.0-core-spec-os-en.html), scoped by class. A deny is negation over the derivations of the same pair only, which keeps the access relation a nonrecursive stratified view and its maintenance exact; see [Materialization](./materialization.md#why-the-maintenance-is-exact).

## What a creator keeps

The creator of a row, the user behind its `sa_created_by`, holds 15 on it through `direct_grant`, ORed with whatever grants survive. `_direct_grant_for_object` removes it only when the user holds a deny bound on the object itself whose mask, claim and binding together with the binding mask set, carries the owner bit. An inherited deny never touches the creator, so an author always reaches what they made inside a denied subtree.

## Why a deny is a claim

1. One rule path. A deny is bound, granted and synced exactly as a grant is. Its grant's window is not read: the recompute ignores windows and only `_best_grant` applies them, to grants, so a deny subtracts while its grant is live. It needs no table, index or read path of its own; the one index it adds, `idx_claim_deny_bit`, is partial and holds the deny claims only.

2. A deny is scoped to people. The same subtree can be denied to contractors and open to staff, because the deny reaches only the users who hold it.

3. The cost is paid once. The subtraction happens when a pair is recomputed; the read is the same pair lookup with or without denies.

## Worked scenario: admin over an organization, a denied branch

Ada administers the organization Acme. One branch, Research, is closed to her for writing, and one document under Research is shared with her directly.

### Rows

| Table | Row |
|---|---|
| `organization` | Acme, root. Sales and Research under Acme. Lab and Archive under Research |
| `public.doc` | Two documents under each organization, registered with `init_claimius_tables('public.doc')` |
| `claim` | Admin, `create_claim(p_sa_access => 1)`, stored 15 |
| `claim` | No write on Research, `create_claim(p_sa_access => 18)`, stored 18 |
| `claim` | Share, `create_claim(p_sa_access => 4)`, stored 4 |
| `claim_object` | Admin on Acme, `inherits` true |
| `claim_object` | No write on Research on Research, `inherits` true |
| `claim_object` | Share on Lab doc 1 |
| `user_claim` | Ada holds Admin, No write on Research, and Share |

### Masks in `user_object`

| Object | Grants reaching Ada | Deny reaching Ada | `sa_access` |
|---|---|---|---|
| Acme | Admin 15, direct | none | 15 |
| Acme's documents, Sales and its documents | Admin 15, inherited | none | 15 |
| Research | Admin 15, inherited | 2, direct on Research | 13 |
| Research's documents, Lab, Archive and their documents | Admin 15, inherited | 2, inherited | 13 |
| Lab doc 1 | Admin 15 inherited, Share 4 direct | 2, inherited | `13 | 4 = 13` |

With the deny widened to read, 20, every object of the Research branch holds 11, and Lab doc 1 holds `11 | 4 = 15`, because the directly bound Share is not subtracted by an inherited deny. A document Ada creates under Lab holds 15 in both cases, through `direct_grant`.

Composition behaves by its class. A Vendor claim linked to Admin with link mask 4 gives its holders 4 on Acme, the one object Admin is bound to, and nothing below it: a composed grant does not cascade.
