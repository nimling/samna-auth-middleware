BEGIN;
SELECT plan(9);

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

UPDATE claimius.samna_user
SET profile = '{"bio": "Builds access control"}'::jsonb
WHERE user_id = 'a0000000-0000-4000-8000-000000000e01';

SELECT is(
    claimius.upsert_profile_item((SELECT id FROM boot WHERE key = 'app'), 'a0000000-0000-4000-8000-000000000e01', 'certificate', '{"label": "Certificate", "value": "Solutions Architect", "index": 0}'::jsonb),
    '{"bio": "Builds access control", "sqltest_profile": {"certificate": {"label": "Certificate", "value": "Solutions Architect", "index": 0}}}'::jsonb,
    'an app writes an item under its slug and the field, beside the user''s own keys'
);

SELECT is(
    (SELECT profile -> 'sqltest_profile' -> 'certificate' ->> 'value' FROM claimius.samna_user WHERE user_id = 'a0000000-0000-4000-8000-000000000e01'),
    'Solutions Architect',
    'the item sits on the user row'
);

SELECT throws_ok($$
    SELECT claimius.upsert_profile_item((SELECT id FROM boot WHERE key = 'app'), 'a0000000-0000-4000-8000-000000000e01', 'Name!', '{"label": "x", "value": "y"}'::jsonb)
$$, '22023', NULL, 'a malformed field is refused');

SELECT throws_ok($$
    SELECT claimius.upsert_profile_item((SELECT id FROM boot WHERE key = 'app'), gen_random_uuid(), 'certificate', '{"label": "x", "value": "y"}'::jsonb)
$$, 'P0404', NULL, 'a user with no live row in the app gets no item');

SELECT is(
    claimius.delete_profile_item((SELECT id FROM boot WHERE key = 'app'), 'a0000000-0000-4000-8000-000000000e01', 'certificate'),
    '{"bio": "Builds access control"}'::jsonb,
    'removing the last field removes the app''s object and leaves the user''s own keys'
);

SELECT * FROM finish();
ROLLBACK;
