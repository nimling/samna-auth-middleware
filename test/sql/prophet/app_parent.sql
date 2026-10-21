BEGIN;
SELECT plan(9);

CREATE TEMP TABLE boot (key TEXT PRIMARY KEY, id UUID);

DO $$
DECLARE
    v_init    JSONB;
    v_sys_app UUID;
    v_sys_uc  UUID;
    v_tenant  UUID := '10000000-0000-4000-8000-000000000e01';
    v_other   UUID := '10000000-0000-4000-8000-000000000e02';
    v_root    UUID;
    v_child   UUID;
BEGIN
    v_init := claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_parent',
        p_system_app_name         => 'SQL parent test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_system_user_id          => '00000000-0000-4000-8000-000000000e00',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_sys_app := (v_init -> 'samna_app' ->> 'id')::UUID;
    v_sys_uc  := (v_init -> 'user_claim' ->> 'id')::UUID;
    INSERT INTO boot VALUES ('sys_uc', v_sys_uc);

    PERFORM claimius.upsert_root_organization(v_sys_app, 'Tenant', v_sys_uc, p_id => v_tenant);
    PERFORM claimius.upsert_root_organization(v_sys_app, 'Other', v_sys_uc, p_id => v_other);

    v_root := (claimius.upsert_app(
        p_actor_user_claim_id => v_sys_uc, p_slug => 'sqltest_parent_root', p_name => 'Root app',
        p_org_id => v_tenant, p_private_key => 'key', p_private_seed => 'seed'
    ) -> 'samna_app' ->> 'id')::UUID;
    v_child := (claimius.upsert_app(
        p_actor_user_claim_id => v_sys_uc, p_slug => 'sqltest_parent_child', p_name => 'Child app',
        p_org_id => v_tenant, p_private_key => 'key', p_private_seed => 'seed',
        p_redirect_uris => ARRAY['https://child.example', 'https://api.example/api/auth/callback'],
        p_parent_id => v_root
    ) -> 'samna_app' ->> 'id')::UUID;
    INSERT INTO boot VALUES ('root', v_root), ('child', v_child);
END $$;

CREATE FUNCTION pg_temp.boot(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM boot WHERE key = p_key
$$ LANGUAGE sql;

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.samna_app
        WHERE id = pg_temp.boot('root') AND sa_parent_id = id AND sa_root_id = id),
    'a root app references itself as parent and root');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.samna_app
        WHERE id = pg_temp.boot('child') AND sa_parent_id = pg_temp.boot('root') AND sa_root_id = pg_temp.boot('root')),
    'a child app takes its parent and the parent''s root');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.inheritance_info
        WHERE tree_type = 'parenthood' AND ancestor_type = 'claimius.samna_app'
          AND ancestor_id = pg_temp.boot('root') AND descendant_id = pg_temp.boot('child') AND depth = 1),
    'the parenthood closure carries the child under its root');

SELECT is_empty($$
    SELECT 'claim' FROM claimius.claim WHERE app_id = pg_temp.boot('child')
    UNION ALL
    SELECT 'claim_object' FROM claimius.claim_object WHERE app_id = pg_temp.boot('child')
    UNION ALL
    SELECT 'samna_user' FROM claimius.samna_user WHERE app_id = pg_temp.boot('child')
    UNION ALL
    SELECT 'prophet_state' FROM claimius.prophet_state WHERE app_id = pg_temp.boot('child')
    UNION ALL
    SELECT 'claim_id' FROM claimius.samna_app WHERE id = pg_temp.boot('child') AND claim_id IS NOT NULL
$$, 'a child app owns no claims, bindings, users or sync state');

SELECT ok(EXISTS (SELECT 1 FROM claimius.get_access('00000000-0000-4000-8000-000000000e00', pg_temp.boot('root'),
        pg_temp.boot('child'), 'claimius.samna_app', 4)),
    'the root app''s owner reads the child app through the parenthood tree');

SELECT is(claimius.upsert_app(
        p_actor_user_claim_id => pg_temp.boot('sys_uc'), p_slug => 'sqltest_parent_child', p_name => 'Child app renamed',
        p_org_id => '10000000-0000-4000-8000-000000000e01', p_private_key => 'key', p_private_seed => 'seed',
        p_parent_id => pg_temp.boot('root')) -> 'samna_app' ->> 'name',
    'Child app renamed',
    'upsert_app updates a child app in place on its slug');

SELECT throws_like($$
    SELECT claimius.upsert_app(
        p_actor_user_claim_id => pg_temp.boot('sys_uc'), p_slug => 'sqltest_parent_child', p_name => 'Child app',
        p_org_id => '10000000-0000-4000-8000-000000000e01', p_private_key => 'key', p_private_seed => 'seed')
$$, 'upsert_app: app sqltest_parent_child has parent%',
    'upsert_app never turns a child app into a root');

SELECT throws_like($$
    SELECT claimius.upsert_app(
        p_actor_user_claim_id => pg_temp.boot('sys_uc'), p_slug => 'sqltest_parent_stray', p_name => 'Stray app',
        p_org_id => '10000000-0000-4000-8000-000000000e02', p_private_key => 'key', p_private_seed => 'seed',
        p_parent_id => pg_temp.boot('root'))
$$, 'upsert_app: parent app sqltest_parent_root is owned by organization%',
    'a child app is owned by its parent''s organization');

SELECT throws_like($$
    SELECT claimius.upsert_app(
        p_actor_user_claim_id => pg_temp.boot('sys_uc'), p_slug => 'sqltest_parent_orphan', p_name => 'Orphan app',
        p_org_id => '10000000-0000-4000-8000-000000000e01', p_private_key => 'key', p_private_seed => 'seed',
        p_parent_id => '10000000-0000-4000-8000-0000000000ff')
$$, 'upsert_app: parent app % not found',
    'a child app needs a live parent');

SELECT * FROM finish();
ROLLBACK;
