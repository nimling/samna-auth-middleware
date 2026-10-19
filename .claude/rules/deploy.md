# Testing and releasing

Two artefacts ship in one tag: the Go module `github.com/nimling/samna-auth-middleware/v2` and the `@nimling` package in `ts/`. The release procedure and the bump commands are the `deploy` skill, the test recipes the `test` skill, and what runs locally the `run` skill, under `.claude/skills/`. How testing runs across repos: `../.claude/rules/platform.md` Testing.

## Every change

1. A Go change with a browser facing contract is mirrored into `ts/src/` in the same change: types into `types.ts` by json tag, sentinel errors as named exports, endpoints, params, headers and statuses into `client.ts`, `fetch.ts` or `session.ts`.
2. Go tests sit beside the package as `<package>/<name>_test.go`, with the `dbtest` tag when they need a database. TypeScript tests sit beside the module as `ts/src/<name>.test.ts`.
3. `ts/src/index.ts` is the package surface: a rewrite of that file keeps every export, the stream reader included (`readStream`, `EVENT_STREAM`, `Step`, `StepStatus`, `StreamResult` from `./stream`). `alpha0071` shipped `dist/stream.js` without the export and the connect portal could not import it.
4. A schema change is proven by the consumers' Go and hurl tests, locally and then on tst after the release is pinned and deployed.

## Releasing

1. Consumers move `go get ...@vX.Y.Z` and the `ref` of their `Claimius Schema` include in `database/migrate.yml` together.
2. A new major moves the module path suffix too, and every import in every consumer follows.
3. Never hand edit either version field, never `git tag`, never `pnpm publish`, never commit `ts/dist/`.
