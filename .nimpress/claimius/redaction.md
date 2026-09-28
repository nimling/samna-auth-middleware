---
layout: doc
title: Redaction
description: How a scope on an object binding narrows a grant to named fields, how recompute_user_object merges the scopes of several grants on one pair, what each read returns, and what a response withholds.
tags: redaction, scope, claim_object, field scope, user_object.scope, recompute_user_object, withhold
lastUpdated: true
---

# Redaction

A `claim_object` with an object pair may carry a `scope`. Its keys are field names of the bound object, and it narrows the grant to those fields. A scope of NULL means the whole object.

```json
{ "first_name": true, "last_name": true }
```

A binding of a read claim to a user's `samna_user` row with that scope lets its holders see who the user is and nothing else about them.

## How scopes merge on a pair

Every derivation in a pair's `grants` array carries the scope of its binding. `recompute_user_object` merges them into `user_object.scope`:

1. When the user is the creator, or any surviving grant has a NULL scope, `scope` is NULL: the whole object.

2. Otherwise `scope` is the concatenation `s_1 || s_2 || ... || s_G` of the grants' scope objects, the union of their keys, a later grant's value replacing an earlier one's under the same key.

3. `sa_access` is the OR over all surviving grants whatever their scope, so a check on the object as a whole passes when any grant carries the required bits.

4. A deny subtracts its bits from the grants it covers whatever either scope names; see [Denials](./denials.md). A scope narrows a grant; it never narrows a deny.

5. A scope travels down a cascade with its grant: an inherited derivation carries the scope of the binding on the ancestor.

| Grants on one document | `user_object.scope` | `sa_access` |
|---|---|---|
| Read scoped `{"name": true}`, read scoped `{"description": true}` | `{"name": true, "description": true}` | 4 |
| Read scoped `{"name": true}`, read unscoped | NULL | 4 |
| Write scoped `{"name": true}`, read unscoped | NULL | 14 |

The merge is `G` concatenations per recompute of the pair, and no work on a read.

## What each read returns

1. `get_objects` and `search` return the pair's merged `scope`.

2. `get_access`, and `mutation_access` through it, return the scope of the one grant `_best_grant` chose, `grant_scope`, not the merged one. A handler that needs the union of what the user may see on one object reads it from the listing row, or from every derivation.

3. `get_direct_access` returns the scope of the binding it found on the closest ancestor.

## What a response withholds

The implementing side renders the object, so the implementing side redacts:

1. A field outside a non NULL scope is withheld from a response and refused in a write body.

2. With `scope` NULL nothing is withheld.

## Why the scope is stored on the pair

A listing of `n` objects renders `n` scopes, and each is a column of the row the listing already reads. Merging grants at request time would repeat the derivation per row, `O(C + a_x + B_x)` index entries each, where the stored scope costs none. Redaction by field set is the object attribute decision [NIST SP 800-162](https://csrc.nist.gov/pubs/sp/800/162/upd2/final) describes; claimius keeps it to the one attribute that needs no policy language, the field name.
