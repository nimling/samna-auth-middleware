# Structure

1. `prophet/` and `disciple/` are the two roles, each with `sdb` (database layer), `echo` (handlers), `sam` (route registration and sync) and `database/` (the claimius SQL set with its `migrate.yml`). A binary imports one role.
2. Each `database/` is a baseline of nine files and nothing else: `V1.0__claimius_roles.sql`, `V1.1__claimius_baseline.sql`, `V2.1` to `V2.4__claimius_functions_*.sql`, `V3.0__claimius_triggers.sql`, `V4.0__claimius_init.sql`, `V6.0__claimius_grants.sql`. There are no delta files and no migrations; a change is written into the file that owns it, and a consumer moves its `ref` and runs one scoped `smig rebase` of its `claimius` base step.
3. `prophet/database` is pulled by samna_auth, `disciple/database` by every disciple, as the `Claimius Schema` git include of the consumer's `database/migrate.yml`. The two trees differ; never copy a file between them.
4. `smw/` the Echo middleware, `shared/` the access and model surface both roles share, `client/` the prophet HTTP client, `utils/` config, cookies and env, `bff/` with `cmd/bff` the backend for frontend proxy, `uptime/` the heartbeat reporter, `notification/`, `audit/`, `analytics/`, `dpop/`, `pkg/`. `internal/store/` holds `store.Upsert`, the column keyed upsert `shared/apply.go` applies a feed row and a routed answer with. `plan/auth.md` is a design note of model, sync and route changes. `docs/` holds the Go host guides `prophet.md`, `disciple.md`, `sync.md`, `app_scoping.md`, `clients.md` and `marshalling.md`; nothing under `.claude/`, `.nimpress/`, the justfile or the workflows reads them. `sqlsync` at the top level is a compiled Mach-O binary, not a package.
5. `sauth/` is the Go surface the `sauth` cli builds on; the cli itself lives in samna_auth `internal/sauth/`.
6. `example/prophet` and `example/disciple` are runnable hosts; `test/hurl/` runs against `example/disciple`.
7. `test/sql/<role>/` pgTAP per role, `bench/<role>/` the claimius bench under `just bench`.
8. `ts/` the TypeScript package; `ts/dist/` is never committed.
9. `.nimpress/` the published docs and the per release changelog.
