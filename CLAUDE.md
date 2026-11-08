# Samna Auth Middleware

The library every Samna service embeds to authenticate and authorize through the prophet: JWT and session validation, client credentials, sync, notifications, and the `claimius` schema for both roles. Go module `github.com/nimling/samna-auth-middleware/v2` and the `@nimling` package in `ts/`, released together. Shared platform facts: `../.claude/rules/platform.md`.

1. Structure, packages and the two schema sets: `.claude/rules/structure.md`
2. How a consumer connects to the prophet: `.claude/rules/connect.md`
3. Tests and releases: `.claude/rules/deploy.md`
4. The schema and its functions: `.claude/rules/claimius.md`
5. Access lookups on `*DB`: `.claude/rules/access.md`
6. Design and walkthroughs: `README.md`, `docs/`, `.nimpress/claimius/`; open plans in `.claude/plans/`
