---
layout: doc
title: Denials
description: Deny claims, the two deny classes, the subtraction _apply_denies computes over the derivations of one pair, closure under implication, denies scoped to fields, what a creator keeps, and a worked scenario of an organization admin with a denied branch.
tags: deny, ACCESS_DENY, direct deny, inherited deny, _apply_denies, _close_access, _build_grants_for_objects, scoped deny, creator, direct_grant, scenario
lastUpdated: true
---

# Denials

A deny is a derivation whose effective mask carries the deny bit, 16: a claim stored with bit 16 and bound with a NULL mask or a mask that keeps bit 16, or a composition whose link or binding carries it. Its other bits, `e & 15`, name what it takes away. A deny mask is stored as given, never expanded, and a deny claim is bound and granted like any claim: `insert_claim_object` puts it on an object, `insert_user_claim` gives it to the users it applies to.

## The classes

`_build_grants_for_objects` collects every derivation reaching a user on an object and tags it with the class of the binding's position, `tree_type` in `grants`:

| Class | Reaches the object through |
|---|---|
| `direct` | A binding on the object itself, held or composed |
| `ownership`, `location`, `parenthood` | A binding on an ancestor, held or composed, through that tree |

## Levels

Every claim carries `level`, an integer from 0 that defaults to 0, and every derivation carries the level of its claim; a composed derivation carries the higher level of A and B. Levels order the denies before the classes do:

1. A deny subtracts from every grant below its level, whatever the classes of the two. A level 9 deny bound on an organization removes a level 0 grant bound directly on an object below it.

2. At the same level the classes decide, as below: a `direct` deny subtracts from every grant, a tree deny from the tree grants.

3. A deny below a grant's level does not touch it.

With every claim at level 0 the classes alone decide, so a deployment that never sets a level resolves as before levels existed. `_apply_denies`, the ancestor walk in `get_direct_access` and `get_scope_access` apply the same rule, and a derivation's level is stored in `grants` only when it is above 0.

## How a deny subtracts

