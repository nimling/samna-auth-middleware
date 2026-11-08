# Samna Auth Middleware

The library every Samna service embeds to authenticate and authorize through the prophet: JWT and session validation, client credentials, the sync, notifications, and the `claimius` schema for both roles. Go module `github.com/nimling/samna-auth-middleware/v2` and the `@nimling` package in `ts/`, released together. GitHub `nimling/samna-auth-middleware`, on the Samna Ecosystem board (`../.claude/rules/github_project.md`). Shared platform facts: `../.claude/rules/platform.md`.

The SQL under `prophet/database` and `disciple/database` is the truth of the schema; `.nimpress/claimius/` is the design, its math and its measured costs, read on disk; `README.md` and `docs/` describe the repo for a developer.

## Commands

1. `just build`: `go build ./...`. `cd ts && pnpm install --frozen-lockfile && pnpm run build` builds the package.
2. `just test`: `go test ./...`, then the `dbtest` suites on role databases `scripts/db.sh` builds in local `postgres:17` containers. `just test <TestName>` runs one. `just verify` runs the prophet to disciple scenario, `just test-hurl` the hurl files against `example/disciple`, `just bench` the claimius bench.
3. `bash scripts/db.sh prophet|disciple`: a throwaway role database, printing the `PG*` line to export.
4. `just deploy`: `sbump patch` over `APP_VERSION` and `ts/package.json`, tag and push in one run; the tag runs the release, publish and docs workflows.

## Conventions

1. Claimius logic lives in SQL. Go here is the Echo middleware, the sync, the access lookups calling SQL functions and one line per job; a loop, a fold or a class that grew in Go is a finding.
2. The database folders are the baseline, not a migration line: `V1.0` roles, `V1.1` tables, `V2.1` to `V2.4` functions, `V3.0` triggers, `V4.0` init, `V6.0` grants. Nothing else is read or cited.
3. Retention is `claimius.prune(p_settings JSONB)` on both roles, run by `shared/retention.go` `PruneLoop`. No table, no kinds, no endpoint, no class, no cold storage; application data is never aged.
4. The profile is one `profile JSONB` on `claimius.samna_user`: the user writes top level keys on Samna Auth, an app writes under its own slug through `PUT /sam/profile`, the sync carries it.
5. The claim level, stated once: only `claim.level` compares, the higher level wins, equal levels fall to the classes; the level of an organization or location is tree depth.
6. A Go change with a browser facing contract is mirrored into `ts/src/` in the same change. No code comments; `COMMENT ON` stays.
7. This repo ships nothing for one consumer. A consumer pins a version and writes its own migration in its own repo.

## Rules

1. `.claude/rules/structure.md`, always: packages, the two roles and the baseline files.
2. `.claude/rules/connect.md`, always: how a consumer connects to the prophet, its env keys, the cli login routes, runners and retention.
3. `.claude/rules/deploy.md`, always: what every change mirrors and tests, and the release constraints.
4. `.claude/rules/claimius.md`, path scoped to the roles, `shared/`, `smw/`, `sauth/`, `test/sql/`, `bench/` and SQL: keeping the canonical skill in step with the schema.

## Skills

1. `claimius`: the canonical claimius skill every repo uses through `../.claude/skills/claimius`; writing or reviewing code that reads, writes or checks access through claimius. Its `references/` hold the function surface, the `*DB` access lookups and the SQL patterns.
2. `deploy`: deploy, release or ship this repo.
3. `test`: `just test`, one test, `just test-hurl`, `just verify`, the ts tests and `just bench`.
4. `run`: a role database, the example disciple or the bff locally.
