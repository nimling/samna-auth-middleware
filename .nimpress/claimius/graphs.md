---
gate: samna-developer
layout: doc
title: Graphs
description: The owner graph and the claim graph, their node shape, the window that bounds the rows they return, the rows each reads as a function of the tree and the reader, the per user cache of the default claim graph validated on read against the snapshot it was built under, and its hit ratio.
tags: get_owner_graph, get_claim_graph, _window_graph, _window_links, p_width, p_sort, p_next_id, claim_graph_cache, pg_snapshot, pg_visible_in_snapshot, txid, get_graph_links_owner, get_graph_links_claim
lastUpdated: true
---

# Graphs

Two functions render what a user can reach as a graph of flat nodes. Every node has five columns: `object_id`, `object_type` schema qualified, `label`, `parent_id`, and a `data` payload carrying the reader's `access` and `scope` and the node's own fields. Grouping on `parent_id` draws the tree with no further query.

| Function | Draws |
|---|---|
| `get_owner_graph(p_user_id, p_app_id, p_start_id, p_depth, p_required_access, p_width, p_sort, p_next_id)` | Organizations under their owners, locations under their parents or owners, and every registered row under its parent, location or owner |
| `get_claim_graph(p_user_id, p_start_id, p_depth, p_required_access, p_width, p_sort, p_next_id)` | The user, the users the user sees, the user's grants, the claims, their bindings, and the objects each binding reaches |
| `get_graph_links_owner`, `get_graph_links_claim` | The edges between the reader's nodes as one jsonb array, filtered by included and excluded types, read from the pairs as they stand, and windowed by the same `width`, `sort` and `next_id` |

The registered types a graph covers are read from `table_info`, so an implementing side's tables appear the moment they are registered and nothing about them is written into a graph function. The graph and link functions are declared with `plan_cache_mode = force_custom_plan`: each call is planned for its own `start_id`, `width` and `next_id`.

Both node graphs and the claim link graph begin with `reconcile_if_pending(p_user_id)`, the reader's share settled from its cursor, and `check_user_active`, as a listing does; an inactive reader gets an empty graph. The claim graph and its link graph then make the reader a viewer and fold its `user_users_diff` rows, since they show who the reader sees. The owner link graph settles nothing.

## The window

Both graphs take `p_width`, `p_sort` and `p_next_id`, applied by `_window_graph` to the graph once built:

1. Every parent shows at most `p_width` children of each type, 0 meaning all. A node whose parent is not in the graph is a top node, and the top nodes are the children of no parent.

2. Children come in name order, `label` then `object_id`, or with `p_sort = 'usage'` by the reader's decayed usage of the pair, heaviest first, then name; see [Usage](./usage.md).

3. A node is kept when it is inside its parent's window and its parent is kept.

4. With `p_next_id` the window continues after that node: its later siblings of its type, `p_width` of them, each with its windowed subtree. The node itself is not returned, and an id not in the graph returns nothing.

Any `p_sort` other than `name` or `usage`, or a negative `p_width`, is refused with SQLSTATE `22023`. The window bounds the rows returned and sent, not the rows read: the graph is built whole and windowed after. With `p_width = 0` and no `p_next_id` the graph is returned as built.

### The link window

The link graphs take the same three arguments after `p_depth`: `p_width INTEGER DEFAULT 0`, `p_sort TEXT DEFAULT 'name'` and `p_next_id UUID DEFAULT NULL`, refused the same way.

1. `_window_links` windows the links as `_window_graph` windows the nodes: every link target is a node whose parent is the link source, a source no link targets is a top node, and a link is kept when its target is kept under its source.

2. Each type is matched back to the registered type it normalizes from, and labels come from `object_info`, the claim or the `samna_user` row, so a node graph and a link graph drawn with one window show the same nodes in the same order.

3. The graph's own nodes join too: the objects in scope of the owner links, and the reader's claims and `samna_user` row of the claim links. One no link targets is a top node, so a root or a start that keeps no child is windowed like any other.

4. A windowed link graph returns `node_ids` beside `links`, the ids of every kept node. A kept node that keeps no child has no link, so a consumer builds the window's object set from `node_ids`, not from the link endpoints. Without a window the link graph returns `links` alone.

## The owner links

`get_graph_links_owner` links every object of the reader in scope to its closest ancestor the reader also reaches:

1. It reads the reader's pairs that hold a live grant, `_best_grant`, or a direct grant, once, into an array of composite ids.

2. For every object of the reader in scope it reads its closure range in `idx_inheritance_info_descendant` and keeps the ancestors whose composite id is in the array, a hashed membership test per edge.

3. Per tree it keeps the reached ancestors at the least depth: every depth one edge, and otherwise the closest.

4. A `start_id` is matched through `idx_inheritance_info_ancestor` with the registered types, one range.

`_window_graph` and `_window_links` test a parent and a source against a hashed set the same way, never a nested scan of the rows they come from.

## The owner graph

1. The roots: the app's root organizations, `app_object_ids`, or the one given as `start_id`, kept when the reader reaches them through `get_objects` over organizations.

2. The walk: every live organization below the roots to `depth` levels, 0 meaning all, one recursive query over `idx_organization_sa_owner_id`, each kept when `get_objects` returns it for the reader and joined to the reader's pair for its `access` and `scope`. The walked organizations are written to a transaction temporary table `_owner_graph_orgs`.

3. The locations owned by the walked organizations, kept when the reader reaches them, with `longitude` and `latitude` in `data`.

4. For every registered type with an owner or a location, `table_info` read once, the live rows owned by the walked organizations or pinned in their locations, each joined to the reader's pair with the mask covering `required` and to its `object_info` row for name, description, owner and location, in `(sa_owner_id, sa_name, id)` order, the order of `idx_object_info_owner_name`. Each row's parent is its own parent, then its location, then its owner.

