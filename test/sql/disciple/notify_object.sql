BEGIN;
SELECT plan(5);

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-000000000f50';
    v_org    UUID := '10000000-0000-4000-8000-000000000f51';
    v_writer UUID := 'a0000000-0000-4000-8000-000000000f51';
    v_reader UUID := 'a0000000-0000-4000-8000-000000000f52';
    v_write  UUID;
    v_read   UUID;
    v_link   UUID;
    v_held   UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();

    PERFORM claimius.upsert_app_user(p_user_id => v_writer, p_first_name => 'Writer');
    PERFORM claimius.upsert_app_user(p_user_id => v_reader, p_first_name => 'Reader');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000f55', p_first_name => 'Outsider');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_org, 'Notice', 'company', v_org, v_org, 0, v_sys);

    v_write := (claimius.insert_claim(
        p_name => 'Notice writer', p_description => NULL, p_sa_access => 6,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_write, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_read := (claimius.insert_claim(
        p_name => 'Notice reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_read, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);

    PERFORM claimius.insert_user_claim(p_claim_id => v_write, p_user_id => v_writer,
        p_sa_owner_id => v_org, p_sa_created_by => v_sys);
    PERFORM claimius.insert_user_claim(p_claim_id => v_read, p_user_id => v_reader,
        p_sa_owner_id => v_org, p_sa_created_by => v_sys);

    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000f53', p_first_name => 'Composed');
    v_held := (SELECT uc.id FROM claimius.user_claim uc WHERE uc.user_id = v_reader AND uc.claim_id = v_read);
    v_link := (claimius.insert_claim(
        p_name => 'Notice link', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_link, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_held,
        p_object_id => v_read, p_object_type => 'claimius.claim', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_claim_id => v_link, p_user_id => 'a0000000-0000-4000-8000-000000000f53',
        p_sa_owner_id => v_org, p_sa_created_by => v_sys);
END $$;

SELECT is_empty($$
    SELECT 1 FROM claimius.user_object
    WHERE user_id = 'a0000000-0000-4000-8000-000000000f52'
      AND object_type = 'claimius.organization' AND object_id = '10000000-0000-4000-8000-000000000f51'
$$, 'the reader has not read the organization, so its pair is not settled');

SELECT ok(claimius.notify_object(NULL, 'Notice', NULL, '{}', 'info', NULL,
        'claimius.organization', '10000000-0000-4000-8000-000000000f51', 'a0000000-0000-4000-8000-000000000f51', NULL, NULL) > 0,
    'a notice on the organization reaches its readers');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.notification
        WHERE user_id = 'a0000000-0000-4000-8000-000000000f52' AND title = 'Notice'),
    'the reader whose pair was never settled is notified');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.notification
        WHERE user_id = 'a0000000-0000-4000-8000-000000000f53' AND title = 'Notice'),
    'a holder of a claim composed with a binding claim is notified');

SELECT is_empty($$
    SELECT 1 FROM claimius.notification
    WHERE title = 'Notice'
      AND user_id IN ('a0000000-0000-4000-8000-000000000f51', 'a0000000-0000-4000-8000-000000000f55')
$$, 'neither the writer nor a user without a claim reaching the organization is notified');

SELECT * FROM finish();
ROLLBACK;
