---
gate: samna-developer
layout: doc
title: Users
description: One person or machine, one stable user_id, one samna_user row per app, machine clients, delegation through get_user_access, and user_users with the count it holds, the user_users_diff rows a write raises and a read folds, what each reads and writes, and why the count is exact.
tags: samna_user, user_id, upsert_app_user, guest, upsert_guest_user, update_claim_guest, claimius.code, samk, get_guest_access, check_user_active, merge_user, samna_client, get_user_access, get_users, user_users, user_users_diff, _settle_user_users_diff, _user_users_for_viewer, delegation
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

1. The rows of one person across apps share `user_id` and differ in everything else; `idx_samna_user_user_app` is unique on `(user_id, app_id)` among live rows. A foreign key from an application table targets `samna_user.user_id`, never `samna_user.id`.

2. `upsert_app_user(user_id, app_id, first_name, last_name, user_name, email, phone, external_id, user_image, sa_created_by)` returns the live row of the user in the app and inserts it when absent. When the app names a default claim in `samna_app.claim_id` and the user holds no live grant of it, it inserts one. The grant is stamped with `sa_created_by`, the acting `user_claim.id`, when the caller passes one, as `upsert_app_client` does, and otherwise with the grant's own id: a first login is the grant acting on its own authority, so the write settles the user it names. A repeated call writes nothing, so it runs on every login without a guard.

3. `merge_user(target, source, actor)` merges one `user_id` into another in every app the source has a row in. It moves the source's grants, relations and field values to the target, retires a source grant of a claim the target already holds, moves the `object_asset` links on the source to the target and publishes them to the apps that see the target, copies the source profile onto an existing target row, deletes the source row with its pairs, and writes one audit row per app. Authorship survives it, because `sa_created_by` holds a `user_claim.id` and never a user id.

4. `_restamp_created_by` runs with the base set. It rewrites a `sa_created_by` that holds a user id to that user's `user_claim.id` where the answer is unambiguous: the stamp is the `user_id` of a `samna_user` of the row's app holding exactly one live `user_claim` in that app. Every other stamp, and every `audit` row, stays as it is. The row's app is its `app_id`, the app itself for a `samna_app`, and the apps an organization or location is linked to; a disciple holds one app. It writes under `replay_mode`, since the creator a recompute finds is the same user before and after the rewrite, and a second run rewrites nothing.

5. `check_user_active(app_id, user_id)` runs in every read. A user who is not active, or whose row is soft deleted, resolves to no access whatever grants they hold, so suspending a user is one column write.

6. `user_image` on an app's row is written by the prophet alone. On Samna Auth it holds the user's profile image while the user shares it with the apps they use, and is cleared when they stop; the sync carries the column to every disciple of the app, a cleared one included, so `shared.SamnaUser.UserImage` on a disciple is the shared image or nil. A disciple never writes it back: `/sam` refuses `user_image` on a user update with `400` and takes none on a user create.

7. Soft deleting a `samna_user` fires `tg_cascade_user_soft_delete` on the transition of `sa_deleted_at` from NULL, which runs `cascade_user_soft_delete`: it soft deletes the user's live grants, relations and field values in the app. Each grant it ends raises the assignment area of that user for that claim; see [Materialization](./materialization.md). The cascade lives in the write, so every read stays a pure predicate.

## Machine clients

1. A client is a `samna_client` row with a unique `client_id`, a bcrypt `secret_hash`, a `status` and an optional `expires_at`.

2. `samna_client.client_id` is the client's `user_id`. The client has a per app `samna_user` row of type `client`, created with `upsert_app_user(client_id, ...)`, and its grants are `user_claim` rows on that id.

3. Writes the client performs stamp `sa_created_by` with one of the client's own grants on the target app. A client without a grant on the app has no actor token and cannot create rows.

4. A disciple holds its app's `samna_client` rows, so it verifies a client against its own copy.

## Guests

A guest is the holder of one claim without an account: a `samna_user` of type `guest` with the id `uuid.NewSHA1(app, "guest:" + claim)`, one per app and claim. A guest reads at most: `get_access` answers a guest user nothing past READ and returns its mask with the READ bit only, whatever its claim holds.

