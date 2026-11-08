BEGIN;
SELECT plan(5);

CREATE TEMP TABLE crew (key TEXT PRIMARY KEY, id UUID);

DO $$
DECLARE
    v_init   JSONB;
    v_app    UUID;
    v_sys_uc UUID;
    v_org    UUID := '10000000-0000-4000-8000-000000000f61';
    v_target UUID := 'a0000000-0000-4000-8000-000000000f61';
    v_source UUID := 'a0000000-0000-4000-8000-000000000f62';
    v_extra  UUID;
BEGIN
    v_init := claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_merge',
        p_system_app_name         => 'SQL merge test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_app    := (v_init -> 'samna_app' ->> 'id')::UUID;
    v_sys_uc := (v_init -> 'user_claim' ->> 'id')::UUID;

    PERFORM claimius.upsert_root_organization(v_app, 'Merge', v_sys_uc, p_id => v_org);
    PERFORM claimius.upsert_app_user(p_user_id => v_target, p_app_id => v_app, p_first_name => 'Target');
    PERFORM claimius.upsert_app_user(p_user_id => v_source, p_app_id => v_app, p_first_name => 'Source');

    v_extra := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Extra', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_extra, p_user_id => v_source,
                                       p_sa_owner_id => v_org, p_sa_created_by => v_sys_uc);

    INSERT INTO crew VALUES
        ('app', v_app),
        ('target', v_target),
        ('source', v_source),
        ('member', (SELECT claim_id FROM claimius.samna_app WHERE id = v_app)),
        ('extra', v_extra);
END $$;

CREATE FUNCTION pg_temp.crew(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM crew WHERE key = p_key
$$ LANGUAGE sql;

SELECT is(
    (SELECT count(*)::INT FROM claimius.user_claim
     WHERE app_id = pg_temp.crew('app') AND claim_id = pg_temp.crew('member') AND sa_deleted_at IS NULL
       AND user_id IN (pg_temp.crew('target'), pg_temp.crew('source'))),
    2,
    'both users hold the default claim before the merge'
);

SELECT lives_ok(
    format('SELECT claimius.merge_user(%L, %L)', pg_temp.crew('target'), pg_temp.crew('source')),
    'a merge where both users hold the same claim succeeds'
);

SELECT is(
    (SELECT count(*)::INT FROM claimius.user_claim
     WHERE app_id = pg_temp.crew('app') AND claim_id = pg_temp.crew('member') AND user_id = pg_temp.crew('target') AND sa_deleted_at IS NULL),
    1,
    'the target holds the shared claim once'
);

SELECT is(
    (SELECT count(*)::INT FROM claimius.user_claim
     WHERE app_id = pg_temp.crew('app') AND claim_id = pg_temp.crew('extra') AND user_id = pg_temp.crew('target') AND sa_deleted_at IS NULL),
    1,
    'a claim only the source held moves to the target'
);

SELECT is(
    (SELECT count(*)::INT FROM claimius.samna_user WHERE user_id = pg_temp.crew('source') AND sa_deleted_at IS NULL),
    0,
    'the source user is gone'
);

SELECT * FROM finish();
ROLLBACK;
