# Disciple sync

The disciple keeps its local replica current by pulling from the prophet. A consumer wires this with a single call and nothing else.

## Entry point

```go
syncErrs := sasync.Start(ctx)
```

`Start(ctx context.Context) <-chan error` is the whole contract. One line in the consumer main. It returns a buffered error channel the main server process can read to surface sync trouble. The consumer never spawns the goroutine, never checks debug mode, never writes a restart loop.

## What Start owns internally

1. Debug check. When the process runs in debug mode the disciple is self contained and there is no prophet to pull from, so `Start` does no work and returns a channel that never emits. The consumer does not gate the call on an env var.

2. Its own goroutine. `Start` spawns the background worker itself and returns immediately. The consumer does not write `go func()`.

3. The pull loop. Initial snapshot or incremental pull, then a steady pull cadence that brings in every object the disciple did not directly create, including materialized access state.

4. Automatic restart with backoff. A failed pull is retried with exponential backoff that resets on the next success. A transient prophet outage self heals. The worker never exits on a recoverable error.

5. Error surfacing. Each failure is offered to the returned channel without blocking the worker. The channel is buffered and the send is dropped when full, so a consumer that ignores the channel never stalls the loop.

## Consumer shape

```go
syncErrs := sasync.Start(context.Background())
go func() {
    for err := range syncErrs {
        e.Logger.Errorf("disciple sync: %v", err)
    }
}()
e.Logger.Fatal(e.Start(":" + serverPort))
```

The only consumer code is the call and an optional drain to log. No debug branch. No restart logic. No lifecycle management. Everything that makes the sync correct lives behind `Start`.
