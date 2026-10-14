# App scoping

A prophet hosts many apps in one database. A disciple mirrors exactly one. Scoping is what keeps those apps apart, and there are two mechanisms. Which one applies is a property of the table, not a choice at the call site.

## Own `app_id` column

Most tables carry `app_id` and belong to exactly one app: `samna_user`, `claim`, `user_claim`, `claim_object`, `user_object`, `user_users`, `user_relation`, `user_field`, `object_field`, `samna_client`, `samna_secret`, `audit`, `access_diff`.

The same human gets one `samna_user` row per app, all sharing one `user_id`. Every query against these tables on a prophet filters on `app_id`.

## `app_object` links

`claimius.organization` and `claimius.location` have no `app_id` column. They are shared hierarchy objects. An object belongs to every app holding a live link in `claimius.app_object`:

```sql
CREATE TABLE claimius.app_object (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id        UUID NOT NULL REFERENCES claimius.samna_app(id),
    object_type   TEXT NOT NULL,
    object_id     UUID NOT NULL,
    sa_created_by UUID,
    sa_deleted_at TIMESTAMPTZ,
    sa_created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

A live link is unique per `(app_id, object_type, object_id)`. One organization tree can serve several apps without being duplicated once per app, and unlinking one app leaves the other apps untouched.

## The helpers

| Function | Returns | Use |
|----------|---------|-----|
| `app_object_ids(app_id, object_type)` | `SETOF UUID` | The filter that stands in for `app_id = ?` on organizations and locations |
| `apps_for_object(object_type, object_id, app_id?)` | `SETOF UUID` | Which apps see this object: `app_object` links, the apps holding a user, the app itself, a claim's app, else `app_id` |
| `app_has_object(app_id, object_type, object_id)` | `BOOLEAN` | Guard before acting on a shared object |
| `upsert_app_object(app_id, object_type, object_id, created_by)` | `UUID` | Create the link and recompute the pair of every user who can reach the object in the app. Idempotent |
| `delete_app_object(app_id, object_type, object_id)` | `BOOLEAN` | Soft delete the link and delete the app's `user_object` rows on the object |
| `upsert_root_organization(app_id, name, ...)` | | Create a root and link it in one call. Idempotent on app and name |

`app_object` is never written directly. `upsert_app_object` and `delete_app_object` also maintain the materialized access rows, which a bare INSERT would skip.

Filtering a shared table:

```sql
SELECT * FROM claimius.organization
WHERE id IN (SELECT claimius.app_object_ids($app_id, 'claimius.organization'));
```

## What the disciple sees

A disciple has no `app_object` table, and its `organization` and `location` tables have no `app_id` column either. It holds one app's data by construction, so there is nothing to filter against.

The three pieces that make that work:

1. The prophet selects organizations and locations for the sync snapshot through `app_object_ids`, and every other table through `app_id = ?`. On the feed an organization or location event is written once for every app linked to the row, stamped with that app, a new link publishes the linked row to its app, and an unlink publishes the row's delete to that app alone.
2. The disciple strips `app_id` from every payload before upsert and skips `claimius.samna_app` entirely.
3. The prophet refuses with `P0403` a `claim`, `user_claim` or `claim_object` whose owning organization, or whose bound organization or location, is not linked to the row's app, since that row could never reach the app's disciples. `claimius._check_app_scope` is the trigger and `claimius.app_has_scope(app_id, owner_id, object_type, object_id)` the test. So every row the feed carries lands on the disciple as it arrives; nothing is filtered or parked.

So app scoping is written on the prophet side only. A disciple query that filters organizations by `app_id` refers to a column that does not exist there.

## Consequences worth knowing

A disciple whose prophet fails to select organizations receives none, and `RootAccess`, which checks the lowest `sa_root_id` among the actor's live claims whose root organization is live, then finds none. Every handler that gates on `RootAccess` answers 403 while every other part of the service looks healthy. When a disciple reports blanket 403s on listing endpoints, check the organization count in its replica before looking at claims.

A new app on an existing prophet does not get organizations by copying them. It gets them by linking: `upsert_app` links the owning organization and its tree when it creates the app, `upsert_root_organization` links a fresh tree, and `upsert_app_object` links an existing one.
