# Samna Auth Middleware

The library every Samna service embeds to authenticate and authorize through the prophet: JWT and session validation, client credentials, sync, notifications, and the `claimius` schema for both roles. Go module `github.com/nimling/samna-auth-middleware/v2` and the `@nimling` package in `ts/`, released together. GitHub `nimling/samna-auth-middleware`, on the Samna Ecosystem board (`../.claude/rules/github_project.md`). Shared platform facts: `../.claude/rules/platform.md`.

Design and walkthroughs: `.nimpress/claimius/` is the source of truth for the access model, its math and its measured costs, read on disk; `README.md` and `docs/` describe the repo for a developer. The claim level rule, stated once: the higher level wins, equal levels fall to the classes; see `.nimpress/claimius/denials.md` Levels.

## Index

1. `.claude/rules/structure.md`, always: packages, the two roles and the two schema sets.
2. `.claude/rules/connect.md`, always: how a consumer connects to the prophet, its env keys, the cli login routes and the consumers on v2.
3. `.claude/rules/deploy.md`, always: what every change mirrors and tests, and the release constraints.
4. `.claude/rules/claimius.md`, path scoped to `prophet/`, `disciple/`, `shared/`, `smw/`, `sauth/`, `test/sql/`, `bench/` and SQL: the schema this repo ships and keeping the canonical skill in step with it.
5. `.claude/skills/claimius`: the canonical claimius skill every repo uses through `../.claude/skills/claimius`; use when writing or reviewing code that reads, writes or checks access through claimius. Its `references/` hold the function surface, the `*DB` access lookups and the SQL patterns.
6. `.claude/skills/deploy`: use when the user says deploy, release or ship in this repo.
7. `.claude/skills/test`: use when testing a change: `just test`, one test with `just test <TestName>`, `just test-hurl`, `just verify`, the ts tests and `just bench`.
8. `.claude/skills/run`: use when starting a role database, the example disciple or the bff locally.
9. `.claude/plans/` holds the plans the v2 rebuild was built from. They are history: the pages under `.nimpress/claimius/` and the SQL carry the design now, and nothing in a plan overrides them.
