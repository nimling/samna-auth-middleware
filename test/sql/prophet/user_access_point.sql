BEGIN;
SELECT plan(6);

DO $$
DECLARE
    v_app   UUID;
    v_sys   UUID := '00000000-0000-4000-8000-0000000009e0';
    v_a     UUID := 'a0000000-0000-4000-8000-0000000009e1';
    v_b     UUID := 'a0000000-0000-4000-8000-0000000009e2';
    v_c     UUID := 'a0000000-0000-4000-8000-0000000009e3';
    v_root  UUID := '10000000-0000-4000-8000-0000000009e0';
    v_crew  UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_user_point',
        p_system_app_name         => 'SQL user point test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;
    PERFORM claimius.upsert_app_user(p_user_id => v_a, p_app_id => v_app, p_first_name => 'Ada');
    PERFORM claimius.upsert_app_user(p_user_id => v_b, p_app_id => v_app, p_first_name => 'Bo', p_external_id => 'ext-bo');
    PERFORM claimius.upsert_app_user(p_user_id => v_c, p_app_id => v_app, p_first_name => 'Cy');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Point root', 'customer', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    SELECT md5('point kid ' || k)::UUID, 'Kid ' || k, 'customer', v_root, v_root, 1, v_sys
    FROM generate_series(1, 20) k;
    v_crew := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Point crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_b, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM count(*) FROM claimius.get_organizations(v_a, v_app, 0);
    PERFORM count(*) FROM claimius.get_organizations(v_b, v_app, 0);
END $$;

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_user_point'
$$ LANGUAGE sql;

CREATE TEMP TABLE pending AS SELECT count(*) AS n, (SELECT count(*) FROM claimius.user_users WHERE app_id = pg_temp.app()) AS folded FROM claimius.user_users_diff WHERE app_id = pg_temp.app();

SELECT is((SELECT n FROM pending), 0::BIGINT, 'with no viewer the settled pairs raise no user_users_diff row');

SELECT is(
    (SELECT access FROM claimius.get_user_access('a0000000-0000-4000-8000-0000000009e1', pg_temp.app(), 'ext-bo', 0)),
    36,
    'a user sharing objects through rows still pending is seen with member and read, resolved by external id'
);

SELECT is(
    (SELECT count(*) FROM claimius.user_users WHERE viewer_id = 'a0000000-0000-4000-8000-0000000009e1'),
    1::BIGINT,
    'the point check makes the actor a viewer and builds its row toward the one user it shares objects with'
);

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_user_access('a0000000-0000-4000-8000-0000000009e1', pg_temp.app(), 'a0000000-0000-4000-8000-0000000009e3', 0)$$,
    'a user sharing nothing is not seen'
);

SELECT set_eq(
    $$SELECT u.user_id FROM claimius.get_users('a0000000-0000-4000-8000-0000000009e1', pg_temp.app(), 0) u$$,
    $$SELECT s.user_id FROM claimius.samna_user s
      WHERE s.app_id = pg_temp.app()
        AND (s.user_id = 'a0000000-0000-4000-8000-0000000009e1'
             OR EXISTS (SELECT 1 FROM claimius.get_user_access('a0000000-0000-4000-8000-0000000009e1', pg_temp.app(), s.user_id::TEXT, 0)))$$,
    'the listing and the point check see the same users'
);

SELECT is(
    (SELECT access FROM claimius.get_user_access('a0000000-0000-4000-8000-0000000009e1', pg_temp.app(), 'a0000000-0000-4000-8000-0000000009e2', 0)),
    36,
    'after the listing folds the rows the stored count answers the same'
);

SELECT * FROM finish();
ROLLBACK;
