BEGIN;
SELECT plan(10);

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_claim_areas'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.claim(p_name TEXT) RETURNS UUID AS $$
    SELECT id FROM claimius.claim WHERE name = p_name AND app_id = pg_temp.app()
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.areas() RETURNS TABLE (kind TEXT, object_type TEXT, object_id UUID, cascade BOOLEAN) AS $$
    SELECT d.kind, d.object_type, d.object_id, d.cascade FROM claimius.access_diff d WHERE d.app_id = pg_temp.app()
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys   UUID := '00000000-0000-4000-8000-000000d3a000';
    v_root  UUID := '10000000-0000-4000-8000-000000d3a000';
    v_a     UUID := '10000000-0000-4000-8000-000000d3a001';
    v_a1    UUID := '10000000-0000-4000-8000-000000d3a002';
    v_b     UUID := '10000000-0000-4000-8000-000000d3a003';
    v_d     UUID := '10000000-0000-4000-8000-000000d3a004';
    v_app   UUID;
    v_crew  UUID;
    v_link  UUID;
    v_user  UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_claim_areas',
        p_system_app_name         => 'SQL claim change areas test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-000000d3a001', 'a0000000-0000-4000-8000-000000d3a002',
                                  'a0000000-0000-4000-8000-000000d3a003', 'a0000000-0000-4000-8000-000000d3a004']::UUID[] LOOP
        PERFORM claimius.upsert_app_user(p_user_id => v_user, p_app_id => v_app, p_first_name => 'Holder');
    END LOOP;
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Areas root', 'company', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_a, 'Areas a', 'company', v_root, v_root, 1, v_sys),
           (v_b, 'Areas b', 'company', v_root, v_root, 1, v_sys),
           (v_d, 'Areas d', 'company', v_root, v_root, 1, v_sys),
           (v_a1, 'Areas a1', 'company', v_a, v_root, 2, v_sys);
    v_crew := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Areas crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_a, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_b, p_object_type => 'claimius.organization', p_inherits => FALSE);
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_d, p_object_type => 'claimius.organization', p_inherits => FALSE);
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-000000d3a001', 'a0000000-0000-4000-8000-000000d3a002',
                                  'a0000000-0000-4000-8000-000000d3a003']::UUID[] LOOP
        PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
        PERFORM count(*) FROM claimius.get_objects(v_user, v_app, 'claimius.organization');
    END LOOP;
    v_link := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Areas link', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    DELETE FROM claimius.access_diff WHERE app_id = v_app;
END $$;

SELECT is(
    (SELECT sa_access FROM claimius.get_access('a0000000-0000-4000-8000-000000d3a001', pg_temp.app(),
        '10000000-0000-4000-8000-000000d3a002', 'claimius.organization', 4)),
    4, 'a holder reads a1 through the inheriting binding on a');

SELECT claimius.update_claim(pg_temp.claim('Areas crew'), p_sa_access => 6);

SELECT set_eq(
    $$SELECT kind, object_type, object_id, cascade FROM pg_temp.areas()$$,
    $$VALUES ('object', 'claimius.organization', '10000000-0000-4000-8000-000000d3a001'::UUID, TRUE),
             ('object', 'claimius.organization', '10000000-0000-4000-8000-000000d3a003'::UUID, FALSE),
             ('object', 'claimius.organization', '10000000-0000-4000-8000-000000d3a004'::UUID, FALSE),
             ('object', 'claimius.claim', (SELECT pg_temp.claim('Areas crew')), FALSE)$$,
    'a mask change raises one object area per binding, with cascade where the binding inherits, and one on the claim object');

SELECT is((SELECT count(*) FROM pg_temp.areas()), 4::BIGINT, 'and none per holder');

SELECT is(
    (SELECT sa_access FROM claimius.get_access('a0000000-0000-4000-8000-000000d3a001', pg_temp.app(),
        '10000000-0000-4000-8000-000000d3a002', 'claimius.organization', 4)),
    14, 'a holder''s check under the binding is covered by the area on the bound node and carries the expanded mask');

SELECT is(
    (SELECT sa_access FROM claimius.get_access('a0000000-0000-4000-8000-000000d3a002', pg_temp.app(),
        '10000000-0000-4000-8000-000000d3a003', 'claimius.organization', 4)),
    14, 'a holder''s check on a bound object is covered by the area on it');

DELETE FROM claimius.access_diff WHERE app_id = pg_temp.app();
DO $$
BEGIN
    PERFORM claimius.insert_claim_object(
        p_app_id => pg_temp.app(), p_claim_id => pg_temp.claim('Areas link'), p_sa_owner_id => '10000000-0000-4000-8000-000000d3a000',
        p_sa_root_id => '10000000-0000-4000-8000-000000d3a000',
        p_sa_created_by => (SELECT uc.id FROM claimius.user_claim uc WHERE uc.user_id = 'a0000000-0000-4000-8000-000000d3a001' AND uc.claim_id = pg_temp.claim('Areas crew')),
        p_object_id => pg_temp.claim('Areas crew'), p_object_type => 'claimius.claim', p_inherits => TRUE);
END $$;

SELECT set_eq(
    $$SELECT kind, object_type, object_id FROM pg_temp.areas()$$,
    $$VALUES ('object', 'claimius.organization', '10000000-0000-4000-8000-000000d3a001'::UUID),
             ('object', 'claimius.organization', '10000000-0000-4000-8000-000000d3a003'::UUID),
             ('object', 'claimius.organization', '10000000-0000-4000-8000-000000d3a004'::UUID),
             ('object', 'claimius.claim', (SELECT pg_temp.claim('Areas crew'))),
             ('object', 'claimius.claim', (SELECT pg_temp.claim('Areas link')))$$,
    'a link raises one object area per binding of both claims and one on each claim object, n_A + n_B + 2');

DELETE FROM claimius.access_diff WHERE app_id = pg_temp.app();
SELECT claimius.update_claim(pg_temp.claim('Areas crew'), p_level => 2);

SELECT is((SELECT count(*) FROM pg_temp.areas() WHERE kind = 'object'), 4::BIGINT,
    'a level change raises the same four object areas');
SELECT is_empty($$SELECT 1 FROM pg_temp.areas() WHERE kind <> 'object'$$, 'and no area of another kind');


DO $$
BEGIN
    PERFORM claimius.insert_user_claim(p_app_id => pg_temp.app(), p_claim_id => pg_temp.claim('Areas link'),
        p_user_id => 'a0000000-0000-4000-8000-000000d3a004', p_sa_owner_id => '10000000-0000-4000-8000-000000d3a000',
        p_sa_created_by => '00000000-0000-4000-8000-000000d3a000');
END $$;

SELECT is(
    (SELECT count(*) FROM pg_temp.areas() WHERE kind = 'assignment' AND object_id IS NULL),
    1::BIGINT, 'a grant raises one assignment area for its user');

SELECT is(
    (SELECT claim_name FROM claimius.get_access('a0000000-0000-4000-8000-000000d3a004', pg_temp.app(),
        '10000000-0000-4000-8000-000000d3a002', 'claimius.organization', 4)),
    'Areas link', 'the link holder''s check under the binding is covered and answers through the linked claim');

SELECT * FROM finish();
ROLLBACK;
