BEGIN;
SELECT plan(6);

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-0000000000b0';
    v_uma    UUID := 'a0000000-0000-4000-8000-0000000000b1';
    v_vic    UUID := 'a0000000-0000-4000-8000-0000000000b2';
    v_wes    UUID := 'a0000000-0000-4000-8000-0000000000b3';
    v_q      UUID := '10000000-0000-4000-8000-000000000b00';
    v_x      UUID := '10000000-0000-4000-8000-000000000b01';
    v_y      UUID := '10000000-0000-4000-8000-000000000b02';
    v_z      UUID := '10000000-0000-4000-8000-000000000b03';
    v_a      UUID;
    v_b      UUID;
    v_d      UUID;
    v_uma_b  UUID;
BEGIN
    PERFORM claimius.init();

    PERFORM claimius.ensure_app_user(p_user_id => v_uma, p_first_name => 'Uma');
    PERFORM claimius.ensure_app_user(p_user_id => v_vic, p_first_name => 'Vic');
    PERFORM claimius.ensure_app_user(p_user_id => v_wes, p_first_name => 'Wes');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_q, 'Claim home', 'company', v_q, v_q, 0, v_sys),
           (v_x, 'X', 'company', v_x, v_x, 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_y, 'Y', 'company', v_x, v_x, 1, v_sys),
           (v_z, 'Z', 'company', v_x, v_x, 1, v_sys);

    v_b := (claimius.create_claim(
        p_name => 'B', p_description => NULL, p_sa_access => 2,
        p_sa_owner_id => v_q, p_sa_root_id => v_q, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    v_a := (claimius.create_claim(
        p_name => 'A', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_q, p_sa_root_id => v_q, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    v_d := (claimius.create_claim(
        p_name => 'No read on X', p_description => NULL, p_sa_access => 20,
        p_sa_owner_id => v_q, p_sa_root_id => v_q, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.create_claim(
        p_name => 'C', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_q, p_sa_root_id => v_q, p_sa_created_by => v_sys);

    PERFORM claimius.assign_claim_object(
        p_claim_id => v_b, p_sa_owner_id => v_x, p_sa_root_id => v_x, p_sa_created_by => v_sys,
        p_object_id => v_x, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.assign_claim_object(
        p_claim_id => v_b, p_sa_owner_id => v_x, p_sa_root_id => v_x, p_sa_created_by => v_sys,
        p_object_id => v_y, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.assign_claim_object(
        p_claim_id => v_d, p_sa_owner_id => v_x, p_sa_root_id => v_x, p_sa_created_by => v_sys,
        p_object_id => v_x, p_object_type => 'claimius.organization', p_inherits => TRUE);

    v_uma_b := (claimius.assign_claim_user(
        p_claim_id => v_b, p_user_id => v_uma, p_sa_owner_id => v_q, p_sa_created_by => v_sys
    ) -> 'user_claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_user(p_claim_id => v_a, p_user_id => v_vic, p_sa_owner_id => v_q, p_sa_created_by => v_sys);
    PERFORM claimius.assign_claim_user(p_claim_id => v_d, p_user_id => v_vic, p_sa_owner_id => v_q, p_sa_created_by => v_sys);
    PERFORM claimius.assign_claim_user(p_claim_id => (SELECT id FROM claimius.claim WHERE name = 'C'), p_user_id => v_wes,
        p_sa_owner_id => v_q, p_sa_created_by => v_sys);

    PERFORM claimius.assign_claim_object(
        p_claim_id => v_a, p_sa_owner_id => v_q, p_sa_root_id => v_q, p_sa_created_by => v_uma_b,
        p_object_id => v_b, p_object_type => 'claimius.claim', p_inherits => TRUE);
END $$;

SELECT is(
    (SELECT g.sa_access FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000b1', 'claimius.claim') g
      WHERE g.object_id = (SELECT id FROM claimius.claim WHERE name = 'B')),
    4,
    'holding a claim gives read on the claim'
);

SELECT is(
    (SELECT g.sa_access FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000b2', 'claimius.claim') g
      WHERE g.object_id = (SELECT id FROM claimius.claim WHERE name = 'B')),
    4,
    'a composition link gives its holders read on the linked claim'
);

SELECT set_eq(
    $$SELECT o.name, g.sa_access
        FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000b2', 'claimius.organization') g
        JOIN claimius.organization o ON o.id = g.object_id
       WHERE o.sa_root_id = '10000000-0000-4000-8000-000000000b01'$$,
    $$VALUES ('Y', 4)$$,
    'a composed binding on the object itself is direct and stands over an inherited deny, the cascaded one does not'
);

SELECT set_eq(
    $$SELECT o.name, g.sa_access
        FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000b1', 'claimius.organization') g
        JOIN claimius.organization o ON o.id = g.object_id
       WHERE o.sa_root_id = '10000000-0000-4000-8000-000000000b01'$$,
    $$VALUES ('X', 14), ('Y', 14), ('Z', 14)$$,
    'the holder of the linked claim keeps its own grants'
);

SELECT is(
    (SELECT claim_name FROM claimius.get_direct_access('a0000000-0000-4000-8000-0000000000b2',
        '10000000-0000-4000-8000-000000000b02', 'claimius.organization', 4, FALSE)),
    'A',
    'get_direct_access resolves a composed grant to the held linking claim'
);

SELECT throws_ok(
    $$SELECT claimius.assign_claim_object(
        p_claim_id => (SELECT id FROM claimius.claim WHERE name = 'C'),
        p_sa_owner_id => '10000000-0000-4000-8000-000000000b00', p_sa_root_id => '10000000-0000-4000-8000-000000000b00',
        p_sa_created_by => (SELECT uc.id FROM claimius.user_claim uc WHERE uc.user_id = 'a0000000-0000-4000-8000-0000000000b3'
                              AND uc.claim_id = (SELECT id FROM claimius.claim WHERE name = 'C')),
        p_object_id => (SELECT id FROM claimius.claim WHERE name = 'B'), p_object_type => 'claimius.claim')$$,
    'P0001', NULL,
    'a writer who neither holds nor reads the target claim cannot compose it'
);

SELECT * FROM finish();
ROLLBACK;
