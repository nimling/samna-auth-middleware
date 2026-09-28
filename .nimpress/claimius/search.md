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

4. `owner_subtree_ids` scopes to everything under given organizations, and `parent_id` with `parent_type` to everything under a location or a parenthood node, each one `inheritance_info` probe per candidate.

5. Rows are ordered by `ts_rank × (1 + ln(1 + score))`, with `score` the usage score of [Usage](./usage.md), then by `object_id`, so offset paging tiles without overlap.

## Cost

For a user reaching `V_u` objects and a query matching `M_q` objects of the app:

```
cost = min( V_u × (probe of object_info + text test),  M_q × probe of user_object )
```

Measured with the text on every pair and only the first path: 0.44 ms for a user reaching 288 rows; 70 to 199 ms for a user reaching 20,000 rows with a query that matches all of them, the cost being 15,778 heap reads of 606 byte rows. On the first path the reader's range is now index only, `⌈V_u / 87⌉` leaf pages, and the object side is `object_info`, one row per object, cached long before the per user index is. A selective query, `M_q ≪ V_u`, takes the second path and costs `M_q` probes whatever the reader reaches.

Why the text is per object: a text index across pairs would return every user's matching rows and discard all but the reader's, `M_q × Ū_o` entries for `M_q` answers. Across objects it returns each object once, and the reader's primary key keeps only what the reader reaches. The planner picks the path from the estimated `M_q` against the reader's range, which is the inequality above.

## Rendering fields

`sa_name`, `sa_description` and `sa_link` live in `object_info`, so a search result renders without reading the implementing side's table. A rename writes the one `object_info` row, and every reader sees it on the next read, with no area and no settle.
