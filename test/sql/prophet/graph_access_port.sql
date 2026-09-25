DO $$
DECLARE
    v_init     JSONB;
    v_app      UUID;
    v_a        UUID := 'a0000000-0000-4000-8000-00000000000a';
    v_b        UUID := 'b0000000-0000-4000-8000-00000000000b';
    v_root     UUID := '10000000-0000-4000-8000-000000000001';
    v_alpha    UUID := '10000000-0000-4000-8000-000000000011';
    v_bravo    UUID := '10000000-0000-4000-8000-000000000012';
    v_charlie  UUID := '10000000-0000-4000-8000-000000000013';
    v_grand    UUID := '10000000-0000-4000-8000-000000000111';
    v_org      UUID;
    v_write    UUID;
    v_read     UUID;
    v_row      RECORD;
    v_count    INTEGER;
    v_ids      UUID[];
BEGIN
    v_init := claimius.init(
        p_system_app_slug         => 'sqltest',
        p_system_app_name         => 'SQL test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_app := (v_init -> 'samna_app' ->> 'id')::UUID;

    PERFORM claimius.ensure_app_user(p_user_id => v_a, p_app_id => v_app, p_first_name => 'Ada', p_external_id => 'ext-a');
    PERFORM claimius.ensure_app_user(p_user_id => v_b, p_app_id => v_app, p_first_name => 'Bo', p_external_id => 'ext-b');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Root', 'company', v_root, v_root, 0, v_a);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_alpha, 'Alpha', 'company', v_root, v_root, 1, v_a),
           (v_bravo, 'Bravo', 'company', v_root, v_root, 1, v_a),
           (v_charlie, 'Charlie', 'company', v_root, v_root, 1, v_a);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_grand, 'Grand', 'company', v_alpha, v_root, 2, v_a);
    FOREACH v_org IN ARRAY ARRAY[v_root, v_alpha, v_bravo, v_charlie, v_grand] LOOP
        PERFORM claimius.link_app_object(v_app, 'claimius.organization', v_org);
    END LOOP;

    v_write := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Root writer', p_description => NULL, p_sa_access => 6,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_write, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_sa_access => 6, p_inherits => TRUE
    );
    PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_write, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_a);

    v_read := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Alpha reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_alpha, p_sa_root_id => v_root, p_sa_created_by => v_a, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_read, p_sa_owner_id => v_alpha, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_object_id => v_alpha, p_object_type => 'claimius.organization', p_sa_access => 4, p_inherits => TRUE
    );
    PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_read, p_user_id => v_a, p_sa_owner_id => v_alpha, p_sa_created_by => v_a);

    SELECT * INTO v_row FROM claimius.resolve_access(v_a, v_app, v_grand::TEXT, 'claimius.organization', 2);
    IF v_row.claim_id IS DISTINCT FROM v_write THEN
        RAISE EXCEPTION 'resolve_access write on grand: expected the root writer claim %, got %', v_write, v_row.claim_id;
    END IF;

    SELECT * INTO v_row FROM claimius.get_direct_access(v_a, v_app, v_grand, 'claimius.organization', 2);
    IF v_row.claim_id IS DISTINCT FROM v_write THEN
        RAISE EXCEPTION 'get_direct_access with write mask: expected root writer, got %', v_row.claim_id;
    END IF;

    SELECT u.target_user_id INTO v_row FROM claimius.get_user_access(v_b, v_app, 'ext-b', 0) u;
    IF v_row.target_user_id IS DISTINCT FROM v_b THEN
        RAISE EXCEPTION 'get_user_access by external_id: expected %, got %', v_b, v_row.target_user_id;
    END IF;

    SELECT count(*) INTO v_count
      FROM claimius.get_owner_graph(v_a, v_app, NULL, 0, 4) g
     WHERE g.object_type = 'claimius.organization'
       AND g.object_id IN (v_root, v_alpha, v_bravo, v_charlie, v_grand);
    IF v_count <> 5 THEN
        RAISE EXCEPTION 'get_owner_graph width 0: expected 5 organizations, got %', v_count;
    END IF;

    SELECT array_agg(g.object_id ORDER BY g.label) INTO v_ids
      FROM claimius.get_owner_graph(v_a, v_app, v_root, 1, 4, 1) g
     WHERE g.object_type = 'claimius.organization'
       AND g.parent_id = v_root;
    IF v_ids IS DISTINCT FROM ARRAY[v_alpha] THEN
        RAISE EXCEPTION 'get_owner_graph width 1 from root: expected only Alpha, got %', v_ids;
    END IF;

    SELECT array_agg(g.object_id ORDER BY g.label) INTO v_ids
      FROM claimius.get_owner_graph(v_a, v_app, v_root, 1, 4, 1, 'name', v_alpha) g
     WHERE g.object_type = 'claimius.organization'
       AND g.parent_id = v_root;
    IF v_ids IS DISTINCT FROM ARRAY[v_bravo] THEN
        RAISE EXCEPTION 'get_owner_graph cursor after Alpha: expected Bravo, got %', v_ids;
    END IF;

    PERFORM claimius.touch_object_usage(v_app, v_a, 'claimius.organization', v_charlie);
    PERFORM claimius.touch_object_usage(v_app, v_a, 'claimius.organization', v_charlie);
    SELECT array_agg(g.object_id) INTO v_ids
      FROM claimius.get_owner_graph(v_a, v_app, v_root, 1, 4, 1, 'usage') g
     WHERE g.object_type = 'claimius.organization'
       AND g.parent_id = v_root;
    IF v_ids IS DISTINCT FROM ARRAY[v_charlie] THEN
        RAISE EXCEPTION 'get_owner_graph width 1 sorted by usage: expected Charlie, got %', v_ids;
    END IF;

    SELECT count(*) INTO v_count
      FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
     WHERE n.nspname = 'claimius'
       AND p.proname IN ('get_owner_graph', 'get_claim_graph', 'get_graph_links_owner', 'get_graph_links_claim', '_prune_graph_rows')
       AND 'plan_cache_mode=force_custom_plan' = ANY(p.proconfig);
    IF v_count <> 5 THEN
        RAISE EXCEPTION 'plan_cache_mode pinned on % of 5 graph functions', v_count;
    END IF;

    SELECT count(*) INTO v_count
      FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
     WHERE n.nspname = 'claimius'
       AND p.proname IN ('get_owner_graph', 'get_claim_graph', 'get_graph_links_owner', 'get_graph_links_claim',
                         'mutation_access', 'resolve_access', 'get_user_access', 'get_direct_access');
    IF v_count <> 9 THEN
        RAISE EXCEPTION 'expected nine definitions for the ported functions (resolve_access keeps its app free overload), found %', v_count;
    END IF;
END $$;
