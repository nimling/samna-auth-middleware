---
layout: doc
title: Storage
description: The rows and bytes each claimius table holds as functions of the data, the indexes each carries and the index entries each row written costs, the shape of the B-trees a read descends, and the dead rows each maintenance step leaves.
tags: storage, index, btree, gin, brin, heap only tuple, fillfactor, autovacuum, user_object, object_info, inheritance_info, user_users, sync_event, access_diff, audit
lastUpdated: true
---

# Storage

## Rows per table

| Table | Rows | Grows with |
|---|---|---|
| `user_object` | `P = Σ_u V_u = Σ_x U_x` | users times reach |
| `object_info` | one per live registered object, `O` | objects |
| `user_users` | `Σ_u S_u ≤ U (U − 1)` | ordered pairs of users sharing an object that is not a claim |
| `inheritance_info` | `Σ_x Σ_{t ∋ x} (d_t(x) + 1)` | objects times depth |
| `claim`, `claim_object`, `user_claim` | claims, bindings, grants | the model, never its product |
| `access_diff` | open areas: at most one per claim, one per claim and assignee, one per object raised and not yet cleared | writes between the reads of their holders |
| `access_diff_user` | one per open area and holder that settled it | reads between raises |
| `user_activity` | one per user and app that used it | users |
| `sync_event` | per app at most as many as its source rows, and none at or below the lowest cursor of the consumers in contact | source rows written; see [Sync](./sync.md) |
| `audit` | audited events, one partition per month | writes |

The pairs dominate: `user_object` is the only table that grows with the product of users and reach, and every other table grows with the rows of the model, with its depth, or with its writes. A claim bound to `n` objects and held by `k` users is `1 + n + k` source rows; the pairs it yields are `k |T_c|`, stored only for the holders that read.

### Bytes per pair

A pair holds only what is per user: the key, `sa_access`, `grants`, `scope`, `direct_grant`, the usage count and two timestamps. The owner, location, root, name, description, link and search vector live once in `object_info`. Measured on the change that moved them there, `d4c71ff`, a pair took about 1,316 bytes of heap and index before against about 805 after, and a settled pair wrote 19 index entries before against 2 after: the primary key and `idx_user_object_object_user`. The bytes of one pair are

```
bytes(pair) ≈ 24 + key + 4 + |grants| + |scope| + 1 + 8 + 3 × 8   heap
            + 2 × (8 + key + 4)                                    two index entries with their line pointers
```

where `key` is the width of `(user_id, app_id, object_type, object_id)`, 48 bytes of uuids and the type name, and `|grants|` grows with the derivations `G_x`, about 150 bytes each. A disciple drops `app_id` from both.

### Bytes per closure row

A closure row is two object pairs, a root, a depth and two timestamps: about 140 bytes of heap and three index entries, the primary key and the two covering indexes, of about 100 bytes each, so an object at depth `d` in one tree costs about `(d + 1) × 440` bytes of closure.

## Indexes and entries per row written

| Table | Indexes | Entries per row written |
|---|---|---|
| `user_object` | primary key `(user_id, app_id, object_type, object_id) INCLUDE (sa_access)`; `idx_user_object_object_user (app_id, object_type, object_id, user_id)` | 2; 0 for a heap only update of `grants`, `scope`, `direct_grant` or the usage count |
| `object_info` | primary key `(object_type, object_id)`; `(sa_owner_id, sa_name, object_id)`; `(sa_location_id, sa_name, object_id)`; GIN on `search_vector` | 3 btree and one GIN entry per distinct lexeme, per object written, never per pair |
| `user_users` | primary key; `(app_id, viewer_id)`; `(app_id, target_user_id)` | 3 |
| `inheritance_info` | primary key; descendant covering; ancestor covering | 3 |
| `access_diff` | primary key; one partial index by kind; `(app_id, user_id)` for an assignment's area | 2, or 3 for an assignment |
| `access_diff_user` | primary key `(diff_id, user_id)` | 1 |
| `user_activity` | primary key `(user_id, app_id)` | 1 |
| `sync_event` | primary key `seq`; `(app_id, txid, seq)`; BRIN on `written_at` | 2, and a BRIN summary per range of pages |
| `claim` | primary key; `app_id`; `sa_owner_id`; `sa_root_id`; `(sa_access & 16)` partial for denies | 4, or 5 for a deny |
| `user_claim` | primary key; live unique `(claim_id, user_id, app_id)`; `(user_id, app_id)`; `claim_id`; `sa_owner_id` | 5 |
| `claim_object` | primary key; `app_id`; `claim_id`; `sa_owner_id`; `(object_type, object_id)` partial; `(claim_id, object_type, object_id)` partial and live; `(claim_id)` partial for live scope grants | 6 for a live object binding, 5 for a live scope grant |
| `audit` | primary key `(id, sa_created_at)`; `app_id`; `sa_owner_id`; `(object_type, object_id)`; BRIN on `sa_created_at`, per month partition | 4 and a BRIN summary |

