BEGIN;
SELECT plan(14);

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_pairs'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.stored() RETURNS TABLE (viewer_id UUID, target_user_id UUID, sharing BIGINT) AS $$
    SELECT uu.viewer_id, uu.target_user_id, uu.sharing_object_count::BIGINT
    FROM claimius.user_users uu
    WHERE uu.app_id = pg_temp.app()
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.rebuilt() RETURNS TABLE (viewer_id UUID, target_user_id UUID, sharing BIGINT) AS $$
    SELECT mine.user_id, other.user_id, count(*)
    FROM claimius.user_object mine
        JOIN claimius.user_object other
            ON other.app_id = mine.app_id
           AND other.object_type = mine.object_type
           AND other.object_id = mine.object_id
           AND other.user_id <> mine.user_id
    WHERE mine.app_id = pg_temp.app()
      AND mine.object_type <> 'claimius.claim'
    GROUP BY mine.user_id, other.user_id
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.list_all() RETURNS BIGINT AS $$
    SELECT sum((SELECT count(*) FROM claimius.get_objects(u, pg_temp.app(), 'claimius.organization')))::BIGINT
    FROM unnest(ARRAY['a0000000-0000-4000-8000-0000000000e1', 'a0000000-0000-4000-8000-0000000000e2',
                      'a0000000-0000-4000-8000-0000000000e3', 'a0000000-0000-4000-8000-0000000000e4']::UUID[]) AS u
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.sharing(p_viewer UUID, p_target UUID) RETURNS BIGINT AS $$
    SELECT coalesce((SELECT s.sharing FROM pg_temp.stored() s WHERE s.viewer_id = p_viewer AND s.target_user_id = p_target), 0)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.user_users_writes() RETURNS BIGINT AS $$
    SELECT n_tup_ins + n_tup_upd + n_tup_del FROM pg_stat_xact_user_tables
    WHERE schemaname = 'claimius' AND relname = 'user_users'
$$ LANGUAGE sql;

DO $$
DECLARE
    v_app   UUID;
    v_sys   UUID := '00000000-0000-4000-8000-0000000000e0';
    v_root  UUID := '10000000-0000-4000-8000-0000000000e0';
    v_node  UUID := '10000000-0000-4000-8000-0000000000e1';
    v_crew  UUID;
    v_desk  UUID;
    v_user  UUID;
BEGIN
    v_app := (claimius.init(
        p_system_app_slug         => 'sqltest_pairs',
        p_system_app_name         => 'SQL pair storage test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000e1', 'a0000000-0000-4000-8000-0000000000e2',
                                  'a0000000-0000-4000-8000-0000000000e3', 'a0000000-0000-4000-8000-0000000000e4']::UUID[] LOOP
        PERFORM claimius.ensure_app_user(p_user_id => v_user, p_app_id => v_app, p_first_name => 'Sharer');
    END LOOP;

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Pairs root', 'company', v_root, v_root, 0, v_sys);
    PERFORM claimius.link_app_object(v_app, 'claimius.organization', v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_node, 'Pairs node', 'company', v_root, v_root, 1, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-0000000000e2', 'Pairs one', 'company', v_node, v_root, 2, v_sys),
           ('10000000-0000-4000-8000-0000000000e3', 'Pairs two', 'company', v_node, v_root, 2, v_sys),
           ('10000000-0000-4000-8000-0000000000e4', 'Pairs three', 'company', v_node, v_root, 2, v_sys);

    v_crew := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Pairs crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_node, p_object_type => 'claimius.organization', p_inherits => TRUE);
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000e1', 'a0000000-0000-4000-8000-0000000000e2',
                                  'a0000000-0000-4000-8000-0000000000e3']::UUID[] LOOP
        PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    END LOOP;

    v_desk := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Pairs desk', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_desk, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => '10000000-0000-4000-8000-0000000000e2', p_object_type => 'claimius.organization', p_inherits => FALSE);
    PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_desk, p_user_id => 'a0000000-0000-4000-8000-0000000000e4',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);
END $$;

SELECT is(pg_temp.list_all(), 13::BIGINT, 'three crew holders list the node and its three children, the desk holder lists one child');

SELECT set_eq('SELECT * FROM pg_temp.stored()', 'SELECT * FROM pg_temp.rebuilt()',
    'after the grants settle the counted user_users equal a full rebuild from user_object');

SELECT ok(
    pg_temp.sharing('a0000000-0000-4000-8000-0000000000e1', 'a0000000-0000-4000-8000-0000000000e2') >= 4
    AND pg_temp.sharing('a0000000-0000-4000-8000-0000000000e1', 'a0000000-0000-4000-8000-0000000000e4') = 1
    AND pg_temp.sharing('a0000000-0000-4000-8000-0000000000e4', 'a0000000-0000-4000-8000-0000000000e1') = 1,
    'two crew holders share the four organizations and a crew holder shares the one desk child with the desk holder, both directions'
);

DO $$
BEGIN
    PERFORM claimius.remove_user_claim(
        (SELECT uc.id FROM claimius.user_claim uc JOIN claimius.claim c ON c.id = uc.claim_id
          WHERE c.name = 'Pairs crew' AND uc.user_id = 'a0000000-0000-4000-8000-0000000000e3' AND uc.sa_deleted_at IS NULL),
        '00000000-0000-4000-8000-0000000000e0');
END $$;
SELECT pg_temp.list_all();

SELECT set_eq('SELECT * FROM pg_temp.stored()', 'SELECT * FROM pg_temp.rebuilt()',
    'after a revoke settles the counted user_users equal a full rebuild');

