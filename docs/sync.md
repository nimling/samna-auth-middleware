# Disciple sync

The disciple keeps its local replica current by pulling from the prophet. A consumer wires this with a single call and nothing else.

## Entry point

```go
sam.SyncStart(ctx)
```

`SyncStart(ctx context.Context)` is the whole contract. One line in the consumer main, after the database is initialized. It returns immediately. The consumer never spawns the goroutine, never checks debug mode, never writes a restart loop, and never handles a returned error channel.

## Where it pulls from

Sync is pull only. The prophet serves two routes under the `/sam` group, `GET /sam/sync/snapshot` and `GET /sam/sync/:seq`, and nothing pushes to a disciple or to the prophet: there is no inbound sync route on either side. The delta route reads `claimius.get_sync_events`, which returns the committed events of the app in sequence order, an organization or location written once per app linked to it. An empty pull is held open up to 25 s until an event of the app commits, and a cursor the feed cannot serve answers 409 and the disciple takes the snapshot.

## What travels

The snapshot carries `samna_user`, `organization`, `location`, `claim`, `user_claim`, `claim_object`, `samna_client`, `samna_secret`, `object_field`, `user_field`, `user_relation`, `audit`, `user_object`, `object_users` and `user_users`.

Organizations and locations are selected through `claimius.app_object_ids`, since those two tables carry no `app_id`. Every other table is selected with `app_id = ?`. `claimius.samna_app` is not applied on the disciple at all, and `app_id` is stripped from each payload before upsert.

This is why a machine client created on the prophet becomes usable on a disciple without any further step, and why it must be created under the serving app's id.

## What SyncStart owns internally

1. Debug check. When the process runs in debug mode the disciple is self contained and there is no prophet to pull from, so `SyncStart` does no work. The consumer does not gate the call on an env var.

2. Its own goroutine. `SyncStart` spawns the background worker itself and returns immediately. The consumer does not write `go func()`.

3. The pull loop. Initial snapshot or incremental pull, then a steady pull cadence that brings in every object the disciple did not directly create, including materialized access state.

4. Automatic restart with backoff. A failed pull is retried with exponential backoff that resets on the next success. A transient prophet outage self heals. The worker never exits on a recoverable error.

5. Error logging. Each failure is logged through the database logger. The consumer surfaces nothing itself.

## Consumer shape

```go
sam.SyncStart(context.Background())
e.Logger.Fatal(e.Start(":" + serverPort))
```

The only consumer code is the call. No debug branch. No restart logic. No lifecycle management. No error channel. Everything that makes the sync correct lives behind `SyncStart`.
</content>
