DO $$
DECLARE
    v_actor  UUID := 'd0000000-0000-4000-8000-00000000000d';
    v_orphan UUID := 'e0000000-0000-4000-8000-00000000000e';
    v_seed   TEXT;
BEGIN
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by, private_seed)
    VALUES (v_orphan, 'Orphaned owner', 'company', gen_random_uuid(), v_orphan, 0, v_actor, '');

    SELECT private_seed INTO v_seed FROM claimius.organization WHERE id = v_orphan;
    IF v_seed IS NULL THEN
        RAISE EXCEPTION 'an organization whose owner row is missing must keep an empty seed, got NULL';
    END IF;

    DELETE FROM claimius.organization WHERE id = v_orphan;
END $$;
