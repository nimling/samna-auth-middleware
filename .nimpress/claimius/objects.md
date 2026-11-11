---
gate: samna-developer
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

2. A composite id is built with `claimius.composite_id(parts...)`, the MD5 of the parts joined by `:` read as a UUID. The primary keys of `user_users` and `access_diff` are composite ids of their key columns, so every copy of the schema derives the same id for the same row, and a repeated write of one area collapses onto one row. `user_object` is keyed on its columns themselves, `(user_id, object_type, object_id)`.

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
| `app_id` | The app of the row | no: a row without one resolves to the apps of its `app_object` links, else to `get_app_id()` |

`upsert_table_info(VARIADIC object_types)` registers the named tables:

1. For each it records the column shape in `table_info`, one row with one `has_*` flag per column, read from `information_schema` once, and attaches the calc trigger `tg_calc_access`: `calc_hierarchical_access` when the table has both `sa_parent_id` and `sa_root_id`, `calc_object_access` otherwise, plus the before insert self root trigger.

2. Under `replay_mode` it seeds the rows already present with `_bulk_seed_user_object`: the `object_info` row of every row, then the pairs of every user affected by every live row, collected in one statement and grouped by user, recomputed in batches of 1,000 objects per user. Each pair raises its `user_users_diff` row like any other, for the reads and the drain to fold. The seed emits no sync event.

Adding `sa_parent_id` and `sa_root_id` to a table already in production is a three step change: add the columns, backfill them, then re-register the table with `upsert_table_info` so the engine picks up the new shape, `calc_hierarchical_access` in place of `calc_object_access`. Registration is idempotent. It reseeds the table's `object_info` rows and pairs but builds no closure for the rows already there, so the parenthood tree of the backfilled rows is built by `recompute_state` after it.

`delete_table_info(VARIADIC object_types)` deregisters the named tables: it detaches their triggers and deletes their `table_info`, `inheritance_info`, `user_object` and `object_info` rows, the `user_object` rows counted out of `user_users` like any other delete. A name that is not a registered table raises.

`recompute_state` rebuilds the closure of every root of every tree from live rows and seeds every registered type a live binding names, the path after a bulk write that bypassed the triggers.

`table_info` is why no claimius function reflects on `information_schema` at run time: the calc triggers, the settle and the graphs read one row per type.

## The closure

`inheritance_info` holds one row per `(tree_type, ancestor, descendant)` with its `depth` and the tree's `root_id`, self edges at depth 0 included. The primary key is `(tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id)`, and two covering indexes answer both directions:

| Question | Index |
|---|---|
| Which nodes sit above `x` | `idx_inheritance_info_descendant (descendant_type, descendant_id, tree_type) INCLUDE (ancestor_type, ancestor_id, root_id, depth)`, index only |
| Which objects sit below `y` | `idx_inheritance_info_ancestor (ancestor_type, ancestor_id, tree_type) INCLUDE (descendant_type, descendant_id, root_id, depth)`, index only |

A registered row holds an ownership row for itself, its owner and each of the owner's ancestors, and, when pinned in a location, a location row for itself, the location and each of the location's ancestors.

### What each write moves

1. A registered row without its own tree, `calc_object_access` through `_retree_object`. An insert or a restore runs `_attach_object_to_trees`: the self edge and the owner's ancestor rows copied with depth plus one, one `INSERT ... SELECT` per tree. A soft delete runs `_detach_object_from_trees`: every ownership and location row whose descendant is the row. An owner or location change runs both. A row without a tree of its own has no descendants, so nothing else moves. An update that changes neither owner, location nor deletion moves no closure row.

2. A row of a table with its own parenthood tree, and every organization and location, `calc_hierarchical_access` under the advisory lock of its root. An insert writes its self edge and the rows linking it to its parent's ancestors; a parent change splices its stored subtree with `_splice_subtree`; a soft delete drops the subtree's rows; a restore rebuilds only the subtree with `build_*_tree(root, node)`. Its pins in the other trees move through `_retree_object` as in 1. See [Organizations](./organizations.md) and [Locations](./locations.md).

3. A row applied under `replay_mode` runs the same triggers, so a disciple keeps its closure by the same incremental steps as the prophet. `apply_row_delta(type, id, old, new)` is the same step as a function of the old and new row, deleting the row's descendant edges and inserting them under its new parents, which `rebuild_tree(root)` replays root first to rebuild one root from live rows.

A write moves only the rows of the subtree it touches, never the rows of the rest of the tenant.

## object_info

`object_info` holds, once per live registered object, what is the same for every user who reaches it: `sa_owner_id`, `sa_location_id`, `sa_root_id`, `sa_created_by`, `sa_owner_root_id`, `sa_name`, `sa_description`, `sa_link`, and `search_vector`, generated from the name at weight A and the description at weight B. `sa_created_by` is the grant the row was created under, so a settle reads the creator here and nothing from the implementing side's table. `sa_owner_root_id` is the root of the owning organization: the organization's own root, or the root of the owner of the row or of its location. An area of the object is keyed under it, and a check ranges the areas of it.

| Index | Serves |
|---|---|
| primary key `(object_type, object_id)` | the join from a pair to its object |
| `idx_object_info_owner_name (sa_owner_id, sa_name, object_id)` | the children of one owner in name order, for the graphs |
| `idx_object_info_location_name (sa_location_id, sa_name, object_id)` | the rows of one location in name order |
| `idx_object_info_created_by (sa_created_by, sa_owner_root_id)` | the objects a grant created, for the roots of a reader's share and for the full build of its reach |
| `idx_object_info_search` GIN on `search_vector` | search; see [Search](./search.md) |

