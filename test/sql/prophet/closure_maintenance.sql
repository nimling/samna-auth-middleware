BEGIN;
SELECT plan(13);

CREATE TABLE public.sqltest_node (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id         UUID,
    name           TEXT NOT NULL,
    description    TEXT,
    sa_owner_id    UUID NOT NULL,
    sa_location_id UUID,
    sa_parent_id   UUID,
    sa_root_id     UUID,
    sa_created_by  UUID NOT NULL,
    sa_created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_deleted_at  TIMESTAMPTZ
);

CREATE TABLE public.sqltest_leaf (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id         UUID,
    name           TEXT NOT NULL,
    description    TEXT,
    sa_owner_id    UUID NOT NULL,
    sa_location_id UUID,
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
    FOR v_id IN SELECT id FROM public.sqltest_node WHERE sa_parent_id = id AND sa_deleted_at IS NULL LOOP
        PERFORM claimius.build_parenthood_tree('public.sqltest_node', v_id);
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

DO $$
DECLARE
    v_a  UUID := 'a0000000-0000-4000-8000-0000000000c1';
    v_r  UUID := '10000000-0000-4000-8000-0000000000c1';
    v_oa UUID := '10000000-0000-4000-8000-0000000000c2';
    v_ob UUID := '10000000-0000-4000-8000-0000000000c3';
    v_o1 UUID := '10000000-0000-4000-8000-0000000000c4';
    v_o2 UUID := '10000000-0000-4000-8000-0000000000c5';
    v_l  UUID := '20000000-0000-4000-8000-0000000000c1';
    v_l1 UUID := '20000000-0000-4000-8000-0000000000c2';
    v_l2 UUID := '20000000-0000-4000-8000-0000000000c3';
    v_n  UUID := '30000000-0000-4000-8000-0000000000c1';
    v_n1 UUID := '30000000-0000-4000-8000-0000000000c2';
    v_n2 UUID := '30000000-0000-4000-8000-0000000000c3';
BEGIN
    PERFORM claimius.init(
        p_system_app_slug         => 'sqltest_closure',
        p_system_app_name         => 'SQL closure test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    PERFORM claimius.init_claimius_tables('public.sqltest_node', 'public.sqltest_leaf');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_r, 'Closure root', 'company', v_r, v_r, 0, v_a);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_oa, 'Closure A', 'company', v_r, v_r, 1, v_a),
           (v_ob, 'Closure B', 'company', v_r, v_r, 1, v_a);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_o1, 'Closure A1', 'company', v_oa, v_r, 2, v_a);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_o2, 'Closure A2', 'company', v_o1, v_r, 3, v_a);

    INSERT INTO claimius.location (id, name, sa_owner_id, sa_parent_id, sa_root_id, sa_created_by)
    VALUES (v_l, 'Closure site', v_r, v_l, v_l, v_a);
    INSERT INTO claimius.location (id, name, sa_owner_id, sa_parent_id, sa_root_id, sa_created_by)
    VALUES (v_l1, 'Closure floor', v_oa, v_l, v_l, v_a);
    INSERT INTO claimius.location (id, name, sa_owner_id, sa_parent_id, sa_root_id, sa_created_by)
    VALUES (v_l2, 'Closure room', v_o1, v_l1, v_l, v_a);
    UPDATE claimius.organization SET sa_location_id = v_l2 WHERE id = v_ob;

    INSERT INTO public.sqltest_node (id, name, sa_owner_id, sa_location_id, sa_parent_id, sa_root_id, sa_created_by)
    VALUES (v_n, 'Node', v_o1, v_l2, v_n, v_n, v_a);
    INSERT INTO public.sqltest_node (id, name, sa_owner_id, sa_location_id, sa_parent_id, sa_root_id, sa_created_by)
    VALUES (v_n1, 'Node one', v_o2, NULL, v_n, v_n, v_a);
    INSERT INTO public.sqltest_node (id, name, sa_owner_id, sa_location_id, sa_parent_id, sa_root_id, sa_created_by)
    VALUES (v_n2, 'Node two', v_o2, v_l1, v_n1, v_n, v_a);
    INSERT INTO public.sqltest_leaf (id, name, sa_owner_id, sa_location_id, sa_created_by)
    VALUES ('40000000-0000-4000-8000-0000000000c1', 'Leaf', v_o2, v_l2, v_a);
