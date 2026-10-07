SELECT plan(8);
CREATE TEMP TABLE started AS SELECT pg_current_xact_id()::TEXT::BIGINT AS txid;

CREATE FUNCTION pg_temp.entries() RETURNS BIGINT AS $$
    SELECT sum(pg_stat_get_xact_tuples_returned(i.indexrelid))::BIGINT
    FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'claimius'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.inserted() RETURNS BIGINT AS $$
    SELECT pg_stat_get_xact_tuples_inserted('claimius.user_object'::REGCLASS)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.pairs(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.user_object
    WHERE user_id = p_user AND object_type = 'claimius.organization'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.listed(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.get_objects(p_user, 'claimius.organization')
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.org(p_n INTEGER) RETURNS UUID AS $$
    SELECT ('10000000-0000-4000-8000-' || lpad(to_hex(14000000 + p_n), 12, '0'))::UUID
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-000000d1a000';
    v_flat   UUID := 'a0000000-0000-4000-8000-000000d1a001';
    v_deep   UUID := 'a0000000-0000-4000-8000-000000d1a002';
    v_claim  UUID;
    v_n      INTEGER;
BEGIN
    PERFORM claimius.upsert_disciple_state();
    PERFORM claimius.upsert_app_user(p_user_id => v_flat, p_first_name => 'Flat');
    PERFORM claimius.upsert_app_user(p_user_id => v_deep, p_first_name => 'Deep');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (pg_temp.org(0), 'Share root', 'company', pg_temp.org(0), pg_temp.org(0), 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (pg_temp.org(1), 'Share node', 'company', pg_temp.org(0), pg_temp.org(0), 1, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    SELECT md5('share flat ' || n)::UUID, 'Share flat ' || n, 'company', pg_temp.org(1), pg_temp.org(0), 2, v_sys
    FROM generate_series(1, 1000) n;
    v_claim := (claimius.insert_claim(
        p_name => 'Share flat crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => pg_temp.org(0), p_sa_root_id => pg_temp.org(0), p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_claim, p_sa_owner_id => pg_temp.org(0), p_sa_root_id => pg_temp.org(0), p_sa_created_by => v_sys,
        p_object_id => pg_temp.org(1), p_object_type => 'claimius.organization', p_inherits => TRUE);

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (pg_temp.org(10), 'Share deep root', 'company', pg_temp.org(10), pg_temp.org(10), 0, v_sys);
    FOR v_n IN 11 .. 14 LOOP
        INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
        VALUES (pg_temp.org(v_n), 'Share deep ' || v_n, 'company', pg_temp.org(v_n - 1), pg_temp.org(10), v_n - 10, v_sys);
    END LOOP;
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    SELECT md5('share deep ' || n)::UUID, 'Share deep leaf ' || n, 'company', pg_temp.org(14), pg_temp.org(10), 5, v_sys
    FROM generate_series(1, 1000) n;
    FOR v_n IN 10 .. 14 LOOP
        v_claim := (claimius.insert_claim(
            p_name => 'Share deep crew ' || v_n, p_description => NULL, p_sa_access => 4,
            p_sa_owner_id => pg_temp.org(10), p_sa_root_id => pg_temp.org(10), p_sa_created_by => v_sys
        ) -> 'claim' ->> 'id')::UUID;
        PERFORM claimius.insert_claim_object(
            p_claim_id => v_claim, p_sa_owner_id => pg_temp.org(10), p_sa_root_id => pg_temp.org(10), p_sa_created_by => v_sys,
            p_object_id => pg_temp.org(v_n), p_object_type => 'claimius.organization', p_inherits => TRUE);
    END LOOP;
END $$;
COMMIT;

DO $$
DECLARE
    v_claim UUID;
BEGIN
    PERFORM claimius.insert_user_claim(p_claim_id => (SELECT id FROM claimius.claim WHERE name = 'Share flat crew'),
        p_user_id => 'a0000000-0000-4000-8000-000000d1a001', p_sa_owner_id => pg_temp.org(0), p_sa_created_by => '00000000-0000-4000-8000-000000d1a000');
    FOR v_claim IN SELECT id FROM claimius.claim WHERE name LIKE 'Share deep crew %' LOOP
        PERFORM claimius.insert_user_claim(p_claim_id => v_claim,
            p_user_id => 'a0000000-0000-4000-8000-000000d1a002', p_sa_owner_id => pg_temp.org(10), p_sa_created_by => '00000000-0000-4000-8000-000000d1a000');
    END LOOP;
END $$;
COMMIT;

BEGIN;
CREATE TEMP TABLE flat_run AS SELECT pg_temp.entries() AS entries, pg_temp.inserted() AS inserted;
CREATE TEMP TABLE flat_rows AS SELECT pg_temp.listed('a0000000-0000-4000-8000-000000d1a001') AS n;
CREATE TEMP TABLE flat_cost AS
SELECT pg_temp.entries() - (SELECT entries FROM flat_run) AS entries, pg_temp.inserted() - (SELECT inserted FROM flat_run) AS inserted;
SELECT is((SELECT n FROM flat_rows), 1001::BIGINT, 'the first listing after the grant lists the node and its 1,000 organizations');
SELECT is((SELECT inserted FROM flat_cost), 1001::BIGINT, 'and writes one pair per object, two index entries each');
SELECT diag('the first listing over 1,001 objects at depth 1 read ' || (SELECT entries FROM flat_cost) || ' index entries');
SELECT cmp_ok((SELECT entries FROM flat_cost), '<=', 2 * 1001 + 0 + 40::BIGINT,
    'the first listing reads at most 2|T| + V_u + 40 entries: the closure entry and the object_info row of each object and the reader''s empty range');
COMMIT;

BEGIN;
CREATE TEMP TABLE deep_run AS SELECT pg_temp.entries() AS entries, pg_temp.inserted() AS inserted;
CREATE TEMP TABLE deep_rows AS SELECT pg_temp.listed('a0000000-0000-4000-8000-000000d1a002') AS n;
CREATE TEMP TABLE deep_cost AS
SELECT pg_temp.entries() - (SELECT entries FROM deep_run) AS entries, pg_temp.inserted() - (SELECT inserted FROM deep_run) AS inserted;
SELECT is((SELECT n FROM deep_rows), 1005::BIGINT, 'the reader at depth 4 lists the chain of five and the 1,000 leaves');
SELECT is((SELECT inserted FROM deep_cost), 1005::BIGINT, 'and writes one pair per object');
SELECT diag('the first listing over 1,005 objects at depth 4 under five bindings read ' || (SELECT entries FROM deep_cost) || ' index entries');
SELECT cmp_ok((SELECT entries FROM deep_cost), '<=', 2 * 1005 + 0 + 80::BIGINT,
    'the same share at depth 4 with five bindings on the chain reads at most 2|T| + V_u + 80 entries');
SELECT cmp_ok((SELECT entries FROM deep_cost) - (SELECT entries FROM flat_cost), '<=', 80::BIGINT,
    'depth and the bindings on the chain add a fixed cost, nothing per object');
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
