BEGIN;
SELECT plan(18);

CREATE TEMP TABLE crew (key TEXT PRIMARY KEY, id UUID);

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-000000000c50';
    v_org    UUID := '10000000-0000-4000-8000-000000000c51';
    v_hidden UUID := '10000000-0000-4000-8000-000000000c5f';
    v_owner  UUID := 'a0000000-0000-4000-8000-000000000c51';
    v_own    UUID;
    v_read   UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();

    PERFORM claimius.upsert_app_user(p_user_id => v_owner, p_first_name => 'Owner');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000c52', p_first_name => 'Invited');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000c53', p_first_name => 'Late');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000c55', p_first_name => 'Outsider');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_org, 'Codes', 'company', v_org, v_org, 0, v_sys),
           (v_hidden, 'Hidden', 'company', v_hidden, v_hidden, 0, v_sys);

    v_own := (claimius.insert_claim(
        p_name => 'Code owner', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_own, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_read := (claimius.insert_claim(
        p_name => 'Code reader', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_read, p_sa_owner_id => v_org, p_sa_root_id => v_org, p_sa_created_by => v_sys,
        p_object_id => v_org, p_object_type => 'claimius.organization', p_inherits => TRUE);

    INSERT INTO crew VALUES
        ('org', v_org),
        ('hidden', v_hidden),
        ('owner_uc', (claimius.insert_user_claim(p_claim_id => v_own, p_user_id => v_owner,
                         p_sa_owner_id => v_org, p_sa_created_by => v_sys) -> 'user_claim' ->> 'id')::UUID),
        ('reader_claim', v_read);
END $$;

CREATE FUNCTION pg_temp.crew(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM crew WHERE key = p_key
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.hash(p_value TEXT) RETURNS TEXT AS $$
    SELECT encode(digest(p_value, 'sha256'), 'hex')
$$ LANGUAGE sql;

SELECT throws_ok($$
    SELECT claimius.insert_code('a0000000-0000-4000-8000-000000000c51', pg_temp.crew('reader_claim'), pg_temp.hash('guest-value'), 'guest')
$$, '22023', NULL, 'a guest code needs a guest claim');

SELECT lives_ok($$
    SELECT claimius.update_claim_guest(pg_temp.crew('reader_claim'), 'a0000000-0000-4000-8000-000000000c51', 'code')
$$, 'an owner of the claim makes it a guest claim in code mode');

INSERT INTO crew
SELECT 'guest', uc.user_id
FROM claimius.user_claim uc
JOIN claimius.samna_user u ON u.user_id = uc.user_id AND u.type = 'guest'
WHERE uc.claim_id = pg_temp.crew('reader_claim') AND uc.sa_deleted_at IS NULL;

SELECT isnt(pg_temp.crew('guest'), NULL, 'the guest claim has a guest holding it');

SELECT throws_ok($$
    SELECT claimius.insert_code('a0000000-0000-4000-8000-000000000c51', pg_temp.crew('reader_claim'), 'guest-value', 'guest')
$$, '22023', NULL, 'a value is refused where the hash belongs');

SELECT throws_ok($$
    SELECT claimius.insert_code('a0000000-0000-4000-8000-000000000c55', pg_temp.crew('reader_claim'), pg_temp.hash('guest-value'), 'guest')
$$, 'P0403', NULL, 'a user who does not own the claim creates no code');

SELECT lives_ok($$
    SELECT claimius.insert_code('a0000000-0000-4000-8000-000000000c51', pg_temp.crew('reader_claim'), pg_temp.hash('guest-value'), 'guest', p_count => 1)
$$, 'an owner of the claim creates a guest code by its hash');

SELECT ok(NOT EXISTS (SELECT 1 FROM claimius.code WHERE to_jsonb(code)::TEXT LIKE '%guest-value%'), 'the value is stored nowhere');

SELECT is((claimius.get_code(pg_temp.hash('guest-value')) ->> 'guest_user_id')::UUID, pg_temp.crew('guest'), 'the code resolves to the guest of its claim');

SELECT is((SELECT count(*) FROM claimius.update_code_used(pg_temp.hash('guest-value'))), 1::BIGINT, 'the first use of a single use code passes');
SELECT is((SELECT count(*) FROM claimius.update_code_used(pg_temp.hash('guest-value'))), 0::BIGINT, 'the second use finds nothing');

SELECT is_empty($$
    SELECT 1 FROM claimius.get_access(pg_temp.crew('guest'), pg_temp.crew('org'), 'claimius.organization', 2)
$$, 'under the default guest ceiling a guest write is refused, whatever its claim holds');

SELECT is((SELECT sa_access FROM claimius.get_access(pg_temp.crew('guest'), pg_temp.crew('org'), 'claimius.organization', 4)),
          4, 'under the default guest ceiling a guest holds READ only');

SELECT is_empty($$
    SELECT 1 FROM claimius.get_guest_access('claimius.organization', pg_temp.crew('org'))
$$, 'a guest claim in code mode serves no request without a credential');

SELECT lives_ok($$
    SELECT claimius.update_claim_guest(pg_temp.crew('reader_claim'), 'a0000000-0000-4000-8000-000000000c51', 'open')
$$, 'an owner opens the guest claim');

SELECT results_eq($$
    SELECT user_id, sa_access FROM claimius.get_guest_access('claimius.organization', pg_temp.crew('org'))
$$, $$ VALUES (pg_temp.crew('guest'), 4) $$, 'an open guest claim serves its object as its guest, held to the default guest ceiling');

SELECT is_empty($$
    SELECT 1 FROM claimius.get_guest_access('claimius.organization', pg_temp.crew('hidden'))
$$, 'an object no open guest claim covers is served to no one');

SELECT lives_ok($$
    SELECT claimius.insert_code('a0000000-0000-4000-8000-000000000c51', pg_temp.crew('reader_claim'), pg_temp.hash('invite-value'), 'invite', p_count => 1)
$$, 'an owner of the claim creates a single use invite');

SELECT throws_ok($$
    SELECT claimius.redeem_code(pg_temp.hash('invite-value'), 'a0000000-0000-4000-8000-000000000c52'),
           claimius.redeem_code(pg_temp.hash('invite-value'), 'a0000000-0000-4000-8000-000000000c53')
$$, 'P0403', NULL, 'a single use invite grants once');

SELECT * FROM finish();
ROLLBACK;
