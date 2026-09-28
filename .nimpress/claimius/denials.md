---
layout: doc
title: Denials
description: Deny claims, how a direct and a cascaded deny subtract, what survives a deny, what a creator keeps, and a worked scenario of an organization admin with a denied subtree.
tags: deny, ACCESS_DENY, direct deny, cascaded deny, _apply_denies, creator, direct_grant, scenario
lastUpdated: true
---

# Denials

A deny is a claim with the deny bit, 16, set in `sa_access`. The other bits name what it takes away. It is stored verbatim, never expanded, and bound and granted like any claim: `assign_claim_object` puts it on an object, `assign_claim_user` gives it to the users it applies to.

## How a deny subtracts

Every grant that reaches a user on an object has a class.

| Class | Reaches the object through |
|---|---|
| Direct | A binding on the object itself, or a composed binding on the object itself |
| Inherited | A binding on an ancestor, cascaded through a tree, directly or through composition |
| Creator | The object's own `sa_created_by`, a grant of the user |

Every deny has the same two classes, direct and inherited. The rules:

1. A direct deny subtracts its bits from every grant on the object, of every class.

2. An inherited deny subtracts its bits from inherited grants only. A grant bound on the object itself survives it. That is how one object is shared out of a denied subtree.

3. After subtraction the mask is closed under the expansion of [Claims](./claims.md): a bit whose implied bits are gone goes with them. Denying write to an owner leaves read and execute, 12, because owner implies write. Denying read leaves nothing, because every other permission implies read.

4. The surviving grants are ORed. A user with no bit left is not in `user_object` for that object: no listing shows it, no check passes it, `mutation_access` answers `P0403`.

5. The member bit, 32, is outside the expansion. It is subtracted only by a deny that names it.

Written as a formula over the grant masks of one user on one object, with `G_d`, `G_i`, `G_c` the ORed direct, inherited and creator grants and `X_d`, `X_i` the ORed direct and inherited deny bits:

```
sa_access = close( (G_d | G_c) & ~X_d  |  G_i & ~(X_d | X_i) )
```

where `close(m)` removes owner unless write, read and execute are present, write unless read and execute are present, and execute unless read is present. `_build_grants_for_object` computes it over the derivations of one pair, for a check and for the settle alike, so a listing reads the result and a check evaluates it over the bounded bindings of one object. This is the deny overrides combining rule of [XACML 3.0, appendix C](https://docs.oasis-open.org/xacml/3.0/xacml-3.0-core-spec-os-en.html). A deny is negation over the derivations of the same pair only, which keeps the access relation a nonrecursive stratified view and its maintenance exact; see Result 1 of [Theory](./theory.md).

## What a creator keeps

The creator of a row holds 15 on it through `sa_created_by`. An inherited deny never touches the creator grant, so an author always reaches what they made inside a denied subtree. A direct deny subtracts from it like from any grant, and a direct deny carrying the owner bit, 17 or wider, takes it away entirely.

## Why a deny is a claim

1. One rule path. A deny is bound, granted, windowed, audited and synced exactly as a grant is. Nothing about denies needs its own table, index or read path.

2. Deny is scoped to people. The same subtree can be denied to contractors and open to staff, because the deny reaches only the users who hold it.

3. The cost is paid once. The subtraction happens when an area settles; the read is the same primary key probe with or without denies. See [Materialization](./materialization.md).

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
| `claim_object` | Share on Lab doc 1, `inherits` true |
| `user_claim` | Ada holds Admin, No write on Research, and Share |

### Settled masks in `user_object`

| Object | Grants reaching Ada | Deny reaching Ada | `sa_access` |
|---|---|---|---|
| Acme and its documents | Admin 15, inherited or on Acme itself | none | 15 |
| Sales and its documents | Admin 15, inherited | none | 15 |
| Research | Admin 15, inherited | 18, direct on Research | 12 |
| Research documents, Lab, Archive and their documents | Admin 15, inherited | 18, inherited | 12 |
| Lab doc 1 | Admin 15 inherited, Share 4 direct | 18, inherited | 12 |

With the deny widened to read, 20, the Research branch leaves `user_object` entirely: Research, Lab, Archive and their documents are absent, and `mutation_access` answers `P0403` on each. Lab doc 1 keeps 4, the directly bound Share, because an inherited deny does not reach a direct grant. A document Ada creates under Lab keeps 15 in both cases.

Composition behaves the same way. A Vendor claim linked to Admin with link mask 4 gives its holders 4 on Acme, Sales and their documents, and nothing on the Research branch once the read deny is in place, because the composed grant is inherited there.
