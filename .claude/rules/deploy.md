# Deploy rules

This repo publishes two artefacts: the Go module consumed via `go get github.com/nimling/samna-auth-middleware` and the typescript package under `ts/` consumed via npm. The full procedure lives at `.claude/skills/deploy/SKILL.md`. The constraints below apply outside of any single deploy run.

## Never

1. Edit `APP_VERSION` in `.env` or `ts/package.json` `version` by hand. `../sbump/sbump.sh` owns both fields. Hand edits drift from the tag and break the cross artefact pair.

2. Tag manually with `git tag` or `git push --tags`. `just deploy` writes the tag and pushes it as one operation via `sbump`.

3. Use `--no-verify` or `--no-gpg-sign` on commits going out as part of a release.

4. Run `pnpm publish` directly. The tag pushed by `just deploy` triggers the workflow that publishes the typescript package.

5. Ship a Go change with a browser facing contract without mirroring it into `ts/src/`. The Go module and the typescript package go out in the same tag or not at all.

6. Skip `just test`, `just build`, or the typescript build before `just deploy`. A red Go test, a failing Go build, or a failing typescript build must block the bump.

7. Commit generated files (`ts/dist/`, `node_modules/`) as part of a release.

## Always

1. Update or add Go tests under `test/` for any change that has an observable contract on the Go side.

2. Update or add typescript tests under `ts/src/__tests__/` for any change that has an observable contract on the typescript side.

3. Match the json tag, not the Go field name, when mirroring Go types into typescript.

4. Write a single sentence commit message that describes the user visible change. The release notes derive from `git log` after the tag.

5. Use the existing `justfile` recipes: `just test`, `just build`, `just deploy`. Direct `go test`, `go build`, `pnpm build`, or `sbump.sh` invocation drifts from the contract.

## Version policy

`just deploy` does a patch bump by default. For minor or major, pass the explicit argument to `sbump.sh` instead. The repo currently does not gate this; the human running the deploy picks the right bump.
