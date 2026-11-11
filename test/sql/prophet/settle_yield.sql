CREATE EXTENSION IF NOT EXISTS dblink;
SELECT plan(8);
CREATE TEMP TABLE started AS SELECT pg_current_xact_id()::TEXT::BIGINT AS txid;
SELECT dblink_connect('yield_writer', 'dbname=' || current_database() || ' user=' || current_user);

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_yield'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.unsettled() RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.access_diff d
    WHERE d.app_id = pg_temp.app()
      AND d.kind = 'object' AND d.object_id = '10000000-0000-4000-8000-000000000e90'
      AND d.txid >= coalesce((SELECT a.settled_txid FROM claimius.user_activity a
                              WHERE a.user_id = 'a0000000-0000-4000-8000-000000000e91' AND a.app_id = pg_temp.app()), '0'::XID8)
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys  UUID := '00000000-0000-4000-8000-000000000e90';
    v_root UUID := '10000000-0000-4000-8000-000000000e90';
    v_app  UUID;
    v_crew UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_yield',
        p_system_app_name         => 'SQL settle yield test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000e91', p_app_id => v_app, p_first_name => 'Reader');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Yield root', 'customer', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    v_crew := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Yield crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-000000000e91',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000000e91', v_app, 'claimius.organization');
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
END $$;
COMMIT;

SELECT dblink_exec('yield_writer', 'BEGIN');
SELECT dblink_exec('yield_writer', format($w$INSERT INTO claimius.user_object (app_id, user_id, object_type, object_id, sa_access) VALUES (%L, 'a0000000-0000-4000-8000-000000000e91', 'claimius.organization', '10000000-0000-4000-8000-000000000e90', 4)$w$, pg_temp.app()));
BEGIN;
SET LOCAL lock_timeout = '10s';
CREATE TEMP TABLE waited_from AS SELECT clock_timestamp() AS at;

SELECT lives_ok($$SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000000e91', pg_temp.app(), 'claimius.organization')$$,
    'the reader lists while a writer acting as the reader holds the pair its share would write');

SELECT cmp_ok(clock_timestamp() - (SELECT at FROM waited_from), '<', INTERVAL '2 seconds',
    'the reader gives the wait up well before deadlock_timeout instead of waiting for the writer');

SELECT is(pg_temp.unsettled(), 1::BIGINT, 'the area stays open for the reader');

SELECT is(current_setting('lock_timeout'), '10s', 'the caller''s lock_timeout is back after the read');
COMMIT;

SELECT dblink_exec('yield_writer', 'ROLLBACK');

SELECT lives_ok($$SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000000e91', pg_temp.app(), 'claimius.organization')$$,
    'the reader lists again once the writer let the pair go');

SELECT is(pg_temp.unsettled(), 0::BIGINT, 'and settles its share of the area');
COMMIT;

SELECT dblink_exec('yield_writer', 'BEGIN');
SELECT dblink_exec('yield_writer', 'LOCK TABLE claimius.access_diff IN ACCESS EXCLUSIVE MODE');
BEGIN;
SET LOCAL lock_timeout = '10s';
CREATE TEMP TABLE held_from AS SELECT clock_timestamp() AS at;

SELECT lives_ok($$SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000000e91', pg_temp.app(), 'claimius.organization')$$,
    'the reader lists while another transaction holds the open areas themselves');

SELECT cmp_ok(clock_timestamp() - (SELECT at FROM held_from), '<', INTERVAL '2 seconds',
    'and gives up choosing areas instead of failing the read');
COMMIT;

SELECT dblink_exec('yield_writer', 'COMMIT');

SELECT dblink_disconnect('yield_writer');
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
