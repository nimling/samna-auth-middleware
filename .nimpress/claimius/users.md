---
layout: doc
title: Users
description: One person or machine, one stable user_id, one samna_user row per app, machine clients, delegation through get_user_access, and user_users with the count it holds, what counting it reads and writes, and why the count is exact.
tags: samna_user, user_id, upsert_app_user, check_user_active, merge_user, samna_client, get_user_access, get_users, user_users, _count_user_users, _lock_objects, delegation
lastUpdated: true
---

# Users

A user is who acts. A person and a machine client are both users, and both hold grants the same way.

| Column | Is | Use |
|---|---|---|
| `user_id` | The stable identity | Every reference anywhere points here |
| `id` | The row key for one app | Internal to that row |
| `app_id` | The app scope | Present in every query against user state on a prophet |
| `external_id` | The host system's reference | Lookup from an integration |
| `type` | `user`, `client`, `system`, `guest` | Separates people from machines |
| `status` | `active`, `pending`, `suspended`, `inactive` | Gates every read before any grant is read |

## One identity, one row per app

1. The rows of one person across apps share `user_id` and differ in everything else; `idx_samna_user_user_app` is unique on `(user_id, app_id)` among live rows. A foreign key from an application table targets `samna_user.user_id`, never `samna_user.id`, the split [RFC 7643](https://www.rfc-editor.org/rfc/rfc7643) draws between a subject identifier and a per system record.

2. `upsert_app_user(user_id, app_id, first_name, last_name, user_name, email, phone, external_id, user_image, sa_created_by)` probes for the live row and inserts it when absent, then, when the app names a default claim in `samna_app.claim_id`, probes for a live grant of it and inserts one. The grant is stamped with `sa_created_by`, the acting `user_claim.id`, when a caller granting it passes one, as `upsert_app_client` does, and otherwise with its own id: a first login is the grant acting on its own authority, and the user it names is the user behind it, so the write settles that user. At most two rows written, and none after the first call, so it runs on every login without a guard.

3. `merge_user(target, source, actor)` moves a person's grants, relations and field values from one `user_id` to another in every app, deletes the source's pairs and rows, and writes one audit row per app. Authorship survives it, because `sa_created_by` holds a `user_claim.id` and never a user id. A v1.1.9 set stamped some rows with a user id, the default grant of a first login among them; every replay of the base set runs `_restamp_created_by`, which rewrites such a stamp on every claimius table but `audit` to the user's `user_claim.id` where it is unambiguous, the stamp being the `user_id` of a `samna_user` of the row's app that holds exactly one live `user_claim` in that app, and leaves every other stamp. The row's app is its `app_id`, the app itself for a `samna_app`, and the apps an organization or location is linked to; a disciple holds one app. It writes under `replay_mode`, since the creator a recompute finds is the same user before and after, and a second run rewrites nothing.

4. `check_user_active(app_id, user_id)` runs in every read, one probe on `(user_id, app_id)`. A user who is not active, or whose row is soft deleted, resolves to no access whatever grants they hold, so suspending a user is one column write.

5. Soft deleting a `samna_user` fires `tg_cascade_user_soft_delete` on the transition of `sa_deleted_at` from NULL, which runs `cascade_user_soft_delete`: three updates, over the user's live grants, relations and field values in the app. Each grant update raises the claim area of that grant for that user; see [Materialization](./materialization.md). The cascade lives in the write, so every read stays a pure predicate.

## Machine clients

1. A client is a `samna_client` row with a unique `client_id`, a bcrypt `secret_hash`, a `status` and an optional `expires_at`.

2. `samna_client.client_id` is the client's `user_id`. The client has a per app `samna_user` row of type `client`, created with `upsert_app_user(client_id, ...)`, and its grants are `user_claim` rows on that id.

3. Writes the client performs stamp `sa_created_by` with one of the client's own grants on the target app. A client without a grant on the app has no actor token and cannot create rows.

4. A disciple holds its app's `samna_client` rows, so it verifies a client against its own copy.

## Acting for another user

An actor acts for a target without becoming the target. `get_user_access(actor, app, target, required)` settles what the actor's listing would, checks the actor active, resolves the target named as `external_id`, `user_id` or `samna_user.id`, in that order, among the app's live active users, and then ORs three branches:

| Branch | Mask | Reads |
|---|---|---|
| Self, the actor is the target | 47: owner, write, read, execute, member | nothing |
| Shared visibility, a `user_users` row from actor to target | 36: read, member | one range of `idx_user_users_viewer`, at most `S_u` entries |
| A claim bound to the target's `samna_user` row | `eff(c, b) & 15` of each non deny binding, inside the grant's window | `_user_claim_grants`: the actor's `C` grants, their claims and `C` probes of `idx_claim_object_lookup` |

```
entries(get_user_access) = resolution + 1 + S_u + 1 + 2C
```

`external_id` carries no index, so the resolution reads the app's `U` user rows through `idx_samna_user_app_id`. The row the actor writes carries the actor's `user_claim.id` in `sa_created_by` and the target's `user_id` as its subject. This is delegation in the sense of [RFC 8693, section 1.1](https://www.rfc-editor.org/rfc/rfc8693#section-1.1): the acting party stays visible. The verdict is a live grant, so revoking it revokes the delegation on the next read.

`get_users(user, app, required)` lists the targets of the same three branches that are live and active in the app: the reader, the `S_u` users of the reader's `user_users` range and the targets of the reader's bindings on `claimius.samna_user`, then one `samna_user` probe per target.

## Who can see whom

`user_users` holds, for every viewer `u` and target `v` of one app who reach a common object that is not a claim, the count of those objects:

```
sharing_object_count(u, v) = |{x ∈ V_u ∩ V_v : type(x) ≠ claimius.claim}|
rows = Σ_u S_u ≤ U (U − 1)
```

A pair on a claim moves no count: holding a claim reads the claim, and a claim is a template, not an object two users share. Two holders of one claim therefore do not see each other through it.

The table is kept by counting. `tg_count_user_users_insert` and `tg_count_user_users_delete` are statement triggers on `user_object` that call `_count_user_users` with the transition table `changed` of the pairs the statement inserted or deleted:

1. `_lock_objects` takes the transaction advisory lock of every object of the statement, one key per `object_type:object_id`, in sorted key order.

2. For each pair `(u, x)` of `changed`, one range of `idx_user_object_object_user` on `(app, type, x)` returns the other users of `x`.

3. Every other user `v` of `x` moves `(u, v)` and `(v, u)` by one, the moves summed per `(viewer, target)` over the statement, so each row is written once: an upsert that adds on insert, an update that subtracts on delete, and a delete when the count reaches zero.

```
advisory locks  = distinct objects of the statement, up to 512 a transaction, then one covering lock
entries read    = Σ_{pairs entering or leaving} U_x
rows written    ≤ Σ_{pairs entering or leaving} 2 (U_x − 1)
```

A pair entering on an object one other user holds writes two rows, one in each direction, as the pgTAP suite `pair_storage` checks. A settle that changes one pair of `u` touches the users of that one object, not the `V_u` objects of `u` and every other user on each of them.

The counts stay quiet under `replay_mode`. The bulk seed of `upsert_table_info`, whose pairs are written under it, and `rebuild_user_object`, which repairs any drift, rebuild them with `_refresh_user_users(app, u)`, which reads `u`'s pairs and every other user on each of their objects, `V_u + Σ_{x ∈ V_u} U_x` entries, and rewrites only the counts that changed.

### Why the count is exact

`sharing_object_count` is a count over the self join `P ⋈_x P` of the pair relation, restricted to objects that are not claims, a nonrecursive view. A pair `(u, x)` entering or leaving `P` changes exactly the tuples `(u, v)` and `(v, u)` for the other users `v` of `x`, each by one. Gupta, Mumick and Subrahmanian show that for nonrecursive views the counting algorithm computes exactly the tuples inserted or deleted ([Maintaining Views Incrementally](https://dl.acm.org/doi/10.1145/170035.170066), SIGMOD 1993). Two pairs on one object entering or leaving in the same statement are counted once per ordered pair of users, so the delta is exact within a statement.

Across transactions it is exact because they serialize per object. Without a lock, two transactions adding pairs on one object each read the other users without the other's uncommitted pair, and the tuple between the two users is never counted. `_count_user_users` holds the advisory lock of every object of its statement before it reads the other users, so the second reads after the first commits and counts the tuple once. The writer took those locks before its first write: a settle for the whole sorted set of objects its share names, `recompute_user_object` for its own sorted set. So no transaction takes a lower key after a higher one inside a settle, two settles over overlapping objects cannot deadlock, and the trigger takes the keys again without waiting. A transaction past `_object_lock_budget()` holds the covering lock exclusive in place of its per object keys, and every per object locker holds that lock shared, so it still excludes every transaction that touches its objects and the count stays exact; see [Materialization](./materialization.md#the-lock-budget).

Holding a claim reads the claim, but a claim is a template, not an object two users share, so its pairs move no count.

**Sources.** A. Gupta, I. S. Mumick, V. S. Subrahmanian, [Maintaining Views Incrementally](https://dl.acm.org/doi/10.1145/170035.170066), SIGMOD 1993. [PostgreSQL, Advisory Locks](https://www.postgresql.org/docs/17/explicit-locking.html#ADVISORY-LOCKS).

### Why the table exists

`get_users` must answer from one index range. Without `user_users` the list of users visible to `u` is the self join, `Σ_{x ∈ V_u} U_x` entries per call. With it, the list is one range of `S_u` rows, and the join is paid once per pair that enters or leaves, not once per read.

## Per user state on objects

| Table | Holds | Written by |
|---|---|---|
| `user_relation` | Pins, priority, a parent and sibling for ordering, `used_at` | The user; see [Usage](./usage.md) |
| `object_field` | Per app field definitions for an `object_type` | The implementing side |
| `user_field` | A user's value for one `object_field` | The user |
| `user_object` | Who reaches an object, with which mask and how often they checked it | Claimius; see [Materialization](./materialization.md) |

A `user_relation` belongs to its user alone: `resolve_access` answers it with one probe matching `id` and `user_id`, no grant read.
