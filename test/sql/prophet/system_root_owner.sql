BEGIN;
SELECT plan(4);

CREATE TEMP TABLE sro (key TEXT PRIMARY KEY, id UUID);

DO $$
DECLARE
    v_init      JSONB;
    v_sys_app   UUID;
    v_sys_uc    UUID;
    v_root      UUID := '10000000-0000-4000-8000-000000000f71';
    v_from      UUID := '10000000-0000-4000-8000-000000000f72';
    v_to        UUID := '10000000-0000-4000-8000-000000000f73';
    v_admin     UUID := 'a0000000-0000-4000-8000-000000000f71';
    v_plain     UUID := 'a0000000-0000-4000-8000-000000000f72';
    v_flat      UUID := 'a0000000-0000-4000-8000-000000000f73';
    v_sysadmin  UUID;
    v_flatadmin UUID;
    v_admin_uc  UUID;
    v_made      JSONB;
    v_app       UUID;
    v_owner     UUID;
    v_app_uc    UUID;
BEGIN
    v_init := claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_system_root_owner',
        p_system_app_name         => 'SQL system root owner test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_system_user_id          => '00000000-0000-4000-8000-000000000f70',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_sys_app := (v_init -> 'samna_app' ->> 'id')::UUID;
    v_sys_uc  := (v_init -> 'user_claim' ->> 'id')::UUID;

    PERFORM claimius.upsert_root_organization(v_sys_app, 'Root', v_sys_uc, p_id => v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_from, 'From', 'customer', v_root, v_root, 1, v_sys_uc),
           (v_to, 'To', 'customer', v_root, v_root, 1, v_sys_uc);

    PERFORM claimius.upsert_app_user(p_user_id => v_admin, p_app_id => v_sys_app, p_first_name => 'Admin');
    PERFORM claimius.upsert_app_user(p_user_id => v_flat, p_app_id => v_sys_app, p_first_name => 'Flat');

    v_sysadmin := (claimius.insert_claim(
        p_app_id => v_sys_app, p_name => 'System Administrator', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_sys_app, p_claim_id => v_sysadmin, p_sa_owner_id => v_root, p_sa_root_id => v_root,
        p_sa_created_by => v_sys_uc, p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_admin_uc := (claimius.insert_user_claim(
        p_app_id => v_sys_app, p_claim_id => v_sysadmin, p_user_id => v_admin,
        p_sa_owner_id => v_root, p_sa_created_by => v_sys_uc
    ) -> 'user_claim' ->> 'id')::UUID;

    v_flatadmin := (claimius.insert_claim(
        p_app_id => v_sys_app, p_name => 'Flat Administrator', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_sys_app, p_claim_id => v_flatadmin, p_sa_owner_id => v_root, p_sa_root_id => v_root,
        p_sa_created_by => v_sys_uc, p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => FALSE);
    PERFORM claimius.insert_user_claim(
        p_app_id => v_sys_app, p_claim_id => v_flatadmin, p_user_id => v_flat,
        p_sa_owner_id => v_root, p_sa_created_by => v_sys_uc);

    v_made := claimius.upsert_app(
        p_actor_user_claim_id => v_admin_uc,
        p_slug                => 'sqltest_system_root_owner_app',
        p_name                => 'Moved app',
        p_org_id              => v_from,
        p_private_key         => 'key',
        p_private_seed        => 'seed'
    );
    v_app    := (v_made -> 'samna_app' ->> 'id')::UUID;
    v_owner  := (v_made -> 'claim' ->> 'id')::UUID;
    v_app_uc := (v_made -> 'user_claim' ->> 'id')::UUID;

    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_to);
    PERFORM claimius.upsert_app_user(p_user_id => v_plain, p_app_id => v_app, p_first_name => 'Plain');
    PERFORM claimius.upsert_app_user(p_user_id => v_flat, p_app_id => v_app, p_first_name => 'Flat');

    INSERT INTO sro VALUES
        ('app', v_app), ('owner', v_owner), ('app_uc', v_app_uc),
        ('plain_uc', (claimius.insert_user_claim(
            p_app_id => v_app, p_claim_id => v_owner, p_user_id => v_plain,
            p_sa_owner_id => v_from, p_sa_created_by => v_app_uc) -> 'user_claim' ->> 'id')::UUID),
        ('flat_uc', (claimius.insert_user_claim(
            p_app_id => v_app, p_claim_id => v_owner, p_user_id => v_flat,
            p_sa_owner_id => v_from, p_sa_created_by => v_app_uc) -> 'user_claim' ->> 'id')::UUID);
END $$;

CREATE FUNCTION pg_temp.sro(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM sro WHERE key = p_key
$$ LANGUAGE sql;

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.claim_object
        WHERE app_id = pg_temp.sro('app') AND sa_deleted_at IS NULL),
    'the moved app already holds pairs, so the first binding exception does not apply');

SELECT throws_ok($$
    SELECT claimius.insert_claim_object(
        p_app_id => pg_temp.sro('app'), p_claim_id => pg_temp.sro('owner'),
        p_sa_owner_id => '10000000-0000-4000-8000-000000000f73', p_sa_root_id => '10000000-0000-4000-8000-000000000f71',
        p_sa_created_by => pg_temp.sro('plain_uc'),
        p_object_id => '10000000-0000-4000-8000-000000000f73', p_object_type => 'claimius.organization', p_inherits => FALSE)
    $$,
    'P0403', NULL,
    'a user without a system grant is refused');

SELECT throws_ok($$
    SELECT claimius.insert_claim_object(
        p_app_id => pg_temp.sro('app'), p_claim_id => pg_temp.sro('owner'),
        p_sa_owner_id => '10000000-0000-4000-8000-000000000f73', p_sa_root_id => '10000000-0000-4000-8000-000000000f71',
        p_sa_created_by => pg_temp.sro('flat_uc'),
        p_object_id => '10000000-0000-4000-8000-000000000f73', p_object_type => 'claimius.organization', p_inherits => FALSE)
    $$,
    'P0403', NULL,
    'a system grant without inherits does not reach a child organization');

SELECT lives_ok($$
    SELECT claimius.insert_claim_object(
        p_app_id => pg_temp.sro('app'), p_claim_id => pg_temp.sro('owner'),
        p_sa_owner_id => '10000000-0000-4000-8000-000000000f73', p_sa_root_id => '10000000-0000-4000-8000-000000000f71',
        p_sa_created_by => pg_temp.sro('app_uc'),
        p_object_id => '10000000-0000-4000-8000-000000000f73', p_object_type => 'claimius.organization', p_inherits => TRUE)
    $$,
    'the system root owner binds the app owner claim on a sibling organization');

SELECT * FROM finish();
ROLLBACK;
