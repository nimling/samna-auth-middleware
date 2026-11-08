---
layout: doc
title: Audit
description: The append only trail in the claimius schema, written in the transaction of the change it records, partitioned by month, what one row costs in index entries, how a read of it is gated, and why it lives in the database of the service that did the work.
tags: audit, write_audit, ensure_audit_partition, audit_default, get_audit, WriteAudit, sa_created_by, actor, partition, integrity
lastUpdated: true
---

# Audit

`claimius.audit` is the append only record of what was done, by which grant, to what.

| Column | Holds |
|---|---|
| `app_id` | The app, defaulting to `get_app_id()`; absent on a disciple, which mirrors one app |
| `operation`, `type` | What happened and its kind: an insert, an update, an `error`, a merge, a denied attempt, a sync reconcile |
| `object_type`, `object_id` | What it happened to |
| `sa_owner_id` | The organization the object belongs to, which decides who may read the row |
| `sa_created_by` | The `user_claim.id` the actor acted under, NULL for an event with no actor |
| `message` | Free text, the error for an `error` row |
| `sa_created_at` | When, and the partition key |

## Written in the change's transaction

1. `write_audit(app, operation, type, object_type, object_id, owner, created_by, message)` on a prophet, and the same without `app` on a disciple, inserts one row and returns it. It is the one path in, for claimius's own functions and for the service.

2. The service writes its rows through `shared.WriteAudit(ctx, idb, appID, operation, type, object_type, object_id, owner, created_by, message)`, where `idb` is the `bun.IDB` of the transaction of the change and `appID` is nil on a disciple. The role wrappers are `sdb.Write(ctx, idb, appID, ...)` on the prophet and `sdb.Write(ctx, idb, ...)` on the disciple. A row therefore commits with its change or not at all, and an error in it fails the change.

3. `offer_handover`, `accept_handover` and `merge_user` write their own rows. A replica that falls back to its snapshot writes one row naming the stalled cursor, in the snapshot's transaction.

4. The calc triggers write an `error` row when they fail and raise the error again, under the row's app; `calc_hierarchical_access` takes, for an organization or location, the first app the row is linked to, and writes none for a row that resolves to no app, an organization not yet linked. The row is in the failing transaction, so it persists only when the caller catches the error in a savepoint and commits.

A row carries the actor as a grant, the value every row carries in `sa_created_by`, so the trail joins to the grants, claims and organizations with keys that already exist, and a change to a row, which carries no `sa_updated_by`, is attributed here.

## Monthly partitions

`audit` is partitioned by range on `sa_created_at`, one partition per UTC month named `audit_YYYYMM`, with `audit_default` taking any row no month takes. `write_audit` calls `ensure_audit_partition(now())` first. When the month's partition exists that is one catalog probe. On the first row of a month it takes a transaction advisory lock, creates the partition, moves that month's rows out of `audit_default` and attaches it; it runs as its owner, so a writer that only inserts audit rows can open a month. An unpartitioned `audit` from before becomes `audit_default` and its rows move to their months. Each partition holds exactly the grants of `audit`, set by `_match_audit_partition_grants` when it is attached and on every replay of the grants file, so no partition grants more than the table; see [Security](./security.md#roles).

A month is retired by detaching its partition, one catalog change, where deleting its rows would write one dead row and one entry removal per row in every index.

## What a row costs

One heap row with four btree entries on a prophet, the primary key `(id, sa_created_at)`, `app_id`, `sa_owner_id` and `(object_type, object_id)`, three on a disciple, which carries no `app_id`, and a BRIN summary on `sa_created_at` that changes only when a new range of pages starts. It is written in a transaction that is already writing: no second round trip, no queue, no consumer. The table grows by one row per audited event and is not carried on the feed, so the trail of an app lives in the database of the service that did the work: a disciple's own tables are audited on the disciple, shared writes on the prophet that decided them.

## Reading it

`get_audit(user, app, required, limit, offset)` returns the app's rows newest first whose `sa_owner_id` is an organization the user reaches with `required`. It settles the reader's share of the open areas, reads the reader's organizations, one `get_objects` range of `V_{u,organization}` pairs, then the app's rows newest first, partition by partition, kept by owner until `limit + offset` are kept. With a share `f` of the app's rows owned by organizations the reader reaches, a page reads about

```
V_{u,organization} + (limit + offset) / f     rows
```

`resolve_access` answers one `claimius.audit` row the same way: one probe of the row and the reader's organizations.

## Why in the database

1. Integrity. A row written in the transaction of the change cannot describe a change that did not commit, and a change that writes its row cannot commit without it. A row written after the fact by another process can be lost between the two.

2. Tamper surface. Neither the writer nor the reader role holds `UPDATE` or `DELETE` on `audit`; see [Security](./security.md). A service connecting as either cannot remove its own tracks.

3. One actor model. The actor is a grant, so a row names the claim, the owning organization and the window that made the act legal.

A settle writes derived state, not facts, so it writes no audit rows: the fact is the write that raised the area.
