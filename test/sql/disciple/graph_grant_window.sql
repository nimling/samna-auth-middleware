BEGIN;
SELECT plan(2);

DO $$
DECLARE
    v_sys   UUID := '00000000-0000-4000-8000-0000000008d0';
    v_root  UUID := '10000000-0000-4000-8000-0000000008d0';
    v_crew  UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-0000000008d1', p_first_name => 'Live');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-0000000008d2', p_first_name => 'Expired');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Window root', 'customer', v_root, v_root, 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-0000000008d1', 'Alpha', 'customer', v_root, v_root, 1, v_sys),
           ('10000000-0000-4000-8000-0000000008d2', 'Bravo', 'customer', v_root, v_root, 1, v_sys);
    v_crew := (claimius.insert_claim(
        p_name => 'Window crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-0000000008d1',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM claimius.insert_user_claim(p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-0000000008d2',
        p_sa_owner_id => v_root, p_sa_created_by => v_sys, p_ends_at => now() - interval '1 day');
    PERFORM claimius.get_objects('a0000000-0000-4000-8000-0000000008d1', 'claimius.organization');
    PERFORM claimius.get_objects('a0000000-0000-4000-8000-0000000008d2', 'claimius.organization');
END $$;

CREATE FUNCTION pg_temp.owner_targets(p_user UUID) RETURNS TABLE (target_id UUID) AS $$
    SELECT (l ->> 'target_id')::UUID
    FROM jsonb_array_elements(claimius.get_graph_links_owner(
        p_user, 4, NULL, NULL, '10000000-0000-4000-8000-0000000008d0', 0, 0, 'name', NULL) -> 'links') l
    WHERE l ->> 'target_type' = 'organization'
$$ LANGUAGE sql;

SELECT is((SELECT count(*) FROM pg_temp.owner_targets('a0000000-0000-4000-8000-0000000008d1')), 2::BIGINT,
    'a live grant reaches the children in the owner links');
SELECT is((SELECT count(*) FROM pg_temp.owner_targets('a0000000-0000-4000-8000-0000000008d2')), 0::BIGINT,
    'an expired grant reaches nothing in the owner links');

SELECT * FROM finish();
ROLLBACK;
