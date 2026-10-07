---
layout: doc
title: Users
description: One person or machine, one stable user_id, one samna_user row per app, machine clients, delegation through get_user_access, and user_users with the count it holds, the user_users_diff rows a write raises and a read folds, what each reads and writes, and why the count is exact.
tags: samna_user, user_id, upsert_app_user, check_user_active, merge_user, samna_client, get_user_access, get_users, user_users, user_users_diff, _settle_user_users_diff, _user_users_for_viewer, delegation
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

5. Soft deleting a `samna_user` fires `tg_cascade_user_soft_delete` on the transition of `sa_deleted_at` from NULL, which runs `cascade_user_soft_delete`: three updates, over the user's live grants, relations and field values in the app. Each grant update raises the assignment area of that user for that claim; see [Materialization](./materialization.md). The cascade lives in the write, so every read stays a pure predicate.

## Machine clients

1. A client is a `samna_client` row with a unique `client_id`, a bcrypt `secret_hash`, a `status` and an optional `expires_at`.

2. `samna_client.client_id` is the client's `user_id`. The client has a per app `samna_user` row of type `client`, created with `upsert_app_user(client_id, ...)`, and its grants are `user_claim` rows on that id.

3. Writes the client performs stamp `sa_created_by` with one of the client's own grants on the target app. A client without a grant on the app has no actor token and cannot create rows.

4. A disciple holds its app's `samna_client` rows, so it verifies a client against its own copy.

## Guests

A guest is the holder of one claim without an account: a `samna_user` of type `guest` with the id `uuid.NewSHA1(app, "guest:" + claim)`, one per app and claim. A disciple asks for one with `POST /sam/guest` and the claim and the user that owns it; the prophet runs `upsert_guest_user(app, claim, actor, guest)`, which refuses a claim outside the calling app with `P0404` and an actor that does not own the claim with `P0403`, writes the guest and its grant of the claim under the actor's owning grant, and signs the guest token with the calling app's key, bounded by the grant's `starts_at` and `ends_at`. The guest lives on the prophet, the only place that can sign its token, and reaches the app's disciples through the feed like any user, so a disciple answers a guest's access from the synced grant.

## Acting for another user

An actor acts for a target without becoming the target. `get_user_access(actor, app, target, required)` settles what the actor's listing would, checks the actor active, makes the actor a viewer, which builds its `user_users` rows from its pairs on its first view and refreshes a stamp after, resolves the target named as `external_id`, `user_id` or `samna_user.id`, in that order, among the app's live active users, one probe each of `idx_samna_user_app_external`, `idx_samna_user_user_app` and the primary key, and then ORs three branches. It folds nothing:

| Branch | Mask | Reads |
|---|---|---|
| Self, the actor is the target | 47: owner, write, read, execute, member | nothing |
| Shared visibility, a positive count from actor to target | 36: read, member | the one `user_users` row of the pair by its key, and the pending `user_users_diff` rows of the actor and of the target, `m_a + m_t` entries of `idx_user_users_diff_user`, with two pair probes for each object they name: only an object one of the two moved on can change their count |
| A claim bound to the target's `samna_user` row | `eff(c, b) & 15` of each non deny binding, inside the grant's window | `_user_claim_grants`: the actor's `C` grants, their claims and `C` probes of `idx_claim_object_lookup` |

```
entries(get_user_access) = 3 + 1 + 1 + (m_a + m_t) + 2 |objects of those rows| + 1 + 2C
```

