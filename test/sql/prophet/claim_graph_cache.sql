CREATE EXTENSION IF NOT EXISTS dblink;
SELECT plan(6);
CREATE TEMP TABLE started AS SELECT pg_current_xact_id()::TEXT::BIGINT AS txid;
SELECT dblink_connect('graph_reader', 'dbname=' || current_database() || ' user=' || current_user);
SELECT dblink_connect('graph_writer', 'dbname=' || current_database() || ' user=' || current_user);
SELECT dblink_exec('graph_reader', $$SET lock_timeout = '10s'$$);

CREATE FUNCTION pg_temp.read() RETURNS TABLE(reason TEXT, at TIMESTAMPTZ) AS $$
    SELECT * FROM dblink('graph_reader', $read$
        SELECT coalesce((SELECT g.data ->> 'reason'
                         FROM claimius.get_claim_graph('a0000000-0000-4000-8000-0000000000c1', (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_graph')) g
                         WHERE g.object_type = 'claimius.user_claim' AND g.label = 'Graph crew' AND g.data ? 'reason'), 'none'),
               now()
    $read$) AS t(reason TEXT, at TIMESTAMPTZ)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.leftover() RETURNS BIGINT AS $$
DECLARE
    v_table TEXT;
    v_rows  BIGINT;
    v_total BIGINT := 0;
BEGIN
    FOR v_table IN
        SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'claimius' AND c.relkind IN ('r', 'p') AND NOT c.relispartition AND c.relname <> 'table_info'
    LOOP
        EXECUTE format('SELECT count(*) FROM claimius.%I WHERE xmin::TEXT::BIGINT >= $1', v_table) INTO v_rows USING (SELECT txid FROM started);
        v_total := v_total + v_rows;
    END LOOP;
    RETURN v_total;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION pg_temp.snapshot() RETURNS TEXT AS $$
    SELECT snapshot::TEXT FROM claimius.claim_graph_cache WHERE user_id = 'a0000000-0000-4000-8000-0000000000c1'
$$ LANGUAGE sql;

DO $$
DECLARE
    v_app   UUID;
    v_sys   UUID := '00000000-0000-4000-8000-0000000000c0';
    v_root  UUID := '10000000-0000-4000-8000-0000000000c0';
    v_crew  UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_graph',
        p_system_app_name         => 'SQL graph cache test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-0000000000c1', p_app_id => v_app, p_first_name => 'Graph');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Graph root', 'customer', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);

    v_crew := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Graph crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-0000000000c1',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);
END $$;
COMMIT;

SELECT reason FROM pg_temp.read();
CREATE TEMP TABLE built AS SELECT pg_temp.snapshot() AS snapshot;
SELECT reason FROM pg_temp.read();

SELECT is(pg_temp.snapshot(), (SELECT snapshot FROM built),
    'a graph whose grants, claims and bindings all predate its snapshot is served from the cache');
COMMIT;

SELECT dblink_exec('graph_writer', 'BEGIN');
SELECT dblink_exec('graph_writer', $$UPDATE claimius.user_claim SET reason = 'Rotated' WHERE user_id = 'a0000000-0000-4000-8000-0000000000c1' AND claim_id = (SELECT id FROM claimius.claim WHERE name = 'Graph crew')$$);
DELETE FROM claimius.claim_graph_cache WHERE user_id = 'a0000000-0000-4000-8000-0000000000c1';
COMMIT;

CREATE TEMP TABLE open_build AS SELECT * FROM pg_temp.read();
SELECT is((SELECT reason FROM open_build), 'none',
    'a graph built while the grant is being rewritten reads it as it was');
SELECT isnt(pg_temp.snapshot(), NULL, 'the graph built while the grant is being rewritten is cached');
COMMIT;

SELECT dblink_exec('graph_writer', 'COMMIT');

SELECT ok(
    (SELECT sa_updated_at FROM claimius.user_claim WHERE user_id = 'a0000000-0000-4000-8000-0000000000c1' AND claim_id = (SELECT id FROM claimius.claim WHERE name = 'Graph crew')) < (SELECT at FROM open_build),
    'the rewrite is stamped before the graph was built, so no timestamp comparison can see it');

SELECT is((SELECT reason FROM pg_temp.read()), 'Rotated',
    'the rewrite of the grant that began before the build and committed after it makes the cached graph stale');
COMMIT;

SELECT dblink_disconnect('graph_reader');
SELECT dblink_disconnect('graph_writer');
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

SELECT is(pg_temp.leftover(), 0::BIGINT, 'the test leaves no row behind');
COMMIT;
DROP EXTENSION dblink;

SELECT * FROM finish();
COMMIT;
DISCARD TEMP;
