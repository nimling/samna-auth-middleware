BEGIN;
SELECT plan(4);

CREATE TEMP TABLE consumer_ids (name TEXT PRIMARY KEY, id UUID);

DO $$
DECLARE
    v_app     UUID;
    v_sys     UUID := '00000000-0000-4000-8000-0000000008d0';
    v_owner   UUID := 'a0000000-0000-4000-8000-0000000008d1';
    v_viewer  UUID := 'a0000000-0000-4000-8000-0000000008d2';
    v_root    UUID := '10000000-0000-4000-8000-0000000008d0';
    v_owners  UUID;
    v_viewers UUID;
    v_target  UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_consumer_binding',
        p_system_app_name         => 'SQL consumer binding test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;
    PERFORM claimius.upsert_app_user(p_user_id => v_owner, p_app_id => v_app, p_first_name => 'Owner');
    PERFORM claimius.upsert_app_user(p_user_id => v_viewer, p_app_id => v_app, p_first_name => 'Viewer');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Consumer root', 'company', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);

    v_owners := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Consumer owners', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys) -> 'claim' ->> 'id')::UUID;
    v_viewers := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Consumer viewers', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys) -> 'claim' ->> 'id')::UUID;
    v_target := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Consumer row owners', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_owners, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_viewers, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);

    INSERT INTO consumer_ids VALUES
        ('app', v_app),
        ('target', v_target),
        ('owner_grant', (claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_owners, p_user_id => v_owner,
            p_sa_owner_id => v_root, p_sa_created_by => v_sys) -> 'user_claim' ->> 'id')::UUID),
        ('viewer_grant', (claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_viewers, p_user_id => v_viewer,
            p_sa_owner_id => v_root, p_sa_created_by => v_sys) -> 'user_claim' ->> 'id')::UUID);
END $$;

CREATE FUNCTION pg_temp.id(p_name TEXT) RETURNS UUID AS $$
    SELECT id FROM consumer_ids WHERE name = p_name
$$ LANGUAGE sql;

SELECT lives_ok(
    $$SELECT claimius.insert_claim_object(
        p_app_id => pg_temp.id('app'), p_claim_id => pg_temp.id('target'),
        p_sa_owner_id => '10000000-0000-4000-8000-0000000008d0', p_sa_root_id => '10000000-0000-4000-8000-0000000008d0',
        p_sa_created_by => pg_temp.id('owner_grant'),
        p_object_id => '20000000-0000-4000-8000-0000000008d1', p_object_type => 'public.sqltest_consumer_row', p_inherits => TRUE)$$,
    'an owner of the binding''s organization binds an owner claim on a row only a disciple holds'
);

SELECT throws_ok(
    $$SELECT claimius.insert_claim_object(
        p_app_id => pg_temp.id('app'), p_claim_id => pg_temp.id('target'),
        p_sa_owner_id => '10000000-0000-4000-8000-0000000008d0', p_sa_root_id => '10000000-0000-4000-8000-0000000008d0',
        p_sa_created_by => pg_temp.id('viewer_grant'),
        p_object_id => '20000000-0000-4000-8000-0000000008d2', p_object_type => 'public.sqltest_consumer_row', p_inherits => TRUE)$$,
    'P0403', NULL,
    'a reader of the binding''s organization cannot hand out owner on a row only a disciple holds'
);

SELECT is(
    (SELECT count(*) FROM claimius.claim_object
      WHERE object_type = 'public.sqltest_consumer_row' AND claim_id = pg_temp.id('target')),
    1::BIGINT,
    'the prophet stores the one binding the owner made, its type passed through untouched'
);

SELECT throws_ok(
    $$SELECT claimius.insert_claim_object(
        p_app_id => pg_temp.id('app'), p_claim_id => pg_temp.id('target'),
        p_sa_owner_id => '10000000-0000-4000-8000-0000000008d0', p_sa_root_id => '10000000-0000-4000-8000-0000000008d0',
        p_sa_created_by => pg_temp.id('viewer_grant'),
        p_object_id => '10000000-0000-4000-8000-0000000008d0', p_object_type => 'claimius.organization', p_inherits => TRUE)$$,
    'P0403', NULL,
    'a binding on a claimius object is still judged on the object itself'
);

SELECT * FROM finish();
ROLLBACK;
