---
layout: doc
title: Search
description: Full text search over everything a user can reach, answered from the search vector each user_object pair carries, with the entries each of its two plans reads and why the text lives on the pair.
tags: search, search_vector, tsvector, websearch_to_tsquery, ts_rank, user_object, idx_user_object_search
lastUpdated: true
---

# Search

`search(user, app, query, object_types, required, object_ids, exclude_object_ids, owner_subtree_ids, parent_id, parent_type, limit, offset)` returns ranked matches across every type the user reaches, each with its `user_claim_id`, and a `total_count` for paging.

## How it runs

1. The query is parsed with `websearch_to_tsquery('simple', query)`, the syntax users already type: words, quoted phrases, `or`, a leading minus ([PostgreSQL, Parsing Queries](https://www.postgresql.org/docs/17/textsearch-controls.html#TEXTSEARCH-PARSING-QUERIES)). The `simple` configuration does no stemming, so names and codes in any language match as written.

2. The text lives on every pair: `user_object.search_vector` is `setweight(to_tsvector('simple', name), 'A') || setweight(to_tsvector('simple', description), 'B')`, written by `recompute_user_object` from the object's `name` and `description`, under the GIN index `idx_user_object_search`.

3. The candidates are the reader's pairs whose vector matches, filtered by type, by the id lists, by the mask, and, when given, by a subtree: `owner_subtree_ids` keeps a pair whose object has an ownership ancestor in the list, `parent_id` one whose object sits under the node in the location or parenthood tree, one probe of `inheritance_info` per candidate.

4. Without a subtree filter the reader's claims are searched by name and description beside the objects, each claim's vector computed at read time.

5. Rows are ordered by `ts_rank`, then `object_id`, so offset paging tiles without overlap, and `count(*) OVER ()` gives the total.

## Cost

For a user reaching `V_u` objects, a query matching `M_q` objects of the app each reached by `U_x` users, and a page of `l` rows after `o`, the planner takes the cheaper of two paths to the candidates:

| Path | Entries read |
|---|---|
| The reader's range of `idx_user_object_user_type`, each pair's vector tested | `V_u` heap rows |
| The query's postings in `idx_user_object_search`, each kept when it is the reader's | `Σ_{x matching} U_x ≈ M_q Ū_o` postings |

Then every candidate `n` kept is ranked and chooses its grant, and the whole set is sorted:

```
entries = Δ_a + 1 + min(V_u, M_q Ū_o) + n (2 G + s)          comparisons = n log₂ n
```

with `s` the subtree probes, one per candidate when a subtree filter is set, and `C` more rows for the claim branch. A ranked page needs every candidate ranked and counted, so page `o / l` costs the same `n` as the first. A client that reads many pages narrows the query instead, which lowers `n`.

The text index spans the pairs of every user, so a match costs its postings once per user reaching the object, `Ū_o` per match, of which the reader keeps one. The reader's range costs one heap row per object the reader reaches, whatever the query. The planner picks by its estimate of `M_q Ū_o` against `V_u`, which is the inequality between the two rows of the table.

## Why the text lives on the pair

A search answered from the pair needs no join: the row that proves the reader reaches the object also carries its text, its name for the result and its owner and location for the filters. The cost is the text stored `U_x` times per object, and a rename written to the actor's pair at once and to the others' at the next settle that covers the object; see [Materialization](./materialization.md#why-the-pair-carries-the-fields-of-the-object).