1. A guest claim is a claim granted to its guest, with a mode in `claim.guest_mode`: `open`, anyone without a credential acts as the guest; `code`, only the holder of a live guest code does; `off`, the default, neither. `sdb.Instance().PutClaimGuest(ctx, ac, claimID, mode)`, with `ac` OWNER on the claim, routes to `PUT /sam/claim/:id/guest`, where `update_claim_guest` ensures the guest and its grant through `upsert_guest_user` and sets the mode. `off` leaves the guest and its grant in place.

2. `POST /sam/guest` still signs a guest token for an owner of the claim. The token always carries `exp`: the access token lifetime, `SAUTH_ACCESS_TOKEN_DURATION` or 900 seconds, capped by the grant's `ends_at`.

3. The guest lives on the prophet, the only place that can sign its token, and reaches the app's disciples through the feed like any user, so a disciple answers a guest's access from the synced grant.

## Access codes

An access code opens one claim. The table is `claimius.code`, written on the prophet and synced to every disciple of the app; a disciple routes its writes to `/sam/code` as it routes secrets. The value is 32 random bytes in base64url, shown once at creation; the table holds `hash`, its hex SHA-256, never the value.

| Column | Is |
|---|---|
| `kind` | `invite`: a signed in user redeems it into a grant of the claim. `guest`: its holder acts as the guest of the claim |
| `claim_id` | The claim it opens |
| `object_id`, `object_type` | Optional object the service handed it out for |
| `data` | Free JSON the service keeps with it |
| `starts_at`, `ends_at` | The window it resolves in; an invite's window is also the window of the grant it writes |
| `count`, `used_count` | Uses allowed, 0 unlimited, and taken; a use is taken in one statement on the prophet |
| `revoked_at` | Set by `DeleteCode`; a revoked code never resolves again |

1. The helpers are `GetCodeValue()`, `GetCodeHash(value)` and `GetCodeURL(base, path, value)`; on a disciple `PostCode(ctx, ac, claimID, body, base)` returns the row with `value` and `url` once, `GetCodes(ctx, claimID)` lists a claim's codes, `DeleteCode(ctx, ac, id)` revokes one and `GetCodeAccess(ctx, value)` resolves one, cached for 30 seconds and cleared by any code, claim or grant event of the feed. `ac` is OWNER on the claim; the prophet's `insert_code` and `delete_code` check it again. The prophet set has the same functions in `prophet/sdb` with the app id first, for samna_auth; `uuid.Nil` there finds, redeems or lists a code in any app, and `CodeAccess.AppID` names the app.

2. Action codes stay with the service that runs actions: bookable's own `code` table and its Tengo actions use these helpers and nothing else.

## The guard

`smw.UserAuthMiddleware` resolves the actor in this order: bearer, cookie, machine client, then the code, then the anonymous guest.

1. `samk` is a reserved query parameter on every route, `smw.CodeParam`. An API call may send the value in the header `SAuth-Code` instead; a browser carries it in the `__Host-sauth_samk` cookie after its first visit. The guard takes the value off the request, out of the URL and the header, before the handler or a request logger reads it, and sets `Referrer-Policy: no-referrer` on the answer.

2. A guest code acts as the guest of its claim only on a route group mounted with `smw.UserAuthOptions{Guest: true}`, and every such request writes an audit row naming the claim and the code id, never the value. On a navigation GET there the guard sets `__Host-sauth_samk`, `HttpOnly; Secure; SameSite=Lax; Path=/`, expiring with the code, and answers `303` to the same URL without `samk`. On any other route a guest code is no identity: it is still taken off the request, spends no use, writes no audit row, sets no `CodeKey` and leaves the cookie alone, and the request answers as one without a credential, `401` or the login redirect on a navigation, or proceeds without an actor on an `Optional` route. A signed in user there who brings `samk` on a navigation GET gets the `303` without it and no cookie. `GET /code` from `sam.RegisterEndpoints` answers a guest code only where that group is mounted with `Guest`.

