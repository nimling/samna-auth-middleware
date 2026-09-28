---
layout: doc
title: Users
description: One person or machine, one stable user_id, one samna_user row per app, and the tables that hang per user state off that id.
tags: samna_user, user_id, ensure_app_user, merge_user, samna_client, user_relation, user_users, user_field, object_field, delegation
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
| `status` | `active`, `pending`, `suspended`, `inactive` | Gates access before any grant is read |
| `user_image` | The avatar | Rendering |

## One identity, one row per app

1. The rows for one person across apps share `user_id` and differ in everything else. A foreign key from an application table targets `samna_user.user_id`, never `samna_user.id`, the split [RFC 7643](https://www.rfc-editor.org/rfc/rfc7643) draws between a subject identifier and a per system record.

2. `ensure_app_user(user_id, app_id, first_name, last_name, ...)` creates the per app row on first contact and returns the existing one after. It is idempotent, so it runs on every login without a guard.

3. `merge_user` moves a person across apps. Authorship survives it, because `sa_created_by` holds a `user_claim.id` and never a user id.

4. `check_user_active(app_id, user_id)` runs first in every read. A user who is not active resolves to no access, whatever grants they hold, so suspending a user is one column write. Soft deleting a `samna_user` fires `tg_cascade_user_soft_delete`, which soft deletes the user's grants, relations and field values once, on the transition, in the write that caused it.

## Machine clients

1. A client is a `samna_client` row with a `client_id`, a bcrypt `secret_hash`, a `status` and an optional `expires_at`.

2. `samna_client.client_id` is the client's `user_id`. The client has a per app `samna_user` row of type `client`, created with `ensure_app_user(client_id, ...)`, and its grants are `user_claim` rows on that id.

3. Writes the client performs stamp `sa_created_by` with one of the client's own grants on the target app. A client without a grant on the app has no actor token and cannot create rows; provisioning assigns the owning claim first.

4. A disciple verifies client credentials against its own replica of `samna_client`, so serving a machine call never asks the prophet.

## Acting for another user

An actor acts for a target without becoming the target. `get_user_access(actor, app, target, required)` resolves the actor's right on the target's `samna_user` row, with the target named as `external_id`, `user_id` or `samna_user.id`, resolved in that order inside the same query. It returns the actor's `user_claim.id`, which the written row carries in `sa_created_by`, and the target's `user_id` as its subject. Three branches are ORed:

| Branch | Grants |
|---|---|
| Self | owner, write, read, execute, member |
| Shared visibility, a row in `user_users` | read, member |
| A claim bound to the target's `samna_user` row | The claim's mask |

This is delegation in the sense of [RFC 8693, section 1.1](https://www.rfc-editor.org/rfc/rfc8693#section-1.1): the acting party stays visible. The verdict is a live grant, so revoking it revokes the delegation on the next read.

## Who can see whom

`user_users` records that two users of one app share at least one object they can both reach, with the count and the first and last time. `get_users` lists the rows of it for a viewer.

1. `user_users` is maintained from `user_object`. When a `(user, object)` pair enters or leaves `user_object`, every other user on that object gains or loses one on the pair's `sharing_object_count`, and a pair whose count falls to zero is deleted.

2. The increments of one settle are aggregated before they are written. The settle statement joins the pairs that entered or left to the other users on their objects, groups by `(viewer, target)`, and writes each pair once with its summed count. The tuples it processes are `Σ U_o` over the changed pairs; the rows it writes are the distinct pairs, at most `U_s × U_area` for `U_s` users settled and `U_area` users in the area. A binding held by 50 users on a node of 300 objects each reached by 20 others processes about a million tuples and writes about 3,500 rows, where writing each increment would write the million.

3. Why: `get_users` must answer from one index range, so the pair table is kept. The inequality is between processing a tuple, `cpu_tuple_cost`, 0.06 µs, and writing a row with its indexes, tens of µs: aggregating first wins whenever increments repeat a pair, which in any shared tree they do. The sync feed carries source rows only, so the pair table costs its prophet nothing beyond its own rows; see [Sync](./sync.md).

## Per user state on objects

| Table | Holds | Written by |
|---|---|---|
| `user_relation` | Pins, priority, tags, a parent and sibling for ordering, `used_at` | The user, through the api |
| `object_field` | Per app field definitions for an `object_type`: name, `json_schema`, `required`, a default `value` | The implementing side, to carry optional data about an object on the prophet |
| `user_field` | A user's value for one `object_field` | The user |
| `user_object` | Who reaches an object, and how often and how recently they used it | Claimius, and the usage flush; see [Usage](./usage.md) |

A `user_relation` belongs to its user alone: `resolve_access` answers it with a match on `user_id`, no grant needed.
