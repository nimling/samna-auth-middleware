BEGIN;
SELECT plan(2);

SELECT is(
    (SELECT string_agg(r.kind || ':' || r.class, ',' ORDER BY r.kind) FROM claimius.get_retention() r WHERE r.kind NOT LIKE 'gotest%'),
    'access_diff:log,analytics_capture:cache,analytics_event:log,analytics_heat:log,analytics_session:log,audit:audit,notification:log,par_request:token,peer_collision:log,rate_limit:token,sync_event:log',
    'every prophet kind carries its class'
);

SELECT throws_ok($$
    SELECT claimius.upsert_retention('gotest_log', 'log', 30, 'claimius.get_retention')
$$, '42883', NULL, 'a prune takes the interval to keep');

SELECT * FROM finish();
ROLLBACK;
