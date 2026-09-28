BEGIN;

SELECT claimius.init();

DO $$
DECLARE
    v_a        UUID := 'a0000000-0000-4000-8000-0000000000fa';
    v_root     UUID := '10000000-0000-4000-8000-0000000000f1';
    v_app_ref  UUID := '20000000-0000-4000-8000-0000000000f2';
    v_claim    UUID;
    v_count    INTEGER;
BEGIN
    PERFORM claimius.ensure_app_user(p_user_id => v_a, p_first_name => 'Ada');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Foreign root', 'company', v_root, v_root, 0, v_a);

    v_claim := (claimius.create_claim(
        p_name => 'App reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_sa_access => 4, p_inherits => TRUE
    );
    PERFORM claimius.assign_claim_object(
        p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_object_id => v_app_ref, p_object_type => 'claimius.samna_app', p_sa_access => 4, p_inherits => TRUE
    );
    PERFORM claimius.assign_claim_user(p_claim_id => v_claim, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_a);

    SELECT count(*) INTO v_count FROM claimius.get_organizations(v_a, 4) WHERE id = v_root;
    IF v_count <> 1 THEN
        RAISE EXCEPTION 'a claim also bound to a table the disciple does not hold must still grant its organization, got % rows', v_count;
    END IF;

    PERFORM claimius._raise_claim_diff(v_claim);
    PERFORM count(*) FROM claimius.get_organizations(v_a, 4);
    SELECT count(*) INTO v_count FROM claimius.access_diff WHERE kind = 'claim' AND claim_id = v_claim;
    IF v_count <> 0 THEN
        RAISE EXCEPTION 'a claim bound to a table the disciple does not hold must settle on read, % diff rows left', v_count;
    END IF;
END $$;

ROLLBACK;