The count of one pair is read by its key and not out of the viewer's whole range: at alpha0024 the branch read `_user_users_for_viewer`, the viewer's `S_u` rows and the pending rows of every object it holds, and the resolution read the app's `U` user rows, so bookable's user listing, one call per row, read 437,835,068 index entries for 200 users; in the bench, the same 200 calls over the directory of one of 200 holders of 2,000 organizations read 5,008 entries in 71 ms, about 23 per row, of which 20 resolve the target through the three `samna_user` probes. The row the actor writes carries the actor's `user_claim.id` in `sa_created_by` and the target's `user_id` as its subject. This is delegation in the sense of [RFC 8693, section 1.1](https://www.rfc-editor.org/rfc/rfc8693#section-1.1): the acting party stays visible. The verdict is a live grant, so revoking it revokes the delegation on the next read.

`get_users(user, app, required)` makes the reader a viewer and lists the targets of the same three branches that are live and active in the app: the reader, the users `_user_users_for_viewer` counts above zero, the stored `S_u` and the moves of the pending rows, and the targets of the reader's bindings on `claimius.samna_user`, then one `samna_user` probe per target. Measured in the bench for one of 200 holders over 2,000 organizations: the first view builds the viewer's rows, 400,000 holder pairs through a hash join, 6,212 index entries and 2.0 seconds, and every later view reads 7 entries in 10 ms.

## Who can see whom

`user_users` holds, for every viewer `u` of one app, a user who has read the directory, and every target `v` who reaches a common object that is not a claim, the count of those objects:

```
sharing_object_count(u, v) = |{x ∈ V_u ∩ V_v : type(x) ≠ claimius.claim}|
rows = Σ_{u viewing} S_u ≤ U (U − 1)
```

A pair on a claim moves no count: holding a claim reads the claim, and a claim is a template, not an object two users share. Two holders of one claim therefore do not see each other through it.

The table is kept by folding, the way `user_object` is kept by settling, for the viewers only; see [Materialization](./materialization.md#the-user-users-fold):

1. A user becomes a viewer on its first `get_users`, `get_user_access` as the actor or `get_claim_graph`, or the first after 30 days away: `_settle_directory(app, u)` builds its rows from its pairs as `C(P ⊖ D)` over its objects, holding the pending rows on them `FOR KEY SHARE`, and stamps `user_activity.directory_at`. A user who never views the directory has no rows and costs nothing.

2. A statement that inserts or deletes pairs raises one `user_users_diff` row per pair on an object that is not a claim, `+1` for a pair entering and `−1` for one leaving, through `tg_raise_user_users_diff_insert` and `tg_raise_user_users_diff_delete`, while the app has a viewer who read the directory within 30 days, one range of the viewer index per statement. It reads nothing else and locks nothing.

3. A read that shows who sees whom folds, through `_settle_user_users_diff(app, u)`, the rows of every object the reader holds or moved on: per object it takes every row or none, and moves `(a, b)` by `now(a) now(b) − before(a) before(b)` for every ordered pair of users of the object of which one moved, where `now` reads the pairs and `before` is the pairs less the rows' sum, writing the rows of the viewers among them.

4. It then reads `_user_users_for_viewer(app, u)`: the stored range of `u` plus the same moves for the objects whose rows are still pending in its snapshot.

```
first view of u    = Σ_{x ∈ V_u} U_x holder pairs read, S_u rows written
write              = 1 row, 2 entries, per pair entering or leaving with a viewer in the app; nothing else read, nothing locked
fold of x          = m_x + U_x entries read, at most 2 m_x (U_x − 1) user_users rows written, for viewers
read of u          = S_u + Σ_{x ∈ T_u} (m_x + U_x) entries
```

with `m_x` the rows pending on `x` and `T_u` the objects with rows pending that `u` holds or moved on. A pair entering on an object one other user holds raises one row, and its fold writes two `user_users` rows, one in each direction, when both are viewers, as the pgTAP suite `pair_storage` checks; `user_access_point` checks that the pairs settled in an app with no viewer raise no row, and that the point check makes the actor a viewer and builds its row toward the one user it shares objects with.

### Why the count is exact

Write `C(Q)(u, v) = Σ_x m_u(x) m_v(x)` over the objects that are not claims, with `m_u(x)` the multiplicity of `(u, x)` in a bag of pairs `Q`, so that `sharing_object_count = C(P)` for the pairs `P`. Write `D` for the `user_users_diff` rows and `P ⊖ D` for the pairs less the rows' sum per pair. The fold keeps one invariant: in every snapshot, `user_users = C(P ⊖ D)`.

1. A row and the pair change it records are written by one statement of one transaction, so every snapshot sees both or neither. At the start no row exists and the counted table is `C(P)`, which is how every database stood before the rows existed.

2. A fold of `x` sees `P_x` and the rows `D_x` in the one snapshot of its statement and moves `user_users` by `C(P_x) − C(P_x ⊖ D_x)`, which is the counting algorithm on the self join `P ⋈_x P`: a pair `(u, x)` entering or leaving changes exactly `(u, v)` and `(v, u)` for the other users `v` of `x`, each by one, and several moving at once change `(u, v)` by the product difference ([Gupta, Mumick and Subrahmanian](https://dl.acm.org/doi/10.1145/170035.170066)). It deletes `D_x` in the same transaction, so when it commits the invariant holds again for `x`.

3. A fold moves `x` only after locking every row of `D_x` its statement sees. Two folds that overlap in time on `x` see nested sets of rows, since a later snapshot sees every transaction an earlier one sees; rows a committed fold deleted are gone from the later view and their change is in its `P_x`. Both cannot lock all of their rows, because the smaller set lies inside the larger and a row is locked by one of them, so at most one moves `x` and the other leaves it, with no wait, `SKIP LOCKED` ([PostgreSQL, The Locking Clause](https://www.postgresql.org/docs/17/sql-select.html#SQL-FOR-UPDATE-SHARE)). The folds of `x` that commit therefore form a chain, each starting from the `P_x ⊖ D_x` the one before it ended at, and their moves telescope to `C(P_x)` less the start.

4. Moves of different objects commute. Each fold adds its integers to the rows it moves; an upsert or update under `READ COMMITTED` reapplies to the newest committed version of a row it waited on ([PostgreSQL, Read Committed Isolation Level](https://www.postgresql.org/docs/17/transaction-iso.html#XACT-READ-COMMITTED)), so two folds that move one row sum, in whichever order they commit. A row is deleted at zero.

5. A write never reads another user's pairs and never locks anything the fold needs: the write that inserts or deletes a pair inserts its row and is done. So two writers adding pairs on one object at the same moment, which an eager count without a lock would have counted once too few, raise two rows, and the fold that takes both counts the pair of them once.

6. A pair is inserted and deleted under its own row lock, so the changes of one pair are serial, and a snapshot that sees a later change sees the earlier one. A fold that takes the later row takes the earlier with it, so `P ⊖ D` holds every pair zero or one times and no count is ever negative.

By 1 to 4 the invariant holds in every snapshot. `_user_users_for_viewer` adds `C(P_x) − C(P_x ⊖ D_x)` for the objects of `u` still pending in its own snapshot to the stored range, so it returns `C(P)(u, ·)` exactly as that snapshot holds the pairs, whether the fold before it moved everything, was skipped by another fold, or was given up at the lock timeout. The count is exact by snapshot, and no lock per object is needed to make it so. The eager count before it had to hold the advisory lock of every object until commit, because a count taken inside the write had to read the other holders of the object as they would stand when it committed.

Holding a claim reads the claim, but a claim is a template, not an object two users share, so its pairs raise no row.

**Sources.** A. Gupta, I. S. Mumick, V. S. Subrahmanian, [Maintaining Views Incrementally](https://dl.acm.org/doi/10.1145/170035.170066), SIGMOD 1993. [PostgreSQL, Transaction Isolation](https://www.postgresql.org/docs/17/transaction-iso.html). [PostgreSQL, The Locking Clause](https://www.postgresql.org/docs/17/sql-select.html#SQL-FOR-UPDATE-SHARE).

### Why the table exists

`get_users` must answer from one index range. Without `user_users` the list of users visible to `u` is the self join, `Σ_{x ∈ V_u} U_x` entries per call. With it, the list is one range of `S_u` rows: the join is paid once on the user's first view, and from then on once per pair that enters or leaves, by the first read or drain that folds it, not once per read; a user who never views the directory pays nothing, and an app with no viewer raises no row.

## Per user state on objects

| Table | Holds | Written by |
|---|---|---|
| `user_relation` | Pins, priority, a parent and sibling for ordering, `used_at` | The user; see [Usage](./usage.md) |
| `object_field` | Per app field definitions for an `object_type` | The implementing side |
| `user_field` | A user's value for one `object_field` | The user |
| `user_object` | Who reaches an object, with which mask and how often they checked it | Claimius; see [Materialization](./materialization.md) |

A `user_relation` belongs to its user alone: `resolve_access` answers it with one probe matching `id` and `user_id`, no grant read.
