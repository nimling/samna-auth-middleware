---
layout: doc
title: Objects
description: What an object is to claimius, how an implementing side registers its tables with init_claimius_tables, the sa_* columns, table_info, the inheritance_info closure with its row counts, and object_field.
tags: object_type, object_id, init_claimius_tables, table_info, inheritance_info, closure, sa_owner_id, sa_location_id, sa_parent_id, sa_root_id, object_field
lastUpdated: true
---

# Objects

To claimius an object is an `object_type` and `object_id` pair that a `claim_object` binds and that `user_object` records access for. Claimius owns and validates its own types, the tables in the `claimius` schema. It never constructs the implementing side's objects and reads no more of them than the `sa_*` columns, `name` and `description`.

## Object types and ids

1. An `object_type` is schema qualified at every boundary: `claimius.organization`, `public.bookable`, never `bookable`. `init_claimius_tables` refuses a type without a schema.

2. A composite id is built with `claimius.composite_id`, a deterministic UUID over its text arguments. The primary keys of `user_object`, `user_users`, `access_diff` and `reconcile_queue` are composite ids of their key columns, so every copy of the schema derives the same id for the same pair, and a repeated write of one pair or one area collapses onto one row.

## Registration

The implementing side makes a table participate by carrying the `sa_*` columns and naming it in `init_claimius_tables('public.doc', ...)`.

| Column | Means |
|---|---|
| `sa_owner_id` | The owning organization, the ownership tree |
| `sa_created_by` | The `user_claim.id` the creator acted under |
| `sa_deleted_at` | Soft delete |
| `sa_created_at`, `sa_updated_at` | Timestamps |
| `sa_location_id` | A pin into the location tree |
| `sa_parent_id` with `sa_root_id` | The table's own parenthood tree |
| `name`, `description` | Rendering and search text copied into each `user_object` row |

`init_claimius_tables` takes the complete list of the implementing side's tables.

1. For each it records the column shape in `table_info`, one row per type with a `has_*` flag per column, and attaches the calc trigger: `calc_hierarchical_access` when the table has both `sa_parent_id` and `sa_root_id`, `calc_object_access` otherwise, plus the self root trigger.

2. A table registered before and absent from the list is deregistered: its triggers detached, its `table_info` row, its `inheritance_info` rows and its `user_object` and `reconcile_queue` rows deleted, and `user_users` counted down with the pairs.

3. Under `replay_mode` it seeds `user_object` for the rows already present: for every live row `x` and every app of it, `recompute_user_object` for each user of `_affected_users_for_object(x)`, then `_refresh_user_users` for every user holding a pair of the type, since no count moves under `replay_mode`. The seed recomputes `Σ_x |affected(x)|` pairs.

`table_info` is why no claimius function reflects on `information_schema` at run time: the calc trigger, the settle and the graphs read one row per type, one index probe on `idx_table_info_object_type`.

## The closure

`inheritance_info` holds one row per `(tree_type, ancestor, descendant)` with its `depth`, self edges at depth 0 included. The primary key is `(tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id)`, and two covering indexes answer both directions:

| Question | Read |
|---|---|
| Which nodes sit above `x` | `idx_inheritance_info_descendant (descendant_type, descendant_id, tree_type) INCLUDE (ancestor_type, ancestor_id, root_id, depth)`: one descent, `a_x + t_x` entries for `t_x` trees holding `x`, index only |
| Which objects sit below `y` | `idx_inheritance_info_ancestor (ancestor_type, ancestor_id, tree_type) INCLUDE (descendant_type, descendant_id, root_id, depth)`: one descent, `|desc(y)| + 1` entries, index only |

The rows of one object and of the whole table:

```
rows(x)  = Σ_{t ∋ x} (d_t(x) + 1)
rows     = Σ_x rows(x)  ≤  O (D_o + D_l + D_q + 3)
```

A registered row owned at depth `d_o` of the ownership tree and pinned at depth `d_l` of the location tree holds `d_o + 1 + d_l + 1` rows. The depth of a registered row counts from its owner, so a row owned by an organization at depth 3 has `d_o = 4`.

### What each write does to it

1. A registered row insert, `_attach_object_to_trees`: the self edge, then the owner's ancestor rows copied with depth plus one, one `INSERT ... SELECT` per tree: `d_o + 1` rows in the ownership tree and `d_l + 1` in the location tree, each with three index entries. When the owner's self edge is missing the whole root is rebuilt instead; see [Organizations](./organizations.md).

2. A registered row owner or location change, or its soft delete, `_detach_object_from_trees`: every ownership and location row whose descendant is the row, `rows(x)` deletes, then the attach of 1 on the new position. Only the row itself moves, because a registered row without its own parenthood tree has no descendants.

3. A row of a table with its own parenthood tree, `calc_hierarchical_access`: `build_parenthood_tree` deletes the tree's rows for the root and rebuilds them with one recursive query, `N_r` rows deleted and `N_r` inserted for a root whose parenthood closure holds `N_r` rows.

4. An organization or location write rebuilds the closure of its root; see [Organizations](./organizations.md) and [Locations](./locations.md).

### Why a closure table

`get_access` needs nothing from the closure, because it reads a stored pair; `get_direct_access` reads the ancestors of one object. The settle needs both directions: the ancestors of one object, to find the bindings that reach it, and the descendants of one node, to find the objects a binding reaches. A closure table answers each with one descent and an index only range, with no recursion in the reading query.

1. Against a walk of the parent columns: a recursive query pays one dependent probe per level, `D` descents for the ancestors and one descent per node of the subtree for the descendants. The closure pays one.

2. Against an interval labeling, a pre and post order interval per node: an ancestor test is one comparison, but every insert must find a free label between its neighbours. That is the online list labeling problem, and Bulánek, Koucký and Saks prove `Ω(log² n)` relabelled items per insert when the label space is linear in `n`. The closure writes the `d + 1` rows of the new node and relabels nothing.

3. The cost is storage, `d + 1` rows per object and tree, and the rows a move rewrites. Moving a subtree of `n_S` nodes by hand rewrites `n_S` times the depth above the move, the cost Karwin derives for closure tables; claimius instead rebuilds the root, which writes `2 N_r` rows but is one set based statement per tree.

**Sources.** [PostgreSQL, Index-Only Scans and Covering Indexes](https://www.postgresql.org/docs/17/indexes-index-only-scans.html). J. Bulánek, M. Koucký, M. Saks, [Tight Lower Bounds for the Online Labeling Problem](https://iuuk.mff.cuni.cz/~koucky/papers/monlab.pdf), STOC 2012. B. Karwin, [Moving Subtrees in Closure Table Hierarchies](https://www.percona.com/blog/moving-subtrees-in-closure-table/).

## Optional data about an object

`object_field` defines a field for an `object_type` per app, with a `json_schema`, a `required` flag and a default `value`; `user_field` holds a user's value for it. Both are app scoped, carried on the feed, and registered with claimius like its other internal tables.

## Soft delete

A registered row is deleted by stamping `sa_deleted_at`. The calc trigger detaches its closure rows and raises its object area, and the settle of that area deletes the `user_object` row of every user reaching it, because `recompute_user_object` finds the row gone. A hard delete would leave `audit` rows and `sa_created_by` references pointing at nothing, so claimius never issues one on a registered table.
