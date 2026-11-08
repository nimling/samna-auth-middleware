SELECT claimius.upsert_disciple_state();

DO $$
DECLARE
    v_a       UUID := 'a0000000-0000-4000-8000-00000000000a';
    v_b       UUID := 'b0000000-0000-4000-8000-00000000000b';
    v_root    UUID := '10000000-0000-4000-8000-000000000001';
    v_alpha   UUID := '10000000-0000-4000-8000-000000000011';
    v_bravo   UUID := '10000000-0000-4000-8000-000000000012';
    v_charlie UUID := '10000000-0000-4000-8000-000000000013';
    v_grand   UUID := '10000000-0000-4000-8000-000000000111';
    v_hall    UUID := '40000000-0000-4000-8000-000000000001';
    v_wing    UUID := '40000000-0000-4000-8000-000000000002';
    v_claim   UUID;
BEGIN
    PERFORM claimius.upsert_app_user(p_user_id => v_a, p_first_name => 'Ada', p_external_id => 'ext-a');
    PERFORM claimius.upsert_app_user(p_user_id => v_b, p_first_name => 'Bo', p_external_id => 'ext-b');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Root', 'company', v_root, v_root, 0, v_a);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_alpha, 'Alpha', 'company', v_root, v_root, 1, v_a),
           (v_bravo, 'Bravo', 'company', v_root, v_root, 1, v_a),
           (v_charlie, 'Charlie', 'company', v_root, v_root, 1, v_a);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_grand, 'Grand', 'company', v_alpha, v_root, 2, v_a);
    INSERT INTO claimius.location (id, name, sa_owner_id, sa_parent_id, sa_root_id, sa_created_by)
    VALUES (v_hall, 'Hall', v_root, v_hall, v_hall, v_a);
    INSERT INTO claimius.location (id, name, sa_owner_id, sa_parent_id, sa_root_id, sa_created_by)
    VALUES (v_wing, 'Wing', v_root, v_hall, v_hall, v_a);

    v_claim := (claimius.insert_claim(p_name => 'Root writer', p_description => NULL, p_sa_access => 6,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root,
        p_sa_created_by => v_a, p_object_id => v_root, p_object_type => 'claimius.organization', p_sa_access => 6);
    PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_a);

    v_claim := (claimius.insert_claim(p_name => 'Alpha reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_alpha, p_sa_root_id => v_root, p_sa_created_by => v_a) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_alpha, p_sa_root_id => v_root,
        p_sa_created_by => v_a, p_object_id => v_alpha, p_object_type => 'claimius.organization', p_sa_access => 4);
    PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_a, p_sa_owner_id => v_alpha, p_sa_created_by => v_a);
END $$;

DO $$
DECLARE
    v_b      UUID := 'b0000000-0000-4000-8000-00000000000b';
    v_guests UUID := '10000000-0000-4000-8000-000000000201';
    v_owner  UUID;
    v_claim  UUID;
BEGIN
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_guests, 'Guests', 'company', v_guests, v_guests, 0, v_b);

    v_owner := (claimius.insert_claim(p_name => 'Guests owner', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_guests, p_sa_root_id => v_guests, p_sa_created_by => v_b) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_claim_id => v_owner, p_sa_owner_id => v_guests, p_sa_root_id => v_guests,
        p_sa_created_by => v_b, p_object_id => v_guests, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_claim_id => v_owner, p_user_id => v_b, p_sa_owner_id => v_guests, p_sa_created_by => v_b);

    v_claim := (claimius.insert_claim(p_name => 'Guests reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_guests, p_sa_root_id => v_guests, p_sa_created_by => v_b) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_guests, p_sa_root_id => v_guests,
        p_sa_created_by => v_b, p_object_id => v_guests, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.update_claim_guest(v_claim, v_b, 'code');
    PERFORM claimius.insert_code(v_b, v_claim, encode(digest('hurl-guest-code', 'sha256'), 'hex'), 'guest',
        p_object_id => v_guests, p_object_type => 'claimius.organization', p_data => '{"view": "board"}');
    v_claim := (claimius.insert_claim(p_name => 'Guests invited', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_guests, p_sa_root_id => v_guests, p_sa_created_by => v_b) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_code(v_b, v_claim, encode(digest('hurl-invite-code', 'sha256'), 'hex'), 'invite');
END $$;
