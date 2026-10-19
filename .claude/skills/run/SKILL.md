---
name: run
description: Starts what samna_auth_middleware runs locally. A throwaway prophet or disciple claimius database through scripts/db.sh, the example disciple on it in debug mode, and the sauth-bff binary in front of a static build or a dev server. Use when asked to run, start or try the middleware, the bff or a role database locally, or to see a route answer. Not for running the tests, which is the test skill, and not for a consumer service's dev server, which is that repo's own run.
argument-hint: [db|example|bff]
allowed-tools: Bash(bash scripts/db.sh *), Bash(docker *), Bash(psql *), Bash(go *), Bash(curl *)
---

What this library runs on the local machine. Tests are the `test` skill; a consumer's dev server is that repo's `run`. The env keys a disciple reads are `.claude/rules/connect.md`.

## Role database

1. `bash scripts/db.sh disciple` or `bash scripts/db.sh prophet` starts a `postgres:17` container on a random local port, runs `smig up --yes` on `<role>/database/migrate.yml`, and prints `name=<container>` and one `export PGHOST=127.0.0.1 PGPORT=<port> ...` line. Needs docker and `smig` on PATH.
2. Export the printed line in the shell that runs the host; the password is `claimius`, the database `claimius`.
3. `smig up` failing prints the step and the SQL error on stderr; the container is left running, remove it.
4. Stop with `docker rm -f <container>`; the container runs with `--rm` and holds no volume.

## Example disciple

1. Build a disciple database as above, then `psql -v ON_ERROR_STOP=1 -q -f test/hurl/seed.sql` for the fixture tree `just test-hurl` reads.
2. `PORT=<port> SAUTH_DEBUG_USER_ID=a0000000-0000-4000-8000-00000000000a go run ./example/disciple`. `PORT` defaults to `8080`. Debug mode skips the sync and acts as the debug user on every request, so no prophet and no login are needed.
3. See it work: `curl -s http://127.0.0.1:<port>/api/root` answers the debug user's root access; `/api/organizations`, `/api/locations`, `/api/graph/owner` and `/api/graph/claim` follow.
4. `example/prophet` serves `/sam` and `/api/ping` on `:8080` against a prophet database; run it with `go run ./example/prophet` the same way.

## sauth-bff

1. `cmd/bff` is the backend for frontend built from `bff/`; its routes and the handoff are `.nimpress/bff.md`. No recipe runs it and no Go code loads `.env`, so export the env first: `set -a; . ./.env; set +a; go run ./cmd/bff`.
2. Its own keys: `SAUTH_BFF_PORT` (default `4300`), `SAUTH_BFF_BASE_PATH` (default `/api/auth`), `SAUTH_BFF_STATIC_DIR` for a built site, or `SAUTH_BFF_PROXY_TARGET` for a dev server to proxy instead.
3. The disciple keys `sdb.Init` reads: `SAUTH_ENDPOINT`, `SAUTH_APP_SLUG`, `SAUTH_APP_SECRET`, `SAUTH_REDIRECT_URI` as `<origin><base path>/callback`, the `PG*` of a disciple database, and `SAUTH_CLIENT_ID` with `SAUTH_CLIENT_SECRET` for the subscription forward. The app and its secrets come from the workspace `apps` skill, never a seed.
4. Sign in: open `http://localhost:4300/api/auth/login?next=/`, which runs the login at the prophet `SAUTH_ENDPOINT` names. With `SAUTH_DEBUG_USER_ID` set in `.env` there is no login and `/api/auth/me` answers the debug user.
5. See it work: `curl -s http://localhost:4300/api/status/health` answers the health check; `/api/auth/me` answers 401 without a session.
6. `session handoff: SAUTH_ENDPOINT is not a url` and a 502 on `/api/auth/session` mean `SAUTH_ENDPOINT` is empty or not a url.
7. samna-developer runs this binary from its own `just bff`, which downloads the released `sauth-bff-<os>-<arch>`; a local change is tried here with `go run`.
