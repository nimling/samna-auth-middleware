---
layout: doc
title: Search
description: Full text search over everything a user can reach, matched against the search vector each object carries once in object_info and ranked by text match plus the reader's decayed usage, with the entries each of its two plans reads.
tags: search, search_vector, tsvector, websearch_to_tsquery, ts_rank, _decayed_usage, used_count, user_object, object_info, idx_object_info_search, total_count
lastUpdated: true
---

# Search

Two overloads of `search` return ranked matches across every type the user reaches, each row with its `user_claim_id`:

| Overload | Adds |
|---|---|
| `search(user, app, query, object_types, required, limit, offset)` | Nothing: the reader's objects, ranked |
| `search(user, app, query, object_types, required, object_ids, exclude_object_ids, owner_subtree_ids, parent_id, parent_type, limit, offset)` | Id lists, a subtree filter, the reader's claims beside the objects, and `total_count` |

## How it runs

1. `reconcile_if_pending(app, u)` settles the reader's share of every area raised since its cursor, the object areas of its roots and its own assignments, all of them in one pass, as a listing does; see [Materialization](./materialization.md#a-listing-settles-the-readers-share).

2. The query is parsed with `websearch_to_tsquery('simple', query)`, the syntax users already type: words, quoted phrases, `or`, a leading minus ([PostgreSQL, Parsing Queries](https://www.postgresql.org/docs/17/textsearch-controls.html#TEXTSEARCH-PARSING-QUERIES)). The `simple` configuration does no stemming, so names and codes in any language match as written.

3. The text lives once per object: `object_info.search_vector` is generated as `setweight(to_tsvector('simple', sa_name), 'A') || setweight(to_tsvector('simple', sa_description), 'B')` from the fields the calc triggers write when the object is written, under the GIN index `idx_object_info_search`.

4. The candidates are the reader's pairs whose object's vector matches, filtered by type, by the id lists, by the mask `(sa_access & required) = required`, and by a pair whose grant survives its window or which the reader created. With `owner_subtree_ids` a candidate is kept when an ownership ancestor is in the list, and with `parent_id` and `parent_type` when it sits under that node in the location or parenthood tree: one probe of `inheritance_info` per candidate.

5. In the second overload, without a subtree filter, the reader's claims are searched by name and description beside the objects, their vector computed at read time from the `C` claims the reader holds inside their window.

6. Rows are ranked, ordered by rank and then `object_id`, so offset paging tiles without overlap or skip, and `count(*) OVER ()` returns the total before the limit.

## The rank

```
rank(u, x) = ts_rank(search_vector, q) + _decayed_usage(used_count, last_used_at)
_decayed_usage(n, t) = n · 2^(−(now − t) / 1,209,600 s)
```

The usage term is the pair's count of successful single object checks, halved for every 14 days since the latest one, and 0 for a pair never used; see [Usage](./usage.md). Among equal text matches what the reader opens most and lately comes first, and a count from months ago weighs almost nothing. The term is computed at read time from two columns of the pair, so the rank is never stored and never stale.

## What it reads

For a user reaching `V_u` objects, a query matching `M_q` objects each reached by `U_x` users, and a page of `l` rows after `o`, the planner takes the cheaper of two paths to the candidates:

| Path | Entries read |
|---|---|
| The reader's range of the `user_object` primary key, each pair's `object_info` row probed and its vector tested | `2 V_u` |
| The query's postings in `idx_object_info_search`, each probed for the reader's pair in the primary key | `2 M_q`, a posting and a probe per match |

Every candidate `n` kept then chooses its grant, `2 G` probes, and the whole set is ranked and sorted:

```
entries = s + 1 + min(2 V_u, 2 M_q) + n (2 G + f)          comparisons = n log₂ n
```

with `s` the settle of the reader's share, `f` the subtree probe, one per candidate when a subtree filter is set, and `2C` more for the claim branch. A ranked page needs every candidate ranked and counted, so page `o / l` costs the same `n` as the first; a client that reads far narrows the query, which lowers `n`.

The text index spans objects, not pairs, so a match costs one posting and one probe of the reader's primary key whatever `U_x` is. The reader's range costs one pair and one `object_info` row per object the reader reaches, whatever the query. The planner picks by its estimate of `M_q` against `V_u`, the inequality between the two rows of the table.

## Why the text lives on the object

The name, description, owner and location of an object are the same for every user reaching it, so they are stored once in `object_info` and joined by key, one probe per candidate. On the pair they would cost `U_x` copies per object, and a rename would rewrite `U_x` rows or raise an area for every holder. In `object_info` a rename writes one row and raises no area, and every reader's next search matches the new text.
