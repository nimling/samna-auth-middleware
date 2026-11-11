BEGIN;
SELECT plan(9);

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
        p_system_app_slug         => 'sqltest_guest',
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
    SELECT claimius.upsert_peer_grant((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_guest'), 'sqltest_mirror', 'peer-secret', pg_temp.crew('owner_uc'))
$$, 'the home grants a peer prophet its app');

SELECT is(
    (SELECT prophet FROM claimius.get_peer_grant((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_guest'), 'peer-secret')),
    'sqltest_mirror',
    'the peer secret finds the grant'
);

SELECT is_empty($$
    SELECT 1 FROM claimius.get_peer_grant((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_guest'), 'wrong-secret')
$$, 'another secret finds no grant');

SELECT is_empty($$
    SELECT 1 FROM claimius.get_peer_grant((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_guest'), NULL)
$$, 'no secret finds no grant');

SELECT ok(
    NOT EXISTS (SELECT 1 FROM claimius.peer_grant WHERE secret_hash = 'peer-secret'),
    'the raw peer secret is never stored'
);

SELECT lives_ok($$
    SELECT claimius.upsert_guest_user((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_guest'), pg_temp.crew('member'), 'a0000000-0000-4000-8000-000000000e51', 'a0000000-0000-4000-8000-0000000000f1')
$$, 'an owner of the claim makes its guest');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.samna_user u JOIN claimius.user_claim uc ON uc.user_id = u.user_id AND uc.app_id = u.app_id
        WHERE u.user_id = 'a0000000-0000-4000-8000-0000000000f1' AND u.app_id = (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_guest') AND u.type = 'guest'
          AND uc.claim_id = pg_temp.crew('member') AND uc.sa_created_by = pg_temp.crew('owner_uc') AND uc.sa_deleted_at IS NULL),
    'the guest is a guest user of the app holding the claim under the owner''s grant');

SELECT lives_ok($$
    SELECT claimius.upsert_guest_user((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_guest'), pg_temp.crew('member'), 'a0000000-0000-4000-8000-000000000e51', 'a0000000-0000-4000-8000-0000000000f1')
$$, 'making the same guest again is a no op');

SELECT throws_ok($$
    SELECT claimius.upsert_guest_user((SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_guest'), pg_temp.crew('member'), 'a0000000-0000-4000-8000-000000000e55', 'a0000000-0000-4000-8000-0000000000f2')
$$, 'P0403', NULL, 'a user who does not own the claim makes no guest');

SELECT * FROM finish();
ROLLBACK;
