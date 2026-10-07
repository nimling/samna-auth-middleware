BEGIN;
SELECT plan(14);

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_binding'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.org(p_n INTEGER) RETURNS UUID AS $$
    SELECT ('10000000-0000-4000-8000-' || lpad(to_hex(3840 + p_n), 12, '0'))::UUID
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.fresh() RETURNS UUID[] AS $$
    SELECT array_agg(pg_temp.org(n)) FROM generate_series(41, 45) n
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.recomputed() RETURNS BIGINT AS $$
    SELECT pg_stat_get_xact_tuples_inserted('claimius.user_object'::REGCLASS)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.cursor_of(p_user UUID) RETURNS XID8 AS $$
    SELECT coalesce((SELECT a.settled_txid FROM claimius.user_activity a WHERE a.user_id = p_user AND a.app_id = pg_temp.app()), '0'::XID8)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.listed(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.get_objects(p_user, pg_temp.app(), 'claimius.organization')
$$ LANGUAGE sql;

CREATE TEMP TABLE work (n BIGINT);

DO $$
DECLARE
    v_sys   UUID := '00000000-0000-4000-8000-000000000f00';
    v_hal   UUID := 'a0000000-0000-4000-8000-000000000f01';
    v_lin   UUID := 'a0000000-0000-4000-8000-000000000f02';
    v_oto   UUID := 'a0000000-0000-4000-8000-000000000f03';
    v_root  UUID := pg_temp.org(0);
    v_owner UUID;
    v_link  UUID;
    v_hal_uc UUID;
    v_n     INTEGER;
    v_app   UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_binding',
        p_system_app_name         => 'SQL binding test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    PERFORM claimius.upsert_app_user(p_user_id => v_hal, p_app_id => v_app, p_first_name => 'Hal');
    PERFORM claimius.upsert_app_user(p_user_id => v_lin, p_app_id => v_app, p_first_name => 'Lin');
    PERFORM claimius.upsert_app_user(p_user_id => v_oto, p_app_id => v_app, p_first_name => 'Oto');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Bind root', 'company', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    SELECT pg_temp.org(n), 'Bind ' || n, 'company', v_root, v_root, 1, v_sys FROM generate_series(1, 45) n;
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (pg_temp.org(46), 'Bind child', 'company', pg_temp.org(45), v_root, 2, v_sys);

    v_owner := (claimius.insert_claim(p_app_id => v_app,
        p_name => 'Bind owner', p_description => NULL, p_sa_access => 1,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    FOR v_n IN 1 .. 40 LOOP
        PERFORM claimius.insert_claim_object(p_app_id => v_app,
            p_claim_id => v_owner, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
            p_object_id => pg_temp.org(v_n), p_object_type => 'claimius.organization', p_inherits => FALSE);
    END LOOP;
    v_hal_uc := (claimius.insert_user_claim(p_app_id => v_app,
        p_claim_id => v_owner, p_user_id => v_hal, p_sa_owner_id => v_root, p_sa_created_by => v_sys
    ) -> 'user_claim' ->> 'id')::UUID;

    v_link := (claimius.insert_claim(p_app_id => v_app,
        p_name => 'Bind reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_app_id => v_app,
        p_claim_id => v_link, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_hal_uc,
        p_object_id => v_owner, p_object_type => 'claimius.claim', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_link, p_user_id => v_lin, p_sa_owner_id => v_root, p_sa_created_by => v_sys);

    PERFORM pg_temp.listed(v_hal);
    PERFORM pg_temp.listed(v_lin);
    PERFORM pg_temp.listed(v_oto);

    FOR v_n IN 41 .. 45 LOOP
        PERFORM claimius.insert_claim_object(p_app_id => v_app,
            p_claim_id => v_owner, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
            p_object_id => pg_temp.org(v_n), p_object_type => 'claimius.organization', p_inherits => FALSE);
    END LOOP;
END $$;

SELECT is_empty(
    $$SELECT 1 FROM claimius.access_diff
       WHERE kind = 'claim' AND user_id IS NULL AND claim_id = (SELECT id FROM claimius.claim WHERE name = 'Bind owner')$$,
    'a binding raises no area over the whole claim'
);

SELECT set_eq(
    $$SELECT object_id FROM claimius.access_diff WHERE kind = 'object' AND object_id IN (SELECT pg_temp.org(n) FROM generate_series(0, 46) n)$$,
    $$SELECT unnest(pg_temp.fresh())$$,
    'five bindings raise one object area on each bound object and none on the other organizations'
);

INSERT INTO work SELECT pg_temp.recomputed();

SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000000f01'), 45::BIGINT,
    'the holder lists the forty objects bound before and the five bound since');

SELECT is(pg_temp.recomputed() - (SELECT n FROM work), 5::BIGINT,
    'the holder''s listing writes the five newly bound pairs and none of the forty');

SELECT is(
    (SELECT count(*) FROM claimius.access_diff WHERE kind = 'object' AND object_id = ANY(pg_temp.fresh())),
    5::BIGINT,
    'the areas stay stored for the holders who have not read since, until the drain trims them'
);

SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000000f02'), 45::BIGINT,
    'a holder of a claim linking to the bound claim lists the five new objects too');

SELECT is(pg_temp.cursor_of('a0000000-0000-4000-8000-000000000f02'), pg_snapshot_xmin(pg_current_snapshot()),
    'the link holder''s listing moves its cursor past the areas');

SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000000f03'), 0::BIGINT,
    'a user holding neither claim lists nothing');

DO $$
BEGIN
    PERFORM claimius.delete_claim_object(
        (SELECT id FROM claimius.claim_object WHERE object_id = pg_temp.org(41) AND sa_deleted_at IS NULL),
        '00000000-0000-4000-8000-000000000f00');
END $$;

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_access('a0000000-0000-4000-8000-000000000f01', pg_temp.app(), pg_temp.org(41), 'claimius.organization', 1)$$,
    'a removed binding takes the grant away on the holder''s next check'
);

SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000000f02'), 44::BIGINT,
    'a removed binding takes the object out of the link holder''s next listing');

DO $$
BEGIN
    PERFORM claimius.update_claim_object(
        (SELECT id FROM claimius.claim_object WHERE object_id = pg_temp.org(45) AND sa_deleted_at IS NULL),
        p_inherits => TRUE);
END $$;

SELECT is(
    (SELECT sa_access FROM claimius.get_access('a0000000-0000-4000-8000-000000000f01', pg_temp.app(), pg_temp.org(46), 'claimius.organization', 1)),
    15,
    'a binding turned to inherit reaches the bound object''s child on the holder''s next check'
);

SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000000f02'), 45::BIGINT,
    'the link holder lists the child once the binding inherits');

DO $$
BEGIN
    PERFORM claimius.update_claim_object(
        (SELECT id FROM claimius.claim_object WHERE object_id = pg_temp.org(45) AND sa_deleted_at IS NULL),
        p_inherits => FALSE);
END $$;

SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000000f01'), 44::BIGINT,
    'a binding that stops inheriting takes the child out of the holder''s next listing');

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_access('a0000000-0000-4000-8000-000000000f02', pg_temp.app(), pg_temp.org(46), 'claimius.organization', 4)$$,
    'and out of the link holder''s next check'
);

SELECT * FROM finish();
ROLLBACK;
