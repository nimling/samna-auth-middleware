BEGIN;
SELECT plan(8);

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_subtree'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.org(p_n INTEGER) RETURNS UUID AS $$
    SELECT ('10000000-0000-4000-8000-' || lpad(to_hex(14400000 + p_n), 12, '0'))::UUID
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.listed(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.get_objects(p_user, pg_temp.app(), 'claimius.organization')
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.pairs(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.user_object
    WHERE user_id = p_user AND app_id = pg_temp.app() AND object_type = 'claimius.organization'
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-000000d4a000';
    v_holder UUID := 'a0000000-0000-4000-8000-000000d4a001';
    v_app    UUID;
    v_crew   UUID;
    v_n      INTEGER;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_subtree',
        p_system_app_name         => 'SQL subtree delete test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;
    PERFORM claimius.upsert_app_user(p_user_id => v_holder, p_app_id => v_app, p_first_name => 'Holder');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (pg_temp.org(0), 'Subtree root', 'customer', pg_temp.org(0), pg_temp.org(0), 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', pg_temp.org(0));
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (pg_temp.org(1), 'Subtree node', 'customer', pg_temp.org(0), pg_temp.org(0), 1, v_sys);
    FOR v_n IN 1 .. 5 LOOP
        INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
        VALUES (pg_temp.org(10 + v_n), 'Subtree child ' || v_n, 'customer', pg_temp.org(1), pg_temp.org(0), 2, v_sys);
        INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
        VALUES (pg_temp.org(20 + v_n), 'Subtree grandchild ' || v_n, 'customer', pg_temp.org(10 + v_n), pg_temp.org(0), 3, v_sys);
    END LOOP;
    v_crew := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Subtree crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => pg_temp.org(0), p_sa_root_id => pg_temp.org(0), p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => pg_temp.org(0), p_sa_root_id => pg_temp.org(0), p_sa_created_by => v_sys,
        p_object_id => pg_temp.org(1), p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_holder, p_sa_owner_id => pg_temp.org(0), p_sa_created_by => v_sys);
END $$;

SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000d4a001'), 11::BIGINT, 'the holder lists the node and its ten descendants');

DELETE FROM claimius.access_diff WHERE app_id = pg_temp.app();
UPDATE claimius.organization SET sa_deleted_at = now() WHERE id = pg_temp.org(1);

SELECT is(
    (SELECT count(*) FROM claimius.access_diff WHERE app_id = pg_temp.app() AND kind = 'object' AND NOT cascade
      AND object_id IN (SELECT pg_temp.org(10 + n) FROM generate_series(1, 5) n UNION SELECT pg_temp.org(20 + n) FROM generate_series(1, 5) n)),
    10::BIGINT, 'soft deleting the node raises one area per dropped descendant');
SELECT ok(EXISTS (SELECT 1 FROM claimius.access_diff WHERE app_id = pg_temp.app() AND kind = 'object' AND cascade AND object_id = pg_temp.org(1)),
    'and the cascade area on the node itself');
SELECT is_empty($$SELECT 1 FROM claimius.inheritance_info WHERE descendant_id = pg_temp.org(25)$$,
    'the closure rows of the descendants are gone');

SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000d4a001'), 0::BIGINT, 'the holder''s next listing no longer returns the subtree');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-000000d4a001'), 0::BIGINT, 'and holds no pair on it');

UPDATE claimius.organization SET sa_deleted_at = NULL WHERE id = pg_temp.org(1);

SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000d4a001'), 11::BIGINT, 'restoring the node returns the subtree on the holder''s next listing');
SELECT is(
    (SELECT sa_access FROM claimius.get_access('a0000000-0000-4000-8000-000000d4a001', pg_temp.app(), pg_temp.org(25), 'claimius.organization', 4)),
    4, 'and a check on a grandchild answers the inherited grant');

SELECT * FROM finish();
ROLLBACK;
