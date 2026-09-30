---
name: deploy
description: Build, test, mirror, commit, push, and tag a new version of samna_auth_middleware so both the Go module and the @nimling typescript package go out together.
---

# deploy

Ship a new version of `samna_auth_middleware`. The repo publishes two artefacts that must stay in sync: the Go module consumed via `go get github.com/nimling/samna-auth-middleware/v2` and the typescript package under `ts/` consumed via npm. A deploy that ships the Go side without the typescript mirror is a deploy that breaks consumers.

When the user says "deploy" in this repo, execute these steps in order. Stop on the first failure and surface the error.

## 1. Write or update Go tests for meaningful parts of the change

Every piece of new behaviour in `shared/`, `prophet/`, `disciple/`, `smw/`, `client/`, `utils/` or `uptime/` that has an observable contract needs a test beside the package it covers, as `<package>/<name>_test.go`, with the `dbtest` build tag when it needs a database. A SQL contract gets a pgTAP file under `test/sql/<role>/`, one per role that carries the change. Look at the diff with `git diff` and `git status`, identify the meaningful surface, and either extend an existing test file or add one. Trivial moves and rename only diffs do not need a new test. Anything that changes a return value, a sentinel error, an http status, a SQL function call, a parameter shape, an enum, or a struct field MUST be covered.

The test must assert the observable contract, not the internal call graph. Hit the public function, assert on the returned value or error sentinel.

## 2. Run the Go tests

```
just test
```

Runs `go test ./...`, then for each role `smig test` on `<role>/database/migrate.yml` `--from` the last tag, then a fresh `smig test --keep` whose container the `dbtest` tagged suites run against: `./shared/... ./disciple/... ./smw/...` on the disciple set, and `./prophet/...` on the prophet set with a second kept container exported as `PEER_PG*` for the federation tests. The containers are removed after each role. `just test <package>` runs `go test ./<package>/...` alone. `just test-hurl` runs the hurl files under `test/hurl/` against the example disciple; run it when the change touches a route. Failure: report the failing test names and stop. Do not commit, do not bump.

## 3. Mirror the Go change to typescript under `ts/`

The typescript package under `ts/src/` mirrors the Go surface that is callable from a browser or node consumer. Walk through the Go diff and apply the equivalent change to `ts/src/`:

1. Type changes in `shared/models.go` or any other Go type that has a counterpart in `ts/src/types.ts` go into `ts/src/types.ts`. Match the json tag, not the Go field name.
2. New or changed sentinel errors get a matching named export in `ts/src/`.
3. New http endpoints, query parameters, header names, or status codes that the typescript client speaks get mirrored into `ts/src/client.ts`, `ts/src/fetch.ts`, or `ts/src/session.ts` as appropriate.
4. Update the typescript tests beside the module, `ts/src/<name>.test.ts`, if the contract change has a browser side assertion.

If the Go diff has zero browser facing surface, this step is a no op and the deploy can move on. Document that no op in the commit message so a reviewer does not wonder.

## 4. Build the Go module

```
just build
```

`go build ./...`. Failure: report the build output and stop.

## 5. Type check and build the typescript package

```
cd ts && pnpm install --frozen-lockfile && pnpm run build
```

Emits `ts/dist/`. Failure: report the offending errors and stop.

## 6. Commit and push

Stage every changed file relevant to the release. Write a single sentence describing the user visible change. Push to `origin/main`.

```
git add -A
git commit -m "<one sentence describing the change>"
git push
```

If the working tree has nothing to commit after the build outputs are excluded by `.gitignore`, skip the commit but still push so the bump in the next step pushes a clean ref.

## 7. Bump and tag

```
just deploy
```

Calls `sbump patch --env APP_VERSION --json ts/package.json@.version --push-version --auto --workflow`. Patch bump, tag write, tag push. The GitHub Actions workflows attached to tag pushes create the release, build and publish the `@nimling` typescript package, and publish `.nimpress` to the docs site; consumers of the Go module pick the new tag up via `go get github.com/nimling/samna-auth-middleware/v2@vX.Y.Z` and move the `ref` of their claimius git include to the same tag. The module path carries the major version, `github.com/nimling/samna-auth-middleware/v2` from 2.0.0 on, because Go accepts a tag at v2 or above only for a path ending in its major; a breaking release moves both the tag and the path, and every import in every consumer follows.

For minor or major releases, run `sbump major|minor --env APP_VERSION --json ts/package.json@.version --push-version --auto --workflow` instead of `just deploy`, which always bumps a patch. A release carries its `.nimpress/changelog/v<version>.md` entry in the commit before the bump.

## Never

1. Skip steps 1, 2, 4, or 5. A missing test, a failing test, a failing Go build, or a failing typescript build must block the deploy.

2. Edit `ts/package.json` `version` by hand. `sbump` owns that field. The same applies to `APP_VERSION` in `.env`.

3. Use `--no-verify` or `--no-gpg-sign`.

4. Run `pnpm publish` directly. The tag pushed by `just deploy` triggers the workflow that publishes.

5. Ship a Go change with a browser facing contract without mirroring it into `ts/src/`. The two sides go out in the same tag or not at all.
