BEGIN;
SELECT plan(16);

CREATE TEMP TABLE crew (key TEXT PRIMARY KEY, id UUID);

DO $$
DECLARE
    v_init   JSONB;
    v_app    UUID;
    v_sys_uc UUID;
    v_org    UUID := '10000000-0000-4000-8000-000000000c51';
    v_owner  UUID := 'a0000000-0000-4000-8000-000000000c51';
    v_own    UUID;
    v_read   UUID;
BEGIN
    v_init := claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_code',
        p_system_app_name         => 'SQL code test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_app    := (v_init -> 'samna_app' ->> 'id')::UUID;
    v_sys_uc := (v_init -> 'user_claim' ->> 'id')::UUID;

    PERFORM claimius.upsert_root_organization(v_app, 'Codes', v_sys_uc, p_id => v_org);
    PERFORM claimius.upsert_app_user(p_user_id => v_owner, p_app_id => v_app, p_first_name => 'Owner');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000c52', p_app_id => v_app, p_first_name => 'Invited');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000c53', p_app_id => v_app, p_first_name => 'Late');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000c55', p_app_id => v_app, p_first_name => 'Outsider');

    v_own := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Code owner', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_own, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_read := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Code reader', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_read, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys_uc,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);

    INSERT INTO crew VALUES
        ('app', v_app),
        ('org', v_org),
        ('owner_uc', (claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_own, p_user_id => v_owner,
                         p_sa_owner_id => v_org, p_sa_created_by => v_sys_uc) -> 'user_claim' ->> 'id')::UUID),
        ('reader_claim', v_read);
END $$;

CREATE FUNCTION pg_temp.crew(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM crew WHERE key = p_key
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.hash(p_value TEXT) RETURNS TEXT AS $$
    SELECT encode(digest(p_value, 'sha256'), 'hex')
$$ LANGUAGE sql;

SELECT throws_ok($$
    SELECT claimius.insert_code(pg_temp.crew('app'), 'a0000000-0000-4000-8000-000000000c51', pg_temp.crew('reader_claim'), pg_temp.hash('guest-value'), 'guest')
$$, '22023', NULL, 'a guest code needs a guest claim');

SELECT lives_ok($$
    SELECT claimius.update_claim_guest(pg_temp.crew('app'), pg_temp.crew('reader_claim'), 'a0000000-0000-4000-8000-000000000c51',
                                       'a0000000-0000-4000-8000-0000000000c9', 'code')
$$, 'an owner of the claim makes it a guest claim in code mode');

SELECT is((SELECT guest_mode FROM claimius.claim WHERE id = pg_temp.crew('reader_claim')), 'code', 'the claim holds its guest mode');

SELECT throws_ok($$
    SELECT claimius.insert_code(pg_temp.crew('app'), 'a0000000-0000-4000-8000-000000000c51', pg_temp.crew('reader_claim'), 'guest-value', 'guest')
$$, '22023', NULL, 'a value is refused where the hash belongs');

SELECT throws_ok($$
    SELECT claimius.insert_code(pg_temp.crew('app'), 'a0000000-0000-4000-8000-000000000c55', pg_temp.crew('reader_claim'), pg_temp.hash('guest-value'), 'guest')
$$, 'P0403', NULL, 'a user who does not own the claim creates no code');

SELECT lives_ok($$
    SELECT claimius.insert_code(pg_temp.crew('app'), 'a0000000-0000-4000-8000-000000000c51', pg_temp.crew('reader_claim'), pg_temp.hash('guest-value'), 'guest',
                                p_count => 1)
$$, 'an owner of the claim creates a guest code by its hash');

SELECT ok(NOT EXISTS (SELECT 1 FROM claimius.code WHERE to_jsonb(code)::TEXT LIKE '%guest-value%'), 'the value is stored nowhere');

SELECT is((claimius.get_code(pg_temp.crew('app'), pg_temp.hash('guest-value')) ->> 'guest_user_id')::UUID,
          'a0000000-0000-4000-8000-0000000000c9'::UUID, 'the code resolves to the guest of its claim');

SELECT is((SELECT count(*) FROM claimius.update_code_used(pg_temp.crew('app'), pg_temp.hash('guest-value'))), 1::BIGINT, 'the first use of a single use code passes');
SELECT is((SELECT count(*) FROM claimius.update_code_used(pg_temp.crew('app'), pg_temp.hash('guest-value'))), 0::BIGINT, 'the second use finds nothing');

SELECT isnt_empty($$
    SELECT 1 FROM claimius.get_access('a0000000-0000-4000-8000-0000000000c9', pg_temp.crew('app'), pg_temp.crew('org'), 'claimius.organization', 2)
$$, 'a guest writes what its claim grants');

SELECT is((SELECT sa_access FROM claimius.get_access('a0000000-0000-4000-8000-0000000000c9', pg_temp.crew('app'), pg_temp.crew('org'), 'claimius.organization', 4)),
          15, 'a guest holds exactly what its claim grants');

SELECT lives_ok($$
    SELECT claimius.insert_code(pg_temp.crew('app'), 'a0000000-0000-4000-8000-000000000c51', pg_temp.crew('reader_claim'), pg_temp.hash('invite-value'), 'invite',
                                p_count => 1)
$$, 'an owner of the claim creates a single use invite');

SELECT is((claimius.redeem_code(pg_temp.crew('app'), pg_temp.hash('invite-value'), 'a0000000-0000-4000-8000-000000000c52') -> 'user_claim' ->> 'user_id')::UUID,
          'a0000000-0000-4000-8000-000000000c52'::UUID, 'the invite grants its claim to the user who redeems it');

SELECT throws_ok($$
    SELECT claimius.redeem_code(pg_temp.crew('app'), pg_temp.hash('invite-value'), 'a0000000-0000-4000-8000-000000000c53')
$$, 'P0403', NULL, 'a used up invite grants nothing more');

SELECT ok((claimius.delete_code(pg_temp.crew('app'), 'a0000000-0000-4000-8000-000000000c51',
                               (SELECT id FROM claimius.code WHERE hash = pg_temp.hash('guest-value'))) -> 'code' ->> 'revoked_at') IS NOT NULL,
          'an owner of the claim revokes a code');

SELECT * FROM finish();
ROLLBACK;
