BEGIN;
SELECT plan(6);

DO $$
DECLARE
    v_sys   UUID := '00000000-0000-4000-8000-000000000f70';
    v_root  UUID := '10000000-0000-4000-8000-000000000f70';
    v_crew  UUID;
BEGIN
    PERFORM claimius.init();

    PERFORM claimius.ensure_app_user(p_user_id => 'a0000000-0000-4000-8000-000000000f71', p_first_name => 'Linker');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Link root', 'company', v_root, v_root, 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-000000000f73', 'Charlie', 'company', v_root, v_root, 1, v_sys),
           ('10000000-0000-4000-8000-000000000f71', 'Alpha', 'company', v_root, v_root, 1, v_sys),
           ('10000000-0000-4000-8000-000000000f72', 'Bravo', 'company', v_root, v_root, 1, v_sys);

    v_crew := (claimius.create_claim(
        p_name => 'Link crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.assign_claim_user(p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-000000000f71',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM claimius.get_objects('a0000000-0000-4000-8000-000000000f71', 'claimius.organization');
END $$;

CREATE FUNCTION pg_temp.owner_targets(p_width INTEGER, p_next UUID) RETURNS TABLE (target_id UUID) AS $$
    SELECT (l ->> 'target_id')::UUID
    FROM jsonb_array_elements(claimius.get_graph_links_owner(
        'a0000000-0000-4000-8000-000000000f71', 4, NULL, NULL, '10000000-0000-4000-8000-000000000f70', 0, p_width, 'name', p_next) -> 'links') l
    WHERE l ->> 'source_id' = '10000000-0000-4000-8000-000000000f70' AND l ->> 'target_type' = 'organization'
$$ LANGUAGE sql;

SELECT is((SELECT count(*) FROM pg_temp.owner_targets(0, NULL)), 3::BIGINT,
    'without a window the owner links reach every child');
SELECT set_eq($$SELECT * FROM pg_temp.owner_targets(2, NULL)$$,
    $$VALUES ('10000000-0000-4000-8000-000000000f71'::UUID), ('10000000-0000-4000-8000-000000000f72'::UUID)$$,
    'a window of two keeps the links to the first two children in name order');
SELECT set_eq($$SELECT * FROM pg_temp.owner_targets(2, '10000000-0000-4000-8000-000000000f72')$$,
    $$VALUES ('10000000-0000-4000-8000-000000000f73'::UUID)$$,
    'the continuation after Bravo keeps the link to the next child');

SELECT is((
    SELECT count(*) FROM jsonb_array_elements(claimius.get_graph_links_claim(
        'a0000000-0000-4000-8000-000000000f71', 0, NULL, NULL, NULL, 0, 2, 'name', NULL) -> 'links') l
    WHERE l ->> 'source_type' = 'claim' AND l ->> 'target_type' = 'organization'), 2::BIGINT,
    'a window of two keeps two organization links of the claim');
SELECT is((
    SELECT count(*) FROM jsonb_array_elements(claimius.get_graph_links_claim(
        'a0000000-0000-4000-8000-000000000f71', 0, NULL, NULL, NULL, 0) -> 'links') l
    WHERE l ->> 'source_type' = 'claim' AND l ->> 'target_type' = 'organization'), 4::BIGINT,
    'without a window the claim links reach the root and its three children');

SELECT throws_ok($$SELECT claimius.get_graph_links_owner('a0000000-0000-4000-8000-000000000f71', 4, NULL, NULL, NULL, 0, 2, 'size', NULL)$$,
    '22023', NULL, 'a sort other than name or usage is refused');

SELECT * FROM finish();
ROLLBACK;
