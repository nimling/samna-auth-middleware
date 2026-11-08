BEGIN;
SELECT plan(12);

CREATE FUNCTION pg_temp.pairs(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.user_object
    WHERE user_id = p_user AND object_type = 'claimius.organization'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.cursor_of(p_user UUID) RETURNS XID8 AS $$
    SELECT coalesce((SELECT a.settled_txid FROM claimius.user_activity a WHERE a.user_id = p_user), '0'::XID8)
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

SELECT is(
    (SELECT count(*) FROM claimius.access_diff WHERE kind = 'assignment'),
    3::BIGINT, 'three grants raise three assignment areas, one per assignee');

UPDATE claimius.access_diff SET sa_created_at = now() - INTERVAL '1 hour';
INSERT INTO claimius.user_activity (user_id, last_used_at) VALUES
    ('a0000000-0000-4000-8000-0000000000f1', now() - INTERVAL '1 minute'),
    ('a0000000-0000-4000-8000-0000000000f2', now() - INTERVAL '1 day');

SELECT is(claimius.drain_access_diff(0), 0, 'a drain of no pairs settles nothing');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f1'), 0::BIGINT, 'no holder is settled before the drain');

SELECT cmp_ok(claimius.drain_access_diff(), '>', 0, 'the drain settles shares');

SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f1'), 2::BIGINT,
    'a holder who reads more often than areas are raised is settled ahead of the read');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f2'), 0::BIGINT,
    'a holder who reads less often than areas are raised waits for the read');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f3'), 0::BIGINT,
    'a holder with no activity waits for the read');
SELECT ok(pg_temp.cursor_of('a0000000-0000-4000-8000-0000000000f1') > '0'::XID8
       AND pg_temp.cursor_of('a0000000-0000-4000-8000-0000000000f2') = '0'::XID8,
    'the share settled moves its holder''s cursor and leaves the cursors of the shares left behind');

SELECT count(*) FROM claimius.get_users('a0000000-0000-4000-8000-0000000000f1');

SELECT is(
    (SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000f2', 'claimius.organization')),
    2::BIGINT,
    'a holder the drain left settles its own share on its read');

SELECT cmp_ok((SELECT count(*) FROM claimius.user_users_diff), '>', 0::BIGINT,
    'the pairs a listing settled left user_users_diff rows, which a listing does not fold');

SELECT claimius.drain_access_diff();

SELECT is_empty($$SELECT 1 FROM claimius.user_users_diff$$,
    'the drain folds the user_users_diff rows');

SELECT set_eq($$
    SELECT other.user_id AS target_user_id, count(*) AS sharing
    FROM claimius.user_object mine
        JOIN claimius.user_object other
            ON other.object_type = mine.object_type
           AND other.object_id = mine.object_id
           AND other.user_id <> mine.user_id
    WHERE mine.user_id = 'a0000000-0000-4000-8000-0000000000f1'
      AND mine.object_type <> 'claimius.claim'
    GROUP BY other.user_id
$$, $$
    SELECT uu.target_user_id, uu.sharing_object_count::BIGINT
    FROM claimius.user_users uu
    WHERE uu.viewer_id = 'a0000000-0000-4000-8000-0000000000f1'
$$, 'and the stored user_users of the viewer equal a count over the pairs');

SELECT * FROM finish();
ROLLBACK;
