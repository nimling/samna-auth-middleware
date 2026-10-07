BEGIN;
SELECT plan(4);

CREATE TABLE public.sqltest_nb_widget (
    id            UUID PRIMARY KEY,
    app_id        UUID NOT NULL,
    name          TEXT NOT NULL,
    sa_owner_id   UUID NOT NULL,
    sa_created_by UUID,
    sa_deleted_at TIMESTAMPTZ,
    sa_created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DO $$
DECLARE
    v_app   UUID;
    v_sys   UUID := '00000000-0000-4000-8000-0000000006b0';
    v_user  UUID := 'a0000000-0000-4000-8000-0000000006b1';
    v_root  UUID := '10000000-0000-4000-8000-0000000006b0';
    v_crew  UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_no_bypass',
        p_system_app_name         => 'SQL no bypass test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;
    PERFORM claimius.upsert_app_user(p_user_id => v_user, p_app_id => v_app, p_first_name => 'Reader');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'No bypass root', 'company', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    v_crew := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'No bypass crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_user,
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM claimius.upsert_table_info('public.sqltest_nb_widget');
    INSERT INTO public.sqltest_nb_widget (id, app_id, name, sa_owner_id, sa_created_by)
    VALUES ('20000000-0000-4000-8000-0000000006b1', v_app, 'Widget', v_root, v_sys);
END $$;

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_no_bypass'
$$ LANGUAGE sql;

SELECT is(
    (SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000006b1', pg_temp.app(), 'public.sqltest_nb_widget')),
    1::BIGINT,
    'a healthy settle lists the widget the reader reaches through its root'
);

ALTER TABLE public.sqltest_nb_widget RENAME COLUMN name TO title;
SELECT claimius._raise_object_diff(pg_temp.app(), 'public.sqltest_nb_widget', '20000000-0000-4000-8000-0000000006b1');

SELECT throws_ok(
    $$SELECT * FROM claimius.get_access('a0000000-0000-4000-8000-0000000006b1', pg_temp.app(),
        '20000000-0000-4000-8000-0000000006b1', 'public.sqltest_nb_widget', 0)$$,
    '42703', NULL,
    'a check whose settle fails raises the error instead of reading a stale pair'
);

SELECT throws_ok(
    $$SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000006b1', pg_temp.app(), 'public.sqltest_nb_widget')$$,
    '42703', NULL,
    'a listing whose settle fails raises the error instead of leaving the area open'
);

ALTER TABLE public.sqltest_nb_widget RENAME COLUMN title TO name;

SELECT lives_ok(
    $$SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000006b1', pg_temp.app(), 'public.sqltest_nb_widget')$$,
    'once the table is whole the same listing settles'
);

SELECT * FROM finish();
ROLLBACK;
