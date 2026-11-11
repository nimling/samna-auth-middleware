BEGIN;
SELECT plan(19);

CREATE TEMP TABLE lvl (key TEXT PRIMARY KEY, id UUID);

DO $$
DECLARE
    v_init  JSONB;
    v_app   UUID;
    v_sys   UUID;
    v_root  UUID := '10000000-0000-4000-8000-0000000001a1';
    v_child UUID := '10000000-0000-4000-8000-0000000001a2';
    v_g0    UUID;
    v_g7    UUID;
    v_d0    UUID;
    v_d5    UUID;
    v_a6    UUID;
    v_b0    UUID;
    v_s3    UUID;
    v_sd1   UUID;
    v_sd3   UUID;
BEGIN
    v_init := claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_level',
        p_system_app_name         => 'SQL level test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_app := (v_init -> 'samna_app' ->> 'id')::UUID;
    v_sys := (v_init -> 'user_claim' ->> 'id')::UUID;

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Root', 'customer', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_child, 'Child', 'customer', v_root, v_root, 1, v_sys);

    PERFORM claimius.upsert_app_user(p_user_id => ('a0000000-0000-4000-8000-0000000001a' || n)::UUID, p_app_id => v_app, p_first_name => 'U' || n)
    FROM generate_series(1, 6) n;

    v_g0  := (claimius.insert_claim(v_app, 'Read level 0', NULL, 4, v_root, v_root, v_sys) -> 'claim' ->> 'id')::UUID;
    v_g7  := (claimius.insert_claim(v_app, 'Read level 7', NULL, 4, v_root, v_root, v_sys, p_level => 7) -> 'claim' ->> 'id')::UUID;
    v_d0  := (claimius.insert_claim(v_app, 'Deny read level 0', NULL, 20, v_root, v_root, v_sys) -> 'claim' ->> 'id')::UUID;
    v_d5  := (claimius.insert_claim(v_app, 'Deny read level 5', NULL, 20, v_root, v_root, v_sys, p_level => 5) -> 'claim' ->> 'id')::UUID;
    v_a6  := (claimius.insert_claim(v_app, 'Link level 6', NULL, 4, v_root, v_root, v_sys, p_level => 6) -> 'claim' ->> 'id')::UUID;
    v_b0  := (claimius.insert_claim(v_app, 'Linked level 0', NULL, 4, v_root, v_root, v_sys) -> 'claim' ->> 'id')::UUID;
    v_s3  := (claimius.insert_claim(v_app, 'Page level 3', NULL, 4, v_root, v_root, v_sys, p_level => 3) -> 'claim' ->> 'id')::UUID;
    v_sd1 := (claimius.insert_claim(v_app, 'Page deny level 1', NULL, 20, v_root, v_root, v_sys, p_level => 1) -> 'claim' ->> 'id')::UUID;
    v_sd3 := (claimius.insert_claim(v_app, 'Page deny level 3', NULL, 20, v_root, v_root, v_sys, p_level => 3) -> 'claim' ->> 'id')::UUID;

    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
    PERFORM claimius.insert_claim_object(p_app_id => v_app, p_claim_id => c, p_sa_owner_id => v_root, p_sa_root_id => v_root,
        p_sa_created_by => v_sys, p_object_id => v_child, p_object_type => 'claimius.organization', p_inherits => TRUE)
    FROM unnest(ARRAY[v_g0, v_g7, v_b0]) c;
    PERFORM claimius.insert_claim_object(p_app_id => v_app, p_claim_id => c, p_sa_owner_id => v_root, p_sa_root_id => v_root,
        p_sa_created_by => v_sys, p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE)
    FROM unnest(ARRAY[v_d0, v_d5]) c;
    PERFORM claimius.insert_claim_object(p_app_id => v_app, p_claim_id => v_a6, p_sa_owner_id => v_root, p_sa_root_id => v_root,
        p_sa_created_by => v_sys, p_object_id => v_b0, p_object_type => 'claimius.claim', p_inherits => TRUE);
    PERFORM claimius.insert_claim_object(p_app_id => v_app, p_claim_id => c, p_sa_owner_id => v_root, p_sa_root_id => v_root,
        p_sa_created_by => v_sys, p_scope => '{"page": {"report": true}}'::JSONB)
    FROM unnest(ARRAY[v_s3, v_sd1, v_sd3]) c;
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => g.claim_id, p_user_id => g.user_id::UUID,
        p_sa_owner_id => v_root, p_sa_created_by => v_sys)
    FROM (VALUES
        (v_g0, 'a0000000-0000-4000-8000-0000000001a1'), (v_d5, 'a0000000-0000-4000-8000-0000000001a1'),
        (v_g0, 'a0000000-0000-4000-8000-0000000001a2'), (v_d0, 'a0000000-0000-4000-8000-0000000001a2'),
        (v_g7, 'a0000000-0000-4000-8000-0000000001a3'), (v_d5, 'a0000000-0000-4000-8000-0000000001a3'),
        (v_a6, 'a0000000-0000-4000-8000-0000000001a4'), (v_d5, 'a0000000-0000-4000-8000-0000000001a4'),
        (v_s3, 'a0000000-0000-4000-8000-0000000001a5'), (v_sd1, 'a0000000-0000-4000-8000-0000000001a5'),
        (v_s3, 'a0000000-0000-4000-8000-0000000001a6'), (v_sd3, 'a0000000-0000-4000-8000-0000000001a6')
    ) g(claim_id, user_id);
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);

    INSERT INTO lvl VALUES ('app', v_app), ('sys', v_sys), ('root', v_root), ('child', v_child), ('g0', v_g0), ('b0', v_b0),
        ('owner', (claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_g0, p_user_id => 'a0000000-0000-4000-8000-0000000001a6',
                   p_sa_owner_id => v_root, p_sa_created_by => v_sys) -> 'user_claim' ->> 'id')::UUID);
