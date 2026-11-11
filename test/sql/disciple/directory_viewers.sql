BEGIN;
SELECT plan(11);

CREATE FUNCTION pg_temp.rebuilt(p_viewer UUID) RETURNS TABLE (target_user_id UUID, sharing BIGINT) AS $$
    SELECT other.user_id, count(*)
    FROM claimius.user_object mine
        JOIN claimius.user_object other
            ON other.object_type = mine.object_type
           AND other.object_id = mine.object_id
           AND other.user_id <> mine.user_id
    WHERE mine.user_id = p_viewer
      AND mine.object_type <> 'claimius.claim'
    GROUP BY other.user_id
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.stored(p_viewer UUID) RETURNS TABLE (target_user_id UUID, sharing BIGINT) AS $$
    SELECT uu.target_user_id, uu.sharing_object_count::BIGINT
    FROM claimius.user_users uu
    WHERE uu.viewer_id = p_viewer
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.viewed(p_viewer UUID) RETURNS TABLE (target_user_id UUID, sharing BIGINT) AS $$
    SELECT v.target_user_id, v.sharing_object_count::BIGINT
    FROM claimius._user_users_for_viewer(p_viewer) v
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.seen(p_viewer UUID) RETURNS SETOF UUID AS $$
    SELECT u.user_id FROM claimius.get_users(p_viewer) u
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys   UUID := '00000000-0000-4000-8000-000000d6a000';
    v_root  UUID := '10000000-0000-4000-8000-000000d6a000';
    v_desk  UUID := '10000000-0000-4000-8000-000000d6a001';
    v_crew  UUID;
    v_user  UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-000000d6a001', 'a0000000-0000-4000-8000-000000d6a002',
                                  'a0000000-0000-4000-8000-000000d6a003']::UUID[] LOOP
        PERFORM claimius.upsert_app_user(p_user_id => v_user, p_first_name => 'Sharer');
    END LOOP;
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Viewers root', 'customer', v_root, v_root, 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_desk, 'Viewers desk', 'customer', v_root, v_root, 1, v_sys);
    v_crew := (claimius.insert_claim(
        p_name => 'Viewers crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-000000d6a001', 'a0000000-0000-4000-8000-000000d6a002',
                                  'a0000000-0000-4000-8000-000000d6a003']::UUID[] LOOP
        PERFORM claimius.insert_user_claim(p_claim_id => v_crew, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
        PERFORM count(*) FROM claimius.get_objects(v_user, 'claimius.organization');
    END LOOP;
    PERFORM claimius._settle_user_users_diff();
END $$;

SELECT is((SELECT count(*) FROM claimius.user_object WHERE object_type = 'claimius.organization'), 6::BIGINT,
    'three holders hold a pair on the root and the desk each');
SELECT is_empty($$SELECT 1 FROM claimius.user_users $$,
    'a user who never lists users causes no user_users row, however many pairs are folded');
SELECT is_empty($$SELECT 1 FROM claimius.user_users_diff $$,
    'and the folded rows are gone');

SELECT set_eq(
    $$SELECT * FROM pg_temp.seen('a0000000-0000-4000-8000-000000d6a001')$$,
    $$VALUES ('a0000000-0000-4000-8000-000000d6a001'::UUID), ('a0000000-0000-4000-8000-000000d6a002'::UUID), ('a0000000-0000-4000-8000-000000d6a003'::UUID)$$,
    'the first get_users of a user sees the two others it shares objects with');
SELECT set_eq(
    $$SELECT * FROM pg_temp.stored('a0000000-0000-4000-8000-000000d6a001')$$,
    $$SELECT * FROM pg_temp.rebuilt('a0000000-0000-4000-8000-000000d6a001')$$,
    'its first get_users builds its user_users rows equal to the self join over the pairs');
SELECT is_empty($$SELECT 1 FROM claimius.user_users WHERE viewer_id <> 'a0000000-0000-4000-8000-000000d6a001'$$,
    'and builds rows for no other user');
SELECT isnt((SELECT directory_at FROM claimius.user_activity WHERE user_id = 'a0000000-0000-4000-8000-000000d6a001'), NULL,
    'the user is marked as a viewer');

INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
VALUES ('10000000-0000-4000-8000-000000d6a002', 'Viewers late', 'customer', '10000000-0000-4000-8000-000000d6a000',
        '10000000-0000-4000-8000-000000d6a000', 1, '00000000-0000-4000-8000-000000d6a000');
SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000d6a001', 'claimius.organization');
SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000d6a002', 'claimius.organization');

SELECT cmp_ok((SELECT count(*) FROM claimius.user_users_diff), '>', 0::BIGINT,
    'pairs entering while the app has a viewer raise user_users_diff rows');

SELECT claimius._settle_user_users_diff();

SELECT set_eq(
    $$SELECT * FROM pg_temp.stored('a0000000-0000-4000-8000-000000d6a001')$$,
    $$SELECT * FROM pg_temp.rebuilt('a0000000-0000-4000-8000-000000d6a001')$$,
    'the fold keeps the viewer''s rows equal to the self join');
SELECT is_empty($$SELECT 1 FROM claimius.user_users WHERE viewer_id <> 'a0000000-0000-4000-8000-000000d6a001'$$,
    'and still writes rows for no other user');

SELECT count(*) FROM claimius.get_users('a0000000-0000-4000-8000-000000d6a002');

SELECT set_eq(
    $$SELECT * FROM pg_temp.viewed('a0000000-0000-4000-8000-000000d6a002')$$,
    $$SELECT * FROM pg_temp.rebuilt('a0000000-0000-4000-8000-000000d6a002')$$,
    'a second user''s first get_users builds its own rows equal to the self join');

SELECT * FROM finish();
ROLLBACK;
