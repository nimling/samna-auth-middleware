SELECT plan(7);
CREATE TEMP TABLE started AS SELECT pg_current_xact_id()::TEXT::BIGINT AS txid;
CREATE TEMP TABLE bench (step TEXT PRIMARY KEY, rows_out BIGINT, entries BIGINT, took INTERVAL);

CREATE FUNCTION pg_temp.on() RETURNS BOOLEAN AS $$
    SELECT current_setting('claimius.bench', TRUE) = 'on'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_bench'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.entries() RETURNS BIGINT AS $$
    SELECT sum(pg_stat_get_xact_tuples_returned(i.indexrelid))::BIGINT
    FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'claimius'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.measure(p_step TEXT, p_sql TEXT) RETURNS VOID AS $$
DECLARE
    v_entries BIGINT := pg_temp.entries();
    v_from    TIMESTAMPTZ := clock_timestamp();
    v_rows    BIGINT;
BEGIN
    EXECUTE p_sql INTO v_rows;
    INSERT INTO bench VALUES (p_step, v_rows, pg_temp.entries() - v_entries, clock_timestamp() - v_from);
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION pg_temp.org(p_n INTEGER) RETURNS UUID AS $$
    SELECT ('10000000-0000-4000-8000-' || lpad(to_hex(15000000 + p_n), 12, '0'))::UUID
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.usr(p_n INTEGER) RETURNS UUID AS $$
    SELECT ('a0000000-0000-4000-8000-' || lpad(to_hex(15000000 + p_n), 12, '0'))::UUID
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys   UUID := '00000000-0000-4000-8000-000000d9a000';
    v_app   UUID;
    v_claim UUID;
    v_n     INTEGER;