3. An invite code needs a login. Without one, a navigation stores the value in the same cookie for ten minutes and goes to `/api/auth/login?next=` the URL without `samk`; the login returns there, and the guard redeems the invite for the signed in user in one transaction, `redeem_code`, granting the claim. A user who already holds the claim spends no use.

4. A signed in user who brings a guest code stays the actor and is never downgraded; the code adds nothing to the user's own rights. How the two should combine is not decided: no union is made.

5. Twenty failed resolves from one address within a minute answer `429` with `Retry-After` for the rest of the minute, valid codes included. A resolve the database cannot answer is `503`, never a pass.

6. A route group mounted with `smw.UserAuthOptions{Guest: true}` serves a request with no credential and no code as anonymous: `UserIDKey` is `shared.AnonymousUserID` and `AnonymousKey` is set. `MutationAccess`, `GetAccess`, `MutationAccessMany` and `ObjectWhere` resolve that actor through `get_guest_access(object_type, object_id)`: the guest of an open guest claim covering the object, capped at READ, with an audit row per resolved check. An object no open claim covers answers as before, the handler's `403` turned into `401`. Without `Guest` neither an open guest claim nor a guest code makes anyone the guest.

## Acting for another user

An actor acts for a target without becoming the target. `get_user_access(actor, app, target, required)` answers what the actor may do to the target user:

1. It settles what the actor's listing would and checks the actor active.

