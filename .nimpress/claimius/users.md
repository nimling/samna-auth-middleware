---
layout: doc
title: Users
description: One person or machine, one stable user_id, one samna_user row per app, delegation through get_user_access, and user_users with the count it holds and what counting it costs.
tags: samna_user, user_id, ensure_app_user, merge_user, samna_client, get_user_access, user_users, user_relation, delegation
lastUpdated: true
---

# Users

A user is who acts. A person and a machine client are both users, and both hold grants the same way.

| Column | Is | Use |
|---|---|---|
| `user_id` | The stable identity | Every reference anywhere points here |
| `id` | The row key for one app | Internal to that row |
| `app_id` | The app scope | Present in every query against user state |
| `external_id` | The host system's reference | Lookup from an integration |
| `type` | `user`, `client`, `system`, `guest` | Separates people from machines |
| `status` | `active`, `pending`, `suspended`, `inactive` | Gates every read before any grant is read |

## One identity, one row per app

1. The rows of one person across apps share `user_id` and differ in everything else; `idx_samna_user_user_app` is unique on `(user_id, app_id)` among live rows. A foreign key from an application table targets `samna_user.user_id`, never `samna_user.id`, the split [RFC 7643](https://www.rfc-editor.org/rfc/rfc7643) draws between a subject identifier and a per system record.

2. `ensure_app_user(user_id, app_id, first_name, last_name, ...)` creates the per app row on first contact and returns the existing one after, so it runs on every login without a guard.

3. `merge_user` moves a person across apps. Authorship survives it, because `sa_created_by` holds a `user_claim.id` and never a user id.

4. `check_user_active(app_id, user_id)` runs in every read, one probe of `idx_samna_user_user_app`. A user who is not active resolves to no access whatever grants they hold, so suspending a user is one column write. Soft deleting a `samna_user` fires `tg_cascade_user_soft_delete`, which soft deletes the user's grants, relations and field values in the app, once, on the transition: three updates over the user's `C` grants and their relations and fields.

## Machine clients

1. A client is a `samna_client` row with a unique `client_id`, a `secret_hash`, a `status` and an optional `expires_at`.

2. `samna_client.client_id` is the client's `user_id`. The client has a per app `samna_user` row of type `client`, created with `ensure_app_user(client_id, ...)`, and its grants are `user_claim` rows on that id.

3. Writes the client performs stamp `sa_created_by` with one of the client's own grants on the target app. A client without a grant on the app has no actor token and cannot create rows.

4. A disciple holds its app's `samna_client` rows, so it verifies a client against its own copy.

## Acting for another user

An actor acts for a target without becoming the target. `get_user_access(actor, app, target, required)` resolves the target named as `external_id`, `user_id` or `samna_user.id`, in that order, with one query over `samna_user` in the app, then ORs three branches:

| Branch | Mask | Reads |
|---|---|---|
| Self, the actor is the target | 47: owner, write, read, execute, member | nothing |
| Shared visibility, a `user_users` row from actor to target | 36: read, member | one probe of `idx_user_users_viewer` |
| A claim bound to the target's `samna_user` row | the claim's `(m_c | coalesce(b, m_c)) & 15` | the actor's `C` grants and their bindings on the target, `_user_claim_grants` |

The row the actor writes carries the actor's `user_claim.id` in `sa_created_by` and the target's `user_id` as its subject. This is delegation in the sense of [RFC 8693, section 1.1](https://www.rfc-editor.org/rfc/rfc8693#section-1.1): the acting party stays visible. The verdict is a live grant, so revoking it revokes the delegation on the next read.

`get_users(user, app, required)` lists the targets of the same three branches, so a user sees the users they share an object with and the users a claim binds them to.

## Who can see whom

`user_users` holds, for every viewer `u` and target `v` of one app who reach at least one common object, the count of those objects:

```
sharing_object_count(u, v) = |V_u ∩ V_v|
```

It is kept by counting. `tg_count_user_users_insert` and `tg_count_user_users_delete` are statement triggers on `user_object` that call `_count_user_users` with the pairs the statement inserted or deleted:

1. For each pair `(u, x)` that entered or left, one range of `idx_user_object_object_user` on `(app, x)` returns the `U_x` users of `x`.

2. Every other user `v` of `x` moves `sharing_object_count(u, v)` and `sharing_object_count(v, u)` by one, the moves summed per `(viewer, target)` over the statement, so each row is written once: an upsert on entry, an update on exit, and a delete when the count reaches zero.

```
entries read  = Σ_{pairs entering or leaving} U_x
rows written  ≤ Σ_{pairs entering or leaving} 2 (U_x − 1)
```

A settle that changes one pair of `u` touches the users of that one object, not the `V_u` objects of `u` and every other user on each of them. On the data set of [the index](./index.md#symbols), `Σ_u S_u = 111,472`, so the average user shares objects with 111 others; a full rebuild of one user would read `V_u + Σ_{x ∈ V_u} U_x` entries and rewrite up to `2 S_u` rows, where one pair entering reads `U_x`.

The counts stay quiet under `replay_mode`: a disciple takes `user_users` from the feed, and the bulk seed of `init_claimius_tables` rebuilds it with `_refresh_user_users(app, u)`, which reads `u`'s pairs and every other user on each of their objects from `user_object` and rewrites only the counts that changed. `rebuild_user_object` runs the same rebuild after it recomputes every pair.

### Why the count is exact

`sharing_object_count` is a count over the join `P ⋈_x P` of the pair relation with itself, a nonrecursive view. A pair `(u, x)` entering or leaving `P` changes exactly the tuples `(u, v)` and `(v, u)` for the other users `v` of `x`, each by one. Gupta, Mumick and Subrahmanian show that for nonrecursive views the counting approach computes exactly the tuples inserted or deleted ([Maintaining Views Incrementally](https://dl.acm.org/doi/10.1145/170035.170066), SIGMOD 1993). Two pairs on one object entering or leaving in the same statement are counted once per ordered pair of users, so the delta is exact within a statement as well.

### Why the table exists

`get_users` must answer from one index range: without `user_users` the list of users visible to `u` is the self join `Σ_{x ∈ V_u} U_x`, per call. With it, the list is one range of `S_u` rows, and the join is paid once per pair that enters or leaves, not once per read.

## Per user state on objects

| Table | Holds | Written by |
|---|---|---|
| `user_relation` | Pins, priority, a parent and sibling for ordering, `used_at` | The user; see [Usage](./usage.md) |
| `object_field` | Per app field definitions for an `object_type`: name, `json_schema`, `required`, a default `value` | The implementing side |
| `user_field` | A user's value for one `object_field` | The user |
| `user_object` | Who reaches an object and with which mask | Claimius; see [Materialization](./materialization.md) |

A `user_relation` belongs to its user alone: `resolve_access` answers it with one probe matching `id` and `user_id`, no grant read.
