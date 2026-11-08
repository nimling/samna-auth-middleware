# Disciple sync

The disciple keeps its local copy current by pulling from the prophet. A consumer wires this with a single call and nothing else.

## Entry point

```go
if err := sam.Init(ctx); err != nil {
	e.Logger.Fatal(err)
}
```

`sam.Init(ctx context.Context) error` is the whole contract. One call in the consumer main, after `sdb.Init`, on every pod, with the context `main` makes from `signal.NotifyContext`. It returns at once, and returns an error only when it cannot start: no database, or a second call while it runs. Cancelling the context is the only way it ends; `sam.Done()` is closed once every worker has returned. The consumer never spawns the goroutine, never checks debug mode, never writes a restart loop and never elects a leader.

## Where it pulls from

Sync is pull only. The prophet serves two routes under the `/sam` group, `GET /sam/sync/snapshot` and `GET /sam/sync/:cursor`, and nothing pushes to a disciple or to the prophet: there is no inbound sync route on either side. The cursor is a `client.SyncCursor`, a `(txid, seq)` position written `<txid>.<seq>`. The delta route reads `claimius.get_sync_events`, which returns the app's events after the cursor in `(txid, seq)` order, only those whose transaction is below the `xmin` of the reading snapshot, so a sequence that commits late is never passed over. An organization or location event is written once per app linked to it. An empty pull is held open up to 25 s until an event of the app commits, and a cursor the feed cannot serve answers `409` and the disciple takes the snapshot.

## What travels

Source rows only: `samna_user`, `organization`, `location`, `claim`, `user_claim`, `claim_object`, `samna_client`, `samna_secret`, `object_field`, `user_field` and `user_relation`. The feed and the snapshot never carry `user_object`, `user_users` or `audit`; the disciple derives its own pairs from the claims, bindings and grants it applies.

Organizations and locations are selected through `claimius.app_object_ids`, since those two tables carry no `app_id`. Every other table is selected with `app_id = ?`. `claimius.samna_app` is not applied on the disciple at all, and `app_id` is stripped from each payload before upsert.

This is why a machine client created on the prophet becomes usable on a disciple without any further step, and why it must be created under the serving app's id.

## What sam.Init owns internally

1. Debug check. When the process runs in debug mode the disciple is self contained and there is no prophet to pull from, so `sam.Init` does no work. The consumer does not gate the call on an env var.

2. Its own goroutines. `sam.Init` spawns the background workers itself and returns immediately. The consumer does not write `go func()`.

3. One puller per database. Every pod runs `LeadSync`, and only the pod holding the session level advisory lock `SyncLockKey(app slug)` pulls. The others try again every 5 s and take over when the holder goes.

4. The pull loop. The snapshot when the cursor is zero, then one page after the cursor at a time, each applied in one transaction under `replay_mode` that moves the cursor first. Every row of the page is applied as it arrives; nothing is filtered or parked, since the prophet refuses a claim, grant or binding the app's feed could not carry. Between pulls it drains open access areas whose holders read often enough to be settled ahead.

5. Binding. The disciple binds to the prophet that answers it, routes its writes there on every pod, and moves only to a prophet that accepted a handover of the app from the bound one.

6. Automatic restart with backoff. A failed pull is retried with a delay that doubles up to 30 s and resets on the next success. Ten failed pulls in a row, or a `409`, take a fresh snapshot and audit the stalled cursor. The worker never exits on a recoverable error.

7. Error logging. Each failure is logged through the database logger. The consumer surfaces nothing itself.

## Reading a routed write back

A routed write answers with the app's feed position in `SAuth-Seq`, kept on the builder as `Seq()`. A read that must see that write calls `sdb.Instance().WaitForSeq(ctx, q.Seq(), timeout)`, which returns once the disciple's cursor has reached it.

## Consumer shape

The call and the join, in the order every runner of the middleware follows: [Lifecycle](../.nimpress/disciple.md#lifecycle). No debug branch. No restart logic. No error channel. Everything that makes the sync correct lives behind `sam.Init`. See [Sync in Go](../.nimpress/claimius/middleware/sync.md).
