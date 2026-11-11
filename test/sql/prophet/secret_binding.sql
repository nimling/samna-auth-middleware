BEGIN;
SELECT plan(10);

CREATE TEMP TABLE crew (key TEXT PRIMARY KEY, id UUID);

DO $$
DECLARE
    v_init   JSONB;
    v_app    UUID;
    v_sys_uc UUID;
    v_org    UUID := '10000000-0000-4000-8000-000000000e51';
    v_owner  UUID := 'a0000000-0000-4000-8000-000000000e51';
    v_reader UUID := 'a0000000-0000-4000-8000-000000000e52';
    v_own    UUID;
    v_read   UUID;
    v_member UUID;
BEGIN
    v_init := claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_vault',
        p_system_app_name         => 'SQL crew test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_app    := (v_init -> 'samna_app' ->> 'id')::UUID;
    v_sys_uc := (v_init -> 'user_claim' ->> 'id')::UUID;

    PERFORM claimius.upsert_root_organization(v_app, 'Crew', v_sys_uc, p_id => v_org);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-000000000e5f', 'Unlinked', 'customer', '10000000-0000-4000-8000-000000000e5f',
            '10000000-0000-4000-8000-000000000e5f', 0, v_sys_uc);
    PERFORM claimius.upsert_app_user(p_user_id => v_owner, p_app_id => v_app, p_first_name => 'Owner');
    PERFORM claimius.upsert_app_user(p_user_id => v_reader, p_app_id => v_app, p_first_name => 'Reader');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000e55', p_app_id => v_app, p_first_name => 'Outsider');

    v_own := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Crew owner', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_own, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_read := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Crew reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_read, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_member := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Crew member', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;

    INSERT INTO crew VALUES
        ('owner_uc', (claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_own, p_user_id => v_owner,
                         p_sa_owner_id => v_org, p_sa_created_by => v_sys_uc) -> 'user_claim' ->> 'id')::UUID),
        ('owner_claim', v_own),
        ('reader_uc', (claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_read, p_user_id => v_reader,
                          p_sa_owner_id => v_org, p_sa_created_by => v_sys_uc) -> 'user_claim' ->> 'id')::UUID),
        ('reader_claim', v_read),
        ('member', v_member);
END $$;

CREATE FUNCTION pg_temp.crew(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM crew WHERE key = p_key
$$ LANGUAGE sql;

SELECT lives_ok($$
    SELECT claimius.insert_secret((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_vault'), 'api_key', 'cipher', NULL, '10000000-0000-4000-8000-000000000e51', pg_temp.crew('owner_uc'))
$$, 'an owner of the organization stores a secret of the app');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.claim_object co JOIN claimius.samna_secret s ON s.id = co.object_id
        WHERE s.key = 'api_key' AND co.object_type = 'claimius.samna_secret'
          AND co.claim_id = pg_temp.crew('owner_claim') AND co.sa_deleted_at IS NULL),
    'the secret is bound to the claim of the actor that stored it');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.get_secrets('a0000000-0000-4000-8000-000000000e51', (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_vault'), 4)
        WHERE key = 'api_key'),
    'the actor lists the secret through get_secrets');

SELECT is_empty($$
    SELECT 1 FROM claimius.get_secrets('a0000000-0000-4000-8000-000000000e55', (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_vault'), 4)
    WHERE key = 'api_key'
$$, 'a user without a claim reaching the secret does not list it');

SELECT is((SELECT value FROM claimius.get_secret_value('a0000000-0000-4000-8000-000000000e51', (SELECT id FROM claimius.samna_secret WHERE key = 'api_key'))),
    'cipher', 'the creator reads the stored ciphertext through the claim insert_secret bound');

SELECT is((SELECT app_id FROM claimius.get_secret_value('a0000000-0000-4000-8000-000000000e52', (SELECT id FROM claimius.samna_secret WHERE key = 'api_key'))),
    (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_vault'), 'a holder of a claim reaching the secret reads it with the app that wrote it');

SELECT is_empty($$
    SELECT 1 FROM claimius.get_secret_value('a0000000-0000-4000-8000-000000000e55', (SELECT id FROM claimius.samna_secret WHERE key = 'api_key'))
$$, 'a user without a claim reaching the secret reads nothing');

SELECT is_empty($$
    SELECT 1 FROM claimius.get_secret_value('a0000000-0000-4000-8000-000000000e51', gen_random_uuid())
$$, 'an unknown secret reads nothing');

SELECT throws_ok($$
    SELECT claimius.insert_secret((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_vault'), 'stray', 'cipher', NULL, '10000000-0000-4000-8000-000000000e51', gen_random_uuid())
$$, 'P0001', 'created_by invalid', 'a secret without a live actor of the app is refused');

SELECT throws_ok($$
    SELECT claimius.insert_secret((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_vault'), 'stray', 'cipher', NULL, '10000000-0000-4000-8000-000000000e5f', pg_temp.crew('owner_uc'))
$$, 'P0001', 'owner_id invalid', 'a secret owned by an organization not linked to the app is refused');

SELECT * FROM finish();
ROLLBACK;
