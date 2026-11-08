BEGIN;
SELECT plan(5);

CREATE TEMP TABLE crew (key TEXT PRIMARY KEY, id UUID);

CREATE FUNCTION pg_temp.hash(p_value TEXT) RETURNS TEXT AS $$
    SELECT encode(digest(p_value, 'sha256'), 'hex')
$$ LANGUAGE sql;

DO $$
DECLARE
    v_init    JSONB;
    v_app     UUID;
    v_foreign UUID := '50000000-0000-4000-8000-000000000c61';
    v_org     UUID;
    v_sys_uc  UUID;
    v_claim   UUID;
    v_fclaim  UUID;
    v_code    UUID;
    v_fcode   UUID;
BEGIN
    v_init := claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_prune_code',
        p_system_app_name         => 'SQL prune code test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_app    := (v_init -> 'samna_app' ->> 'id')::UUID;
    v_org    := (v_init -> 'organization' ->> 'id')::UUID;
    v_sys_uc := (v_init -> 'user_claim' ->> 'id')::UUID;

    INSERT INTO claimius.samna_app (id, slug, name, private_key, private_seed, sa_owner_id, sa_created_by)
    VALUES (v_foreign, 'sqltest_prune_foreign', 'Foreign app', 'key', 'seed', v_org, v_sys_uc);
    PERFORM claimius.upsert_app_object(v_foreign, 'claimius.organization', v_org);

    v_claim := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Home code claim', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    v_fclaim := (claimius.insert_claim(
        p_app_id => v_foreign, p_name => 'Foreign code claim', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;

    INSERT INTO claimius.code (app_id, hash, kind, claim_id, ends_at, sa_owner_id, sa_created_by)
    VALUES (v_app, pg_temp.hash('home-value'), 'invite', v_claim, now() - interval '30 days', v_org, v_sys_uc)
    RETURNING id INTO v_code;
    INSERT INTO claimius.code (app_id, hash, kind, claim_id, ends_at, sa_owner_id, sa_created_by)
    VALUES (v_foreign, pg_temp.hash('foreign-value'), 'invite', v_fclaim, now() - interval '30 days', v_org, v_sys_uc)
    RETURNING id INTO v_fcode;

    INSERT INTO crew VALUES
        ('app', v_app),
        ('foreign', v_foreign),
        ('home_code', v_code),
        ('foreign_code', v_fcode);

    INSERT INTO claimius.prophet_peer (prophet, endpoint, app_slug, app_secret, sa_created_by)
    VALUES ('sqltest_prune_peer', 'http://peer.test', 'sqltest_prune_foreign', 'secret', v_sys_uc);
    UPDATE claimius.samna_app SET home_prophet = 'sqltest_prune_peer' WHERE id = v_foreign;
END $$;

CREATE FUNCTION pg_temp.crew(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM crew WHERE key = p_key
$$ LANGUAGE sql;

SELECT is(claimius.get_app_home(pg_temp.crew('foreign')), 'sqltest_prune_peer', 'the foreign app is decided by its peer');

SELECT throws_ok(
    format('DELETE FROM claimius.code WHERE id = %L', pg_temp.crew('foreign_code')),
    'P0409', NULL, 'a direct delete of the foreign app''s code is refused outside replay'
);

SELECT lives_ok($$ SELECT claimius.prune('{"size_mb": 0}'::jsonb) $$, 'prune runs with an expired code of an app another prophet decides');

SELECT is((SELECT count(*) FROM claimius.code WHERE id = pg_temp.crew('home_code')), 0::BIGINT, 'the expired code of an app decided here is pruned');
SELECT is((SELECT count(*) FROM claimius.code WHERE id = pg_temp.crew('foreign_code')), 1::BIGINT, 'the expired code of an app another prophet decides remains');

SELECT * FROM finish();
ROLLBACK;
