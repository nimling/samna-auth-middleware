---
gate: samna-developer
layout: doc
title: Search
description: Search over everything a user can reach, inside an organization, a location or a parent, with or without a query. The read starts from the smallest of the reader's pairs, the scope's closure rows and the query's postings, a host adds its own rows through a registered search source, and every row passes the same check and carries the same field scope as a read of that object. The paths, the cost of each, the bench and how a result joins across sources and services.
tags: search, search_source, upsert_search_source, delete_search_source, search_hit, _search_scope_smaller, _search_text_smaller, search_vector, websearch_to_tsquery, ts_rank, _decayed_usage, user_object, object_info, inheritance_info, idx_object_info_search, idx_inheritance_info_ancestor, total_count, scope, federation
lastUpdated: true
---

# Search

Search answers one question: which objects the reader may see that match the query and sit inside the scope, best first. It is the intersection of three sets, each already stored and indexed by claimius, and it reads from the smallest of them.

```
result = top_p( A ∩ S ∩ Q , rank )

A  the reader's pairs        user_object primary key, one range per reader and type
S  the objects in the scope  inheritance_info, one range per ancestor in idx_inheritance_info_ancestor
Q  the query's matches       object_info.search_vector under the GIN index idx_object_info_search
```

## The surface

| Function | Answers |
|---|---|
| `search(user, [app,] query, object_types, required, limit, offset)` | The reader's objects matching the query, ranked |
| `search(user, [app,] query, object_types, required, object_ids, exclude_object_ids, owner_subtree_ids, parent_id, parent_type, limit, offset)` | The same inside an owner subtree or under a parent, with id lists, the reader's claims beside the objects when there is no scope, the rows of registered search sources, and `total_count` |
| `upsert_search_source(object_type, fn)` | Registers a host's search function for one of its registered types |
| `delete_search_source(object_type)` | Removes it |

On a disciple the app argument is absent. `limit` defaults to 20 and `offset` to 0. Each row carries the object, its type, name, description, link, the pair's mask and field scope, owner, location, whether it is held only by creation, its rank and, in the scoped overload, `total_count`.

## How it runs

1. `reconcile_if_pending` settles the reader's share of every area raised since its cursor, as a listing does, and `check_user_active` refuses an inactive reader; see [Materialization](./materialization.md#a-listing-settles-the-reader-s-share).

2. A query is parsed with `websearch_to_tsquery('simple', query)`: words, quoted phrases, `or`, a leading minus, no stemming, so names and codes in any language match as written. An empty query, or one with no lexeme, is no query: the search lists every accessible object in scope.

3. The read starts from the smallest set:

| Path | Chosen when | Reads |
|---|---|---|
| The scope's closure rows, each probed for the reader's pair | an owner subtree or a parent is set and `_search_scope_smaller` counts the scope's rows no more than the reader's pairs | `S` closure entries and one pair probe and one `object_info` row per object in scope |
| The query's postings, each probed for the reader's pair | a query without scope, when `_search_text_smaller` estimates the matches fewer than the reader's pairs | `M_q` postings and one pair probe per match |
| The reader's range | otherwise | two entries per pair of the requested types |

4. `_search_scope_smaller` counts both sets up to `k`, from 64 and four times larger each round, and stops at the first round where one falls short, so the choice costs at most a constant times the smaller set. `_search_text_smaller` compares the planner's estimates of the two and reads no index entry.

5. Every candidate keeps its pair's mask test against `required`, the grant `_best_grant` chooses from the derivations stored on the pair, and the creator rule, exactly as `get_objects` and `get_access` do; the scope and parent filters keep their meaning whichever path read the candidate.

6. A non empty query adds the rows of every registered search source of the requested types, each joined to the reader's pair by primary key and kept only when the pair passes the same test, and checked against the scope through the closure.

7. Without a scope, and when the types include `claimius.claim`, the claims the reader holds through a live grant are listed or matched beside the objects, as before.

8. One row per object is kept, the best ranked, and the rows are ordered by rank and then `object_id`, so offset paging tiles without overlap or skip. `total_count` is the number of rows before the limit.

## The rank

```
rank(u, x) = text(x, q) + _decayed_usage(used_count, last_used_at)
text(x, q) = ts_rank(search_vector, q)       for a match in object_info
           = score ∈ [0, 1]                  for a row of a search source
           = 0                               for an empty query
_decayed_usage(n, t) = n · 2^(−(now − t) / 1,209,600 s)
```

Among equal text matches what the reader opens most and lately comes first; an empty query lists by usage alone. The usage term is computed from two columns of the pair at read time, so the rank is never stored and never stale. A claim row is ranked on its text match alone.

## Search sources

A host searches more than a name and a description: a booking's attendees, a reading's text, a mapper's nodes. It registers one function per registered object type:

```sql
CREATE FUNCTION public.search_bookings(p_query TEXT, p_scope_ids UUID[])
RETURNS TABLE(object_id UUID, score REAL) AS $$
    SELECT b.id, ts_rank(b.attendee_vector, websearch_to_tsquery('simple', p_query))
    FROM public.booking b
    WHERE b.attendee_vector @@ websearch_to_tsquery('simple', p_query)
$$ LANGUAGE sql STABLE;

SELECT claimius.upsert_search_source('public.booking', 'public.search_bookings(text, uuid[])'::regprocedure);
```

1. The function takes `(text, uuid[])`, the query and the scope ids, and returns `(object_id, score)`; `upsert_search_source` refuses another signature with SQLSTATE `22023` and an unregistered type with `P0404`.

2. The function never decides access. Search joins each row to the reader's pair, keeps it only when the pair passes, and returns the pair's grant and field scope with it, so a source cannot widen what a reader sees.

