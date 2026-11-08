BEGIN;
SELECT plan(29);

CREATE TEMP TABLE crew (key TEXT PRIMARY KEY, id UUID);

CREATE FUNCTION pg_temp.crew(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM crew WHERE key = p_key
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.cost(p_module TEXT, p_kind TEXT, p_start TIMESTAMPTZ, p_end TIMESTAMPTZ, p_quantity NUMERIC, p_amount NUMERIC, p_ref TEXT,
                             p_schema JSONB DEFAULT '{}', p_detail JSONB DEFAULT '{}', p_supersedes UUID DEFAULT NULL) RETURNS UUID AS $$
    INSERT INTO claimius.app_cost (app_id, module, kind, period_start, period_end, quantity, unit, amount, currency, ref, schema, detail, supersedes_id, sa_owner_id, sa_created_by)
    VALUES (pg_temp.crew('app'), p_module, p_kind, p_start, p_end, p_quantity, 'request', p_amount, 'NOK', p_ref, p_schema, p_detail, p_supersedes, pg_temp.crew('org'), pg_temp.crew('grant'))
    RETURNING id
$$ LANGUAGE sql;

DO $$
DECLARE
    v_init   JSONB;
    v_system UUID;
    v_org    UUID;
    v_sys_uc UUID;
    v_app    UUID := '50000000-0000-4000-8000-0000000c0501';
    v_actor  UUID := 'a0000000-0000-4000-8000-0000000c0501';
    v_other  UUID := 'a0000000-0000-4000-8000-0000000c0502';
    v_claim  UUID;
    v_grant  UUID;
BEGIN
    v_init := claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_cost',
        p_system_app_name         => 'SQL cost test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_system := (v_init -> 'samna_app' ->> 'id')::UUID;
    v_org    := (v_init -> 'organization' ->> 'id')::UUID;
    v_sys_uc := (v_init -> 'user_claim' ->> 'id')::UUID;

    INSERT INTO claimius.samna_app (id, slug, name, private_key, private_seed, sa_owner_id, sa_created_by)
    VALUES (v_app, 'sqltest_cost_app', 'Cost app', 'key', 'seed', v_org, v_sys_uc);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_org);

    PERFORM claimius.upsert_app_user(p_user_id => v_actor, p_app_id => v_app, p_first_name => 'Actor');
    PERFORM claimius.upsert_app_user(p_user_id => v_other, p_app_id => v_app, p_first_name => 'Other');

    v_claim := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Cost owners', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_app_id => v_app, p_claim_id => v_claim, p_sa_owner_id => v_org, p_sa_root_id => v_org,
        p_sa_created_by => v_sys_uc, p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_claim_object(p_app_id => v_app, p_claim_id => v_claim, p_sa_owner_id => v_org, p_sa_root_id => v_org,
        p_sa_created_by => v_sys_uc, p_object_id => v_app, p_object_type => 'claimius.samna_app', p_inherits => TRUE);
    v_grant := (claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_claim, p_user_id => v_actor,
        p_sa_owner_id => v_org, p_sa_created_by => v_sys_uc) -> 'user_claim' ->> 'id')::UUID;

    INSERT INTO crew VALUES ('system', v_system), ('app', v_app), ('org', v_org), ('actor', v_actor), ('other', v_other), ('grant', v_grant);
END $$;

SELECT has_table('claimius', 'app_cost', 'app_cost exists on the prophet');

SELECT col_not_null('claimius', 'app_cost', 'app_id', 'a prophet cost row names its app');

SELECT ok(EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'claimius' AND indexname = 'idx_app_cost_app_module_ref'
                   AND indexdef LIKE '%UNIQUE%(app_id, module, ref)%sa_deleted_at IS NULL%')
      AND NOT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'claimius' AND indexname = 'idx_app_cost_app_module_period'),
    'an app has one live row per module and ref, and the hourly index is gone');

SELECT ok(NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'claimius.app_cost'::REGCLASS AND conname = 'app_cost_period_start_check')
      AND EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'claimius.app_cost'::REGCLASS AND conname = 'app_cost_period_check'),
    'the hour check is gone and the window check stands');

SELECT is((SELECT is_internal FROM claimius.table_info WHERE object_type = 'claimius.app_cost'), TRUE,
    'app_cost is registered like the other routed tables');

SELECT ok(EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = 'claimius.app_cost'::REGCLASS AND tgname = 'tg_enforce_app_home')
      AND EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = 'claimius.app_cost'::REGCLASS AND tgname = 'tg_update_txid')
      AND EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = 'claimius.app_cost'::REGCLASS AND tgname = 'tg_update_timestamp')
      AND EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = 'claimius.app_cost'::REGCLASS AND tgname = 'tg_calc_access')
      AND EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = 'claimius.app_cost'::REGCLASS AND tgname = 'tg_emit_sync_event'),
    'a cost row carries the timestamp, txid, home, access calc and sync triggers');

