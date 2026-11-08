CREATE EXTENSION IF NOT EXISTS dblink;
SELECT plan(12);
CREATE TEMP TABLE started AS SELECT pg_current_xact_id()::TEXT::BIGINT AS txid;
SELECT dblink_connect('fold_a', 'dbname=' || current_database() || ' user=' || current_user);
SELECT dblink_connect('fold_b', 'dbname=' || current_database() || ' user=' || current_user);

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_user_users_fold'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.rebuilt() RETURNS TABLE (viewer_id UUID, target_user_id UUID, sharing BIGINT) AS $$
    SELECT mine.user_id AS viewer_id, other.user_id AS target_user_id, count(*) AS sharing
    FROM claimius.user_object mine
        JOIN claimius.user_object other
            ON other.app_id = mine.app_id
           AND other.object_type = mine.object_type
           AND other.object_id = mine.object_id
           AND other.user_id <> mine.user_id
    WHERE mine.app_id = pg_temp.app()
      AND mine.object_type <> 'claimius.claim'
    GROUP BY mine.user_id, other.user_id
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.viewed() RETURNS TABLE (viewer_id UUID, target_user_id UUID, sharing BIGINT) AS $$
    SELECT s.user_id, v.target_user_id, v.sharing_object_count::BIGINT
    FROM claimius.samna_user s
        CROSS JOIN LATERAL claimius._user_users_for_viewer(pg_temp.app(), s.user_id) v
    WHERE s.app_id = pg_temp.app()
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.stored() RETURNS TABLE (viewer_id UUID, target_user_id UUID, sharing BIGINT) AS $$
    SELECT uu.viewer_id, uu.target_user_id, uu.sharing_object_count::BIGINT
    FROM claimius.user_users uu
    WHERE uu.app_id = pg_temp.app()
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys  UUID := '00000000-0000-4000-8000-000000000c90';
    v_root UUID := '10000000-0000-4000-8000-000000000c90';
    v_desk UUID := '10000000-0000-4000-8000-000000000c91';
    v_app  UUID;
    v_crew UUID;
    v_user UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_user_users_fold',
        p_system_app_name         => 'SQL user_users fold test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-000000000c91', 'a0000000-0000-4000-8000-000000000c92',
                                  'a0000000-0000-4000-8000-000000000c93']::UUID[] LOOP
        PERFORM claimius.upsert_app_user(p_user_id => v_user, p_app_id => v_app, p_first_name => 'Sharer');
    END LOOP;
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Fold root', 'company', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_desk, 'Fold desk', 'company', v_root, v_root, 1, v_sys);
    v_crew := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Fold crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_desk, p_object_type => 'claimius.organization', p_inherits => FALSE);
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-000000000c91', 'a0000000-0000-4000-8000-000000000c92',
                                  'a0000000-0000-4000-8000-000000000c93']::UUID[] LOOP
        PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_user,
            p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    END LOOP;
    PERFORM count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000000c93', v_app, 'claimius.organization');
    PERFORM claimius._settle_user_users_diff(v_app);
END $$;
COMMIT;

CREATE TEMP TABLE marks AS
SELECT coalesce(max(id), 0) AS diff_id, (SELECT count(*) FROM claimius.user_users WHERE app_id = pg_temp.app()) AS stored
FROM claimius.user_users_diff;
COMMIT;

SELECT dblink_exec('fold_a', 'BEGIN');
SELECT * FROM dblink('fold_a', format($w$SELECT claimius.recompute_user_object(%L, 'a0000000-0000-4000-8000-000000000c91', 'claimius.organization', '10000000-0000-4000-8000-000000000c91')$w$, pg_temp.app())) AS t(result TEXT);
SELECT dblink_exec('fold_b', 'BEGIN');
SELECT dblink_exec('fold_b', 'SET LOCAL lock_timeout = ''5s''');
SELECT dblink_send_query('fold_b', format($w$SELECT claimius.recompute_user_object(%L, 'a0000000-0000-4000-8000-000000000c92', 'claimius.organization', '10000000-0000-4000-8000-000000000c91')$w$, pg_temp.app()));
SELECT pg_sleep(1);

SELECT is(dblink_is_busy('fold_b'), 0,
    'a second writer adds its pair on the object while the first still holds its own uncommitted, and waits on nothing');

SELECT dblink_exec('fold_a', 'COMMIT');
SELECT * FROM dblink_get_result('fold_b') AS t(changed BOOLEAN);
SELECT * FROM dblink_get_result('fold_b') AS t(changed BOOLEAN);
SELECT dblink_exec('fold_b', 'COMMIT');

