BEGIN;
SELECT plan(9);

CREATE TEMP TABLE crew (key TEXT PRIMARY KEY, id UUID);

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-000000000e50';
    v_org    UUID := '10000000-0000-4000-8000-000000000e51';
    v_owner  UUID := 'a0000000-0000-4000-8000-000000000e51';
    v_reader UUID := 'a0000000-0000-4000-8000-000000000e52';
    v_own    UUID;
    v_read   UUID;
    v_member UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();

    PERFORM claimius.upsert_app_user(p_user_id => v_owner, p_first_name => 'Owner');
    PERFORM claimius.upsert_app_user(p_user_id => v_reader, p_first_name => 'Reader');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000e55', p_first_name => 'Outsider');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_org, 'Crew', 'company', v_org, v_org, 0, v_sys);

    v_own := (claimius.insert_claim(
        p_name => 'Crew owner', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_own, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_read := (claimius.insert_claim(
        p_name => 'Crew reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_read, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_member := (claimius.insert_claim(
        p_name => 'Crew member', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;

    INSERT INTO crew VALUES
        ('owner_uc', (claimius.insert_user_claim(p_claim_id => v_own, p_user_id => v_owner,
                         p_sa_owner_id => v_org, p_sa_created_by => v_sys) -> 'user_claim' ->> 'id')::UUID),
        ('owner_claim', v_own),
        ('reader_uc', (claimius.insert_user_claim(p_claim_id => v_read, p_user_id => v_reader,
                          p_sa_owner_id => v_org, p_sa_created_by => v_sys) -> 'user_claim' ->> 'id')::UUID),
        ('reader_claim', v_read),
        ('member', v_member);
END $$;

CREATE FUNCTION pg_temp.crew(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM crew WHERE key = p_key
$$ LANGUAGE sql;

SELECT lives_ok($$
    SELECT claimius.insert_secret('api_key', 'cipher', NULL, '10000000-0000-4000-8000-000000000e51', pg_temp.crew('owner_uc'))
$$, 'an owner of the organization stores a secret');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.claim_object co JOIN claimius.samna_secret s ON s.id = co.object_id
        WHERE s.key = 'api_key' AND co.object_type = 'claimius.samna_secret'
          AND co.claim_id = pg_temp.crew('owner_claim') AND co.sa_deleted_at IS NULL),
    'the secret is bound to the claim of the actor that stored it');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.get_secrets('a0000000-0000-4000-8000-000000000e51', 4) WHERE key = 'api_key'),
    'the actor lists the secret through get_secrets');

SELECT is_empty($$
    SELECT 1 FROM claimius.get_secrets('a0000000-0000-4000-8000-000000000e55', 4) WHERE key = 'api_key'
$$, 'a user without a claim reaching the secret does not list it');

SELECT is(claimius.get_secret_value('a0000000-0000-4000-8000-000000000e51', (SELECT id FROM claimius.samna_secret WHERE key = 'api_key')),
    'cipher', 'the creator reads the stored ciphertext through the claim insert_secret bound');

SELECT is(claimius.get_secret_value('a0000000-0000-4000-8000-000000000e52', (SELECT id FROM claimius.samna_secret WHERE key = 'api_key')),
    'cipher', 'a holder of a claim reaching the secret reads it');

SELECT is(claimius.get_secret_value('a0000000-0000-4000-8000-000000000e55', (SELECT id FROM claimius.samna_secret WHERE key = 'api_key')),
    NULL::TEXT, 'a user without a claim reaching the secret reads nothing');

SELECT is(claimius.get_secret_value('a0000000-0000-4000-8000-000000000e51', gen_random_uuid()),
    NULL::TEXT, 'an unknown secret reads nothing');

SELECT throws_ok($$
    SELECT claimius.insert_secret('stray', 'cipher', NULL, '10000000-0000-4000-8000-000000000e51', gen_random_uuid())
$$, 'P0001', 'created_by invalid', 'a secret without a live actor is refused');

SELECT * FROM finish();
ROLLBACK;