END $$;

SELECT is(pg_temp.closure_drift(), 0::BIGINT, 'inserts of organizations, locations, parenthood rows and owned rows leave the closure a full build writes');

SELECT is(
    (SELECT count(*) FROM claimius.inheritance_info
      WHERE tree_type = 'ownership' AND descendant_id = '10000000-0000-4000-8000-0000000000c5'),
    4::BIGINT,
    'an organization inserted at depth three holds its self edge and its three ancestors'
);

UPDATE claimius.organization SET sa_owner_id = '10000000-0000-4000-8000-0000000000c3'
 WHERE id = '10000000-0000-4000-8000-0000000000c4';
SELECT is(pg_temp.closure_drift(), 0::BIGINT, 'reparenting an organization with a subtree splices the subtree to its new ancestors');

UPDATE claimius.location SET sa_parent_id = '20000000-0000-4000-8000-0000000000c1'
 WHERE id = '20000000-0000-4000-8000-0000000000c3';
SELECT is(pg_temp.closure_drift(), 0::BIGINT, 'reparenting a location carries the rows pinned under it');

UPDATE public.sqltest_node SET sa_parent_id = '30000000-0000-4000-8000-0000000000c1'
 WHERE id = '30000000-0000-4000-8000-0000000000c3';
SELECT is(pg_temp.closure_drift(), 0::BIGINT, 'reparenting a parenthood row splices its parenthood subtree');

UPDATE claimius.location SET sa_owner_id = '10000000-0000-4000-8000-0000000000c3'
 WHERE id = '20000000-0000-4000-8000-0000000000c2';
UPDATE claimius.organization SET sa_location_id = '20000000-0000-4000-8000-0000000000c2'
 WHERE id = '10000000-0000-4000-8000-0000000000c3';
UPDATE public.sqltest_node SET sa_owner_id = '10000000-0000-4000-8000-0000000000c2', sa_location_id = NULL
 WHERE id = '30000000-0000-4000-8000-0000000000c2';
SELECT is(pg_temp.closure_drift(), 0::BIGINT, 'a location owner change, an organization pin change and a parenthood owner change reattach their rows');

UPDATE claimius.organization SET sa_deleted_at = now() WHERE id = '10000000-0000-4000-8000-0000000000c4';
SELECT is(pg_temp.closure_drift(), 0::BIGINT, 'soft deleting an organization drops its subtree and every row owned in it');

UPDATE claimius.organization SET sa_deleted_at = NULL WHERE id = '10000000-0000-4000-8000-0000000000c4';
SELECT is(pg_temp.closure_drift(), 0::BIGINT, 'restoring an organization rebuilds its subtree under its live parent');

UPDATE claimius.location SET sa_deleted_at = now() WHERE id = '20000000-0000-4000-8000-0000000000c2';
SELECT is(pg_temp.closure_drift(), 0::BIGINT, 'soft deleting a location drops its location subtree and its own ownership rows');

UPDATE claimius.location SET sa_deleted_at = NULL WHERE id = '20000000-0000-4000-8000-0000000000c2';
SELECT is(pg_temp.closure_drift(), 0::BIGINT, 'restoring a location rebuilds its location subtree and its ownership rows');

UPDATE public.sqltest_node SET sa_deleted_at = now() WHERE id = '30000000-0000-4000-8000-0000000000c1';
DELETE FROM public.sqltest_leaf WHERE id = '40000000-0000-4000-8000-0000000000c1';
SELECT is(pg_temp.closure_drift(), 0::BIGINT, 'soft deleting a parenthood root and hard deleting an owned row remove their rows');

DELETE FROM claimius.organization WHERE id = '10000000-0000-4000-8000-0000000000c5';
SELECT is(pg_temp.closure_drift(), 0::BIGINT, 'hard deleting an organization drops its subtree');

UPDATE claimius.organization SET sa_owner_id = '10000000-0000-4000-8000-0000000000c4'
 WHERE id = '10000000-0000-4000-8000-0000000000c3';
SELECT is(pg_temp.closure_drift(), 0::BIGINT, 'reparenting an organization under its own descendant drops the subtree its root no longer reaches');

SELECT * FROM finish();
ROLLBACK;