`_write_object_info(type, ids)` brings the rows of a set of objects in step with the objects themselves: it reads them with `_denormalize_objects`, each object's row and its owner and location for the roots, upserts a live one only where a field differs, and deletes the row of one gone or soft deleted. The calc triggers call it on every insert and delete and on an update that changes the name, description, owner, location, root, creator or deletion; the bulk paths call it for the whole table.

`recompute_user_object` reads `object_info` and inserts nothing: an object with no row is not live to it, and the pair goes. A listing, a search and a graph join `object_info` by key and never read the implementing side's table. A rename writes the one `object_info` row and no pair.

## Optional data about an object

`object_field` defines a field for an `object_type` per app, with a `json_schema`, a `required` flag and a default `value`; `user_field` holds a user's value for it. Both are app scoped source rows, carried on the feed, and registered with claimius like its other internal tables.

## Soft delete

A registered row is deleted by stamping `sa_deleted_at`. The calc trigger detaches its closure rows, deletes its `object_info` row and raises its object area, and the settle of that area deletes the pair of every user who reaches it, because `recompute_user_object` finds the row gone. A hard delete would leave `audit` rows and `sa_created_by` references pointing at nothing, so claimius never issues one on a registered table.

## Cost

`table_info` is read with one probe of `idx_table_info_object_type`. `_denormalize_objects` reads one `table_info` probe and, per object, its row and its owner and location.

The closure rows of one object and of the whole table, with `t_x` the trees holding `x` and `a_x` its ancestors over them:

```
rows(x) = Σ_{t ∋ x} (d_t(x) + 1) = a_x + t_x
rows    = Σ_x rows(x)
```

A registered row owned by an organization at depth `d` holds `d + 2` ownership rows, and pinned in a location at depth `d_l` it holds `d_l + 2` location rows more. Every row carries three index entries. The ancestors of `x` are one descent of `idx_inheritance_info_descendant`, `a_x + t_x` entries; the descendants of `y` one descent of `idx_inheritance_info_ancestor`, `|desc(y)| + 1` entries.

| Write | Closure rows |
|---|---|
| Insert or restore of a row without its own tree | `rows(x)` inserted |
| Soft delete of a row without its own tree | `rows(x)` deleted |
| Insert into a parenthood tree at depth `d` | `d + 1` |
| Parent change, depth `d` to `d'` | `n_S · d` deleted, `n_S · d'` inserted |
| Soft delete or restore of a subtree | `Σ_{x ∈ S} (d(x) + 1)` |

```
closure rows written by a write = rows moved, never rows of the tenant
insert: rows(x)        move: n_S (d + d')        delete or restore: Σ_{x ∈ S} rows(x)
```

The registration seed recomputes `Σ_x |affected(x)|` pairs, `(u, x)` with `u` in `_affected_users_for_object(x)`, in `Σ_u ⌈n_u / 1000⌉` calls of `recompute_user_object`, where `n_u` is the objects of the type user `u` is affected on, instead of one call per pair.

```
object_info rows          = live registered objects, independent of users
rows written per rename   = 1, with 3 btree entries and one GIN entry per changed lexeme
pairs written per rename  = 0
```

Stored on the pair these fields would cost `U_x` copies per object, and a rename would rewrite every copy. A listing, a search and a graph pay one `object_info` probe per row they return.

## Proofs and measurements

### Measurements

1. Registration seed on bookable's dev data, 23 tables, 46,063 objects and 210 users: 253 calls of `recompute_user_object` instead of 57,082 one per pair, and the registration with `recompute_state` after it takes 92 seconds instead of 865.

2. Keeping the per object fields in `object_info` instead of on each pair brings the average pair from 1,316 to about 805 bytes.

### Why a closure table

`get_access` needs nothing from the closure, because it reads a stored pair. The settle needs both directions: the ancestors of one object, to find the bindings that reach it, and the descendants of one node, to find the objects a binding reaches. A closure table answers each with one descent and an index only range, with no recursion in the reading query.

1. Against a walk of the parent columns: a recursive query pays one dependent probe per level, `D` descents for the ancestors and one per node of the subtree for the descendants. The closure pays one.

2. Against an interval labeling, a pre and post order interval per node: an ancestor test is one comparison, but every insert must find a free label between its neighbours. That is the online list labeling problem, and Bulánek, Koucký and Saks prove `Ω(log² n)` relabelled items per insert when the label space is linear in `n`. The closure writes the `d + 1` rows of the new node and relabels nothing.

3. The cost is storage, `d + 1` rows per object and tree, and a move rewrites `n_S` times the depth above it, the cost Karwin derives for closure tables, which `_splice_subtree` pays exactly.

**Sources.** [PostgreSQL, Index-Only Scans and Covering Indexes](https://www.postgresql.org/docs/17/indexes-index-only-scans.html). J. Bulánek, M. Koucký, M. Saks, [Tight Lower Bounds for the Online Labeling Problem](https://iuuk.mff.cuni.cz/~koucky/papers/monlab.pdf), STOC 2012. B. Karwin, [Moving Subtrees in Closure Table Hierarchies](https://www.percona.com/blog/moving-subtrees-in-closure-table/).
