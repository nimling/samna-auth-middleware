# Testing and releasing

Two artefacts ship in one tag: the Go module `github.com/nimling/samna-auth-middleware/v2` and the `@nimling` package in `ts/`.

## Every change

1. A Go change with a browser facing contract is mirrored into `ts/src/` in the same change: types into `types.ts` by json tag, sentinel errors as named exports, endpoints, params, headers and statuses into `client.ts`, `fetch.ts` or `session.ts`.
2. Go tests sit beside the package as `<package>/<name>_test.go`, with the `dbtest` tag when they need a database. SQL contracts get a pgTAP file per role under `test/sql/<role>/`. TypeScript tests sit beside the module as `ts/src/<name>.test.ts`.
3. `just test` runs `go test ./...`, then per role `smig test` fresh and `--from` the last tag, then the `dbtest` suites on a kept container; the prophet run adds `PEER_*` and `DISCIPLE_*` containers for federation. `just test <pkg>` runs one package without a database.
4. `just test-hurl` runs `test/hurl/` against `example/disciple`; run it when a route changes. `just verify` checks a prophet and a disciple read the same objects at the same level.

## Releasing

1. `just test`, `just build`, and `cd ts && pnpm install --frozen-lockfile && pnpm run build` pass first.
2. Add `.nimpress/changelog/v<version>.md` in the release commit, commit, push.
3. `just deploy` bumps a patch across `APP_VERSION` and `ts/package.json`, tags and pushes; the tag runs `release.yml`, `typescript-publish.yml` and `docs.yml`. Minor or major: `sbump minor|major --env APP_VERSION --json ts/package.json@.version --push-version --auto --workflow`.
4. Consumers move `go get ...@vX.Y.Z` and the `ref` of their `Claimius Schema` include in `database/migrate.yml` together.
5. A new major moves the module path suffix too, and every import in every consumer follows.
6. Never hand edit either version field, never `git tag`, never `pnpm publish`, never commit `ts/dist/`.
