BEGIN;

DO $$
DECLARE
    v_init     JSONB;
    v_app      UUID;
    v_a        UUID := 'a0000000-0000-4000-8000-0000000000d1';
    v_root     UUID := '10000000-0000-4000-8000-0000000000d1';
    v_child    UUID := '10000000-0000-4000-8000-0000000000d2';
    v_other    UUID := '10000000-0000-4000-8000-0000000000d3';
    v_leaf     UUID := '10000000-0000-4000-8000-0000000000d4';
    v_writer   UUID;
    v_fenced   UUID;
    v_deny     UUID;
    v_count    INTEGER;
BEGIN
    v_init := claimius.init(
        p_system_app_slug         => 'sqltest_walk',
        p_system_app_name         => 'SQL walk test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_app := (v_init -> 'samna_app' ->> 'id')::UUID;

    PERFORM claimius.ensure_app_user(p_user_id => v_a, p_app_id => v_app, p_first_name => 'Ada');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Walk root', 'company', v_root, v_root, 0, v_a),
           (v_other, 'Walk other', 'company', v_other, v_other, 0, v_a);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_child, 'Walk child', 'company', v_root, v_root, 1, v_a),
           (v_leaf, 'Walk leaf', 'company', v_other, v_other, 1, v_a);
    PERFORM claimius.link_app_object(v_app, 'claimius.organization', v_root);
    PERFORM claimius.link_app_object(v_app, 'claimius.organization', v_other);

    v_writer := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Root writer', p_description => NULL, p_sa_access => 6,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_writer, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_sa_access => 6, p_inherits => TRUE
    );
    PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_writer, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_a);

    v_deny := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Child write deny', p_description => NULL, p_sa_access => 18,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_deny, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_object_id => v_child, p_object_type => 'claimius.organization', p_sa_access => 18, p_inherits => TRUE
    );
    PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_deny, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_a);

    SELECT count(*) INTO v_count FROM claimius.get_direct_access(v_a, v_app, v_child, 'claimius.organization', 2, FALSE);
    IF v_count <> 0 THEN
        RAISE EXCEPTION 'a write deny bound to the object must stop the ancestor walk from granting write';
    END IF;

    v_fenced := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Other root only', p_description => NULL, p_sa_access => 6,
        p_sa_owner_id => v_other, p_sa_root_id => v_other, p_sa_created_by => v_a, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_fenced, p_sa_owner_id => v_other, p_sa_root_id => v_other, p_sa_created_by => v_a,
        p_object_id => v_other, p_object_type => 'claimius.organization', p_sa_access => 6, p_inherits => FALSE
    );
    PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_fenced, p_user_id => v_a, p_sa_owner_id => v_other, p_sa_created_by => v_a);

    SELECT count(*) INTO v_count FROM claimius.get_direct_access(v_a, v_app, v_leaf, 'claimius.organization', 2, FALSE);
    IF v_count <> 0 THEN
        RAISE EXCEPTION 'a binding with inherits false must not reach below its object through the ancestor walk';
    END IF;

    SELECT count(*) INTO v_count FROM claimius.get_direct_access(v_a, v_app, v_other, 'claimius.organization', 2, FALSE);
    IF v_count <> 1 THEN
        RAISE EXCEPTION 'a binding with inherits false must still grant on its own object, got % rows', v_count;
    END IF;
END $$;

ROLLBACK;
