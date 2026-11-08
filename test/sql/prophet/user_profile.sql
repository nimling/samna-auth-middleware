BEGIN;
SELECT plan(18);

SELECT has_column('claimius', 'samna_user', c, 'samna_user carries ' || c)
FROM unnest(ARRAY['bio', 'job_title', 'company', 'website', 'linkedin']) AS c;

SELECT col_type_is('claimius', 'samna_user', c, 'text', c || ' is text')
FROM unnest(ARRAY['bio', 'job_title', 'company', 'website', 'linkedin']) AS c;

SELECT col_is_null('claimius', 'samna_user', c, c || ' is nullable')
FROM unnest(ARRAY['bio', 'job_title', 'company', 'website', 'linkedin']) AS c;

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
    (SELECT ROW(bio, job_title, company, website, linkedin)::TEXT FROM claimius.samna_user WHERE user_id = 'a0000000-0000-4000-8000-000000000e01'),
    ROW(NULL::TEXT, NULL::TEXT, NULL::TEXT, NULL::TEXT, NULL::TEXT)::TEXT,
    'upsert_app_user writes a user without profile fields'
);

UPDATE claimius.samna_user
SET bio = 'Builds access control', job_title = 'Engineer', company = 'Nimtech', website = 'https://example.no', linkedin = 'https://www.linkedin.com/in/example'
WHERE user_id = 'a0000000-0000-4000-8000-000000000e01';

SELECT is(
    (SELECT ROW(bio, job_title, company, website, linkedin)::TEXT FROM claimius.samna_user WHERE user_id = 'a0000000-0000-4000-8000-000000000e01'),
    ROW('Builds access control', 'Engineer', 'Nimtech', 'https://example.no', 'https://www.linkedin.com/in/example')::TEXT,
    'profile fields persist on the user row'
);

SELECT is(
    (SELECT payload ->> 'linkedin' FROM claimius.sync_event
     WHERE event_type = 'claimius.samna_user' AND payload ->> 'user_id' = 'a0000000-0000-4000-8000-000000000e01'
     ORDER BY seq DESC LIMIT 1),
    'https://www.linkedin.com/in/example',
    'the sync feed carries the profile fields to disciples'
);

SELECT * FROM finish();
ROLLBACK;
