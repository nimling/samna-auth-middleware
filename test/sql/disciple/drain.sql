BEGIN;
SELECT plan(10);

CREATE FUNCTION pg_temp.pairs(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.user_object
    WHERE user_id = p_user AND object_type = 'claimius.organization'
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys   UUID := '00000000-0000-4000-8000-0000000000f0';
    v_root  UUID := '10000000-0000-4000-8000-0000000000f0';
    v_crew  UUID;
    v_user  UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();

    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                                  'a0000000-0000-4000-8000-0000000000f3']::UUID[] LOOP
        PERFORM claimius.upsert_app_user(p_user_id => v_user, p_first_name => 'Drain');
    END LOOP;

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Drain root', 'company', v_root, v_root, 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-0000000000f1', 'Drain child', 'company', v_root, v_root, 1, v_sys);

    v_crew := (claimius.insert_claim(
        p_name => 'Drain crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                                  'a0000000-0000-4000-8000-0000000000f3']::UUID[] LOOP
        PERFORM claimius.insert_user_claim(p_claim_id => v_crew, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    END LOOP;
END $$;

CREATE TEMP TABLE raised AS
SELECT raise_count FROM claimius.access_diff WHERE kind = 'claim' AND user_id IS NULL
    AND claim_id = (SELECT id FROM claimius.claim WHERE name = 'Drain crew');
SELECT is((SELECT count(*) FROM raised), 1::BIGINT, 'the claim and its binding open one area however often they raise it');
SELECT claimius._raise_claim_diff((SELECT id FROM claimius.claim WHERE name = 'Drain crew'));
SELECT claimius._raise_claim_diff((SELECT id FROM claimius.claim WHERE name = 'Drain crew'));
SELECT is(
    (SELECT raise_count FROM claimius.access_diff WHERE kind = 'claim' AND user_id IS NULL
        AND claim_id = (SELECT id FROM claimius.claim WHERE name = 'Drain crew')),
    (SELECT raise_count + 2 FROM raised),
    'every raise of an open area counts'
);

UPDATE claimius.access_diff SET sa_created_at = now() - INTERVAL '1 hour';
INSERT INTO claimius.user_activity (user_id, last_used_at) VALUES
    ('a0000000-0000-4000-8000-0000000000f1', now() - INTERVAL '1 minute'),
    ('a0000000-0000-4000-8000-0000000000f2', now() - INTERVAL '1 day');

SELECT is(claimius.drain_access_diff(0), 0, 'a drain of no pairs settles nothing');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f1'), 0::BIGINT, 'no holder is settled before the drain');

SELECT cmp_ok(claimius.drain_access_diff(), '>', 0, 'the drain settles shares');

SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f1'), 2::BIGINT,
    'a holder who reads more often than the area is raised is settled ahead of the read');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f2'), 0::BIGINT,
    'a holder who reads less often than the area is raised waits for the read');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f3'), 0::BIGINT,
    'a holder with no activity waits for the read');
SELECT is_empty(
    $$SELECT 1 FROM claimius.access_diff_user WHERE user_id IN ('a0000000-0000-4000-8000-0000000000f2', 'a0000000-0000-4000-8000-0000000000f3')$$,
    'the shares left behind are not recorded as settled');

SELECT is(
    (SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000f2', 'claimius.organization')),
    2::BIGINT,
    'a holder the drain left settles its own share on its read');

SELECT * FROM finish();
ROLLBACK;
