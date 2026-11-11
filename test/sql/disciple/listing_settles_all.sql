BEGIN;
SELECT plan(12);

CREATE EXTENSION IF NOT EXISTS dblink;

CREATE FUNCTION pg_temp.open() RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.access_diff
    WHERE kind = 'object' AND object_type = 'claimius.organization'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.cursor_of(p_user UUID) RETURNS XID8 AS $$
    SELECT coalesce((SELECT a.settled_txid FROM claimius.user_activity a WHERE a.user_id = p_user), '0'::XID8)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.raise(p_count INTEGER) RETURNS VOID AS $$
    SELECT claimius._raise_object_diff('claimius.organization', gen_random_uuid(), TRUE, '10000000-0000-4000-8000-000000000b90')
    FROM generate_series(1, p_count);
$$ LANGUAGE sql;

CREATE TEMP TABLE seen (step TEXT PRIMARY KEY, cursor_at XID8);

CREATE FUNCTION pg_temp.list(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.get_objects(p_user, 'claimius.organization')
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.share_key(p_user UUID) RETURNS BIGINT AS $$
    SELECT hashtextextended('claimius.share:' || p_user::TEXT, 0)
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-000000000b90';
    v_root   UUID := '10000000-0000-4000-8000-000000000b90';
    v_vault  UUID := '10000000-0000-4000-8000-000000000b9f';
    v_crew   UUID;
    v_secret UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();

    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000b91', p_first_name => 'Reader');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000b92', p_first_name => 'Other');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Budget root', 'customer', v_root, v_root, 0, v_sys),
           (v_vault, 'Budget vault', 'customer', v_vault, v_vault, 0, v_sys);

    v_crew := (claimius.insert_claim(
        p_name => 'Budget crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-000000000b91',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM claimius.insert_user_claim(p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-000000000b92',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);

    v_secret := (claimius.insert_claim(
        p_name => 'Budget vault keys', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_vault, p_sa_root_id => v_vault, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_secret, p_sa_owner_id => v_vault, p_sa_root_id => v_vault, p_sa_created_by => v_sys,
        p_object_id => v_vault, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_claim_id => v_secret, p_user_id => 'a0000000-0000-4000-8000-000000000b91',
        p_sa_owner_id => v_vault, p_sa_created_by => v_sys);
END $$;

SELECT ok(EXISTS (SELECT 1 FROM claimius.get_objects('a0000000-0000-4000-8000-000000000b91', 'claimius.organization')
                  WHERE object_id = '10000000-0000-4000-8000-000000000b9f'),
    'the reader reaches the vault through its binding');

SELECT pg_temp.raise(7000);
DO $$
BEGIN
    PERFORM claimius.delete_claim_object(co.id, '00000000-0000-4000-8000-000000000b90')
    FROM claimius.claim_object co
    WHERE co.object_id = '10000000-0000-4000-8000-000000000b9f' AND co.sa_deleted_at IS NULL;
END $$;

INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
SELECT gen_random_uuid(), 'Budget child ' || g, 'customer', '10000000-0000-4000-8000-000000000b90', '10000000-0000-4000-8000-000000000b90', 1,
       '00000000-0000-4000-8000-000000000b90'
FROM generate_series(1, 700) AS g;

SELECT cmp_ok((SELECT count(*) FROM claimius.access_diff),
    '>', (SELECT current_setting('max_locks_per_transaction')::BIGINT * current_setting('max_connections')::BIGINT),
    'more areas are open than the lock table holds on default settings, the revoke the newest of them');

SELECT lives_ok($$CREATE TEMP TABLE listed AS
        SELECT object_id FROM claimius.get_objects('a0000000-0000-4000-8000-000000000b91', 'claimius.organization')$$,
    'the reader lists with every area open and 700 new pairs to move, and no out of shared memory');

SELECT cmp_ok((SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid() AND locktype = 'advisory'),
    '<=', 16::BIGINT,
    'the reader holds its share lock and the root locks of the writes before it, no lock per object');

SELECT is_empty($$SELECT 1 FROM listed WHERE object_id = '10000000-0000-4000-8000-000000000b9f'$$,
    'the listing never returns the vault the revoke behind thousands of open areas took away');

SELECT is((SELECT count(*) FROM listed l JOIN claimius.organization o ON o.id = l.object_id WHERE o.name LIKE 'Budget child %'), 700::BIGINT,
    'the one listing returns every one of the 700 organizations it gained, past what a budget of 512 would move');

SELECT is(pg_temp.cursor_of('a0000000-0000-4000-8000-000000000b91'), pg_snapshot_xmin(pg_current_snapshot()),
    'one listing settles the reader''s share of every open area and moves its cursor to the snapshot xmin');

SELECT pg_temp.raise(600);
INSERT INTO seen VALUES ('raised', pg_temp.cursor_of('a0000000-0000-4000-8000-000000000b92'));

SELECT dblink_connect('budget_peer', 'dbname=' || current_database());
SELECT dblink_exec('budget_peer', 'BEGIN');
SELECT * FROM dblink('budget_peer', format('SELECT pg_advisory_xact_lock(%s)', pg_temp.share_key('a0000000-0000-4000-8000-000000000b92'))) AS t(held TEXT);

SELECT pg_temp.list('a0000000-0000-4000-8000-000000000b92');
SELECT is(pg_temp.cursor_of('a0000000-0000-4000-8000-000000000b92'), (SELECT cursor_at FROM seen WHERE step = 'raised'),
    'a read finds the user''s share held by another session and settles none of it');

SELECT pg_temp.list('a0000000-0000-4000-8000-000000000b91');
SELECT is(pg_temp.cursor_of('a0000000-0000-4000-8000-000000000b91'), pg_snapshot_xmin(pg_current_snapshot()),
    'another user''s share settles meanwhile, all of it');

SELECT dblink_exec('budget_peer', 'COMMIT');
SELECT dblink_disconnect('budget_peer');

SELECT pg_temp.raise(600);
SELECT pg_temp.list('a0000000-0000-4000-8000-000000000b92');
SELECT is(pg_temp.cursor_of('a0000000-0000-4000-8000-000000000b92'), pg_snapshot_xmin(pg_current_snapshot()),
    'once the other session lets go the share settles, all of it');

SELECT count(*) FROM claimius.get_users('a0000000-0000-4000-8000-000000000b91');
SELECT count(*) FROM claimius.get_users('a0000000-0000-4000-8000-000000000b92');

SELECT set_eq($$
    SELECT a.user_id AS viewer_id, b.user_id AS target_user_id, count(DISTINCT a.object_id) AS sharing
    FROM claimius.user_object a
        JOIN claimius.user_object b
            ON b.object_type = a.object_type AND b.object_id = a.object_id AND b.user_id <> a.user_id
    WHERE a.object_type <> 'claimius.claim'
    GROUP BY a.user_id, b.user_id
$$, $$
    SELECT s.user_id, v.target_user_id, v.sharing_object_count::BIGINT
    FROM claimius.samna_user s
        CROSS JOIN LATERAL claimius._user_users_for_viewer(s.user_id) v
    $$, 'the shared object counts the two readers read match their pairs');

SELECT cmp_ok((SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid() AND locktype = 'advisory'),
    '<=', 16::BIGINT,
    'every listing over thousands of open areas still holds no lock per object');

SELECT * FROM finish();
ROLLBACK;
