---
layout: doc
title: Search
description: Full text search over everything a user can reach, driven from the smaller of the user's own rows in user_object and the objects matching the query in object_info, ranked by text relevance and usage.
tags: search, search_vector, tsvector, websearch_to_tsquery, ts_rank, user_object, object_info
lastUpdated: true
---

# Search

`search(user, app, query, object_types, required, object_ids, exclude_object_ids, owner_subtree_ids, parent_id, parent_type, limit, offset)` returns ranked matches across every type the user reaches, each with its `user_claim_id`, and a `total_count` for paging.

## How it runs

1. The query is parsed with `websearch_to_tsquery('simple', query)`, the syntax users already type: words, quoted phrases, `or`, a leading minus ([PostgreSQL, Parsing Queries](https://www.postgresql.org/docs/17/textsearch-controls.html#TEXTSEARCH-PARSING-QUERIES)). The `simple` configuration does no stemming, so names and codes in any language match as written.

2. The text lives once per object, in `object_info.search_vector`, `setweight(to_tsvector('simple', sa_name), 'A') || setweight(to_tsvector('simple', sa_description), 'B')`, written with the object's own write, under a GIN index with one entry per object. The candidates come from whichever side is smaller: the reader's own range of the `user_object` primary key, each tested against its object's vector, or the objects matching the query through the GIN index, each probed into the reader's `user_object` by primary key. Both end in the reader's pair, so no row of another user is ever returned.

3. The claims the user holds are searched by name and description beside the objects, unless a subtree or parent filter is set.

4. `owner_subtree_ids` scopes to everything under given organizations, and `parent_id` with `parent_type` to everything under a location or a parenthood node, each a membership test on the candidate's up set in its `object_info` row, which the candidate already reads for its text.

5. Rows are ordered by `ts_rank × (1 + ln(1 + score))`, with `score` the usage score of [Usage](./usage.md), then by `object_id`, so offset paging tiles without overlap.

## Cost

For a user reaching `V_u` objects, a query matching `M_q` objects of the app, and a page of `l` rows after `o`, with the constants of [Scaling](./scaling.md):

```
n    = V_u or M_q, whichever side the cheaper term below reads
c_1  = t_d + t_m        a row of the reader's range: its object_info probe and the text test
c_2  = t_g + t_d        a match of the GIN index: its posting and the reader's primary key probe
cost = min(V_u c_1, M_q c_2) + n (t_rank + t_h) + 2 n log₂(o + l) × cpu_operator_cost
```

`t_m` and `t_rank` are the text test and `ts_rank` on a short vector, estimated at about 1 µs each, `t_g` a posting entry, well under a descent, and `t_h` the heap fetch of the pair's usage columns, a cached tuple or one random read. Both paths cost about one descent a candidate, so the planner takes the reader's range when `V_u < M_q` and the text index when `M_q < V_u`, and a search costs about 5 µs of calibration time per candidate whichever it takes: 0.25 ms for a query matching 50 objects, 100 ms for one matching all of a reader's 20,000, each divided by `σ` on a machine, 0.45 ms and 180 ms on a `D2ds_v5`.

Measured with the text on every pair and only the first path: 0.44 ms for a user reaching 288 rows; 70 to 199 ms for a user reaching 20,000 rows with a query that matches all of them, the cost being 15,778 heap reads of 606 byte rows. On the first path the reader's range is now index only, `⌈V_u / 87⌉` leaf pages, and the object side is `object_info`, one row per object, cached long before the per user index is.

A ranked page needs every candidate ranked, so page `o / l` costs the same `n` as the first; the total count comes with it at no extra cost. A client that reads many pages narrows the query instead, which lowers `M_q` and with it `n`.

Why the text is per object: a text index across pairs would return every user's matching rows and discard all but the reader's, `M_q × Ū_o` entries for `M_q` answers. Across objects it returns each object once, and the reader's primary key keeps only what the reader reaches. The planner picks the path from the estimated `M_q` against the reader's range, which is the inequality above.

## Rendering fields

`sa_name`, `sa_description` and `sa_link` live in `object_info`, so a search result renders without reading the implementing side's table. A rename writes the one `object_info` row, and every reader sees it on the next read, with no area and no settle.
