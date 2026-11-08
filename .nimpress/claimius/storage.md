---
gate: samna-developer
layout: doc
title: Storage
description: The rows and bytes each claimius table holds as functions of the data, the indexes each carries and the index entries each row written costs, the shape of the B-trees a read descends, and the dead rows each maintenance step leaves.
tags: storage, index, btree, gin, brin, heap only tuple, fillfactor, autovacuum, user_object, object_info, inheritance_info, user_users, user_users_diff, sync_event, access_diff, audit
lastUpdated: true
---

# Storage

Every claimius table falls in one of three groups: the model, the rows a writer states (`claim`, `user_claim`, `claim_object`, organizations, locations); the derived state, which claimius keeps from the model (`user_object`, `object_info`, `inheritance_info`, `user_users`); and the bookkeeping that keeps the derived state current and the feed served (`access_diff`, `user_users_diff`, `usage_diff`, `user_activity`, `sync_event`, `audit`). This page names what each table holds, the indexes it carries, and what each maintenance step leaves behind. The counts and byte sizes are in [Cost](#cost).

## What each table holds

| Table | One row per | Grows with |
|---|---|---|
| `user_object` | user and object the user reaches, for the users who read | users times reach |
| `object_info` | live registered object | objects |
| `user_users` | ordered pair of users sharing an object that is not a claim, for the viewers who read the directory | viewers times the users they share objects with |
| `inheritance_info` | object and ancestor in each tree it sits in, itself included | objects times depth |
| `claim`, `claim_object`, `user_claim` | claim, binding, grant | the model, never its product |
| `access_diff` | area raised since the oldest cursor among the users used within 7 days: one per object raised and one per claim and assignee | writes between the trims |
| `user_users_diff` | pair that entered or left on an object that is not a claim while the app had a viewer, not yet folded | pair changes between the reads and drains that fold them |
| `usage_diff` | pair and flush not yet folded | flushes between the listings of the pair's user |
| `user_activity` | user and app that used it, holding the cursor | users |
| `sync_event` | source row written, per app, and none at or below the lowest cursor of the consumers in contact | source rows written; see [Sync](./sync.md) |
| `audit` | audited event, one partition per month | writes |

`user_object` is the only table that grows with the product of users and reach. Every other table grows with the rows of the model, with its depth, or with its writes. A pair holds only what is per user: the key, `sa_access`, `grants`, `scope`, `direct_grant`, the usage count and its timestamps. The owner, location, root, name, description, link and search vector of an object live once in `object_info`. A disciple drops `app_id` from both.

## Indexes

| Table | Indexes |
|---|---|
| `user_object` | primary key `(user_id, app_id, object_type, object_id) INCLUDE (sa_access)`; `idx_user_object_object_user (app_id, object_type, object_id, user_id)` |
| `object_info` | primary key `(object_type, object_id)`; `(sa_owner_id, sa_name, object_id)`; `(sa_location_id, sa_name, object_id)`; `(sa_created_by, sa_owner_root_id)`; GIN on `search_vector` |
| `user_users` | primary key; `(app_id, viewer_id)`; `(app_id, target_user_id)` |
| `inheritance_info` | primary key; `idx_inheritance_info_descendant` and `idx_inheritance_info_ancestor`, each covering the other end, the root and the depth |
| `access_diff` | primary key; `(app_id, object_type, object_id)` partial for object areas; `(app_id, root_id, txid)`; `(app_id, txid)`; `(app_id, user_id, txid)` partial for assignments |
| `user_users_diff` | primary key `(app_id, object_type, object_id, id)`; `(app_id, user_id)` |
| `usage_diff` | primary key `(app_id, user_id, id)` |
| `user_activity` | primary key `(user_id, app_id)`; `(app_id, directory_at)` partial for viewers |
| `sync_event` | primary key `seq`; `(app_id, txid, seq)`; BRIN on `written_at` |
| `claim` | primary key; `app_id`; `sa_owner_id`; `sa_root_id`; `(sa_access & 16)` partial for denies; on a prophet `(app_id, id)` unique, which the app scoped grants and bindings reference |
| `user_claim` | primary key; live unique `(claim_id, user_id, app_id)`; `(user_id, app_id)`; `claim_id`; `sa_owner_id` |
| `claim_object` | primary key; `app_id`; `claim_id`; `sa_owner_id`; `(object_type, object_id)` partial; `(claim_id, object_type, object_id)` partial and live; `(claim_id)` partial for live scope grants |
| `audit` | primary key `(id, sa_created_at)`; `app_id` on a prophet; `sa_owner_id`; `(object_type, object_id)`; BRIN on `sa_created_at`, per month partition |

1. The GIN index on `search_vector` takes its entries through the pending list and merges them in bulk.

2. An update of `user_object` that changes only `grants`, `scope`, `direct_grant`, the mark or the usage count touches no indexed column and stays on its page as a heap only update. So does a cursor move in `user_activity`.

3. A soft delete sets `sa_deleted_at`, which the partial indexes of `user_claim` and `claim_object` name, so it is never a heap only update on those tables.

4. A raise of an existing area in `access_diff` moves its `txid`, which is indexed, so it writes its index entries again.

## What each step leaves behind

1. A pair recompute that changes nothing writes nothing: the settle compares before it writes.

2. A pair updated leaves one dead version, which a later update on the same page can prune, since `user_object` keeps `fillfactor = 90`.

3. A pair entering or leaving while the app has a viewer leaves one `user_users_diff` row, dead once folded. The fold updates the `user_users` rows of the viewers who share the object, one dead version each, so a pair that entered and left before a fold moves no count at all.

4. A settle that only moves marks leaves one heap only version per pair.

5. An area raise leaves one dead version per raise after the first. A settle writes nothing to an area. The trim deletes the areas below the lowest active cursor in one statement per app.

6. A usage flush leaves one `usage_diff` row per pair counted, dead once folded, and one dead version per `user_activity` row it moves. A share settle leaves one heap only version of the cursor; the fold leaves one dead version per pair it moves.

7. `trim_sync_event` deletes the outbox rows owed to nobody in one statement per pass. Audit rows older than `audit_days` are deleted by row by `claimius.prune`, from every partition, once the table is over its size limit; no partition is dropped.

`user_object`, `access_diff`, `user_users_diff` and `usage_diff` set `autovacuum_vacuum_scale_factor` and `autovacuum_analyze_scale_factor` to 0.02. Each turns over a small share of its rows on every settle or flush, and at the default scale factor a large table would carry a fifth of its size in dead versions before a vacuum ran.

## Cost

Symbols as in [Claimius](./index.md#symbols).

### Rows per table

| Table | Rows |
|---|---|
| `user_object` | `P = Σ_u V_u = Σ_x U_x` |
| `object_info` | `O` |
| `user_users` | `Σ_{u viewing} S_u ≤ U (U − 1)` |
| `inheritance_info` | `Σ_x Σ_{t ∋ x} (d_t(x) + 1)` |

A claim bound to `n` objects and held by `k` users is `1 + n + k` source rows; the pairs it yields are `k |T_c|`, stored only for the holders that read.

### Bytes per pair

```
bytes(pair) ≈ 24 + key + 4 + |grants| + |scope| + 1 + 8 + 3 × 8   heap
            + 2 × (8 + key + 4)                                    two index entries with their line pointers
```

`key` is the width of `(user_id, app_id, object_type, object_id)`, 48 bytes of uuids and the type name, and `|grants|` grows with the derivations `G_x`, about 150 bytes each.

### Bytes per closure row

A closure row is two object pairs, a root, a depth and two timestamps: about 140 bytes of heap and three index entries, the primary key and the two covering indexes, of about 100 bytes each. An object at depth `d` in one tree costs about `(d + 1) × 440` bytes of closure.

### Index entries per row written

| Table | Entries per row written |
|---|---|
| `user_object` | 2; 0 for a heap only update |
| `object_info` | 4 btree and one GIN entry per distinct lexeme, per object written, never per pair |
| `user_users` | 3 |
| `inheritance_info` | 3 |
| `access_diff` | 4, and 4 again for each raise of an existing area |
| `user_users_diff` | 2 |
| `usage_diff` | 1 |
| `user_activity` | 1, 2 for a viewer; 0 for a cursor move |
| `sync_event` | 2, and a BRIN summary per range of pages |
| `claim` | 5 on a prophet, 6 for a deny; 3 and 4 on a disciple |
| `user_claim` | 5 |
| `claim_object` | 6 for a live object binding, 5 for a live scope grant |
| `audit` | 4 on a prophet, 3 on a disciple, and a BRIN summary |

Per pair that enters or leaves, the materialization writes one `user_object` row with 2 entries and, for an object that is not a claim, one `user_users_diff` row with 2 entries. The fold that takes the row later writes up to `2 (U_x − 1)` `user_users` rows with 3 entries each, once for every row of the object it folds together.

### The shape of a B-tree

A B-tree page is 8,192 bytes with a 24 byte header and a 16 byte special area; an index tuple is an 8 byte header plus its `MAXALIGN`ed key and include columns, plus a 4 byte line pointer. Leaves are filled to 90 % when an index is built, and over 99 % of the pages of a large index are leaves. With `e` bytes per entry, line pointer included:

```
B = 8,152 × 0.9 / e                   entries per leaf
h = 1 + ⌈log_B (N / B)⌉               levels for N entries
```

The levels above the leaves are about `1 / B` of the index, so a descent reads `h` pages of which all but the leaf are shared by every descent.

1. The `user_object` primary key carries four key columns and `sa_access`, about `e = 84` bytes for a type name of 15 characters, `B ≈ 87`. The reader's range of `V_{u,τ}` pairs is about `⌈V_{u,τ} / 87⌉` leaf pages in `object_id` order, and a page of `p` rows reads `⌈p / 87⌉ + 1` of them after one descent; its further cost is the `p` heap rows it reads for `grants` and the `p` `object_info` rows it probes. Include columns disable deduplication; the key order is what lets a page stop after `p` entries.

2. `idx_user_object_object_user` carries no include column and no duplicate keys, about the same width, and serves the `U_x` users of one object as one range.

3. `idx_inheritance_info_descendant` and `idx_inheritance_info_ancestor` carry include columns, so they never deduplicate and every closure row pays its full entry.

### What stays cached

A check touches one pair, its object's `object_info` row, its `G_x` grants in `idx_user_claim_unique` and their claims; a listing touches the reader's range and its `p` heap rows; a settle touches the closure of the area and the bindings on it. The per pair data of the users who read is the part that grows with users times reach:

```
working set ≈ U_active × V̄ × bytes(pair)
```

The closure, `object_info` and the model's own tables grow with objects and grants, not with users.

### Dead rows and autovacuum

Autovacuum starts on a table when its dead rows exceed `autovacuum_vacuum_threshold + autovacuum_vacuum_scale_factor × rows`, 50 plus a fifth of the table by default. At 0.02 the four tables above are vacuumed once a fiftieth of their rows is dead.

## Proofs and measurements

Measured: a pair takes about 805 bytes of heap and index, and a settled pair writes 2 index entries, the primary key and `idx_user_object_object_user`.

**Sources.** [PostgreSQL, Database Page Layout](https://www.postgresql.org/docs/17/storage-page-layout.html); [PostgreSQL, B-Tree Structure](https://www.postgresql.org/docs/17/btree.html#BTREE-STRUCTURE); [PostgreSQL, Deduplication](https://www.postgresql.org/docs/17/btree.html#BTREE-DEDUPLICATION); [PostgreSQL, Heap Only Tuples](https://www.postgresql.org/docs/17/storage-hot.html); [PostgreSQL, GIN Fast Update Technique](https://www.postgresql.org/docs/17/gin.html#GIN-FAST-UPDATE); [PostgreSQL, Automatic Vacuuming](https://www.postgresql.org/docs/17/runtime-config-autovacuum.html).
