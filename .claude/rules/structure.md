# Structure

1. `prophet/` and `disciple/` are the two roles, each with `sdb` (database layer), `echo` (handlers), `sam` (route registration and sync) and `database/` (the claimius SQL set with its `migrate.yml`). A binary imports one role.
2. `prophet/database` is pulled by samna_auth, `disciple/database` by every disciple, as the `Claimius Schema` git include of the consumer's `database/migrate.yml`. The two trees differ; never copy a file between them.
3. `smw/` the Echo middleware, `shared/` the access and model surface both roles share, `client/` the prophet HTTP client, `utils/` config, cookies and env, `bff/` with `cmd/bff` the backend for frontend proxy, `uptime/` the heartbeat reporter, `notification/`, `audit/`, `analytics/`, `dpop/`, `pkg/`.
4. `sauth/` is the Go surface the `sauth` cli builds on; the cli itself lives in samna_auth `internal/sauth/`.
5. `example/prophet` and `example/disciple` are runnable hosts; `test/hurl/` runs against `example/disciple`.
6. `test/sql/<role>/` pgTAP per role, `bench/<role>/` the claimius bench under `just bench`.
7. `ts/` the TypeScript package; `ts/dist/` is never committed.
8. `.nimpress/` the published docs and the per release changelog.
