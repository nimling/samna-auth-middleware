---
layout: doc
title: Users
description: One person or machine, one stable user_id, one samna_user row per app, delegation through get_user_access, and user_users with the count it holds and what refreshing it costs.
tags: samna_user, user_id, ensure_app_user, merge_user, samna_client, get_user_access, user_users, object_users, user_relation, delegation
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

It is derived from `object_users`, the `(object, user)` projection of `user_object`, by `_refresh_user_users(app, u)`:

1. Delete every `user_users` row of viewer `u`: `S_u` rows.

2. Join `u`'s `object_users` rows, one range of `idx_object_users_user` returning `V_u` entries, to every other user's row on the same object, one range of `idx_object_users_object` per object returning `U_x` entries, group by target and insert one row per target: `S_u` rows, each with its three index entries.

```
entries read  = V_u + Σ_{x ∈ V_u} U_x
rows written  = 2 S_u                     S_u ≤ min(U − 1, Σ_{x ∈ V_u} (U_x − 1))
```

Every settle refreshes the viewers it touched, once each, after all their pairs are written. On the data set of [the index](./index.md#symbols), `Σ_u S_u = 111,472`, so the average user shares objects with 111 others and a refresh rewrites about 223 rows.

### Why the count is exact

`sharing_object_count` is a count over the join `P ⋈_x P` of the pair relation with itself, a nonrecursive view. Recomputing a viewer's rows from `object_users` is the recomputation of every tuple of that view whose first column is `u`, and a pair `(u, x)` entering or leaving `P` changes only rows whose viewer is `u` or whose target is `u`. Gupta, Mumick and Subrahmanian show that for nonrecursive views the counting approach computes exactly the tuples inserted or deleted ([Maintaining Views Incrementally](https://dl.acm.org/doi/10.1145/170035.170066), SIGMOD 1993). `_refresh_user_users` recomputes the viewer side of every change it is called for; the target side of the same change is refreshed when the target is itself a settled user of the area.

### Why the table exists

`get_users` must answer from one index range: without `user_users` the list of users visible to `u` is the self join above, `Σ_{x ∈ V_u} U_x` entries per call. With it, the list is one range of `S_u` rows, and the join is paid once per settle of `u`, not once per read.

## Per user state on objects

| Table | Holds | Written by |
|---|---|---|
| `user_relation` | Pins, priority, a parent and sibling for ordering, `used_at` | The user; see [Usage](./usage.md) |
| `object_field` | Per app field definitions for an `object_type`: name, `json_schema`, `required`, a default `value` | The implementing side |
| `user_field` | A user's value for one `object_field` | The user |
| `user_object` | Who reaches an object and with which mask | Claimius; see [Materialization](./materialization.md) |

A `user_relation` belongs to its user alone: `resolve_access` answers it with one probe matching `id` and `user_id`, no grant read.
