BEGIN;
SELECT plan(12);

CREATE TEMP TABLE crew (key TEXT PRIMARY KEY, id UUID);

CREATE FUNCTION pg_temp.crew(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM crew WHERE key = p_key
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-0000000a55d0';
    v_org    UUID := '10000000-0000-4000-8000-0000000a55d1';
    v_reader UUID := 'a0000000-0000-4000-8000-0000000a55d1';
    v_claim  UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();

    PERFORM claimius.upsert_app_user(p_user_id => v_reader, p_first_name => 'Reader');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-0000000a55d2', p_first_name => 'Outsider');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_org, 'Asset crew', 'customer', v_org, v_org, 0, v_sys);

    v_claim := (claimius.insert_claim(
        p_name => 'Asset reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_claim, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_reader,
        p_sa_owner_id => v_org, p_sa_created_by => v_sys);

    INSERT INTO crew VALUES ('org', v_org), ('reader', v_reader), ('outsider', 'a0000000-0000-4000-8000-0000000a55d2'),
        ('org_link', '30000000-0000-4000-8000-0000000a55d1'), ('self_link', '30000000-0000-4000-8000-0000000a55d2');
END $$;

SELECT has_table('claimius', 'object_asset', 'object_asset exists on the disciple');

SELECT hasnt_column('claimius', 'object_asset', 'app_id', 'a disciple link carries no app_id');

SELECT col_not_null('claimius', 'object_asset', 'sa_created_by', 'a link always names the grant it was written under');

SELECT ok(EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'claimius' AND indexname = 'idx_object_asset_asset_object'
                   AND indexdef LIKE '%UNIQUE%(asset_id, object_type, object_id)%sa_deleted_at IS NULL%'),
    'an asset has one live link per object on the disciple');

SELECT ok(has_table_privilege('claimius_disciple_client', 'claimius.object_asset', 'INSERT')
      AND has_table_privilege('claimius_disciple_client', 'claimius.object_asset', 'UPDATE')
      AND has_table_privilege('claimius_disciple_client', 'claimius.object_asset', 'DELETE')
      AND has_function_privilege('claimius_reader', 'claimius.get_object_assets(uuid, text, uuid, text)', 'EXECUTE'),
    'the sync applies links and readers execute get_object_assets');

SELECT lives_ok($q$DO $$
BEGIN
    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
    INSERT INTO claimius.object_asset (id, object_type, object_id, asset_id, kind, mime_type, sa_owner_id, sa_created_by)
    VALUES (pg_temp.crew('org_link'), 'claimius.organization', pg_temp.crew('org'), gen_random_uuid(), 'logo', 'image/png', pg_temp.crew('org'), gen_random_uuid()),
           (pg_temp.crew('self_link'), 'claimius.samna_user', pg_temp.crew('reader'), gen_random_uuid(), 'profile', 'image/png', pg_temp.crew('reader'), gen_random_uuid())
    ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name;
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);
END $$$q$, 'the disciple applies a link upsert under replay');

SELECT is(
    ARRAY(SELECT id FROM claimius.get_object_assets(pg_temp.crew('reader'), 'claimius.organization', pg_temp.crew('org'))),
    ARRAY[pg_temp.crew('org_link')],
    'a reader with READ on the organization reads its link');

SELECT is_empty(
    format($$SELECT 1 FROM claimius.get_object_assets(%L, 'claimius.organization', %L)$$, pg_temp.crew('outsider'), pg_temp.crew('org')),
    'a reader without READ on the organization reads none of its links');

SELECT is(
    ARRAY(SELECT id FROM claimius.get_object_assets(pg_temp.crew('reader'), 'claimius.samna_user', pg_temp.crew('reader'), 'profile')),
    ARRAY[pg_temp.crew('self_link')],
    'a user reads its own profile link');

SELECT is_empty(
    format($$SELECT 1 FROM claimius.get_object_assets(%L, 'claimius.samna_user', %L)$$, pg_temp.crew('outsider'), pg_temp.crew('reader')),
    'a user who does not see another user reads none of its links');

SELECT lives_ok($q$DO $$
BEGIN
    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
    UPDATE claimius.object_asset SET name = 'Renamed' WHERE id = pg_temp.crew('org_link');
    DELETE FROM claimius.object_asset WHERE id = pg_temp.crew('org_link');
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);
END $$$q$, 'the disciple applies a link update and delete under replay');

SELECT is_empty(
    format($$SELECT 1 FROM claimius.get_object_assets(%L, 'claimius.organization', %L)$$, pg_temp.crew('reader'), pg_temp.crew('org')),
    'a deleted link is gone from the reader');

SELECT * FROM finish();
ROLLBACK;