3. A score is clamped to `[0, 1]`, so the rows of every source and of `object_info` rank on one scale.

4. Registering and removing a source is revoked from `claimius_reader`; a host does it in a migration.

## A joined result across services

Every service holds its own disciple of the same schema and answers the same call. A caller that searches Bookable, Connect and Sync at once asks each for its top `p` and merges the lists by rank.

```
top_p(⋃ services) ⊆ ⋃ top_p(service)
```

The merge is exact: an object in the overall top `p` is in the top `p` of its own service, because every service ranks with the same function on the same scale. The cursor of a joined page is the vector of each service's offset.

## What the implementing side still owns

Search decides which objects the reader may see and returns each with the pair's field scope. The implementing side renders them:

1. A search runs as the caller. A route that searches "what user X reaches" filters the caller's own results by X's access, behind `get_user_access` on X, never by searching as X.

2. Every field a response renders goes through the same redaction as the object's own read; see [Redaction](./redaction.md). A row hydrated beside a result, such as its organization or location, is a lookup by id and renders only what such a lookup may.

3. One question about one object is `get_access`, never `get_objects` filtered to one id.

## Cost

For a reader with `V` pairs of the requested types, a scope with `S` closure rows, a query matching `M_q` objects, `n` rows kept and a page of `p`:

```
entries     = s + c + min(2V, 3S, 2M_q) + n f
comparisons = n log₂ n
```

with `s` the settle of the reader's share, `c` the choice, at most `4 · min(V, S)` for the scope and nothing for the text, and `f` the closure probe of a source row or of a candidate the scope did not drive. `total_count` needs every candidate ranked, so page `o / p` costs the same `n` as the first.

The floor of any search is `p`, the rows it returns. Reading the reach and filtering it, `2V` whatever the scope, grows with what the reader can reach; this search grows with the smaller of the three sets.

| Reader | `V` | Scope `S` | Reach then scope | This search |
|---|---|---|---|---|
| A user | 5,000 | 500 | 10,000 | 1,500 |
| A broad holder | 10,000 | 500 | 20,000 | 1,500 |
| An administrator after ten broad changes | 100,000 | 500 | 200,000 | 1,500 |

### Against the other systems

| System | A search inside an organization |
|---|---|
| SpiceDB | `LookupResources`, about `1.1 V` reads, advised below 10,000 resources, intersected with a separate text index; or a check per text match, `9 M_q` reads and up to `M_q` round trips |
| OpenFGA, Auth0 FGA | `ListObjects` capped at 1,000 results by default, then the same intersection |
| Ory Keto | a check per candidate |
| Entra ID | no answer per object |
| Claimius | `min(2V, 3S, 2M_q)` entries in the database the request already reads, one statement, no round trip |

A tuple store derives the reader's set on every read, so it pays at least `V` before it can intersect. Claimius stores that set, so it can start from whichever of the three is smallest.

## Proofs and measurements

### The bench

`bench/disciple/bench.sql`, one reader holding 16,052 organizations through one claim on a root, a branch of 51 organizations under it, index entries counted by `pg_stat_get_xact_tuples_returned` over the claimius indexes:

| Step | Rows | Fresh tables | Analyzed tables |
|---|---|---|---|
| The reach, then the branch through the closure, `get_objects` and an `EXISTS` per row | 51 | 32,157 entries, 56 ms | 35 ms |
| `search` with no query inside the branch | 50 | 270 entries, 3.1 ms | 204 entries, 5.1 ms |
| `search` for `desk 7` inside the branch | 1 | 270 entries, 2.0 ms | 204 entries, 3.9 ms |
| `search` for `desk` without scope, 50 matches | 50 | 32,106 entries, 52 ms | 100 entries, 16.5 ms |

1. Inside a scope the search reads in proportion to the scope, 4 to 5 entries per object of the branch, whatever the reader's reach: 119 times fewer entries than the reach then the scope on fresh tables.

2. Without a scope the text path needs the planner's estimate of the matches. On analyzed tables it reads the 50 postings and their 50 pairs; on fresh tables the estimate is missing and the search reads the reader's range, as it did before.

### Why the scope is a read path and not a filter

The closure `inheritance_info` already holds one row per ancestor and descendant, and `idx_inheritance_info_ancestor` returns every descendant of one node as one range, with the descendant's type and id included. Starting from that range and probing the reader's primary key reads the scope once; filtering the reader's range by the scope reads the reach once and probes the closure once per pair. The two return the same rows, so the cheaper one is chosen per call.

### Why the text lives on the object

The name, description, owner and location of an object are the same for every user reaching it, so they are stored once in `object_info` and joined by key. On the pair they would cost `U_x` copies per object, and a rename would rewrite `U_x` rows. In `object_info` a rename writes one row and every reader's next search matches the new text. A host keeps no second copy of the same text: what a host searches beyond the name and description is its search source.

**Sources.** [PostgreSQL, Parsing Queries](https://www.postgresql.org/docs/17/textsearch-controls.html#TEXTSEARCH-PARSING-QUERIES). E. Demaine, A. López-Ortiz, J. I. Munro, *Adaptive set intersections, unions, and differences*, SODA 2000. R. Fagin, A. Lotem, M. Naor, *Optimal aggregation algorithms for middleware*, JCSS 2003. SpiceDB, [Protecting a list endpoint](https://authzed.com/docs/spicedb/modeling/protecting-a-list-endpoint). OpenFGA, [Relationship queries](https://openfga.dev/docs/interacting/relationship-queries). Ory Keto, [API overview](https://www.ory.com/docs/keto/concepts/api-overview).
