BEGIN;
SELECT plan(2);

SELECT is(
    (SELECT string_agg(r.kind || ':' || r.class, ',' ORDER BY r.kind) FROM claimius.get_retention() r),
    'access_diff:log,audit:audit,notification:log',
    'every disciple kind carries its class'
);

SELECT throws_ok($$
    SELECT claimius.upsert_retention('gotest_log', 'log', 30, 'claimius.get_retention')
$$, '42883', NULL, 'a prune takes the interval to keep');

SELECT * FROM finish();
ROLLBACK;
