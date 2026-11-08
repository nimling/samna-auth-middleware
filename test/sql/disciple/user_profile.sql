BEGIN;
SELECT plan(5);

SELECT has_column('claimius', 'samna_user', 'profile', 'samna_user carries profile');
SELECT col_type_is('claimius', 'samna_user', 'profile', 'jsonb', 'profile is jsonb');
SELECT col_default_is('claimius', 'samna_user', 'profile', '''{}''::jsonb', 'profile starts empty');

SELECT claimius.upsert_disciple_state();
SELECT claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-0000000000e1', p_first_name => 'Profile');

SELECT is(
    (SELECT profile FROM claimius.samna_user WHERE user_id = 'a0000000-0000-4000-8000-0000000000e1'),
    '{}'::jsonb,
    'a user written without a profile holds an empty object'
);

UPDATE claimius.samna_user
SET profile = '{"bio": "Builds access control", "bookable": {"certificate": {"label": "Certificate", "value": "Solutions Architect", "index": 0}}}'::jsonb
WHERE user_id = 'a0000000-0000-4000-8000-0000000000e1';

SELECT is(
    (SELECT profile -> 'bookable' -> 'certificate' ->> 'label' FROM claimius.samna_user WHERE user_id = 'a0000000-0000-4000-8000-0000000000e1'),
    'Certificate',
    'the profile the sync writes is read back as stored'
);

SELECT * FROM finish();
ROLLBACK;
