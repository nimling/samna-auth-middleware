---
layout: doc
title: Usage
description: What claimius records about how its objects and apps are used, the per user relations with used_at, and the prophet's consent gated capture of sessions and events folded into a heat grid, with the rows each step reads and writes and how reading it is itself a scope grant.
tags: usage, user_relation, used_at, consent, analytics_session, analytics_event, analytics_heat, analytics_capture, capture_user_apps, scope grant
lastUpdated: true
---

# Usage

Claimius records use in two places: per user relations to objects, which a user writes and which travel with the app, and capture of sessions and events, which a prophet holds for the apps it serves.

## Per user relations

`user_relation` holds one row per user, app and related object: `pinned`, `priority`, `used_at`, a `description`, and a `parent_id` and `sibling_id` into the user's own relations for ordering. It carries recents, favourites and per user tags against any object.

1. A relation belongs to its user alone. `resolve_access` answers `claimius.user_relation` with one probe matching `id` and `user_id`, and reads no grant.

2. A relation is keyed by the object pair and reads nothing of the object, so relating to an object costs one row with its seven btree entries, one object area row, settled by the next read in the app at no pair, and one feed event, whatever the object is.

3. A recents list is the user's range of `idx_user_relation_user_id` ordered by `used_at`. Joined to the user's pairs it lists only what the user still reaches.

4. `user_relation` is carried on the feed, so a user's relations are the same on the prophet and on the disciple of the app.

## Capture on the prophet

A prophet records how its apps are used, gated by consent and read through a scope grant. A disciple holds no capture: it forwards consent and capture to its prophet on its app key and reads them back from there.

| Table | One row per | Indexes |
|---|---|---|
| `consent` | decision of a user, or an anonymous visitor, in an app | one live decision per user and per anonymous id, unique partial |
| `analytics_session` | session key of an app | `(app_id, session_key)` unique, `(app_id, user_id, started_at)`, `(app_id, anonymous_id, started_at)` |
| `analytics_event` | signal | `(session_id, occurred_at)`, `(app_id, path, occurred_at)`, `(app_id, type, occurred_at)`, BRIN on `received_at` |
| `analytics_heat` | app, path, viewport width, type, day and cell | primary key over all of them, `(app_id, path, day)` |
| `analytics_capture` | app and path | a serialised copy of the page, replaced on each post |

### Recording

1. Consent first. `post_consent` revokes the live decision and inserts the new one: one update, one insert. `consent_allows(app, category, user, anonymous)` reads the live decision, one probe, and a session is recorded only when it allows `analytics`.

2. `post_analytics_session` upserts the session on `(app_id, session_key)`: one row.

3. `post_analytics_events` inserts a batch of `n` events in one statement from a jsonb array and moves the session's `last_seen_at`: `n + 1` rows written, `4n` btree entries and a BRIN summary that only changes when a new page range starts. Arrival order is `received_at` order, which is why that column is BRIN: one summary per range of pages, where a btree would add one entry per event ([PostgreSQL, BRIN Indexes](https://www.postgresql.org/docs/17/brin.html)).

### The heat grid

`post_analytics_heat(from, to, app)` folds every positioned event of whole days into cells: the page coordinates divided by `analytics_bucket_size()`, 16, floored, per app, path, viewport width, type and day, with the summed weight, a click 4 and any other signal 1, and the distinct sessions. For `E` positioned events on one page, day, type and width of a page `W` wide and `H` high:

```
cells ≤ min(E, ⌈W / 16⌉ ⌈H / 16⌉)
```

A cell is replaced, not added to, and only when its weight or sessions changed, so folding a day again is idempotent and writes only the cells that moved. `get_analytics_heat` reads one range of `idx_analytics_heat_read` for the page and days, the cells, not the events.

`delete_analytics_event(before)` folds every day before the cutoff and then deletes the raw events of those days. The heat grid keeps everything it had and the raw signal leaves: storage moves from `E` events to at most the grid, per page and day. The cutoff is a whole day in the past, because a cell replaced from half a day would lose the other half.

### Reading capture is a scope grant

`capture_user_apps(user)` returns the apps whose capture a person may read: the app of every live grant carrying a scope binding under `$.analytics` with read and no deny, plus every app id that binding names under `analytics.app`. `analytics_enabled(app)` is true when the app holds any such grant. Both are the capability match of [Capabilities](./capabilities.md): `1 + 2C + b` entries and `b` jsonpath tests. `get_analytics_events`, `get_analytics_sessions` and `get_analytics_summary` filter their rows by that set of apps, so reading capture is gated by the same claims as everything else, with no table of its own.
