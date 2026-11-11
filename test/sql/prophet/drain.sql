BEGIN;
SELECT plan(14);

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_drain'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.served() RETURNS UUID AS $$
    SELECT '50000000-0000-4000-8000-0000000000f5'::UUID
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.pairs(p_user UUID, p_app UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.user_object
    WHERE user_id = p_user AND app_id = p_app AND object_type = 'claimius.organization'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.cursor_of(p_user UUID, p_app UUID) RETURNS XID8 AS $$
    SELECT coalesce((SELECT a.settled_txid FROM claimius.user_activity a WHERE a.user_id = p_user AND a.app_id = p_app), '0'::XID8)
$$ LANGUAGE sql;

DO $$
DECLARE
    v_app   UUID;
    v_sys   UUID := '00000000-0000-4000-8000-0000000000f0';
    v_root  UUID := '10000000-0000-4000-8000-0000000000f0';
    v_crew  UUID;
    v_user  UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_drain',
        p_system_app_name         => 'SQL drain test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    INSERT INTO claimius.samna_app (id, slug, name, private_key, private_seed, sa_owner_id, sa_created_by)
    SELECT pg_temp.served(), 'sqltest_drain_served', 'Served app', 'key', 'seed', sa_owner_id, sa_created_by
    FROM claimius.samna_app WHERE id = v_app;

    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                                  'a0000000-0000-4000-8000-0000000000f3']::UUID[] LOOP
        PERFORM claimius.upsert_app_user(p_user_id => v_user, p_app_id => v_app, p_first_name => 'Drain');
    END LOOP;
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-0000000000f4', p_app_id => pg_temp.served(), p_first_name => 'Served');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Drain root', 'customer', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    PERFORM claimius.upsert_app_object(pg_temp.served(), 'claimius.organization', v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-0000000000f1', 'Drain child', 'customer', v_root, v_root, 1, v_sys);

    v_crew := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Drain crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                                  'a0000000-0000-4000-8000-0000000000f3']::UUID[] LOOP
        PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    END LOOP;

    v_crew := (claimius.insert_claim(
        p_app_id => pg_temp.served(), p_name => 'Served crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => pg_temp.served(), p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => pg_temp.served(), p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-0000000000f4',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);
END $$;

SELECT claimius.update_sync_cursor(pg_temp.served(), '70000000-0000-4000-8000-0000000000f1', 0, 0);

SELECT is(
    (SELECT count(*) FROM claimius.access_diff WHERE app_id = pg_temp.app() AND kind = 'assignment'
       AND user_id IN ('a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2', 'a0000000-0000-4000-8000-0000000000f3')
       AND claim_id = (SELECT id FROM claimius.claim WHERE name = 'Drain crew')),
    3::BIGINT, 'three grants raise three assignment areas, one per assignee');

UPDATE claimius.access_diff SET sa_created_at = now() - INTERVAL '1 hour';
INSERT INTO claimius.user_activity (user_id, app_id, last_used_at) VALUES
    ('a0000000-0000-4000-8000-0000000000f1', pg_temp.app(), now() - INTERVAL '1 minute'),
    ('a0000000-0000-4000-8000-0000000000f2', pg_temp.app(), now() - INTERVAL '1 day'),
    ('a0000000-0000-4000-8000-0000000000f4', pg_temp.served(), now() - INTERVAL '1 minute');

SELECT is((SELECT settled FROM claimius.drain_access_diff(0)), 0, 'a drain of no pairs settles nothing');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f1', pg_temp.app()), 0::BIGINT, 'no holder is settled before the drain');

SELECT cmp_ok((SELECT settled FROM claimius.drain_access_diff()), '>', 0, 'the drain settles shares');

SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f1', pg_temp.app()), 2::BIGINT,
    'a holder who reads more often than areas are raised is settled ahead of the read');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f2', pg_temp.app()), 0::BIGINT,
    'a holder who reads less often than areas are raised waits for the read');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f3', pg_temp.app()), 0::BIGINT,
    'a holder with no activity waits for the read');
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f4', pg_temp.served()), 0::BIGINT,
    'an app a disciple is in contact with is drained there, not here');
SELECT ok(pg_temp.cursor_of('a0000000-0000-4000-8000-0000000000f1', pg_temp.app()) > '0'::XID8
       AND pg_temp.cursor_of('a0000000-0000-4000-8000-0000000000f2', pg_temp.app()) = '0'::XID8,
    'the share settled moves its holder''s cursor and leaves the cursors of the shares left behind');

UPDATE claimius.sync_consumer SET last_contact_at = now() - INTERVAL '8 days' WHERE app_id = pg_temp.served();
SELECT * FROM claimius.drain_access_diff();
SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000f4', pg_temp.served()), 2::BIGINT,
    'an app no disciple has contacted for 7 days is drained by the prophet');

SELECT count(*) FROM claimius.get_users('a0000000-0000-4000-8000-0000000000f1', pg_temp.app());

SELECT is(
    (SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000f2', pg_temp.app(), 'claimius.organization')),
    2::BIGINT,
    'a holder the drain left settles its own share on its read');

SELECT cmp_ok((SELECT count(*) FROM claimius.user_users_diff WHERE app_id = pg_temp.app()), '>', 0::BIGINT,
    'the pairs a listing settled left user_users_diff rows, which a listing does not fold');

SELECT * FROM claimius.drain_access_diff();

SELECT is_empty($$SELECT 1 FROM claimius.user_users_diff WHERE app_id = pg_temp.app()$$,
    'the drain folds the user_users_diff rows');

SELECT set_eq($$
    SELECT other.user_id AS target_user_id, count(*) AS sharing
    FROM claimius.user_object mine
        JOIN claimius.user_object other
            ON other.app_id = mine.app_id
           AND other.object_type = mine.object_type
           AND other.object_id = mine.object_id
           AND other.user_id <> mine.user_id
    WHERE mine.app_id = pg_temp.app()
      AND mine.user_id = 'a0000000-0000-4000-8000-0000000000f1'
      AND mine.object_type <> 'claimius.claim'
    GROUP BY other.user_id
$$, $$
    SELECT uu.target_user_id, uu.sharing_object_count::BIGINT
    FROM claimius.user_users uu
    WHERE uu.app_id = pg_temp.app() AND uu.viewer_id = 'a0000000-0000-4000-8000-0000000000f1'
$$, 'and the stored user_users of the viewer equal a count over the pairs');

SELECT * FROM finish();
ROLLBACK;