`_apply_denies` settles the derivations of one pair, with the symbols of [the model](./index.md#the-access-of-a-pair):

1. A deny of class `direct` subtracts its bits from every grant, of every class, and from the creator.

2. A deny of any other class subtracts its bits from the grants of classes other than `direct` only. A grant bound on the object itself survives it. That is how one object is shared out of a denied subtree.

3. What remains of each grant is closed under implication, and a grant left with no bit is dropped.

```
D_g   = X_d | [k_g ≠ direct] X_o
a_g   = close(e_g & 15 & ~D_g)
A(u, x) = ⋁_g a_g | [made(u, x)] close(15 & ~X_d)
```

`close` keeps a bit only when every bit it implies survives: read needs nothing, execute needs read, write needs read and execute, owner needs all three. So denying write to a holder of 15 leaves `close(13) = 12`, read and execute, because owner implies write and cannot stand without it; denying read leaves `close(11) = 0`, and the grant is dropped, because every other bit implies read; denying execute leaves `close(7) = 4`. A grant claim's mask is closed under `X` when stored, `eff` of two such masks is closed too, and `close` keeps it so after a subtraction, so a check tests the bits it needs against a mask where no write stands without read.

The member bit is masked off every derivation, `& 15`, so it never reaches `user_object`.

The cost is `G_x × X` mask operations for `G_x` grants and `X` denies on one pair, in memory, paid once per recompute of the pair; with no deny and no scope the function returns the grants unchanged with one aggregate. The read of a pair is the same with or without denies. This is the deny overrides combining rule of [XACML 3.0, appendix C](https://docs.oasis-open.org/xacml/3.0/xacml-3.0-core-spec-os-en.html), scoped by class. A deny is negation over the derivations of the same pair only, which keeps the access relation a nonrecursive stratified view and its maintenance exact; see [the model](./index.md#why-the-view-is-maintained-exactly).

## Denies scoped to fields

A deny binding with a `scope` subtracts from the fields the scope names only, and only from fields the object's table carries. `F_d(f)` and `F_o(f)` collect them by class, and each grant is settled per field:

```
a_g(f) = close(e_g & 15 & ~(D_g | F_d(f) | [k_g ≠ direct] F_o(f)))
```

An unscoped grant keeps its mask for every other field, recorded under `*`. The field masks are the pair's `scope`; see [Redaction](./redaction.md). A scoped deny never lowers `A(u, x)` below what the unnamed fields keep.

## What a creator keeps

The creator of a row, the user behind its `sa_created_by`, holds `close(15 & ~X_d)` on it through `direct_grant`, ORed with whatever grants survive. Only a deny bound on the object itself reaches the creator; an inherited deny never does, so an author always reaches what they made inside a denied subtree.

## Why a deny is a claim

1. One rule path. A deny is bound, granted and synced exactly as a grant is. It needs no table, index or read path of its own; the one index it adds, `idx_claim_deny_bit`, is partial and holds the deny claims only.

2. A deny is scoped to people. The same subtree can be denied to contractors and open to staff, because the deny reaches only the users who hold it.

3. The cost is paid once. The subtraction happens when a pair is recomputed; the read is the same pair lookup with or without denies.

4. The window of a deny's grant is not read when the pair is recomputed, so a deny subtracts from the pairs of its holders while its grant is live.

## Worked scenario: admin over an organization, a denied branch

Ada administers the organization Acme. One branch, Research, is closed to her for writing, and one document under Research is shared with her directly.

### Rows

| Table | Row |
|---|---|
| `organization` | Acme, root. Sales and Research under Acme. Lab and Archive under Research |
| `public.doc` | Two documents under each organization, registered with `upsert_table_info('public.doc')` |
| `claim` | Admin, `insert_claim(p_sa_access => 1)`, stored 15 |
| `claim` | No write on Research, `insert_claim(p_sa_access => 18)`, stored 18 |
| `claim` | Share, `insert_claim(p_sa_access => 4)`, stored 4 |
| `claim_object` | Admin on Acme, `inherits` true |
| `claim_object` | No write on Research on Research, `inherits` true |
| `claim_object` | Share on Lab doc 1 |
| `user_claim` | Ada holds Admin, No write on Research, and Share |

Every binding mask is NULL, so `eff` is the claim mask: 15, 18 and 4.

### Masks in `user_object`

| Object | Grants reaching Ada | Denies reaching Ada | `sa_access` |
|---|---|---|---|
| Acme | Admin 15, `direct` | none | 15 |
| Acme's documents, Sales and its documents | Admin 15, `ownership` | none | 15 |
| Research | Admin 15, `ownership` | 2, `direct` | `close(15 & ~2) = 12` |
| Research's documents, Lab, Archive and their documents | Admin 15, `ownership` | 2, `ownership` | `close(15 & ~2) = 12` |
| Lab doc 1 | Admin 15 `ownership`, Share 4 `direct` | 2, `ownership` | `12 | 4 = 12` |

With the deny widened to read, 20, Admin's grant on every object of the Research branch closes to `close(15 & ~4) = 0` and is dropped: Ada holds no pair on Research, Lab, Archive or their documents, except Lab doc 1, which holds 4, because the directly bound Share is a `direct` grant and an `ownership` deny does not subtract from it. A document Ada creates under Lab holds 15 in both cases, through `direct_grant`, because no deny is bound on the document itself.

Composition behaves by the position of the binding. A Vendor claim, 4, linked to Admin with link mask 4 gives its holders `comp = eff(Vendor, 4) & eff(Admin, NULL) = 4 & 15 = 4` on Acme, class `direct`, and 4 on everything below Acme, class `ownership`, because Admin's binding inherits. A deny held by a vendor on Research subtracts from those as it would from Admin's own grant.
