CREATE EXTENSION IF NOT EXISTS dblink;
SELECT plan(3);
SELECT dblink_connect('sync_open', 'dbname=' || current_database() || ' user=' || current_user);
SELECT dblink_connect('sync_done', 'dbname=' || current_database() || ' user=' || current_user);
COMMIT;

SELECT dblink_exec('sync_open', 'BEGIN');
SELECT dblink_exec('sync_open', $$
    INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
    VALUES ('upsert', 'claimius.user_field', '{"id": "60000000-0000-4000-8000-0000000000d1"}', '50000000-0000-4000-8000-0000000000d0')
$$);
SELECT dblink_exec('sync_done', $$
    INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
    VALUES ('upsert', 'claimius.user_field', '{"id": "60000000-0000-4000-8000-0000000000d2"}', '50000000-0000-4000-8000-0000000000d0')
$$);
COMMIT;

SELECT is(
    (SELECT count(*) FROM claimius.sync_event WHERE app_id = '50000000-0000-4000-8000-0000000000d0'),
    1::BIGINT,
    'the event of the committed transaction is in the outbox while the lower sequence is still open'
);
COMMIT;

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_sync_events('50000000-0000-4000-8000-0000000000d0', 0, 0)$$,
    'the feed serves neither the open event nor the committed one above it'
);
COMMIT;

SELECT dblink_exec('sync_open', 'COMMIT');
COMMIT;

SELECT is(
    ARRAY(SELECT e.payload ->> 'id' FROM claimius.get_sync_events('50000000-0000-4000-8000-0000000000d0', 0, 0) e),
    ARRAY['60000000-0000-4000-8000-0000000000d1', '60000000-0000-4000-8000-0000000000d2'],
    'once the open transaction commits the feed serves both events in sequence order'
);
COMMIT;

DELETE FROM claimius.sync_event WHERE app_id = '50000000-0000-4000-8000-0000000000d0';
SELECT dblink_disconnect('sync_open');
SELECT dblink_disconnect('sync_done');
DROP EXTENSION dblink;

SELECT * FROM finish();
COMMIT;
DISCARD TEMP;
