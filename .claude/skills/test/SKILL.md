---
name: test
description: Runs the samna_auth_middleware tests. The Go suites with and without a database, the dbtest role databases, the hurl files against the example disciple, the prophet to disciple scenario check, the TypeScript tests in ts/, the rust crate's tests and format check, and the claimius bench. Use when asked to test, verify or prove a change in this repo, or to run one test. Not for starting a host or a database to work against, which is the run skill, and not for releasing, which is the deploy skill.
argument-hint: [<TestName>|hurl|verify|ts|rust|bench]
allowed-tools: Bash(just *), Bash(go *), Bash(cd ts *), Bash(pnpm *), Bash(cd rust *), Bash(cargo *), Bash(docker *)
---

The tests of this repo through its `justfile`. Starting a host is the `run` skill; releasing is the `deploy` skill. Testing across repos is `../.claude/rules/platform.md` Testing; where tests sit is `.claude/rules/deploy.md` Every change 2.

## Go

1. `just test` runs `go test ./...`, then the `dbtest` tagged suites against role databases `scripts/db.sh` builds with `smig up` in local `postgres:17` containers: `./shared/... ./disciple/... ./smw/...` on the disciple set, then `./prophet/...` on the prophet set with a peer prophet exported as `PEER_PG*` and a disciple exported as `DISCIPLE_PG*` for the federation tests. The containers are removed on exit. Needs docker and `smig` on PATH.
2. One test: `just test <TestName>` runs the same set with `-run <TestName>` on every `go test`, role databases included, so a `dbtest` test runs against its database; `just test '^<TestName>$'` matches the name exactly.
3. Passes means every package `ok` and the recipe exits 0. A failure prints the test name and the file line; report those and stop.
4. Pass the run on a change before any commit, and before `just deploy` as the `deploy` skill orders.

## Hurl

1. `just test-hurl` builds a disciple database, loads `test/hurl/seed.sql`, builds `./example/disciple`, starts it on a free port with `SAUTH_DEBUG_USER_ID=a0000000-0000-4000-8000-00000000000a`, and runs `test/hurl/*.hurl` with the seeded ids as variables. Run it when a change touches a route.
2. `example disciple did not start` prints the server log; read it for the `sdb.Init` or migration error.
3. Passes means every hurl file reports success.

## Scenario check

1. `just verify` builds a prophet and a disciple and runs `go test -tags dbtest -run Scenario -v ./prophet/echo/`: the two read the same objects at the same level. Run it when a change touches the sync or the access functions.

## TypeScript

1. `cd ts && pnpm install --frozen-lockfile && pnpm test` runs vitest over `ts/src/*.test.ts`.
2. Run it when a change touches `ts/src/` or mirrors a Go contract into it.

## Rust

1. `just test-rust` runs `cargo test` in `rust/`, over `rust/tests/*_test.rs` with a fake prophet on a loopback port where a test needs one; `rust/tests/db_test.rs` is `#[ignore]` and runs with `cargo test -- --ignored` against the `PG*` of a `scripts/db.sh disciple` database.
2. `just fmt-rust` runs `cargo fmt --check`. Misformatted: run `cargo fmt` in `rust/`, confirm `just fmt-rust` passes, and keep the change with the release, `.claude/rules/deploy.md` Releasing 3.
3. `just build-rust` runs `cargo build --release`.
4. A route change is also proven on the Rust twin: `cargo build --example disciple` in `rust/`, then the `test-hurl` steps with `rust/target/debug/examples/disciple` in place of the Go binary.
5. Run them when a change touches `rust/` or mirrors a Go contract into it; all of `just test-rust` and `just fmt-rust` pass before a release.

## SQL

1. The pgTAP files under `test/sql/<role>/` are the `tests:` of each role's `migrate.yml`. Only `smig test` runs them, and `smig test` is never run (`../.claude/rules/platform.md` Testing 2). A SQL contract is proven by the `dbtest` Go suites and the consumers' tests, `.claude/rules/deploy.md` Every change 4.
2. `just bench` runs `bench/<role>/bench.sql` per role with `claimius.bench` on and prints the measured steps. `bench/` is gitignored, so a role whose `bench.sql` is missing fails that role.

## Environments

1. Every recipe here is local. `set dotenv-load` exports `.env`, and the database recipes override `PG*` with the containers they build.
2. A schema or route change is then proven on tst by the consumers' Go and hurl tests once the release is pinned and deployed there, `.claude/rules/deploy.md` Every change 4.
