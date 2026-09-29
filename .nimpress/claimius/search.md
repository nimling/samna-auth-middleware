---
layout: doc
title: Search
description: Full text search over everything a user can reach, answered from the search vector each object carries once in object_info, with the entries each of its two plans reads and why the text lives on the object.
tags: search, search_vector, tsvector, websearch_to_tsquery, ts_rank, user_object, object_info, idx_object_info_search
lastUpdated: true
---

# Search

`search(user, app, query, object_types, required, object_ids, exclude_object_ids, owner_subtree_ids, parent_id, parent_type, limit, offset)` returns ranked matches across every type the user reaches, each with its `user_claim_id`, and a `total_count` for paging.

## How it runs

1. The query is parsed with `websearch_to_tsquery('simple', query)`, the syntax users already type: words, quoted phrases, `or`, a leading minus ([PostgreSQL, Parsing Queries](https://www.postgresql.org/docs/17/textsearch-controls.html#TEXTSEARCH-PARSING-QUERIES)). The `simple` configuration does no stemming, so names and codes in any language match as written.

2. The text lives once per object: `object_info.search_vector` is generated as `setweight(to_tsvector('simple', name), 'A') || setweight(to_tsvector('simple', description), 'B')` from the `name` and `description` the calc triggers write to `object_info` when the object is written, under the GIN index `idx_object_info_search`.

3. The candidates are the reader's pairs whose object's vector matches, filtered by type, by the id lists, by the mask, and, when given, by a subtree: `owner_subtree_ids` keeps a pair whose object has an ownership ancestor in the list, `parent_id` one whose object sits under the node in the location or parenthood tree, one probe of `inheritance_info` per candidate.

4. Without a subtree filter the reader's claims are searched by name and description beside the objects, each claim's vector computed at read time.

5. Rows are ordered by `ts_rank`, then `object_id`, so offset paging tiles without overlap, and `count(*) OVER ()` gives the total.

## Cost

For a user reaching `V_u` objects, a query matching `M_q` objects of the app each reached by `U_x` users, and a page of `l` rows after `o`, the planner takes the cheaper of two paths to the candidates:

| Path | Entries read |
|---|---|
| The reader's range of the `user_object` primary key, each pair's `object_info` row probed and its vector tested | `2 V_u` rows |
| The query's postings in `idx_object_info_search`, each probed for the reader's pair | `2 M_q`: a posting and a primary key probe per match |

Then every candidate `n` kept is ranked and chooses its grant, and the whole set is sorted:

```
entries = Δ_a + 1 + min(2 V_u, 2 M_q) + n (2 G + s)          comparisons = n log₂ n
```

with `s` the subtree probes, one per candidate when a subtree filter is set, and `C` more rows for the claim branch. A ranked page needs every candidate ranked and counted, so page `o / l` costs the same `n` as the first. A client that reads many pages narrows the query instead, which lowers `n`.

The text index spans objects, not pairs, so a match costs one posting and one probe of the reader's primary key, whatever the number of users reaching the object. The reader's range costs one pair and one `object_info` row per object the reader reaches, whatever the query. The planner picks by its estimate of `M_q` against `V_u`, which is the inequality between the two rows of the table.

## Why the text lives on the object

The text, name, owner and location of an object are the same for every user reaching it, so they are stored once in `object_info` and joined by key, one probe per candidate. Stored on the pair they cost `U_x` copies per object and a rename that rewrites `U_x` rows or raises an area for every reader. In `object_info` a rename is one row, raises no area, and every reader's next search matches the new text; see [Materialization](./materialization.md#why-the-fields-of-the-object-live-once).
