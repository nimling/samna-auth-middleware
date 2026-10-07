CREATE EXTENSION IF NOT EXISTS dblink;
SELECT plan(11);
CREATE TEMP TABLE started AS SELECT pg_current_xact_id()::TEXT::BIGINT AS txid;
SELECT dblink_connect('listing_writer', 'dbname=' || current_database() || ' user=' || current_user);

CREATE FUNCTION pg_temp.unsettled() RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.access_diff d
    WHERE d.kind = 'object' AND d.object_type = 'claimius.organization'
      AND d.txid >= coalesce((SELECT a.settled_txid FROM claimius.user_activity a
                              WHERE a.user_id = 'a0000000-0000-4000-8000-000000000fa1'), '0'::XID8)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.list() RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000000fa1', 'claimius.organization')
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.write() RETURNS INTERVAL AS $$
DECLARE
    v_from TIMESTAMPTZ := clock_timestamp();
BEGIN
    IF dblink_exec('listing_writer', $w$
        BEGIN;
        SET LOCAL lock_timeout = '5s';
        INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
        VALUES (gen_random_uuid(), 'Writer child', 'company',
                '10000000-0000-4000-8000-000000000fa0', '10000000-0000-4000-8000-000000000fa0', 1,
                '00000000-0000-4000-8000-000000000fa0');
        COMMIT;
    $w$, FALSE) = 'ERROR' THEN
        PERFORM dblink_exec('listing_writer', 'ROLLBACK', FALSE);
        RETURN NULL;
    END IF;
    RETURN clock_timestamp() - v_from;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION pg_temp.flush() RETURNS INTERVAL AS $$
DECLARE
    v_from TIMESTAMPTZ := clock_timestamp();
BEGIN
    IF dblink_exec('listing_writer', $w$
        BEGIN;
        SET LOCAL lock_timeout = '5s';
        DO $d$ BEGIN PERFORM claimius.touch_object_usage(
            ARRAY['a0000000-0000-4000-8000-000000000fa1']::UUID[],
            ARRAY['claimius.organization'], ARRAY['10000000-0000-4000-8000-000000000fa0']::UUID[], ARRAY[1]::BIGINT[]); END $d$;
        COMMIT;
    $w$, FALSE) = 'ERROR' THEN
        PERFORM dblink_exec('listing_writer', 'ROLLBACK', FALSE);
        RETURN NULL;
    END IF;
    RETURN clock_timestamp() - v_from;
END;
$$ LANGUAGE plpgsql;

DO $$
DECLARE
    v_sys  UUID := '00000000-0000-4000-8000-000000000fa0';
    v_root UUID := '10000000-0000-4000-8000-000000000fa0';
    v_crew UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000fa1', p_first_name => 'Reader');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Listing root', 'company', v_root, v_root, 0, v_sys);
    v_crew := (claimius.insert_claim(
        p_name => 'Listing crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-000000000fa1',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM pg_temp.list();
    PERFORM count(*) FROM claimius.get_users('a0000000-0000-4000-8000-000000000fa1');
    PERFORM claimius._raise_object_diff('claimius.organization', gen_random_uuid(), TRUE, v_root)
    FROM generate_series(1, 600);
END $$;
COMMIT;
SELECT pg_temp.write();

BEGIN;
SELECT cmp_ok(pg_temp.unsettled(), '>', 128::BIGINT,
    'more areas are open for the reader than one batch settles');

SELECT lives_ok($$SELECT pg_temp.list()$$, 'the reader lists over every open area');

SELECT is(pg_temp.unsettled(), 0::BIGINT, 'one read settles every open area of the type it lists');

CREATE TEMP TABLE timed (step TEXT PRIMARY KEY, took INTERVAL);
INSERT INTO timed SELECT 'areas', pg_temp.write();
SELECT diag('writer beside the reader of 600 open areas took ' || coalesce((SELECT took FROM timed WHERE step = 'areas')::TEXT, 'over the 5s lock_timeout'));

SELECT cmp_ok((SELECT took FROM timed WHERE step = 'areas'), '<', INTERVAL '2 seconds',
    'a writer completes while the reader that settled them is still in its transaction');

SELECT cmp_ok((SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid() AND locktype = 'advisory'), '<=', 1::BIGINT,
    'the reader holds its share lock and no lock per object');
COMMIT;

BEGIN;
INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
SELECT gen_random_uuid(), 'Listing child ' || g, 'company',
       '10000000-0000-4000-8000-000000000fa0', '10000000-0000-4000-8000-000000000fa0', 1,
       '00000000-0000-4000-8000-000000000fa0'
FROM generate_series(1, 700) AS g;
COMMIT;

BEGIN;
SELECT is(pg_temp.list(),
    (SELECT count(*) FROM claimius.organization WHERE sa_root_id = '10000000-0000-4000-8000-000000000fa0' AND sa_deleted_at IS NULL),
    'one read with 700 new pairs to settle lists every organization the reader reaches');

SELECT is(pg_temp.unsettled(), 0::BIGINT, 'the one read leaves no area open, past what a budget of 512 would move');

INSERT INTO timed SELECT 'pairs', pg_temp.write();
SELECT diag('writer beside the reader of 700 new pairs took ' || coalesce((SELECT took FROM timed WHERE step = 'pairs')::TEXT, 'over the 5s lock_timeout'));

SELECT cmp_ok((SELECT took FROM timed WHERE step = 'pairs'), '<', INTERVAL '2 seconds',
    'a writer completes while the reader that moved 700 pairs is still in its transaction');

SELECT cmp_ok((SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid() AND locktype = 'advisory'), '<=', 1::BIGINT,
    'moving 700 pairs the reader still holds its share lock and no lock per object');

CREATE TEMP TABLE held AS
SELECT uo.object_id FROM claimius.user_object uo
WHERE uo.user_id = 'a0000000-0000-4000-8000-000000000fa1' AND uo.object_type = 'claimius.organization'
  AND uo.object_id = '10000000-0000-4000-8000-000000000fa0'
FOR NO KEY UPDATE;

INSERT INTO timed SELECT 'usage', pg_temp.flush();
SELECT diag('usage flush beside the reader holding its pair took ' || coalesce((SELECT took FROM timed WHERE step = 'usage')::TEXT, 'over the 5s lock_timeout'));

SELECT cmp_ok((SELECT took FROM timed WHERE step = 'usage'), '<', INTERVAL '2 seconds',
    'a usage flush on a pair the reader holds completes while the reader is still in its transaction');
COMMIT;

SELECT set_eq($$
    SELECT a.user_id AS viewer_id, b.user_id AS target_user_id, count(DISTINCT a.object_id) AS sharing
    FROM claimius.user_object a
        JOIN claimius.user_object b
            ON b.object_type = a.object_type AND b.object_id = a.object_id AND b.user_id <> a.user_id
    WHERE a.object_type <> 'claimius.claim'
    GROUP BY a.user_id, b.user_id
$$, $$
    SELECT s.user_id, v.target_user_id, v.sharing_object_count::BIGINT
    FROM claimius.samna_user s
        CROSS JOIN LATERAL claimius._user_users_for_viewer(s.user_id) v
$$, 'the shared object counts users read match the pairs');
COMMIT;

SELECT dblink_disconnect('listing_writer');
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
