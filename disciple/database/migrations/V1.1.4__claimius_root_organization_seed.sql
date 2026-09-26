CREATE OR REPLACE FUNCTION claimius._set_self_owner_on_insert()
    RETURNS TRIGGER AS $$
DECLARE
    v_root UUID;
    v_zero CONSTANT UUID := '00000000-0000-0000-0000-000000000000'::uuid;
BEGIN
    IF NEW.sa_root_id  = v_zero THEN NEW.sa_root_id  := NULL; END IF;
    IF NEW.sa_owner_id = v_zero THEN NEW.sa_owner_id := NULL; END IF;

    IF (NEW.private_seed IS NULL OR NEW.private_seed = '')
       AND NEW.sa_owner_id IS NOT NULL AND NEW.sa_owner_id <> NEW.id THEN
        NEW.private_seed := coalesce((
            SELECT o.private_seed
            FROM claimius.organization o
            WHERE o.id = NEW.sa_owner_id
        ), '');
    END IF;

    IF NEW.sa_root_id IS NOT NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.sa_owner_id IS NULL OR NEW.sa_owner_id = NEW.id THEN
        NEW.sa_owner_id := NEW.id;
        NEW.sa_root_id  := NEW.id;
        RETURN NEW;
    END IF;

    SELECT sa_root_id INTO v_root
    FROM claimius.organization
    WHERE id = NEW.sa_owner_id;

    NEW.sa_root_id := coalesce(v_root, NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
