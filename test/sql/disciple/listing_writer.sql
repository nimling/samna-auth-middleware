CREATE EXTENSION IF NOT EXISTS dblink;
SELECT plan(12);
CREATE TEMP TABLE started AS SELECT pg_current_xact_id()::TEXT::BIGINT AS txid;
SELECT dblink_connect('listing_writer', 'dbname=' || current_database() || ' user=' || current_user);

CREATE FUNCTION pg_temp.unsettled() RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.access_diff d
    WHERE d.kind = 'object' AND d.object_type = 'claimius.organization'
      AND NOT EXISTS (
          SELECT 1 FROM claimius.access_diff_user du
          WHERE du.diff_id = d.id AND du.user_id = 'a0000000-0000-4000-8000-000000000fa1' AND du.raise_count = d.raise_count)
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
        SELECT claimius._lock_objects(ARRAY['public.writer:' || gen_random_uuid()]);
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

CREATE FUNCTION pg_temp.covering() RETURNS BIGINT AS $$
    SELECT count(*) FROM pg_locks
    WHERE pid = pg_backend_pid() AND locktype = 'advisory' AND objsubid = 2 AND mode = 'ExclusiveLock'
$$ LANGUAGE sql;

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
    PERFORM claimius._raise_object_diff('claimius.organization', gen_random_uuid(), TRUE)
    FROM generate_series(1, 600);
END $$;
COMMIT;
SELECT pg_temp.write();

BEGIN;
SELECT cmp_ok(pg_temp.unsettled(), '>', (claimius._object_lock_budget() / 4)::BIGINT,
    'more areas are open for the reader than one batch settles');

SELECT lives_ok($$SELECT pg_temp.list()$$, 'the reader lists over every open area');

SELECT is(pg_temp.unsettled(), 0::BIGINT, 'one read settles every open area of the type it lists');

CREATE TEMP TABLE timed (step TEXT PRIMARY KEY, took INTERVAL);
INSERT INTO timed SELECT 'areas', pg_temp.write();
SELECT diag('writer beside the reader of 600 open areas took ' || coalesce((SELECT took FROM timed WHERE step = 'areas')::TEXT, 'over the 5s lock_timeout'));

SELECT cmp_ok((SELECT took FROM timed WHERE step = 'areas'), '<', INTERVAL '2 seconds',
    'a writer completes while the reader that settled them is still in its transaction');

SELECT is(pg_temp.covering(), 0::BIGINT, 'the reader never holds the lock every writer shares exclusive');
COMMIT;

BEGIN;
INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
SELECT gen_random_uuid(), 'Listing child ' || g, 'company',
       '10000000-0000-4000-8000-000000000fa0', '10000000-0000-4000-8000-000000000fa0', 1,
       '00000000-0000-4000-8000-000000000fa0'
FROM generate_series(1, 700) AS g;
COMMIT;

BEGIN;
SELECT lives_ok($$SELECT pg_temp.list()$$, 'the reader lists with 700 new pairs to settle');

INSERT INTO timed SELECT 'pairs', pg_temp.write();
SELECT diag('writer beside the reader of 700 new pairs took ' || coalesce((SELECT took FROM timed WHERE step = 'pairs')::TEXT, 'over the 5s lock_timeout'));

SELECT cmp_ok((SELECT took FROM timed WHERE step = 'pairs'), '<', INTERVAL '2 seconds',
    'a writer completes while the reader past the lock budget is still in its transaction');

SELECT is(pg_temp.covering(), 0::BIGINT, 'past the budget the reader still never holds the shared lock exclusive');

SELECT cmp_ok((SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid() AND locktype = 'advisory'),
    '<=', (claimius._object_lock_budget() + 16)::BIGINT,
    'the reader holds no more advisory locks than the budget');

SELECT cmp_ok(pg_temp.unsettled(), '>', 0::BIGINT, 'the areas past the budget stay open for the next read');
COMMIT;

BEGIN; SELECT pg_temp.list() WHERE pg_temp.unsettled() > 0; COMMIT;
BEGIN; SELECT pg_temp.list() WHERE pg_temp.unsettled() > 0; COMMIT;
BEGIN; SELECT pg_temp.list() WHERE pg_temp.unsettled() > 0; COMMIT;
BEGIN; SELECT pg_temp.list() WHERE pg_temp.unsettled() > 0; COMMIT;
BEGIN; SELECT pg_temp.list() WHERE pg_temp.unsettled() > 0; COMMIT;
BEGIN; SELECT pg_temp.list() WHERE pg_temp.unsettled() > 0; COMMIT;
BEGIN; SELECT pg_temp.list() WHERE pg_temp.unsettled() > 0; COMMIT;
BEGIN; SELECT pg_temp.list() WHERE pg_temp.unsettled() > 0; COMMIT;
SELECT is(pg_temp.list(),
    (SELECT count(*) FROM claimius.organization WHERE sa_root_id = '10000000-0000-4000-8000-000000000fa0' AND sa_deleted_at IS NULL),
    'later reads settle the rest and the reader lists every organization it reaches');

SELECT is_empty($$
    SELECT a.user_id, b.user_id, count(DISTINCT a.object_id)
    FROM claimius.user_object a
        JOIN claimius.user_object b
            ON b.object_type = a.object_type AND b.object_id = a.object_id AND b.user_id <> a.user_id
    WHERE a.object_type <> 'claimius.claim'
    GROUP BY a.user_id, b.user_id
    EXCEPT
    SELECT viewer_id, target_user_id, sharing_object_count::BIGINT FROM claimius.user_users
$$, 'the shared object counts match the pairs');
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