The walk reads the subtree, not the reader's reach: an organization the reader cannot see is read and dropped. `start_id` and `depth` bound the rows read; the window bounds the rows returned.

## The claim graph

For the default shape, no `start_id`, `depth = 0` and `required = 0`, the graph is built from the reader's pairs:

1. every derivation in the `grants` of the reader's pairs that names one of the reader's claims, each joined to the binding it came through and to its `object_info` row;

2. the users the reader sees, `_user_users_for_viewer` after the fold of the reader's `user_users_diff` rows, each with its `samna_user` row and the reader's pair on it;

3. the reader's grants and claims;

4. the bindings of those claims, each with the reader's pair and `object_info` row on its object.

With `start_id` the graph reads that claim's bindings and, through `idx_inheritance_info_ancestor`, the objects each reaches to `depth`, where the claim and the binding both inherit, each with the reader's pair and its `object_info` row. `depth = 1` keeps the directly bound objects only.

## The claim graph cache

The default shape is stored per user in `claim_graph_cache (user_id, graph, snapshot)`, primary key `user_id`, as one jsonb array, and served from it, then windowed.

### What drops an entry

| Write | Drops |
|---|---|
| Pairs inserted, deleted or updated in any column, a mark that moves alone and a usage fold included | The entry of each user of the statement, one delete per statement over its transition tables |
| `user_users` rows inserted, moved by a fold or deleted, or the user's `samna_user` row | The entry of each viewer of the statement, one delete per statement |
| An `object_info` row updated, a rename or a move | The entry of every user holding a pair on the object |

A usage fold moves `used_count` and `last_used_at` on the pair, and that update drops the entry of every user whose pairs it touched, as any update of a pair does.

### What validates an entry

A claim or binding change drops nothing: the entry is validated on read.

1. `snapshot` is the `pg_current_snapshot()` taken before the graph's sources were read.

2. `tg_update_txid` stamps `txid = pg_current_xact_id()` on every insert and update of `claim`, `user_claim` and `claim_object`, under replay too, so `txid` names the local transaction that last wrote the row.

3. The read serves the entry only while every `user_claim` row of the user, soft deleted ones included, the claim of each and every binding of those claims carries a `txid` visible in the snapshot, `pg_visible_in_snapshot(txid, snapshot)` ([PostgreSQL, Transaction ID and Snapshot Information Functions](https://www.postgresql.org/docs/17/functions-info.html#FUNCTIONS-PG-SNAPSHOT)).

4. A stale entry is rebuilt and stored with a new snapshot.

5. The entry is served only when the fold before it left none of the user's `user_users_diff` rows pending: a pending row changes the users the graph shows without moving a `user_users` row, so it drops nothing, and the graph is built again until a fold takes it.

## Cost

With `w` the width, `τ` the types under a parent and `k` the levels, a window returns at most

```
Σ_{i=1}^{k} (w τ)^i
```

nodes below the top, whatever the tree holds. With `p_sort = 'usage'` each node costs one probe of the reader's pair.

### The owner links

```
entries = V_u + Σ_x (a_x + t_x)
```

the reader's range read once, and the closure range of every object in scope, whatever the planner estimates the reader holds.

### The owner graph

With `N_w` organizations in the walked subtree, `L_w` locations owned in it and `R_w` rows of all registered types owned or pinned under it:

```
rows read    = 2 N_w + L_w + R_w, each with one pair probe, and R_w object_info probes
rows written = N_w + (the nodes kept), to two temporary tables
```

### The claim graph

| Shape | Reads |
|---|---|
| Default | `Σ_{x ∈ V_u} G_x` derivations, `S_u` stored `user_users` rows and the pending moves of `T_u`, the `C` grants and claims, `Σ_c n_c` bindings |
| `start_id` | `|T_c|` closure entries, each with a pair probe and an `object_info` probe |
| Cache hit | one primary key probe and the array's elements |
| Cache validation | `1 + C' + C' + Σ_c n_c` entries for the `C'` grant rows of the user and the `n_c` bindings of each |
| `object_info` update | one range of `idx_user_object_object_user` to drop the holders' entries |

### The hit ratio

With `w` invalidating writes between two opens of the graph by one user, arriving independently of the opens, the fraction of opens served from the cache is `1 / (1 + w)`. Only writes that reach the user's own pairs, `user_users` rows, `samna_user` row, the `object_info` of objects the user holds, or the user's own grants, claims and bindings count in `w`; a claim write elsewhere in the app leaves the entry valid.

## Proofs and measurements

### Why the cache is validated by snapshot

A write that began before the build and committed after it is invisible to the snapshot the build took, so its `txid` fails `pg_visible_in_snapshot` and the entry is stale. A timestamp taken at the start of that write would be older than the build and could not show it. Every source of the default shape is either watched by a trigger that drops the entry or carries a `txid` the read tests, so a served entry equals a graph built at the read.

### Why the owner links read the pairs into an array

PostgreSQL estimates a scan of a common table expression from the expression and not from its rows, so a join of the closure to the reader's pair set, or an anti join of the deeper edges against the depth one edges held that way, can be planned as a nested loop over both. Reading the pairs once into an array and testing each edge by hashed membership, and keeping the least depth per tree by one window, keeps the cost at the reader's range plus the closure ranges whatever the statistics say.

### Measurements

1. Owner links over 3,660 organizations: 18,258 index entries and 55 ms, 18,267 on analyzed tables.

2. `graph_links_scale`, 3,660 organizations in three levels: the owner links read 21,821 index entries in 51 ms.
