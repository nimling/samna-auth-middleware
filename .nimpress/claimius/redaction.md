---
layout: doc
title: Redaction
description: How a scope on an object binding narrows a grant to named fields, how the scopes of several grants combine in user_object, and what a response withholds.
tags: redaction, scope, claim_object, field scope, user_object.scope, withhold
lastUpdated: true
---

# Redaction

A `claim_object` with an object pair may carry a `scope`. Its keys are field names of the bound object, and it narrows the grant to those fields. A scope of NULL means the whole object.

```json
{ "name": true, "description": true }
```

A binding of the Read claim to a user's `samna_user` row with scope `{"first_name": true, "last_name": true}` lets its holders see who the user is and nothing else about them.

## How scopes combine

For one user on one object, after the deny subtraction of [Denials](./denials.md), every surviving grant contributes its mask to the fields it names, or to every field when it names none.

1. `user_object.scope` is NULL when every surviving grant is unscoped. The row's `sa_access` then holds for every field.

2. Otherwise `user_object.scope` maps each named field to the OR of the masks of the grants naming it, and the key `*` to the OR of the unscoped grants, the mask every other field gets.

3. `sa_access` stays the OR over all grants, so a check on the object as a whole passes when any field carries the required bits. Field level withholding reads the scope.

4. A scoped deny subtracts its bits from the fields it names and leaves the rest of the object alone.

5. A scope travels down a cascade with its grant. Field names a descendant's type does not carry grant and withhold nothing.

| Grants on one document | `user_object.scope` | `sa_access` |
|---|---|---|
| Read scoped `name`, Read scoped `description` | `{"name": 4, "description": 4, "*": 0}` | 4 |
| Read scoped `name`, Read unscoped | NULL | 4 |
| Write scoped `name`, Read unscoped | `{"name": 14, "*": 4}` | 14 |
| Read unscoped, deny read scoped `description` | `{"description": 0, "*": 4}` | 4 |

## What a response withholds

The implementing side renders the object, so the implementing side redacts. The contract:

1. A handler reads the grant with `get_access` or `mutation_access`. Both return the settled `scope` of the row, the same value `get_objects` and `search` return per row.

2. A field whose mask in the scope does not cover the bits the operation needs is withheld from a response and refused in a write body.

3. With `scope` NULL nothing is withheld.

## Why the scope is settled, not evaluated per request

The scope is part of the per user access row for the same reason the mask is: a listing of a thousand objects renders a thousand scopes, and each one is a column read. Merging grants at request time would repeat the grant walk per row. Redaction by field set is the attribute part of access control that [NIST SP 800-162](https://csrc.nist.gov/pubs/sp/800/162/upd2/final) describes as an object attribute decision; claimius keeps it to the one attribute that needs no policy language, the field name.
