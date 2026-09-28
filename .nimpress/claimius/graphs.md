---
layout: doc
title: Graphs
description: The owner graph and the claim graph, their node shape, depth and width windows, next_id paging, ordering by name or usage, the link functions, and the cache of the default claim graph.
tags: get_owner_graph, get_claim_graph, get_graph_links_owner, get_graph_links_claim, claim_graph_cache, width, depth, next_id, sort
lastUpdated: true
---

# Graphs

Two functions render what a user can reach as a graph of flat nodes. Every node has five columns: `object_id`, `object_type` schema qualified, `label`, `parent_id`, and a `data` payload carrying at least `access` and `level`. Grouping on `parent_id` draws the tree with no further query.

| Function | Draws |
|---|---|
| `get_owner_graph(user, app, start_id, depth, required, width, sort, next_id)` | Organizations, locations under their owners, and every registered row under its parent, location or owner, in that order of priority |
| `get_claim_graph(user, app, start_id, depth, required, width, sort, next_id)` | The user, the users the user can see, the user's grants, the claims, their bindings, and the objects each binding reaches |
| `get_graph_links_owner`, `get_graph_links_claim` | The edges between nodes as `{source, target, relation}`, a direct edge winning over a deeper edge to the same target |

The registered types a graph covers are read from `table_info`, so an implementing side's tables appear the moment they are registered and nothing about them is written into a graph function.

## Windows

A graph of a large tree is read a window at a time.

1. `start_id` roots the window at one organization for the owner graph, one claim for the claim graph.

2. `depth` bounds the levels below the start, 0 meaning all. Each level is one join on `inheritance_info` at `depth = level`, index only.

3. `width` bounds the children kept per parent and type, 0 meaning all. Children are numbered with `row_number() OVER (PARTITION BY parent_id, object_type ORDER BY ...)` and kept while the number is at most `width`.

4. `sort` orders siblings: `name` by label then `object_id`, `usage` by the usage score of [Usage](./usage.md) then label then `object_id`.

5. `next_id` continues a parent's children after a given child: the window keeps the siblings whose sort key is greater than the key of `next_id`, compared as a row value `(sort_key, label, object_id)`. The caller sets `start_id` to the parent and `next_id` to the last child it holds, and gets the next `width` children.

Why keyset paging: an offset reads again every skipped sibling on every page; a row value comparison on the sort key starts where the last page ended. The cost of a page is `O(width)` after the siblings of one parent are read, whatever page it is.

## Cost

A window rooted at `start_id` with depth `d` reads the closure rows under the start to depth `d`, index only through `idx_inheritance_info_ancestor`, the reader's `user_object` rows for them by primary key, and their labels from `object_info`, one row per object shared by every reader. Measured: the owner graph of a user reaching 10 organizations and 288 rows in 4.5 ms; the claim graph of the same user in 4.3 ms; the owner graph from the root to depth 2 for a user reaching everything in 43 ms.

## The claim graph cache

The default shape of the claim graph, no start, unlimited depth, no mask, is stored per user in `claim_graph_cache` as one jsonb array and served from it. A trigger drops the user's entry when one of the user's `user_object` rows, `user_users` rows or the user's `samna_user` row changes, and drops the app's entries when a claim or binding of the app changes. Windows are cut from the cached array with the same numbering as above.
