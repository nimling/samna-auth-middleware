BEGIN;
SELECT plan(14);

CREATE FUNCTION pg_temp.pairs(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.user_object
     WHERE user_id = p_user
       AND object_type = 'claimius.organization'
       AND object_id IN ('10000000-0000-4000-8000-0000000000d1', '10000000-0000-4000-8000-0000000000d2',
                         '10000000-0000-4000-8000-0000000000d3', '10000000-0000-4000-8000-0000000000d4')
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_settle'
$$ LANGUAGE sql;

DO $$
DECLARE
    v_app   UUID;
    v_sys   UUID := '00000000-0000-4000-8000-0000000000d0';
    v_ada   UUID := 'a0000000-0000-4000-8000-0000000000d0';
    v_root  UUID := '10000000-0000-4000-8000-0000000000d0';
    v_node  UUID := '10000000-0000-4000-8000-0000000000d1';
    v_admin UUID;
    v_crew  UUID;
    v_user  UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_settle',
        p_system_app_name         => 'SQL settle test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    PERFORM claimius.upsert_app_user(p_user_id => v_ada, p_app_id => v_app, p_first_name => 'Ada');
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000d1', 'a0000000-0000-4000-8000-0000000000d2',
                                  'a0000000-0000-4000-8000-0000000000d3']::UUID[] LOOP
        PERFORM claimius.upsert_app_user(p_user_id => v_user, p_app_id => v_app, p_first_name => 'Holder');
    END LOOP;

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Settle root', 'company', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_node, 'Settle node', 'company', v_root, v_root, 1, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-0000000000d2', 'Settle one', 'company', v_node, v_root, 2, v_sys),
           ('10000000-0000-4000-8000-0000000000d3', 'Settle two', 'company', v_node, v_root, 2, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-0000000000d4', 'Settle three', 'company', '10000000-0000-4000-8000-0000000000d3', v_root, 3, v_sys);

    v_admin := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Settle admin', p_description => NULL, p_sa_access => 1,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_admin, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_admin, p_user_id => v_ada, p_sa_owner_id => v_root, p_sa_created_by => v_sys);

    v_crew := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Settle crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_node, p_object_type => 'claimius.organization', p_inherits => TRUE);
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000d1', 'a0000000-0000-4000-8000-0000000000d2',
                                  'a0000000-0000-4000-8000-0000000000d3']::UUID[] LOOP
        PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    END LOOP;
END $$;

SELECT is(
    pg_temp.pairs('a0000000-0000-4000-8000-0000000000d1') + pg_temp.pairs('a0000000-0000-4000-8000-0000000000d2')
        + pg_temp.pairs('a0000000-0000-4000-8000-0000000000d3'),
    0::BIGINT,
    'binding a claim over a node with three descendants and granting it to three holders settles no holder in the write'
);

SELECT is(
    (SELECT sa_access FROM claimius.get_access('a0000000-0000-4000-8000-0000000000d1', pg_temp.app(),
        '10000000-0000-4000-8000-0000000000d3', 'claimius.organization', 4)),
    4,
    'a check by one holder answers the inherited grant'
);

SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000d1'), 1::BIGINT,
    'the check settles the one pair it asks about and none of the other three objects of the area');

SELECT is(
    (SELECT count(*) FROM claimius.access_diff_user WHERE user_id = 'a0000000-0000-4000-8000-0000000000d1'),
    0::BIGINT,
    'a check records no share as settled'
);

SELECT set_eq(
    $$SELECT o.name, g.sa_access
        FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000d1', pg_temp.app(), 'claimius.organization') g
        JOIN claimius.organization o ON o.id = g.object_id$$,
    $$VALUES ('Settle node', 4), ('Settle one', 4), ('Settle two', 4), ('Settle three', 4)$$,
    'a listing by the holder returns the node and its subtree'
);

SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000d1'), 4::BIGINT,
    'the listing settles the holder''s share of the area, four pairs');

SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000000d2') + pg_temp.pairs('a0000000-0000-4000-8000-0000000000d3'), 0::BIGINT,
    'the other holders stay untouched until they read');

SELECT ok(
    EXISTS (SELECT 1 FROM claimius.access_diff WHERE kind = 'claim' AND claim_id = (SELECT id FROM claimius.claim WHERE name = 'Settle crew')),
    'the claim area stays open while holders have not settled it'
);

SELECT is(
    (SELECT sa_access FROM claimius.get_access('a0000000-0000-4000-8000-0000000000d2', pg_temp.app(),
        '10000000-0000-4000-8000-0000000000d4', 'claimius.organization', 4)),
    4,
    'another holder gets the correct grant on its own first read'
);

SELECT is(
    (SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000d2', pg_temp.app(), 'claimius.organization'))
        + (SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000d3', pg_temp.app(), 'claimius.organization')),
    8::BIGINT,
    'the other holders list the four objects each'
);

SELECT is_empty(
    $$SELECT 1 FROM claimius.access_diff WHERE kind = 'claim' AND claim_id = (SELECT id FROM claimius.claim WHERE name = 'Settle crew')$$,
    'the claim area clears once every holder has settled it'
);

CREATE TEMP TABLE ada_before AS
SELECT object_type, object_id, sa_access, grants FROM claimius.user_object WHERE user_id = 'a0000000-0000-4000-8000-0000000000d0';

DO $$
DECLARE
    v_app   UUID := pg_temp.app();
    v_root  UUID := '10000000-0000-4000-8000-0000000000d0';
    v_actor UUID := (SELECT id FROM claimius.user_claim WHERE user_id = 'a0000000-0000-4000-8000-0000000000d0'
                       AND claim_id = (SELECT id FROM claimius.claim WHERE name = 'Settle admin'));
    v_desk  UUID;
BEGIN
    v_desk := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Settle desk', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_actor
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_desk, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_actor,
        p_object_id => '10000000-0000-4000-8000-0000000000d1', p_object_type => 'claimius.organization', p_sa_access => 4, p_inherits => TRUE);
END $$;

SELECT set_eq(
    $$SELECT object_type, object_id FROM (
        SELECT object_type, object_id, sa_access, grants FROM claimius.user_object WHERE user_id = 'a0000000-0000-4000-8000-0000000000d0'
        EXCEPT
        SELECT object_type, object_id, sa_access, grants FROM ada_before
      ) d$$,
    $$VALUES ('claimius.claim', (SELECT id FROM claimius.claim WHERE name = 'Settle desk')),
             ('claimius.organization', '10000000-0000-4000-8000-0000000000d1'::UUID)$$,
    'the actor''s write settles its pairs on the new claim and the bound node only, not the claim''s reach'
);

DO $$
BEGIN
    PERFORM claimius.delete_claim_object(
        (SELECT co.id FROM claimius.claim_object co
           JOIN claimius.claim c ON c.id = co.claim_id
          WHERE c.name = 'Settle crew' AND co.sa_deleted_at IS NULL),
        '00000000-0000-4000-8000-0000000000d0');
END $$;

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_access('a0000000-0000-4000-8000-0000000000d1', pg_temp.app(),
        '10000000-0000-4000-8000-0000000000d2', 'claimius.organization', 4)$$,
    'a removed binding takes the grant away on the holder''s next check'
);

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000d3', pg_temp.app(), 'claimius.organization')$$,
    'a removed binding takes the subtree out of another holder''s next listing'
);

SELECT * FROM finish();
ROLLBACK;
