---
gate: samna-developer
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
| `private_key` | The key the prophet signs the app's tokens with, wrapped at rest under `SAUTH_MASTER_KEY` |
| `private_seed` | The seed the app's secrets are encrypted with, wrapped at rest under `SAUTH_MASTER_KEY`; `GET /sam/app` answers it opened to the app itself, which holds it in memory to open its secrets |
| `app_secret`, `secret_version` | The hashed secret a disciple authenticates with |
| `redirect_uris`, `connection`, `provider_ids` | The login configuration |
| `claim_id` | The default claim `upsert_app_user` grants a new user |
| `home_prophet` | The peer prophet that decides the app's writes, NULL when this prophet does; see [Prophets and disciples](./prophets.md) |
| `sa_owner_id` | The organization that owns the app |

## Two scoping mechanisms

1. Own column. On a prophet, `samna_client`, `samna_secret`, `app_object`, `user_identity`, `audit`, `object_field`, `user_field`, `user_relation`, `object_asset`, `app_cost`, `code`, `notification`, `sync_event` and the consent and analytics tables carry `app_id`. A row of one of them belongs to one app. `samna_user`, `claim`, `user_claim`, `claim_object`, `user_object`, `user_users` and `access_diff` carry none: a claim reaches every app linked to its owning organization, a grant or a binding the apps of its claim, and a user the apps of the claims it holds.

2. Links. `organization` and `location` carry no `app_id`. An object belongs to every app holding a live `app_object` row for it.

| Function | Does |
|---|---|
| `app_object_ids(app_id, object_type)` | The object ids of one type linked to an app, the filter in place of `app_id = ?` |
| `app_has_object(app_id, object_type, object_id)` | The boolean guard |
| `apps_for_object(object_type, object_id, app_id?)` | The apps that see one object: its `app_object` links; for a user, every app linked to the owning organization of a claim the user holds a live grant of; the app itself; for a claim, every app linked to its owning organization; else `app_id` when given |
| `upsert_app_object(app_id, object_type, object_id, created_by)` | Links, idempotent, raises no area and moves no pair; returns the link id |
| `delete_app_object(app_id, object_type, object_id)` | Soft deletes the link; no pair moves |
| `upsert_root_organization(app_id, name, actor, ...)` | Creates a root and links it, idempotent on app and name; see [Organizations](./organizations.md) |
| `upsert_app_organization(app_id, org_id, cascade, created_by)` | Links an organization and, with `cascade`, every organization and location under it |

The unique index is `(app_id, object_type, object_id)` where `sa_deleted_at IS NULL`, so a live link is unique per app and object. `app_object` is never written directly.

