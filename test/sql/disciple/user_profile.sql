BEGIN;
SELECT plan(17);

SELECT has_column('claimius', 'samna_user', c, 'samna_user carries ' || c)
FROM unnest(ARRAY['bio', 'job_title', 'company', 'website', 'linkedin']) AS c;

SELECT col_type_is('claimius', 'samna_user', c, 'text', c || ' is text')
FROM unnest(ARRAY['bio', 'job_title', 'company', 'website', 'linkedin']) AS c;

SELECT col_is_null('claimius', 'samna_user', c, c || ' is nullable')
FROM unnest(ARRAY['bio', 'job_title', 'company', 'website', 'linkedin']) AS c;

SELECT claimius.upsert_disciple_state();
SELECT claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-0000000000e1', p_first_name => 'Profile');

SELECT is(
    (SELECT ROW(bio, job_title, company, website, linkedin)::TEXT FROM claimius.samna_user WHERE user_id = 'a0000000-0000-4000-8000-0000000000e1'),
    ROW(NULL::TEXT, NULL::TEXT, NULL::TEXT, NULL::TEXT, NULL::TEXT)::TEXT,
    'a user written without profile fields holds none'
);

UPDATE claimius.samna_user
SET bio = 'Builds access control', job_title = 'Engineer', company = 'Nimtech', website = 'https://example.no', linkedin = 'https://www.linkedin.com/in/example'
WHERE user_id = 'a0000000-0000-4000-8000-0000000000e1';

SELECT is(
    (SELECT ROW(bio, job_title, company, website, linkedin)::TEXT FROM claimius.samna_user WHERE user_id = 'a0000000-0000-4000-8000-0000000000e1'),
    ROW('Builds access control', 'Engineer', 'Nimtech', 'https://example.no', 'https://www.linkedin.com/in/example')::TEXT,
    'profile fields persist on the user row'
);

SELECT * FROM finish();
ROLLBACK;
