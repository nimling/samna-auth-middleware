---
layout: doc
title: Apps
description: samna_app, the two scoping mechanisms, app scoped tables and app_object links, what each costs a query, scoping per role, and the identity helpers that tell SQL which app it runs as.
tags: samna_app, app_id, app_object, app_object_ids, link_app_object, unlink_app_object, _app_ids_for_row, get_app, get_app_id, get_disciple_app, scoping, tenancy
lastUpdated: true
---

# Apps

An app is one product that claimius governs: Samna Auth itself, Bookable, Nimployer. `claimius.samna_app` holds it.

| Column | Holds |
|---|---|
| `slug` | The unique name a disciple presents to its prophet |
| `private_key` | The key the prophet signs the app's tokens with |
| `private_seed` | The seed the app's secrets are encrypted with |
| `app_secret`, `secret_version` | The hashed secret a disciple authenticates with |
| `redirect_uris`, `connection`, `provider_ids` | The login configuration |
| `claim_id` | The default claim `ensure_app_user` grants a new user |
| `home_prophet` | The peer prophet that decides the app's writes, NULL when this prophet does; see [Prophets and disciples](./prophets.md) |
| `sa_owner_id` | The organization that owns the app |

## Two scoping mechanisms

1. Own column. On a prophet, `samna_user`, `claim`, `user_claim`, `claim_object`, `user_object`, `user_users`, `user_relation`, `user_field`, `object_field`, `samna_client`, `samna_secret`, `audit`, `access_diff` and `sync_event` carry `app_id`. A row belongs to one app, and every read function filters on it. `app_id` is the second column of the `user_object` primary key and of `idx_user_claim_user_app`, and the first of `idx_user_users_viewer`, so the filter narrows the same descent that finds the user.

2. Links. `organization` and `location` carry no `app_id`. An object belongs to every app holding a live `app_object` row for it.

| Function | Does | Reads and writes |
|---|---|---|
| `app_object_ids(app_id, object_type)` | The object ids of one type linked to an app, the filter in place of `app_id = ?` | one range of `idx_app_object_unique` |
| `app_has_object(app_id, object_type, object_id)` | The boolean guard | one probe |
| `apps_for_object(object_type, object_id)` | The apps linked to one object | one range of `idx_app_object_object` |
| `link_app_object(app_id, object_type, object_id, created_by)` | Links, idempotent | one probe; on a new link one row with four index entries and, outside replay, `|affected(x)|` pair recomputes |
| `unlink_app_object(app_id, object_type, object_id)` | Soft deletes the link | one update, and a delete of the app's `U_x` pairs on the object, counted out of `user_users` |
| `create_root_organization(app_id, name, actor, ...)` | Creates a root and links it, idempotent on app and name | see [Organizations](./organizations.md) |
| `link_app_organization(app_id, org_id, cascade)` | Links an organization and, with `cascade`, every organization and location under it | one `link_app_object` per node |

The unique index is `(app_id, object_type, object_id)` where `sa_deleted_at IS NULL`, so a live link is unique per app and object. `app_object` is never written directly.

Users, grants and access are per app by nature, since the same person holds different grants in two products, so a per app row costs nothing extra. Organizations and locations are shared structure: the same company and the same building serve every product. With `n_a` apps reaching an organization, the link stores the organization once and `n_a` link rows, and a move changes the closure once.

## Which apps a write belongs to

`_app_ids_for_row(object_type, row)` scopes a row for its calc trigger: the row's `app_id` when its table has the column; otherwise its live `app_object` links; otherwise, for a table of the implementing side, `get_app_id()`. An internal table without `app_id`, an organization or location, resolves to no app until it is linked, and the link settles its pairs when it lands. The calc trigger runs its per app work once per app the row resolves to, so a row linked to `n_a` apps raises `n_a` areas.

`init_claimius_tables` refuses a table without `app_id` on a prophet that hosts apps besides its system app, because `get_app_id()` would scope every row of it to the system app.

## Scoping per role

1. A prophet holds every app it is home for, and every app it mirrors from a peer. Every read function takes the app as a parameter and filters on it in its body.

2. A disciple mirrors one app. Its tables carry no `app_id` column and it has no `app_object`: every row it holds is already in scope, and its read functions take no app argument, `get_objects(user, type, required, after, limit)`. The prophet selects what reaches a disciple by `app_id` and `app_object_ids`, so scoping is written on the prophet side only; see [Sync](./sync.md).

## Knowing which app the SQL runs as

The identity helpers live in the prophet set, where more than one app exists.

| Function | Returns | Reads |
|---|---|---|
| `get_app()` | The system app, by the slug marked in `prophet_state.system_app_slug`; raises when none is marked | two probes |
| `get_disciple_app()` | The app named in `disciple_state.disciple_app_slug`, written by `init_disciple`; raises when no `disciple_state` row exists | two probes |
| `get_prophet_app_id()`, `get_disciple_app_id()` | The same as scalars | two probes |
| `get_app_id()` | The system app when one is marked, otherwise the app of `disciple_state` | two probes |

A hybrid is a prophet: `init_hybrid` runs `init` and registers its upstream in `prophet_peer`, and writes no `disciple_state` row. So on a hybrid `get_app()` and `get_app_id()` return the system app and `get_disciple_app()` raises; the app it mirrors is found by its slug in `prophet_peer`. The scalar forms exist for `DEFAULT` expressions, which cannot hold a subquery. A system app slug is never written into SQL, because every deployment names its own.
