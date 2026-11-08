BEGIN;
SELECT plan(6);

SELECT is(
    (SELECT string_agg(r.kind || ':' || r.class, ',' ORDER BY r.kind) FROM claimius.get_retention() r WHERE r.kind NOT LIKE 'gotest%'),
    'access_diff:log,analytics_capture:cache,analytics_event:log,analytics_heat:log,analytics_session:log,audit:audit,consent:record,notification:log,par_request:token,peer_collision:log,rate_limit:token,sync_event:log',
    'every prophet kind carries its class'
);

SELECT throws_ok($$ SELECT claimius.delete_expired('audit') $$, '22023', NULL, 'an audit kind is never deleted by age');

SELECT throws_ok($$ SELECT claimius.delete_expired('consent') $$, '22023', NULL, 'a record kind is never deleted by age');

SELECT throws_ok($$
    SELECT claimius.upsert_retention('gotest_record', 'record', 30, 'claimius.delete_exported_consent')
$$, '22023', NULL, 'a record kind needs an export function');

SELECT throws_ok($$
    SELECT claimius.upsert_retention('gotest_log', 'log', 30, 'claimius.delete_expired_notification', 'claimius.get_expired_consent')
$$, '22023', NULL, 'a log kind takes no export function');

SELECT throws_ok($$
    SELECT claimius.upsert_retention('gotest_log', 'log', 30, 'claimius.delete_exported_consent')
$$, '42883', NULL, 'a log prune takes the interval to keep');

SELECT * FROM finish();
ROLLBACK;
