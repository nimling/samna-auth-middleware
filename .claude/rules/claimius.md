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
2. A change to a function name, an argument, a table a function writes, a `*DB` access call or a trap updates the skill and its references in the same change, and the workspace rule `../.claude/rules/claimius.md` where it names the same thing.
3. Where the workspace rule and the skill disagree, the SQL wins, then `.claude/skills/claimius/references/functions.md`.
