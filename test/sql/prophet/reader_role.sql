BEGIN;
SELECT plan(21);

DO $$
DECLARE
    v_app   UUID;
    v_a     UUID := 'a0000000-0000-4000-8000-0000000000f1';
    v_root  UUID := '10000000-0000-4000-8000-0000000000f1';
    v_write UUID;
    v_page  UUID;
BEGIN
    v_app := (claimius.init(
        p_system_app_slug         => 'sqltest_reader',
        p_system_app_name         => 'SQL reader test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    PERFORM claimius.ensure_app_user(p_user_id => v_a, p_app_id => v_app, p_first_name => 'Reader', p_external_id => 'ext-reader');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Reader root', 'company', v_root, v_root, 0, v_a);
    PERFORM claimius.link_app_object(v_app, 'claimius.organization', v_root);

    v_write := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Reader writer', p_description => NULL, p_sa_access => 6,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_write, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_sa_access => 6, p_inherits => TRUE
    );
    PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_write, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_a);

    v_page := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Reader page', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a, p_inherits => FALSE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_page, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_sa_access => 4, p_inherits => FALSE, p_scope => '{"page": {"reports": true}}'::JSONB
    );
    PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_page, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_a);

    PERFORM claimius.write_audit(v_app, 'update', 'test', 'claimius.organization', v_root, v_root, NULL, 'reader audit');
END $$;

CREATE TEMP TABLE app_ids AS SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_reader';
GRANT SELECT ON app_ids TO claimius_reader;

SET LOCAL ROLE claimius_reader;

CREATE TEMP TABLE reader_answers (probe TEXT PRIMARY KEY, answer BIGINT);

INSERT INTO reader_answers
SELECT 'get_access', count(*) FROM claimius.get_access('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids), '10000000-0000-4000-8000-0000000000f1', 'claimius.organization', 4)
UNION ALL
SELECT 'get_direct_access', count(*) FROM claimius.get_direct_access('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids), '10000000-0000-4000-8000-0000000000f1', 'claimius.organization', 2)
UNION ALL
SELECT 'resolve_access', count(*) FROM claimius.resolve_access('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids), '10000000-0000-4000-8000-0000000000f1', 'claimius.organization', 2)
UNION ALL
SELECT 'mutation_access', count(*) FROM claimius.mutation_access('a0000000-0000-4000-8000-0000000000f1', '10000000-0000-4000-8000-0000000000f1', 'claimius.organization', 2)
UNION ALL
SELECT 'get_objects', count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids), 'claimius.organization', 4)
UNION ALL
SELECT 'get_organizations', count(*) FROM claimius.get_organizations('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids), 4)
UNION ALL
SELECT 'get_locations', count(*) + 1 FROM claimius.get_locations('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids), 4)
UNION ALL
SELECT 'get_users', count(*) FROM claimius.get_users('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids), 4)
UNION ALL
SELECT 'get_user_access', count(*) FROM claimius.get_user_access('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids), 'ext-reader', 2)
UNION ALL
SELECT 'get_claims', count(*) FROM claimius.get_claims('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids), 4)
UNION ALL
SELECT 'get_scope_access', count(*) FROM claimius.get_scope_access('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids), '$.page.reports', 4)
UNION ALL
SELECT 'get_audit', count(*) FROM claimius.get_audit('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids), 4)
UNION ALL
SELECT 'get_secrets', count(*) + 1 FROM claimius.get_secrets('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids), 4)
UNION ALL
SELECT 'search', count(*) FROM claimius.search('a0000000-0000-4000-8000-0000000000f1'::UUID, (SELECT id FROM app_ids), 'Reader'::TEXT, NULL::TEXT[], 4, 20, 0)
UNION ALL
SELECT 'get_owner_graph', count(*) FROM claimius.get_owner_graph('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids))
UNION ALL
SELECT 'get_claim_graph', count(*) FROM claimius.get_claim_graph('a0000000-0000-4000-8000-0000000000f1', (SELECT id FROM app_ids))
UNION ALL
SELECT 'get_owner_ancestors', count(*) FROM claimius.get_owner_ancestors('claimius.organization', '10000000-0000-4000-8000-0000000000f1')
UNION ALL
SELECT 'app_has_object', count(*) FILTER (WHERE claimius.app_has_object((SELECT id FROM app_ids), 'claimius.organization', '10000000-0000-4000-8000-0000000000f1')) FROM (SELECT 1) one
UNION ALL
SELECT 'get_app', count(*) FROM claimius.get_app();

DO $$
BEGIN
    PERFORM 1 FROM claimius.user_object LIMIT 1;
    INSERT INTO reader_answers VALUES ('select user_object', 1);
EXCEPTION WHEN insufficient_privilege THEN
    INSERT INTO reader_answers VALUES ('select user_object', 0);
END $$;

DO $$
BEGIN
    DELETE FROM claimius.claim_graph_cache;
    INSERT INTO reader_answers VALUES ('delete claim_graph_cache', 1);
EXCEPTION WHEN insufficient_privilege THEN
    INSERT INTO reader_answers VALUES ('delete claim_graph_cache', 0);
END $$;

RESET ROLE;

SELECT ok(answer > 0, probe || ' answers under claimius_reader')
FROM reader_answers
WHERE probe NOT IN ('select user_object', 'delete claim_graph_cache')
ORDER BY probe;

SELECT is((SELECT answer FROM reader_answers WHERE probe = 'select user_object'), 0::BIGINT,
          'claimius_reader cannot select user_object directly');

SELECT is((SELECT answer FROM reader_answers WHERE probe = 'delete claim_graph_cache'), 0::BIGINT,
          'claimius_reader holds no DML on claim_graph_cache');

SELECT * FROM finish();
ROLLBACK;
