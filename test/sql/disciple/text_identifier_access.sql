BEGIN;
SELECT plan(14);

DO $$
DECLARE
    v_a       UUID := 'a0000000-0000-4000-8000-00000000000a';
    v_b       UUID := 'b0000000-0000-4000-8000-00000000000b';
    v_root    UUID := '10000000-0000-4000-8000-000000000001';
    v_alpha   UUID := '10000000-0000-4000-8000-000000000011';
    v_bravo   UUID := '10000000-0000-4000-8000-000000000012';
    v_charlie UUID := '10000000-0000-4000-8000-000000000013';
    v_grand   UUID := '10000000-0000-4000-8000-000000000111';
    v_write   UUID;
    v_read    UUID;
BEGIN
    PERFORM claimius.init();
    PERFORM claimius.ensure_app_user(p_user_id => v_a, p_first_name => 'Ada', p_external_id => 'ext-a');
    PERFORM claimius.ensure_app_user(p_user_id => v_b, p_first_name => 'Bo', p_external_id => 'ext-b');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Root', 'company', v_root, v_root, 0, v_a);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_alpha, 'Alpha', 'company', v_root, v_root, 1, v_a),
           (v_bravo, 'Bravo', 'company', v_root, v_root, 1, v_a),
           (v_charlie, 'Charlie', 'company', v_root, v_root, 1, v_a);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_grand, 'Grand', 'company', v_alpha, v_root, 2, v_a);

    v_write := (claimius.create_claim(
        p_name => 'Root writer', p_description => NULL, p_sa_access => 6,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_claim_id => v_write, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_sa_access => 6, p_inherits => TRUE
    );
    PERFORM claimius.assign_claim_user(p_claim_id => v_write, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_a);

    v_read := (claimius.create_claim(
        p_name => 'Alpha reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_alpha, p_sa_root_id => v_root, p_sa_created_by => v_a, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_claim_id => v_read, p_sa_owner_id => v_alpha, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_object_id => v_alpha, p_object_type => 'claimius.organization', p_sa_access => 4, p_inherits => TRUE
    );
    PERFORM claimius.assign_claim_user(p_claim_id => v_read, p_user_id => v_a, p_sa_owner_id => v_alpha, p_sa_created_by => v_a);
END $$;

SELECT is(
    (SELECT claim_id FROM claimius.mutation_access(
        'a0000000-0000-4000-8000-00000000000a', '10000000-0000-4000-8000-000000000111', 'claimius.organization', 2)),
    (SELECT id FROM claimius.claim WHERE name = 'Root writer'),
    'mutation_access with a text object id resolves write on a grandchild to the inherited root writer claim'
);

SELECT isnt(
    (SELECT user_claim_id FROM claimius.mutation_access(
        'a0000000-0000-4000-8000-00000000000a', '10000000-0000-4000-8000-000000000111', 'claimius.organization', 4)),
    NULL,
    'mutation_access read on a grandchild returns an actor token'
);

SELECT throws_ok(
    $$SELECT * FROM claimius.mutation_access(
        'a0000000-0000-4000-8000-00000000000a', 'not-a-uuid', 'claimius.organization', 4)$$,
    'P0404', NULL,
    'mutation_access on a malformed object id answers P0404'
);

SELECT throws_ok(
    $$SELECT * FROM claimius.mutation_access(
        'b0000000-0000-4000-8000-00000000000b', '10000000-0000-4000-8000-000000000111', 'claimius.organization', 2)$$,
    'P0403', NULL,
    'mutation_access raises P0403 when the object exists and the user holds no grant'
);

SELECT throws_ok(
    $$SELECT * FROM claimius.mutation_access(
        'a0000000-0000-4000-8000-00000000000a', '10000000-0000-4000-8000-000000000999', 'claimius.organization', 2)$$,
    'P0404', NULL,
    'mutation_access raises P0404 on an unknown object'
);

SELECT throws_ok(
    $$SELECT * FROM claimius.mutation_access(
        'a0000000-0000-4000-8000-00000000000a', 'ext-nobody', 'claimius.samna_user', 0)$$,
    'P0404', NULL,
    'mutation_access raises P0404 on an unknown samna_user text identifier'
);

SELECT is(
    (SELECT claim_id FROM claimius.get_direct_access(
        'a0000000-0000-4000-8000-00000000000a', '10000000-0000-4000-8000-000000000111', 'claimius.organization', 2)),
    (SELECT id FROM claimius.claim WHERE name = 'Root writer'),
    'get_direct_access with p_required write returns the root writer claim on a grandchild'
);

SELECT throws_ok(
    $$SELECT * FROM claimius.get_direct_access(
        'b0000000-0000-4000-8000-00000000000b', '10000000-0000-4000-8000-000000000111', 'claimius.organization', 2, TRUE)$$,
    'P0403', NULL,
    'get_direct_access with p_raise raises P0403 when the object exists and the user holds no grant'
);

SELECT throws_ok(
    $$SELECT * FROM claimius.get_direct_access(
        'a0000000-0000-4000-8000-00000000000a', '10000000-0000-4000-8000-000000000999', 'claimius.organization', 2, TRUE)$$,
    'P0404', NULL,
    'get_direct_access with p_raise raises P0404 on an unknown object'
);

SELECT is(
    (SELECT target_user_id FROM claimius.get_user_access('b0000000-0000-4000-8000-00000000000b', 'ext-b', 0)),
    'b0000000-0000-4000-8000-00000000000b'::UUID,
    'get_user_access resolves a target by external_id'
);

SELECT is(
    (SELECT target_user_id FROM claimius.get_user_access(
        'b0000000-0000-4000-8000-00000000000b', 'b0000000-0000-4000-8000-00000000000b', 0)),
    'b0000000-0000-4000-8000-00000000000b'::UUID,
    'get_user_access resolves a target by user_id'
);

SELECT is(
    (SELECT target_user_id FROM claimius.get_user_access(
        'b0000000-0000-4000-8000-00000000000b',
        (SELECT id::TEXT FROM claimius.samna_user WHERE user_id = 'b0000000-0000-4000-8000-00000000000b'), 0)),
    'b0000000-0000-4000-8000-00000000000b'::UUID,
    'get_user_access resolves a target by samna_user.id'
);

SELECT is(
    (SELECT count(*) FROM claimius.get_owner_graph('a0000000-0000-4000-8000-00000000000a', NULL, 0, 4) g
      WHERE g.object_type = 'claimius.organization'
        AND g.object_id IN ('10000000-0000-4000-8000-000000000001', '10000000-0000-4000-8000-000000000011',
                            '10000000-0000-4000-8000-000000000012', '10000000-0000-4000-8000-000000000013',
                            '10000000-0000-4000-8000-000000000111')),
    5::BIGINT,
    'get_owner_graph with no start and unlimited depth returns every readable organization'
);

SELECT results_eq(
    $$SELECT g.object_id FROM claimius.get_owner_graph(
        'a0000000-0000-4000-8000-00000000000a', '10000000-0000-4000-8000-000000000001', 1, 4) g
       WHERE g.object_type = 'claimius.organization' AND g.parent_id = '10000000-0000-4000-8000-000000000001'
       ORDER BY g.label$$,
    $$VALUES ('10000000-0000-4000-8000-000000000011'::UUID), ('10000000-0000-4000-8000-000000000012'::UUID),
             ('10000000-0000-4000-8000-000000000013'::UUID)$$,
    'get_owner_graph from the root at depth 1 returns its three children in label order'
);

SELECT * FROM finish();
ROLLBACK;
