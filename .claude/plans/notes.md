# Working notes

1. Order: plan and arguments first, code after. No code change until plan section 11 and the capacity math are evaluated and agreed.

2. A correctness agent started on plan items 57 to 67 and was stopped midway at 22:09. Its partial edits to `{prophet,disciple}/database/V2.1` to `V2.4` and `shared/access.go` were reverted to the last verified state: prophet from the smig copy `smig-reconcile-812851228` (21:22), disciple from `smig-reconcile-2612126862` plus the superseded signature drops from `/tmp/drops-disciple`, `shared/access.go` from HEAD. Its approach, including `claimius._expand_access`, is in its transcript `/private/tmp/claude-501/-Users-slyotis-Projects-nimtech-samna-samna-auth-middleware/a37eadf5-9fda-4eed-95d8-0af8fbbaf73e/tasks/a7fbcb77d4daecf22.output`.

3. Its new pgTAP files encode the design and fail against today's code by intent: `test/sql/{prophet,disciple}/claim_mask.sql`, `claim_composition.sql`, `deny_scenario.sql`, `redaction.sql`, and `test/sql/disciple/text_identifier_access.sql` (rewritten at 22:02). They are the acceptance tests for items 57 to 67 once the plan is agreed; until then `just test` fails on them.

4. Verified state before that: build, vet, go test, both roles fresh and upgraded from v1.1.9 and v1.1.0-alpha0078, dbtest suites, hurl. nimployer, bookable_server_test and samna_auth each proven against it (uncommitted changes in each repo, their own `V2.0.0__claimius_unified.sql`).

5. Running: the capacity and concept math agent writing `.nimpress/claimius/scaling.md` and editing the design docs; its concept changes come back as plan items.

6. Release, when agreed: smig first (its commit `a69ca73` is already pushed on `samna/migrate` main, not released), then the middleware; `just deploy` is blocked by the permission check. Every live database: `smig upgrade`, `smig rebase --prune="Claimius Migrations"`, `smig up`.

7. Open with the user: the scale benchmark and context conditions question; `.nimpress/access.md` contradicts the usage design; the design's claim that Azure lacks the latest Postgres was wrong (18 is offered, custom extensions are not).
