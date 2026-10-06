---
layout: doc
title: Objects
description: What an object is to claimius, how an implementing side registers its tables with upsert_table_info, the sa_* columns and table_info, the inheritance_info closure with its row counts and the rows each write moves, object_info written once per object, and why the closure is a table.
tags: object_type, object_id, upsert_table_info, table_info, inheritance_info, closure, object_info, _write_object_info, _splice_subtree, _retree_object, apply_row_delta, recompute_state, object_field
lastUpdated: true
---

# Objects

To claimius an object is an `object_type` and `object_id` pair that a `claim_object` binds and that `user_object` records access for. Claimius owns and validates its own types, the tables in the `claimius` schema. It never constructs the implementing side's objects and reads no more of them than the `sa_*` columns, `name` and `description`.

## Object types and ids

1. An `object_type` is schema qualified at every boundary: `claimius.organization`, `public.bookable`, never `bookable`. `upsert_table_info` refuses a type without a schema.

2. A composite id is built with `claimius.composite_id(parts...)`, the MD5 of the parts joined by `:` read as a UUID. The primary keys of `user_users`, `access_diff` and `reconcile_queue` are composite ids of their key columns, so every copy of the schema derives the same id for the same row, and a repeated write of one area collapses onto one row. `user_object` is keyed on its columns themselves, `(user_id, app_id, object_type, object_id)`.

## Registration

The implementing side makes a table participate by carrying the `sa_*` columns and naming it in `upsert_table_info('public.doc', ...)`.

| Column | Means | Required |
|---|---|---|
| `sa_owner_id` | The owning organization, the ownership tree | yes |
| `sa_created_by` | The `user_claim.id` the creator acted under | yes |
| `sa_deleted_at`, `sa_created_at`, `sa_updated_at` | Soft delete and timestamps | yes |
| `sa_location_id` | A pin into the location tree | no |
| `sa_parent_id` with `sa_root_id` | The table's own parenthood tree, both or neither | no |
| `name`, `description` | Rendering and search text, copied into `object_info` | no |
| `app_id` | The app of the row | on a prophet that hosts apps besides its system app |

`upsert_table_info` takes the complete list of the implementing side's tables:

1. For each it records the column shape in `table_info`, one row with one `has_*` flag per column, read from `information_schema` once, and attaches the calc trigger `tg_calc_access`: `calc_hierarchical_access` when the table has both `sa_parent_id` and `sa_root_id`, `calc_object_access` otherwise, plus the before insert self root trigger.

2. A table registered before and absent from the list is deregistered: its triggers detached, and its `table_info` row, its `inheritance_info` rows, its `user_object`, `object_info` and `reconcile_queue` rows deleted. The `user_object` delete is counted out of `user_users` like any other.

3. Under `replay_mode` it seeds the rows already present with `_bulk_seed_user_object`: `object_info` for every row, then the pairs `(u, x)` with `u` in `_affected_users_for_object(x)` for every live row `x` and every app of it, collected in one statement and grouped by user, and one `recompute_user_object` per user and 1,000 of its objects, each pair raising its `user_users_diff` row like any other, for the reads and the drain to fold. The seed recomputes the same `Σ_x |affected(x)|` pairs in `Σ_u ⌈n_u / 1000⌉` calls, where `n_u` is the objects of the type user `u` is affected on, instead of one call per pair, and emits no sync event. On bookable's dev data, 23 tables, 46,063 objects and 210 users, that is 253 calls instead of 57,082, and its registration with `recompute_state` after it takes 92 seconds instead of 865.

`recompute_state` rebuilds the closure of every root of every tree from live rows and seeds every registered type a live binding names, the path after a bulk write that bypassed the triggers.

`table_info` is why no claimius function reflects on `information_schema` at run time: the calc triggers, the settle and the graphs read one row per type, one probe of `idx_table_info_object_type`.

## The closure

`inheritance_info` holds one row per `(tree_type, ancestor, descendant)` with its `depth` and the tree's `root_id`, self edges at depth 0 included. The primary key is `(tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id)`, and two covering indexes answer both directions:

| Question | Read |
|---|---|
| Which nodes sit above `x` | `idx_inheritance_info_descendant (descendant_type, descendant_id, tree_type) INCLUDE (ancestor_type, ancestor_id, root_id, depth)`: one descent, `a_x + t_x` entries for the `t_x` trees holding `x`, index only |
| Which objects sit below `y` | `idx_inheritance_info_ancestor (ancestor_type, ancestor_id, tree_type) INCLUDE (descendant_type, descendant_id, root_id, depth)`: one descent, `|desc(y)| + 1` entries, index only |

The rows of one object and of the whole table:

```
rows(x) = Σ_{t ∋ x} (d_t(x) + 1) = a_x + t_x
rows    = Σ_x rows(x)
```

A registered row owned by an organization at depth `d` holds `d + 2` ownership rows, and pinned in a location at depth `d_l` it holds `d_l + 2` location rows more. Every row carries three index entries.

### What each write moves

