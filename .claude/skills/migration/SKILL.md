---
name: migration
description: Author a claimius schema migration in samna_auth_middleware, name it after the release tag it ships in, split it per role, and land it in the same bump. Use when adding a column, an index, a constraint, or a function change to the prophet or disciple claimius schema.
---

# migration

Author a schema change against the claimius baseline. The constraints outside a single run live at `.claude/rules/migrations.md`; this is the procedure.

## 1. Decide which roles carry the change

`prophet/database/` is the prophet baseline and `disciple/database/` is the disciple baseline. Read both before writing. A column the sync stream carries exists on both sides and the migration is written twice, once per role. A prophet only concern such as a session or a rate limit table is written once, under `prophet/database/migrations/`.

Never write one file and reach it from both roles. The split is what lets a consumer migrate a prophet without touching a disciple.

## 2. Resolve the version from the tag it will ship in

The version is the release tag, `v` dropped, prerelease folded in as a final integer component with its leading zeros removed.

```
v1.2.3            ->  V1.2.3
v1.1.0-alpha0070  ->  V1.1.0.70
```

Read the tag that is about to be cut, not the one already on the branch:

```
git describe --tags --abbrev=0
```

That prints the current tag. `just deploy` does a patch bump, so the migration takes the next one. For the alpha line the counter is the last component of `APP_VERSION` in `.env`, incremented by one.

## 3. Write the file

Path is `<role>/database/migrations/V<version>__claimius_<name>.sql`. The slug is always `claimius`. The name is lowercase alphanumerics and underscores, naming the thing that changed, never the fact that it is a migration.

```
prophet/database/migrations/V1.1.0.70__claimius_code_reference.sql
disciple/database/migrations/V1.1.0.70__claimius_code_reference.sql
```

The body converges on a database that already carries the change:

```sql
ALTER TABLE claimius.<table>
    ADD COLUMN IF NOT EXISTS "<column>" <type> <default>;

CREATE INDEX IF NOT EXISTS idx_<table>_<column>
    ON claimius.<table> ("<column>")
    WHERE sa_deleted_at IS NULL;
```

No `session_replication_role`. `COMMENT ON FUNCTION` carries the argument signature so it survives an overload.

## 4. Mirror into the baseline

A fresh build reads the baseline, a running database reads the migration, and both must land on the same shape. Apply the same change to the matching file in `prophet/database/` and `disciple/database/`. Skipping this is what makes a rebuilt environment differ from a migrated one.

## 5. Gate

```
just build
just test
```

Both green before the bump. A red build or test blocks the migration, per `deploy.md`.

## 6. Ship it in the tag it is named for

Follow the `deploy` skill. The migration, its baseline mirror, and the bump go out as one tag. A migration whose filename version does not match the tag it shipped in is the failure this whole naming rule exists to prevent, and the release workflow checks it.

## 7. Tell the consumers

The migration reaches a consumer only when that consumer adds a `type: migration` step pointing at the role folder, because smig does not descend into subfolders. Report to the user which consumers need the step and the ref bump:

- `samna_auth` pulls `prophet/database` in `database/migrate.yml`
- `nimployer` pulls `disciple/database` in `database/migrate.yml`

Both pin the module by `ref`, so the ref moves to the new tag in the same change that adds the step.
