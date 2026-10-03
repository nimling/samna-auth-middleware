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
        p_system_app_slug         => 'sqltest_crew',
        p_system_app_name         => 'SQL crew test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_app    := (v_init -> 'samna_app' ->> 'id')::UUID;
    v_sys_uc := (v_init -> 'user_claim' ->> 'id')::UUID;

    PERFORM claimius.upsert_root_organization(v_app, 'Crew', v_sys_uc, p_id => v_org);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-000000000e5f', 'Unlinked', 'company', '10000000-0000-4000-8000-000000000e5f',
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
    SELECT claimius.insert_organization_user((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_crew'), pg_temp.crew('owner_uc'), '10000000-0000-4000-8000-000000000e51',
        jsonb_build_object('user_id', 'a0000000-0000-4000-8000-000000000e53', 'first_name', 'New', 'claim_id', pg_temp.crew('member')))
$$, 'an owner of the organization creates a user in it');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.claim_object co JOIN claimius.claim c ON c.id = co.claim_id
        WHERE co.object_type = 'claimius.samna_user' AND co.object_id = 'a0000000-0000-4000-8000-000000000e53'
          AND c.type = 'virtual' AND co.sa_created_by = pg_temp.crew('owner_uc') AND co.sa_deleted_at IS NULL),
    'the new user''s virtual claim is bound on the user under the owner''s user_claim');

SELECT ok(EXISTS (SELECT 1 FROM claimius.get_user_access('a0000000-0000-4000-8000-000000000e51',
        (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_crew'), 'a0000000-0000-4000-8000-000000000e53', 15)),
    'the owner holds the new user in full');

SELECT throws_like($$
    SELECT claimius.insert_organization_user((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_crew'), pg_temp.crew('reader_uc'), '10000000-0000-4000-8000-000000000e51',
        jsonb_build_object('user_id', 'a0000000-0000-4000-8000-000000000e54', 'first_name', 'Refused', 'claim_id', pg_temp.crew('member')))
$$, 'cannot grant more access than held%', 'a reader of the organization is refused a user in it');

SELECT throws_like($$
    SELECT claimius.insert_claim_object(
        p_app_id => (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_crew'), p_claim_id => pg_temp.crew('owner_claim'),
        p_sa_owner_id => '10000000-0000-4000-8000-000000000e51', p_sa_root_id => '10000000-0000-4000-8000-000000000e51',
        p_sa_created_by => pg_temp.crew('owner_uc'),
        p_object_id => 'a0000000-0000-4000-8000-000000000e55', p_object_type => 'claimius.samna_user')
$$, 'cannot grant more access than held%', 'an owner binding on a user holding no claim of the organization is refused');

CREATE FUNCTION pg_temp.identity_user() RETURNS JSONB AS $$
    SELECT claimius.insert_organization_user(
        (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_crew'), pg_temp.crew('owner_uc'), '10000000-0000-4000-8000-000000000e51',
        jsonb_build_object('first_name', 'Linked', 'claim_id', pg_temp.crew('member')),
        p_identity => jsonb_build_object('external_id', 'ext-e5', 'id_claim', 'sub', 'type', 'token', 'data', '{}'::JSONB),
        p_reason => 'Invited')
$$ LANGUAGE sql;

CREATE TEMP TABLE first_identity AS SELECT pg_temp.identity_user() AS result;
CREATE TEMP TABLE second_identity AS SELECT pg_temp.identity_user() AS result;

SELECT is((SELECT (result ->> 'created')::BOOLEAN FROM first_identity), TRUE,
    'the first call for an outside identity creates its user');

SELECT is((SELECT (result ->> 'created')::BOOLEAN FROM second_identity), FALSE,
    'the second call for the same identity finds that user');

SELECT is((SELECT result -> 'samna_user' ->> 'user_id' FROM second_identity),
          (SELECT result -> 'samna_user' ->> 'user_id' FROM first_identity),
    'both calls name the same user');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.user_token t JOIN claimius.user_claim uc ON uc.user_id = t.user_id AND uc.app_id = t.app_id
        WHERE t.external_id = 'ext-e5' AND t.app_id = (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_crew')
          AND t.user_id = (SELECT (result -> 'samna_user' ->> 'user_id')::UUID FROM first_identity)
          AND uc.claim_id = pg_temp.crew('member') AND uc.reason = 'Invited' AND t.sa_deleted_at IS NULL),
    'the identity is recorded in the app and the user holds the claim with its reason');

SELECT throws_like($$
    SELECT claimius.insert_organization_user((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_crew'), pg_temp.crew('owner_uc'),
        '10000000-0000-4000-8000-000000000e5f',
        jsonb_build_object('first_name', 'Nowhere', 'claim_id', pg_temp.crew('member')))
$$, 'owner_id invalid', 'an organization not linked to the app takes no user');

SELECT * FROM finish();
ROLLBACK;