The GIN index takes its entries through the pending list and merges them in bulk ([PostgreSQL, GIN Fast Update Technique](https://www.postgresql.org/docs/17/gin.html#GIN-FAST-UPDATE)). A soft delete sets `sa_deleted_at`, which the partial indexes of `user_claim` and `claim_object` name, so it is never a heap only update there and writes an entry in every index of the row ([PostgreSQL, Heap Only Tuples](https://www.postgresql.org/docs/17/storage-hot.html)). The indexes on `sa_deleted_at` alone that no read used, on `claim`, `user_claim` and `claim_object`, and `(event_type, seq)` on `sync_event`, are dropped by the base files.

Per pair that enters or leaves, the materialization writes one `user_object` row with 2 entries and, for an object that is not a claim, up to `2 (U_x − 1)` `user_users` rows with 3 entries each.

## The shape of a B-tree

A B-tree page is 8,192 bytes with a 24 byte header and a 16 byte special area; an index tuple is an 8 byte header plus its `MAXALIGN`ed key and include columns, plus a 4 byte line pointer ([PostgreSQL, Database Page Layout](https://www.postgresql.org/docs/17/storage-page-layout.html)). Leaves are filled to 90 % when an index is built, and over 99 % of the pages of a large index are leaves ([PostgreSQL, B-Tree Structure](https://www.postgresql.org/docs/17/btree.html#BTREE-STRUCTURE)). With `e` bytes per entry, line pointer included:

```
B = 8,152 × 0.9 / e                   entries per leaf
h = 1 + ⌈log_B (N / B)⌉               levels for N entries
```

The levels above the leaves are about `1 / B` of the index, so a descent reads `h` pages of which all but the leaf are shared by every descent.

1. The `user_object` primary key carries four key columns and `sa_access`, about `e = 84` bytes for a type name of 15 characters, `B ≈ 87`. The reader's range of `V_{u,τ}` pairs is about `⌈V_{u,τ} / 87⌉` leaf pages in `object_id` order, and a page of `p` rows reads `⌈p / 87⌉ + 1` of them after one descent; its further cost is the `p` heap rows it reads for `grants` and the `p` `object_info` rows it probes. Include columns disable deduplication ([PostgreSQL, Deduplication](https://www.postgresql.org/docs/17/btree.html#BTREE-DEDUPLICATION)); the key order is what lets a page stop after `p` entries.

2. `idx_user_object_object_user` carries no include column and no duplicate keys, about the same width, and serves the `U_x` users of one object as one range.

3. `idx_inheritance_info_descendant` and `idx_inheritance_info_ancestor` carry include columns, so they never deduplicate and every closure row pays its full entry.

## What stays cached

A check touches one pair, its object's `object_info` row, its `G_x` grants in `idx_user_claim_unique` and their claims; a listing touches the reader's range and its `p` heap rows; a settle touches the closure of the area and the bindings on it. The per pair data of the users who read is the part that grows with users times reach:

```
working set ≈ U_active × V̄ × bytes(pair)
```

The closure, `object_info` and the model's own tables grow with objects and grants, not with users.

## Dead rows each step leaves

1. A pair recompute that changes nothing writes nothing, because its upsert updates only where a column is distinct.

2. A pair updated leaves one dead version, which a later update on the same page can prune, since `fillfactor = 90` keeps room on the page.

3. A pair entering or leaving updates up to `2 (U_x − 1)` `user_users` rows, one dead version each; `_refresh_user_users` rewrites only the counts that changed.

4. An area raise leaves one dead version per raise after the first; a settle leaves one `access_diff_user` version per share; a clear deletes the area and, by cascade, its records.

5. A usage flush leaves one dead version per pair counted and per `user_activity` row moved.

6. `trim_sync_event` deletes the outbox rows owed to nobody in one statement per pass; a month of audit is retired by detaching its partition, not by deleting rows.

Autovacuum starts on a table when its dead rows exceed `autovacuum_vacuum_threshold + autovacuum_vacuum_scale_factor × rows`, 50 plus a fifth of the table by default ([PostgreSQL, Automatic Vacuuming](https://www.postgresql.org/docs/17/runtime-config-autovacuum.html)). `user_object` and `access_diff` set the scale factor, and the analyze scale factor, to 0.02: both turn over a small share of their rows on every settle, and at the default a large `user_object` would carry a fifth of its size in dead versions before a vacuum ran.
