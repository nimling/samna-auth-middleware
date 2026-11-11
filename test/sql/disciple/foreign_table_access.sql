BEGIN;
SELECT plan(3);

DO $$
DECLARE
    v_a       UUID := 'a0000000-0000-4000-8000-0000000000fa';
    v_root    UUID := '10000000-0000-4000-8000-0000000000f1';
    v_app_ref UUID := '20000000-0000-4000-8000-0000000000f2';
    v_claim   UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();
    PERFORM claimius.upsert_app_user(p_user_id => v_a, p_first_name => 'Ada');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Foreign root', 'customer', v_root, v_root, 0, v_a);

    v_claim := (claimius.insert_claim(
        p_name => 'App reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_sa_access => 4, p_inherits => TRUE
    );
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_object_id => v_app_ref, p_object_type => 'claimius.samna_app', p_sa_access => 4, p_inherits => TRUE
    );
    PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_a);
END $$;

SELECT is(
    (SELECT count(*) FROM claimius.get_organizations('a0000000-0000-4000-8000-0000000000fa', 4)
      WHERE id = '10000000-0000-4000-8000-0000000000f1'),
    1::BIGINT,
    'a claim also bound to a table the disciple does not hold still grants its organization'
);

SELECT claimius._raise_claim_diff((SELECT id FROM claimius.claim WHERE name = 'App reader'));

SELECT lives_ok(
    $$SELECT count(*) FROM claimius.get_organizations('a0000000-0000-4000-8000-0000000000fa', 4)$$,
    'a read settles a raised claim diff over a table the disciple does not hold without error'
);

SELECT is_empty(
    $$SELECT 1 FROM claimius.access_diff
       WHERE kind = 'claim' AND claim_id = (SELECT id FROM claimius.claim WHERE name = 'App reader')$$,
    'the claim diff row clears after the read'
);

SELECT * FROM finish();
ROLLBACK;
