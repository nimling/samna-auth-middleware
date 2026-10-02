BEGIN;
SELECT plan(5);

CREATE TEMP TABLE scope (key TEXT PRIMARY KEY, id UUID);

DO $$
DECLARE
    v_init    JSONB;
    v_sys_app UUID;
    v_sys_uc  UUID;
    v_tenant  UUID := '10000000-0000-4000-8000-000000000e51';
    v_reader  UUID := 'a0000000-0000-4000-8000-000000000e51';
    v_read    UUID;
    v_made    JSONB;
BEGIN
    v_init := claimius.init(
        p_system_app_slug         => 'sqltest_claim_scope',
        p_system_app_name         => 'SQL claim scope test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_system_user_id          => '00000000-0000-4000-8000-000000000e50',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_sys_app := (v_init -> 'samna_app' ->> 'id')::UUID;
    v_sys_uc  := (v_init -> 'user_claim' ->> 'id')::UUID;

    PERFORM claimius.create_root_organization(v_sys_app, 'Shared tenant', v_sys_uc, p_id => v_tenant);
    PERFORM claimius.ensure_app_user(p_user_id => v_reader, p_app_id => v_sys_app, p_first_name => 'Reader');
    v_read := (claimius.create_claim(
        p_app_id => v_sys_app, p_name => 'Shared tenant reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_tenant, p_sa_root_id => v_tenant, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_sys_app, p_claim_id => v_read, p_sa_owner_id => v_tenant, p_sa_root_id => v_tenant,
        p_sa_created_by => v_sys_uc, p_object_id => v_tenant, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.assign_claim_user(
        p_app_id => v_sys_app, p_claim_id => v_read, p_user_id => v_reader,
        p_sa_owner_id => v_tenant, p_sa_created_by => v_sys_uc);

    v_made := claimius.create_app(
        p_actor_user_claim_id => v_sys_uc,
        p_slug                => 'sqltest_claim_scope_tenant',
        p_name                => 'Shared tenant app',
        p_org_id              => v_tenant,
        p_private_key         => 'key',
        p_private_seed        => 'seed'
    );
    INSERT INTO scope VALUES
        ('sys_app', v_sys_app),
        ('read', v_read),
        ('tenant_app', (v_made -> 'samna_app' ->> 'id')::UUID),
        ('tenant_claim', (v_made -> 'claim' ->> 'id')::UUID);
END $$;

CREATE FUNCTION pg_temp.scope(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM scope WHERE key = p_key
$$ LANGUAGE sql;

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.claim
        WHERE id = pg_temp.scope('tenant_claim') AND app_id = pg_temp.scope('tenant_app')
          AND sa_owner_id = '10000000-0000-4000-8000-000000000e51'),
    'the second app owns a claim under the organization the reader reads in the first');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.get_claims('a0000000-0000-4000-8000-000000000e51', pg_temp.scope('sys_app'), 0)
        WHERE id = pg_temp.scope('read')),
    'the reader lists the claim it holds in its own app');

SELECT is_empty($$
    SELECT id FROM claimius.get_claims('a0000000-0000-4000-8000-000000000e51', pg_temp.scope('sys_app'), 0)
    WHERE app_id <> pg_temp.scope('sys_app')$$,
    'the listing of one app holds no claim of another app the shared organization owns');

SELECT is_empty($$
    SELECT 1 FROM claimius.get_access('a0000000-0000-4000-8000-000000000e51', pg_temp.scope('sys_app'),
        pg_temp.scope('tenant_claim'), 'claimius.claim', 4)$$,
    'the cascade from the shared organization grants nothing on a claim of another app');

SELECT is_empty($$
    SELECT 1 FROM claimius.user_object uo
    JOIN claimius.claim c ON c.id = uo.object_id
    WHERE uo.app_id = pg_temp.scope('sys_app') AND uo.object_type = 'claimius.claim' AND c.app_id <> uo.app_id$$,
    'no pair of the first app names a claim of another app');

SELECT * FROM finish();
ROLLBACK;
