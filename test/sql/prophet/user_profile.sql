BEGIN;
SELECT plan(10);

SELECT has_column('claimius', 'samna_user', 'profile', 'samna_user carries profile');
SELECT col_type_is('claimius', 'samna_user', 'profile', 'jsonb', 'profile is jsonb');
SELECT col_default_is('claimius', 'samna_user', 'profile', '''{}''::jsonb', 'profile starts empty');
SELECT hasnt_table('claimius', 'profile_item', 'the profile item table is gone');

CREATE TEMP TABLE boot (key TEXT PRIMARY KEY, id UUID);

DO $$
DECLARE
    v_init JSONB;
BEGIN
    v_init := claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_profile',
        p_system_app_name         => 'SQL profile test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_system_user_id          => '00000000-0000-4000-8000-000000000e00',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    INSERT INTO boot VALUES ('app', (v_init -> 'samna_app' ->> 'id')::UUID);
    PERFORM claimius.upsert_app_user(
        p_user_id => 'a0000000-0000-4000-8000-000000000e01',
        p_app_id => (v_init -> 'samna_app' ->> 'id')::UUID,
        p_first_name => 'Profile'
    );
END $$;

SELECT is(
    claimius.update_profile('a0000000-0000-4000-8000-000000000e01', '{"bio": {"schema": {"type": "string", "format": "multiline"}, "value": "Builds access control"}}'::jsonb),
    '{"bio": {"schema": {"type": "string", "format": "multiline"}, "value": "Builds access control"}}'::jsonb,
    'the user writes an own entry with its schema and value'
);

SELECT is(
    claimius.upsert_profile_item((SELECT id FROM boot WHERE key = 'app'), 'a0000000-0000-4000-8000-000000000e01', 'certificate', '{"schema": {"type": "string", "title": "Certificate"}, "value": "Solutions Architect"}'::jsonb),
    '{"bio": {"schema": {"type": "string", "format": "multiline"}, "value": "Builds access control"}, "sqltest_profile.certificate": {"schema": {"type": "string", "title": "Certificate"}, "value": "Solutions Architect"}}'::jsonb,
    'an app writes an entry under its slug and the field, beside the user''s own keys'
);

SELECT throws_ok($$
    SELECT claimius.update_profile('a0000000-0000-4000-8000-000000000e01', '{"sqltest_profile.certificate": {"schema": {"type": "string"}, "value": "x"}}'::jsonb)
$$, '22023', NULL, 'the user cannot write an app''s key');

SELECT throws_ok($$
    SELECT claimius.upsert_profile_item((SELECT id FROM boot WHERE key = 'app'), 'a0000000-0000-4000-8000-000000000e01', 'certificate', '{"value": "no schema"}'::jsonb)
$$, '22023', NULL, 'an entry without a schema is refused');

SELECT throws_ok($$
    SELECT claimius.upsert_profile_item((SELECT id FROM boot WHERE key = 'app'), gen_random_uuid(), 'certificate', '{"schema": {"type": "string"}, "value": "x"}'::jsonb)
$$, 'P0404', NULL, 'a user with no live row in the app gets no entry');

SELECT is(
    claimius.delete_profile_item((SELECT id FROM boot WHERE key = 'app'), 'a0000000-0000-4000-8000-000000000e01'),
    '{"bio": {"schema": {"type": "string", "format": "multiline"}, "value": "Builds access control"}}'::jsonb,
    'removing the app''s entries leaves the user''s own keys'
);

SELECT * FROM finish();
ROLLBACK;
