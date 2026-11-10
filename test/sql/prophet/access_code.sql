BEGIN;
SELECT plan(17);

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
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000c54', p_app_id => v_app, p_first_name => 'Viewer');
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

SELECT results_eq($$
    SELECT type::TEXT, first_name FROM claimius.samna_user WHERE user_id = claimius.anonymous_user_id() AND app_id = pg_temp.crew('app') AND sa_deleted_at IS NULL
$$, $$ VALUES ('anonymous', 'Public') $$, 'upsert_system_app writes the app''s anonymous user, type anonymous, named Public');

SELECT throws_ok($$
    SELECT claimius.insert_code(pg_temp.crew('app'), 'a0000000-0000-4000-8000-000000000c51', pg_temp.crew('reader_claim'), pg_temp.hash('some-value'), 'guest')
$$, '23514', NULL, 'invite is the only kind of code');

SELECT throws_ok($$
    SELECT claimius.insert_code(pg_temp.crew('app'), 'a0000000-0000-4000-8000-000000000c51', pg_temp.crew('reader_claim'), 'invite-value', 'invite')
$$, '22023', NULL, 'a value is refused where the hash belongs');

SELECT is_empty($$
    SELECT 1 FROM claimius.get_access(claimius.anonymous_user_id(), pg_temp.crew('app'), pg_temp.crew('org'), 'claimius.organization', 4)
$$, 'the anonymous user holds nothing before a grant');

SELECT lives_ok($$
    INSERT INTO crew SELECT 'public_uc', (claimius.insert_user_claim(p_app_id => pg_temp.crew('app'), p_claim_id => pg_temp.crew('reader_claim'), p_user_id => claimius.anonymous_user_id(),
                                      p_sa_owner_id => pg_temp.crew('org'), p_sa_created_by => pg_temp.crew('owner_uc')) -> 'user_claim' ->> 'id')::UUID
$$, 'an owner grants the claim to the anonymous user: the public grant');

SELECT throws_ok($$
    SELECT claimius.insert_user_claim(p_app_id => pg_temp.crew('app'), p_claim_id => pg_temp.crew('reader_claim'), p_user_id => 'a0000000-0000-4000-8000-000000000c54',
                                      p_sa_owner_id => pg_temp.crew('org'), p_sa_created_by => pg_temp.crew('owner_uc'),
                                      p_options => ('{"samk": {"hash": "' || pg_temp.hash('x') || '", "header": true}}')::jsonb)
$$, '22023', NULL, 'options are refused on a grant of any other user');

SELECT results_eq($$
    SELECT sa_access, user_claim_id FROM claimius.get_access(claimius.anonymous_user_id(), pg_temp.crew('app'), pg_temp.crew('org'), 'claimius.organization', 4)
$$, $$ VALUES (15, pg_temp.crew('owner_uc')) $$, 'the anonymous user holds the public grant with the mask of the claim and the token of the grant that made it public');

SELECT is_empty($$
    SELECT 1 FROM claimius.get_access('a0000000-0000-4000-8000-000000000c54', pg_temp.crew('app'), pg_temp.crew('org'), 'claimius.organization', 4)
$$, 'a public grant is the anonymous user''s and no one else''s');

SELECT results_eq($$
    SELECT object_id FROM claimius.get_objects(claimius.anonymous_user_id(), pg_temp.crew('app'), 'claimius.organization', 4)
$$, $$ VALUES (pg_temp.crew('org')) $$, 'the anonymous user lists what its grants reach');

SELECT is((SELECT count(*) FROM claimius.get_public_claims(pg_temp.crew('app'))), 1::BIGINT, 'the app''s public claims are the anonymous user''s grants with their bindings');

SELECT throws_ok($$
    SELECT claimius.update_user_claim(pg_temp.crew('public_uc'), p_options => '{"samk": {"hash": "short", "header": true}}'::jsonb)
$$, '22023', NULL, 'options outside the shape are refused');

SELECT lives_ok($$
    SELECT claimius.update_user_claim(pg_temp.crew('public_uc'), p_options => ('{"samk": {"hash": "' || pg_temp.hash('public-value') || '", "header": true}}')::jsonb)
$$, 'an owner puts a code on the public grant');

SELECT is_empty($$
    SELECT 1 FROM claimius.get_access(claimius.anonymous_user_id(), pg_temp.crew('app'), pg_temp.crew('org'), 'claimius.organization', 4)
$$, 'a grant behind a code serves no request that does not present it');

SELECT lives_ok($$ SELECT set_config('claimius.samk', pg_temp.hash('public-value'), TRUE) $$, 'the request presents the code in claimius.samk');

SELECT is((SELECT sa_access FROM claimius.get_access(claimius.anonymous_user_id(), pg_temp.crew('app'), pg_temp.crew('org'), 'claimius.organization', 4)),
          15, 'the presented code opens the grant in the walk');

SELECT lives_ok($$
    SELECT claimius.insert_code(pg_temp.crew('app'), 'a0000000-0000-4000-8000-000000000c51', pg_temp.crew('reader_claim'), pg_temp.hash('invite-value'), 'invite',
                                p_count => 1)
$$, 'an owner of the claim creates a single use invite');

SELECT is((claimius.redeem_code(pg_temp.crew('app'), pg_temp.hash('invite-value'), 'a0000000-0000-4000-8000-000000000c52') -> 'user_claim' ->> 'user_id')::UUID,
          'a0000000-0000-4000-8000-000000000c52'::UUID, 'the invite grants its claim to the user who redeems it');

SELECT * FROM finish();
ROLLBACK;
