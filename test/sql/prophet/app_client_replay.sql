BEGIN;
SELECT plan(3);

CREATE TEMP TABLE t_ctx (app_id UUID, actor UUID, client_id UUID);

DO $$
DECLARE
    v_init JSONB;
BEGIN
    v_init := claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_client_replay',
        p_system_app_name         => 'SQL client replay test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_system_user_id          => '00000000-0000-4000-8000-000000000e00',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    INSERT INTO t_ctx VALUES (
        (v_init -> 'samna_app' ->> 'id')::UUID,
        (v_init -> 'user_claim' ->> 'id')::UUID,
        'c0000000-0000-4000-8000-000000000e01'
    );
END $$;

SELECT lives_ok(
    $$SELECT claimius.upsert_app_client(app_id, client_id, 'h', 'replay-client', 'Replay client', 4, actor) FROM t_ctx$$,
    'upsert_app_client creates the client, its binding and its grant');
SELECT lives_ok(
    $$SELECT claimius.upsert_app_client(app_id, client_id, 'h', 'replay-client', 'Replay client', 4, actor) FROM t_ctx$$,
    'upsert_app_client replays without a duplicate grant');
SELECT is(
    (SELECT count(*)::int FROM claimius.user_claim uc JOIN claimius.claim c ON c.id = uc.claim_id AND c.name = 'Replay client', t_ctx
     WHERE uc.app_id = t_ctx.app_id AND uc.user_id = t_ctx.client_id AND uc.sa_deleted_at IS NULL),
    1,
    'the client holds one live capability grant after two calls');
SELECT * FROM finish();
ROLLBACK;
