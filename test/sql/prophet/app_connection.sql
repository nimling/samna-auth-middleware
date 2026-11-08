BEGIN;
SELECT plan(3);

SELECT claimius.upsert_system_app(
    p_system_app_slug         => 'sqltest_static',
    p_system_app_name         => 'SQL static test',
    p_system_app_private_key  => 'key',
    p_system_app_private_seed => 'seed',
    p_system_user_id          => '00000000-0000-4000-8000-000000000f00',
    p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
);

SELECT is(
    enum_range(NULL::claimius.app_connection)::TEXT[],
    ARRAY['direct', 'code', 'bff', 'static'],
    'app_connection carries the four session delivery modes');
SELECT lives_ok(
    $$UPDATE claimius.samna_app SET connection = 'static' WHERE slug = 'sqltest_static'$$,
    'an app takes the static connection');
SELECT is(
    (SELECT connection::TEXT FROM claimius.samna_app WHERE slug = 'sqltest_static'),
    'static',
    'the static connection reads back');
SELECT * FROM finish();
ROLLBACK;
