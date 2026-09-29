---
layout: doc
title: Usage
description: What claimius records about how its objects and apps are used, the per pair count of checks with its decay and where it is read, the per user relations, and the prophet's consent gated capture of sessions and events folded into a heat grid, with the rows each step reads and writes.
tags: usage, used_count, last_used_at, touch_object_usage, _decayed_usage, user_activity, user_relation, consent, analytics_session, analytics_event, analytics_heat, analytics_capture, capture_user_apps
lastUpdated: true
---

# Usage

Claimius records use in three places: a count on each pair of how often its user checked the object, local to the database; per user relations to objects, which a user writes and which travel with the app; and capture of sessions and events, which a prophet holds for the apps it serves.

## The count on the pair

`user_object` carries `used_count` and `last_used_at`: how many successful single object checks of the pair were counted, and when the latest flush that counted one ran. A settle never writes them, so they survive every recompute of the pair, and they leave with the pair.

### Recording

The middleware counts in process and flushes in batches; see [Access in Go](./middleware/access.md). A flush is one call:

```
touch_object_usage(user_ids[], app_ids[], object_types[], object_ids[], counts[])
```

1. The arrays are grouped per pair and summed, so repeats of a pair in one call add up.

2. The pairs that exist are locked `FOR NO KEY UPDATE` in key order and each updated once: `used_count = used_count + n`, `last_used_at = now()`. A pair that is not settled, that has no `user_object` row, is skipped, so usage never creates a pair and never outlives one. Key order makes two replicas flushing the same pairs wait on each other instead of deadlocking.

3. Every user and app counted moves its `user_activity` row to now, one upsert per distinct `(user, app)`.

4. Nothing is written to `sync_event`: usage is local to the database that served the checks.

For a flush of `K` distinct pairs of `J` distinct users:

```
entries read = K          rows written ≤ K + J          outbox rows = 0          statements = 1
```

An update of `used_count` and `last_used_at` touches no indexed column, so on a page with room, which `fillfactor = 90` leaves, it is a heap only update and writes no index entry. The overload `touch_object_usage(user_ids[], object_types[], object_ids[], counts[])` fills the app with `get_app_id()`.

### Decay

```
_decayed_usage(n, t) = n · 2^(−max(now − t, 0) / 14 days)          0 when n = 0 or t is NULL
```

A count halves every 14 days after its latest use, computed at read time from the two columns, so nothing is rewritten to age it. Of two pairs used equally often, the one used lately weighs more, and a burst of use months ago fades to nothing.

### Where it is read

| Reader | Uses |
|---|---|
| `search` | `rank = ts_rank(search_vector, q) + _decayed_usage(used_count, last_used_at)`; see [Search](./search.md) |
| `get_owner_graph`, `get_claim_graph` with `p_sort = 'usage'` | Children in a window ordered by decayed usage, heaviest first; see [Graphs](./graphs.md) |
| `drain_access_diff` | `user_activity.last_used_at` as the read rate estimator `1 / (now − last_used_at)`; see [Materialization](./materialization.md#the-drain) |

## Per user relations

`user_relation` holds one row per user, app and related object: `pinned`, `priority`, `used_at`, a `description`, and a `parent_id` and `sibling_id` into the user's own relations for ordering. It carries recents, favourites and per user tags against any object.

1. A relation belongs to its user alone. `resolve_access` answers `claimius.user_relation` with one probe matching `id` and `user_id`, and reads no grant.

2. A relation is keyed by the object pair and reads nothing of the object. Writing one costs one row with its seven btree entries, no closure row, since it carries no owner, one object area, and one outbox row.

3. A recents list is the user's range of `idx_user_relation_user_id` ordered by `used_at`. Joined to the user's pairs it lists only what the user still reaches.

4. `user_relation` is carried on the feed, so a user's relations are the same on the prophet and on the disciple of the app.

## Capture on the prophet

A prophet records how its apps are used, gated by consent and read through a scope grant. A disciple holds no capture: it forwards consent and capture to its prophet on its app key and reads them back from there.

| Table | One row per | Indexes |
|---|---|---|
| `consent` | decision of a user, or an anonymous visitor, in an app | one live decision per user and per anonymous id, unique partial |
| `analytics_session` | session key of an app | `(app_id, session_key)` unique, `(app_id, user_id, started_at)`, `(app_id, anonymous_id, started_at)` |
| `analytics_event` | signal | `(session_id, occurred_at)`, `(app_id, path, occurred_at)`, `(app_id, type, occurred_at)`, BRIN on `received_at` |
| `analytics_heat` | app, path, viewport width, type, day and cell | primary key over all of them, `idx_analytics_heat_read` on `(app_id, path, day)` |
| `analytics_capture` | app and path | a serialised copy of the page, replaced on each post |

### Recording

1. Consent first. `post_consent` revokes the live decision and inserts the new one: one update, one insert. `consent_allows(app, category, user, anonymous)` reads the live decision, one probe, and a session is recorded only when it allows `analytics`.

2. `post_analytics_session` upserts the session on `(app_id, session_key)`: one row.

3. `post_analytics_events` inserts a batch of `n` events in one statement from a jsonb array and moves the session's `last_seen_at`: `n + 1` rows written, `4n` btree entries and a BRIN summary that changes only when a new page range starts. Arrival order is `received_at` order, which is why that column is BRIN: one summary per range of pages, where a btree would add one entry per event ([PostgreSQL, BRIN Indexes](https://www.postgresql.org/docs/17/brin.html)).

### The heat grid

`post_analytics_heat(from, to, app)` folds every positioned event of whole days into cells: the page coordinates divided by `analytics_bucket_size()`, 16, floored, per app, path, viewport width, type and day, with the summed weight of `analytics_event_weight`, a click 4 and any other signal 1, and the distinct sessions. For `E` positioned events on one page, day, type and width of a page `W` wide and `H` high:

```
cells ≤ min(E, ⌈W / 16⌉ ⌈H / 16⌉)
```

A cell is replaced, not added to, and only when its weight or sessions changed, so folding a day again writes only the cells that moved. `get_analytics_heat` reads one range of `idx_analytics_heat_read` for the page and days: the cells, not the events.

`delete_analytics_event(before)` folds every day before the cutoff and then deletes the raw events of those days. The heat grid keeps everything it had and the raw signal leaves: storage moves from `E` events to at most the grid, per page and day. The cutoff is a whole day in the past, because a cell replaced from half a day would lose the other half.

### Reading capture is a scope grant

`capture_user_apps(user)` returns the apps whose capture a person may read: the app of every live grant carrying a scope binding under `$.analytics` with read and no deny, plus every app id that binding names under `analytics.app`. `analytics_enabled(app)` is true when the app holds any such grant. Both are the capability match of [Capabilities](./capabilities.md), which reads the user's grants and their scope bindings and evaluates one jsonpath per binding. `get_analytics_events`, `get_analytics_sessions` and `get_analytics_summary` filter their rows by that set of apps, so reading capture is gated by the same claims as everything else, with no table of its own.
