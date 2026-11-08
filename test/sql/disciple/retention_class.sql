BEGIN;
SELECT plan(9);

SELECT is(
    (SELECT string_agg(r.kind || ':' || r.class, ',' ORDER BY r.kind) FROM claimius.get_retention() r),
    'access_diff:log,audit:audit,notification:log',
    'every disciple kind carries its class'
);

SELECT throws_ok($$ SELECT claimius.delete_expired('audit') $$, '22023', NULL, 'an audit kind is never deleted by age');

SELECT throws_ok($$
    SELECT claimius.upsert_retention('gotest_record', 'record', 30, 'claimius.delete_exported_audit')
$$, '22023', NULL, 'a record kind needs an export function');

SELECT throws_ok($$
    SELECT claimius.upsert_retention('gotest_log', 'log', 30, 'claimius.delete_expired_notification', 'claimius.get_expired_audit')
$$, '22023', NULL, 'a log kind takes no export function');

SELECT throws_ok($$
    SELECT claimius.upsert_retention('gotest_log', 'log', 30, 'claimius.delete_exported_audit')
$$, '42883', NULL, 'a log prune takes the interval to keep');

SELECT claimius.upsert_audit_partition(now() - INTERVAL '3 years');
INSERT INTO claimius.audit (id, operation, type, object_type, object_id, sa_owner_id, sa_created_at)
VALUES ('a0000000-0000-4000-8000-0000000e0001', 'update', 'test', 'claimius.organization',
        '10000000-0000-4000-8000-0000000e0001', '10000000-0000-4000-8000-0000000e0001', now() - INTERVAL '3 years');

CREATE TEMP TABLE expired AS
SELECT e.key, e.data->>'sa_root_id' AS root_id FROM claimius.get_expired('audit', '', 100) e
WHERE e.data->>'id' = 'a0000000-0000-4000-8000-0000000e0001';

SELECT is((SELECT count(*) FROM expired), 1::BIGINT, 'the expired audit row is read for export');

SELECT is((SELECT root_id FROM expired), NULL, 'an audit row whose owner is no organization carries no root');

SELECT is(
    claimius.delete_exported('audit', NULL, 'gotest/none/audit/2000/01/a-b.ndjson.gz', repeat('0', 64), ARRAY(SELECT key FROM expired), TRUE),
    1::BIGINT,
    'delete_exported removes exactly the exported keys'
);

SELECT is(
    (SELECT row_count FROM claimius.retention_export WHERE object_path = 'gotest/none/audit/2000/01/a-b.ndjson.gz' AND root_id IS NULL),
    1::BIGINT,
    'the exported batch has its manifest row under no root'
);

SELECT * FROM finish();
ROLLBACK;
