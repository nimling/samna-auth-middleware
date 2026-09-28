---
layout: doc
title: Objects
description: What an object is to claimius, how an implementing side registers its tables with init_claimius_tables, the sa_* columns, table_info, the inheritance_info closure, and object_field.
tags: object_type, object_id, init_claimius_tables, table_info, object_info, owner_ids, location_ids, parent_ids, sa_owner_id, sa_parent_id, sa_root_id, object_field
lastUpdated: true
---

# Objects

To claimius an object is a relation: an `object_type` and `object_id` pair that a `claim_object` binds and that `user_object` computes access for. Claimius owns and validates its own types, the tables in the `claimius` schema. It never constructs the implementing side's objects and never reads their data beyond the `sa_*` columns and a name and description for rendering.

## Object types

1. An `object_type` is always schema qualified at every boundary: `claimius.organization`, `public.bookable`, never `bookable`. The `public` schema belongs to the implementing side.

2. A composite id is `<schema>.<table>:<uuid>`, built with `claimius.composite_id`. The primary keys of `user_users` and `access_diff` are deterministic hashes of their key columns through the same function, so repeated writes of one pair or area collapse onto one row. `user_object` is keyed by its four columns, `(user_id, app_id, object_type, object_id)`, and `object_info` by `(object_type, object_id)`; both are derived on each side and never travel, so neither needs a hashed id.

## Registration

The implementing side makes a table participate by carrying the `sa_*` columns and naming it in `init_claimius_tables('public.doc', ...)`, in its own migration.

| Column | Required | Means |
|---|---|---|
| `sa_owner_id` | yes | The owning organization, the ownership tree |
| `sa_created_by` | yes | The `user_claim.id` the creator acted under |
| `sa_deleted_at` | yes | Soft delete |
| `sa_created_at`, `sa_updated_at` | yes | Timestamps |
| `sa_location_id` | no | A pin into the location tree |
| `sa_parent_id` with `sa_root_id` | no, both or neither | The table's own parenthood tree |
| `name`, `description` | no | Rendering and search text copied into the object's `object_info` row |

`init_claimius_tables` is idempotent and takes the complete list of the implementing side's tables. For each it records the column shape in `table_info`, attaches the calc trigger and the self root trigger, and seeds `user_object` for rows already present, under `replay_mode` so the seed emits nothing. A table registered before and absent from the list is deregistered: triggers detached, its `object_info` rows and access rows removed. Adding `sa_location_id`, or the `sa_parent_id` and `sa_root_id` pair, to a table in production is a migration, a backfill and a rerun with the same list.

`table_info` is why no claimius function reflects on `information_schema` at run time: the calc trigger, the settle and the graphs read one row per type.

## The up set

Every object's ancestors are stored on its own `object_info` row, one array per tree, nearest first, the object itself excluded.

| Column | Holds |
|---|---|
| `owner_ids` | The owning organization and its chain to the root, followed by the owner chain of every location in `location_ids` that is not already present |
| `location_ids` | The pinned location, or for a location its parent, and the chain to the location root |
| `parent_ids` | The parenthood chain of a table that declares `sa_parent_id` and `sa_root_id` |

The hop of an ancestor is its position in its array. `idx_object_info_ancestors`, a GIN index on `owner_ids || location_ids || parent_ids`, finds everything under one node.

| Question | Read |
|---|---|
| Which nodes sit above `x` | The primary key of `object_info`, one descent, the arrays from the row |
| Which objects sit below `y` | The posting list of `y` in `idx_object_info_ancestors`, then the rows |

1. An insert copies its parent's arrays with the parent prepended: one row, its primary key entry and one posting per ancestor, appended to the GIN pending list and merged in bulk ([PostgreSQL, GIN](https://www.postgresql.org/docs/17/gin.html)).

2. A move of a subtree `S` rewrites the arrays of its `n_S` rows and nothing else. Exactly those rows change their ancestors, so no encoding that answers from the object's own row can write fewer.

3. A soft delete marks the row. The evaluation reads only live ancestors, so a binding on a deleted node reaches nothing beneath it.

Why arrays on the object: a check needs the ancestors of one object and a settle the objects under one node, and both are one index read here. Against a closure table of one row per ancestor pair, an insert writes 6 entries instead of 20, a move `7 n_S` instead of `32 n_S`, and an object takes about 150 bytes instead of 2,400, at depth 4. Against an interval labeling, an insert writes one row where online list labeling must relabel `Ω(log n)` others. The proofs are Result 3 of [Theory](./theory.md). The arrays live on `object_info` because the row a check reads for existence and creator then gives the ancestors too, one descent saved on every check.
## Optional data about an object

The prophet carries data about an implementing side's object only where the implementing side configures it. `object_field` defines a field for an `object_type` per app, with a `json_schema`, a `required` flag and a default `value`; `user_field` holds a user's value for it. Both are app scoped, synced to the app's disciples, and read through the same grants as everything else.

## Soft delete

A registered row is deleted by stamping `sa_deleted_at`. The calc trigger clears its up set and raises its area, and the settle removes every `user_object` row for it. A hard delete would leave the audit trail and `sa_created_by` references pointing at nothing, so claimius never issues one on a registered table.
