CREATE EXTENSION IF NOT EXISTS dblink;
SELECT plan(4);
SELECT dblink_connect('sync_early', 'dbname=' || current_database() || ' user=' || current_user);
SELECT dblink_connect('sync_late', 'dbname=' || current_database() || ' user=' || current_user);
COMMIT;

SELECT dblink_exec('sync_early', 'BEGIN');
SELECT * FROM dblink('sync_early', 'SELECT pg_current_xact_id()::TEXT') AS t (txid TEXT);
SELECT dblink_exec('sync_late', 'BEGIN');
SELECT dblink_exec('sync_late', $$
    INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
    VALUES ('upsert', 'claimius.user_field', '{"id": "60000000-0000-4000-8000-0000000000e2"}', '50000000-0000-4000-8000-0000000000e0')
$$);
SELECT dblink_exec('sync_early', $$
    INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
    VALUES ('upsert', 'claimius.user_field', '{"id": "60000000-0000-4000-8000-0000000000e1"}', '50000000-0000-4000-8000-0000000000e0')
$$);
SELECT dblink_exec('sync_early', 'COMMIT');
COMMIT;

CREATE TEMP TABLE served AS
SELECT e.txid, e.seq, e.payload ->> 'id' AS id
FROM claimius.get_sync_events('50000000-0000-4000-8000-0000000000e0', 0, 0) e;
COMMIT;

SELECT is(
    ARRAY(SELECT id FROM served),
    ARRAY['60000000-0000-4000-8000-0000000000e1'],
    'the transaction that took its xid first and its sequence last is served once it commits, ahead of the open one'
);
COMMIT;

SELECT dblink_exec('sync_late', 'COMMIT');
COMMIT;

SELECT ok(
    (SELECT s.seq FROM served s) > (SELECT e.seq FROM claimius.sync_event e WHERE e.payload ->> 'id' = '60000000-0000-4000-8000-0000000000e2'),
    'the event committed last carries the lower sequence, so a cursor by sequence alone would pass over it'
);
COMMIT;

SELECT is(
    ARRAY(SELECT e.payload ->> 'id'
          FROM claimius.get_sync_events('50000000-0000-4000-8000-0000000000e0',
                                        (SELECT s.txid FROM served s), (SELECT s.seq FROM served s)) e),
    ARRAY['60000000-0000-4000-8000-0000000000e2'],
    'the feed after the served cursor serves the event whose lower sequence committed last'
);
COMMIT;

SELECT is(
    ARRAY(SELECT e.payload ->> 'id' FROM claimius.get_sync_events('50000000-0000-4000-8000-0000000000e0', 0, 0) e),
    ARRAY['60000000-0000-4000-8000-0000000000e1', '60000000-0000-4000-8000-0000000000e2'],
    'the whole feed is in transaction order, not sequence order'
);
COMMIT;

DELETE FROM claimius.sync_event WHERE app_id = '50000000-0000-4000-8000-0000000000e0';
SELECT dblink_disconnect('sync_early');
SELECT dblink_disconnect('sync_late');
DROP EXTENSION dblink;

SELECT * FROM finish();
COMMIT;
DISCARD TEMP;
