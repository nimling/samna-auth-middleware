SELECT plan(12);
CREATE TEMP TABLE started AS SELECT pg_current_xact_id()::TEXT::BIGINT AS txid;

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_trim'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.cursor_of(p_user UUID) RETURNS XID8 AS $$
    SELECT coalesce((SELECT a.settled_txid FROM claimius.user_activity a WHERE a.user_id = p_user AND a.app_id = pg_temp.app()), '0'::XID8)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.low_cursor() RETURNS XID8 AS $$
    SELECT min(a.settled_txid) FROM claimius.user_activity a
    WHERE a.app_id = pg_temp.app() AND a.last_used_at > now() - INTERVAL '7 days'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.trimmed() RETURNS XID8 AS $$
    SELECT ps.access_trimmed_txid FROM claimius.prophet_state ps WHERE ps.app_id = pg_temp.app()
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
    v_sys   UUID := '00000000-0000-4000-8000-000000d5a000';
    v_root  UUID := '10000000-0000-4000-8000-000000d5a000';
    v_app   UUID;
    v_crew  UUID;
    v_user  UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_trim',
        p_system_app_name         => 'SQL trim test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-000000d5a001', 'a0000000-0000-4000-8000-000000d5a002']::UUID[] LOOP
        PERFORM claimius.upsert_app_user(p_user_id => v_user, p_app_id => v_app, p_first_name => 'Reader');
    END LOOP;
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Trim root', 'customer', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    SELECT md5('trim org ' || n)::UUID, 'Trim org ' || n, 'customer', v_root, v_root, 1, v_sys
    FROM generate_series(1, 3) n;
    v_crew := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Trim crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-000000d5a001', 'a0000000-0000-4000-8000-000000d5a002']::UUID[] LOOP
        PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    END LOOP;
END $$;
COMMIT;

BEGIN;
SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000d5a001') + pg_temp.listed('a0000000-0000-4000-8000-000000d5a002'), 8::BIGINT,
    'both readers list the root and its three organizations');
COMMIT;

UPDATE claimius.user_activity SET last_used_at = now() - INTERVAL '1 day'
WHERE user_id = 'a0000000-0000-4000-8000-000000d5a002' AND app_id = pg_temp.app();
COMMIT;

INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
VALUES ('10000000-0000-4000-8000-000000d5a010', 'Trim late', 'customer',
        '10000000-0000-4000-8000-000000d5a000', '10000000-0000-4000-8000-000000d5a000', 1, '00000000-0000-4000-8000-000000d5a000');
COMMIT;

BEGIN;
SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000d5a001'), 5::BIGINT, 'the first reader lists the late organization');
COMMIT;

BEGIN;
SELECT ok(EXISTS (SELECT 1 FROM claimius.access_diff d WHERE d.app_id = pg_temp.app() AND d.txid < pg_temp.low_cursor()),
    'before the trim areas below the oldest active cursor are still stored');
SELECT is(pg_temp.trimmed(), '0'::XID8, 'and nothing has been trimmed');
COMMIT;

SELECT claimius.trim_access_diff();
COMMIT;

BEGIN;
SELECT is_empty($$SELECT 1 FROM claimius.access_diff d WHERE d.app_id = pg_temp.app() AND d.txid < pg_temp.low_cursor()$$,
    'the trim removes every area below the oldest cursor among the users used within 7 days');
SELECT is(pg_temp.trimmed(), pg_temp.low_cursor(), 'and records that cursor as the trimmed position');
SELECT ok(EXISTS (SELECT 1 FROM claimius.access_diff d WHERE d.app_id = pg_temp.app() AND d.object_id = '10000000-0000-4000-8000-000000d5a010'),
    'an area above that cursor stays');
COMMIT;

UPDATE claimius.user_activity SET last_used_at = now() - INTERVAL '8 days'
WHERE user_id = 'a0000000-0000-4000-8000-000000d5a002' AND app_id = pg_temp.app();
COMMIT;

DO $$
BEGIN
    PERFORM claimius.delete_user_claim(
        (SELECT uc.id FROM claimius.user_claim uc JOIN claimius.claim c ON c.id = uc.claim_id
          WHERE c.name = 'Trim crew' AND uc.user_id = 'a0000000-0000-4000-8000-000000d5a002' AND uc.sa_deleted_at IS NULL),
        '00000000-0000-4000-8000-000000d5a000');
END $$;
COMMIT;

INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
VALUES ('10000000-0000-4000-8000-000000d5a011', 'Trim later', 'customer',
        '10000000-0000-4000-8000-000000d5a000', '10000000-0000-4000-8000-000000d5a000', 1, '00000000-0000-4000-8000-000000d5a000');
COMMIT;

BEGIN;
SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000d5a001'), 6::BIGINT, 'the active reader lists the later organization too');
COMMIT;

SELECT claimius.trim_access_diff();
COMMIT;

BEGIN;
SELECT is_empty($$SELECT 1 FROM claimius.access_diff d WHERE d.app_id = pg_temp.app() AND d.kind = 'assignment' AND d.user_id = 'a0000000-0000-4000-8000-000000d5a002'$$,
    'the revoke''s assignment area of the absent reader is trimmed once the active cursors pass it');
SELECT ok(pg_temp.cursor_of('a0000000-0000-4000-8000-000000d5a002') < pg_temp.trimmed(),
    'the absent reader''s cursor is below the trimmed position');
COMMIT;

BEGIN;
SELECT is(pg_temp.listed('a0000000-0000-4000-8000-000000d5a002'), 0::BIGINT,
    'a reader absent longer than the window resettles its reach and sees the revoke that happened while it was away');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-000000d5a002'), 0::BIGINT, 'and holds no pair the revoke took away');
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
