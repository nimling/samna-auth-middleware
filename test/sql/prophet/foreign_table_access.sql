BEGIN;

DO $$
DECLARE
    v_init     JSONB;
    v_app      UUID;
    v_a        UUID := 'a0000000-0000-4000-8000-0000000000fa';
    v_root     UUID := '10000000-0000-4000-8000-0000000000f1';
    v_foreign  UUID := '20000000-0000-4000-8000-0000000000f2';
    v_claim    UUID;
    v_count    INTEGER;
BEGIN
    v_init := claimius.init(
        p_system_app_slug         => 'sqltest_foreign',
        p_system_app_name         => 'SQL foreign test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_app := (v_init -> 'samna_app' ->> 'id')::UUID;

    PERFORM claimius.ensure_app_user(p_user_id => v_a, p_app_id => v_app, p_first_name => 'Ada');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Foreign root', 'company', v_root, v_root, 0, v_a);
    PERFORM claimius.link_app_object(v_app, 'claimius.organization', v_root);

    v_claim := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Disciple writer', p_description => NULL, p_sa_access => 6,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_claim, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_a);

    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_object_id => v_foreign, p_object_type => 'public.sqltest_disciple_only', p_sa_access => 6, p_inherits => TRUE
    );

    SELECT count(*) INTO v_count
      FROM claimius.claim_object
     WHERE claim_id = v_claim AND object_type = 'public.sqltest_disciple_only' AND object_id = v_foreign;
    IF v_count <> 1 THEN
        RAISE EXCEPTION 'the prophet must store a binding to a table only a disciple holds, got % rows', v_count;
    END IF;

    SELECT count(*) INTO v_count
      FROM claimius.search(v_a, v_app, 'Foreign', NULL::TEXT[], 0, NULL::UUID[], NULL::UUID[], NULL::UUID[], NULL::UUID, NULL::TEXT, ARRAY[gen_random_uuid()], 20, 0);
    IF v_count <> 0 THEN
        RAISE EXCEPTION 'search with a type filter on a prophet without typed tables must return nothing, got %', v_count;
    END IF;
END $$;

ROLLBACK;