DO $$
DECLARE
    v_app  UUID := pg_temp.app();
    v_root UUID := '10000000-0000-4000-8000-0000000000e0';
    v_sys  UUID := '00000000-0000-4000-8000-0000000000e0';
    v_deny UUID;
BEGIN
    v_deny := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Pairs deny', p_description => NULL, p_sa_access => 20,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_deny, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => '10000000-0000-4000-8000-0000000000e3', p_object_type => 'claimius.organization', p_inherits => FALSE);
    PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_deny, p_user_id => 'a0000000-0000-4000-8000-0000000000e2',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);
END $$;
SELECT pg_temp.list_all();

SELECT set_eq('SELECT * FROM pg_temp.stored()', 'SELECT * FROM pg_temp.rebuilt()',
    'after a deny settles the counted user_users equal a full rebuild');

UPDATE claimius.organization SET sa_deleted_at = now() WHERE id = '10000000-0000-4000-8000-0000000000e2';
SELECT pg_temp.list_all();

SELECT set_eq('SELECT * FROM pg_temp.stored()', 'SELECT * FROM pg_temp.rebuilt()',
    'after a soft delete settles the counted user_users equal a full rebuild');

SELECT is(
    pg_temp.sharing('a0000000-0000-4000-8000-0000000000e1', 'a0000000-0000-4000-8000-0000000000e4')
        + pg_temp.sharing('a0000000-0000-4000-8000-0000000000e4', 'a0000000-0000-4000-8000-0000000000e1'),
    0::BIGINT,
    'the pair of users whose one shared object went has no user_users row left in either direction'
);

INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
VALUES ('10000000-0000-4000-8000-0000000000e5', 'Pairs late', 'company', '10000000-0000-4000-8000-0000000000e1',
        '10000000-0000-4000-8000-0000000000e0', 2, '00000000-0000-4000-8000-0000000000e0');
SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000e1', pg_temp.app(), 'claimius.organization');
CREATE TEMP TABLE writes AS SELECT pg_temp.user_users_writes() AS n;
SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000e2', pg_temp.app(), 'claimius.organization');

SELECT is(pg_temp.user_users_writes() - (SELECT n FROM writes), 2::BIGINT,
    'a pair entering on an object one other user holds writes two user_users rows, one each direction');

SELECT set_eq('SELECT * FROM pg_temp.stored()', 'SELECT * FROM pg_temp.rebuilt()',
    'after a pair enters the counted user_users equal a full rebuild');

CREATE TEMP TABLE counted AS SELECT * FROM pg_temp.stored();
SELECT claimius._refresh_user_users(pg_temp.app(), u.user_id)
FROM (SELECT DISTINCT user_id FROM claimius.user_object WHERE app_id = pg_temp.app()) u;

SELECT set_eq('SELECT * FROM pg_temp.stored()', 'SELECT * FROM counted',
    'the bulk rebuild leaves every counted row as it was');

SELECT set_eq(
    $$SELECT user_id FROM claimius.user_object
       WHERE app_id = pg_temp.app() AND object_type = 'claimius.organization'
         AND object_id = '10000000-0000-4000-8000-0000000000e4'$$,
    $$VALUES ('a0000000-0000-4000-8000-0000000000e1'::UUID), ('a0000000-0000-4000-8000-0000000000e2'::UUID)$$,
    'the reverse lookup by object on user_object returns the users holding the object'
);

SELECT has_index('claimius', 'user_object', 'idx_user_object_object_user',
    ARRAY['app_id', 'object_type', 'object_id', 'user_id'],
    'the reverse lookup is an index on user_object keyed from the object side');

SELECT hasnt_table('claimius', 'object_users', 'no object_users table remains');

DO $$
DECLARE
    v_app   UUID := pg_temp.app();
    v_root  UUID := '10000000-0000-4000-8000-0000000000e0';
    v_sys   UUID := '00000000-0000-4000-8000-0000000000e0';
    v_badge UUID;
    v_user  UUID;
BEGIN
    v_badge := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Pairs badge', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000e5', 'a0000000-0000-4000-8000-0000000000e6']::UUID[] LOOP
        PERFORM claimius.ensure_app_user(p_user_id => v_user, p_app_id => v_app, p_first_name => 'Badge');
        PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_badge, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    END LOOP;
END $$;

CREATE TEMP TABLE badge_seen AS
SELECT u.user_id FROM claimius.get_users('a0000000-0000-4000-8000-0000000000e5', pg_temp.app()) u
UNION ALL
SELECT u.user_id FROM claimius.get_users('a0000000-0000-4000-8000-0000000000e6', pg_temp.app()) u;

SELECT ok(
    (SELECT count(*) FROM claimius.user_object o JOIN claimius.claim c ON c.id = o.object_id
      WHERE o.app_id = pg_temp.app() AND o.object_type = 'claimius.claim' AND c.name = 'Pairs badge'
        AND o.user_id IN ('a0000000-0000-4000-8000-0000000000e5', 'a0000000-0000-4000-8000-0000000000e6')) = 2
    AND NOT EXISTS (SELECT 1 FROM badge_seen s
                     WHERE s.user_id NOT IN ('a0000000-0000-4000-8000-0000000000e5', 'a0000000-0000-4000-8000-0000000000e6'))
    AND (SELECT count(*) FROM badge_seen) = 2,
    'two users who share only a held claim each read the claim and do not see each other through get_users'
);

SELECT * FROM finish();
ROLLBACK;
