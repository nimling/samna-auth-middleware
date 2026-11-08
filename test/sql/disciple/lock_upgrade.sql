CREATE EXTENSION IF NOT EXISTS dblink;
SELECT plan(5);
SELECT dblink_connect('lock_writer', 'dbname=' || current_database() || ' user=' || current_user);
SELECT dblink_exec('lock_writer', 'BEGIN');
SELECT dblink_exec('lock_writer', 'DO $d$ BEGIN PERFORM claimius._lock_objects(ARRAY[''public.writer:1'']); END $d$');
SET LOCAL lock_timeout = '2s';

SELECT lives_ok($$SELECT claimius._lock_objects(ARRAY['public.reader:1'])$$,
    'the reader takes one object under the lock the writer shares');

SELECT lives_ok($$SELECT claimius._lock_objects(ARRAY(
        SELECT 'public.reader:' || g FROM generate_series(2, claimius._object_lock_budget() + 1) AS g))$$,
    'crossing the budget while holding object locks does not wait for the lock the writer shares');

SELECT cmp_ok(current_setting('claimius.object_locks')::INTEGER, '>', claimius._object_lock_budget(),
    'the reader goes on one lock per object past the budget');

SELECT dblink_exec('lock_writer', 'COMMIT');

SELECT lives_ok($$SELECT claimius._lock_objects(ARRAY['public.reader:0'])$$,
    'once no other transaction shares the lock the reader crosses again');

SELECT is(current_setting('claimius.object_locks'), '-1',
    'and takes every object at once');

SELECT dblink_disconnect('lock_writer');
DROP EXTENSION dblink;

SELECT * FROM finish();
COMMIT;
DISCARD TEMP;
