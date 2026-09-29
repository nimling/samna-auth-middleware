BEGIN;
SELECT plan(9);

CREATE EXTENSION IF NOT EXISTS dblink;

CREATE FUNCTION pg_temp.open() RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.access_diff
    WHERE kind = 'object' AND object_type = 'claimius.organization'
$$ LANGUAGE sql;

CREATE TEMP TABLE seen (step TEXT PRIMARY KEY, open BIGINT);

CREATE FUNCTION pg_temp.list(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.get_objects(p_user, 'claimius.organization')
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.share_key(p_user UUID) RETURNS BIGINT AS $$
    SELECT hashtextextended('claimius.share:' || p_user::TEXT, 0)
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys   UUID := '00000000-0000-4000-8000-000000000b90';
    v_root  UUID := '10000000-0000-4000-8000-000000000b90';
    v_crew  UUID;
BEGIN
    PERFORM claimius.init();

    PERFORM claimius.ensure_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000b91', p_first_name => 'Reader');
    PERFORM claimius.ensure_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000b92', p_first_name => 'Other');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Budget root', 'company', v_root, v_root, 0, v_sys);

    v_crew := (claimius.create_claim(
        p_name => 'Budget crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.assign_claim_user(p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-000000000b91',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM claimius.assign_claim_user(p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-000000000b92',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);

    PERFORM claimius._raise_object_diff('claimius.organization', gen_random_uuid(), TRUE)
    FROM generate_series(1, 7000);
END $$;

SELECT cmp_ok((SELECT count(*) FROM claimius.access_diff),
    '>', (SELECT current_setting('max_locks_per_transaction')::BIGINT * current_setting('max_connections')::BIGINT),
    'more areas are open than the lock table holds on default settings');

INSERT INTO seen VALUES ('seeded', pg_temp.open());

SELECT lives_ok($$SELECT pg_temp.list('a0000000-0000-4000-8000-000000000b91')$$,
    'the reader lists with every area open and no out of shared memory');

SELECT cmp_ok((SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid() AND locktype = 'advisory'),
    '<=', (claimius._object_lock_budget() + 16)::BIGINT,
    'the reader holds no more advisory locks than the budget');

INSERT INTO seen VALUES ('first', pg_temp.open());
SELECT ok((SELECT open FROM seen WHERE step = 'first') < (SELECT open FROM seen WHERE step = 'seeded')
      AND (SELECT open FROM seen WHERE step = 'seeded') - (SELECT open FROM seen WHERE step = 'first') <= claimius._object_lock_budget(),
    'one listing settles at most one batch of the budget and leaves the rest open');

SELECT dblink_connect('budget_peer', 'dbname=' || current_database());
SELECT dblink_exec('budget_peer', 'BEGIN');
SELECT * FROM dblink('budget_peer', format('SELECT pg_advisory_xact_lock(%s)', pg_temp.share_key('a0000000-0000-4000-8000-000000000b92'))) AS t(held TEXT);

SELECT pg_temp.list('a0000000-0000-4000-8000-000000000b92');
SELECT is(pg_temp.open(), (SELECT open FROM seen WHERE step = 'first'),
    'a read finds the user''s share held by another session and settles none of it');

SELECT pg_temp.list('a0000000-0000-4000-8000-000000000b91');
INSERT INTO seen VALUES ('second', pg_temp.open());
SELECT cmp_ok((SELECT open FROM seen WHERE step = 'second'), '<', (SELECT open FROM seen WHERE step = 'first'),
    'another user''s share settles meanwhile, the next batch of it');

SELECT dblink_exec('budget_peer', 'COMMIT');
SELECT dblink_disconnect('budget_peer');

SELECT pg_temp.list('a0000000-0000-4000-8000-000000000b92');
SELECT cmp_ok(pg_temp.open(), '<', (SELECT open FROM seen WHERE step = 'second'),
    'once the other session lets go the share settles, one batch');

SELECT is_empty($$
    SELECT a.user_id, b.user_id, count(DISTINCT a.object_id)
    FROM claimius.user_object a
        JOIN claimius.user_object b
            ON b.object_type = a.object_type AND b.object_id = a.object_id AND b.user_id <> a.user_id
    WHERE a.object_type <> 'claimius.claim'
    GROUP BY a.user_id, b.user_id
    EXCEPT
    SELECT viewer_id, target_user_id, sharing_object_count::BIGINT FROM claimius.user_users
$$, 'the shared object counts of the two readers match their pairs');

SELECT cmp_ok((SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid() AND locktype = 'advisory'),
    '<=', (claimius._object_lock_budget() + 16)::BIGINT,
    'three listings over thousands of open areas still hold no more than the budget');

SELECT * FROM finish();
ROLLBACK;
