# Disciple migrations

Alters against the disciple claimius baseline in the folder above. A consumer reaches them with a `type: migration` step pointing at `disciple/database/migrations`, separate from the `type: base` step that pulls `disciple/database`, because smig does not descend into subfolders.

Prophet side alters go in `prophet/database/migrations` instead. A change that both roles carry is written twice, once per role, so either role can be migrated on its own.

Naming, the gate, and the write up are in `.claude/rules/migrations.md` and `.claude/skills/migration/SKILL.md`.
