BEGIN;
SELECT plan(17);

CREATE TEMP TABLE crew (key TEXT PRIMARY KEY, id UUID);

CREATE FUNCTION pg_temp.crew(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM crew WHERE key = p_key
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.hour(p_module TEXT, p_start TIMESTAMPTZ, p_quantity NUMERIC, p_amount NUMERIC) RETURNS UUID AS $$
    INSERT INTO claimius.app_cost (app_id, module, period_start, quantity, unit, amount, currency, sa_owner_id, sa_created_by)
    VALUES (pg_temp.crew('app'), p_module, p_start, p_quantity, 'request', p_amount, 'NOK', pg_temp.crew('org'), pg_temp.crew('grant'))
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

SELECT ok(EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'claimius' AND indexname = 'idx_app_cost_app_module_period'
                   AND indexdef LIKE '%UNIQUE%(app_id, module, period_start)%sa_deleted_at IS NULL%'),
    'an app has one live row per module and hour');

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
      AND has_function_privilege('claimius_reader', 'claimius.get_app_cost(uuid, uuid, text, timestamptz, timestamptz, text)', 'EXECUTE'),
    'claimius_writer writes cost rows directly and claimius_reader executes get_app_cost');

INSERT INTO crew VALUES
    ('nine', pg_temp.hour('sqltest_cost_app', '2026-10-30 09:00+00', 10, 1.5)),
    ('ten', pg_temp.hour('sqltest_cost_app', '2026-10-30 10:00+00', 5, 0.5)),
    ('uptime', pg_temp.hour('uptime', '2026-10-30 09:00+00', 60, 0.06));

SELECT throws_ok(
    $$SELECT pg_temp.hour('sqltest_cost_app', '2026-10-30 09:00+00', 1, 1)$$,
    '23505', NULL, 'a second live row for the same app, module and hour is refused');

SELECT throws_ok(
    $$SELECT pg_temp.hour('sqltest_cost_app', '2026-10-30 11:30+00', 1, 1)$$,
    '23514', NULL, 'a period_start off the hour is refused');

SELECT throws_ok(
    $$INSERT INTO claimius.app_cost (app_id, module, period_start, quantity, unit, amount, currency, sa_owner_id, sa_created_by)
      VALUES (pg_temp.crew('app'), 'sqltest_cost_app', '2026-10-30 12:00+00', 1, 'request', 1, 'nok', pg_temp.crew('org'), pg_temp.crew('grant'))$$,
    '23514', NULL, 'a currency that is not an ISO 4217 code is refused');

SELECT is(
    (SELECT array_agg(e.app_id) FROM claimius.sync_event e WHERE e.event_type = 'claimius.app_cost' AND (e.payload ->> 'id')::UUID = pg_temp.crew('nine')),
    ARRAY[pg_temp.crew('app')],
    'a cost row is published on its app''s feed');

SELECT is(
    (SELECT count(*) FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'))),
    3::BIGINT,
    'a reader with read on the app gets one bucket per module and hour');

SELECT results_eq(
    $$SELECT module, period_start, quantity, amount, hours FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), 'sqltest_cost_app', NULL, NULL, 'day')$$,
    $$VALUES ('sqltest_cost_app', '2026-10-30 00:00+00'::TIMESTAMPTZ, 15::NUMERIC, 2.0::NUMERIC, 2::BIGINT)$$,
    'a day bucket sums the module''s hours and counts them');

SELECT is(
    (SELECT quantity FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), 'sqltest_cost_app', '2026-10-30 10:00+00', '2026-10-30 11:00+00')),
    5::NUMERIC,
    'from is inclusive and to exclusive on period_start');

SELECT is(
    (SELECT count(*) FROM claimius.get_app_cost(pg_temp.crew('other'), pg_temp.crew('app'))),
    0::BIGINT,
    'a reader without read on the app gets no bucket');

SELECT throws_ok(
    $$SELECT * FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'), NULL, NULL, NULL, 'week')$$,
    '22023', NULL, 'a granularity other than hour or day is refused');

UPDATE claimius.app_cost SET sa_deleted_at = now() WHERE id = pg_temp.crew('ten');

SELECT is(
    (SELECT count(*) FROM claimius.get_app_cost(pg_temp.crew('actor'), pg_temp.crew('app'))),
    2::BIGINT,
    'a deleted hour leaves the buckets');

SELECT lives_ok($$ SELECT claimius.prune('{"size_mb": 0}'::jsonb) $$, 'prune runs with cost rows present');

SELECT is((SELECT count(*) FROM claimius.app_cost WHERE app_id = pg_temp.crew('app')), 3::BIGINT,
    'cost rows are application data and are never pruned');

SELECT * FROM finish();
ROLLBACK;
