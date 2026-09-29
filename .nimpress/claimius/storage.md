---
layout: doc
title: Storage
description: The rows and bytes each claimius table holds as functions of the data, the indexes each carries and the index entries each write costs, the shape of the B-trees a read descends, and the dead rows each maintenance step leaves.
tags: storage, index, btree, gin, deduplication, user_object, inheritance_info, user_users, sync_event, access_diff, vacuum
lastUpdated: true
---

# Storage

## Rows per table

| Table | Rows | Grows with |
|---|---|---|
| `user_object` | `P = Σ_u V_u = O Ū_o` | users times reach |
| `user_users` | `Σ_u S_u ≤ U (U − 1)` | pairs of users sharing an object |
| `inheritance_info` | `Σ_x Σ_{t ∋ x} (d_t(x) + 1)` | objects times depth |
| `claim`, `claim_object`, `user_claim` | claims, bindings, grants | the model, never its product |
| `access_diff` | at most one per claim and one per object raised since the last read in the app | writes between reads |
| `sync_event` | events since the lowest live cursor | source rows written; see [Sync](./sync.md) |
| `audit` | audited events | writes |

On the data set of [the index](./index.md#symbols), `user_object` held 393,908 rows averaging 606 bytes of heap, of which 119 were the search vector and 239 the `grants` array, and `user_users` 111,472 rows of about 120 bytes. The pairs dominate: they are the only table that grows with the product of users and objects, `P = O Ū_o`, and every other table grows with the rows of the model or with its depth.

A closure row is two object pairs, a root, a depth and two timestamps: about 140 bytes of heap and three index entries of about 100 bytes each, so an object at depth `d` in one tree costs about `(d + 1) × 440` bytes of closure.

## Indexes and entries per write

| Table | Indexes | Entries per row written |
|---|---|---|
| `user_object` | primary key; `(user_id, app_id, object_type, sa_access)`; `(app_id, object_type, object_id, user_id)`; `(app_id, sa_owner_id)`; `(app_id, sa_location_id)` partial; `(app_id, sa_root_id)`; GIN `jsonb_path_ops` on `grants`; GIN on `search_vector` | 5 or 6 btree, about 6 per derivation in `grants`, one per distinct lexeme |
| `user_users` | primary key; `(app_id, viewer_id)`; `(app_id, target_user_id)` | 3 |
| `inheritance_info` | primary key; descendant covering; ancestor covering | 3 |
| `access_diff` | primary key; one of two partial indexes by kind | 2 |
| `sync_event` | primary key; `(app_id, seq)`; `(event_type, seq)`; `written_at` | 4 |
| `user_claim` | primary key; live unique `(claim_id, user_id, app_id)`; `(user_id, app_id)`; `claim_id`; `sa_owner_id`; `sa_deleted_at` | 6 |
| `claim_object` | primary key; `app_id`; `claim_id`; `(object_type, object_id)` partial; `(claim_id)` partial for scope grants; `sa_owner_id`; `sa_deleted_at`; `(claim_id, object_type, object_id)` partial | 6 or 7 |
| `audit` | primary key; `app_id`; `sa_owner_id`; `sa_created_at`; `(object_type, object_id)` | 5 |

The GIN indexes take their entries through the pending list and merge them in bulk ([PostgreSQL, GIN fast update](https://www.postgresql.org/docs/17/gin.html#GIN-FAST-UPDATE)). A soft delete sets an indexed column, so it is never a heap only update and writes an entry in every index of its table ([PostgreSQL, Heap Only Tuples](https://www.postgresql.org/docs/17/storage-hot.html)).

Per pair written, the materialization therefore writes one `user_object` row with `≈ 6 + 6 G_x + n_w` entries for `n_w` distinct lexemes, and on insert or delete one update of each `user_users` row it shares with another user of the object.

## The shape of a B-tree

A B-tree page is 8,192 bytes with a 24 byte header and a 16 byte special area; an index tuple is an 8 byte header plus its `MAXALIGN`ed key and include columns, plus a 4 byte line pointer ([Database Page Layout](https://www.postgresql.org/docs/17/storage-page-layout.html)). Leaves are filled to 90 % on build, and over 99 % of pages are leaves ([B-Tree Structure](https://www.postgresql.org/docs/17/btree.html#BTREE-STRUCTURE)). With `e` bytes per entry:

```
B = 8,152 × 0.9 / e                   entries per leaf
h = 1 + ⌈log_B (N / B)⌉               levels for N entries
```

The levels above the leaves are `1 / B` of the index, so a descent reads `h` pages of which all but the leaf are shared by every descent.

1. The pair primary key is one UUID, `e = 28`, `B ≈ 262`: three levels hold 18 million pairs.

2. `idx_user_object_user_object` is keyed `(user_id, app_id, object_type, object_id)` and includes `sa_access`, so every pair is its own entry of about 64 bytes and the reader's range of `V_{u,τ}` pairs is about `⌈V_{u,τ} × 64 / 7,337⌉` leaf pages in `object_id` order. A page of `p` rows reads `⌈p × 64 / 7,337⌉ + 1` of them after one descent, and a listing's further cost is the `p` heap rows it reads for `grants` and the rendering fields. Include columns disable deduplication ([Deduplication](https://www.postgresql.org/docs/17/btree.html#BTREE-DEDUPLICATION)); the key order is what lets a page stop after `p` entries.

3. `idx_inheritance_info_descendant` and `idx_inheritance_info_ancestor` carry include columns, so they never deduplicate and every closure row pays its full entry, about 100 bytes.

## What must stay cached

A check touches one pair, its `G_x` grants in `idx_user_claim_unique` and their claims; a listing touches the reader's range and its `V_{u,τ}` heap rows; a settle touches the closure of the area and the bindings on it. The per pair data of the users who read is the part that grows with users times reach:

```
working set ≈ U_active × V̄ × (606 + entries of one pair) bytes
```

When it exceeds the cache, a listing reads up to `V_{u,τ}` heap pages from storage, and a check two: its pair's leaf and heap page. The closure and the model's own tables grow with objects and grants, not with users, and leave the cache last.

## Dead rows each step leaves

1. A pair recompute that changes nothing writes nothing, because its upsert updates only when a column is distinct.

2. A pair entering or leaving updates `2 (U_x − 1)` `user_users` rows, one dead row each; the bulk rebuild `_refresh_user_users` rewrites only the counts that changed.

3. An area is inserted by the write and deleted by the settle: one dead row per settled area.

4. `trim_sync_event` deletes every event below the watermark in one statement.

Autovacuum starts on a table when its dead rows exceed `autovacuum_vacuum_threshold + autovacuum_vacuum_scale_factor × rows`, 50 plus a fifth of the table by default ([PostgreSQL, Automatic Vacuuming](https://www.postgresql.org/docs/17/runtime-config-autovacuum.html)). `access_diff` holds few live rows and turns them over on every settle, so its threshold is reached on a count of settles; `user_users` turns over `S_u` rows per refresh, so it is the derived table that makes the most dead rows per write.
