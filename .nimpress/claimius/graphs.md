---
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
| `get_owner_graph(user, app, start_id, depth, required, width, sort, next_id)` | Organizations under their owners, locations under their parents or owners, and every registered row under its parent, location or owner |
| `get_claim_graph(user, app, start_id, depth, required, width, sort, next_id)` | The user, the users the user sees, the user's grants, the claims, their bindings, and the objects each binding reaches |
| `get_graph_links_owner`, `get_graph_links_claim` | The edges between the reader's nodes as one jsonb array, filtered by included and excluded types, read from the pairs as they stand, and windowed by the same `width`, `sort` and `next_id` |

The registered types a graph covers are read from `table_info`, so an implementing side's tables appear the moment they are registered and nothing about them is written into a graph function. Both graphs begin with `reconcile_if_pending(app, u)` and `check_user_active`, as a listing does.

## The window

Both graphs take `p_width`, `p_sort` and `p_next_id`, applied by `_window_graph` to the graph once built:

1. Every parent shows at most `p_width` children of each type, 0 meaning all. A node whose parent is not in the graph is a top node, and the top nodes are the children of no parent.

2. Children come in name order, `label` then `object_id`, or with `p_sort = 'usage'` by the reader's `_decayed_usage(used_count, last_used_at)` of the pair, heaviest first, then name; see [Usage](./usage.md).

3. A node is kept when it is inside its parent's window and its parent is kept.

4. With `p_next_id` the window continues after that node: its later siblings of its type, `p_width` of them, each with its windowed subtree. The node itself is not returned, and an id not in the graph returns nothing.

Any other `p_sort`, or a negative `p_width`, is refused with SQLSTATE `22023`. With `w` the width, `τ` the types under a parent and `k` the levels, a window returns at most `Σ_{i=1}^{k} (w τ)^i` nodes below the top, whatever the tree holds. The window bounds the rows returned and sent, not the rows read: the graph is built whole and windowed after, and with `p_sort = 'usage'` each node costs one probe of the reader's pair. With `p_width = 0` and no `p_next_id` the graph is returned as built.

The link graphs take the same three arguments after `p_depth`, `p_width INTEGER DEFAULT 0`, `p_sort TEXT DEFAULT 'name'` and `p_next_id UUID DEFAULT NULL`, refused the same way, and `_window_links` windows the links as `_window_graph` windows the nodes: every link target is a node whose parent is the link source, a source no link targets is a top node, and a link is kept when its target is kept under its source. Each type is matched back to the registered type it normalizes from, and labels come from `object_info`, the claim or the `samna_user` row, so a node graph and a link graph drawn with one window show the same nodes in the same order. The graph's own nodes join too, the objects in scope of the owner links and the reader's claims and `samna_user` row of the claim links, and one no link targets is a top node, so a root or a start that keeps no child is windowed like any other. A windowed link graph returns `node_ids` beside `links`, the ids of every kept node: a kept node that keeps no child has no link, so a consumer builds the window's object set from `node_ids`, not from the link endpoints. Without a window the link graph returns `links` alone.

## The owner graph

1. The roots: the app's root organizations, `app_object_ids`, or the one given as `start_id`, kept when the reader reaches them through `get_objects` over organizations.

2. The walk: every live organization below the roots to `depth` levels, 0 meaning all, one recursive query over `idx_organization_sa_owner_id`, each kept when `get_objects` returns it for the reader and joined to the reader's pair for its `access` and `scope`. The walked organizations are written once more to a transaction temporary table `_owner_graph_orgs`.

3. The locations owned by the walked organizations, kept when the reader reaches them, with `longitude` and `latitude` in `data`.

4. For every registered type with an owner or a location, `table_info` read once, the live rows owned by the walked organizations or pinned in their locations, each joined to the reader's pair with the mask covering `required` and to its `object_info` row for name, description, owner and location, in `(sa_owner_id, sa_name, id)` order, the order of `idx_object_info_owner_name`. Each row's parent is its own parent, then its location, then its owner.

With `N_w` organizations in the walked subtree, `L_w` locations owned in it and `R_w` rows of all registered types owned or pinned under it:

```
rows read    = 2 N_w + L_w + R_w, each with one pair probe, and R_w object_info probes
rows written = N_w + (the nodes kept), to two temporary tables
```

The walk reads the subtree, not the reader's reach: an organization the reader cannot see is read and dropped. `start_id` and `depth` bound the rows read; the window bounds the rows returned.

## The claim graph

For the default shape, no `start_id`, `depth = 0` and `required = 0`, the graph is built from the reader's pairs: every derivation in the `grants` of the reader's `V_u` pairs that names one of the reader's claims, `Σ_{x ∈ V_u} G_x` elements, each joined to the binding it came through and to its `object_info` row; the `S_u` users the reader sees through `user_users`, each with its `samna_user` row and the reader's pair on it; the reader's `C` grants and claims; and the bindings of those claims, `Σ_c n_c`, each with the reader's pair and `object_info` row on its object.

With `start_id` the graph reads that claim's bindings and, through `idx_inheritance_info_ancestor`, the objects each reaches to `depth`, where the claim and the binding both inherit: `|T_c|` closure entries, each with the reader's pair and its `object_info` row. `depth = 1` keeps the directly bound objects only.

## The claim graph cache

The default shape is stored per user in `claim_graph_cache (app_id, user_id, graph, snapshot)` as one jsonb array, and served from it: one primary key probe and the array's elements, then the window.

### What drops an entry

| Write | Drops |
|---|---|
| A pair of the user inserted, deleted, or updated in `sa_access`, `grants`, `scope` or `direct_grant` | That user's entry, one probe |
| A `user_users` row of the user as viewer, or the user's `samna_user` row | That user's entry, one probe |
| An `object_info` row updated, a rename or a move | The entry of every user holding a pair on the object, one range of `idx_user_object_object_user` |

A usage flush moves only `used_count` and `last_used_at`, which the trigger does not watch, so it drops nothing.

### What validates an entry

A claim or binding change drops nothing: the entry is validated on read. `snapshot` is the `pg_current_snapshot()` taken before the graph's sources were read. `tg_update_txid` stamps `txid = pg_current_xact_id()` on every insert and update of `claim`, `user_claim` and `claim_object`, under replay too, so `txid` names the local transaction that last wrote the row. The read serves the entry only while every `user_claim` row of the user, the claim of each and every binding of those claims in the app carries a `txid` visible in the snapshot, `pg_visible_in_snapshot(txid, snapshot)` ([PostgreSQL, Transaction ID and Snapshot Information Functions](https://www.postgresql.org/docs/17/functions-info.html#FUNCTIONS-PG-SNAPSHOT)):

```
entries(validate) = 1 + C' + C' + Σ_{c} n_c
```

for the `C'` grant rows of the user, soft deleted ones included, and the `n_c` bindings of each. A write that began before the build and committed after it is invisible to the snapshot and makes the entry stale, which a timestamp taken at the start of that write could not show. A stale entry is rebuilt and stored with a new snapshot.

### The hit ratio

With `w` invalidating writes between two opens of the graph by one user, arriving independently of the opens, the fraction of opens served from the cache is `1 / (1 + w)`. Only writes that reach the user's own pairs, `user_users` rows, `samna_user` row, the `object_info` of objects the user holds, or the user's own grants, claims and bindings count in `w`; a claim write elsewhere in the app leaves the entry valid.