END $$;

DO $$
DECLARE
    v_root  UUID := '10000000-0000-4000-8000-0000000001a1';
    v_child UUID := '10000000-0000-4000-8000-0000000001a2';
    v_sys   UUID := (SELECT id FROM lvl WHERE key = 'sys');
    v_app   UUID := (SELECT id FROM lvl WHERE key = 'app');
    v_admin UUID;
    v_deny3 UUID;
    v_x     UUID;
    v_g5    UUID;
    v_g2    UUID;
    v_mine  UUID;
    v_w0    UUID;
    v_w     UUID;
BEGIN
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-0000000001b1', p_app_id => v_app, p_first_name => 'X');
    v_admin := (claimius.insert_claim(v_app, 'Admin', NULL, 15, v_root, v_root, v_sys) -> 'claim' ->> 'id')::UUID;
    v_deny3 := (claimius.insert_claim(v_app, 'Deny read level 3', NULL, 20, v_root, v_root, v_sys, p_level => 3) -> 'claim' ->> 'id')::UUID;
    v_g5 := (claimius.insert_claim(v_app, 'Handout level 5', NULL, 4, v_root, v_root, v_sys, p_level => 5) -> 'claim' ->> 'id')::UUID;
    v_g2 := (claimius.insert_claim(v_app, 'Handout level 2', NULL, 4, v_root, v_root, v_sys, p_level => 2) -> 'claim' ->> 'id')::UUID;
    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
    PERFORM claimius.insert_claim_object(p_app_id => v_app, p_claim_id => v_admin, p_sa_owner_id => v_root, p_sa_root_id => v_root,
        p_sa_created_by => v_sys, p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_claim_object(p_app_id => v_app, p_claim_id => c, p_sa_owner_id => v_root, p_sa_root_id => v_root,
        p_sa_created_by => v_sys, p_object_id => v_child, p_object_type => 'claimius.organization', p_inherits => TRUE)
    FROM unnest(ARRAY[v_deny3, v_g5, v_g2]) c;
    v_x := (claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_admin, p_user_id => 'a0000000-0000-4000-8000-0000000001b1',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys) -> 'user_claim' ->> 'id')::UUID;
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_deny3, p_user_id => 'a0000000-0000-4000-8000-0000000001b1',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    v_mine := (claimius.insert_claim(v_app, 'Raised by X', NULL, 4, v_root, v_root, v_x, p_level => 2) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_app_id => v_app, p_claim_id => v_mine, p_sa_owner_id => v_root, p_sa_root_id => v_root,
        p_sa_created_by => v_sys, p_object_id => v_child, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-0000000001b2', p_app_id => v_app, p_first_name => 'W');
    v_w0 := (claimius.insert_claim(v_app, 'Write level 0', NULL, 2, v_root, v_root, v_sys) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_app_id => v_app, p_claim_id => v_w0, p_sa_owner_id => v_root, p_sa_root_id => v_root,
        p_sa_created_by => v_sys, p_object_id => v_child, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_w := (claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_w0, p_user_id => 'a0000000-0000-4000-8000-0000000001b2',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys) -> 'user_claim' ->> 'id')::UUID;
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);
    INSERT INTO lvl VALUES ('x', v_x), ('g5', v_g5), ('g2', v_g2), ('mine', v_mine), ('w', v_w);
