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

2. `depth` bounds the levels below the start, 0 meaning all. Level `d` below a node `y` is the set of objects whose up set holds `y` at position `d`, read from the posting list of `y` in `idx_object_info_ancestors`, and level 1 from the owner and location indexes of `object_info` in name order.

3. `width` bounds the children kept per parent and type, 0 meaning all. Children are numbered with `row_number() OVER (PARTITION BY parent_id, object_type ORDER BY ...)` and kept while the number is at most `width`.

4. `sort` orders siblings: `name` by label then `object_id`, `usage` by the usage score of [Usage](./usage.md) then label then `object_id`.

5. `next_id` continues a parent's children after a given child: the window keeps the siblings whose sort key is greater than the key of `next_id`, compared as a row value `(sort_key, label, object_id)`. The caller sets `start_id` to the parent and `next_id` to the last child it holds, and gets the next `width` children.

Why keyset paging: an offset reads again every skipped sibling on every page; a row value comparison on the sort key starts where the last page ended. The cost of a page is `O(width)` after the siblings of one parent are read, whatever page it is.

## Cost

A window rooted at `start_id` with depth `d` and width `W`, over a tree of fan out `F`, returns `n_w = Σ_{i=1..d} min(F, W)^i` nodes. To keep the best `W` children of a parent it must rank them, so it reads rows

```
n_r = Σ_{i=0..d−1} min(F, W)^i × r(F)          r(F) = F  ranked by usage
                                               r(F) = min(F, W / v)  ranked by name
```

each at `t_n ≈ 2 t_d`, the reader's `user_object` probe and the node's `object_info` row, which also carries its up set, plus the sort. `v` is the share of a parent's children the reader can see.

1. Ranked by usage the score is the reader's own, so no index can order the children and every one of the `F` is read. The window costs in the fan out of what it expands.

2. Ranked by name the order is the same for every reader, so `object_info` carries it: `(sa_owner_id, sa_name, object_id)` and `(sa_location_id, sa_name, object_id)` give the children of an owner or a location in label order, and the window reads them in that order, probing the reader's primary key for each, until `W` are visible: `W / v` rows. Those two indexes replace the single column indexes on `sa_owner_id` and `sa_location_id`, so an `object_info` write touches as many indexes as before. The gain is `F v / W`: an organization holding 10,000 rows shown fifty at a time reads 50 rows instead of 10,000, 0.25 ms of calibration time instead of 50 ms.

3. `next_id` continues from the row value `(sa_name, object_id)` of the last child, so every page costs `W / v`, whatever page it is.

Measured on the older shape: the owner graph of a user reaching 10 organizations and 288 rows in 4.5 ms; the claim graph of the same user in 4.3 ms; the owner graph from the root to depth 2 for a user reaching everything in 43 ms.

## The claim graph cache

The default shape of the claim graph, no start, unlimited depth, no mask, is stored per user in `claim_graph_cache` as one jsonb array with the time it was built, and served from it. The entry is dropped when one of the user's `user_object` rows, `user_users` rows or the user's `samna_user` row changes, all of which are written per user. A change to a claim or a binding is not propagated to the entries at write time, because the holders of a claim are exactly the users a write must never enumerate. The read validates instead: it compares the entry's build time with the latest `sa_updated_at` among the user's `user_claim` rows, the `C` claims they grant and those claims' `claim_object` rows, soft deleted ones included, one range of `idx_user_claim_user_app`, `C` claim probes and `C` binding ranges, and rebuilds when one is newer. Windows are cut from the cached array with the same numbering as above.

Why validation on read: with `λ_c` the rate of claim and binding changes in the app, `N_c` its claims and `r_g` the rate at which one user opens the claim graph, dropping every entry of the app on every change gives a hit ratio of `r_g / (r_g + λ_c)` and costs a delete per cached user on each change; validating against the user's own claims gives `r_g / (r_g + λ_c C / N_c)` and costs nothing on a write. An app with a change a minute across 1,000 claims, whose users open the graph once an hour and hold five claims, gets 1.6 % hits the first way and 77 % the second.
