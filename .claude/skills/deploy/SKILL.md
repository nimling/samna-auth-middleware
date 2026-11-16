---
name: deploy
description: Releases samna_auth_middleware. Tests, mirrors the Go change into the @nimling typescript package and the rust crate, builds all three, commits, pushes and tags through sbump so the Go module, the typescript package and the crate go out in one tag. Use when the user says deploy, release or ship in this repo, or asks for a patch, minor or major release of the middleware. Not for running the tests alone, which is the test skill, not for moving a consumer onto the new tag, which is that consumer's work, and not for the sbump flags themselves, which is the sbump skill.
argument-hint: [patch|minor|major]
allowed-tools: Bash(just *), Bash(git *), Bash(cd ts *), Bash(pnpm *), Bash(cd rust *), Bash(cargo *), Bash(sbump *)
---

The release of this repo, in order, stopping on the first failure with the error verbatim. The test recipes are the `test` skill; the standing release rules are `.claude/rules/deploy.md`; sbump itself is the `sbump` skill.

## 1. Tests for the change

1. Read the change with `git diff` and `git status` and name its observable surface.
2. New behaviour in `shared/`, `prophet/`, `disciple/`, `smw/`, `client/`, `utils/` or `uptime/` with an observable contract gets a test beside the package, `<package>/<name>_test.go`, with the `dbtest` build tag when it needs a database. A SQL contract gets a pgTAP file under `test/sql/<role>/`, one per role that carries the change.
3. Anything that changes a return value, a sentinel error, an http status, a SQL function call, a parameter shape, an enum or a struct field is covered. Trivial moves and rename only diffs need none.
4. The test hits the public function and asserts on the returned value or the error sentinel, never on the internal call graph.

## 2. Run the tests

1. `just test`, as the `test` skill describes. A change touching a route adds `just test-hurl`; one touching the sync or the access functions adds `just verify`.
2. Failure: report the failing test names and stop. No commit, no bump.

## 3. Mirror into `ts/`

1. Go types with a counterpart in `ts/src/types.ts`, from `shared/models.go` or elsewhere, change there too, matched by json tag, not Go field name.
2. A new or changed sentinel error gets a matching named export in `ts/src/`.
3. A new endpoint, query parameter, header or status the client speaks goes into `ts/src/client.ts`, `ts/src/fetch.ts` or `ts/src/session.ts`.
4. A contract with a browser side assertion updates `ts/src/<name>.test.ts`.
5. `ts/src/index.ts` keeps every export, the stream reader from `./stream` included: `.claude/rules/deploy.md` Every change 3.
6. A Go diff with no browser facing surface makes this step a no op; say so in the commit message.

## 4. Mirror into `rust/`

1. A Go change in a package the crate mirrors, `.claude/rules/structure.md` 9, lands in the matching module under `rust/src/`: the serde struct by json tag, a sentinel as an `Error` variant in `shared/error.rs`, the behaviour in place.
2. A contract with a Rust side assertion updates `rust/tests/<module>_test.rs`; a route change is also run through `test/hurl/` against `rust/examples/disciple.rs`.
3. A Go diff in a prophet only package makes this step a no op; say so in the commit message.

## 5. Build

1. `just build` runs `go build ./...`. Failure: report the build output and stop.
2. `cd ts && pnpm install --frozen-lockfile && pnpm run build` type checks and emits `ts/dist/`, which is never committed. Failure: report the errors and stop.
3. `just build-rust` runs `cargo build --release` in `rust/`.
4. `just test-rust` runs `cargo test` in `rust/`.
5. `just fmt-rust` runs `cargo fmt --check`. Failure: run `cargo fmt` in `rust/`, confirm `just fmt-rust` passes, and include the formatting in the release commit; `just deploy` runs it again first and aborts before any version write.

## 6. Commit and push

1. Add `.nimpress/changelog/v<version>.md` for the version step 7 writes.
2. Stage the files of the release by name with `git add <files>`, never another session's work, never `ts/dist/`, `node_modules/` or `rust/target/`. When the tree holds changes that are not the release, release from a clean clone, because `sbump` refuses a dirty tree.
3. `git commit -m "<one line>"` describing the user visible change, `#<number>: <description>` when an issue covers it: `~/.claude/rules/git.md`.
4. `git push` on the current branch. Nothing to commit after `.gitignore` drops the build outputs: skip the commit and still push.

## 7. Bump and tag

1. Patch: `just deploy`, which runs `just fmt-rust`, then `sbump patch --env APP_VERSION --json ts/package.json@.version --toml rust/Cargo.toml@.package.version --toml 'rust/Cargo.lock@.package[name=samna-auth-middleware].version' --push-version --auto --workflow`: all four version fields, the tag and the push in one run.
2. Minor or major: `sbump minor` or `sbump major` with the same targets; `just deploy` always bumps a patch.
3. The tag runs `release.yml`, `typescript-publish.yml` and `docs.yml`: the GitHub release with the `sauth-bff-<os>-<arch>` binaries and the `rust` job that formats, builds and tests the crate, the `@nimling` package publish, and `.nimpress` to the docs site. Success is all three green on the tag.
4. Consumers then move `go get github.com/nimling/samna-auth-middleware/v2@vX.Y.Z`, the `tag` of the `samna-auth-middleware` git dependency in `Cargo.toml`, and the `ref` of their `Claimius Schema` include together: `.claude/rules/deploy.md` Releasing 1. A breaking release moves the module path suffix too, Releasing 2.

## Never

1. Skip steps 1, 2 or 5. A missing test, a failing test, a failing Go build, a failing typescript build or a failing rust build, test or format check blocks the release.
2. Hand edit `ts/package.json` `version`, `rust/Cargo.toml` `[package].version`, `rust/Cargo.lock` or `APP_VERSION` in `.env`; `sbump` owns all four.
3. Use `--no-verify` or `--no-gpg-sign`.
4. Run `pnpm publish`, `cargo publish` or `git tag`; the tag `just deploy` pushes runs the publish, and the crate is pinned by that tag.
5. Ship a Go change with a browser facing contract without its `ts/src/` mirror, or one in a mirrored package without its `rust/src/` mirror, in the same tag.
6. Commit `ts/dist/` or `rust/target/`.
