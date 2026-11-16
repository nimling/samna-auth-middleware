# Testing and releasing

Three artefacts ship in one tag: the Go module `github.com/nimling/samna-auth-middleware/v2`, the `@nimling` package in `ts/` and the crate `samna-auth-middleware` in `rust/`, pinned by consumers as a git dependency on the tag. The release procedure and the bump commands are the `deploy` skill, the test recipes the `test` skill, and what runs locally the `run` skill, under `.claude/skills/`. How testing runs across repos: `../.claude/rules/platform.md` Testing.

## Every change

1. A Go change with a browser facing contract is mirrored into `ts/src/` in the same change: types into `types.ts` by json tag, sentinel errors as named exports, endpoints, params, headers and statuses into `client.ts`, `fetch.ts` or `session.ts`.
2. A Go change in a package the crate mirrors (`.claude/rules/structure.md` 9) is mirrored into `rust/src/` in the same change: the serde struct by json tag, the sentinel as an `Error` variant, the behaviour in the matching module.
3. Go tests sit beside the package as `<package>/<name>_test.go`, with the `dbtest` tag when they need a database. TypeScript tests sit beside the module as `ts/src/<name>.test.ts`. Rust tests sit under `rust/tests/<module>_test.rs`, `#[ignore]` when they need a database.
4. `ts/src/index.ts` is the package surface: a rewrite of that file keeps every export, the stream reader included (`readStream`, `EVENT_STREAM`, `Step`, `StepStatus`, `StreamResult` from `./stream`). A release once shipped `dist/stream.js` without the export and the connect portal could not import it.
5. A schema change is proven by the consumers' Go and hurl tests, locally and then on tst after the release is pinned and deployed.

## Releasing

1. Consumers move `go get ...@vX.Y.Z`, the `tag` of the `samna-auth-middleware` git dependency in their `Cargo.toml`, and the `ref` of their `Claimius Schema` include in `database/migrate.yml` together.
2. A new major moves the module path suffix too, and every import in every consumer follows.
3. Never hand edit any version field, `APP_VERSION`, `ts/package.json`, `rust/Cargo.toml` or `rust/Cargo.lock`; never `git tag`, never `pnpm publish`, never `cargo publish`, never commit `ts/dist/` or `rust/target/`.