A user is one row and a grant one row, and the apps that see them follow from the organization the claim is owned in: the same person holds different claims in two products because the claims are owned in different trees. Organizations and locations are shared structure: the same company and the same building serve every product, so they are stored once and linked to each app that uses them, and a move changes their closure once.

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
    p_app_id              UUID    DEFAULT NULL,
    p_parent_id           UUID    DEFAULT NULL
) RETURNS JSONB
```

It stamps `p_actor_user_claim_id` into every `sa_created_by` and:

1. Refuses, SQLSTATE `22023`, an actor that is not a live `user_claim`, an organization that is not live, and a slug already owned by another organization.

2. Upserts the `samna_app` row on its slug, owned by `p_org_id`, and its `prophet_state` row.

3. Inserts the anonymous user when absent, as `upsert_system_app` does; it copies no user row.

4. Links the organization and its tree to the app, `upsert_app_organization` with `cascade`.

5. Creates the owner claim, `<name> Owner` with `p_owner_access`, inheriting, binds it on the organization and on the app, and grants it to the actor's user in the app.

6. Creates the default claim, `<name> Reader` with read unless `p_default_claim` gives `name`, `description` or `sa_access`, binds it on the app and on the organization, and sets it as the app's `claim_id`.

It is idempotent on the slug and on each claim's name, and returns one jsonb object keyed `samna_app`, `organization`, `samna_user`, `claim`, `default_claim`, `user_claim` and `claim_object`. The owner claim is bound on the organization before it is bound on the app, and the `samna_app` row sits under its owning organization in the ownership tree, so the actor who holds owner on that organization reaches the app through the inheriting binding and the binding on the app is judged like any other; `_check_claim_composition` has no case for an app. That is how the authority over an organization extends to the apps it owns; see [Claims](./claims.md). `app_secret` is written afterwards by the implementing side, as for the system app, and the reader role cannot call `upsert_app`.

## Initiate apps

Apps form a tree through `sa_parent_id` and `sa_root_id`, the same parenthood columns any registered table can carry: a root app references itself in both, and the `parenthood` closure keeps every Initiate app under its root, so a grant on the root app reaches its Initiate apps.

An Initiate app is another frontend of a disciple's app. It has its own slug, `redirect_uris`, pages and style. Its first redirect URI is the origin its frontend lives on, and the disciple's callback marked with its slug, `<SAUTH_REDIRECT_URI>?app=<slug>`, is among them. Everything else is the root's: users, claims, organizations, the sync and the tokens.

`upsert_app` with `p_parent_id` registers one. The parent must be live and owned by `p_org_id`; the call upserts the `samna_app` row under the parent and returns `{"samna_app": ...}` without a `prophet_state` row, a `samna_user`, organization links or claims. A rerun with another parent, or without one for an Initiate app, is refused with `22023`: an app is never reparented.

A disciple reads its root's Initiate apps from `GET /sam/app/initiate` and keeps them in memory, refreshed every minute. The frontend opens `/api/auth/<slug>/login`; the disciple authorizes that app with the marked callback as `redirect_uri`, the prophet returns to `/api/auth/callback?app=<slug>`, the callback exchanges the code with the root's credentials and that same `redirect_uri`, and the person returns to the Initiate app's origin. `/api/auth/<slug>/logout` returns there too.

The Initiate app's configuration in samna_auth decides how its frontend reaches the disciple. Its registered callback is either the disciple's own `/api/auth/callback?app=<slug>` or the frontend's host with the same path when that host routes `/api` to the disciple. `cookie_domain` and `cookie_same_site` set its session cookies: host only or a shared parent domain, and `none` when the frontend calls the disciple from another site. Every origin among its redirect URIs is allowed by `smw.GetOriginAllowed` in the disciple's CORS, and `smw.InitiateMiddleware` keeps a refresh on the same cookie settings.

## Login shapes

An app carries one `connection`, read once at login. It decides where the session lands once the provider has signed the user in, and it is the only thing that differs between a product service, a site behind the bff binary and a site with no server at all. There is one app model: a root app, its connection, and its Initiate apps.

| `connection` | The provider callback | The session lands | Used by |
|---|---|---|---|
| `code` | Mints an auth code and redirects to the app's redirect URI; the app's own callback exchanges it with the app secret, server to server | In cookies the disciple writes for its own host | Every product service |
| `bff` | Writes the session cookies itself and redirects the browser to `next` | In cookies the prophet writes for the host the request came in on | A static build or a dev server behind [sauth-bff](../bff.md), the disciple with no tables |
| `static` | Writes the session cookies on its own host and redirects to the page the login started from | In cookies on the prophet host, read through the prophet's `/api/me/*` with credentials | A site with no server of its own, see [Static sites](../static.md) |

With `bff` on another host than the prophet, the callback hands the browser to the site's `<base>/session` with a one time value, which the bff proxies to the prophet with the site's host forwarded, so the prophet writes the cookies for the site's domain and redeems the value once. On the same host the callback writes the cookies directly. Nothing else in the model depends on the connection: the claims, bindings, pages and the checks are the same under all three.

An Initiate app, below, is a second frontend of the same root under any connection, with its own slug, callback, cookie domain and origins.

### Cost

A check reads nothing for the connection: it is read once at login, with the app row the login already reads. A disciple holds its root's Initiate apps in memory from one `GET /sam/app/initiate` a minute. The cross host session costs one row written and one probe to redeem it, once per login.

## Which apps a write belongs to

`_app_ids_for_row(object_type, row)` names the apps a row reaches, for the outbox and the home check:

1. A claim resolves to the apps linked to its owning organization, a `user_claim` or `claim_object` to the apps of its claim, and a `samna_user` to the apps where the user holds a live grant of a claim owned in a linked organization, each through `apps_for_object`. A `samna_app` row resolves to no app.

2. Otherwise the row's `app_id` when its table has the column.

3. Otherwise the apps `apps_for_object` names for it, which for an organization or location are its live `app_object` links.

4. Otherwise, for a table of the implementing side, `get_app_id()`. An internal table without `app_id`, an organization or location, resolves to no app until it is linked.

The sync trigger writes one outbox row per app the row resolves to, so a row that reaches several apps is published to each; the calc triggers recompute the actor's pair once and raise one area, whatever the number of apps.

`upsert_table_info` requires `sa_owner_id`, `sa_created_by`, `sa_created_at`, `sa_updated_at` and `sa_deleted_at`, and `sa_parent_id` with `sa_root_id` both or neither; it asks nothing about `app_id`.

## Scoping per role

1. A prophet holds every app it is home for, and every app it mirrors from a peer. The access reads take the user alone, `get_objects(user, type, required, after, limit)`, on both roles; only the reads that join an app's own rows take the app as a parameter, `get_organizations`, `get_locations`, `get_audit`, `get_secrets`, `get_object_assets`, `get_app_cost`, `get_owner_graph` and `get_graph_links_claim`.

2. A disciple mirrors one app. It has no `app_object` and no `app_id` column: every row it holds is already in scope. The prophet selects what reaches a disciple by `app_id`, by its `app_object` links and by the organization routing of claims, grants, bindings and users, so scoping is written on the prophet side only; see [Sync](./sync.md).

## Knowing which app the SQL runs as

The identity helpers live in the prophet set, where more than one app exists.

| Function | Returns |
|---|---|
| `get_app()` | The system app, by the slug marked in `prophet_state.system_app_slug`; raises when none is marked |
| `get_disciple_app()` | The app named in `disciple_state.disciple_app_slug`, written by `upsert_disciple_state`; raises when no `disciple_state` row exists |
| `get_prophet_app_id()`, `get_disciple_app_id()` | The same as scalars |
| `get_app_id()` | The system app when one is marked, otherwise the app of `disciple_state` |

A hybrid is a prophet: `upsert_prophet_peer` runs `upsert_system_app` and registers its upstream in `prophet_peer`, and writes no `disciple_state` row. So on a hybrid `get_app()` and `get_app_id()` return the system app and `get_disciple_app()` raises; the app it mirrors is found by its slug in `prophet_peer`. The scalar forms exist for `DEFAULT` expressions, which cannot hold a subquery. A system app slug is never written into SQL, because every deployment names its own.

## Cost

The `user_object` primary key is `(user_id, object_type, object_id)`, `idx_user_claim_user_app` is `(user_id)` and `idx_user_users_viewer` is `(viewer_id)`: one descent finds the user, and no app filter is read.

| Function | Reads and writes |
|---|---|
| `app_object_ids` | one range of `idx_app_object_unique` |
| `app_has_object` | one probe |
| `apps_for_object` | one range of `idx_app_object_object`; for a user one range of `idx_user_claim_user_app` with a claim probe and an `app_object` probe per grant; for a claim one claim probe and one `app_object` probe |
| `upsert_app_object` | one probe; on a new link one row with four index entries |
| `delete_app_object` | one update |
| `upsert_app_organization` | one `upsert_app_object` per node of the tree |
| `get_app`, `get_disciple_app`, `get_prophet_app_id`, `get_disciple_app_id`, `get_app_id` | two probes |

With `n_a` apps reaching an organization, the link stores the organization once and `n_a` link rows, and a row linked to `n_a` apps writes `n_a` outbox rows and raises one area on a write. A per app row on the scoped tables costs nothing extra, since those rows differ per app in any case.
