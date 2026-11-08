BEGIN;
SELECT plan(5);

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
    PERFORM claimius.init();

    PERFORM claimius.ensure_app_user(p_user_id => v_owner, p_first_name => 'Owner');
    PERFORM claimius.ensure_app_user(p_user_id => v_reader, p_first_name => 'Reader');
    PERFORM claimius.ensure_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000e55', p_first_name => 'Outsider');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_org, 'Crew', 'company', v_org, v_org, 0, v_sys);

    v_own := (claimius.create_claim(
        p_name => 'Crew owner', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_claim_id => v_own, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_read := (claimius.create_claim(
        p_name => 'Crew reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_claim_id => v_read, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_member := (claimius.create_claim(
        p_name => 'Crew member', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;

    INSERT INTO crew VALUES
        ('owner_uc', (claimius.assign_claim_user(p_claim_id => v_own, p_user_id => v_owner,
                         p_sa_owner_id => v_org, p_sa_created_by => v_sys) -> 'user_claim' ->> 'id')::UUID),
        ('owner_claim', v_own),
        ('reader_uc', (claimius.assign_claim_user(p_claim_id => v_read, p_user_id => v_reader,
                          p_sa_owner_id => v_org, p_sa_created_by => v_sys) -> 'user_claim' ->> 'id')::UUID),
        ('reader_claim', v_read),
        ('member', v_member);
END $$;

CREATE FUNCTION pg_temp.crew(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM crew WHERE key = p_key
$$ LANGUAGE sql;

SELECT lives_ok($$
    SELECT claimius.create_organization_user(pg_temp.crew('owner_uc'), pg_temp.crew('owner_claim'), '10000000-0000-4000-8000-000000000e51',
        jsonb_build_object('user_id', 'a0000000-0000-4000-8000-000000000e53', 'first_name', 'New', 'claim_id', pg_temp.crew('member')))
$$, 'an owner of the organization creates a user in it');

SELECT ok(EXISTS (
        SELECT 1 FROM claimius.claim_object co JOIN claimius.claim c ON c.id = co.claim_id
        WHERE co.object_type = 'claimius.samna_user' AND co.object_id = 'a0000000-0000-4000-8000-000000000e53'
          AND c.type = 'virtual' AND co.sa_created_by = pg_temp.crew('owner_uc') AND co.sa_deleted_at IS NULL),
    'the new user''s virtual claim is bound on the user under the owner''s user_claim');

SELECT ok(EXISTS (SELECT 1 FROM claimius.get_user_access('a0000000-0000-4000-8000-000000000e51',
        'a0000000-0000-4000-8000-000000000e53', 15)),
    'the owner holds the new user in full');

SELECT throws_like($$
    SELECT claimius.create_organization_user(pg_temp.crew('reader_uc'), pg_temp.crew('reader_claim'), '10000000-0000-4000-8000-000000000e51',
        jsonb_build_object('user_id', 'a0000000-0000-4000-8000-000000000e54', 'first_name', 'Refused', 'claim_id', pg_temp.crew('member')))
$$, 'cannot grant more access than held%', 'a reader of the organization is refused a user in it');

SELECT throws_like($$
    SELECT claimius.assign_claim_object(
        p_claim_id => pg_temp.crew('owner_claim'),
        p_sa_owner_id => '10000000-0000-4000-8000-000000000e51', p_sa_root_id => '10000000-0000-4000-8000-000000000e51',
        p_sa_created_by => pg_temp.crew('owner_uc'),
        p_object_id => 'a0000000-0000-4000-8000-000000000e55', p_object_type => 'claimius.samna_user')
$$, 'cannot grant more access than held%', 'an owner binding on a user holding no claim of the organization is refused');

SELECT * FROM finish();
ROLLBACK;
