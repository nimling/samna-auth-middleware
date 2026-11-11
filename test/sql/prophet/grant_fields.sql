SELECT plan(9);
CREATE TEMP TABLE started AS SELECT pg_current_xact_id()::TEXT::BIGINT AS txid;

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_fields'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.writes() RETURNS BIGINT AS $$
    SELECT sum(n_tup_ins + n_tup_upd + n_tup_del) FROM pg_stat_xact_user_tables
    WHERE schemaname = 'claimius' AND relname IN ('user_object', 'user_claim', 'claim', 'access_diff', 'user_activity')
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.grant_reads() RETURNS BIGINT AS $$
    SELECT sum(pg_stat_get_xact_tuples_returned(i.indexrelid))::BIGINT
    FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid
    WHERE c.oid IN ('claimius.user_claim'::REGCLASS, 'claimius.claim'::REGCLASS)
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys     UUID := '00000000-0000-4000-8000-000000d7a000';
    v_timed   UUID := 'a0000000-0000-4000-8000-000000d7a001';
    v_named   UUID := 'a0000000-0000-4000-8000-000000d7a002';
    v_root    UUID := '10000000-0000-4000-8000-000000d7a000';
    v_x       UUID := '10000000-0000-4000-8000-000000d7a001';
    v_app     UUID;
    v_crew    UUID;
    v_name    UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_fields',
        p_system_app_name         => 'SQL grant fields test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;
    PERFORM claimius.upsert_app_user(p_user_id => v_timed, p_app_id => v_app, p_first_name => 'Timed');
    PERFORM claimius.upsert_app_user(p_user_id => v_named, p_app_id => v_app, p_first_name => 'Named');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Fields root', 'customer', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_x, 'Fields x', 'customer', v_root, v_root, 1, v_sys);
    v_crew := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Fields crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_timed, p_sa_owner_id => v_root,
        p_sa_created_by => v_sys, p_ends_at => now() + INTERVAL '1 hour');
    v_name := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Fields name', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_name, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_x, p_object_type => 'claimius.organization', p_inherits => FALSE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_name, p_user_id => v_named, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
END $$;
COMMIT;

BEGIN;
SELECT is((SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000d7a001', pg_temp.app(), 'claimius.organization')), 2::BIGINT,
    'inside its window the timed grant lists the root and x');
SELECT is(
    (SELECT claim_name FROM claimius.get_access('a0000000-0000-4000-8000-000000d7a001', pg_temp.app(),
        '10000000-0000-4000-8000-000000d7a001', 'claimius.organization', 4)),
    'Fields crew', 'and a check answers the grant by name');
SELECT is(
    (SELECT g ->> 'claim_name' FROM claimius.user_object uo CROSS JOIN LATERAL jsonb_array_elements(uo.grants) g
      WHERE uo.user_id = 'a0000000-0000-4000-8000-000000d7a001' AND uo.app_id = pg_temp.app()
        AND uo.object_type = 'claimius.organization' AND uo.object_id = '10000000-0000-4000-8000-000000d7a001'),
    'Fields crew', 'the pair stores the claim name, the grant and its window with the derivation');
SELECT is((SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000d7a002', pg_temp.app(), 'claimius.organization')), 1::BIGINT,
    'the named holder lists x');
COMMIT;

UPDATE claimius.user_object uo
SET grants = (SELECT jsonb_agg(jsonb_set(g, '{ends_at}', to_jsonb(now() - INTERVAL '1 second'))) FROM jsonb_array_elements(uo.grants) g)
WHERE uo.user_id = 'a0000000-0000-4000-8000-000000d7a001';
COMMIT;

BEGIN;
CREATE TEMP TABLE closed AS SELECT pg_temp.writes() AS writes, pg_temp.grant_reads() AS reads;
SELECT is_empty(
    $$SELECT 1 FROM claimius.get_access('a0000000-0000-4000-8000-000000d7a001', pg_temp.app(),
        '10000000-0000-4000-8000-000000d7a001', 'claimius.organization', 4)$$,
    'once the window closes a check answers nothing');
SELECT is((SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000d7a001', pg_temp.app(), 'claimius.organization')), 0::BIGINT,
    'and a listing returns nothing');
SELECT is(pg_temp.writes() - (SELECT writes FROM closed), 0::BIGINT, 'with no row written');
SELECT is(pg_temp.grant_reads() - (SELECT reads FROM closed), 0::BIGINT, 'and no probe of the grant or the claim: the window is read from the pair');
COMMIT;

SELECT claimius.update_claim((SELECT id FROM claimius.claim WHERE name = 'Fields name'), p_name => 'Fields renamed');
COMMIT;

BEGIN;
SELECT is(
    (SELECT claim_name FROM claimius.get_objects('a0000000-0000-4000-8000-000000d7a002', pg_temp.app(), 'claimius.organization')),
    'Fields renamed', 'a claim renamed shows its new name on the holder''s next read');
COMMIT;

DO $$
DECLARE
    v_table TEXT;
    v_from  BIGINT := (SELECT txid FROM started);
    v_rows  BIGINT;
    v_left  BOOLEAN := TRUE;
    v_pass  INTEGER := 0;
BEGIN
    WHILE v_left AND v_pass < 10 LOOP
        v_left := FALSE;
        v_pass := v_pass + 1;
        FOR v_table IN
            SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
            WHERE n.nspname = 'claimius' AND c.relkind IN ('r', 'p') AND NOT c.relispartition AND c.relname <> 'table_info'
        LOOP
            BEGIN
                EXECUTE format('DELETE FROM claimius.%I WHERE xmin::TEXT::BIGINT >= $1', v_table) USING v_from;
                GET DIAGNOSTICS v_rows = ROW_COUNT;
                v_left := v_left OR v_rows > 0;
            EXCEPTION WHEN foreign_key_violation THEN
                v_left := TRUE;
            END;
        END LOOP;
    END LOOP;
END $$;
COMMIT;

SELECT * FROM finish();
COMMIT;
DISCARD TEMP;