1. A registered row without its own tree, `calc_object_access` through `_retree_object`. An insert or a restore runs `_attach_object_to_trees`: the self edge and the owner's ancestor rows copied with depth plus one, one `INSERT ... SELECT` per tree, `rows(x)` rows. A soft delete runs `_detach_object_from_trees`: every ownership and location row whose descendant is the row, `rows(x)` deletes. An owner or location change runs both. A row without a tree of its own has no descendants, so nothing else moves. An update that changes neither owner, location nor deletion moves no closure row.

2. A row of a table with its own parenthood tree, and every organization and location, `calc_hierarchical_access` under the advisory lock of its root. An insert writes its self edge and the rows linking it to its parent's ancestors, `d + 1`; a parent change splices its stored subtree `S` with `_splice_subtree`, `n_S · d` rows deleted and `n_S · d'` inserted; a soft delete drops `Σ_{x ∈ S} (d(x) + 1)` rows; a restore rebuilds only the subtree with `build_*_tree(root, node)`. Its pins in the other trees move through `_retree_object` as in 1. See [Organizations](./organizations.md) and [Locations](./locations.md).

3. A row applied under `replay_mode` runs the same triggers, so a disciple keeps its closure by the same incremental steps as the prophet. `apply_row_delta(type, id, old, new)` is the same step as a function of the old and new row, deleting the row's descendant edges and inserting them under its new parents, which `rebuild_tree(root)` replays root first to rebuild one root from live rows.

```
closure rows written by a write = rows moved, never rows of the tenant
insert: rows(x)        move: n_S (d + d')        delete or restore: Σ_{x ∈ S} rows(x)
```

### Why a closure table

`get_access` needs nothing from the closure, because it reads a stored pair. The settle needs both directions: the ancestors of one object, to find the bindings that reach it, and the descendants of one node, to find the objects a binding reaches. A closure table answers each with one descent and an index only range, with no recursion in the reading query.

1. Against a walk of the parent columns: a recursive query pays one dependent probe per level, `D` descents for the ancestors and one per node of the subtree for the descendants. The closure pays one.

2. Against an interval labeling, a pre and post order interval per node: an ancestor test is one comparison, but every insert must find a free label between its neighbours. That is the online list labeling problem, and Bulánek, Koucký and Saks prove `Ω(log² n)` relabelled items per insert when the label space is linear in `n`. The closure writes the `d + 1` rows of the new node and relabels nothing.

3. The cost is storage, `d + 1` rows per object and tree, and a move rewrites `n_S` times the depth above it, the cost Karwin derives for closure tables, which `_splice_subtree` pays exactly.

**Sources.** [PostgreSQL, Index-Only Scans and Covering Indexes](https://www.postgresql.org/docs/17/indexes-index-only-scans.html). J. Bulánek, M. Koucký, M. Saks, [Tight Lower Bounds for the Online Labeling Problem](https://iuuk.mff.cuni.cz/~koucky/papers/monlab.pdf), STOC 2012. B. Karwin, [Moving Subtrees in Closure Table Hierarchies](https://www.percona.com/blog/moving-subtrees-in-closure-table/).

## object_info

`object_info` holds, once per live registered object, what is the same for every user who reaches it: `sa_owner_id`, `sa_location_id`, `sa_root_id`, `sa_name`, `sa_description`, `sa_link`, and `search_vector`, generated as `setweight(to_tsvector('simple', name), 'A') || setweight(to_tsvector('simple', description), 'B')`.

| Index | Serves |
|---|---|
| primary key `(object_type, object_id)` | the join from a pair to its object, one probe |
| `idx_object_info_owner_name (sa_owner_id, sa_name, object_id)` | the children of one owner in name order, for the graphs |
| `idx_object_info_location_name (sa_location_id, sa_name, object_id)` | the rows of one location in name order |
| `idx_object_info_search` GIN on `search_vector` | search; see [Search](./search.md) |

`_write_object_info(type, ids)` brings the rows of a set of objects in step with the objects themselves: it reads them with `_denormalize_objects`, one `table_info` probe and per object its row and its owner and location for the root, upserts a live one only where a field differs, and deletes the row of one gone or soft deleted. The calc triggers call it on every insert and delete and on an update that changes the name, description, owner, location, root or deletion; the bulk paths call it for the whole table. `recompute_user_object` inserts a missing row with `ON CONFLICT DO NOTHING`, so no pair is ever without its object's fields.

```
object_info rows          = live registered objects, independent of users
rows written per rename   = 1, with 3 btree entries and one GIN entry per changed lexeme
pairs written per rename  = 0
```

Stored on the pair these fields cost `U_x` copies per object, and a rename rewrote every copy. Moving them to `object_info` took the average pair from 1,316 to about 805 bytes, measured when the change was made. A listing, a search and a graph join `object_info` by key, one probe per row they return, and never read the implementing side's table.

## Optional data about an object

`object_field` defines a field for an `object_type` per app, with a `json_schema`, a `required` flag and a default `value`; `user_field` holds a user's value for it. Both are app scoped source rows, carried on the feed, and registered with claimius like its other internal tables.

## Soft delete

A registered row is deleted by stamping `sa_deleted_at`. The calc trigger detaches its closure rows, deletes its `object_info` row and raises its object area, and the settle of that area deletes the pair of every user who reaches it, because `recompute_user_object` finds the row gone. A hard delete would leave `audit` rows and `sa_created_by` references pointing at nothing, so claimius never issues one on a registered table.
