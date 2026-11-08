BEGIN;
SELECT plan(11);

CREATE EXTENSION IF NOT EXISTS dblink;

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_budget'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.open() RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.access_diff
    WHERE app_id = pg_temp.app() AND kind = 'object' AND object_type = 'claimius.organization'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.unsettled(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.access_diff d
    WHERE d.app_id = pg_temp.app() AND d.kind = 'object' AND d.object_type = 'claimius.organization'
      AND NOT EXISTS (
          SELECT 1 FROM claimius.access_diff_user du
          WHERE du.diff_id = d.id AND du.user_id = p_user AND du.raise_count = d.raise_count)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.raise(p_count INTEGER) RETURNS VOID AS $$
    SELECT claimius._raise_object_diff(pg_temp.app(), 'claimius.organization', gen_random_uuid(), TRUE)
    FROM generate_series(1, p_count);
    UPDATE claimius.access_diff SET sa_created_at = now() - INTERVAL '1 hour'
    WHERE app_id = pg_temp.app() AND kind = 'object' AND object_type = 'claimius.organization'
      AND object_id <> '10000000-0000-4000-8000-000000000b9f';
$$ LANGUAGE sql;

CREATE TEMP TABLE seen (step TEXT PRIMARY KEY, open BIGINT);

CREATE FUNCTION pg_temp.list(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.get_objects(p_user, pg_temp.app(), 'claimius.organization')
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.share_key(p_user UUID) RETURNS BIGINT AS $$
    SELECT hashtextextended('claimius.share:' || pg_temp.app()::TEXT || ':' || p_user::TEXT, 0)
$$ LANGUAGE sql;

DO $$
DECLARE
    v_app    UUID;
    v_sys    UUID := '00000000-0000-4000-8000-000000000b90';
    v_root   UUID := '10000000-0000-4000-8000-000000000b90';
    v_vault  UUID := '10000000-0000-4000-8000-000000000b9f';
    v_crew   UUID;
    v_secret UUID;
BEGIN
    v_app := (claimius.init(
        p_system_app_slug         => 'sqltest_budget',
        p_system_app_name         => 'SQL lock budget test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    PERFORM claimius.ensure_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000b91', p_app_id => v_app, p_first_name => 'Reader');
    PERFORM claimius.ensure_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000b92', p_app_id => v_app, p_first_name => 'Other');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Budget root', 'company', v_root, v_root, 0, v_sys),
           (v_vault, 'Budget vault', 'company', v_vault, v_vault, 0, v_sys);
    PERFORM claimius.link_app_object(v_app, 'claimius.organization', v_root);
    PERFORM claimius.link_app_object(v_app, 'claimius.organization', v_vault);

    v_crew := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Budget crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-000000000b91',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-000000000b92',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);

    v_secret := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Budget vault keys', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_vault, p_sa_root_id => v_vault, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_secret, p_sa_owner_id => v_vault, p_sa_root_id => v_vault, p_sa_created_by => v_sys,
        p_object_id => v_vault, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_secret, p_user_id => 'a0000000-0000-4000-8000-000000000b91',
        p_sa_owner_id => v_vault, p_sa_created_by => v_sys);
END $$;

SELECT ok(EXISTS (SELECT 1 FROM claimius.get_objects('a0000000-0000-4000-8000-000000000b91', pg_temp.app(), 'claimius.organization')
                  WHERE object_id = '10000000-0000-4000-8000-000000000b9f'),
    'the reader reaches the vault through its binding');

SELECT pg_temp.raise(7000);
DO $$
BEGIN
    PERFORM claimius.remove_claim_object(co.id, '00000000-0000-4000-8000-000000000b90')
    FROM claimius.claim_object co
    WHERE co.app_id = pg_temp.app() AND co.object_id = '10000000-0000-4000-8000-000000000b9f' AND co.sa_deleted_at IS NULL;
END $$;

SELECT cmp_ok((SELECT count(*) FROM claimius.access_diff WHERE app_id = pg_temp.app()),
    '>', (SELECT current_setting('max_locks_per_transaction')::BIGINT * current_setting('max_connections')::BIGINT),
    'more areas are open than the lock table holds on default settings, the revoke the newest of them');

SELECT lives_ok($$CREATE TEMP TABLE listed AS
        SELECT object_id FROM claimius.get_objects('a0000000-0000-4000-8000-000000000b91', pg_temp.app(), 'claimius.organization')$$,
    'the reader lists with every area open and no out of shared memory');

SELECT cmp_ok((SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid() AND locktype = 'advisory'),
    '<=', (claimius._object_lock_budget() + 16)::BIGINT,
    'the reader holds no more advisory locks than the budget');

SELECT is_empty($$SELECT 1 FROM listed WHERE object_id = '10000000-0000-4000-8000-000000000b9f'$$,
    'the listing never returns the vault the revoke behind thousands of open areas took away');

SELECT is(pg_temp.unsettled('a0000000-0000-4000-8000-000000000b91'), 0::BIGINT,
    'one listing settles the reader''s share of every open area of the type it lists');

SELECT pg_temp.raise(600);
INSERT INTO seen VALUES ('raised', pg_temp.open());

SELECT dblink_connect('budget_peer', 'dbname=' || current_database());
SELECT dblink_exec('budget_peer', 'BEGIN');
SELECT * FROM dblink('budget_peer', format('SELECT pg_advisory_xact_lock(%s)', pg_temp.share_key('a0000000-0000-4000-8000-000000000b92'))) AS t(held TEXT);

SELECT pg_temp.list('a0000000-0000-4000-8000-000000000b92');
SELECT is(pg_temp.open(), (SELECT open FROM seen WHERE step = 'raised'),
    'a read finds the user''s share held by another session and settles none of it');

SELECT pg_temp.list('a0000000-0000-4000-8000-000000000b91');
SELECT is(pg_temp.unsettled('a0000000-0000-4000-8000-000000000b91'), 0::BIGINT,
    'another user''s share settles meanwhile, all of it');

SELECT dblink_exec('budget_peer', 'COMMIT');
SELECT dblink_disconnect('budget_peer');

SELECT pg_temp.raise(600);
SELECT pg_temp.list('a0000000-0000-4000-8000-000000000b92');
SELECT is(pg_temp.unsettled('a0000000-0000-4000-8000-000000000b92'), 0::BIGINT,
    'once the other session lets go the share settles, all of it');

SELECT is_empty($$
    SELECT a.user_id, b.user_id, count(DISTINCT a.object_id)
    FROM claimius.user_object a
        JOIN claimius.user_object b
            ON b.app_id = a.app_id AND b.object_type = a.object_type AND b.object_id = a.object_id AND b.user_id <> a.user_id
    WHERE a.app_id = pg_temp.app() AND a.object_type <> 'claimius.claim'
    GROUP BY a.user_id, b.user_id
    EXCEPT
    SELECT viewer_id, target_user_id, sharing_object_count::BIGINT FROM claimius.user_users WHERE app_id = pg_temp.app()
$$, 'the shared object counts of the two readers match their pairs');

SELECT cmp_ok((SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid() AND locktype = 'advisory'),
    '<=', (claimius._object_lock_budget() + 16)::BIGINT,
    'every listing over thousands of open areas still holds no more than the budget');

SELECT * FROM finish();
ROLLBACK;