2. It makes the actor a viewer, which builds its `user_users` rows from its pairs on its first view and refreshes a stamp after; see [Who can see whom](#who-can-see-whom).

3. It resolves the target among the app's live active users, named as `external_id`, `user_id` or `samna_user.id`, tried in that order.

4. It ORs three branches. It folds nothing.

| Branch | Mask | Reads |
|---|---|---|
| Self, the actor is the target | 47: owner, write, read, execute, member | nothing |
| Shared visibility, a positive count from actor to target | 36: read, member | the one `user_users` row of the pair by its key, and the pending `user_users_diff` rows of the actor and of the target: only an object one of the two moved on can change their count |
| A claim bound to the target's `samna_user` row | the effective mask of each non deny binding, limited to owner, write, read and execute, inside the grant's window | the actor's grants, their claims and their bindings on the target |

The row the actor writes carries the actor's `user_claim.id` in `sa_created_by` and the target's `user_id` as its subject, so the acting party stays visible. The verdict is a live grant, so revoking it revokes the delegation on the next read.

## Listing users

`get_users(user, app, required)` makes the reader a viewer and lists the targets of the same three branches that are live and active in the app: the reader, the users the reader's directory counts above zero, including the moves still pending, and the targets of the reader's bindings on `claimius.samna_user`.

## Who can see whom

`user_users` holds, for every viewer `u` of one app and every target `v`, the number of objects that are not claims which both of them reach. A viewer is a user who has read the directory. A pair on a claim moves no count: holding a claim reads the claim, and a claim is a template, not an object two users share, so two holders of one claim do not see each other through it.

The table is kept by folding, the way `user_object` is kept by settling, and for viewers only; see [Materialization](./materialization.md#the-user-users-fold):

1. A user becomes a viewer on its first `get_users`, `get_user_access` as the actor or `get_claim_graph`, or the first after 30 days away. `_settle_directory(app, u)` builds its rows from its pairs, less the pending rows on its objects, which it holds `FOR KEY SHARE`, and stamps `user_activity.directory_at`. Later reads refresh the stamp at most once a day. The build runs under a per viewer advisory lock and rereads the stamp under it, so two first views at once build the rows once. A user who never views the directory has no rows.

2. A statement that inserts or deletes pairs raises one `user_users_diff` row per pair on an object that is not a claim, `+1` for a pair entering and `−1` for one leaving, through `tg_raise_user_users_diff_insert` and `tg_raise_user_users_diff_delete`, while the app has a viewer who read the directory within 30 days. It reads nothing else and locks nothing. An app with no viewer raises no row.

3. A read that shows who sees whom folds, through `_settle_user_users_diff(app, u)`, the rows of every object the reader holds or moved on. Per object it takes every row or none, and moves the count of every ordered pair of users of the object of which one moved, writing the rows of the viewers among them.

4. It then reads `_user_users_for_viewer(app, u)`: the stored rows of `u` plus the same moves for the objects whose rows are still pending in its snapshot. The answer is the exact count as that snapshot holds the pairs.

## Per user state on objects

| Table | Holds | Written by |
|---|---|---|
| `user_relation` | Pins, priority, a parent and sibling for ordering, `used_at` | The user; see [Usage](./usage.md) |
| `object_field` | Per app field definitions for an `object_type` | The implementing side |
| `user_field` | A user's value for one `object_field` | The user |
| `user_object` | Who reaches an object, with which mask and how often they checked it | Claimius; see [Materialization](./materialization.md) |

A `user_relation` belongs to its user alone: `resolve_access` answers it with one probe matching `id` and `user_id`, no grant read.

## Cost

`upsert_app_user` writes at most two rows, the user row and the default grant, and none after the first call. `check_user_active` is one probe on `(user_id, app_id)`. `cascade_user_soft_delete` is three updates.

`get_user_access` resolves the target with one probe each of `idx_samna_user_app_external`, `idx_samna_user_user_app` and the primary key. With `C` the actor's grants, `m_a` and `m_t` the pending `user_users_diff` rows of the actor and the target:

```
entries(get_user_access) = 3 + 1 + 1 + (m_a + m_t) + 2 |objects of those rows| + 1 + 2C
```

The shared visibility branch reads `m_a + m_t` entries of `idx_user_users_diff_user` and two pair probes per object those rows name. The claim branch is `_user_claim_grants`: the actor's `C` grants, their claims and `C` probes of `idx_claim_object_lookup`. The claim branch mask is `eff(c, b) & 15`. `get_users` reads one `samna_user` probe per target after the directory.

The directory, with `V_u` the objects user `u` reaches, `U_x` the holders of `x`, `S_u` the stored rows of `u`, `m_x` the rows pending on `x` and `T_u` the objects with rows pending that `u` holds or moved on:

```
sharing_object_count(u, v) = |{x ∈ V_u ∩ V_v : type(x) ≠ claimius.claim}|
rows = Σ_{u viewing} S_u ≤ U (U − 1)

first view of u    = Σ_{x ∈ V_u} U_x holder pairs read, S_u rows written
write              = 1 row, 2 entries, per pair entering or leaving with a viewer in the app; one range of the viewer index per statement
fold of x          = m_x + U_x entries read, at most 2 m_x (U_x − 1) user_users rows written, for viewers
read of u          = S_u + Σ_{x ∈ T_u} (m_x + U_x) entries
```

A fold moves `(a, b)` by `now(a) now(b) − before(a) before(b)`, where `now` reads the pairs and `before` is the pairs less the rows' sum. The first view builds the rows as `C(P ⊖ D)` over the viewer's objects, in the notation of [Why the count is exact](#why-the-count-is-exact).

## Proofs and measurements

### Measurements

1. `get_user_access` over the directory of one of 200 holders of 2,000 organizations, 200 calls: 5,008 index entries in 71 ms, about 23 per row, of which 20 resolve the target through the three `samna_user` probes. The count of one pair is read by its key, not out of the viewer's whole range.

2. `get_users` for one of 200 holders over 2,000 organizations: the first view builds the viewer's rows, 400,000 holder pairs through a hash join, 6,212 index entries and 2.0 seconds, and every later view reads 7 entries in 10 ms.

3. The pgTAP suite `pair_storage` checks that a pair entering on an object one other user holds raises one row, and that its fold writes two `user_users` rows, one in each direction, when both are viewers. `user_access_point` checks that the pairs settled in an app with no viewer raise no row, and that the point check makes the actor a viewer and builds its row toward the one user it shares objects with.

### Why the count is exact

Write `C(Q)(u, v) = Σ_x m_u(x) m_v(x)` over the objects that are not claims, with `m_u(x)` the multiplicity of `(u, x)` in a bag of pairs `Q`, so that `sharing_object_count = C(P)` for the pairs `P`. Write `D` for the `user_users_diff` rows and `P ⊖ D` for the pairs less the rows' sum per pair. The fold keeps one invariant: in every snapshot, `user_users = C(P ⊖ D)`.

1. A row and the pair change it records are written by one statement of one transaction, so every snapshot sees both or neither. With no row pending the stored table is `C(P)`.

2. A fold of `x` sees `P_x` and the rows `D_x` in the one snapshot of its statement and moves `user_users` by `C(P_x) − C(P_x ⊖ D_x)`, which is the counting algorithm on the self join `P ⋈_x P`: a pair `(u, x)` entering or leaving changes exactly `(u, v)` and `(v, u)` for the other users `v` of `x`, each by one, and several moving at once change `(u, v)` by the product difference ([Gupta, Mumick and Subrahmanian](https://dl.acm.org/doi/10.1145/170035.170066)). It deletes `D_x` in the same transaction, so when it commits the invariant holds again for `x`.

3. A fold moves `x` only after locking every row of `D_x` its statement sees. Two folds that overlap in time on `x` see nested sets of rows, since a later snapshot sees every transaction an earlier one sees; rows a committed fold deleted are gone from the later view and their change is in its `P_x`. Both cannot lock all of their rows, because the smaller set lies inside the larger and a row is locked by one of them, so at most one moves `x` and the other leaves it, with no wait, `SKIP LOCKED` ([PostgreSQL, The Locking Clause](https://www.postgresql.org/docs/17/sql-select.html#SQL-FOR-UPDATE-SHARE)). The folds of `x` that commit therefore form a chain, each starting from the `P_x ⊖ D_x` the one before it ended at, and their moves telescope to `C(P_x)` less the start.

4. Moves of different objects commute. Each fold adds its integers to the rows it moves; an upsert or update under `READ COMMITTED` reapplies to the newest committed version of a row it waited on ([PostgreSQL, Read Committed Isolation Level](https://www.postgresql.org/docs/17/transaction-iso.html#XACT-READ-COMMITTED)), so two folds that move one row sum, in whichever order they commit. A row is deleted at zero.

5. A write never reads another user's pairs and never locks anything the fold needs: the write that inserts or deletes a pair inserts its row and is done. So two writers adding pairs on one object at the same moment raise two rows, and the fold that takes both counts the pair of them once.

6. A pair is inserted and deleted under its own row lock, so the changes of one pair are serial, and a snapshot that sees a later change sees the earlier one. A fold that takes the later row takes the earlier with it, so `P ⊖ D` holds every pair zero or one times and no count is ever negative.

By 1 to 4 the invariant holds in every snapshot. `_user_users_for_viewer` adds `C(P_x) − C(P_x ⊖ D_x)` for the objects of `u` still pending in its own snapshot to the stored range, so it returns `C(P)(u, ·)` exactly as that snapshot holds the pairs, whether the fold before it moved everything, was skipped by another fold, or was given up at the lock timeout. The count is exact by snapshot, and no lock per object is needed to make it so.

### Why the table exists

`get_users` must answer from one index range. Without `user_users` the list of users visible to `u` is the self join, `Σ_{x ∈ V_u} U_x` entries per call. With it, the list is one range of `S_u` rows: the join is paid once on the user's first view, and from then on once per pair that enters or leaves, by the first read or drain that folds it, not once per read; a user who never views the directory pays nothing, and an app with no viewer raises no row.

**Sources.** A. Gupta, I. S. Mumick, V. S. Subrahmanian, [Maintaining Views Incrementally](https://dl.acm.org/doi/10.1145/170035.170066), SIGMOD 1993. [PostgreSQL, Transaction Isolation](https://www.postgresql.org/docs/17/transaction-iso.html). [PostgreSQL, The Locking Clause](https://www.postgresql.org/docs/17/sql-select.html#SQL-FOR-UPDATE-SHARE). The split between a stable subject identifier and a per system record: [RFC 7643](https://www.rfc-editor.org/rfc/rfc7643). Delegation with the acting party visible: [RFC 8693, section 1.1](https://www.rfc-editor.org/rfc/rfc8693#section-1.1).
