BEGIN;
SELECT plan(5);

CREATE FUNCTION pg_temp.org(p_n INTEGER) RETURNS UUID AS $$
    SELECT ('10000000-0000-4000-8000-' || lpad(to_hex(17000000 + p_n), 12, '0'))::UUID
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.usr(p_n INTEGER) RETURNS UUID AS $$
    SELECT ('a0000000-0000-4000-8000-' || lpad(to_hex(17000000 + p_n), 12, '0'))::UUID
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.closure() RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.inheritance_info e
    WHERE e.tree_type = 'ownership' AND e.root_id = pg_temp.org(0) AND e.descendant_type = 'claimius.organization'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.pairs(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.user_object
    WHERE user_id = p_user AND object_type = 'claimius.organization'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.listed(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.get_objects(p_user, 'claimius.organization')
$$ LANGUAGE sql;

ANALYZE claimius.inheritance_info;
ANALYZE claimius.organization;
ANALYZE claimius.object_info;
ANALYZE claimius.claim_object;
ANALYZE claimius.user_claim;
ANALYZE claimius.user_object;
ANALYZE claimius.access_diff;
SET LOCAL statement_timeout = '10s';
CREATE TEMP TABLE built AS SELECT clock_timestamp() AS at;

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-000000d2a000';
    v_claim  UUID;
    v_n      INTEGER;
BEGIN
    PERFORM claimius.upsert_disciple_state();
    FOR v_n IN 1 .. 3 LOOP
        PERFORM claimius.upsert_app_user(p_user_id => pg_temp.usr(v_n), p_first_name => 'Holder ' || v_n);
    END LOOP;

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (pg_temp.org(0), 'Plan root', 'customer', pg_temp.org(0), pg_temp.org(0), 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    SELECT pg_temp.org(n), 'Plan node ' || n, 'customer', pg_temp.org(0), pg_temp.org(0), 1, v_sys
    FROM generate_series(1, 10) n;
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    SELECT md5('plan leaf ' || m || ' ' || n)::UUID, 'Plan leaf ' || m || ' ' || n, 'customer', pg_temp.org(m), pg_temp.org(0), 2, v_sys
    FROM generate_series(1, 10) m, generate_series(1, 99) n;

    v_claim := (claimius.insert_claim(
        p_name => 'Plan crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => pg_temp.org(0), p_sa_root_id => pg_temp.org(0), p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_claim, p_sa_owner_id => pg_temp.org(0), p_sa_root_id => pg_temp.org(0), p_sa_created_by => v_sys,
        p_object_id => pg_temp.org(0), p_object_type => 'claimius.organization', p_inherits => TRUE);
    FOR v_n IN 1 .. 3 LOOP
        PERFORM claimius.insert_user_claim(p_claim_id => v_claim,
            p_user_id => pg_temp.usr(v_n), p_sa_owner_id => pg_temp.org(0), p_sa_created_by => v_sys);
    END LOOP;
END $$;

SELECT cmp_ok(clock_timestamp() - (SELECT at FROM built), '<', INTERVAL '10s',
    'inserting 1,001 organizations in three levels on tables analyzed at one page completes within the statement timeout');
SELECT is(pg_temp.closure(), 2991::BIGINT, 'and leaves one closure row per node and ancestor: 1 + 10 * 2 + 990 * 3');
SELECT is(pg_temp.listed(pg_temp.usr(1)), 1001::BIGINT, 'the first holder''s first listing lists the root and its 1,000 organizations');
SELECT is(pg_temp.pairs(pg_temp.usr(1)), 1001::BIGINT, 'and writes one pair per object');
SELECT is(pg_temp.listed(pg_temp.usr(2)), 1001::BIGINT, 'the second holder lists the same share');
ANALYZE claimius.inheritance_info;
ANALYZE claimius.organization;
ANALYZE claimius.object_info;
ANALYZE claimius.claim_object;
ANALYZE claimius.user_claim;
ANALYZE claimius.user_object;
ANALYZE claimius.access_diff;
SELECT * FROM finish();
ROLLBACK;
