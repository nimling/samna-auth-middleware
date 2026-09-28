---
layout: doc
title: Usage
description: How often and how recently a user reached an object, kept on the per user access pair in user_object, recorded in batches, ranked with a decay computed at read time, and never synced.
tags: usage, recency, user_object, used_count, last_used_at, touch_object_usage, decay, sort usage, HOT update
lastUpdated: true
---

# Usage

Usage is part of a user's access state on an object: how many times the user reached it and when last. It lives on the pair that already records that the user reaches the object, `user_object`.

| Column | Holds |
|---|---|
| `used_count` | Uses since the pair was created |
| `last_used_at` | The time of the latest use |

## What counts as a use

A use is a single object resolution that granted access: `get_access`, `mutation_access`, `resolve_access` or `get_user_access` returning a row. That is the one place claimius resolves a user's point in the tree for one object, and it is what a user opening, editing or acting on something passes through. A listing, a search hit or a graph node is not a use; the object the user then opens is.

## Recording

1. The middleware counts uses in process and flushes them in batches through `touch_object_usage(app_id, user_ids, object_types, object_ids, counts, last_used_at)`, one statement over arrays. See [Middleware](./middleware/index.md).

2. The statement updates existing `user_object` rows only, in primary key order: `used_count = used_count + count`, `last_used_at = greatest(last_used_at, last)`. A use always follows a granted check, so the pair exists. A pair that vanished between the use and the flush, a revoke, is skipped.

3. The read functions stay free of the write. The flush pays one transaction and one commit per batch, where a write inside every check would pay one per use.

### Cost

`user_object` carries two indexes, the primary key `(user_id, app_id, object_type, object_id) INCLUDE (sa_access)` and `(object_id, object_type)`, none of them on a usage column. An update that changes no indexed column and finds room on its page is a heap only tuple update: no index entry is written ([PostgreSQL, Heap Only Tuples](https://www.postgresql.org/docs/17/storage-hot.html)). `user_object` is stored at fill factor 90 to keep that room ([PostgreSQL, CREATE TABLE, fillfactor](https://www.postgresql.org/docs/17/sql-createtable.html)).

| Measured, 1,000 pairs per flush, on the 144 byte pair row of the benchmark | Value |
|---|---|
| Time per flush | 13 to 26 ms |
| Heap only updates | 4,000 of 4,000 |
| WAL per pair, steady | 134 bytes |
| WAL per pair, first touch after a checkpoint | 6.2 KB, the full page image |

At 100 uses a second, a flush every ten seconds is 1,000 pairs: one 20 ms statement and about 130 KB of WAL, plus a page image for each page first touched in a checkpoint cycle.

## Ranking

A ranking weighs frequency by recency. With half life `h`, 14 days by default:

```
score = used_count × 2^( −(now − last_used_at) / h )
```

The score is computed at read time from the two columns, so time passing writes nothing. It is the exponentially decayed count of `used_count` uses placed at `last_used_at`: exact when the uses are recent against `h`, an upper bound otherwise, and at equal counts it ranks recent use above old use.

1. The owner and claim graphs order siblings by `score` when the caller sorts by usage, then by label and `object_id`. See [Graphs](./graphs.md).

2. `search` orders by `ts_rank × (1 + ln(1 + score))`, so among equally good text matches the ones the user works with come first. See [Search](./search.md).

3. A "recent" list orders the user's own primary key range by `last_used_at`.

## Lifetime

1. Usage lives and dies with access. When a revoke removes the pair, its usage goes with it; a later grant starts at zero. A ranking never shows an object the user can no longer reach, because the pair it reads is the access record itself.

2. The settle never touches usage. Its `ON CONFLICT ... DO UPDATE` sets the access columns only, so a pair that already exists keeps its counts, and the flush and the settle never write the same columns. Both take the pair's row lock for one short statement and both visit pairs in primary key order, so they never deadlock.

## Never synced

Usage is local to the database where the use happened. A disciple's users use the disciple. `user_object` is derived state and never enters `sync_event`, so usage never crosses from a disciple to the prophet or between disciples. See [Sync](./sync.md).
