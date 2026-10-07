CREATE EXTENSION IF NOT EXISTS dblink;
SELECT plan(9);
CREATE TEMP TABLE started AS SELECT pg_current_xact_id()::TEXT::BIGINT AS txid;
SELECT dblink_connect('cursor_writer', 'dbname=' || current_database() || ' user=' || current_user);

CREATE FUNCTION pg_temp.entries() RETURNS BIGINT AS $$
    SELECT sum(pg_stat_get_xact_tuples_returned(i.indexrelid))::BIGINT
    FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'claimius'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.cursor_of(p_user UUID) RETURNS XID8 AS $$
    SELECT coalesce((SELECT a.settled_txid FROM claimius.user_activity a WHERE a.user_id = p_user), '0'::XID8)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.pairs(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.user_object
    WHERE user_id = p_user AND object_type = 'claimius.organization'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.listed(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.get_objects(p_user, 'claimius.organization')
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-000000d0a000';
    v_reader UUID := 'a0000000-0000-4000-8000-000000d0a001';
    v_root   UUID := '10000000-0000-4000-8000-000000d0a000';
    v_node   UUID := '10000000-0000-4000-8000-000000d0a001';
    v_crew   UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();
    PERFORM claimius.upsert_app_user(p_user_id => v_reader, p_first_name => 'Reader');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Cursor root', 'company', v_root, v_root, 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_node, 'Cursor node', 'company', v_root, v_root, 1, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    SELECT md5('cursor org ' || n)::UUID, 'Cursor org ' || n, 'company', v_node, v_root, 2, v_sys
    FROM generate_series(1, 1000) n;
    v_crew := (claimius.insert_claim(
        p_name => 'Cursor crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_node, p_object_type => 'claimius.organization', p_inherits => TRUE);
END $$;
COMMIT;

BEGIN;
SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000d0a001'), 0::BIGINT, 'before the grant the reader lists nothing');
COMMIT;

DO $$
BEGIN
    PERFORM claimius.insert_user_claim(p_claim_id => (SELECT id FROM claimius.claim WHERE name = 'Cursor crew'),
        p_user_id => 'a0000000-0000-4000-8000-000000d0a001', p_sa_owner_id => '10000000-0000-4000-8000-000000d0a000',
        p_sa_created_by => '00000000-0000-4000-8000-000000d0a000');
END $$;
COMMIT;

BEGIN;
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-000000d0a001'), 0::BIGINT, 'the grant settles no pair of the grantee');
SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000d0a001'), 1001::BIGINT,
    'the first listing after the grant returns the node and its 1,000 organizations');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-000000d0a001'), 1001::BIGINT, 'and settles all 1,001 pairs');
SELECT is(pg_temp.cursor_of('a0000000-0000-4000-8000-000000d0a001'), pg_snapshot_xmin(pg_current_snapshot()),
    'the listing moves the reader''s cursor to the snapshot xmin');
COMMIT;

SELECT dblink_exec('cursor_writer', 'BEGIN');
SELECT dblink_exec('cursor_writer', $w$
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-000000d0a002', 'Cursor late', 'company',
            '10000000-0000-4000-8000-000000d0a001', '10000000-0000-4000-8000-000000d0a000', 2,
            '00000000-0000-4000-8000-000000d0a000')
$w$);

BEGIN;
CREATE TEMP TABLE second_run AS SELECT pg_temp.entries() AS entries;
CREATE TEMP TABLE second_rows AS SELECT pg_temp.listed('a0000000-0000-4000-8000-000000d0a001') AS n;
SELECT is((SELECT n FROM second_rows), 1001::BIGINT, 'the organization of a transaction still open is not listed');
SELECT diag('the second listing read ' || (pg_temp.entries() - (SELECT entries FROM second_run)) || ' index entries for ' || (SELECT n FROM second_rows) || ' rows');
SELECT cmp_ok(pg_temp.entries() - (SELECT entries FROM second_run), '<=', (SELECT 2 * n + 8 FROM second_rows),
    'a second listing with nothing raised since the cursor reads at most 3 entries beyond its page');
COMMIT;

SELECT dblink_exec('cursor_writer', 'COMMIT');

BEGIN;
SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000d0a001'), 1002::BIGINT,
    'an area raised by a transaction still open at the previous listing is settled by the next');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-000000d0a001'), 1002::BIGINT, 'and its pair is stored');
COMMIT;

SELECT dblink_disconnect('cursor_writer');
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
DROP EXTENSION dblink;

SELECT * FROM finish();
COMMIT;
DISCARD TEMP;
