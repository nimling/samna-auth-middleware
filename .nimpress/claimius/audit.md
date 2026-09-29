---
layout: doc
title: Audit
description: The append only trail in the claimius schema, what a row holds, who writes it and when, what one row costs in index entries, how a read of it is gated, and why it lives in the database of the service that did the work.
tags: audit, write_audit, get_audit, sa_created_by, actor, integrity
lastUpdated: true
---

# Audit

`claimius.audit` is the append only record of what was done, by which grant, to what.

| Column | Holds |
|---|---|
| `app_id` | The app, defaulting to `get_app_id()` |
| `operation`, `type` | What happened: `insert`, `update`, `delete`, `error`, `merge`, a denied attempt |
| `object_type`, `object_id` | What it happened to |
| `sa_owner_id` | The organization the object belongs to, which decides who may read the row |
| `sa_created_by` | The `user_claim.id` the actor acted under, NULL for an event with no actor |
| `message` | Free text, the error for an `error` row |
| `sa_created_at` | When |

## Who writes it

1. `write_audit(app, operation, type, object_type, object_id, owner, created_by, message)` inserts one row and returns it. It is the one path in, for claimius's own functions and for the service.

2. `merge_user` writes one row per app it merges in.

3. The calc triggers write an `error` row when they fail and then raise the error again. The row is in the failing transaction, so it persists only when the caller catches the error in a savepoint and commits.

4. The service writes its own rows, a denied attempt among them, through `write_audit` in the transaction of the change: `shared.WriteAudit` takes the `bun.IDB` of that transaction, and the prophet's and the disciple's `sdb.Write` pass it on.

A row carries the actor as a grant, the same value every row carries in `sa_created_by`, so the trail joins to the grants, claims and organizations with keys that already exist, and later changes to a row, which carry no `sa_updated_by`, are attributed here.

## What a row costs

One insert with four btree entries, the primary key `(id, sa_created_at)`, `app_id`, `sa_owner_id` and `(object_type, object_id)`, and a BRIN summary on `sa_created_at`, in a transaction that is already writing: no second round trip, no queue, no consumer. The table is partitioned by month on `sa_created_at`; `write_audit` opens the month's partition on its first row through `ensure_audit_partition`, a row no month takes lands in `audit_default` and moves when its month opens, and a month is retired by detaching its partition rather than deleting rows. The table grows by one row per audited event and is not carried on the feed, so the trail of an app lives in the database of the service that did the work: a disciple's own tables are audited on the disciple, shared writes on the prophet that decided them.

## Reading it

`get_audit(user, app, required, limit, offset)` returns the app's rows newest first whose `sa_owner_id` is an organization the user reaches with `required`: the user's `V_{u,organization}` pairs, then the app's rows newest first, month partition by month partition, kept by owner until `limit + offset` rows are kept. With a share `f` of the app's rows owned by organizations the user reaches, a page reads about `(limit + offset) / f` rows. `resolve_access` answers one `claimius.audit` row the same way, one probe of the row and the reader's organizations.

## Why in the database

1. Integrity. A row written in the transaction of the change cannot describe a change that did not commit, and a change that writes its row cannot commit without it. A row written after the fact by another process can be lost between the two.

2. Tamper surface. No grant gives the writer or the reader role `UPDATE` or `DELETE` on `audit`; see [Security](./security.md). A service connecting as either can add rows and nothing else, so a compromised service can add noise to the trail but cannot remove its own tracks.

3. One actor model. The actor is a grant, so a row names the claim, the owning organization and the window that made the act legal.

A settle writes derived state, not facts, so it writes no audit rows: the fact is the write that raised the area.
