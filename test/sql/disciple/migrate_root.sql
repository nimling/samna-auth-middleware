BEGIN;
SELECT plan(11);

CREATE TABLE public.sqltest_migrate_row (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    description    TEXT,
    sa_owner_id    UUID NOT NULL,
    sa_created_by  UUID NOT NULL,
    sa_created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_deleted_at  TIMESTAMPTZ
);

CREATE FUNCTION pg_temp.closure_drift() RETURNS BIGINT AS $$
DECLARE
    v_kept  claimius.inheritance_info[];
    v_id    UUID;
    v_drift BIGINT;
BEGIN
    SELECT array_agg(i) INTO v_kept FROM claimius.inheritance_info i;

    DELETE FROM claimius.inheritance_info;
    FOR v_id IN SELECT id FROM claimius.organization WHERE sa_owner_id = id AND sa_deleted_at IS NULL LOOP
        PERFORM claimius.build_ownership_tree(v_id);
    END LOOP;
    FOR v_id IN SELECT id FROM claimius.location WHERE sa_parent_id = id AND sa_deleted_at IS NULL LOOP
        PERFORM claimius.build_location_tree(v_id);
    END LOOP;

    WITH built AS (
        SELECT tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
        FROM claimius.inheritance_info
    ), kept AS (
        SELECT tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
        FROM unnest(v_kept)
    )
    SELECT count(*) INTO v_drift FROM (
        (SELECT * FROM built EXCEPT SELECT * FROM kept)
        UNION ALL
        (SELECT * FROM kept EXCEPT SELECT * FROM built)
    ) d;

    DELETE FROM claimius.inheritance_info;
    INSERT INTO claimius.inheritance_info SELECT * FROM unnest(v_kept);
    RETURN v_drift;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION pg_temp.sees(p_user UUID, p_type TEXT, p_id UUID) RETURNS BOOLEAN AS $$
    SELECT EXISTS (SELECT 1 FROM claimius.get_objects(p_user, p_type, 4) g WHERE g.object_id = p_id)
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys  UUID := '00000000-0000-4000-8000-000000000a90';
    v_uma  UUID := 'a0000000-0000-4000-8000-000000000a91';
    v_vic  UUID := 'a0000000-0000-4000-8000-000000000a92';
    v_old  UUID := '10000000-0000-4000-8000-000000000a90';
    v_new  UUID := '10000000-0000-4000-8000-000000000a91';
    v_sub  UUID := '10000000-0000-4000-8000-000000000a92';
    v_kid  UUID := '10000000-0000-4000-8000-000000000a93';
    v_new_claim UUID;
    v_old_claim UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();
    PERFORM claimius.upsert_table_info('public.sqltest_migrate_row');

    PERFORM claimius.upsert_app_user(p_user_id => v_uma, p_first_name => 'Uma');
    PERFORM claimius.upsert_app_user(p_user_id => v_vic, p_first_name => 'Vic');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_old, 'Old root', 'company', v_old, v_old, 0, v_sys),
           (v_new, 'New root', 'company', v_new, v_new, 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_sub, 'Moved', 'company', v_old, v_old, 1, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_kid, 'Moved child', 'company', v_sub, v_old, 2, v_sys);
    INSERT INTO claimius.location (id, name, sa_owner_id, sa_parent_id, sa_root_id, sa_created_by)
    VALUES ('20000000-0000-4000-8000-000000000a90', 'Moved site', v_kid,
            '20000000-0000-4000-8000-000000000a90', '20000000-0000-4000-8000-000000000a90', v_sys);
    INSERT INTO public.sqltest_migrate_row (id, name, sa_owner_id, sa_created_by)
    VALUES ('40000000-0000-4000-8000-000000000a90', 'Moved row', v_kid, v_sys);

    v_new_claim := (claimius.insert_claim(
        p_name => 'New crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_new, p_sa_root_id => v_new, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_new_claim, p_sa_owner_id => v_new, p_sa_root_id => v_new, p_sa_created_by => v_sys,
        p_object_id => v_new, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_claim_id => v_new_claim, p_user_id => v_uma, p_sa_owner_id => v_new, p_sa_created_by => v_sys);

    v_old_claim := (claimius.insert_claim(
        p_name => 'Old crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_sub, p_sa_root_id => v_old, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_old_claim, p_sa_owner_id => v_old, p_sa_root_id => v_old, p_sa_created_by => v_sys,
        p_object_id => v_old, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_claim_id => v_old_claim, p_user_id => v_vic, p_sa_owner_id => v_old, p_sa_created_by => v_sys);
END $$;

SELECT ok(pg_temp.sees('a0000000-0000-4000-8000-000000000a92', 'claimius.organization', '10000000-0000-4000-8000-000000000a93')
      AND NOT pg_temp.sees('a0000000-0000-4000-8000-000000000a91', 'claimius.organization', '10000000-0000-4000-8000-000000000a93'),
    'before the move the old root''s holder reaches the child and the new root''s does not');

SELECT throws_like($$UPDATE claimius.organization SET sa_owner_id = '10000000-0000-4000-8000-000000000a91', sa_root_id = '10000000-0000-4000-8000-000000000a91'
    WHERE id = '10000000-0000-4000-8000-000000000a92'$$, 'Cross root reparenting is not allowed%',
    'a plain reparent across roots stays refused');

SELECT lives_ok($$SELECT claimius.migrate_root('claimius.organization', '10000000-0000-4000-8000-000000000a92', '10000000-0000-4000-8000-000000000a91')$$,
    'an organization subtree migrates under another root');

SELECT is(pg_temp.closure_drift(), 0::BIGINT, 'the closure after the move is the one a full build writes');

SELECT results_eq($$
    SELECT id, sa_owner_id, sa_root_id FROM claimius.organization
    WHERE id IN ('10000000-0000-4000-8000-000000000a92', '10000000-0000-4000-8000-000000000a93') ORDER BY id$$,
    $$VALUES ('10000000-0000-4000-8000-000000000a92'::UUID, '10000000-0000-4000-8000-000000000a91'::UUID, '10000000-0000-4000-8000-000000000a91'::UUID),
             ('10000000-0000-4000-8000-000000000a93'::UUID, '10000000-0000-4000-8000-000000000a92'::UUID, '10000000-0000-4000-8000-000000000a91'::UUID)$$,
    'the moved organization hangs under the new root and its child keeps its parent under the new root');

SELECT ok((SELECT sa_root_id FROM claimius.claim WHERE name = 'Old crew') = '10000000-0000-4000-8000-000000000a91'
      AND (SELECT sa_root_id FROM claimius.claim WHERE name = 'New crew') = '10000000-0000-4000-8000-000000000a91',
    'a claim owned under the moved subtree takes the new organization root');

SELECT ok(EXISTS (SELECT 1 FROM claimius.inheritance_info
        WHERE tree_type = 'ownership' AND ancestor_id = '10000000-0000-4000-8000-000000000a91'
          AND descendant_type = 'public.sqltest_migrate_row' AND descendant_id = '40000000-0000-4000-8000-000000000a90' AND depth = 3),
    'the registered row sits three levels under the new root');

SELECT ok(pg_temp.sees('a0000000-0000-4000-8000-000000000a91', 'claimius.organization', '10000000-0000-4000-8000-000000000a93')
      AND pg_temp.sees('a0000000-0000-4000-8000-000000000a91', 'claimius.location', '20000000-0000-4000-8000-000000000a90')
      AND pg_temp.sees('a0000000-0000-4000-8000-000000000a91', 'public.sqltest_migrate_row', '40000000-0000-4000-8000-000000000a90'),
    'after the move the new root''s holder reaches the child, its location and its registered row');

SELECT ok(NOT pg_temp.sees('a0000000-0000-4000-8000-000000000a92', 'claimius.organization', '10000000-0000-4000-8000-000000000a93')
      AND NOT pg_temp.sees('a0000000-0000-4000-8000-000000000a92', 'public.sqltest_migrate_row', '40000000-0000-4000-8000-000000000a90'),
    'after the move the old root''s holder reaches nothing under it');

SELECT throws_like($$SELECT claimius.migrate_root('claimius.organization', '10000000-0000-4000-8000-000000000a91', '10000000-0000-4000-8000-000000000a91')$$,
    '%lies inside the subtree%', 'a root does not migrate under itself');

SELECT throws_like($$SELECT claimius.migrate_root('claimius.organization', '10000000-0000-4000-8000-000000000a90', '10000000-0000-4000-8000-000000000a93')$$,
    '%is not a live root%', 'the target must be a root');

SELECT * FROM finish();
ROLLBACK;
