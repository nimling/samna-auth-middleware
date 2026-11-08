SELECT plan(12);
SET track_functions = 'pl';
CREATE TEMP TABLE started AS SELECT pg_current_xact_id()::TEXT::BIGINT AS txid;

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_mark'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.entries() RETURNS BIGINT AS $$
    SELECT sum(pg_stat_get_xact_tuples_returned(i.indexrelid))::BIGINT
    FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'claimius'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.recomputes() RETURNS BIGINT AS $$
    SELECT coalesce((SELECT calls FROM pg_stat_xact_user_functions
                      WHERE schemaname = 'claimius' AND funcname = 'recompute_user_object'), 0)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.updated() RETURNS BIGINT AS $$
    SELECT pg_stat_get_xact_tuples_updated('claimius.user_object'::REGCLASS)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.hot_updated() RETURNS BIGINT AS $$
    SELECT pg_stat_get_xact_tuples_hot_updated('claimius.user_object'::REGCLASS)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.mark() RETURNS XID8 AS $$
    SELECT uo.settled_txid FROM claimius.user_object uo
    WHERE uo.user_id = 'a0000000-0000-4000-8000-000000d2a001' AND uo.app_id = pg_temp.app()
      AND uo.object_type = 'claimius.organization' AND uo.object_id = '10000000-0000-4000-8000-000000d2a003'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.check_x() RETURNS INTEGER AS $$
    SELECT sa_access FROM claimius.get_access('a0000000-0000-4000-8000-000000d2a001', pg_temp.app(),
        '10000000-0000-4000-8000-000000d2a003', 'claimius.organization', 4)
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-000000d2a000';
    v_reader UUID := 'a0000000-0000-4000-8000-000000d2a001';
    v_root   UUID := '10000000-0000-4000-8000-000000d2a000';
    v_a      UUID := '10000000-0000-4000-8000-000000d2a001';
    v_b      UUID := '10000000-0000-4000-8000-000000d2a002';
    v_x      UUID := '10000000-0000-4000-8000-000000d2a003';
    v_app    UUID;
    v_crew   UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_mark',
        p_system_app_name         => 'SQL check mark test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;
    PERFORM claimius.upsert_app_user(p_user_id => v_reader, p_app_id => v_app, p_first_name => 'Reader');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Mark root', 'company', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_a, 'Mark a', 'company', v_root, v_root, 1, v_sys),
           (v_b, 'Mark b', 'company', v_a, v_root, 2, v_sys),
           (v_x, 'Mark x', 'company', v_b, v_root, 3, v_sys);
    v_crew := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Mark crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_reader, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
END $$;
COMMIT;

BEGIN;
SELECT is((SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000d2a001', pg_temp.app(), 'claimius.organization')), 4::BIGINT,
    'the reader lists the root and the chain under it');
COMMIT;

BEGIN;
CREATE TEMP TABLE quiet_run AS SELECT pg_temp.entries() AS entries, pg_temp.mark() AS mark, pg_temp.updated() AS updated;
SELECT is(pg_temp.check_x(), 4, 'a check answers the inherited grant');
SELECT diag('a check with nothing raised since the mark read ' || (pg_temp.entries() - (SELECT entries FROM quiet_run)) || ' index entries');
SELECT cmp_ok(pg_temp.entries() - (SELECT entries FROM quiet_run), '<=', 10::BIGINT,
    'a check with nothing raised since the mark reads the pair, its object_info row, the trimmed position, the two empty area ranges and samna_user');
SELECT is(pg_temp.updated() - (SELECT updated FROM quiet_run), 0::BIGINT, 'and writes nothing');
COMMIT;

INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
VALUES ('10000000-0000-4000-8000-000000d2a004', 'Mark sibling', 'company',
        '10000000-0000-4000-8000-000000d2a000', '10000000-0000-4000-8000-000000d2a000', 1,
        '00000000-0000-4000-8000-000000d2a000');
COMMIT;

BEGIN;
CREATE TEMP TABLE sibling_run AS SELECT pg_temp.entries() AS entries, pg_temp.mark() AS mark, pg_temp.recomputes() AS recomputes, pg_temp.updated() AS updated, pg_temp.hot_updated() AS hot_updated;
SELECT is(pg_temp.check_x(), 4, 'a check after an unrelated raise in the root still answers the grant');
SELECT diag('a check after an unrelated raise in the root read ' || (pg_temp.entries() - (SELECT entries FROM sibling_run)) || ' index entries');
SELECT cmp_ok(pg_temp.entries() - (SELECT entries FROM sibling_run), '<=', 2 * 3 + 11::BIGINT,
    'after an unrelated raise in the root a check reads at most 2a_x + 11 entries');
SELECT is(pg_temp.recomputes() - (SELECT recomputes FROM sibling_run), 0::BIGINT, 'and recomputes nothing');
SELECT ok(pg_temp.mark() > (SELECT mark FROM sibling_run) AND pg_temp.mark() = pg_snapshot_xmin(pg_current_snapshot()),
    'and moves the pair''s mark to the snapshot xmin');
SELECT is(pg_temp.hot_updated() - (SELECT hot_updated FROM sibling_run), pg_temp.updated() - (SELECT updated FROM sibling_run), 'the mark moves as a heap only update');
COMMIT;

SELECT claimius._raise_object_diff(pg_temp.app(), 'claimius.organization', '10000000-0000-4000-8000-000000d2a001', TRUE, '10000000-0000-4000-8000-000000d2a000');
COMMIT;

BEGIN;
CREATE TEMP TABLE raise_run AS SELECT pg_temp.recomputes() AS recomputes;
SELECT is(pg_temp.check_x(), 4, 'a check after a raise on an ancestor answers the grant');
SELECT is(pg_temp.recomputes() - (SELECT recomputes FROM raise_run), 1::BIGINT, 'and recomputes the one pair once');
COMMIT;

BEGIN;
CREATE TEMP TABLE after_run AS SELECT pg_temp.entries() AS entries;
SELECT pg_temp.check_x();
SELECT cmp_ok(pg_temp.entries() - (SELECT entries FROM after_run), '<=', 10::BIGINT,
    'the next check reads the pair after its mark and nothing more');
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
