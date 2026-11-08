BEGIN;
SELECT plan(4);

DO $$
DECLARE
    v_init   JSONB;
    v_app    UUID;
    v_sys_uc UUID;
    v_org    UUID := '10000000-0000-4000-8000-000000000f51';
    v_writer UUID := 'a0000000-0000-4000-8000-000000000f51';
    v_reader UUID := 'a0000000-0000-4000-8000-000000000f52';
    v_write  UUID;
    v_read   UUID;
BEGIN
    v_init := claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_notice',
        p_system_app_name         => 'SQL notice test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_app    := (v_init -> 'samna_app' ->> 'id')::UUID;
    v_sys_uc := (v_init -> 'user_claim' ->> 'id')::UUID;

    PERFORM claimius.upsert_root_organization(v_app, 'Notice', v_sys_uc, p_id => v_org);
    PERFORM claimius.upsert_app_user(p_user_id => v_writer, p_app_id => v_app, p_first_name => 'Writer');
    PERFORM claimius.upsert_app_user(p_user_id => v_reader, p_app_id => v_app, p_first_name => 'Reader');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000f55', p_app_id => v_app, p_first_name => 'Outsider');

    v_write := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Notice writer', p_description => NULL, p_sa_access => 6,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_write, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_read := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Notice reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_read, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);

    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_write, p_user_id => v_writer,
        p_sa_owner_id => v_org, p_sa_created_by => v_sys_uc);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_read, p_user_id => v_reader,
        p_sa_owner_id => v_org, p_sa_created_by => v_sys_uc);
END $$;

SELECT is_empty($$
    SELECT 1 FROM claimius.user_object
    WHERE app_id = (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_notice')
      AND user_id = 'a0000000-0000-4000-8000-000000000f52'
      AND object_type = 'claimius.organization' AND object_id = '10000000-0000-4000-8000-000000000f51'
$$, 'the reader has not read the organization, so its pair is not settled');

SELECT ok(claimius.notify_object((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_notice'), 'Notice', NULL, '{}', 'info', NULL,
        'claimius.organization', '10000000-0000-4000-8000-000000000f51', 'a0000000-0000-4000-8000-000000000f51', NULL, NULL) > 0,
    'a notice on the organization reaches its readers');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.notification
        WHERE app_id = (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_notice')
          AND user_id = 'a0000000-0000-4000-8000-000000000f52' AND title = 'Notice'),
    'the reader whose pair was never settled is notified');

SELECT is_empty($$
    SELECT 1 FROM claimius.notification
    WHERE title = 'Notice'
      AND user_id IN ('a0000000-0000-4000-8000-000000000f51', 'a0000000-0000-4000-8000-000000000f55')
$$, 'neither the writer nor a user without a claim reaching the organization is notified');

SELECT * FROM finish();
ROLLBACK;
