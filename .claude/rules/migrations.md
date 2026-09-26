# Migration rules

Every claimius schema change after the baseline is a migration file, and its version is the release tag it ships in. The procedure lives at `.claude/skills/migration/SKILL.md`.

## Where a migration lives

1. `prophet/database/migrations/` carries prophet side alters. `disciple/database/migrations/` carries disciple side alters. Both sit inside the trees `//go:embed database` already covers, so they travel with the module and with a git include.

2. The split is the point. A change both roles carry is written twice, once per role, so a consumer can migrate a prophet without touching a disciple and the other way round. Never one shared file reached from both.

3. smig does not descend into subfolders: `ReadDir` skips directories. So a consumer reaches a migrations folder only through its own `type: migration` step, never through the `type: base` step that pulls the baseline. A consumer that adds no such step never applies these files.

4. The baseline files in `prophet/database/` and `disciple/database/` stay the current shape of the schema. Never add a table definition under `migrations/`.

## Naming

5. The filename is `V<version>__claimius_<name>.sql`. The slug is always `claimius` because that is the slug both consumers declare for this schema.

6. The version is the release tag the migration ships in, with the leading `v` dropped and the prerelease suffix folded in as a final component:

   - `v1.2.3` becomes `V1.2.3`
   - `v1.1.0-alpha0070` becomes `V1.1.0.70`

   The counter loses its leading zeros so the components stay integers and sort by value, which is what smig's version parser compares.

7. A migration is therefore written in the same change as the bump that ships it. Authoring one without knowing its tag is what produces a file that has to be renamed, and a rename makes it a new file to every server that already applied it.

## Never

8. Never edit a migration that has shipped in a tag. The `type: migration` step is append only and hash locked; a mistake is corrected by a further migration.

9. Never rename a migration. Identity in the ledger is the path relative to the database directory and there is no rename detection, so a rename re-applies the file everywhere.

10. Never write an alter that only works once. Every file converges on a database that already carries the change: `ADD COLUMN IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`, `DROP ... IF EXISTS`, `CREATE OR REPLACE FUNCTION`.

11. Never use `session_replication_role`. Triggers are the contract.

## Always

12. Mirror the change into the baseline in the same tag when the baseline is what a fresh build reads, so a rebuilt database and a migrated one land on the same shape.

13. Run `just build` and `just test` before the bump, per `deploy.md`.
