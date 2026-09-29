BEGIN;
SELECT plan(12);

CREATE TEMP TABLE boot (key TEXT PRIMARY KEY, id UUID);

DO $$
DECLARE
    v_init    JSONB;
    v_sys_app UUID;
    v_sys_uc  UUID;
    v_tenant  UUID := '10000000-0000-4000-8000-000000000d01';
    v_other   UUID := '10000000-0000-4000-8000-000000000d02';
    v_reader  UUID := 'a0000000-0000-4000-8000-000000000d01';
    v_read    UUID;
    v_read_uc UUID;
    v_made    JSONB;
BEGIN
    v_init := claimius.init(
        p_system_app_slug         => 'sqltest_boot',
        p_system_app_name         => 'SQL bootstrap test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_system_user_id          => '00000000-0000-4000-8000-000000000d00',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_sys_app := (v_init -> 'samna_app' ->> 'id')::UUID;
    v_sys_uc  := (v_init -> 'user_claim' ->> 'id')::UUID;
    INSERT INTO boot VALUES ('sys_app', v_sys_app), ('sys_uc', v_sys_uc);

    PERFORM claimius.create_root_organization(v_sys_app, 'Tenant', v_sys_uc, p_id => v_tenant);
    PERFORM claimius.create_root_organization(v_sys_app, 'Other', v_sys_uc, p_id => v_other);

    PERFORM claimius.ensure_app_user(p_user_id => v_reader, p_app_id => v_sys_app, p_first_name => 'Reader');
    v_read := (claimius.create_claim(
        p_app_id => v_sys_app, p_name => 'Tenant reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_tenant, p_sa_root_id => v_tenant, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_sys_app, p_claim_id => v_read, p_sa_owner_id => v_tenant, p_sa_root_id => v_tenant,
        p_sa_created_by => v_sys_uc, p_object_id => v_tenant, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_read_uc := (claimius.assign_claim_user(
        p_app_id => v_sys_app, p_claim_id => v_read, p_user_id => v_reader,
        p_sa_owner_id => v_tenant, p_sa_created_by => v_sys_uc
    ) -> 'user_claim' ->> 'id')::UUID;
    INSERT INTO boot VALUES ('read_uc', v_read_uc);

    v_made := claimius.create_app(
        p_actor_user_claim_id => v_sys_uc,
        p_slug                => 'sqltest_boot_tenant',
        p_name                => 'Tenant app',
        p_org_id              => v_tenant,
        p_private_key         => 'key',
        p_private_seed        => 'seed'
    );
    INSERT INTO boot VALUES
        ('tenant_app', (v_made -> 'samna_app' ->> 'id')::UUID),
        ('tenant_uc', (v_made -> 'user_claim' ->> 'id')::UUID);
END $$;

CREATE FUNCTION pg_temp.boot(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM boot WHERE key = p_key
$$ LANGUAGE sql;

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.user_claim uc
        WHERE uc.id = pg_temp.boot('sys_uc') AND uc.sa_created_by = uc.id
          AND uc.user_id = '00000000-0000-4000-8000-000000000d00'),
    'init grants the system user its user_claim under its own id');

SELECT is_empty($$
    SELECT 'samna_app' FROM claimius.samna_app WHERE id = pg_temp.boot('sys_app') AND sa_created_by <> pg_temp.boot('sys_uc')
    UNION ALL
    SELECT 'organization' FROM claimius.organization o
    WHERE o.id = (SELECT sa_owner_id FROM claimius.samna_app WHERE id = pg_temp.boot('sys_app')) AND o.sa_created_by <> pg_temp.boot('sys_uc')
    UNION ALL
    SELECT 'claim' FROM claimius.claim WHERE app_id = pg_temp.boot('sys_app') AND sa_created_by <> pg_temp.boot('sys_uc')
    UNION ALL
    SELECT 'claim_object' FROM claimius.claim_object WHERE app_id = pg_temp.boot('sys_app') AND sa_created_by <> pg_temp.boot('sys_uc')
      AND claim_id IN (SELECT id FROM claimius.claim WHERE app_id = pg_temp.boot('sys_app') AND type = 'system')
    UNION ALL
    SELECT 'app_object' FROM claimius.app_object WHERE app_id = pg_temp.boot('sys_app') AND sa_created_by IS DISTINCT FROM pg_temp.boot('sys_uc')
      AND object_id = (SELECT sa_owner_id FROM claimius.samna_app WHERE id = pg_temp.boot('sys_app'))
$$, 'every row a fresh prophet writes carries the system user_claim.id in sa_created_by');

SELECT ok(NOT EXISTS (
        SELECT 1 FROM claimius.samna_user su
        WHERE su.user_id IN (SELECT sa_created_by FROM claimius.claim WHERE app_id = pg_temp.boot('sys_app'))),
    'no sa_created_by init writes is a samna_user id');

SELECT ok(EXISTS (SELECT 1 FROM claimius.get_access('00000000-0000-4000-8000-000000000d00', pg_temp.boot('sys_app'),
        (SELECT sa_owner_id FROM claimius.samna_app WHERE id = pg_temp.boot('sys_app')), 'claimius.organization', 15)),
    'the system user holds the system organization in full');

SELECT is(
    (SELECT sa_created_by FROM claimius.samna_app WHERE slug = 'sqltest_boot_tenant'),
    pg_temp.boot('sys_uc'),
    'create_app stamps the new app with the actor user_claim.id');

SELECT is_empty($$
    SELECT 1 FROM claimius.claim WHERE app_id = pg_temp.boot('tenant_app') AND sa_created_by <> pg_temp.boot('sys_uc')
    UNION ALL
    SELECT 1 FROM claimius.claim_object WHERE app_id = pg_temp.boot('tenant_app') AND sa_created_by <> pg_temp.boot('sys_uc')
    UNION ALL
    SELECT 1 FROM claimius.user_claim WHERE app_id = pg_temp.boot('tenant_app') AND sa_created_by <> pg_temp.boot('sys_uc')
$$, 'every claim, binding and grant of the new app carries the actor user_claim.id');

SELECT is((SELECT count(*) FROM claimius.claim_object WHERE app_id = pg_temp.boot('tenant_app') AND sa_deleted_at IS NULL), 4::BIGINT,
    'the owner of the owning organization binds the owner and default claims of the new app');

SELECT ok(EXISTS (SELECT 1 FROM claimius.get_access('00000000-0000-4000-8000-000000000d00', pg_temp.boot('tenant_app'),
        pg_temp.boot('tenant_app'), 'claimius.samna_app', 15)),
    'the actor holds the new app in full inside it');

SELECT ok(EXISTS (SELECT 1 FROM claimius.get_access('00000000-0000-4000-8000-000000000d00', pg_temp.boot('tenant_app'),
        '10000000-0000-4000-8000-000000000d01', 'claimius.organization', 15)),
    'the actor holds the owning organization in full inside the new app');

SELECT is(claimius.create_app(
        p_actor_user_claim_id => pg_temp.boot('sys_uc'), p_slug => 'sqltest_boot_tenant', p_name => 'Tenant app',
        p_org_id => '10000000-0000-4000-8000-000000000d01', p_private_key => 'key', p_private_seed => 'seed') -> 'samna_app' ->> 'id',
    pg_temp.boot('tenant_app')::TEXT,
    'create_app is idempotent on the slug');

SELECT throws_like($$
    SELECT claimius.create_app(
        p_actor_user_claim_id => pg_temp.boot('read_uc'), p_slug => 'sqltest_boot_reader', p_name => 'Reader app',
        p_org_id => '10000000-0000-4000-8000-000000000d01', p_private_key => 'key', p_private_seed => 'seed')
$$, 'cannot grant more access than held%',
    'an actor reading but not owning the owning organization is refused the first binding');

SELECT throws_like($$
    SELECT claimius.create_app(
        p_actor_user_claim_id => pg_temp.boot('tenant_uc'), p_slug => 'sqltest_boot_other', p_name => 'Other app',
        p_org_id => '10000000-0000-4000-8000-000000000d02', p_private_key => 'key', p_private_seed => 'seed')
$$, 'cannot grant more access than held%',
    'an actor without the owning organization in its own app is refused the first binding');

SELECT * FROM finish();
ROLLBACK;
