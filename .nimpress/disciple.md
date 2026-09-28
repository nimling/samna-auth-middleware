---
layout: doc
title: Disciple
description: How a disciple writes through to the prophet, what it owns itself, and how the pull loop keeps its replica level.
tags: disciple, prophet, sync, write through, sync_event, access_diff, replay mode
lastUpdated: true
---

# Disciple

A disciple is a product service that mirrors one app from the prophet. It answers every access question from its own database, and it carries application tables the prophet has never seen.

## Wiring

```go
if _, err := sdb.Init(e.Logger); err != nil {
	e.Logger.Fatal(err)
}
api := e.Group("/api", smw.UserAuthMiddleware(nil))
sam.RegisterEndpoints(api)
sam.SyncStart(context.Background())
```

`SyncStart` returns immediately and owns its goroutine, backoff and logging. In debug mode it does nothing.

## Writes

The builder is the same in both modes. `sdb.Instance().NewInsert()`, `NewUpdate()` and `NewDelete()` take a model and the `AccessClaim` the actor holds.

```go
ac, err := sdb.Instance().MutationAccess(ctx, actor, orgID, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
if err != nil {
	return err
}
err = sdb.Instance().NewInsert().Model(&location).Access(ac).Scan(ctx, &location)
```

Where the write lands depends on the table.

| Table | Deployed | Debug |
|---|---|---|
| `claim`, `claim_object`, `user_claim` | Sent to the prophet, the answer is upserted locally | Local claimius functions |
| `organization`, `location`, `user_relation`, `samna_secret`, `samna_user` | Sent to the prophet, the answer is upserted locally | Local claimius functions |
| The service's own registered tables | Local, through bun | Local, through bun |

A routed write stamps `sa_created_by` and `sa_owner_id` from the `AccessClaim`, posts the model to the prophet over the app key, and upserts the row the prophet answers with. The write is visible on the disciple as soon as the call returns, and every other disciple of the app receives it through sync.

The service's own tables never leave the disciple. The prophet stores the `claim_object` rows that bind a claim to them, addressed by `object_type` and `object_id`, and the disciple settles `user_object` for them locally.

## The pull loop

The disciple pulls. There is no inbound sync endpoint on a disciple.

1. On first start the cursor is zero, so it fetches `sync/snapshot`, upserts every row and records the snapshot sequence.
2. It then fetches `sync/<cursor>` for deltas, applies them under `replay_mode` and moves `disciple_state.last_applied_seq`.
3. It polls every 100 ms while events arrive, backs off to 15 s when idle, and doubles a failure delay up to 30 s.
4. Forty empty pulls, or ten failed ones, trigger a fresh snapshot and an audit row naming the stalled cursor.

The feed is scoped by the prophet: organizations and locations through `app_object_ids`, every other table through `app_id`. The disciple strips `app_id` from each payload and skips `samna_app`.

A snapshot upserts; it never deletes a row the prophet did not send.

## Settling access

Replay mode silences the calc triggers, because the feed already carries the computed state for the tables the prophet knows. For the disciple's own tables the prophet has nothing to send, so every applied `claim`, `claim_object` or `user_claim` marks its claim area in `access_diff`. The next `get_*` that reads that area runs `reconcile_if_pending` and settles it for every user in it. An area nobody reads costs nothing.

A local write settles the acting user inside the same statement, so a handler reads its own write back. Another user's `user_object` row appears on that user's next read.

## What the disciple never does

1. Select from a claimius table directly. Reads go through `get_*` and the `sdb` lookups.
2. Set `replay_mode`. Only the pull loop does.
3. Filter by `app_id`. The column does not exist on a disciple.
4. Sign, decrypt or issue a token. It verifies tokens against the prophet's JWKS.
