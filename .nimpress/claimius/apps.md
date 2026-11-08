---
layout: doc
title: Apps
description: samna_app, how upsert_system_app and upsert_app create an app, the two scoping mechanisms, app scoped tables and app_object links, what each costs a query, scoping per role, and the identity helpers that tell SQL which app it runs as.
tags: samna_app, upsert_system_app, upsert_app, app_id, app_object, app_object_ids, upsert_app_object, delete_app_object, _app_ids_for_row, get_app, get_app_id, get_disciple_app, scoping, tenancy
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
| `claim_id` | The default claim `upsert_app_user` grants a new user |
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
| `upsert_app_object(app_id, object_type, object_id, created_by)` | Links, idempotent | one probe; on a new link one row with four index entries and, outside replay, `|affected(x)|` pair recomputes |
| `delete_app_object(app_id, object_type, object_id)` | Soft deletes the link | one update, and a delete of the app's `U_x` pairs on the object, counted out of `user_users` |
| `upsert_root_organization(app_id, name, actor, ...)` | Creates a root and links it, idempotent on app and name | see [Organizations](./organizations.md) |
| `upsert_app_organization(app_id, org_id, cascade)` | Links an organization and, with `cascade`, every organization and location under it | one `upsert_app_object` per node |

The unique index is `(app_id, object_type, object_id)` where `sa_deleted_at IS NULL`, so a live link is unique per app and object. `app_object` is never written directly.

Users, grants and access are per app by nature, since the same person holds different grants in two products, so a per app row costs nothing extra. Organizations and locations are shared structure: the same company and the same building serve every product. With `n_a` apps reaching an organization, the link stores the organization once and `n_a` link rows, and a move changes the closure once.

## Creating an app

Every row claimius holds carries the `user_claim.id` it was written under in `sa_created_by`, and every binding is judged against what that grant's user holds. An app is created by one of two functions, so that both hold from its first row.

`upsert_system_app` creates the system app of a fresh prophet with its system organization, system user, system claim and default claim. It settles the system user's `user_claim.id` first, the live one on a rerun or a fresh id that the grant row takes once its claim exists, and stamps every row it writes with it. The grant lands before the bindings and the system user's pairs on the organization and the app are recomputed, so each binding is judged against the access the system user holds as creator of both.

`upsert_app` registers every other app, owned by an existing organization, under the authority of an actor of another app:

```sql
claimius.upsert_app(
    p_actor_user_claim_id UUID,
    p_slug                TEXT,
    p_name                TEXT,
    p_org_id              UUID,
    p_private_key         TEXT,
    p_private_seed        TEXT,
    p_redirect_uris       TEXT[]  DEFAULT ARRAY[]::TEXT[],
    p_sync_uri            TEXT    DEFAULT '',
    p_description         TEXT    DEFAULT '',
    p_connection          TEXT    DEFAULT 'code',
    p_owner_access        INTEGER DEFAULT 15,
    p_default_claim       JSONB   DEFAULT NULL,
    p_app_id              UUID    DEFAULT NULL
) RETURNS JSONB
```

It stamps `p_actor_user_claim_id` into every `sa_created_by` and:

1. Refuses, SQLSTATE `22023`, an actor that is not a live `user_claim`, an organization that is not live, and a slug already owned by another organization.

2. Upserts the `samna_app` row on its slug, owned by `p_org_id`, and its `prophet_state` row.

3. Copies the actor's `samna_user` row from the actor's app into the new app.

4. Links the organization and its tree to the app, `upsert_app_organization` with `cascade`.

5. Creates the owner claim, `<name> Owner` with `p_owner_access`, inheriting, binds it on the organization and on the app, and grants it to the actor's user in the app.

6. Creates the default claim, `<name> Reader` with read unless `p_default_claim` gives `name`, `description` or `sa_access`, binds it on the app and on the organization, and sets it as the app's `claim_id`.

It is idempotent on the slug and on each claim's name, and returns one jsonb object keyed `samna_app`, `organization`, `samna_user`, `claim`, `default_claim`, `user_claim` and `claim_object`. The owner claim's first binding is the one binding of an app that no pair can authorize, because the app has none yet; it passes when the actor holds owner on the organization that owns the app, judged in the actor's own app. That rule is how the authority over an organization extends to the apps it owns; see [Claims](./claims.md). `app_secret` is written afterwards by the implementing side, as for the system app, and the reader role cannot call `upsert_app`.

## Which apps a write belongs to

`_app_ids_for_row(object_type, row)` scopes a row for its calc trigger: the row's `app_id` when its table has the column; otherwise its live `app_object` links; otherwise, for a table of the implementing side, `get_app_id()`. An internal table without `app_id`, an organization or location, resolves to no app until it is linked, and the link settles its pairs when it lands. The calc trigger runs its per app work once per app the row resolves to, so a row linked to `n_a` apps raises `n_a` areas.

`upsert_table_info` refuses a table without `app_id` on a prophet that hosts apps besides its system app, because `get_app_id()` would scope every row of it to the system app.

## Scoping per role

1. A prophet holds every app it is home for, and every app it mirrors from a peer. Every read function takes the app as a parameter and filters on it in its body.

2. A disciple mirrors one app. Its tables carry no `app_id` column and it has no `app_object`: every row it holds is already in scope, and its read functions take no app argument, `get_objects(user, type, required, after, limit)`. The prophet selects what reaches a disciple by `app_id` and `app_object_ids`, so scoping is written on the prophet side only; see [Sync](./sync.md).

## Knowing which app the SQL runs as

The identity helpers live in the prophet set, where more than one app exists.

| Function | Returns | Reads |
|---|---|---|
| `get_app()` | The system app, by the slug marked in `prophet_state.system_app_slug`; raises when none is marked | two probes |
| `get_disciple_app()` | The app named in `disciple_state.disciple_app_slug`, written by `upsert_disciple_state`; raises when no `disciple_state` row exists | two probes |
| `get_prophet_app_id()`, `get_disciple_app_id()` | The same as scalars | two probes |
| `get_app_id()` | The system app when one is marked, otherwise the app of `disciple_state` | two probes |

A hybrid is a prophet: `upsert_prophet_peer` runs `upsert_system_app` and registers its upstream in `prophet_peer`, and writes no `disciple_state` row. So on a hybrid `get_app()` and `get_app_id()` return the system app and `get_disciple_app()` raises; the app it mirrors is found by its slug in `prophet_peer`. The scalar forms exist for `DEFAULT` expressions, which cannot hold a subquery. A system app slug is never written into SQL, because every deployment names its own.
