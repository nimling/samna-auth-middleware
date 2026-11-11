---
paths:
  - "prophet/**"
  - "disciple/**"
  - "shared/**"
  - "smw/**"
  - "sauth/**"
  - "test/sql/**"
  - "bench/**"
  - "**/*.sql"
---

# Claimius

This repo ships the `claimius` schema: `prophet/database` for samna_auth, `disciple/database` for every disciple. Function names here are the truth. Long form design: `.nimpress/claimius/`.

1. The canonical `claimius` skill, `.claude/skills/claimius/`, is the one every repo uses through the workspace symlink `../.claude/skills/claimius`. Its `.claude/skills/claimius/references/functions.md` is the function surface of both sets and `.claude/skills/claimius/references/access.md` the Go access lookups on `*DB` in `shared/access.go`.
2. A change to a function name, an argument, a table a function writes, a `*DB` access call or a trap updates the skill and its references in the same change, and `~/.claude/rules/claimius.md` where it names the same thing.
3. Where `~/.claude/rules/claimius.md` and the skill disagree, the SQL wins, then `.claude/skills/claimius/references/functions.md`.
4. Three holders and nothing named guest in the mechanism: a user, a temporary user of type `guest` (`insert_organization_guest(p_caller_uc, p_owner_id, p_user, p_ends_at, p_reason)` on the prophet set, reached through `POST /sam/user/guest`, with its `guest_identity` row, invites in `claimius.code`), and the anonymous user, `claimius.anonymous_user_id()` = `shared.AnonymousUserID`, type `anonymous`, named Public, one `samna_user` per app written by `upsert_system_app`, `upsert_app` and the `V4.0` backfill on the prophet set and by the disciple set's `upsert_disciple_state` and `V4.0` on a disciple, never in Go and never on the feed. A public grant is a `user_claim` of that user; its code is `user_claim.options.samk`, checked by `_check_claim_options` and refused on any other user's grant; the walk counts it only while `claimius.samk` holds the presented hash, which the guard carries on the request context and `shared/main.go` sets on the connection before each statement. No nullable `user_claim.user_id`, no option on `claim`, no guard option or route group.
