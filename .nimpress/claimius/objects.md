---
layout: doc
title: Objects
description: What an object is to claimius, how an implementing side registers its tables with init_claimius_tables, the sa_* columns, table_info, the inheritance_info closure, and object_field.
tags: object_type, object_id, init_claimius_tables, table_info, inheritance_info, sa_owner_id, sa_parent_id, sa_root_id, object_field
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

`init_claimius_tables` is idempotent and takes the complete list of the implementing side's tables. For each it records the column shape in `table_info`, attaches the calc trigger and the self root trigger, and seeds `user_object` for rows already present, under `replay_mode` so the seed emits nothing. A table registered before and absent from the list is deregistered: triggers detached, its closure edges and access rows removed. Adding `sa_location_id`, or the `sa_parent_id` and `sa_root_id` pair, to a table in production is a migration, a backfill and a rerun with the same list.

`table_info` is why no claimius function reflects on `information_schema` at run time: the calc trigger, the settle and the graphs read one row per type.

## The closure

`inheritance_info` holds one row per ancestor and descendant pair per tree, `tree_type` one of `ownership`, `location`, `parenthood`, with the depth zero self edge.

| Index | Serves |
|---|---|
| Primary key `(tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id)` | Uniqueness, a whole root |
| `idx_inheritance_info_descendant (descendant_type, descendant_id, tree_type) INCLUDE (ancestor_type, ancestor_id, root_id, depth)` | The ancestors of one object, index only |
| `idx_inheritance_info_ancestor (ancestor_type, ancestor_id, tree_type) INCLUDE (descendant_type, descendant_id, root_id, depth)` | Everything under one node, index only |

Every registered row is a node: `D + 1` edges for depth `D`. The closure for `N` rows of average depth `D̄` is `N(D̄ + 1)` rows; the benchmark's 20,782 rows of depth up to 5 hold 142,743 edges. The two covering indexes make every ancestor and descendant question an index only scan, which is why tree depth does not appear in read latency.

Why a closure: a cascade asks two questions on every settle, which bindings sit above this object and which objects sit below this binding. A closure answers each with one range scan, `O(D)` and `O(n_S)` rows. A recursive walk costs a round of index probes per level. The price is storage linear in depth and write work on a move, `n_S × D` rows for a subtree `S`; see [Organizations](./organizations.md).

## Optional data about an object

The prophet carries data about an implementing side's object only where the implementing side configures it. `object_field` defines a field for an `object_type` per app, with a `json_schema`, a `required` flag and a default `value`; `user_field` holds a user's value for it. Both are app scoped, synced to the app's disciples, and read through the same grants as everything else.

## Soft delete

A registered row is deleted by stamping `sa_deleted_at`. The calc trigger detaches it from the closure and raises its area, and the settle removes every `user_object` row for it. A hard delete would leave the audit trail and `sa_created_by` references pointing at nothing, so claimius never issues one on a registered table.