SELECT is(
    (SELECT count(*) FROM claimius.user_users_diff
      WHERE id > (SELECT diff_id FROM marks) AND app_id = pg_temp.app()
        AND object_id = '10000000-0000-4000-8000-000000000c91' AND delta = 1),
    2::BIGINT,
    'each writer raised one user_users_diff row for its pair');

SELECT is((SELECT count(*) FROM claimius.user_users WHERE app_id = pg_temp.app()), (SELECT stored FROM marks),
    'neither write moved a user_users row');

SELECT set_eq('SELECT * FROM pg_temp.viewed()', 'SELECT * FROM pg_temp.rebuilt()',
    'the count every user reads equals a count over the committed pairs, the pair between the two writers counted once');

SELECT set_eq(
    $$SELECT user_id FROM claimius.get_users('a0000000-0000-4000-8000-000000000c91', pg_temp.app())$$,
    $$VALUES ('a0000000-0000-4000-8000-000000000c91'::UUID), ('a0000000-0000-4000-8000-000000000c92'::UUID), ('a0000000-0000-4000-8000-000000000c93'::UUID)$$,
    'get_users of the first writer sees the second writer and the earlier holder');
COMMIT;

DO $$
DECLARE
    v_sys  UUID := '00000000-0000-4000-8000-000000000c90';
    v_root UUID := '10000000-0000-4000-8000-000000000c90';
BEGIN
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-000000000c92', 'Fold desk two', 'company', v_root, v_root, 1, v_sys);
    PERFORM claimius.insert_claim_object(
        p_app_id => pg_temp.app(), p_claim_id => (SELECT id FROM claimius.claim WHERE name = 'Fold crew'),
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => '10000000-0000-4000-8000-000000000c92', p_object_type => 'claimius.organization', p_inherits => FALSE);
    PERFORM count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000000c91', pg_temp.app(), 'claimius.organization');
    PERFORM count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000000c92', pg_temp.app(), 'claimius.organization');
END $$;
COMMIT;

SELECT dblink_exec('fold_a', 'BEGIN');
SELECT * FROM dblink('fold_a', format('SELECT claimius._settle_user_users_diff(%L, %L)', pg_temp.app(), 'a0000000-0000-4000-8000-000000000c91')) AS t(result TEXT);
CREATE TEMP TABLE second_fold AS
SELECT clock_timestamp() AS started,
       claimius._settle_user_users_diff(pg_temp.app(), 'a0000000-0000-4000-8000-000000000c92') AS left_open,
       clock_timestamp() AS ended;

SELECT cmp_ok((SELECT left_open FROM second_fold), '>', 0,
    'a second fold leaves the object another fold holds');

SELECT cmp_ok((SELECT ended - started FROM second_fold), '<', INTERVAL '1 second',
    'and waits on nothing');

SELECT set_eq('SELECT * FROM pg_temp.viewed()', 'SELECT * FROM pg_temp.rebuilt()',
    'while the fold is held the count every user reads stays exact');
COMMIT;

SELECT dblink_exec('fold_a', 'COMMIT');
SELECT claimius._settle_user_users_diff(pg_temp.app());
COMMIT;

SELECT set_eq('SELECT * FROM pg_temp.stored()', 'SELECT * FROM pg_temp.rebuilt()',
    'once every row is folded the stored user_users equal a count over the pairs');

SELECT is_empty($$SELECT 1 FROM claimius.user_users_diff WHERE app_id = pg_temp.app()$$,
    'and no user_users_diff row of the app is left');
COMMIT;

DO $$
BEGIN
    PERFORM claimius.delete_user_claim(
        (SELECT uc.id FROM claimius.user_claim uc JOIN claimius.claim c ON c.id = uc.claim_id
          WHERE c.name = 'Fold crew' AND uc.user_id = 'a0000000-0000-4000-8000-000000000c92' AND uc.sa_deleted_at IS NULL),
        '00000000-0000-4000-8000-000000000c90');
    PERFORM count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-000000000c92', pg_temp.app(), 'claimius.organization');
END $$;

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_users('a0000000-0000-4000-8000-000000000c91', pg_temp.app())
       WHERE user_id = 'a0000000-0000-4000-8000-000000000c92'$$,
    'a user whose one shared pair left is no longer seen');

SELECT is_empty(
    $$SELECT 1 FROM claimius.user_users WHERE app_id = pg_temp.app()
       AND 'a0000000-0000-4000-8000-000000000c92' IN (viewer_id, target_user_id)$$,
    'and once folded no user_users row of that user is left in either direction');
COMMIT;

SELECT dblink_disconnect('fold_a');
SELECT dblink_disconnect('fold_b');
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
DROP EXTENSION dblink;

SELECT * FROM finish();
COMMIT;
DISCARD TEMP;
