BEGIN;
SELECT plan(9);

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT '50000000-0000-4000-8000-0000000000c5'::UUID
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.head() RETURNS TABLE (txid BIGINT, seq BIGINT) AS $$
    SELECT h.txid, h.seq FROM claimius.get_sync_seq(pg_temp.app()) h
$$ LANGUAGE sql;

DO $$
DECLARE
    v_system UUID;
    v_user   UUID;
BEGIN
    v_system := (claimius.init(
        p_system_app_slug         => 'sqltest_cursor',
        p_system_app_name         => 'SQL cursor test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    INSERT INTO claimius.samna_app (id, slug, name, private_key, private_seed, sa_owner_id, sa_created_by)
    SELECT pg_temp.app(), 'sqltest_cursor_app', 'Cursor app', 'key', 'seed', sa_owner_id, sa_created_by
    FROM claimius.samna_app WHERE id = v_system;

    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000c6', 'a0000000-0000-4000-8000-0000000000c7',
                                  'a0000000-0000-4000-8000-0000000000c8']::UUID[] LOOP
        PERFORM claimius.ensure_app_user(p_user_id => v_user, p_app_id => pg_temp.app(), p_first_name => 'Cursor');
    END LOOP;
END $$;

SELECT claimius.update_sync_cursor(pg_temp.app(), '70000000-0000-4000-8000-0000000000c1', 9, 5);
SELECT claimius.update_sync_cursor(pg_temp.app(), '70000000-0000-4000-8000-0000000000c2', 8, 30);

SELECT is(
    (SELECT count(*) FROM claimius.sync_consumer WHERE app_id = pg_temp.app()),
    2::BIGINT,
    'two consumers of one app keep a cursor each'
);

SELECT is(
    (SELECT ARRAY[last_applied_txid::TEXT::BIGINT, last_applied_seq] FROM claimius.prophet_state WHERE app_id = pg_temp.app()),
    ARRAY[8, 30]::BIGINT[],
    'the app''s cursor is the lowest of its consumers in (txid, seq) order'
);

SELECT claimius.update_sync_cursor(pg_temp.app(), '70000000-0000-4000-8000-0000000000c2', 10, 7);

SELECT is(
    ARRAY(SELECT last_applied_txid::TEXT || '.' || last_applied_seq FROM claimius.sync_consumer WHERE app_id = pg_temp.app() ORDER BY consumer_id),
    ARRAY['9.5', '10.7'],
    'moving one consumer leaves the other consumer''s cursor where it was'
);

SELECT is(
    claimius.check_sync_cursor(pg_temp.app(), 0, pg_sequence_last_value('claimius.sync_event_seq_seq') + 1),
    'ahead',
    'a cursor above every sequence the prophet issued is sent to the snapshot'
);

SELECT is(
    claimius.check_sync_cursor(pg_temp.app(), (SELECT txid FROM pg_temp.head()), (SELECT seq FROM pg_temp.head())),
    NULL,
    'a cursor at the head of the app''s events is served by the feed'
);

CREATE TEMP TABLE before_burst AS SELECT h.txid, h.seq, claimius.count_sync_source_rows(pg_temp.app()) AS rows FROM pg_temp.head() h;

INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
SELECT 'upsert', 'claimius.user_field', jsonb_build_object('id', gen_random_uuid()), pg_temp.app()
FROM generate_series(1, (SELECT rows FROM before_burst) + 1);

SELECT is(
    claimius.check_sync_cursor(pg_temp.app(), (SELECT txid FROM before_burst), (SELECT seq FROM before_burst)),
    'backlog',
    'a cursor behind by more events than the app has source rows is sent to the snapshot'
);

SELECT is(
    claimius.check_sync_cursor(pg_temp.app(), pg_current_xact_id()::TEXT::BIGINT, (SELECT seq FROM before_burst) + 2),
    NULL,
    'a cursor behind by fewer events than the app has source rows is served by the feed'
);

SELECT claimius.trim_sync_event();

SELECT is(
    (SELECT count(*) FROM claimius.sync_event WHERE app_id = pg_temp.app()),
    (SELECT rows FROM before_burst),
    'trimming keeps at most as many events of an app as it has source rows'
);

SELECT ok(
    (SELECT (trimmed_txid::TEXT::BIGINT, trimmed_seq) FROM claimius.prophet_state WHERE app_id = pg_temp.app()) > (SELECT (txid, seq) FROM before_burst)
    AND claimius.check_sync_cursor(pg_temp.app(), (SELECT txid FROM before_burst), (SELECT seq FROM before_burst)) = 'trimmed',
    'a cursor below the trimmed events is sent to the snapshot'
);

SELECT * FROM finish();
ROLLBACK;