SELECT ok(has_table_privilege('claimius_writer', 'claimius.app_cost', 'INSERT')
      AND has_table_privilege('claimius_writer', 'claimius.app_cost', 'UPDATE')
      AND has_function_privilege('claimius_reader', 'claimius.get_app_cost(uuid, uuid, text, timestamptz, timestamptz, text, text, text, uuid)', 'EXECUTE'),
    'claimius_writer writes cost rows directly and claimius_reader executes get_app_cost');

INSERT INTO crew VALUES
    ('nine', pg_temp.cost('sqltest_cost_app', 'usage', '2026-10-30 09:00+00', '2026-10-30 10:00+00', 10, 1.5, 'h9')),
    ('ten', pg_temp.cost('sqltest_cost_app', 'usage', '2026-10-30 10:00+00', '2026-10-30 11:00+00', 5, 0.5, 'h10')),
    ('span', pg_temp.cost('sqltest_cost_app', 'usage', '2026-10-30 22:00+00', '2026-10-31 02:00+00', 40, 4, 'span')),
    ('uptime', pg_temp.cost('uptime', 'fee', '2026-10-30 09:30+00', '2026-10-30 09:30+00', 1, 0.06, 'check-1'));

SELECT throws_ok(
    $$SELECT pg_temp.cost('sqltest_cost_app', 'usage', '2026-10-30 12:00+00', '2026-10-30 13:00+00', 1, 1, 'h9')$$,
    '23505', NULL, 'a second live row for the same app, module and ref is refused');

SELECT throws_ok(
    $$SELECT pg_temp.cost('sqltest_cost_app', 'usage', '2026-10-30 12:00+00', '2026-10-30 11:00+00', 1, 1, 'backwards')$$,
    '23514', NULL, 'a window ending before it starts is refused');

SELECT throws_ok(
    $$SELECT pg_temp.cost('sqltest_cost_app', 'tax', '2026-10-30 12:00+00', '2026-10-30 13:00+00', 1, 1, 'kind')$$,
    '23514', NULL, 'a kind outside usage, fee, period, credit is refused');

SELECT throws_ok(
    $$SELECT pg_temp.cost('sqltest_cost_app', 'usage', '2026-10-30 12:00+00', '2026-10-30 13:00+00', 1, 1, '')$$,
    '23514', NULL, 'an empty ref is refused');

SELECT throws_ok(
    $$SELECT pg_temp.cost('sqltest_cost_app', 'usage', '2026-10-30 12:00+00', '2026-10-30 13:00+00', 1, 1, 'orphan', '{}', '{}', '99999999-0000-4000-8000-000000000999')$$,
    '23503', NULL, 'supersedes_id must name a cost row');

SELECT throws_ok(
    $$INSERT INTO claimius.app_cost (app_id, module, kind, period_start, period_end, quantity, unit, amount, currency, ref, sa_owner_id, sa_created_by)
      VALUES (pg_temp.crew('app'), 'sqltest_cost_app', 'usage', '2026-10-30 12:00+00', '2026-10-30 13:00+00', 1, 'request', 1, 'nok', 'currency', pg_temp.crew('org'), pg_temp.crew('grant'))$$,
    '23514', NULL, 'a currency that is not an ISO 4217 code is refused');

SELECT is(
    (SELECT array_agg(e.app_id) FROM claimius.sync_event e WHERE e.event_type = 'claimius.app_cost' AND (e.payload ->> 'id')::UUID = pg_temp.crew('nine')),
    ARRAY[pg_temp.crew('app')],
    'a cost row is published on its app''s feed');

SELECT is(
    (SELECT count(*) FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'))),
    7::BIGINT,
    'a reader with read on the app gets one bucket per module, kind and hour, a window across midnight spread over its hours');

SELECT results_eq(
    $$SELECT period_start, quantity, amount, hours, rows FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), 'sqltest_cost_app', NULL, NULL, 'day')$$,
    $$VALUES ('2026-10-30 00:00+00'::TIMESTAMPTZ, 35::NUMERIC, 4::NUMERIC, 4::NUMERIC, 3::BIGINT), ('2026-10-31 00:00+00'::TIMESTAMPTZ, 20::NUMERIC, 2::NUMERIC, 2::NUMERIC, 1::BIGINT)$$,
    'a day bucket sums the module''s rows, prorating the one across midnight by its overlap');

SELECT results_eq(
    $$SELECT kind, period_start, quantity, hours FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), 'uptime', NULL, NULL, 'day')$$,
    $$VALUES ('fee', '2026-10-30 00:00+00'::TIMESTAMPTZ, 1::NUMERIC, 0::NUMERIC)$$,
    'a fee falls whole into the bucket of its instant');