END $$;

CREATE FUNCTION pg_temp.lvl(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM lvl WHERE key = p_key
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.reads(p_user TEXT) RETURNS BOOLEAN AS $$
    SELECT EXISTS (SELECT 1 FROM claimius.get_access(p_user::UUID, pg_temp.lvl('app'), pg_temp.lvl('child'), 'claimius.organization', 4))
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.walks(p_user TEXT) RETURNS BOOLEAN AS $$
    SELECT EXISTS (SELECT 1 FROM claimius.get_direct_access(p_user::UUID, pg_temp.lvl('app'), pg_temp.lvl('child'), 'claimius.organization', 4))
$$ LANGUAGE sql;

SELECT ok(NOT pg_temp.reads('a0000000-0000-4000-8000-0000000001a1'),
    'a deny of a higher level inherited from the parent removes a grant bound on the object itself');
SELECT ok(NOT pg_temp.walks('a0000000-0000-4000-8000-0000000001a1'),
    'the ancestor walk removes it the same way');
SELECT ok(pg_temp.reads('a0000000-0000-4000-8000-0000000001a2'),
    'at the same level a grant bound on the object stands over an inherited deny, as before levels');
SELECT ok(pg_temp.walks('a0000000-0000-4000-8000-0000000001a2'),
    'the ancestor walk keeps it the same way');
SELECT ok(pg_temp.reads('a0000000-0000-4000-8000-0000000001a3'),
    'a deny of a lower level leaves a grant of a higher level');
SELECT ok(pg_temp.reads('a0000000-0000-4000-8000-0000000001a4'),
    'a link takes the higher level of the two claims and stands over a deny below it');
SELECT ok(pg_temp.walks('a0000000-0000-4000-8000-0000000001a4'),
    'the ancestor walk follows the link at the higher level');
SELECT ok(EXISTS (SELECT 1 FROM claimius.get_scope_access('a0000000-0000-4000-8000-0000000001a5', pg_temp.lvl('app'), '$.page.report', 4)),
    'a scope grant stands over a scope deny of a lower level');
SELECT ok(NOT EXISTS (SELECT 1 FROM claimius.get_scope_access('a0000000-0000-4000-8000-0000000001a6', pg_temp.lvl('app'), '$.page.report', 4)),
    'a scope deny of the same level removes the scope grant');

SELECT lives_ok($$
    SELECT claimius.insert_claim_object(p_app_id => pg_temp.lvl('app'), 
        p_claim_id => (claimius.insert_claim(pg_temp.lvl('app'), 'Write level 1', NULL, 2, pg_temp.lvl('root'), pg_temp.lvl('root'), pg_temp.lvl('sys'), p_level => 1) -> 'claim' ->> 'id')::UUID,
        p_sa_owner_id => pg_temp.lvl('root'), p_sa_root_id => pg_temp.lvl('root'), p_sa_created_by => pg_temp.lvl('w'),
        p_object_id => pg_temp.lvl('child'), p_object_type => 'claimius.organization')
$$, 'a level 0 writer with write on the object binds a level 1 claim');

SELECT lives_ok($$
    SELECT claimius.insert_claim_object(p_app_id => pg_temp.lvl('app'), 
        p_claim_id => (claimius.insert_claim(pg_temp.lvl('app'), 'Grant level 5', NULL, 4, pg_temp.lvl('root'), pg_temp.lvl('root'), pg_temp.lvl('sys'), p_level => 5) -> 'claim' ->> 'id')::UUID,
        p_sa_owner_id => pg_temp.lvl('root'), p_sa_root_id => pg_temp.lvl('root'), p_sa_created_by => pg_temp.lvl('owner'),
        p_object_id => pg_temp.lvl('child'), p_object_type => 'claimius.organization')
$$, 'a writer holding read at level 0 binds a level 5 grant of read');

SELECT lives_ok($$
    SELECT claimius.insert_claim_object(p_app_id => pg_temp.lvl('app'), 
        p_claim_id => (claimius.insert_claim(pg_temp.lvl('app'), 'Deny level 5', NULL, 20, pg_temp.lvl('root'), pg_temp.lvl('root'), pg_temp.lvl('sys'), p_level => 5) -> 'claim' ->> 'id')::UUID,
        p_sa_owner_id => pg_temp.lvl('root'), p_sa_root_id => pg_temp.lvl('root'), p_sa_created_by => pg_temp.lvl('owner'),
        p_object_id => pg_temp.lvl('child'), p_object_type => 'claimius.organization')
$$, 'a writer binds a deny above every level it holds');

SELECT lives_ok($$
    SELECT claimius.insert_claim_object(p_app_id => pg_temp.lvl('app'), 
        p_claim_id => (claimius.insert_claim(pg_temp.lvl('app'), 'Deny level 0', NULL, 20, pg_temp.lvl('root'), pg_temp.lvl('root'), pg_temp.lvl('sys')) -> 'claim' ->> 'id')::UUID,
        p_sa_owner_id => pg_temp.lvl('root'), p_sa_root_id => pg_temp.lvl('root'), p_sa_created_by => pg_temp.lvl('owner'),
        p_object_id => pg_temp.lvl('child'), p_object_type => 'claimius.organization')
$$, 'a writer binds a deny at the level it holds');

SELECT throws_like($$
    SELECT claimius.insert_claim_object(p_app_id => pg_temp.lvl('app'), 
        p_claim_id => (claimius.insert_claim(pg_temp.lvl('app'), 'Read level 9', NULL, 4, pg_temp.lvl('root'), pg_temp.lvl('root'), pg_temp.lvl('sys'), p_level => 9) -> 'claim' ->> 'id')::UUID,
        p_sa_owner_id => pg_temp.lvl('root'), p_sa_root_id => pg_temp.lvl('root'), p_sa_created_by => pg_temp.lvl('x'),
        p_object_id => pg_temp.lvl('child'), p_object_type => 'claimius.organization')
$$, '%cannot grant access denied to the writer%is denied 4 on%', 'a writer denied read on an object cannot bind a grant giving read there');

SELECT throws_like($$
    SELECT claimius.insert_user_claim(p_app_id => pg_temp.lvl('app'), p_claim_id => pg_temp.lvl('g5'), p_user_id => 'a0000000-0000-4000-8000-0000000001a2',
        p_sa_owner_id => pg_temp.lvl('root'), p_sa_created_by => pg_temp.lvl('x'))
$$, '%is denied 4 on%', 'a grant of a claim cannot give what a deny removes from the granting actor');

SELECT throws_like($$
    SELECT claimius.insert_user_claim(p_app_id => pg_temp.lvl('app'), p_claim_id => pg_temp.lvl('g2'), p_user_id => 'a0000000-0000-4000-8000-0000000001a2',
        p_sa_owner_id => pg_temp.lvl('root'), p_sa_created_by => pg_temp.lvl('x'))
$$, '%is denied 4 on%', 'the grant is refused whatever the level of the claim against the level of the deny');

SELECT lives_ok($$
    SELECT claimius.insert_user_claim(p_app_id => pg_temp.lvl('app'), p_claim_id => pg_temp.lvl('g2'), p_user_id => 'a0000000-0000-4000-8000-0000000001a3',
        p_sa_owner_id => pg_temp.lvl('root'), p_sa_created_by => pg_temp.lvl('owner'))
$$, 'an actor denied nothing a claim gives hands it out at any level, with no write on the claim');

SELECT throws_like($$
    SELECT claimius.update_claim(pg_temp.lvl('mine'), p_level => 3)
$$, '%is denied 4 on%', 'a claim cannot be raised when its authority is denied what it gives');

SELECT lives_ok($$
    SELECT claimius.update_claim(pg_temp.lvl('g2'), p_level => 9)
$$, 'a claim is raised to any level when its authority is denied nothing it gives');

SELECT * FROM finish();
ROLLBACK;
