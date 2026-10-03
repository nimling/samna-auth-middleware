BEGIN;
SELECT plan(10);

CREATE TEMP TABLE stamp (key TEXT PRIMARY KEY, id UUID);

CREATE FUNCTION pg_temp.stamp(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM stamp WHERE key = p_key
$$ LANGUAGE sql;

DO $$
DECLARE
    v_app     UUID;
    v_sys_uc  UUID;
    v_root    UUID := '10000000-0000-4000-8000-000000000e01';
    v_ada     UUID := 'a0000000-0000-4000-8000-000000000e01';
    v_bo      UUID := 'a0000000-0000-4000-8000-000000000e02';
    v_cy      UUID := 'a0000000-0000-4000-8000-000000000e03';
    v_dan     UUID := 'a0000000-0000-4000-8000-000000000e04';
    v_ev      UUID := 'a0000000-0000-4000-8000-000000000e05';
    v_init    JSONB;
    v_crew    UUID;
    v_planted UUID;
    v_stray   UUID;
BEGIN
    v_init := claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_stamp',
        p_system_app_name         => 'SQL stamp test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_system_user_id          => '00000000-0000-4000-8000-000000000e00',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_app    := (v_init -> 'samna_app' ->> 'id')::UUID;
    v_sys_uc := (v_init -> 'user_claim' ->> 'id')::UUID;
    INSERT INTO stamp VALUES ('app', v_app), ('sys_uc', v_sys_uc), ('root', v_root);

    PERFORM claimius.upsert_root_organization(v_app, 'Stamp root', v_sys_uc, p_id => v_root);

    INSERT INTO stamp VALUES
        ('ada_uc', (claimius.upsert_app_user(p_user_id => v_ada, p_app_id => v_app, p_first_name => 'Ada') -> 'user_claim' ->> 'id')::UUID),
        ('bo_uc', (claimius.upsert_app_user(p_user_id => v_bo, p_app_id => v_app, p_first_name => 'Bo') -> 'user_claim' ->> 'id')::UUID),
        ('cy_uc', (claimius.upsert_app_user(p_user_id => v_cy, p_app_id => v_app, p_first_name => 'Cy') -> 'user_claim' ->> 'id')::UUID),
        ('dan_uc', (claimius.upsert_app_user(p_user_id => v_dan, p_app_id => v_app, p_first_name => 'Dan', p_sa_created_by => v_sys_uc) -> 'user_claim' ->> 'id')::UUID),
        ('ev_uc', (claimius.upsert_app_user(p_user_id => v_ev, p_app_id => v_app, p_first_name => 'Ev') -> 'user_claim' ->> 'id')::UUID);

    v_crew := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Stamp crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys_uc,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_bo,
        p_sa_owner_id => v_root, p_sa_created_by => v_sys_uc);
    UPDATE claimius.user_claim SET sa_deleted_at = now() WHERE app_id = v_app AND user_id = v_cy;

    v_planted := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Stamp planted', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    v_stray := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Stamp stray', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    INSERT INTO stamp VALUES ('crew', v_crew), ('planted', v_planted), ('stray', v_stray);

    UPDATE claimius.user_claim SET sa_created_by = v_ada WHERE id = pg_temp.stamp('ada_uc');
    UPDATE claimius.claim SET sa_created_by = v_ada WHERE id = v_planted;
    UPDATE claimius.organization SET sa_created_by = v_ada WHERE id = v_root;
    UPDATE claimius.samna_app SET sa_created_by = v_ada WHERE id = v_app;
    UPDATE claimius.claim_object SET sa_created_by = v_bo WHERE claim_id = v_crew;
    UPDATE claimius.claim SET sa_created_by = v_cy WHERE id = v_crew;
    UPDATE claimius.claim SET sa_created_by = '99999999-0000-4000-8000-000000000e09' WHERE id = v_stray;
END $$;

SELECT is((SELECT sa_created_by FROM claimius.user_claim WHERE id = pg_temp.stamp('ev_uc')), pg_temp.stamp('ev_uc'),
    'upsert_app_user stamps the default claim of a first login with the new user_claim''s own id');
SELECT is((SELECT sa_created_by FROM claimius.user_claim WHERE id = pg_temp.stamp('dan_uc')), pg_temp.stamp('sys_uc'),
    'upsert_app_user stamps it with the acting user_claim.id a caller passes');

SELECT cmp_ok(claimius._restamp_created_by(), '>=', 4, 'the upgrade rewrites the stamps a v1.1.9 set wrote as user ids');

SELECT is((SELECT sa_created_by FROM claimius.user_claim WHERE id = pg_temp.stamp('ada_uc')), pg_temp.stamp('ada_uc'),
    'the self stamp of a first login becomes the one user_claim the user holds, its own');
SELECT is((SELECT sa_created_by FROM claimius.claim WHERE id = pg_temp.stamp('planted')), pg_temp.stamp('ada_uc'),
    'a claim stamped with a user holding one live user_claim in its app takes that user_claim.id');
SELECT is((SELECT sa_created_by FROM claimius.organization WHERE id = pg_temp.stamp('root')), pg_temp.stamp('ada_uc'),
    'an organization takes it through the app it is linked to');
SELECT is((SELECT sa_created_by FROM claimius.samna_app WHERE id = pg_temp.stamp('app')), pg_temp.stamp('ada_uc'),
    'a samna_app takes it in its own app');
SELECT is((SELECT sa_created_by FROM claimius.claim_object WHERE claim_id = pg_temp.stamp('crew') LIMIT 1),
    'a0000000-0000-4000-8000-000000000e02'::UUID,
    'a stamp whose user holds two live user_claims in the app is left untouched');
SELECT is((SELECT ARRAY[c.sa_created_by, s.sa_created_by] FROM claimius.claim c, claimius.claim s
           WHERE c.id = pg_temp.stamp('crew') AND s.id = pg_temp.stamp('stray')),
    ARRAY['a0000000-0000-4000-8000-000000000e03', '99999999-0000-4000-8000-000000000e09']::UUID[],
    'a stamp whose user holds no live user_claim, and one naming no user, are left untouched');

SELECT is(claimius._restamp_created_by(), 0, 'a second run rewrites nothing');

SELECT * FROM finish();
ROLLBACK;