SELECT is(
    (SELECT quantity FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), 'sqltest_cost_app', '2026-10-30 10:00+00', '2026-10-30 11:00+00')),
    5::NUMERIC,
    'from is inclusive and to exclusive on the window');

SELECT results_eq(
    $$SELECT period_start, quantity FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), 'sqltest_cost_app', '2026-10-15 00:00+00', '2026-11-01 00:00+00', 'month')$$,
    $$VALUES ('2026-10-01 00:00+00'::TIMESTAMPTZ, 55::NUMERIC)$$,
    'a month bucket folds every row of the window');

INSERT INTO crew VALUES ('fix', pg_temp.cost('sqltest_cost_app', 'usage', '2026-10-30 09:00+00', '2026-10-30 10:00+00', 30, 4, 'h9-fix', '{}', '{}', pg_temp.crew('nine')));

SELECT is(
    (SELECT quantity FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), 'sqltest_cost_app', '2026-10-30 09:00+00', '2026-10-30 10:00+00')),
    30::NUMERIC,
    'a superseded row is left out and its correction counted');

INSERT INTO crew
SELECT 'gauge-' || m.method || '-' || n, pg_temp.cost('gauge_' || m.method, 'usage', '2026-10-30 09:00+00'::TIMESTAMPTZ + (n || ' minutes')::INTERVAL, '2026-10-30 09:00+00'::TIMESTAMPTZ + (n + 1 || ' minutes')::INTERVAL,
    n, n * 0.1, 'g' || n,
    jsonb_build_object('$id', 'https://samna.io/schema/gauge/' || m.method, 'x-samna', jsonb_build_object('accumulate', jsonb_build_object('method', m.method, 'group', jsonb_build_array('subject.id')))),
    jsonb_build_object('subject', jsonb_build_object('id', CASE WHEN n % 2 = 0 THEN 'a' ELSE 'b' END)))
FROM unnest(ARRAY['sum', 'max', 'last', 'count']) AS m(method), generate_series(1, 4) AS n;

SELECT results_eq(
    $$SELECT schema_id, group_key, quantity, rows FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), 'gauge_sum')$$,
    $$VALUES ('https://samna.io/schema/gauge/sum', '{"subject.id": "a"}'::JSONB, 6::NUMERIC, 2::BIGINT), ('https://samna.io/schema/gauge/sum', '{"subject.id": "b"}'::JSONB, 4::NUMERIC, 2::BIGINT)$$,
    'method sum adds the rows of each group key and names the schema');

SELECT results_eq(
    $$SELECT group_key, quantity FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), 'gauge_max')$$,
    $$VALUES ('{"subject.id": "a"}'::JSONB, 4::NUMERIC), ('{"subject.id": "b"}'::JSONB, 3::NUMERIC)$$,
    'method max keeps the largest row of each group key');

SELECT results_eq(
    $$SELECT group_key, quantity FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), 'gauge_last')$$,
    $$VALUES ('{"subject.id": "a"}'::JSONB, 4::NUMERIC), ('{"subject.id": "b"}'::JSONB, 3::NUMERIC)$$,
    'method last keeps the latest row of each group key');

SELECT results_eq(
    $$SELECT group_key, quantity, amount FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), 'gauge_count')$$,
    $$VALUES ('{"subject.id": "a"}'::JSONB, 2::NUMERIC, 0.6::NUMERIC), ('{"subject.id": "b"}'::JSONB, 2::NUMERIC, 0.4::NUMERIC)$$,
    'method count makes quantity the row count and still sums the amount');

SELECT is(
    (SELECT count(*) FROM claimius.get_app_cost(pg_temp.crew('other'), pg_temp.crew('app'))),
    0::BIGINT,
    'a reader without read on the app gets no bucket');

SELECT throws_ok(
    $$SELECT * FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), NULL, NULL, NULL, 'week')$$,
    '22023', NULL, 'a granularity other than hour, day or month is refused');

UPDATE claimius.app_cost SET sa_deleted_at = now() WHERE id = pg_temp.crew('ten');

SELECT is(
    (SELECT count(*) FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), 'sqltest_cost_app')),
    5::BIGINT,
    'a deleted row leaves the buckets');

SELECT lives_ok($$ SELECT claimius.prune('{"size_mb": 0}'::jsonb) $$, 'prune runs with cost rows present');

SELECT is((SELECT count(*) FROM claimius.app_cost WHERE app_id = pg_temp.crew('app')), 21::BIGINT,
    'cost rows are application data and are never pruned');

SELECT * FROM finish();
ROLLBACK;
