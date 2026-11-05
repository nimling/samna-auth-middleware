CREATE OR REPLACE FUNCTION claimius.calc_object_access()
    RETURNS TRIGGER AS $$
DECLARE
    v_object_type   TEXT;
    v_obj_id        UUID;
    v_row           JSONB;
    v_root          UUID;
    v_op            TEXT;
    v_actor         UUID;
    v_structural    BOOLEAN := TRUE;
    v_replay        BOOLEAN := claimius.in_replay_mode();
BEGIN
    v_object_type := TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME;
    v_op := TG_OP;
    v_row := to_jsonb(coalesce(NEW, OLD));

    IF v_op = 'DELETE' THEN
        v_obj_id := OLD.id;
    ELSE
        v_obj_id := NEW.id;
    END IF;

    v_structural := claimius._retree_object(v_object_type, v_obj_id, to_jsonb(OLD), to_jsonb(NEW));
    IF v_op <> 'UPDATE' OR EXISTS (
        SELECT 1 FROM unnest(ARRAY['name', 'description', 'sa_owner_id', 'sa_location_id', 'sa_root_id', 'sa_deleted_at', 'sa_created_by']) AS k
        WHERE (to_jsonb(OLD) ->> k) IS DISTINCT FROM (to_jsonb(NEW) ->> k)
    ) THEN
        PERFORM claimius._write_object_info(v_object_type, ARRAY[v_obj_id]);
    END IF;

    IF v_structural THEN
        SELECT coalesce(o.sa_root_id, lo.sa_root_id) INTO v_root
        FROM (SELECT (v_row ->> 'sa_owner_id')::UUID AS owner_id, (v_row ->> 'sa_location_id')::UUID AS location_id) r
            LEFT JOIN claimius.organization o ON o.id = r.owner_id
            LEFT JOIN claimius.location l ON l.id = r.location_id
            LEFT JOIN claimius.organization lo ON lo.id = l.sa_owner_id;
    END IF;

    IF NOT v_replay THEN
        v_actor := claimius._actor_user_for_row(v_object_type, coalesce(NEW, OLD));
        IF v_actor IS NOT NULL THEN
            PERFORM claimius.recompute_user_object(v_actor, ARRAY[v_object_type], ARRAY[v_obj_id]);
        END IF;
    END IF;

    IF v_structural THEN
        PERFORM claimius._raise_object_diff(v_object_type, v_obj_id, FALSE, v_root);
    END IF;

    RETURN coalesce(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.calc_object_access() IS 'Trigger on regular registered tables. Maintains tree memberships, the object_info row and the actor''s pair, and raises the object area under the row''s owner root on a structural write.';

GRANT EXECUTE ON FUNCTION claimius.calc_object_access()
    TO claimius_disciple_client, claimius_reader;

DROP FUNCTION IF EXISTS claimius._attach_object_to_trees(TEXT, UUID, RECORD);
CREATE OR REPLACE FUNCTION claimius._attach_object_to_trees(
    p_object_type TEXT,
    p_object_id   UUID,
    p_row         JSONB
) RETURNS VOID AS $$
DECLARE
    v_owner_id  UUID;
    v_loc_id    UUID;
    v_owner_root UUID;
    v_loc_root  UUID;
BEGIN
    v_owner_id := (p_row ->> 'sa_owner_id')::UUID;
    v_loc_id   := (p_row ->> 'sa_location_id')::UUID;

    IF v_owner_id IS NOT NULL AND p_object_type <> 'claimius.organization' THEN
        SELECT e.root_id INTO v_owner_root
        FROM claimius.organization o
                 JOIN claimius.inheritance_info e
                      ON e.tree_type = 'ownership'
                          AND e.root_id = o.sa_root_id
                          AND e.descendant_type = 'claimius.organization'
                          AND e.descendant_id = o.id
                          AND e.depth = 0
        WHERE o.id = v_owner_id;
        IF v_owner_root IS NOT NULL THEN
            INSERT INTO claimius.inheritance_info (
                tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
            ) VALUES (
                'ownership', v_owner_root, p_object_type, p_object_id, p_object_type, p_object_id, 0
            ) ON CONFLICT DO NOTHING;
            INSERT INTO claimius.inheritance_info (
                tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
            )
            SELECT 'ownership', v_owner_root, e.ancestor_type, e.ancestor_id, p_object_type, p_object_id, e.depth + 1
            FROM claimius.inheritance_info e
            WHERE e.tree_type = 'ownership'
              AND e.root_id = v_owner_root
              AND e.descendant_type = 'claimius.organization'
              AND e.descendant_id   = v_owner_id
            ON CONFLICT DO NOTHING;
        END IF;
    END IF;

    IF v_loc_id IS NOT NULL THEN
        SELECT e.root_id INTO v_loc_root
        FROM claimius.location l
                 JOIN claimius.inheritance_info e
                      ON e.tree_type = 'location'
                          AND e.root_id = l.sa_root_id
                          AND e.descendant_type = 'claimius.location'
                          AND e.descendant_id = l.id
                          AND e.depth = 0
        WHERE l.id = v_loc_id;
        IF v_loc_root IS NOT NULL THEN
            INSERT INTO claimius.inheritance_info (
                tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
            ) VALUES (
                'location', v_loc_root, p_object_type, p_object_id, p_object_type, p_object_id, 0
            ) ON CONFLICT DO NOTHING;
            INSERT INTO claimius.inheritance_info (
                tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
            )
            SELECT 'location', v_loc_root, e.ancestor_type, e.ancestor_id, p_object_type, p_object_id, e.depth + 1
            FROM claimius.inheritance_info e
            WHERE e.tree_type = 'location'
              AND e.root_id = v_loc_root
              AND e.descendant_type = 'claimius.location'
              AND e.descendant_id   = v_loc_id
            ON CONFLICT DO NOTHING;
        END IF;
    END IF;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._attach_object_to_trees(TEXT, UUID, JSONB) IS 'Splices a row that is not a node of its own tree into its ownership tree and, when it carries one, its location tree: the self edge and the parent''s ancestor rows copied with depth + 1. A parent outside the closure leaves the row out. An organization attaches only its location pin.';

GRANT EXECUTE ON FUNCTION claimius._attach_object_to_trees(TEXT, UUID, JSONB)
    TO claimius_disciple_client, claimius_reader;

DROP FUNCTION IF EXISTS claimius._detach_object_from_trees(TEXT, UUID, RECORD);
CREATE OR REPLACE FUNCTION claimius._detach_object_from_trees(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS VOID AS $$
BEGIN
    DELETE FROM claimius.inheritance_info
    WHERE tree_type IN ('ownership', 'location')
      AND (tree_type = 'location' OR p_object_type <> 'claimius.organization')
      AND descendant_type = p_object_type
      AND descendant_id   = p_object_id;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._detach_object_from_trees(TEXT, UUID) IS 'Removes every closure row where a row that is not a node of its own tree is the descendant. An organization detaches only its location pin.';

GRANT EXECUTE ON FUNCTION claimius._detach_object_from_trees(TEXT, UUID)
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._retree_object(
    p_object_type TEXT,
    p_object_id   UUID,
    p_old         JSONB,
    p_new         JSONB
) RETURNS BOOLEAN AS $$
DECLARE
    v_was_active BOOLEAN := p_old IS NOT NULL AND (p_old ->> 'sa_deleted_at') IS NULL;
    v_is_active  BOOLEAN := p_new IS NOT NULL AND (p_new ->> 'sa_deleted_at') IS NULL;
    v_moved      BOOLEAN := (p_old ->> 'sa_owner_id') IS DISTINCT FROM (p_new ->> 'sa_owner_id')
                            OR (p_old ->> 'sa_location_id') IS DISTINCT FROM (p_new ->> 'sa_location_id');
BEGIN
    IF v_was_active AND (NOT v_is_active OR v_moved) THEN
        PERFORM claimius._detach_object_from_trees(p_object_type, p_object_id);
    END IF;
    IF v_is_active AND (NOT v_was_active OR v_moved) THEN
        PERFORM claimius._attach_object_to_trees(p_object_type, p_object_id, p_new);
    END IF;
    RETURN NOT (v_was_active AND v_is_active AND NOT v_moved);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._retree_object(TEXT, UUID, JSONB, JSONB) IS 'Moves one row''s ownership and location closure rows with a write. True when the write is structural.';

GRANT EXECUTE ON FUNCTION claimius._retree_object(TEXT, UUID, JSONB, JSONB)
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.calc_object_trees()
    RETURNS TRIGGER AS $$
BEGIN
    PERFORM claimius._retree_object(
            TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME, (coalesce(NEW, OLD)).id, to_jsonb(OLD), to_jsonb(NEW));
    RETURN coalesce(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.calc_object_trees() IS 'Trigger on owned tables outside the access targets. Maintains their closure rows.';

GRANT EXECUTE ON FUNCTION claimius.calc_object_trees()
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.calc_hierarchical_access()
    RETURNS TRIGGER AS $$
DECLARE
    v_object_type   TEXT;
    v_obj_id        UUID;
    v_root_id       UUID;
    v_old_root_id   UUID;
    v_owner_root    UUID;
    v_op            TEXT;
    v_was_active    BOOLEAN := FALSE;
    v_is_active     BOOLEAN := FALSE;
    v_tree          claimius.tree_type;
    v_parent_col    TEXT := 'sa_parent_id';
    v_old_parent    UUID;
    v_new_parent    UUID;
    v_actor         UUID;
    v_replay        BOOLEAN := claimius.in_replay_mode();
    v_renamed       BOOLEAN := FALSE;
    v_dropped_types TEXT[];
    v_dropped_ids   UUID[];
BEGIN
    v_object_type := TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME;
    v_op := TG_OP;

    IF v_op = 'DELETE' THEN
        v_obj_id := OLD.id;
    ELSE
        v_obj_id := NEW.id;
    END IF;

    IF v_object_type = 'claimius.organization' THEN
        v_tree := 'ownership';
        v_parent_col := 'sa_owner_id';
    ELSIF v_object_type = 'claimius.location' THEN
        v_tree := 'location';
    ELSE
        v_tree := 'parenthood';
    END IF;

    IF v_op <> 'DELETE' THEN
        v_root_id   := (to_jsonb(NEW) ->> 'sa_root_id')::UUID;
        v_new_parent := (to_jsonb(NEW) ->> v_parent_col)::UUID;
        v_is_active := (to_jsonb(NEW) ->> 'sa_deleted_at') IS NULL;
    END IF;
    IF v_op <> 'INSERT' THEN
        v_old_root_id := (to_jsonb(OLD) ->> 'sa_root_id')::UUID;
        v_old_parent := (to_jsonb(OLD) ->> v_parent_col)::UUID;
        v_was_active := (to_jsonb(OLD) ->> 'sa_deleted_at') IS NULL;
    END IF;

    IF v_op = 'UPDATE' AND v_old_root_id IS DISTINCT FROM v_root_id AND NOT v_replay
        AND current_setting('claimius.migrating_root', TRUE) IS DISTINCT FROM 'on' THEN
        RAISE EXCEPTION 'Cross root reparenting is not allowed. Use migrate_root() if absolutely required.';
    END IF;

    PERFORM pg_advisory_xact_lock(hashtext(coalesce(v_root_id, v_old_root_id)::TEXT));

    IF NOT v_is_active THEN
        SELECT array_agg(x.object_type), array_agg(x.object_id)
        INTO v_dropped_types, v_dropped_ids
        FROM claimius._splice_subtree(v_tree, v_object_type, v_obj_id, v_old_root_id, NULL) x;
    ELSIF v_op = 'INSERT' THEN
        INSERT INTO claimius.inheritance_info (
            tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
        ) VALUES (
            v_tree, v_root_id, v_object_type, v_obj_id, v_object_type, v_obj_id, 0
        )
        ON CONFLICT (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id)
            DO UPDATE SET depth = 0, sa_updated_at = now();
        IF v_new_parent <> v_obj_id THEN
            PERFORM claimius._splice_subtree(v_tree, v_object_type, v_obj_id, v_root_id, v_new_parent);
        END IF;
    ELSIF NOT v_was_active OR v_old_root_id IS DISTINCT FROM v_root_id OR NOT EXISTS (
        SELECT 1 FROM claimius.inheritance_info e
        WHERE e.tree_type = v_tree
          AND e.descendant_type = v_object_type
          AND e.descendant_id = v_obj_id
          AND e.depth = 0
    ) THEN
        IF v_tree = 'ownership' THEN
            PERFORM claimius.build_ownership_tree(v_root_id, v_obj_id);
        ELSIF v_tree = 'location' THEN
            PERFORM claimius.build_location_tree(v_root_id, v_obj_id);
        ELSE
            PERFORM claimius.build_parenthood_tree(v_object_type, v_root_id, v_obj_id);
        END IF;
    ELSIF v_old_parent IS DISTINCT FROM v_new_parent THEN
        PERFORM claimius._splice_subtree(v_tree, v_object_type, v_obj_id, v_root_id, v_new_parent);
    END IF;

    IF v_object_type = 'claimius.location' THEN
        PERFORM claimius._reattach_location_in_ownership(v_obj_id, NEW, OLD, v_op);
    ELSE
        PERFORM claimius._retree_object(v_object_type, v_obj_id, to_jsonb(OLD), to_jsonb(NEW));
    END IF;

    IF v_op <> 'UPDATE' OR EXISTS (
        SELECT 1 FROM unnest(ARRAY['name', 'description', 'sa_owner_id', 'sa_location_id', 'sa_root_id', 'sa_deleted_at', 'sa_created_by']) AS k
        WHERE (to_jsonb(OLD) ->> k) IS DISTINCT FROM (to_jsonb(NEW) ->> k)
    ) THEN
        PERFORM claimius._write_object_info(v_object_type, ARRAY[v_obj_id]);
    END IF;
    v_renamed := v_op = 'UPDATE'
                 AND to_jsonb(OLD) - ARRAY['name', 'description', 'sa_updated_at'] = to_jsonb(NEW) - ARRAY['name', 'description', 'sa_updated_at'];

    IF v_object_type = 'claimius.organization' THEN
        v_owner_root := coalesce(v_root_id, v_old_root_id);
    ELSE
        SELECT coalesce(o.sa_root_id, lo.sa_root_id) INTO v_owner_root
        FROM (SELECT (to_jsonb(coalesce(NEW, OLD)) ->> 'sa_owner_id')::UUID AS owner_id,
                     (to_jsonb(coalesce(NEW, OLD)) ->> 'sa_location_id')::UUID AS location_id) r
            LEFT JOIN claimius.organization o ON o.id = r.owner_id
            LEFT JOIN claimius.location l ON l.id = r.location_id
            LEFT JOIN claimius.organization lo ON lo.id = l.sa_owner_id;
    END IF;

    IF NOT v_replay THEN
        v_actor := claimius._actor_user_for_row(v_object_type, coalesce(NEW, OLD));
        IF v_actor IS NOT NULL THEN
            PERFORM claimius.recompute_user_object(v_actor, ARRAY[v_object_type], ARRAY[v_obj_id]);
        END IF;
    END IF;

    IF NOT v_renamed THEN
        PERFORM claimius._raise_object_diff(v_object_type, v_obj_id, TRUE, v_owner_root);
    END IF;
    IF v_dropped_ids IS NOT NULL THEN
        PERFORM claimius._raise_object_diff(x.object_type, x.object_id, FALSE, coalesce(oi.sa_owner_root_id, v_owner_root))
        FROM unnest(v_dropped_types, v_dropped_ids) AS x(object_type, object_id)
            LEFT JOIN claimius.object_info oi ON oi.object_type = x.object_type AND oi.object_id = x.object_id;
    END IF;

    RETURN coalesce(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.calc_hierarchical_access() IS 'Trigger on hierarchical tables. Maintains the closure incrementally, the object_info row and the actor''s pair, raises the cascade area of the node under its owner root, and on a soft delete one area per descendant whose closure rows went with it, so every holder''s next read recomputes its pairs on them.';

GRANT EXECUTE ON FUNCTION claimius.calc_hierarchical_access()
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._reattach_location_in_ownership(
    p_obj_id UUID, p_new RECORD, p_old RECORD, p_op TEXT
) RETURNS VOID AS $$
DECLARE
    v_old_owner UUID;
    v_new_owner UUID;
    v_was_in    BOOLEAN := FALSE;
    v_is_in     BOOLEAN := FALSE;
    v_new_root  UUID;
BEGIN
    IF p_op <> 'INSERT' THEN
        v_old_owner := (to_jsonb(p_old) ->> 'sa_owner_id')::UUID;
        v_was_in := (to_jsonb(p_old) ->> 'sa_deleted_at') IS NULL;
    END IF;
    IF p_op <> 'DELETE' THEN
        v_new_owner := (to_jsonb(p_new) ->> 'sa_owner_id')::UUID;
        v_is_in := (to_jsonb(p_new) ->> 'sa_deleted_at') IS NULL;
    END IF;

    IF v_was_in AND v_is_in AND v_old_owner IS NOT DISTINCT FROM v_new_owner THEN
        RETURN;
    END IF;

    DELETE FROM claimius.inheritance_info
    WHERE tree_type = 'ownership'
      AND descendant_type = 'claimius.location'
      AND descendant_id   = p_obj_id;

    IF v_is_in AND v_new_owner IS NOT NULL THEN
        SELECT e.root_id INTO v_new_root
        FROM claimius.organization o
                 JOIN claimius.inheritance_info e
                      ON e.tree_type = 'ownership'
                          AND e.root_id = o.sa_root_id
                          AND e.descendant_type = 'claimius.organization'
                          AND e.descendant_id = o.id
                          AND e.depth = 0
        WHERE o.id = v_new_owner;
        IF v_new_root IS NOT NULL THEN
            INSERT INTO claimius.inheritance_info (
                tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
            ) VALUES (
                'ownership', v_new_root, 'claimius.location', p_obj_id, 'claimius.location', p_obj_id, 0
            );
            INSERT INTO claimius.inheritance_info (
                tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
            )
            SELECT 'ownership', v_new_root, e.ancestor_type, e.ancestor_id, 'claimius.location', p_obj_id, e.depth + 1
            FROM claimius.inheritance_info e
            WHERE e.tree_type = 'ownership'
              AND e.root_id = v_new_root
              AND e.descendant_type = 'claimius.organization'
              AND e.descendant_id   = v_new_owner;
        END IF;
    END IF;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._reattach_location_in_ownership(UUID, RECORD, RECORD, TEXT) IS 'A location is also a node of its owner''s ownership tree: when the owner changes or the location is deleted or restored, drops its ownership rows and inserts its self edge and its live owner''s chain.';

GRANT EXECUTE ON FUNCTION claimius._reattach_location_in_ownership(UUID, RECORD, RECORD, TEXT)
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.calc_claim_access()
    RETURNS TRIGGER AS $$
DECLARE
    v_table       TEXT;
    v_claim_id    UUID;
    v_actor       UUID;
BEGIN
    IF claimius.in_replay_mode() THEN
        RETURN coalesce(NEW, OLD);
    END IF;

    v_table  := TG_TABLE_NAME;

    IF v_table = 'claim' THEN
        v_claim_id := coalesce(NEW.id, OLD.id);
        IF TG_OP <> 'UPDATE' OR (OLD.name, OLD.sa_access, OLD.level, OLD.inherits, OLD.sa_deleted_at)
                                  IS DISTINCT FROM (NEW.name, NEW.sa_access, NEW.level, NEW.inherits, NEW.sa_deleted_at) THEN
            PERFORM claimius._raise_claim_diff(v_claim_id, NULL, coalesce(OLD.inherits, FALSE) OR coalesce(NEW.inherits, FALSE));
        END IF;
    ELSIF v_table = 'user_claim' THEN
        v_claim_id := coalesce(NEW.claim_id, OLD.claim_id);
        PERFORM claimius._raise_claim_diff(v_claim_id, coalesce(NEW.user_id, OLD.user_id));
    ELSIF coalesce(NEW.object_type, OLD.object_type) = 'claimius.claim' THEN
        v_claim_id := coalesce(NEW.claim_id, OLD.claim_id);
        PERFORM claimius._raise_claim_diff(v_claim_id);
        PERFORM claimius._raise_claim_diff(coalesce(NEW.object_id, OLD.object_id));
    ELSIF coalesce(NEW.object_id, OLD.object_id) IS NULL THEN
        v_claim_id := coalesce(NEW.claim_id, OLD.claim_id);
    ELSE
        v_claim_id := coalesce(NEW.claim_id, OLD.claim_id);
        IF TG_OP <> 'INSERT' THEN
            PERFORM claimius._raise_object_diff(OLD.object_type, OLD.object_id, OLD.inherits OR coalesce(NEW.inherits, FALSE));
        END IF;
        IF TG_OP = 'INSERT' OR (TG_OP = 'UPDATE' AND (NEW.object_type, NEW.object_id) IS DISTINCT FROM (OLD.object_type, OLD.object_id)) THEN
            PERFORM claimius._raise_object_diff(NEW.object_type, NEW.object_id, NEW.inherits);
        END IF;
    END IF;

    v_actor := claimius._actor_user_for_row('claimius.' || v_table, coalesce(NEW, OLD));
    IF v_actor IS NOT NULL THEN
        PERFORM claimius.recompute_user_object(v_actor, ARRAY['claimius.claim'], ARRAY[v_claim_id]);
        IF v_table = 'claim_object' THEN
            IF coalesce(NEW.object_id, OLD.object_id) IS NOT NULL THEN
                PERFORM claimius.recompute_user_object(
                        v_actor, ARRAY[coalesce(NEW.object_type, OLD.object_type)], ARRAY[coalesce(NEW.object_id, OLD.object_id)]);
            END IF;
        END IF;
    END IF;

    RETURN coalesce(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.calc_claim_access() IS 'Trigger on claim, claim_object, user_claim. A claim whose name, mask, level, inherit flag or deletion changed raises an object area on every live binding of it and of the claims it links to, cascade where the binding inherits and the claim did before or after, and one on the claim object; a grant raises its assignee''s assignment area; a link raises both claims the same way; a binding on an object raises that object''s area; a scope binding raises nothing, since no pair carries it. Then settles the actor''s pairs on the claim and on a binding''s object.';

GRANT EXECUTE ON FUNCTION claimius.calc_claim_access()
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._denied_access(
    p_user_id     UUID,
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS INTEGER AS $$
    WITH held AS (
        SELECT duc.claim_id AS deny_id, 15 AS cap
        FROM claimius.user_claim duc
        WHERE duc.user_id = p_user_id
          AND duc.sa_deleted_at IS NULL
        UNION
        SELECT link.object_id, coalesce(link.sa_access & 15, 15)
        FROM claimius.user_claim huc
            JOIN claimius.claim h ON h.id = huc.claim_id AND h.sa_deleted_at IS NULL AND h.inherits
            JOIN claimius.claim_object link ON link.claim_id = h.id AND link.sa_deleted_at IS NULL
                 AND link.object_type = 'claimius.claim' AND link.object_id IS NOT NULL AND link.inherits
        WHERE huc.user_id = p_user_id
          AND huc.sa_deleted_at IS NULL
    )
    SELECT coalesce(bit_or(claimius._effective_access(d.sa_access, dco.sa_access) & held.cap & 15), 0)
    FROM held
        JOIN claimius.claim d ON d.id = held.deny_id AND d.sa_deleted_at IS NULL
        JOIN claimius.claim_object dco ON dco.claim_id = d.id AND dco.sa_deleted_at IS NULL
    WHERE (claimius._effective_access(d.sa_access, dco.sa_access) & 16) <> 0
      AND (
          (dco.object_type = p_object_type AND dco.object_id = p_object_id)
          OR (d.inherits AND dco.inherits AND EXISTS (
              SELECT 1 FROM claimius.inheritance_info a
              WHERE a.descendant_type = p_object_type
                AND a.descendant_id = p_object_id
                AND a.ancestor_type = dco.object_type
                AND a.ancestor_id = dco.object_id
                AND a.depth > 0))
      );
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius._denied_access(UUID, TEXT, UUID) IS 'The bits, owner, write, read and execute, of every live deny binding reaching the user on the object: through a grant the user holds, or through one inheriting link of a held inheriting claim, capped by the link''s mask; bound on the object itself, or on an ancestor in any tree where the claim and the binding inherit. Levels and classes are not applied; a caller masks the result with the user''s settled pair to keep only what the denies took away.';

GRANT EXECUTE ON FUNCTION claimius._denied_access(UUID, TEXT, UUID)
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._check_claim_composition()
    RETURNS TRIGGER AS $$
DECLARE
    v_user_id      UUID;
    v_has_access   BOOLEAN;
    v_actor_access INTEGER;
    v_effective    INTEGER;
    v_denied       INTEGER;
BEGIN
    IF claimius.in_replay_mode() THEN
        RETURN NEW;
    END IF;

    SELECT claimius._effective_access(c.sa_access, NEW.sa_access) INTO v_effective
    FROM claimius.claim c
    WHERE c.id = NEW.claim_id;

    IF NEW.object_id IS NOT NULL
        AND NEW.object_type IS DISTINCT FROM 'claimius.claim'
        AND NEW.sa_deleted_at IS NULL
        AND (coalesce(v_effective, 0) & 16) = 0
        AND (TG_OP = 'INSERT'
             OR NEW.sa_access IS DISTINCT FROM OLD.sa_access
             OR NEW.claim_id IS DISTINCT FROM OLD.claim_id
             OR NEW.object_id IS DISTINCT FROM OLD.object_id)
    THEN
        SELECT uc.user_id INTO v_user_id
        FROM claimius.user_claim uc
        WHERE uc.id = NEW.sa_created_by
          AND uc.sa_deleted_at IS NULL;

        IF v_user_id IS NOT NULL THEN
            PERFORM claimius.reconcile_if_pending(v_user_id, NEW.object_type, NEW.object_id);

            SELECT max(sa_access) INTO v_actor_access
            FROM claimius.user_object
            WHERE user_id     = v_user_id
              AND object_type = NEW.object_type
              AND object_id   = NEW.object_id;

            v_denied := claimius._denied_access(v_user_id, NEW.object_type, NEW.object_id) & ~coalesce(v_actor_access, 0);
            IF (coalesce(v_effective, 0) & v_denied & 15) <> 0 THEN
                RAISE EXCEPTION 'cannot grant access denied to the writer: user % is denied % on % %, requested %',
                    v_user_id, v_denied, NEW.object_type, NEW.object_id, v_effective USING ERRCODE = 'P0403';
            END IF;

            IF (coalesce(v_effective, 0) & ~coalesce(v_actor_access, 0) & 15) <> 0
                AND NEW.object_type = 'claimius.samna_user'
                AND NOT EXISTS (
                    SELECT 1 FROM claimius.claim_object co
                    WHERE co.object_type = 'claimius.samna_user'
                      AND co.object_id = NEW.object_id
                      AND co.sa_deleted_at IS NULL)
                AND EXISTS (
                    SELECT 1 FROM claimius.user_claim m
                    WHERE m.user_id = NEW.object_id
                      AND m.sa_owner_id = NEW.sa_owner_id
                      AND m.sa_deleted_at IS NULL)
            THEN
                PERFORM claimius.reconcile_if_pending(v_user_id, 'claimius.organization', NEW.sa_owner_id);

                IF EXISTS (
                    SELECT 1 FROM claimius.user_object
                    WHERE user_id     = v_user_id
                      AND object_type = 'claimius.organization'
                      AND object_id   = NEW.sa_owner_id
                      AND (sa_access & 1) <> 0)
                THEN
                    v_actor_access := 15;
                END IF;
            END IF;

            IF (coalesce(v_effective, 0) & 16) = 0 AND (coalesce(v_effective, 0) & ~coalesce(v_actor_access, 0) & 15) <> 0 THEN
                RAISE EXCEPTION 'cannot grant more access than held: user % holds % on % %, requested %',
                    v_user_id, coalesce(v_actor_access, 0), NEW.object_type, NEW.object_id, v_effective USING ERRCODE = 'P0403';
            END IF;
        END IF;
    END IF;

    IF NEW.object_type IS DISTINCT FROM 'claimius.claim' OR NEW.object_id IS NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.object_id = NEW.claim_id THEN
        RAISE EXCEPTION 'claim composition cycle: claim % cannot point at itself', NEW.claim_id;
    END IF;

    IF EXISTS (
        SELECT 1 FROM claimius.claim_object
        WHERE claim_id      = NEW.object_id
          AND object_type   = 'claimius.claim'
          AND object_id     = NEW.claim_id
          AND TRUE
          AND sa_deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'claim composition cycle: % already points at %', NEW.object_id, NEW.claim_id;
    END IF;

    SELECT uc.user_id INTO v_user_id
    FROM claimius.user_claim uc
    WHERE uc.id = NEW.sa_created_by
      AND uc.sa_deleted_at IS NULL;

    IF v_user_id IS NULL THEN
        RAISE EXCEPTION 'sa_created_by % is not a valid user_claim', NEW.sa_created_by;
    END IF;

    PERFORM claimius.reconcile_if_pending(v_user_id, 'claimius.claim', NEW.object_id);

    SELECT EXISTS (
        SELECT 1 FROM claimius.user_object
        WHERE user_id     = v_user_id
          AND TRUE
          AND object_type = 'claimius.claim'
          AND object_id   = NEW.object_id
          AND (sa_access & 4) <> 0
    ) INTO v_has_access;

    IF NOT v_has_access THEN
        RAISE EXCEPTION 'cannot compose: user % has no read access to claim %', v_user_id, NEW.object_id USING ERRCODE = 'P0403';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._check_claim_composition() IS 'BEFORE INSERT/UPDATE trigger on claim_object. Rejects a grant whose effective mask gives bits that a deny reaching the actor on the object removes from the actor''s settled pair, whatever the level of either claim, and a grant that hands out more access than the actor holds on the object; denies and removals are exempt and the claim''s level is never judged. The first binding on a samna_user holding a claim assigned under the binding''s owning organization may come from an actor holding owner on that organization. Enforces composition link invariants: no self loop, no reverse link cycle, caller has read on target claim. Bypassed during disciple replay.';

GRANT EXECUTE ON FUNCTION claimius._check_claim_composition()
    TO claimius_disciple_client, claimius_reader;

DROP TRIGGER IF EXISTS tg_check_claim_composition ON claimius.claim_object;
CREATE TRIGGER tg_check_claim_composition
    BEFORE INSERT OR UPDATE ON claimius.claim_object
    FOR EACH ROW EXECUTE FUNCTION claimius._check_claim_composition();

DROP FUNCTION IF EXISTS claimius._check_claim_level(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius._check_claim_denies(
    p_actor_uc  UUID,
    p_claim_id  UUID
) RETURNS VOID AS $$
DECLARE
    v_user_id UUID;
    v_binding RECORD;
    v_held    INTEGER;
    v_denied  INTEGER;
BEGIN
    SELECT uc.user_id INTO v_user_id
    FROM claimius.user_claim uc
    WHERE uc.id = p_actor_uc
      AND uc.sa_deleted_at IS NULL;
    IF v_user_id IS NULL THEN
        RETURN;
    END IF;

    FOR v_binding IN
        SELECT co.object_type, co.object_id, claimius._effective_access(c.sa_access, co.sa_access) & 15 AS bits
        FROM claimius.claim_object co
            JOIN claimius.claim c ON c.id = co.claim_id
        WHERE co.claim_id = p_claim_id
          AND co.sa_deleted_at IS NULL
          AND co.object_id IS NOT NULL
          AND co.object_type <> 'claimius.claim'
          AND (claimius._effective_access(c.sa_access, co.sa_access) & 16) = 0
    LOOP
        v_denied := claimius._denied_access(v_user_id, v_binding.object_type, v_binding.object_id) & v_binding.bits;
        CONTINUE WHEN v_denied = 0;

        PERFORM claimius.reconcile_if_pending(v_user_id, v_binding.object_type, v_binding.object_id);
        SELECT max(uo.sa_access) INTO v_held
        FROM claimius.user_object uo
        WHERE uo.user_id = v_user_id
          AND uo.object_type = v_binding.object_type
          AND uo.object_id = v_binding.object_id;

        v_denied := v_denied & ~coalesce(v_held, 0);
        IF v_denied <> 0 THEN
            RAISE EXCEPTION 'cannot hand out claim %: user % is denied % on % %',
                p_claim_id, v_user_id, v_denied, v_binding.object_type, v_binding.object_id USING ERRCODE = 'P0403';
        END IF;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._check_claim_denies(UUID, UUID) IS 'Refuses to hand out a claim that gives what is denied to the actor, the user of p_actor_uc: on no object the claim grants bits on may those bits overlap what a deny reaching the actor there removes from the actor''s settled pair. The level of the claim plays no part. An actor that is not a live user_claim passes.';

GRANT EXECUTE ON FUNCTION claimius._check_claim_denies(UUID, UUID)
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._check_claim_grant()
    RETURNS TRIGGER AS $$
BEGIN
    IF claimius.in_replay_mode() OR NEW.sa_deleted_at IS NOT NULL THEN
        RETURN NEW;
    END IF;
    IF TG_TABLE_NAME = 'claim' THEN
        PERFORM claimius._check_claim_denies(coalesce(nullif(current_setting('claimius.actor', TRUE), '')::UUID, NEW.sa_created_by), NEW.id);
    ELSE
        PERFORM claimius._check_claim_denies(NEW.sa_created_by, NEW.claim_id);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._check_claim_grant() IS 'Trigger on user_claim, and on claim when its level rises: runs _check_claim_denies for the actor in sa_created_by, so a grant of a claim, or a raise of its level, that gives what a deny removes from the actor is refused with SQLSTATE P0403.';

GRANT EXECUTE ON FUNCTION claimius._check_claim_grant()
    TO claimius_disciple_client, claimius_reader;

DROP TRIGGER IF EXISTS tg_check_claim_grant ON claimius.user_claim;
CREATE TRIGGER tg_check_claim_grant
    BEFORE INSERT OR UPDATE OF claim_id, user_id ON claimius.user_claim
    FOR EACH ROW EXECUTE FUNCTION claimius._check_claim_grant();

DROP TRIGGER IF EXISTS tg_check_claim_level ON claimius.claim;
CREATE TRIGGER tg_check_claim_level
    BEFORE UPDATE OF level ON claimius.claim
    FOR EACH ROW WHEN (NEW.level > OLD.level) EXECUTE FUNCTION claimius._check_claim_grant();

DROP FUNCTION IF EXISTS claimius._cascade_recompute(UUID, TEXT, UUID);

CREATE OR REPLACE FUNCTION claimius.emit_sync_event()
    RETURNS TRIGGER AS $$
DECLARE
    v_op    claimius.sync_operation;
    v_type  TEXT;
    v_payload JSONB;
BEGIN
    IF claimius.in_replay_mode() THEN
        RETURN coalesce(NEW, OLD);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM claimius.prophet_state) THEN
        RETURN coalesce(NEW, OLD);
    END IF;

    v_type := TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME;

    IF TG_OP = 'DELETE' THEN
        v_op := 'delete';
        v_payload := jsonb_build_object('id', (to_jsonb(OLD) ->> 'id'));
    ELSE
        v_op := 'upsert';
        v_payload := to_jsonb(NEW) - 'txid';
    END IF;

    INSERT INTO claimius.sync_event (operation, event_type, payload)
    VALUES (v_op, v_type, v_payload);

    RETURN coalesce(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.emit_sync_event() IS 'Trigger on the source tables. Publishes into sync_event.';

GRANT EXECUTE ON FUNCTION claimius.emit_sync_event()
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._settle_pair(
    p_user_id       UUID,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS TABLE (
                    settled             BOOLEAN,
                    sa_access           INTEGER,
                    grants              JSONB,
                    scope               JSONB,
                    direct_grant        BOOLEAN,
                    sa_owner_id         UUID,
                    sa_location_id      UUID,
                    sa_owner_root_id    UUID
                ) AS $$
DECLARE
    v_lock_timeout  TEXT := current_setting('lock_timeout');
    v_xmin          XID8;
    v_mark          XID8;
    v_trimmed       XID8;
    v_pair          claimius.user_object;
    v_info          claimius.object_info;
    v_has_pair      BOOLEAN := FALSE;
    v_live          BOOLEAN := FALSE;
    v_covered       BOOLEAN := FALSE;
    v_areas         claimius.access_diff[];
    v_settled       BOOLEAN := FALSE;
BEGIN
    PERFORM set_config('lock_timeout', claimius._settle_lock_timeout(), TRUE);
    BEGIN
        v_xmin := pg_snapshot_xmin(pg_current_snapshot());

        SELECT uo.* INTO v_pair
        FROM claimius.user_object uo
        WHERE uo.user_id = p_user_id
          AND uo.object_type = p_object_type
          AND uo.object_id = p_object_id;
        v_has_pair := FOUND;

        SELECT oi.* INTO v_info
        FROM claimius.object_info oi
        WHERE oi.object_type = p_object_type AND oi.object_id = p_object_id;
        v_live := FOUND;

        IF v_has_pair THEN
            v_mark := v_pair.settled_txid;
        ELSE
            SELECT a.settled_txid INTO v_mark
            FROM claimius.user_activity a
            WHERE a.user_id = p_user_id;
            v_mark := coalesce(v_mark, '0'::XID8);
        END IF;

        IF NOT v_live THEN
            IF v_has_pair THEN
                PERFORM claimius.recompute_user_object(p_user_id, ARRAY[p_object_type], ARRAY[p_object_id], NULL, v_xmin);
                v_has_pair := FALSE;
            END IF;
            v_settled := TRUE;
        ELSE
            SELECT ps.access_trimmed_txid INTO v_trimmed
            FROM claimius.disciple_state ps;

            IF v_mark < coalesce(v_trimmed, '0'::XID8) THEN
                v_covered := TRUE;
            ELSIF EXISTS (
                SELECT 1 FROM claimius.access_diff d
                WHERE d.kind = 'assignment'
                  AND d.user_id = p_user_id
                  AND d.txid >= v_mark) THEN
                v_covered := TRUE;
            ELSE
                IF v_info.sa_owner_root_id IS NULL THEN
                    v_areas := ARRAY(
                        SELECT d FROM claimius.access_diff d
                        WHERE d.root_id IS NULL AND d.txid >= v_mark
                        LIMIT 16);
                ELSE
                    v_areas := ARRAY(
                        SELECT d FROM claimius.access_diff d
                        WHERE d.root_id = v_info.sa_owner_root_id AND d.txid >= v_mark
                        LIMIT 16);
                END IF;

                IF cardinality(v_areas) = 0 THEN
                    v_settled := TRUE;
                ELSIF cardinality(v_areas) < 16 THEN
                    SELECT EXISTS (
                        SELECT 1
                        FROM unnest(v_areas) d
                        WHERE d.kind = 'object'
                          AND ((d.object_type = p_object_type AND d.object_id = p_object_id)
                               OR (d.cascade AND EXISTS (
                                   SELECT 1 FROM claimius.inheritance_info e
                                   WHERE e.descendant_type = p_object_type
                                     AND e.descendant_id = p_object_id
                                     AND e.ancestor_type = d.object_type
                                     AND e.ancestor_id = d.object_id
                                     AND e.depth > 0))))
                    INTO v_covered;
                ELSE
                    SELECT EXISTS (
                        SELECT 1
                        FROM (
                            SELECT p_object_type AS object_type, p_object_id AS object_id, TRUE AS self
                            UNION ALL
                            SELECT e.ancestor_type, e.ancestor_id, FALSE
                            FROM claimius.inheritance_info e
                            WHERE e.descendant_type = p_object_type
                              AND e.descendant_id = p_object_id
                              AND e.depth > 0
                        ) n
                            JOIN claimius.access_diff d
                                ON d.kind = 'object'
                               AND d.object_type = n.object_type
                               AND d.object_id = n.object_id
                        WHERE d.txid >= v_mark
                          AND (n.self OR d.cascade))
                    INTO v_covered;
                END IF;
            END IF;

            IF NOT v_settled THEN
                IF v_covered THEN
                    PERFORM claimius.recompute_user_object(p_user_id, ARRAY[p_object_type], ARRAY[p_object_id], NULL, v_xmin);
                    SELECT uo.* INTO v_pair
                    FROM claimius.user_object uo
                    WHERE uo.user_id = p_user_id
                      AND uo.object_type = p_object_type
                      AND uo.object_id = p_object_id;
                    v_has_pair := FOUND;
                ELSIF v_has_pair AND v_pair.settled_txid < v_xmin THEN
                    UPDATE claimius.user_object uo
                    SET settled_txid = v_xmin
                    WHERE uo.user_id = p_user_id
                      AND uo.object_type = p_object_type
                      AND uo.object_id = p_object_id;
                END IF;
                v_settled := TRUE;
            END IF;
        END IF;
    EXCEPTION WHEN lock_not_available OR deadlock_detected THEN
        v_settled := FALSE;
    END;
    PERFORM set_config('lock_timeout', v_lock_timeout, TRUE);

    RETURN QUERY
        SELECT v_settled,
               CASE WHEN v_has_pair THEN v_pair.sa_access END,
               CASE WHEN v_has_pair THEN v_pair.grants END,
               CASE WHEN v_has_pair THEN v_pair.scope END,
               CASE WHEN v_has_pair THEN v_pair.direct_grant END,
               v_info.sa_owner_id, v_info.sa_location_id, v_info.sa_owner_root_id;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._settle_pair(UUID, TEXT, UUID) IS 'The point settle and the read of one pair in one pass: reads the pair and the object''s object_info row, takes the mark from the pair or the cursor when there is no pair, and recomputes the pair when an area of the object''s root or an assignment of the user was raised at or after the mark on the object, with cascade on an ancestor, or when the mark is below the trimmed position; otherwise moves the mark to the snapshot xmin as a heap only update. An object with no object_info row loses its pair. Returns one row: whether the settle ran, false when a lock wait past the settle timeout or a deadlock left the pair as it stands, with the pair''s mask, grants, scope and creator flag, NULL when the user holds no pair, and the object''s owner, location and owner root.';

GRANT EXECUTE ON FUNCTION claimius._settle_pair(UUID, TEXT, UUID)
    TO claimius_disciple_client, claimius_reader;

DROP FUNCTION IF EXISTS claimius.reconcile_if_pending(UUID, TEXT, UUID);
CREATE OR REPLACE FUNCTION claimius.reconcile_if_pending(
    p_user_id       UUID,
    p_object_type   TEXT DEFAULT NULL,
    p_object_id     UUID DEFAULT NULL
)
    RETURNS BOOLEAN AS $$
DECLARE
    v_lock_timeout  TEXT := current_setting('lock_timeout');
    v_settled       BOOLEAN := FALSE;
BEGIN
    IF p_user_id IS NULL THEN
        RETURN FALSE;
    END IF;
    PERFORM set_config('lock_timeout', claimius._settle_lock_timeout(), TRUE);

    IF p_object_type IS NOT NULL AND p_object_id IS NOT NULL THEN
        SELECT s.settled INTO v_settled FROM claimius._settle_pair(p_user_id, p_object_type, p_object_id) s;
    ELSIF pg_try_advisory_xact_lock(hashtextextended('claimius.share:' || p_user_id::TEXT, 0)) THEN
        v_settled := claimius._settle_diff_share(p_user_id);
        IF v_settled THEN
            PERFORM claimius._settle_usage_diff(p_user_id);
        END IF;
    END IF;
    PERFORM set_config('lock_timeout', v_lock_timeout, TRUE);
    RETURN v_settled;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.reconcile_if_pending(UUID, TEXT, UUID) IS 'Settles what a read meets. With a target it settles the one pair through _settle_pair. Without a target it settles the reader''s share through _settle_diff_share under the share lock and folds its usage. Returns whether the settle ran: false when the share lock was held by another read or a lock wait past the settle timeout or a deadlock left the pair as it stands. Called at read entry points.';

GRANT EXECUTE ON FUNCTION claimius.reconcile_if_pending(UUID, TEXT, UUID)
    TO claimius_disciple_client, claimius_reader;

DROP FUNCTION IF EXISTS claimius.drain_access_diff(INTEGER);
CREATE OR REPLACE FUNCTION claimius.drain_access_diff(
    p_limit_pairs   INTEGER DEFAULT 1000
) RETURNS TABLE (settled INTEGER, failures TEXT[]) AS $$
DECLARE
    v_lock_timeout TEXT := current_setting('lock_timeout');
    v_user      RECORD;
    v_since     TIMESTAMPTZ;
    v_areas     INTEGER;
BEGIN
    settled := 0;
    failures := ARRAY[]::TEXT[];
    PERFORM set_config('lock_timeout', claimius._settle_lock_timeout(), TRUE);
    BEGIN
        FOR v_user IN
            SELECT a.user_id, a.settled_txid, a.settled_at, a.last_used_at
            FROM claimius.user_activity a
            WHERE a.settled_txid < (SELECT d.txid FROM claimius.access_diff d ORDER BY d.txid DESC LIMIT 1)
            ORDER BY a.last_used_at DESC
            LIMIT least(greatest(p_limit_pairs, 0), 64)
        LOOP
            SELECT count(*), min(x.sa_created_at)
            INTO v_areas, v_since
            FROM (
                SELECT d.sa_created_at
                FROM claimius.access_diff d
                WHERE d.txid >= v_user.settled_txid
                LIMIT 1000
            ) x;
            v_since := coalesce(v_user.settled_at, v_since, now());
            CONTINUE WHEN extract(epoch FROM now() - v_since) < v_areas * extract(epoch FROM now() - v_user.last_used_at);
            CONTINUE WHEN NOT pg_try_advisory_xact_lock(hashtextextended('claimius.share:' || v_user.user_id::TEXT, 0));
            IF claimius._settle_diff_share(v_user.user_id) THEN
                settled := settled + 1;
            END IF;
        END LOOP;
    EXCEPTION
        WHEN lock_not_available OR deadlock_detected THEN
            NULL;
        WHEN OTHERS THEN
            failures := failures || ('settle: ' || SQLERRM);
    END;
    PERFORM set_config('lock_timeout', v_lock_timeout, TRUE);

    BEGIN
        PERFORM claimius._settle_usage_diff(NULL, greatest(p_limit_pairs, 0));
    EXCEPTION WHEN OTHERS THEN
        failures := failures || ('usage_diff: ' || SQLERRM);
    END;

    BEGIN
        PERFORM claimius._settle_user_users_diff(NULL, greatest(p_limit_pairs, 0));
    EXCEPTION WHEN OTHERS THEN
        failures := failures || ('user_users_diff: ' || SQLERRM);
    END;
    RETURN NEXT;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.drain_access_diff(INTEGER) IS 'Settles shares ahead of their reads, off the request path: takes the users whose cursor is below the newest area, most recently used first and at most 64, and settles each whose read rate 1 / (now - last_used_at) is at least the raise rate n / (now - settled_at) of the n areas since its cursor, under its own share lock. Then folds the usage_diff and user_users_diff rows of the oldest p_limit_pairs objects. Each of the three steps runs in its own subtransaction, so a failing step leaves the others to run: returns the shares settled and one message per failed step, which the caller logs. Trimming is trim_access_diff, run by the retention job.';

GRANT EXECUTE ON FUNCTION claimius.drain_access_diff(INTEGER)
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.trim_access_diff(
    p_keep  INTERVAL DEFAULT INTERVAL '7 days'
) RETURNS BIGINT AS $$
DECLARE
    v_trimmed   XID8;
    v_low       XID8;
    v_deleted   BIGINT;
BEGIN
    SELECT ds.access_trimmed_txid INTO v_trimmed FROM claimius.disciple_state ds LIMIT 1;
    IF NOT FOUND THEN
        RETURN 0;
    END IF;
    SELECT min(a.settled_txid) INTO v_low
    FROM claimius.user_activity a
    WHERE a.last_used_at > now() - p_keep;
    v_low := coalesce(v_low, pg_snapshot_xmin(pg_current_snapshot()));
    IF v_low <= v_trimmed THEN
        RETURN 0;
    END IF;
    DELETE FROM claimius.access_diff d WHERE d.txid < v_low;
    GET DIAGNOSTICS v_deleted = ROW_COUNT;
    UPDATE claimius.disciple_state SET access_trimmed_txid = v_low;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.trim_access_diff(INTERVAL) IS 'Trims every area below the lowest cursor among the users used within p_keep, or below the oldest running transaction when no user was, and records that position as access_trimmed_txid on disciple_state. A reader whose cursor is below it resettles its whole reach. Returns the areas removed. The access_diff retention prune.';

GRANT EXECUTE ON FUNCTION claimius.trim_access_diff(INTERVAL)
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.cascade_user_soft_delete(p_user_id UUID)
    RETURNS VOID AS $$
BEGIN
    UPDATE claimius.user_claim
    SET sa_deleted_at = now()
    WHERE user_id = p_user_id AND TRUE AND sa_deleted_at IS NULL;

    UPDATE claimius.user_relation
    SET sa_deleted_at = now()
    WHERE user_id = p_user_id AND TRUE AND sa_deleted_at IS NULL;

    UPDATE claimius.user_field
    SET sa_deleted_at = now()
    WHERE user_id = p_user_id AND TRUE AND sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.cascade_user_soft_delete(p_user_id uuid) IS 'Propagates soft delete from samna_user to dependent rows.';

GRANT EXECUTE ON FUNCTION claimius.cascade_user_soft_delete(p_user_id uuid)
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.check_user_active(p_user_id UUID)
    RETURNS BOOLEAN AS $$
DECLARE
    v_status        claimius.user_status;
    v_deleted       TIMESTAMPTZ;
BEGIN
    SELECT status, sa_deleted_at INTO v_status, v_deleted
    FROM claimius.samna_user
    WHERE user_id = p_user_id AND TRUE;

    IF v_status IS NULL THEN
        RETURN FALSE;
    END IF;

    IF v_status <> 'active' OR v_deleted IS NOT NULL THEN
        RETURN FALSE;
    END IF;

    RETURN TRUE;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.check_user_active(p_user_id uuid) IS 'Predicate: returns true when the user row exists, has status=active, and is not soft deleted.';

GRANT EXECUTE ON FUNCTION claimius.check_user_active(p_user_id uuid)
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._cascade_user_soft_delete_trg()
    RETURNS TRIGGER AS $$
BEGIN
    IF claimius.in_replay_mode() THEN
        RETURN NEW;
    END IF;
    IF OLD.sa_deleted_at IS NULL AND NEW.sa_deleted_at IS NOT NULL THEN
        PERFORM claimius.cascade_user_soft_delete(NEW.user_id);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._cascade_user_soft_delete_trg() IS 'Trigger wrapper: fires cascade_user_soft_delete once on samna_user soft delete transition. Inert under replay_mode, where the cascaded rows arrive in the feed.';

GRANT EXECUTE ON FUNCTION claimius._cascade_user_soft_delete_trg()
    TO claimius_disciple_client, claimius_reader;

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

COMMENT ON FUNCTION claimius._set_self_owner_on_insert() IS 'BEFORE INSERT for organizations. Self references on root creation; copies parent org sa_root_id otherwise.';

GRANT EXECUTE ON FUNCTION claimius._set_self_owner_on_insert()
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._set_self_root_on_insert()
    RETURNS TRIGGER AS $$
DECLARE
    v_root UUID;
    v_zero CONSTANT UUID := '00000000-0000-0000-0000-000000000000'::uuid;
BEGIN
    IF NEW.sa_root_id   = v_zero THEN NEW.sa_root_id   := NULL; END IF;
    IF NEW.sa_parent_id = v_zero THEN NEW.sa_parent_id := NULL; END IF;

    IF NEW.sa_root_id IS NOT NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.sa_parent_id IS NULL OR NEW.sa_parent_id = NEW.id THEN
        NEW.sa_parent_id := NEW.id;
        NEW.sa_root_id   := NEW.id;
        RETURN NEW;
    END IF;

    SELECT sa_root_id INTO v_root
    FROM claimius.location
    WHERE id = NEW.sa_parent_id;

    NEW.sa_root_id := coalesce(v_root, NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._set_self_root_on_insert() IS 'BEFORE INSERT for locations. Self references on root creation; copies parent location sa_root_id otherwise.';

GRANT EXECUTE ON FUNCTION claimius._set_self_root_on_insert()
    TO claimius_disciple_client, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._set_self_parent_on_insert()
    RETURNS TRIGGER AS $$
DECLARE
    v_root  UUID;
    v_sql   TEXT;
    v_zero  CONSTANT UUID := '00000000-0000-0000-0000-000000000000'::uuid;
BEGIN
    IF NEW.sa_root_id   = v_zero THEN NEW.sa_root_id   := NULL; END IF;
    IF NEW.sa_parent_id = v_zero THEN NEW.sa_parent_id := NULL; END IF;

    IF NEW.sa_root_id IS NOT NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.sa_parent_id IS NULL OR NEW.sa_parent_id = NEW.id THEN
        NEW.sa_parent_id := NEW.id;
        NEW.sa_root_id   := NEW.id;
        RETURN NEW;
    END IF;

    v_sql := format('SELECT sa_root_id FROM %I.%I WHERE id = $1', TG_TABLE_SCHEMA, TG_TABLE_NAME);
    EXECUTE v_sql INTO v_root USING NEW.sa_parent_id;

    NEW.sa_root_id := coalesce(v_root, NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._set_self_parent_on_insert() IS 'BEFORE INSERT for parenthood tables. Self references on root creation; copies parent sa_root_id from the same table otherwise.';

GRANT EXECUTE ON FUNCTION claimius._set_self_parent_on_insert()
    TO claimius_disciple_client, claimius_reader;
CREATE OR REPLACE FUNCTION claimius.recompute_external_access(
    p_user_id UUID
) RETURNS INTEGER AS $$
DECLARE
    v_table RECORD;
    v_ids   UUID[];
    v_count INTEGER := 0;
BEGIN
    FOR v_table IN
        SELECT object_type, schema_name, table_name, has_sa_deleted_at
        FROM claimius.table_info
        WHERE is_internal = FALSE
        LOOP
            EXECUTE format(
                'SELECT ARRAY(SELECT id FROM %I.%I%s UNION SELECT object_id FROM claimius.user_object WHERE user_id = $1 AND object_type = $2)',
                v_table.schema_name,
                v_table.table_name,
                CASE WHEN v_table.has_sa_deleted_at THEN ' WHERE sa_deleted_at IS NULL' ELSE '' END
            ) INTO v_ids USING p_user_id, v_table.object_type;

            IF cardinality(v_ids) > 0 THEN
                PERFORM claimius.recompute_user_object(p_user_id, array_fill(v_table.object_type, ARRAY[cardinality(v_ids)]), v_ids);
                v_count := v_count + cardinality(v_ids);
            END IF;
        END LOOP;

    RETURN v_count;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.recompute_external_access(p_user_id uuid) IS 'Rebuilds user_object for one user over every external registered table, one set based recompute per table. Not called by the sync, which raises the areas each applied row moves for the reads to settle; a manual rebuild for tables that exist only on the disciple.';

GRANT EXECUTE ON FUNCTION claimius.recompute_external_access(p_user_id uuid)
    TO claimius_disciple_client, claimius_reader;
