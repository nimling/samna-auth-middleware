BEGIN;
SELECT plan(13);

CREATE TEMP TABLE crew (key TEXT PRIMARY KEY, id UUID);

CREATE FUNCTION pg_temp.crew(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM crew WHERE key = p_key
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.hour(p_module TEXT, p_start TIMESTAMPTZ, p_quantity NUMERIC, p_amount NUMERIC) RETURNS UUID AS $$
    INSERT INTO claimius.app_cost (module, period_start, quantity, unit, amount, currency, sa_owner_id, sa_created_by)
    VALUES (p_module, p_start, p_quantity, 'request', p_amount, 'NOK', pg_temp.crew('org'), pg_temp.crew('grant'))
    RETURNING id
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-0000000c0d00';
    v_org    UUID := '10000000-0000-4000-8000-0000000c0d01';
    v_reader UUID := 'a0000000-0000-4000-8000-0000000c0d01';
    v_other  UUID := 'a0000000-0000-4000-8000-0000000c0d02';
    v_claim  UUID;
    v_grant  UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();

    PERFORM claimius.upsert_app_user(p_user_id => v_reader, p_first_name => 'Reader');
    PERFORM claimius.upsert_app_user(p_user_id => v_other, p_first_name => 'Other');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_org, 'Cost crew', 'company', v_org, v_org, 0, v_sys);

    v_claim := (claimius.insert_claim(
        p_name => 'Cost reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_claim, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_grant := (claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_reader,
        p_sa_owner_id => v_org, p_sa_created_by => v_sys) -> 'user_claim' ->> 'id')::UUID;

    INSERT INTO crew VALUES ('org', v_org), ('reader', v_reader), ('other', v_other), ('grant', v_grant);
END $$;

SELECT has_table('claimius', 'app_cost', 'app_cost exists on the disciple');

SELECT hasnt_column('claimius', 'app_cost', 'app_id', 'a disciple cost row carries no app_id');

SELECT ok(EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'claimius' AND indexname = 'idx_app_cost_module_period'
                   AND indexdef LIKE '%UNIQUE%(module, period_start)%sa_deleted_at IS NULL%'),
    'the app has one live row per module and hour on the disciple');

SELECT is((SELECT is_internal FROM claimius.table_info WHERE object_type = 'claimius.app_cost'), TRUE,
    'app_cost is registered like the other routed tables');

SELECT ok(EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = 'claimius.app_cost'::REGCLASS AND tgname = 'tg_update_txid')
      AND EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = 'claimius.app_cost'::REGCLASS AND tgname = 'tg_update_timestamp')
      AND EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = 'claimius.app_cost'::REGCLASS AND tgname = 'tg_calc_access'),
    'a cost row carries the timestamp, txid and access calc triggers');

SELECT ok(has_table_privilege('claimius_disciple_client', 'claimius.app_cost', 'INSERT')
      AND has_table_privilege('claimius_disciple_client', 'claimius.app_cost', 'UPDATE')
      AND has_table_privilege('claimius_disciple_client', 'claimius.app_cost', 'DELETE')
      AND has_function_privilege('claimius_reader', 'claimius.get_app_cost(uuid, text, timestamptz, timestamptz, text)', 'EXECUTE'),
    'the sync applies cost rows and readers execute get_app_cost');

SELECT lives_ok($q$DO $$
BEGIN
    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
    INSERT INTO crew VALUES
        ('nine', pg_temp.hour('sqltest_cost_app', '2026-10-30 09:00+00', 10, 1.5)),
        ('ten', pg_temp.hour('sqltest_cost_app', '2026-10-30 10:00+00', 5, 0.5)),
        ('uptime', pg_temp.hour('uptime', '2026-10-30 09:00+00', 60, 0.06));
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);
END $$$q$, 'the feed applies cost rows under replay');

SELECT throws_ok(
    $$SELECT pg_temp.hour('sqltest_cost_app', '2026-10-30 09:00+00', 1, 1)$$,
    '23505', NULL, 'a second live row for the same module and hour is refused');

SELECT is(
    (SELECT count(*) FROM claimius.get_app_cost(pg_temp.crew('reader'))),
    3::BIGINT,
    'a reader with read on the organization gets one bucket per module and hour');

SELECT results_eq(
    $$SELECT module, period_start, quantity, amount, hours FROM claimius.get_app_cost(pg_temp.crew('reader'), 'sqltest_cost_app', NULL, NULL, 'day')$$,
    $$VALUES ('sqltest_cost_app', '2026-10-30 00:00+00'::TIMESTAMPTZ, 15::NUMERIC, 2.0::NUMERIC, 2::BIGINT)$$,
    'a day bucket sums the module''s hours and counts them');

SELECT is(
    (SELECT count(*) FROM claimius.get_app_cost(pg_temp.crew('other'))),
    0::BIGINT,
    'a reader without read on the organization gets no bucket');

SELECT throws_ok(
    $$SELECT * FROM claimius.get_app_cost(pg_temp.crew('reader'), NULL, NULL, NULL, 'week')$$,
    '22023', NULL, 'a granularity other than hour or day is refused');

UPDATE claimius.app_cost SET sa_deleted_at = now() WHERE id = pg_temp.crew('ten');

SELECT is(
    (SELECT count(*) FROM claimius.get_app_cost(pg_temp.crew('reader'))),
    2::BIGINT,
    'a deleted hour leaves the buckets');

SELECT * FROM finish();
ROLLBACK;
