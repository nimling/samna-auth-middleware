---
layout: doc
title: Graphs
description: The owner graph and the claim graph, their node shape and depth window, the rows each reads as a function of the tree and the reader, the link functions, and the per user cache of the default claim graph with its hit ratio.
tags: get_owner_graph, get_claim_graph, get_graph_links_owner, get_graph_links_claim, claim_graph_cache, depth
lastUpdated: true
---

# Graphs

Two functions render what a user can reach as a graph of flat nodes. Every node has five columns: `object_id`, `object_type` schema qualified, `label`, `parent_id`, and a `data` payload carrying the reader's `access` and `scope` and the node's own fields. Grouping on `parent_id` draws the tree with no further query.

| Function | Draws |
|---|---|
| `get_owner_graph(user, app, start_id, depth, required)` | Organizations under their owners, locations under their parents or owners, and every registered row under its parent, location or owner |
| `get_claim_graph(user, app, start_id, depth, required)` | The user, the users the user can see, the user's grants, the claims, their bindings, and the objects each binding reaches |
| `get_graph_links_owner`, `get_graph_links_claim` | The edges between nodes as one jsonb array, filtered by type |

The registered types a graph covers are read from `table_info`, so an implementing side's tables appear the moment they are registered and nothing about them is written into a graph function.

## The owner graph

1. The roots: the app's root organizations, or the one given as `start_id`, kept when the reader reaches them, one `get_objects` range over the reader's organizations.

2. The walk: every live organization below the roots to `depth` levels, 0 meaning all, one recursive query over `idx_organization_sa_owner_id`. Each organization walked probes the reader's pair and is kept when the reader reaches it.

3. The walked organizations are written to a transaction temporary table, and the locations owned by them and every registered row owned by them or pinned in their locations are read type by type, each joined to the reader's pair and kept when the mask covers `required`. A registered row takes its name, description, owner and location from its `object_info` row, and the rows of one type come in `(sa_owner_id, sa_name, id)` order, the order of `idx_object_info_owner_name`, whose twin `idx_object_info_location_name` serves the same window under a location.

With `N_w` organizations in the walked subtree and `R_w` rows of all registered types owned or pinned under it:

```
rows read    = N_w + R_w, each with one pair lookup
rows written = N_w, to the temporary table
```

The walk reads the subtree, not the reader's reach: an organization the reader cannot see is read and dropped. `depth` and `start_id` are the window that bounds it; a reader who reaches little of a wide tree should start the graph at the node it reaches.

## The claim graph

For the default shape, no start, unlimited depth, no mask, the graph is built from the reader's pairs: every pair and every derivation in its `grants` that names one of the reader's claims, `Σ_{x ∈ V_u} G_x` elements, plus the `S_u` users the reader sees, the reader's `C` grants and their claims and bindings. With `start_id` set it reads the claim's bindings and, through `idx_inheritance_info_ancestor`, the objects each reaches, `|T_c|` rows, to `depth`.

## The claim graph cache

The default shape is stored per user in `claim_graph_cache` as one jsonb array, and served from it: one primary key probe and the array's elements. The entry is dropped by triggers on the user's own rows:

| Write | Drops |
|---|---|
| Any row of `user_object` or `user_users` of the user, or the user's `samna_user` row | That user's entry, one probe |
| An `object_info` row updated, a rename or a move | The entry of every user holding a pair on the object, one range of `idx_user_object_object_user` per app |

A change of the access of a pair drops the entry; a usage flush, which moves only `used_count` and `last_used_at`, does not. A claim or binding change drops nothing: the entry stores the `pg_current_snapshot()` taken before its graph was read, and the read serves it only while every `user_claim` row of the user, every claim it names and every binding of those claims carries a `txid` visible in that snapshot, `1 + 2C` probes over the user's grants. `txid` is the local transaction that last wrote the row, stamped by `tg_update_txid`, so a write that began before the build and committed after it is invisible to the snapshot and makes the entry stale, which a timestamp taken at the start of that write could not show.

With `w` invalidating writes between two opens of the graph by one user, arriving independently of the opens, the fraction of opens served from the cache is `1 / (1 + w)`. Only writes to the user's own grants, claims and pairs count in `w`: a claim write elsewhere in the app leaves the entry valid.

## The window

Both graphs take `p_width`, `p_sort` and `p_next_id`. Every parent shows at most `p_width` children of each type, 0 meaning all, in name order or with `p_sort` `usage` by the reader's `used_count × 2^(−age / 14 days)`, computed at read time. A node is kept when it is inside its parent's window and its parent is kept. `p_next_id` continues after that node with its later siblings of its type. Name order is the order `idx_object_info_owner_name` and `idx_object_info_location_name` serve.
