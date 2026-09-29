---
layout: doc
title: Redaction
description: How a scope on an object binding narrows a grant to named fields, how _apply_denies settles the grants and denies of one pair into a mask per field with a star for the rest, what each read returns, and what a response withholds.
tags: redaction, scope, field mask, claim_object, user_object.scope, _apply_denies, _build_grants_for_objects, withhold
lastUpdated: true
---

# Redaction

A `claim_object` with an object pair may carry a `scope`. Its keys are field names of the bound object, and it narrows the grant, or the deny, to those fields. The values under the keys are not read. A scope of NULL means the whole object.

```json
{ "first_name": true, "last_name": true }
```

A binding of a read claim to a user's `samna_user` row with that scope lets its holders see who the user is and nothing else about them.

## A mask per field

Redaction is a field mask: for a pair `(u, x)` and every field `f` of the object's table, the bits `u` holds on `f`,

```
M(u, x, f) = ⋁ over surviving grants and the creator of a(f)
```

with `a_g(f)` of [the model](./index.md#the-access-of-a-pair). The fields of a type are the columns of its table, read from `pg_attribute` in the same statement; a scope key that is not a column is ignored, so a scope naming no column grants nothing. For each derivation `_apply_denies` computes:

1. An unscoped grant carries `close(e_g & 15 & ~D_g)` on every field, and a different mask on each field a scoped deny reaches.

2. A scoped grant carries `close(e_g & 15 & ~D_g(f))` on each field `f` its scope names and nothing on any other field. Its contribution to `sa_access` is the OR over its fields.

3. The creator carries `close(15 & ~(X_d | F_d(f)))`, an unscoped grant.

## What the pair stores

`user_object.sa_access` is `A(u, x)`, the OR of every field mask, so a check on the object as a whole passes when any field carries the required bits. `user_object.scope` stores the field masks compactly:

```
M(*)  = ⋁ over unscoped cells of their mask on unnamed fields
scope = { f : M(f) }  for every field f named by some cell with M(f) ≠ M(*),  plus  { "*" : M(*) }
scope = NULL          when no named field differs from M(*)
```

| Derivations on one document | `scope` | `sa_access` |
|---|---|---|
| Read scoped `{"name"}`, read scoped `{"description"}` | `{"name": 4, "description": 4, "*": 0}` | 4 |
| Read scoped `{"name"}`, read unscoped | NULL | 4 |
| Write, 14, scoped `{"name"}`, read unscoped | `{"name": 14, "*": 4}` | 14 |
| Read unscoped, a direct deny of read scoped `{"description"}` | `{"description": 0, "*": 4}` | 4 |

A deny scoped to a field lowers that field only; an unscoped deny lowers every field; see [Denials](./denials.md). A scope travels down a cascade with its binding: an inherited derivation carries the scope of the binding on the ancestor.

The merge is `G_x` cells times the fields they name, per recompute of the pair, and no work on a read.

## What each read returns

1. `get_access`, and `mutation_access` through it, return the pair's stored `scope`, the field masks of every surviving derivation.

2. `get_objects` and `search` return the pair's stored `scope` on every row.

3. `get_direct_access` returns the raw `scope` of the binding it found, and skips a binding whose scope names no field of the type.

## What a response withholds

The implementing side renders the object, so the implementing side redacts:

1. A field whose mask lacks read is withheld from a response, and a field whose mask lacks write is refused in a write body. A field absent from a non NULL scope takes the mask under `*`.

2. With `scope` NULL every field carries `sa_access` and nothing is withheld.

## Why the mask is stored on the pair

A listing of `n` objects renders `n` scopes, and each is a column of the row the listing already reads. Merging grants at request time would repeat the derivation per row, `O(C + a_x + B_x)` index entries each, where the stored scope costs none. Redaction by field is the object attribute decision [NIST SP 800-162](https://csrc.nist.gov/pubs/sp/800/162/upd2/final) describes; claimius keeps it to the one attribute that needs no policy language, the field name.