BEGIN
    IF NOT pg_temp.on() THEN
        RETURN;
    END IF;
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_bench',
        p_system_app_name         => 'SQL bench',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    PERFORM claimius.upsert_app_user(p_user_id => pg_temp.usr(1), p_app_id => v_app, p_first_name => 'Wide');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (pg_temp.org(1), 'Bench wide root', 'company', pg_temp.org(1), pg_temp.org(1), 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', pg_temp.org(1));
    v_claim := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Bench wide crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => pg_temp.org(1), p_sa_root_id => pg_temp.org(1), p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_claim, p_sa_owner_id => pg_temp.org(1), p_sa_root_id => pg_temp.org(1), p_sa_created_by => v_sys,
        p_object_id => pg_temp.org(1), p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_claim, p_user_id => pg_temp.usr(1), p_sa_owner_id => pg_temp.org(1), p_sa_created_by => v_sys);
    PERFORM count(*) FROM claimius.get_objects(pg_temp.usr(1), v_app, 'claimius.organization');

    PERFORM claimius.upsert_app_user(p_user_id => pg_temp.usr(2), p_app_id => v_app, p_first_name => 'Node');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (pg_temp.org(2), 'Bench node root', 'company', pg_temp.org(2), pg_temp.org(2), 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', pg_temp.org(2));
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (pg_temp.org(3), 'Bench node', 'company', pg_temp.org(2), pg_temp.org(2), 1, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    SELECT md5('bench node org ' || n)::UUID, 'Bench node org ' || n, 'company', pg_temp.org(3), pg_temp.org(2), 2, v_sys
    FROM generate_series(1, 10000) n;
    v_claim := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Bench node crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => pg_temp.org(2), p_sa_root_id => pg_temp.org(2), p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_claim, p_sa_owner_id => pg_temp.org(2), p_sa_root_id => pg_temp.org(2), p_sa_created_by => v_sys,
        p_object_id => pg_temp.org(3), p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM count(*) FROM claimius.get_objects(pg_temp.usr(2), v_app, 'claimius.organization');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (pg_temp.org(4), 'Bench directory root', 'company', pg_temp.org(4), pg_temp.org(4), 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', pg_temp.org(4));
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    SELECT md5('bench directory org ' || n)::UUID, 'Bench directory org ' || n, 'company', pg_temp.org(4), pg_temp.org(4), 1, v_sys
    FROM generate_series(1, 2000) n;
    v_claim := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Bench directory crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => pg_temp.org(4), p_sa_root_id => pg_temp.org(4), p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_claim, p_sa_owner_id => pg_temp.org(4), p_sa_root_id => pg_temp.org(4), p_sa_created_by => v_sys,
        p_object_id => pg_temp.org(4), p_object_type => 'claimius.organization', p_inherits => TRUE);
    FOR v_n IN 101 .. 300 LOOP
        PERFORM claimius.upsert_app_user(p_user_id => pg_temp.usr(v_n), p_app_id => v_app, p_first_name => 'Directory ' || v_n);
        PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_claim, p_user_id => pg_temp.usr(v_n), p_sa_owner_id => pg_temp.org(4), p_sa_created_by => v_sys);
    END LOOP;
END $$;
COMMIT;

DO $$
BEGIN
    IF NOT pg_temp.on() THEN
        RETURN;
    END IF;
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    SELECT md5('bench wide org ' || n)::UUID, 'Bench wide org ' || n, 'company', pg_temp.org(1), pg_temp.org(1), 1, '00000000-0000-4000-8000-000000d9a000'
    FROM generate_series(1, 16000) n;
END $$;
COMMIT;

BEGIN;
SELECT pg_temp.measure('wide first listing', format('SELECT count(*) FROM claimius.get_objects(%L, %L, %L)', pg_temp.usr(1), pg_temp.app(), 'claimius.organization'))
WHERE pg_temp.on();
COMMIT;

BEGIN;
SELECT pg_temp.measure('wide second listing', format('SELECT count(*) FROM claimius.get_objects(%L, %L, %L)', pg_temp.usr(1), pg_temp.app(), 'claimius.organization'))
WHERE pg_temp.on();
COMMIT;

DO $$
BEGIN
    IF NOT pg_temp.on() THEN
        RETURN;
    END IF;
    PERFORM claimius.insert_user_claim(p_app_id => pg_temp.app(), p_claim_id => (SELECT id FROM claimius.claim WHERE name = 'Bench node crew'),
        p_user_id => pg_temp.usr(2), p_sa_owner_id => pg_temp.org(2), p_sa_created_by => '00000000-0000-4000-8000-000000d9a000');
END $$;
COMMIT;

BEGIN;
SELECT pg_temp.measure('node grant first listing', format('SELECT count(*) FROM claimius.get_objects(%L, %L, %L)', pg_temp.usr(2), pg_temp.app(), 'claimius.organization'))
WHERE pg_temp.on();
COMMIT;

DO $$
DECLARE
    v_n INTEGER;
BEGIN
    IF NOT pg_temp.on() THEN
        RETURN;
    END IF;
    FOR v_n IN 101 .. 300 LOOP
        PERFORM count(*) FROM claimius.get_objects(pg_temp.usr(v_n), pg_temp.app(), 'claimius.organization');
    END LOOP;
END $$;
COMMIT;

BEGIN;
SELECT pg_temp.measure('get_users', format('SELECT count(*) FROM claimius.get_users(%L, %L)', pg_temp.usr(101), pg_temp.app()))
WHERE pg_temp.on();
COMMIT;

BEGIN;
SELECT pg_temp.measure('get_user_access per row', format(
    'SELECT count(*) FROM claimius.get_users(%1$L, %2$L) u CROSS JOIN LATERAL claimius.get_user_access(%1$L, %2$L, u.user_id::TEXT, 0) a',
    pg_temp.usr(101), pg_temp.app()))
WHERE pg_temp.on();
COMMIT;

SELECT diag(b.step || ': ' || b.rows_out || ' rows, ' || b.entries || ' index entries, ' || b.took) FROM bench b ORDER BY b.step;

SELECT CASE WHEN pg_temp.on() THEN is((SELECT rows_out FROM bench WHERE step = 'wide first listing'), 16001::BIGINT, 'the first listing over 16,000 new organizations lists them all')
            ELSE skip('bench off: set claimius.bench to on', 1) END;
SELECT CASE WHEN pg_temp.on() THEN cmp_ok((SELECT entries FROM bench WHERE step = 'wide first listing'), '<', 1577903::BIGINT, 'and reads fewer entries than the 1,577,903 of alpha0026')
            ELSE skip('bench off', 1) END;
SELECT CASE WHEN pg_temp.on() THEN cmp_ok((SELECT entries FROM bench WHERE step = 'wide second listing'), '<', 96082::BIGINT, 'the second listing reads fewer entries than the 96,082 of alpha0026')
            ELSE skip('bench off', 1) END;
SELECT CASE WHEN pg_temp.on() THEN cmp_ok((SELECT entries FROM bench WHERE step = 'wide second listing'), '<=', 2 * 16001 + 12::BIGINT, 'and no more than two per row beyond the cursor')
            ELSE skip('bench off', 1) END;
SELECT CASE WHEN pg_temp.on() THEN cmp_ok((SELECT entries FROM bench WHERE step = 'node grant first listing'), '<', 201374::BIGINT, 'the first listing after a grant on a node over 10,000 organizations reads fewer entries than the 201,374 of alpha0026')
            ELSE skip('bench off', 1) END;
SELECT CASE WHEN pg_temp.on() THEN is((SELECT rows_out FROM bench WHERE step = 'get_users'), 200::BIGINT, 'get_users of one of 200 users over 2,000 organizations sees the 200')
            ELSE skip('bench off', 1) END;
SELECT CASE WHEN pg_temp.on() THEN cmp_ok((SELECT entries FROM bench WHERE step = 'get_user_access per row'), '<', 437835068::BIGINT, 'get_user_access per row of the directory reads fewer entries than the 437,835,068 of alpha0024')
            ELSE skip('bench off', 1) END;

DO $$
DECLARE
    v_table TEXT;
    v_from  BIGINT := (SELECT txid FROM started);
    v_rows  BIGINT;
    v_left  BOOLEAN := TRUE;
    v_pass  INTEGER := 0;
BEGIN
    IF NOT pg_temp.on() THEN
        RETURN;
    END IF;
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
