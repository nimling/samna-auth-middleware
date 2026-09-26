-- ============================================================================
-- Claimius V2.2 Internal Functions
-- ----------------------------------------------------------------------------
-- Functions used internally by triggers, init, and other claimius internals.
-- Not exposed to consumers. These functions assume valid inputs and may
-- raise on misuse.
-- ============================================================================

-- ----------------------------------------------------------------------------
-- Tree builders. Used by init, hierarchy reparenting, and bulk operations.
-- Each rewrites every edge in inheritance_info for the given tree from live
-- data. Self-edges (depth = 0) included so a node is its own ancestor.
-- ----------------------------------------------------------------------------

-- build_ownership_tree
-- Rewrites the ownership tree closure rooted at p_root_id from live data.
-- Includes org-org edges plus edges from every registered table whose rows
-- point at any org in the tree via sa_owner_id.
CREATE OR REPLACE FUNCTION claimius.build_ownership_tree(p_root_id UUID)
    RETURNS VOID AS $$
BEGIN
    -- Serialize concurrent rebuilds of the same root. Without this, two
    -- transactions both delete against their own snapshot and collide on
    -- the closure insert with a duplicate inheritance_info_pkey.
    PERFORM pg_advisory_xact_lock(hashtext(p_root_id::TEXT));

    DELETE FROM claimius.inheritance_info
    WHERE tree_type = 'ownership' AND root_id = p_root_id;

    -- Org-org edges. Walk down recursively, recording the path of ancestors
    -- to each node, then explode the path into one edge per ancestor.
    INSERT INTO claimius.inheritance_info (
        tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
    )
    WITH RECURSIVE org_paths AS (
        SELECT id AS node_id, ARRAY[id]::UUID[] AS path
        FROM claimius.organization
        WHERE id = p_root_id AND sa_deleted_at IS NULL
        UNION ALL
        SELECT o.id, p.path || o.id
        FROM claimius.organization o
                 JOIN org_paths p ON o.sa_owner_id = p.node_id
        WHERE o.id <> o.sa_owner_id AND o.sa_deleted_at IS NULL
    )
    SELECT 'ownership'::claimius.tree_type, p_root_id,
           'claimius.organization'::TEXT, p.path[i]::UUID,
           'claimius.organization'::TEXT, p.node_id,
           array_length(p.path, 1) - i
    FROM org_paths p
             CROSS JOIN LATERAL generate_subscripts(p.path, 1) AS i;

    -- Registered-table edges. For each row r in any registered table whose
    -- owner is in the org tree, emit:
    --   self-edge (r, r, 0)
    --   plus (ancestor_of_r.sa_owner_id, r, depth + 1) for each org ancestor
    INSERT INTO claimius.inheritance_info (
        tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
    )
    SELECT 'ownership'::claimius.tree_type, p_root_id, t.object_type, r.id, t.object_type, r.id, 0
    FROM claimius.table_info t
             CROSS JOIN LATERAL claimius._select_owned_rows(t.object_type, p_root_id) r
    WHERE t.has_sa_owner_id
      AND t.has_sa_deleted_at
      AND t.object_type <> 'claimius.organization'
    UNION ALL
    SELECT 'ownership'::claimius.tree_type, p_root_id, e.ancestor_type, e.ancestor_id, t.object_type, r.id, e.depth + 1
    FROM claimius.table_info t
             CROSS JOIN LATERAL claimius._select_owned_rows(t.object_type, p_root_id) r
             JOIN claimius.inheritance_info e
                  ON e.tree_type = 'ownership'
                      AND e.root_id = p_root_id
                      AND e.descendant_type = 'claimius.organization'
                      AND e.descendant_id = r.sa_owner_id
    WHERE t.has_sa_owner_id
      AND t.has_sa_deleted_at
      AND t.object_type <> 'claimius.organization';
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.build_ownership_tree(UUID) IS 'Rewrites ownership tree closure rooted at p_root_id from live data.';

-- build_location_tree
-- Rewrites the location tree closure rooted at p_root_id from live data.
-- Includes location-location edges plus edges from every registered table
-- whose rows point at any location in the tree via sa_location_id.
CREATE OR REPLACE FUNCTION claimius.build_location_tree(p_root_id UUID)
    RETURNS VOID AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext(p_root_id::TEXT));

    DELETE FROM claimius.inheritance_info
    WHERE tree_type = 'location' AND root_id = p_root_id;

    INSERT INTO claimius.inheritance_info (
        tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
    )
    WITH RECURSIVE loc_paths AS (
        SELECT id AS node_id, ARRAY[id]::UUID[] AS path
        FROM claimius.location
        WHERE id = p_root_id AND sa_deleted_at IS NULL
        UNION ALL
        SELECT l.id, p.path || l.id
        FROM claimius.location l
                 JOIN loc_paths p ON l.sa_parent_id = p.node_id
        WHERE l.id <> l.sa_parent_id AND l.sa_deleted_at IS NULL
    )
    SELECT 'location'::claimius.tree_type, p_root_id,
           'claimius.location'::TEXT, p.path[i]::UUID,
           'claimius.location'::TEXT, p.node_id,
           array_length(p.path, 1) - i
    FROM loc_paths p
             CROSS JOIN LATERAL generate_subscripts(p.path, 1) AS i;

    INSERT INTO claimius.inheritance_info (
        tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
    )
    SELECT 'location'::claimius.tree_type, p_root_id, t.object_type, r.id, t.object_type, r.id, 0
    FROM claimius.table_info t
             CROSS JOIN LATERAL claimius._select_located_rows(t.object_type, p_root_id) r
    WHERE t.has_sa_location_id
      AND t.has_sa_deleted_at
    UNION ALL
    SELECT 'location'::claimius.tree_type, p_root_id, e.ancestor_type, e.ancestor_id, t.object_type, r.id, e.depth + 1
    FROM claimius.table_info t
             CROSS JOIN LATERAL claimius._select_located_rows(t.object_type, p_root_id) r
             JOIN claimius.inheritance_info e
                  ON e.tree_type = 'location'
                      AND e.root_id = p_root_id
                      AND e.descendant_type = 'claimius.location'
                      AND e.descendant_id = r.sa_location_id
    WHERE t.has_sa_location_id
      AND t.has_sa_deleted_at;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.build_location_tree(UUID) IS 'Rewrites location tree closure rooted at p_root_id from live data.';

-- build_parenthood_tree
-- Rewrites the parenthood tree closure for one root of a self referencing
-- registered table. Walks the table's sa_parent_id chain only.
CREATE OR REPLACE FUNCTION claimius.build_parenthood_tree(p_object_type TEXT, p_root_id UUID)
    RETURNS VOID AS $$
DECLARE
    v_schema TEXT;
    v_table TEXT;
    v_parts TEXT[];
    v_sql TEXT;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext(p_root_id::TEXT));

    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    DELETE FROM claimius.inheritance_info
    WHERE tree_type = 'parenthood' AND root_id = p_root_id AND ancestor_type = p_object_type;

    v_sql := format($f$
        INSERT INTO claimius.inheritance_info (
            tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
        )
        WITH RECURSIVE node_paths AS (
            SELECT id AS node_id, ARRAY[id]::UUID[] AS path
            FROM %I.%I
            WHERE id = $1 AND sa_deleted_at IS NULL
            UNION ALL
            SELECT t.id, p.path || t.id
            FROM %I.%I t
            JOIN node_paths p ON t.sa_parent_id = p.node_id
            WHERE t.id <> t.sa_parent_id AND t.sa_deleted_at IS NULL
        )
        SELECT 'parenthood'::claimius.tree_type, $1,
               %L::TEXT, p.path[i]::UUID,
               %L::TEXT, p.node_id,
               array_length(p.path, 1) - i
        FROM node_paths p
                 CROSS JOIN LATERAL generate_subscripts(p.path, 1) AS i
    $f$, v_schema, v_table, v_schema, v_table, p_object_type, p_object_type);

    EXECUTE v_sql USING p_root_id;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.build_parenthood_tree(TEXT, UUID) IS 'Rewrites parenthood tree closure for one root of a self referencing registered table.';

-- _select_owned_rows
-- Returns (id, sa_owner_id) for live rows of an object_type whose owner is
-- in the same root tree as p_root_id. Implemented via dynamic SQL.
CREATE OR REPLACE FUNCTION claimius._select_owned_rows(p_object_type TEXT, p_root_id UUID)
    RETURNS TABLE(id UUID, sa_owner_id UUID) AS $$
DECLARE
    v_schema TEXT;
    v_table TEXT;
    v_parts TEXT[];
    v_sql TEXT;
    v_has_deleted BOOLEAN;
    v_deleted_clause TEXT;
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    -- Look up whether this table has sa_deleted_at. Audit and similar
    -- append-only tables don't, so we omit the soft-delete filter for them.
    SELECT has_sa_deleted_at INTO v_has_deleted
    FROM claimius.table_info WHERE object_type = p_object_type;

    v_deleted_clause := CASE WHEN coalesce(v_has_deleted, FALSE)
                                 THEN 'AND r.sa_deleted_at IS NULL'
                             ELSE ''
        END;

    v_sql := format($f$
        WITH RECURSIVE org_tree AS (
            SELECT id FROM claimius.organization WHERE id = $1 AND sa_deleted_at IS NULL
            UNION ALL
            SELECT o.id FROM claimius.organization o JOIN org_tree t ON o.sa_owner_id = t.id
            WHERE o.id <> o.sa_owner_id AND o.sa_deleted_at IS NULL
        )
        SELECT r.id::UUID, r.sa_owner_id::UUID
        FROM %I.%I r
        WHERE r.sa_owner_id IN (SELECT id FROM org_tree)
          %s
    $f$, v_schema, v_table, v_deleted_clause);
    RETURN QUERY EXECUTE v_sql USING p_root_id;
END;
$$ LANGUAGE plpgsql STABLE;

-- _select_located_rows
-- Returns (id, sa_location_id) for live rows of an object_type whose
-- location is in the same root tree as p_root_id.
CREATE OR REPLACE FUNCTION claimius._select_located_rows(p_object_type TEXT, p_root_id UUID)
    RETURNS TABLE(id UUID, sa_location_id UUID) AS $$
DECLARE
    v_schema TEXT;
    v_table TEXT;
    v_parts TEXT[];
    v_sql TEXT;
    v_has_deleted BOOLEAN;
    v_deleted_clause TEXT;
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    SELECT has_sa_deleted_at INTO v_has_deleted
    FROM claimius.table_info WHERE object_type = p_object_type;

    v_deleted_clause := CASE WHEN coalesce(v_has_deleted, FALSE)
                                 THEN 'AND r.sa_deleted_at IS NULL'
                             ELSE ''
        END;

    v_sql := format($f$
        WITH RECURSIVE loc_tree AS (
            SELECT id FROM claimius.location WHERE id = $1 AND sa_deleted_at IS NULL
            UNION ALL
            SELECT l.id FROM claimius.location l JOIN loc_tree t ON l.sa_parent_id = t.id
            WHERE l.id <> l.sa_parent_id AND l.sa_deleted_at IS NULL
        )
        SELECT r.id::UUID, r.sa_location_id::UUID
        FROM %I.%I r
        WHERE r.sa_location_id IN (SELECT id FROM loc_tree)
          %s
    $f$, v_schema, v_table, v_deleted_clause);
    RETURN QUERY EXECUTE v_sql USING p_root_id;
END;
$$ LANGUAGE plpgsql STABLE;

-- ----------------------------------------------------------------------------
-- Registration helpers
-- ----------------------------------------------------------------------------

-- _register_table_info
-- Inspects a table's columns, validates required columns, and populates the
-- table_info row. Raises if required columns are missing.
CREATE OR REPLACE FUNCTION claimius._register_table_info(p_object_type TEXT, p_is_internal BOOLEAN)
    RETURNS VOID AS $$
DECLARE
    v_schema            TEXT;
    v_table             TEXT;
    v_parts             TEXT[];
    v_has_owner         BOOLEAN;
    v_has_location      BOOLEAN;
    v_has_parent        BOOLEAN;
    v_has_root          BOOLEAN;
    v_has_created_by    BOOLEAN;
    v_has_deleted_at    BOOLEAN;
    v_has_created_at    BOOLEAN;
    v_has_updated_at    BOOLEAN;
    v_has_name          BOOLEAN;
    v_has_description   BOOLEAN;
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    -- table must exist
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = v_schema AND table_name = v_table
    ) THEN
        RAISE EXCEPTION 'Table %.% does not exist', v_schema, v_table;
    END IF;

    v_has_owner       := claimius.table_has_column(p_object_type, 'sa_owner_id');
    v_has_location    := claimius.table_has_column(p_object_type, 'sa_location_id');
    v_has_parent      := claimius.table_has_column(p_object_type, 'sa_parent_id');
    v_has_root        := claimius.table_has_column(p_object_type, 'sa_root_id');
    v_has_created_by  := claimius.table_has_column(p_object_type, 'sa_created_by');
    v_has_deleted_at  := claimius.table_has_column(p_object_type, 'sa_deleted_at');
    v_has_created_at  := claimius.table_has_column(p_object_type, 'sa_created_at');
    v_has_updated_at  := claimius.table_has_column(p_object_type, 'sa_updated_at');
    v_has_name        := claimius.table_has_column(p_object_type, 'name');
    v_has_description := claimius.table_has_column(p_object_type, 'description');

    -- Required columns for external tables. Internal tables are exempt
    -- because they include claimius internal computed tables that follow
    -- different rules.
    IF NOT p_is_internal THEN
        IF NOT v_has_owner THEN
            RAISE EXCEPTION 'Table % missing required column sa_owner_id', p_object_type;
        END IF;
        IF NOT v_has_created_by THEN
            RAISE EXCEPTION 'Table % missing required column sa_created_by', p_object_type;
        END IF;
        IF NOT v_has_created_at THEN
            RAISE EXCEPTION 'Table % missing required column sa_created_at', p_object_type;
        END IF;
        IF NOT v_has_updated_at THEN
            RAISE EXCEPTION 'Table % missing required column sa_updated_at', p_object_type;
        END IF;
        IF NOT v_has_deleted_at THEN
            RAISE EXCEPTION 'Table % missing required column sa_deleted_at', p_object_type;
        END IF;
        -- sa_parent_id and sa_root_id must come together
        IF v_has_parent <> v_has_root THEN
            RAISE EXCEPTION 'Table % must have both sa_parent_id and sa_root_id, or neither', p_object_type;
        END IF;

    END IF;

    INSERT INTO claimius.table_info (
        schema_name, table_name, is_internal,
        has_sa_owner_id, has_sa_location_id, has_sa_parent_id, has_sa_root_id,
        has_sa_created_by, has_sa_deleted_at, has_sa_created_at, has_sa_updated_at,
        has_name, has_description
    ) VALUES (
                 v_schema, v_table, p_is_internal,
                 v_has_owner, v_has_location, v_has_parent, v_has_root,
                 v_has_created_by, v_has_deleted_at, v_has_created_at, v_has_updated_at,
                 v_has_name, v_has_description
             )
    ON CONFLICT (object_type) DO UPDATE SET
                                            is_internal = EXCLUDED.is_internal,
                                            has_sa_owner_id = EXCLUDED.has_sa_owner_id,
                                            has_sa_location_id = EXCLUDED.has_sa_location_id,
                                            has_sa_parent_id = EXCLUDED.has_sa_parent_id,
                                            has_sa_root_id = EXCLUDED.has_sa_root_id,
                                            has_sa_created_by = EXCLUDED.has_sa_created_by,
                                            has_sa_deleted_at = EXCLUDED.has_sa_deleted_at,
                                            has_sa_created_at = EXCLUDED.has_sa_created_at,
                                            has_sa_updated_at = EXCLUDED.has_sa_updated_at,
                                            has_name = EXCLUDED.has_name,
                                            has_description = EXCLUDED.has_description,
                                            sa_updated_at = now();
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._register_table_info(TEXT, BOOLEAN) IS 'Registers a table in table_info, validating required columns.';

-- _attach_calc_trigger
-- Attaches the appropriate calc trigger to a registered table. Drops any
-- existing trigger first so the operation is idempotent.
CREATE OR REPLACE FUNCTION claimius._attach_calc_trigger(p_object_type TEXT)
    RETURNS VOID AS $$
DECLARE
    v_schema        TEXT;
    v_table         TEXT;
    v_parts         TEXT[];
    v_has_parent    BOOLEAN;
    v_has_root      BOOLEAN;
    v_func_name     TEXT;
    v_trigger_name  TEXT := 'tg_calc_access';
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    SELECT t.has_sa_parent_id, t.has_sa_root_id
    INTO v_has_parent, v_has_root
    FROM claimius.table_info t WHERE t.object_type = p_object_type;

    IF v_has_parent AND v_has_root THEN
        v_func_name := 'claimius.calc_hierarchical_access';
    ELSE
        v_func_name := 'claimius.calc_object_access';
    END IF;

    EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I.%I', v_trigger_name, v_schema, v_table);
    EXECUTE format(
            'CREATE TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I.%I FOR EACH ROW EXECUTE FUNCTION %s()',
            v_trigger_name, v_schema, v_table, v_func_name
            );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._attach_calc_trigger(TEXT) IS 'Attaches the right calc trigger to a registered table.';

-- _detach_calc_trigger
-- Removes the calc trigger from a previously registered table. Used during
-- rebuild when a table is dropped from the input list.
CREATE OR REPLACE FUNCTION claimius._detach_calc_trigger(p_object_type TEXT)
    RETURNS VOID AS $$
DECLARE
    v_parts TEXT[];
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = v_parts[1] AND table_name = v_parts[2]) THEN
        EXECUTE format('DROP TRIGGER IF EXISTS tg_calc_access ON %I.%I', v_parts[1], v_parts[2]);
    END IF;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._detach_calc_trigger(TEXT) IS 'Removes calc trigger from a deregistered table.';

-- _attach_self_root_trigger
-- Attaches the appropriate self root BEFORE INSERT trigger to a registered
-- table based on its column shape. Picks one of three trigger functions:
--   has_sa_parent_id + has_sa_root_id -> _set_self_parent_on_insert
--   has_sa_root_id only               -> _set_self_root_on_insert
--   has_sa_owner_id + has_sa_root_id  -> _set_self_owner_on_insert
-- Tables without sa_root_id get no trigger (nothing to self reference).
-- Drops any existing trigger first so the operation is idempotent.
CREATE OR REPLACE FUNCTION claimius._attach_self_root_trigger(p_object_type TEXT)
    RETURNS VOID AS $$
DECLARE
    v_schema        TEXT;
    v_table         TEXT;
    v_parts         TEXT[];
    v_has_owner     BOOLEAN;
    v_has_parent    BOOLEAN;
    v_has_root      BOOLEAN;
    v_func_name     TEXT;
    v_trigger_name  TEXT := 'tg_self_root';
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    SELECT t.has_sa_owner_id, t.has_sa_parent_id, t.has_sa_root_id
    INTO v_has_owner, v_has_parent, v_has_root
    FROM claimius.table_info t WHERE t.object_type = p_object_type;

    -- No sa_root_id: nothing to self reference, skip.
    IF NOT v_has_root THEN
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I.%I', v_trigger_name, v_schema, v_table);
        RETURN;
    END IF;

    IF v_has_parent THEN
        -- Parenthood table: parent + root self reference together.
        v_func_name := 'claimius._set_self_parent_on_insert';
    ELSIF v_has_owner THEN
        -- Owner only (organization): owner + root self reference together.
        v_func_name := 'claimius._set_self_owner_on_insert';
    ELSE
        -- Root only (location): only root self references; parent stays NULL.
        v_func_name := 'claimius._set_self_root_on_insert';
    END IF;

    EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I.%I', v_trigger_name, v_schema, v_table);
    EXECUTE format(
            'CREATE TRIGGER %I BEFORE INSERT ON %I.%I FOR EACH ROW EXECUTE FUNCTION %s()',
            v_trigger_name, v_schema, v_table, v_func_name
            );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._attach_self_root_trigger(TEXT) IS 'Attaches the right self root BEFORE INSERT trigger to a registered table.';

-- _detach_self_root_trigger
-- Removes the self root trigger from a previously registered table.
CREATE OR REPLACE FUNCTION claimius._detach_self_root_trigger(p_object_type TEXT)
    RETURNS VOID AS $$
DECLARE
    v_parts TEXT[];
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = v_parts[1] AND table_name = v_parts[2]) THEN
        EXECUTE format('DROP TRIGGER IF EXISTS tg_self_root ON %I.%I', v_parts[1], v_parts[2]);
    END IF;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._detach_self_root_trigger(TEXT) IS 'Removes self root trigger from a deregistered table.';

-- ----------------------------------------------------------------------------
-- Calc functions: the heart of access maintenance
-- ----------------------------------------------------------------------------

-- _user_id_for_user_claim
-- Looks up the user_id for a given user_claim.id. Used to translate
-- sa_created_by into a user reference. Returns NULL if no match (which
-- happens during bootstrap before user_claims exist).
CREATE OR REPLACE FUNCTION claimius._user_id_for_user_claim(p_user_claim_id UUID)
    RETURNS TABLE(user_id UUID) AS $$
BEGIN
    RETURN QUERY
        SELECT uc.user_id
        FROM claimius.user_claim uc
        WHERE uc.id = p_user_claim_id AND uc.sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE;

-- _best_grant
-- Strongest surviving claim grant in a user_object grants array for one user
-- whose mask covers p_required_access. Inlinable SRF shared by get_access,
-- get_objects, and search so every read path resolves the actor token
-- identically.
CREATE OR REPLACE FUNCTION claimius._best_grant(
    p_grants          JSONB,
    p_user_id         UUID,
    p_required_access INTEGER
) RETURNS TABLE (
                    user_claim_id    UUID,
                    claim_id         UUID,
                    claim_name       TEXT,
                    access_bits      INTEGER,
                    grant_scope      JSONB,
                    claim_created_at TIMESTAMPTZ
                ) LANGUAGE sql STABLE AS $$
    SELECT
        uc.id,
        c.id,
        c.name,
        (g ->> 'access')::INTEGER,
        g -> 'scope',
        c.sa_created_at
    FROM jsonb_array_elements(p_grants) g
             JOIN claimius.user_claim uc ON uc.claim_id = (g ->> 'claim_id')::UUID
        AND uc.user_id = p_user_id
        AND uc.sa_deleted_at IS NULL
        AND (uc.starts_at IS NULL OR uc.starts_at <= now())
        AND (uc.ends_at IS NULL OR uc.ends_at > now())
             JOIN claimius.claim c ON c.id = uc.claim_id AND c.sa_deleted_at IS NULL
    WHERE ((g ->> 'access')::INTEGER & p_required_access) = p_required_access
    ORDER BY claimius._popcount((g ->> 'access')::INTEGER) DESC, (g ->> 'access')::INTEGER DESC, c.sa_created_at ASC
    LIMIT 1
$$;

COMMENT ON FUNCTION claimius._best_grant(JSONB, UUID, INTEGER) IS 'Strongest surviving claim grant in a user_object grants array for one user whose mask covers p_required_access. Inlinable SRF shared by get_access, get_objects, and search so every read path resolves the actor token identically.';

-- _user_claim_grants
-- Returns the user's direct claim grants on claimius.samna_user. Shared by
-- get_user_access and get_users so the direct branch resolves identically.
CREATE OR REPLACE FUNCTION claimius._user_claim_grants(
    p_actor_id       UUID,
    p_target_user_id UUID DEFAULT NULL
) RETURNS TABLE (
                    target_user_id UUID,
                    access         INTEGER,
                    user_claim_id  UUID,
                    claim_id       UUID,
                    claim_name     TEXT,
                    owner_id       UUID,
                    root_id        UUID
                ) LANGUAGE sql STABLE AS $$
    SELECT
        co.object_id,
        (c.sa_access | COALESCE(co.sa_access, c.sa_access)) & 15,
        uc.id,
        c.id,
        c.name,
        co.sa_owner_id,
        co.sa_root_id
    FROM claimius.user_claim uc
             JOIN claimius.claim c ON c.id = uc.claim_id AND c.sa_deleted_at IS NULL
             JOIN claimius.claim_object co ON co.claim_id = c.id
        AND co.object_type = 'claimius.samna_user'
        AND co.sa_deleted_at IS NULL
        AND (p_target_user_id IS NULL OR co.object_id = p_target_user_id)
    WHERE uc.user_id = p_actor_id
      AND uc.sa_deleted_at IS NULL
      AND (uc.starts_at IS NULL OR uc.starts_at <= now())
      AND (uc.ends_at IS NULL OR uc.ends_at > now())
      AND ((c.sa_access | COALESCE(co.sa_access, c.sa_access)) & 16) = 0
$$;

-- _build_grants_for_object
-- Computes the grants jsonb array for one (user, object) pair by walking
-- claim_objects bound directly, via inheritance trees, or via composition
-- links (a claim_object row whose object_type is 'claimius.claim'). Applies
-- deny semantics. Returns an empty array if no grants survive.
CREATE OR REPLACE FUNCTION claimius._build_grants_for_object(
    p_user_id     UUID,
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS JSONB AS $$
DECLARE
    v_grants     JSONB := '[]'::JSONB;
    v_denies     JSONB := '[]'::JSONB;
    v_row        RECORD;
    v_grant_bits INTEGER;
    v_deny_bits  INTEGER;
    v_is_deny    BOOLEAN;
BEGIN
    FOR v_row IN
        WITH user_claims AS (
            SELECT uc.claim_id, c.sa_access AS claim_access, c.inherits, c.sa_deleted_at
            FROM claimius.user_claim uc
                JOIN claimius.claim c ON c.id = uc.claim_id
            WHERE uc.user_id = p_user_id
              AND TRUE
              AND uc.sa_deleted_at IS NULL
              AND c.sa_deleted_at IS NULL
        ),
        direct_bindings AS (
            SELECT co.claim_id,
                   p_object_type AS cascaded_from_type,
                   p_object_id   AS cascaded_from_id,
                   'direct'::TEXT AS tree_type,
                   (uc.claim_access | COALESCE(co.sa_access, uc.claim_access)) AS sa_access,
                   co.scope
            FROM claimius.claim_object co
                JOIN user_claims uc ON uc.claim_id = co.claim_id
            WHERE co.object_type = p_object_type
              AND co.object_id = p_object_id
              AND TRUE
              AND co.sa_deleted_at IS NULL
        ),
        cascaded_bindings AS (
            SELECT co.claim_id,
                   co.object_type AS cascaded_from_type,
                   co.object_id   AS cascaded_from_id,
                   e.tree_type::TEXT AS tree_type,
                   (uc.claim_access | COALESCE(co.sa_access, uc.claim_access)) AS sa_access,
                   co.scope
            FROM claimius.claim_object co
                JOIN user_claims uc ON uc.claim_id = co.claim_id AND uc.inherits = TRUE
                JOIN claimius.inheritance_info e
                    ON e.ancestor_type = co.object_type
                   AND e.ancestor_id   = co.object_id
                   AND e.descendant_type = p_object_type
                   AND e.descendant_id   = p_object_id
                   AND e.depth > 0
            WHERE TRUE
              AND co.sa_deleted_at IS NULL
              AND co.inherits = TRUE
        ),
        composed_bindings AS (
            SELECT uc.claim_id,
                   'claimius.claim'::TEXT AS cascaded_from_type,
                   link.object_id         AS cascaded_from_id,
                   'composition'::TEXT    AS tree_type,
                   (
                       (
                           COALESCE(link.sa_access, uc.claim_access)
                           & COALESCE(co.sa_access, cb.sa_access)
                       )
                       | (
                           (COALESCE(link.sa_access, uc.claim_access) | COALESCE(co.sa_access, cb.sa_access))
                           & 16
                       )
                   ) AS sa_access,
                   co.scope
            FROM claimius.claim_object link
                JOIN user_claims uc ON uc.claim_id = link.claim_id AND link.inherits = TRUE
                JOIN claimius.claim cb
                    ON cb.id = link.object_id
                   AND cb.sa_deleted_at IS NULL
                   AND cb.inherits = TRUE
                JOIN claimius.claim_object co
                    ON co.claim_id      = link.object_id
                   AND co.object_type   = p_object_type
                   AND co.object_id     = p_object_id
                   AND TRUE
                   AND co.sa_deleted_at IS NULL
            WHERE link.object_type = 'claimius.claim'
              AND TRUE
              AND link.sa_deleted_at IS NULL
        )
        SELECT * FROM direct_bindings
        UNION ALL
        SELECT * FROM cascaded_bindings
        UNION ALL
        SELECT * FROM composed_bindings
    LOOP
        v_is_deny := (v_row.sa_access & 16) <> 0;
        IF v_is_deny THEN
            v_deny_bits := v_row.sa_access & 15;
            v_denies := v_denies || jsonb_build_array(jsonb_build_object(
                'claim_id', v_row.claim_id,
                'access', v_deny_bits,
                'scope', v_row.scope,
                'cascaded_from', jsonb_build_object('type', v_row.cascaded_from_type, 'id', v_row.cascaded_from_id),
                'tree_type', v_row.tree_type
            ));
        ELSE
            v_grant_bits := v_row.sa_access & 15;
            v_grants := v_grants || jsonb_build_array(jsonb_build_object(
                'claim_id', v_row.claim_id,
                'access', v_grant_bits,
                'scope', v_row.scope,
                'cascaded_from', jsonb_build_object('type', v_row.cascaded_from_type, 'id', v_row.cascaded_from_id),
                'tree_type', v_row.tree_type
            ));
        END IF;
    END LOOP;

    IF jsonb_array_length(v_denies) > 0 THEN
        v_grants := claimius._apply_denies(v_grants, v_denies, p_object_type, p_object_id);
    END IF;

    RETURN v_grants;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._build_grants_for_object(p_user_id uuid, p_object_type text, p_object_id uuid) IS 'Computes grants jsonb for a (user, object) pair. Walks direct, hierarchy cascaded, and claim composition paths. Applies deny subtraction: direct denies clear every grant, inherited denies clear inherited grants.';

-- _apply_denies
-- Subtracts deny bits from the grants a deny covers. A direct deny clears
-- every grant on the object. An inherited deny clears inherited grants in any
-- tree and spares grants bound on the object itself. Returns a new jsonb array
-- of grants. Grants whose access mask becomes 0 after subtraction are dropped.
CREATE OR REPLACE FUNCTION claimius._apply_denies(
    p_grants        JSONB,
    p_denies        JSONB,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS JSONB AS $$
DECLARE
    v_result        JSONB := '[]'::JSONB;
    v_grant         JSONB;
    v_deny          JSONB;
    v_object_key    JSONB;
    v_grant_bits    INTEGER;
    v_deny_bits     INTEGER;
BEGIN
    v_object_key := jsonb_build_object('type', p_object_type, 'id', p_object_id);

    FOR v_grant IN SELECT * FROM jsonb_array_elements(p_grants) LOOP
            v_grant_bits := (v_grant ->> 'access')::INTEGER;
            FOR v_deny IN SELECT * FROM jsonb_array_elements(p_denies) LOOP
                v_deny_bits := (v_deny ->> 'access')::INTEGER;
                -- A direct deny on the object itself subtracts from any
                -- grant on any path.
                IF (v_deny ->> 'tree_type') = 'direct' AND (v_deny -> 'cascaded_from') = v_object_key THEN
                    v_grant_bits := v_grant_bits & ~v_deny_bits;
                -- An inherited deny covers every descendant, whichever tree
                -- it travelled. It does not touch a grant bound directly on
                -- the object, which is how one object is shared out of a
                -- denied subtree.
                ELSIF (v_grant ->> 'tree_type') NOT IN ('direct', 'composition') THEN
                    v_grant_bits := v_grant_bits & ~v_deny_bits;
                END IF;
                EXIT WHEN v_grant_bits = 0;
            END LOOP;
            IF v_grant_bits <> 0 THEN
                v_result := v_result || jsonb_build_array(jsonb_set(v_grant, '{access}', to_jsonb(v_grant_bits)));
            END IF;
        END LOOP;

    RETURN v_result;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

COMMENT ON FUNCTION claimius._apply_denies(p_grants jsonb, p_denies jsonb, p_object_type text, p_object_id uuid) IS 'Subtracts deny bits from grants the deny covers. A direct deny clears every grant on the object; an inherited deny covers every descendant in any tree and leaves grants bound directly on the object intact. Drops grants whose mask becomes 0.';

-- _direct_grant_for_object
-- Determines whether the given user holds direct grant on the given object
-- via sa_created_by chain. A direct grant exists when there is a row in
-- the registered table with this id whose sa_created_by maps (via user_claim
-- lookup) to this user_id and AND no deny at level 0 on this object
-- blocks them.
CREATE OR REPLACE FUNCTION claimius._direct_grant_for_object(
    p_user_id       UUID,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS BOOLEAN AS $$
DECLARE
    v_schema        TEXT;
    v_table         TEXT;
    v_parts         TEXT[];
    v_sql           TEXT;
    v_creator_uc_id UUID;
    v_creator_user  UUID;
    v_has_deny      BOOLEAN;
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    IF p_object_type = 'claimius.samna_user' THEN
        IF NOT EXISTS (
            SELECT 1 FROM claimius.samna_user
            WHERE id = p_object_id
              AND user_id = p_user_id
              AND TRUE
              AND sa_deleted_at IS NULL
        ) THEN
            RETURN FALSE;
        END IF;
    ELSE
        v_sql := format('SELECT sa_created_by FROM %I.%I WHERE id = $1 AND sa_deleted_at IS NULL', v_schema, v_table);
        EXECUTE v_sql INTO v_creator_uc_id USING p_object_id;

        IF v_creator_uc_id IS NULL THEN
            RETURN FALSE;
        END IF;

        IF v_creator_uc_id <> p_user_id THEN
            SELECT uc.user_id INTO v_creator_user
            FROM claimius.user_claim uc
            WHERE uc.id = v_creator_uc_id AND uc.sa_deleted_at IS NULL;

            IF v_creator_user IS NULL OR v_creator_user <> p_user_id THEN
                RETURN FALSE;
            END IF;
        END IF;
    END IF;

    -- A creator's grant is wiped only by a deny bound directly on this
    -- object whose effective mask carries the deny bit (0x10) and the owner
    -- bit (0x01). A deny inherited from an ancestor never removes it, so an
    -- author always reaches what they made.
    SELECT EXISTS (
        SELECT 1
        FROM claimius.user_claim uc
                 JOIN claimius.claim c ON c.id = uc.claim_id
                 JOIN claimius.claim_object co ON co.claim_id = c.id
        WHERE uc.user_id = p_user_id
          AND TRUE
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
          AND TRUE
          AND co.sa_deleted_at IS NULL
          AND ((c.sa_access | co.sa_access) & 16) <> 0
          AND ((c.sa_access | co.sa_access) & 1)  <> 0
          AND co.object_type = p_object_type
          AND co.object_id = p_object_id
    ) INTO v_has_deny;

    RETURN NOT v_has_deny;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._direct_grant_for_object(p_user_id uuid, p_object_type text, p_object_id uuid) IS 'Determines if user has direct (creator) grant. Only a deny bound directly on the object with the owner bit removes it.';

-- _denormalize_object
-- Reads the registered row's name, description, sa_owner_id, sa_location_id,
-- sa_root_id, and produces the denormalized values for user_object.
CREATE OR REPLACE FUNCTION claimius._denormalize_object(
    p_object_type   TEXT,
    p_object_id     UUID,
    OUT sa_name TEXT,
    OUT sa_description TEXT,
    OUT sa_link TEXT,
    OUT sa_owner_id UUID,
    OUT sa_location_id UUID,
    OUT sa_root_id UUID
) AS $$
#variable_conflict use_variable
DECLARE
    v_schema        TEXT;
    v_table         TEXT;
    v_parts         TEXT[];
    v_info          claimius.table_info%ROWTYPE;
    v_sql           TEXT;
    v_select_cols   TEXT;
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    SELECT * INTO v_info FROM claimius.table_info WHERE object_type = p_object_type;

    IF v_info IS NULL THEN
        RETURN;
    END IF;

    v_select_cols := format(
            '%s, %s, %s, %s, %s',
            CASE WHEN v_info.has_name THEN 'name::TEXT' ELSE 'NULL::TEXT' END,
            CASE WHEN v_info.has_description THEN 'description::TEXT' ELSE 'NULL::TEXT' END,
            CASE WHEN v_info.has_sa_owner_id THEN 'sa_owner_id' ELSE 'NULL::UUID' END,
            CASE WHEN v_info.has_sa_location_id THEN 'sa_location_id' ELSE 'NULL::UUID' END,
            CASE WHEN v_info.has_sa_root_id THEN 'sa_root_id' ELSE 'NULL::UUID' END
                     );

    v_sql := format('SELECT %s FROM %I.%I WHERE id = $1', v_select_cols, v_schema, v_table);
    EXECUTE v_sql INTO sa_name, sa_description, sa_owner_id, sa_location_id, sa_root_id USING p_object_id;

    -- Fallback: tables that carry no sa_root_id column derive the root from
    -- the owning organization or, failing that, the parent location.
    IF sa_root_id IS NULL AND sa_owner_id IS NOT NULL THEN
        SELECT o.sa_root_id INTO sa_root_id
        FROM claimius.organization o
        WHERE o.id = sa_owner_id;
    END IF;

    IF sa_root_id IS NULL AND sa_location_id IS NOT NULL THEN
        SELECT l.sa_root_id INTO sa_root_id
        FROM claimius.location l
        WHERE l.id = sa_location_id;
    END IF;

    sa_link := NULL;  -- apps may set this via update_object_metadata; left NULL by default
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._denormalize_object(p_object_type text, p_object_id uuid, OUT sa_name text, OUT sa_description text, OUT sa_link text, OUT sa_owner_id uuid, OUT sa_location_id uuid, OUT sa_root_id uuid) IS 'Reads name/description/owner/location/root from a registered row. Falls back to organization or location sa_root_id when the row carries no sa_root_id column.';

-- recompute_user_object
-- The single function that recomputes the user_object row for one
-- (user_id, object_type, object_id) tuple. It handles insert,
-- update, and delete based on whether grants survive. Also maintains the
-- companion object_users row. The user_users companion is rebuilt once per
-- viewer by the calling orchestrator after its recompute loop settles, since
-- a full rebuild per object is redundant when the viewer's object_users set
-- is only final at the end of the cascade.
CREATE OR REPLACE FUNCTION claimius.recompute_user_object(
    p_user_id       UUID,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS VOID AS $$
DECLARE
    v_grants        JSONB;
    v_direct        BOOLEAN;
    v_id            UUID;
    v_eff_access    INTEGER;
    v_eff_scope     JSONB;
    v_grant_obj     JSONB;
    v_any_unbounded BOOLEAN;
    v_norm          RECORD;
    v_search_vec    TSVECTOR;
    v_has_soft      BOOLEAN;
    v_obj_gone      BOOLEAN;
BEGIN
    IF p_object_type IS NULL OR p_object_id IS NULL THEN
        RETURN;
    END IF;

    v_id := claimius.composite_id(p_user_id::TEXT, p_object_type, p_object_id::TEXT);

    SELECT has_sa_deleted_at INTO v_has_soft
    FROM claimius.table_info
    WHERE object_type = p_object_type;

    IF v_has_soft IS TRUE THEN
        EXECUTE format(
            'SELECT NOT EXISTS (SELECT 1 FROM %I.%I WHERE id = $1 AND sa_deleted_at IS NULL)',
            split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2)
        ) USING p_object_id INTO v_obj_gone;

        IF v_obj_gone THEN
            DELETE FROM claimius.user_object WHERE id = v_id;
            DELETE FROM claimius.object_users
            WHERE object_id = p_object_id
              AND object_type = p_object_type AND user_id = p_user_id;
            RETURN;
        END IF;
    END IF;

    v_grants := claimius._build_grants_for_object(p_user_id, p_object_type, p_object_id);
    v_direct := claimius._direct_grant_for_object(p_user_id, p_object_type, p_object_id);

    -- No surviving access at all: delete the user_object row and companions
    IF jsonb_array_length(v_grants) = 0 AND NOT v_direct THEN
        DELETE FROM claimius.user_object WHERE id = v_id;
        DELETE FROM claimius.object_users
        WHERE TRUE AND object_id = p_object_id
          AND object_type = p_object_type AND user_id = p_user_id;
        RETURN;
    END IF;

    -- Effective access mask: bitwise OR over all surviving grants. Direct
    -- creator contributes the full operator mask (owner | write | read |
    -- execute). Deny semantics already applied in _build_grants_for_object.
    IF v_direct THEN
        v_eff_access := 15;
    ELSE
        v_eff_access := 0;
    END IF;
    FOR v_grant_obj IN SELECT * FROM jsonb_array_elements(v_grants) LOOP
        v_eff_access := v_eff_access | (v_grant_obj ->> 'access')::INTEGER;
    END LOOP;

    -- Effective scope: any grant without a scope grants the whole object,
    -- so the merged scope is NULL. Otherwise union the keys of every
    -- grant's scope object.
    v_any_unbounded := v_direct;
    v_eff_scope := NULL;
    IF NOT v_any_unbounded THEN
        FOR v_grant_obj IN SELECT * FROM jsonb_array_elements(v_grants) LOOP
            IF (v_grant_obj -> 'scope') IS NULL OR jsonb_typeof(v_grant_obj -> 'scope') = 'null' THEN
                v_any_unbounded := TRUE;
                EXIT;
            END IF;
        END LOOP;
    END IF;
    IF NOT v_any_unbounded THEN
        v_eff_scope := '{}'::jsonb;
        FOR v_grant_obj IN SELECT * FROM jsonb_array_elements(v_grants) LOOP
            v_eff_scope := v_eff_scope || coalesce(v_grant_obj -> 'scope', '{}'::jsonb);
        END LOOP;
    END IF;

    -- Pull denormalized fields
    SELECT * INTO v_norm FROM claimius._denormalize_object(p_object_type, p_object_id);

    v_search_vec :=
            setweight(to_tsvector('simple', coalesce(v_norm.sa_name, '')), 'A') ||
            setweight(to_tsvector('simple', coalesce(v_norm.sa_description, '')), 'B');

    INSERT INTO claimius.user_object (
        id, user_id, object_id, object_type,
        sa_access, scope, grants, direct_grant,
        sa_owner_id, sa_location_id, sa_root_id,
        sa_name, sa_description, sa_link, search_vector
    ) VALUES (
                 v_id, p_user_id, p_object_id, p_object_type,
                 v_eff_access, v_eff_scope, v_grants, v_direct,
                 v_norm.sa_owner_id, v_norm.sa_location_id, v_norm.sa_root_id,
                 v_norm.sa_name, v_norm.sa_description, v_norm.sa_link, v_search_vec
             )
    ON CONFLICT (id) DO UPDATE SET
                                   sa_access = EXCLUDED.sa_access,
                                   scope = EXCLUDED.scope,
                                   grants = EXCLUDED.grants,
                                   direct_grant = EXCLUDED.direct_grant,
                                   sa_owner_id = EXCLUDED.sa_owner_id,
                                   sa_location_id = EXCLUDED.sa_location_id,
                                   sa_root_id = EXCLUDED.sa_root_id,
                                   sa_name = EXCLUDED.sa_name,
                                   sa_description = EXCLUDED.sa_description,
                                   sa_link = EXCLUDED.sa_link,
                                   search_vector = EXCLUDED.search_vector,
                                   sa_updated_at = now()
    WHERE claimius.user_object.sa_access        IS DISTINCT FROM EXCLUDED.sa_access
       OR claimius.user_object.scope            IS DISTINCT FROM EXCLUDED.scope
       OR claimius.user_object.grants           IS DISTINCT FROM EXCLUDED.grants
       OR claimius.user_object.direct_grant     IS DISTINCT FROM EXCLUDED.direct_grant
       OR claimius.user_object.sa_owner_id      IS DISTINCT FROM EXCLUDED.sa_owner_id
       OR claimius.user_object.sa_location_id   IS DISTINCT FROM EXCLUDED.sa_location_id
       OR claimius.user_object.sa_root_id       IS DISTINCT FROM EXCLUDED.sa_root_id
       OR claimius.user_object.sa_name          IS DISTINCT FROM EXCLUDED.sa_name
       OR claimius.user_object.sa_description   IS DISTINCT FROM EXCLUDED.sa_description
       OR claimius.user_object.sa_link          IS DISTINCT FROM EXCLUDED.sa_link
       OR claimius.user_object.search_vector    IS DISTINCT FROM EXCLUDED.search_vector;

    -- Maintain object_users
    INSERT INTO claimius.object_users (id, object_id, object_type, user_id)
    VALUES (
               claimius.composite_id(p_object_type, p_object_id::TEXT, p_user_id::TEXT),
               p_object_id, p_object_type, p_user_id
           )
    ON CONFLICT (id) DO NOTHING;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.recompute_user_object(p_user_id uuid, p_object_type text, p_object_id uuid) IS 'Recomputes user_object and object_users for one (user, object) tuple. Purges the row when the object is soft deleted or absent. user_users is rebuilt per viewer by the calling orchestrator.';

-- _refresh_user_users
-- Recomputes user_users entries for one (viewer) pair from
-- object_users.
CREATE OR REPLACE FUNCTION claimius._refresh_user_users(p_viewer_id UUID)
    RETURNS VOID AS $$
BEGIN
    -- Remove existing rows for this viewer
    DELETE FROM claimius.user_users
    WHERE TRUE AND viewer_id = p_viewer_id;

    -- Insert one row per other user the viewer shares any object with
    INSERT INTO claimius.user_users (id, viewer_id, target_user_id, sharing_object_count, first_shared_at, last_shared_at)
    SELECT
        claimius.composite_id(p_viewer_id::TEXT, target.user_id::TEXT),
        p_viewer_id,
        target.user_id,
        count(*),
        min(target.sa_created_at),
        max(target.sa_updated_at)
    FROM claimius.object_users viewer
             JOIN claimius.object_users target
                  ON TRUE
                      AND target.object_id = viewer.object_id
                      AND target.object_type = viewer.object_type
                      AND target.user_id <> viewer.user_id
    WHERE TRUE AND viewer.user_id = p_viewer_id
    GROUP BY target.user_id;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._refresh_user_users(p_viewer_id uuid) IS 'Rebuilds user_users rows for one (app, viewer).';

-- _affected_users_for_object
-- Returns the list of users whose access to the given object could change.
-- Includes any user holding a relevant claim plus any user already in
-- object_users for that object.
CREATE OR REPLACE FUNCTION claimius._affected_users_for_object(
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS TABLE(user_id UUID) AS $$
DECLARE
    v_parts         TEXT[];
    v_has_creator   BOOLEAN;
    v_creator_uc_id UUID;
    v_creator_user  UUID;
BEGIN
    -- The creator of a row holds it through _direct_grant_for_object without
    -- any claim binding, so they are resolved here from sa_created_by. Without
    -- this branch a creator who holds no claim on the object or an ancestor is
    -- never enumerated, no user_object row is ever written for them, and their
    -- direct grant stays invisible to every read path.
    IF p_object_type <> 'claimius.samna_user' THEN
        SELECT ti.has_sa_created_by INTO v_has_creator
        FROM claimius.table_info ti
        WHERE ti.object_type = p_object_type;

        IF v_has_creator IS TRUE THEN
            v_parts := claimius.split_object_type(p_object_type);
            BEGIN
                EXECUTE format('SELECT sa_created_by FROM %I.%I WHERE id = $1', v_parts[1], v_parts[2])
                    INTO v_creator_uc_id USING p_object_id;
            EXCEPTION WHEN OTHERS THEN
                v_creator_uc_id := NULL;
            END;

            IF v_creator_uc_id IS NOT NULL THEN
                SELECT uc.user_id INTO v_creator_user
                FROM claimius.user_claim uc
                WHERE uc.id = v_creator_uc_id
                  AND uc.sa_deleted_at IS NULL;
            END IF;
        END IF;
    END IF;

    RETURN QUERY
        WITH all_users AS (
            -- the creator, resolved through sa_created_by, which is a
            -- user_claim.id and nothing else.
            SELECT v_creator_user AS user_id
            WHERE v_creator_user IS NOT NULL

            UNION

            -- existing object_users
            SELECT ou.user_id FROM claimius.object_users ou
            WHERE TRUE AND ou.object_id = p_object_id AND ou.object_type = p_object_type

            UNION

            -- users holding any user_claim in this app whose claim is bound
            -- directly to this object
            SELECT uc.user_id
            FROM claimius.user_claim uc
                     JOIN claimius.claim_object co
                          ON co.claim_id = uc.claim_id
                         AND TRUE
                         AND co.sa_deleted_at IS NULL
                         AND co.object_type = p_object_type
                         AND co.object_id = p_object_id
            WHERE TRUE
              AND uc.sa_deleted_at IS NULL

            UNION

            -- users holding any user_claim whose claim is bound to an ancestor
            -- of this object via the closure index
            SELECT uc.user_id
            FROM claimius.user_claim uc
                     JOIN claimius.claim_object co
                          ON co.claim_id = uc.claim_id
                         AND TRUE
                         AND co.sa_deleted_at IS NULL
                         AND co.inherits = TRUE
                     JOIN claimius.inheritance_info e
                          ON e.ancestor_type   = co.object_type
                         AND e.ancestor_id     = co.object_id
                         AND e.descendant_type = p_object_type
                         AND e.descendant_id   = p_object_id
                         AND e.depth > 0
            WHERE TRUE
              AND uc.sa_deleted_at IS NULL
        )
        SELECT DISTINCT au.user_id FROM all_users au;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._affected_users_for_object(p_object_type text, p_object_id uuid) IS 'Users whose access to the object could change.';

-- ============================================================================
-- Ancestor walks
-- ============================================================================
-- Three helpers walk an object's structural ancestors. Each returns rows
-- shaped (object_type TEXT, object_id UUID, hop INTEGER) with hop = 0 for
-- the object itself, hop = 1 for its direct parent in that tree, etc.
-- get_ancestors combines all three and tags each row with the tree_type.

-- get_owner_ancestors
-- Reads the ownership tree closure for ancestors of (p_object_type, p_object_id).
-- Returns the object itself at hop 0 (closure self-edge) plus every ancestor
-- with depth as hop.
CREATE OR REPLACE FUNCTION claimius.get_owner_ancestors(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS TABLE(object_type TEXT, object_id UUID, hop INTEGER) AS $$
BEGIN
    RETURN QUERY
        SELECT e.ancestor_type, e.ancestor_id, e.depth
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'ownership'
          AND e.descendant_type = p_object_type
          AND e.descendant_id   = p_object_id
        ORDER BY e.depth;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_owner_ancestors(p_object_type text, p_object_id uuid) IS 'Ownership tree ancestors of an object, hop 0 = self.';

-- get_location_ancestors
-- Reads the location tree closure for location ancestors of an object, plus
-- the owning org chain of each location encountered (so a claim on a parent
-- org of a location grants access via the location too).
CREATE OR REPLACE FUNCTION claimius.get_location_ancestors(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS TABLE(object_type TEXT, object_id UUID, hop INTEGER) AS $$
BEGIN
    -- Location chain (self-edge included if p_object IS a location).
    RETURN QUERY
        SELECT e.ancestor_type, e.ancestor_id, e.depth
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'location'
          AND e.descendant_type = p_object_type
          AND e.descendant_id   = p_object_id
          AND e.ancestor_type   = 'claimius.location';

    -- For non-locations, emit self at hop 0 (location closure has no row for
    -- the object itself when the object is not a location).
    IF p_object_type <> 'claimius.location' THEN
        RETURN QUERY SELECT p_object_type, p_object_id, 0;
    END IF;

    -- For each location in the chain, expand to its owning org chain. Hop
    -- offsets: location's own depth, plus 1 for the owning org, plus the
    -- ownership-chain depth of that org.
    RETURN QUERY
        WITH loc_chain AS (
            SELECT e.ancestor_id AS loc_id, e.depth AS loc_depth
            FROM claimius.inheritance_info e
            WHERE e.tree_type = 'location'
              AND e.descendant_type = p_object_type
              AND e.descendant_id   = p_object_id
              AND e.ancestor_type   = 'claimius.location'
        )
        SELECT 'claimius.organization'::TEXT, eo.ancestor_id, lc.loc_depth + 1 + eo.depth
        FROM loc_chain lc
                 JOIN claimius.location l
                      ON l.id = lc.loc_id AND l.sa_deleted_at IS NULL
                 JOIN claimius.inheritance_info eo
                      ON eo.tree_type = 'ownership'
                     AND eo.descendant_type = 'claimius.organization'
                     AND eo.descendant_id   = l.sa_owner_id
        WHERE l.sa_owner_id IS NOT NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_location_ancestors(p_object_type text, p_object_id uuid) IS 'Location chain ancestors of an object plus each location''s owning org chain.';

-- get_parenthood_ancestors
-- Reads the parenthood tree closure for ancestors of an object whose table
-- is self referencing via sa_parent_id.
CREATE OR REPLACE FUNCTION claimius.get_parenthood_ancestors(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS TABLE(object_type TEXT, object_id UUID, hop INTEGER) AS $$
BEGIN
    RETURN QUERY
        SELECT e.ancestor_type, e.ancestor_id, e.depth
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'parenthood'
          AND e.descendant_type = p_object_type
          AND e.descendant_id   = p_object_id
        ORDER BY e.depth;

    -- For tables that participate in no parenthood tree, the closure has no
    -- self-edge for this row. Emit self at hop 0 so callers always see it.
    IF NOT EXISTS (
        SELECT 1 FROM claimius.inheritance_info e
        WHERE e.tree_type = 'parenthood'
          AND e.descendant_type = p_object_type
          AND e.descendant_id   = p_object_id
          AND e.depth = 0
    ) THEN
        RETURN QUERY SELECT p_object_type, p_object_id, 0;
    END IF;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_parenthood_ancestors(p_object_type text, p_object_id uuid) IS 'Parenthood tree ancestors via sa_parent_id, hop 0 = self.';

-- get_ancestors
-- Combined ancestor walk. Yields rows from the owner, location, and
-- parenthood trees, tagged with tree_type. Hops are independent per tree;
-- the same object may appear in multiple trees with different hops.
CREATE OR REPLACE FUNCTION claimius.get_ancestors(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS TABLE(object_type TEXT, object_id UUID, hop INTEGER, tree_type TEXT) AS $$
BEGIN
    RETURN QUERY
        SELECT a.object_type, a.object_id, a.hop, 'ownership'::TEXT
        FROM claimius.get_owner_ancestors(p_object_type, p_object_id) a;

    RETURN QUERY
        SELECT a.object_type, a.object_id, a.hop, 'location'::TEXT
        FROM claimius.get_location_ancestors(p_object_type, p_object_id) a;

    RETURN QUERY
        SELECT a.object_type, a.object_id, a.hop, 'parenthood'::TEXT
        FROM claimius.get_parenthood_ancestors(p_object_type, p_object_id) a;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_ancestors(p_object_type text, p_object_id uuid) IS 'Combined ancestor walk across owner, location, and parenthood trees.';
CREATE OR REPLACE FUNCTION claimius.apply_row_delta(p_object_type text, p_object_id uuid, p_old jsonb, p_new jsonb)
 RETURNS void
 LANGUAGE plpgsql
AS $function$
DECLARE
    v_old_active BOOLEAN := (p_old IS NOT NULL AND (p_old ->> 'sa_deleted_at') IS NULL);
    v_new_active BOOLEAN := (p_new IS NOT NULL AND (p_new ->> 'sa_deleted_at') IS NULL);
    v_owner_id   UUID;
    v_loc_id     UUID;
    v_parent_id  UUID;
    v_root_id    UUID;
    v_info       claimius.table_info%ROWTYPE;
BEGIN
    SELECT * INTO v_info FROM claimius.table_info WHERE object_type = p_object_type;

    IF v_old_active THEN
        DELETE FROM claimius.inheritance_info
        WHERE descendant_type = p_object_type AND descendant_id = p_object_id;
    END IF;

    IF NOT v_new_active THEN
        RETURN;
    END IF;

    v_owner_id  := (p_new ->> 'sa_owner_id')::UUID;
    v_loc_id    := (p_new ->> 'sa_location_id')::UUID;
    v_parent_id := (p_new ->> 'sa_parent_id')::UUID;
    v_root_id   := (p_new ->> 'sa_root_id')::UUID;

    IF p_object_type = 'claimius.organization' THEN
        INSERT INTO claimius.inheritance_info (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth)
        VALUES ('ownership', v_root_id, p_object_type, p_object_id, p_object_type, p_object_id, 0)
        ON CONFLICT DO NOTHING;

        IF v_owner_id IS NOT NULL AND v_owner_id <> p_object_id THEN
            INSERT INTO claimius.inheritance_info (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth)
            SELECT e.tree_type, e.root_id, e.ancestor_type, e.ancestor_id, p_object_type, p_object_id, e.depth + 1
            FROM claimius.inheritance_info e
            WHERE e.tree_type = 'ownership'
              AND e.descendant_type = 'claimius.organization'
              AND e.descendant_id = v_owner_id
            ON CONFLICT DO NOTHING;
        END IF;
        RETURN;
    END IF;

    IF p_object_type = 'claimius.location' THEN
        INSERT INTO claimius.inheritance_info (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth)
        VALUES ('location', v_root_id, p_object_type, p_object_id, p_object_type, p_object_id, 0)
        ON CONFLICT DO NOTHING;

        IF v_parent_id IS NOT NULL AND v_parent_id <> p_object_id THEN
            INSERT INTO claimius.inheritance_info (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth)
            SELECT e.tree_type, e.root_id, e.ancestor_type, e.ancestor_id, p_object_type, p_object_id, e.depth + 1
            FROM claimius.inheritance_info e
            WHERE e.tree_type = 'location'
              AND e.descendant_type = 'claimius.location'
              AND e.descendant_id = v_parent_id
            ON CONFLICT DO NOTHING;
        END IF;

        IF v_owner_id IS NOT NULL THEN
            INSERT INTO claimius.inheritance_info (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth)
            SELECT 'ownership'::claimius.tree_type, e.root_id, p_object_type, p_object_id, p_object_type, p_object_id, 0
            FROM claimius.inheritance_info e
            WHERE e.tree_type = 'ownership'
              AND e.descendant_type = 'claimius.organization'
              AND e.descendant_id = v_owner_id
              AND e.depth = 0
            ON CONFLICT DO NOTHING;

            INSERT INTO claimius.inheritance_info (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth)
            SELECT e.tree_type, e.root_id, e.ancestor_type, e.ancestor_id, p_object_type, p_object_id, e.depth + 1
            FROM claimius.inheritance_info e
            WHERE e.tree_type = 'ownership'
              AND e.descendant_type = 'claimius.organization'
              AND e.descendant_id = v_owner_id
            ON CONFLICT DO NOTHING;
        END IF;
        RETURN;
    END IF;

    IF v_info.has_sa_owner_id AND v_owner_id IS NOT NULL THEN
        INSERT INTO claimius.inheritance_info (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth)
        SELECT 'ownership'::claimius.tree_type, e.root_id, p_object_type, p_object_id, p_object_type, p_object_id, 0
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'ownership'
          AND e.descendant_type = 'claimius.organization'
          AND e.descendant_id = v_owner_id
          AND e.depth = 0
        ON CONFLICT DO NOTHING;

        INSERT INTO claimius.inheritance_info (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth)
        SELECT 'ownership'::claimius.tree_type, e.root_id, e.ancestor_type, e.ancestor_id, p_object_type, p_object_id, e.depth + 1
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'ownership'
          AND e.descendant_type = 'claimius.organization'
          AND e.descendant_id = v_owner_id
        ON CONFLICT DO NOTHING;
    END IF;

    IF v_info.has_sa_location_id AND v_loc_id IS NOT NULL THEN
        INSERT INTO claimius.inheritance_info (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth)
        SELECT 'location'::claimius.tree_type, e.root_id, p_object_type, p_object_id, p_object_type, p_object_id, 0
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'location'
          AND e.descendant_type = 'claimius.location'
          AND e.descendant_id = v_loc_id
          AND e.depth = 0
        ON CONFLICT DO NOTHING;

        INSERT INTO claimius.inheritance_info (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth)
        SELECT 'location'::claimius.tree_type, e.root_id, e.ancestor_type, e.ancestor_id, p_object_type, p_object_id, e.depth + 1
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'location'
          AND e.descendant_type = 'claimius.location'
          AND e.descendant_id = v_loc_id
        ON CONFLICT DO NOTHING;
    END IF;

    IF v_info.has_sa_parent_id AND v_info.has_sa_root_id THEN
        INSERT INTO claimius.inheritance_info (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth)
        VALUES ('parenthood', v_root_id, p_object_type, p_object_id, p_object_type, p_object_id, 0)
        ON CONFLICT DO NOTHING;

        IF v_parent_id IS NOT NULL AND v_parent_id <> p_object_id THEN
            INSERT INTO claimius.inheritance_info (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth)
            SELECT e.tree_type, e.root_id, e.ancestor_type, e.ancestor_id, p_object_type, p_object_id, e.depth + 1
            FROM claimius.inheritance_info e
            WHERE e.tree_type = 'parenthood'
              AND e.descendant_type = p_object_type
              AND e.descendant_id = v_parent_id
            ON CONFLICT DO NOTHING;
        END IF;
    END IF;
END;
$function$
;

CREATE OR REPLACE FUNCTION claimius.rebuild_tree(p_root_id uuid)
 RETURNS void
 LANGUAGE plpgsql
AS $function$
DECLARE
    v_row RECORD;
    v_info claimius.table_info%ROWTYPE;
    v_conds TEXT;
BEGIN
    DELETE FROM claimius.inheritance_info WHERE root_id = p_root_id;

    FOR v_row IN
        WITH RECURSIVE org_chain AS (
            SELECT o.id, o.sa_owner_id, 0 AS depth, to_jsonb(o) AS row_data
            FROM claimius.organization o
            WHERE o.id = p_root_id AND o.sa_deleted_at IS NULL
            UNION ALL
            SELECT o.id, o.sa_owner_id, oc.depth + 1, to_jsonb(o)
            FROM claimius.organization o
                     JOIN org_chain oc ON o.sa_owner_id = oc.id
            WHERE o.id <> o.sa_owner_id AND o.sa_deleted_at IS NULL
        )
        SELECT id, row_data FROM org_chain ORDER BY depth
    LOOP
        PERFORM claimius.apply_row_delta('claimius.organization', v_row.id, NULL::JSONB, v_row.row_data);
    END LOOP;

    FOR v_row IN
        WITH RECURSIVE loc_chain AS (
            SELECT l.id, l.sa_parent_id, l.sa_owner_id, 0 AS depth, to_jsonb(l) AS row_data
            FROM claimius.location l
            WHERE l.sa_root_id = p_root_id AND l.sa_parent_id = l.id AND l.sa_deleted_at IS NULL
            UNION ALL
            SELECT l.id, l.sa_parent_id, l.sa_owner_id, lc.depth + 1, to_jsonb(l)
            FROM claimius.location l
                     JOIN loc_chain lc ON l.sa_parent_id = lc.id
            WHERE l.id <> l.sa_parent_id AND l.sa_deleted_at IS NULL AND l.sa_root_id = p_root_id
        )
        SELECT id, row_data FROM loc_chain ORDER BY depth
    LOOP
        PERFORM claimius.apply_row_delta('claimius.location', v_row.id, NULL::JSONB, v_row.row_data);
    END LOOP;

    FOR v_info IN
        SELECT * FROM claimius.table_info
        WHERE object_type NOT IN ('claimius.organization', 'claimius.location')
          AND has_sa_deleted_at
    LOOP
        IF v_info.has_sa_parent_id AND v_info.has_sa_root_id THEN
            FOR v_row IN EXECUTE format(
                $f$
                WITH RECURSIVE chain AS (
                    SELECT id, sa_parent_id, 0 AS depth
                    FROM %1$I.%2$I
                    WHERE sa_root_id = $1 AND sa_parent_id = id AND sa_deleted_at IS NULL
                    UNION ALL
                    SELECT r.id, r.sa_parent_id, c.depth + 1
                    FROM %1$I.%2$I r
                             JOIN chain c ON r.sa_parent_id = c.id
                    WHERE r.id <> r.sa_parent_id AND r.sa_deleted_at IS NULL AND r.sa_root_id = $1
                )
                SELECT id FROM chain ORDER BY depth
                $f$,
                split_part(v_info.object_type, '.', 1),
                split_part(v_info.object_type, '.', 2)
            ) USING p_root_id
            LOOP
                EXECUTE format(
                    'SELECT claimius.apply_row_delta($1, $2, NULL::JSONB, to_jsonb(r.*)) FROM %1$I.%2$I r WHERE r.id = $2',
                    split_part(v_info.object_type, '.', 1),
                    split_part(v_info.object_type, '.', 2)
                ) USING v_info.object_type, v_row.id;
            END LOOP;
        ELSE
            v_conds := NULL;
            IF v_info.has_sa_owner_id THEN
                v_conds := 'sa_owner_id IN (SELECT descendant_id FROM claimius.inheritance_info WHERE tree_type = ''ownership'' AND root_id = $1 AND descendant_type = ''claimius.organization'')';
            END IF;
            IF v_info.has_sa_location_id THEN
                v_conds := CASE WHEN v_conds IS NULL THEN '' ELSE v_conds || ' OR ' END
                    || 'sa_location_id IN (SELECT descendant_id FROM claimius.inheritance_info WHERE tree_type = ''location'' AND root_id = $1 AND descendant_type = ''claimius.location'')';
            END IF;
            IF v_conds IS NULL THEN
                CONTINUE;
            END IF;
            FOR v_row IN EXECUTE format(
                'SELECT id FROM %1$I.%2$I WHERE sa_deleted_at IS NULL AND (%3$s)',
                split_part(v_info.object_type, '.', 1),
                split_part(v_info.object_type, '.', 2),
                v_conds
            ) USING p_root_id
            LOOP
                EXECUTE format(
                    'SELECT claimius.apply_row_delta($1, $2, NULL::JSONB, to_jsonb(r.*)) FROM %1$I.%2$I r WHERE r.id = $2',
                    split_part(v_info.object_type, '.', 1),
                    split_part(v_info.object_type, '.', 2)
                ) USING v_info.object_type, v_row.id;
            END LOOP;
        END IF;
    END LOOP;
END;
$function$
;

CREATE OR REPLACE FUNCTION claimius.rebuild_user_object()
 RETURNS void
 LANGUAGE plpgsql
AS $function$
DECLARE
    v_pair RECORD;
BEGIN
    DELETE FROM claimius.user_object;

    FOR v_pair IN
        SELECT DISTINCT uc.user_id, co.object_type, co.object_id
        FROM claimius.user_claim uc
                 JOIN claimius.claim_object co ON co.claim_id = uc.claim_id
        WHERE uc.sa_deleted_at IS NULL
          AND co.sa_deleted_at IS NULL
    LOOP
        PERFORM claimius.recompute_user_object(v_pair.user_id, v_pair.object_type, v_pair.object_id);
    END LOOP;

    PERFORM claimius._refresh_user_users(s.user_id)
    FROM (SELECT DISTINCT user_id FROM claimius.object_users) s;
END;
$function$
;

-- ----------------------------------------------------------------------------
-- _actor_user_for_row
-- The user acting on a row, resolved from sa_created_by, which is a
-- user_claim.id. Claimius is always entered in the context of a user or a
-- client, so this is the one user whose materialization a write settles
-- synchronously. Returns NULL when the table carries no sa_created_by or the
-- assignment no longer resolves.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius._actor_user_for_row(
    p_object_type   TEXT,
    p_row           ANYELEMENT
) RETURNS UUID AS $$
DECLARE
    v_created_by    UUID;
    v_user_id       UUID;
BEGIN
    IF p_object_type = 'claimius.samna_user' THEN
        RETURN NULL;
    END IF;

    v_created_by := (to_jsonb(p_row) ->> 'sa_created_by')::UUID;
    IF v_created_by IS NULL THEN
        RETURN NULL;
    END IF;

    SELECT uc.user_id INTO v_user_id
    FROM claimius.user_claim uc
    WHERE uc.id = v_created_by
      AND uc.sa_deleted_at IS NULL;

    RETURN v_user_id;
EXCEPTION WHEN OTHERS THEN
    RETURN NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._actor_user_for_row(TEXT, ANYELEMENT) IS 'User behind sa_created_by on a row. The one user a write materializes synchronously.';

-- ----------------------------------------------------------------------------
-- _raise_object_diff
-- Marks the area around one object as needing recalculation. One row, no user
-- enumeration. Repeated changes to the same object collapse onto the same id.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius._raise_object_diff(
    p_object_type   TEXT,
    p_object_id     UUID,
    p_cascade       BOOLEAN DEFAULT FALSE
) RETURNS VOID AS $$
BEGIN
    INSERT INTO claimius.access_diff (id, kind, object_type, object_id, cascade)
    VALUES (claimius.composite_id('object', p_object_type, p_object_id::TEXT),
            'object', p_object_type, p_object_id, p_cascade)
    ON CONFLICT (id) DO UPDATE
        SET sa_updated_at = now(),
            cascade = claimius.access_diff.cascade OR EXCLUDED.cascade;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._raise_object_diff(TEXT, UUID, BOOLEAN) IS 'Marks an object area outstanding in access_diff.';

-- ----------------------------------------------------------------------------
-- _raise_claim_diff
-- Marks the area around one claim as needing recalculation.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius._raise_claim_diff(
    p_claim_id      UUID
) RETURNS VOID AS $$
BEGIN
    IF p_claim_id IS NULL THEN
        RETURN;
    END IF;

    INSERT INTO claimius.access_diff (id, kind, claim_id)
    VALUES (claimius.composite_id('claim', 'claimius.claim', p_claim_id::TEXT),
            'claim', p_claim_id)
    ON CONFLICT (id) DO UPDATE SET sa_updated_at = now();
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._raise_claim_diff(UUID) IS 'Marks a claim area outstanding in access_diff.';

-- ----------------------------------------------------------------------------
-- _settle_object_diff
-- Recalculates one object area for every user it touches. This is the work the
-- write no longer does, paid once by the first read that targets the area.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius._settle_object_diff(
    p_object_type   TEXT,
    p_object_id     UUID,
    p_cascade       BOOLEAN DEFAULT FALSE
) RETURNS SETOF UUID AS $$
DECLARE
    v_user RECORD;
BEGIN
    FOR v_user IN
        SELECT user_id FROM claimius._affected_users_for_object(p_object_type, p_object_id)
        LOOP
            PERFORM claimius.recompute_user_object(v_user.user_id, p_object_type, p_object_id);
            RETURN NEXT v_user.user_id;
        END LOOP;

    -- A hierarchical row carries its subtree. The write records one diff and
    -- the closure is walked here, once, by the reader that claims the area.
    IF p_cascade THEN
        FOR v_user IN
            WITH descendants AS (
                SELECT DISTINCT e.descendant_type AS object_type, e.descendant_id AS object_id
                FROM claimius.inheritance_info e
                WHERE e.ancestor_type = p_object_type
                  AND e.ancestor_id = p_object_id
                  AND e.depth > 0
            )
            SELECT DISTINCT u.user_id, d.object_type, d.object_id
            FROM descendants d
                CROSS JOIN LATERAL claimius._affected_users_for_object(d.object_type, d.object_id) u
            LOOP
                PERFORM claimius.recompute_user_object(
                        v_user.user_id, v_user.object_type, v_user.object_id);
                RETURN NEXT v_user.user_id;
            END LOOP;
    END IF;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._settle_object_diff(TEXT, UUID, BOOLEAN) IS 'Recomputes one object for every user reaching it. Returns the users touched.';

-- ----------------------------------------------------------------------------
-- _settle_claim_diff
-- Recalculates the area a claim covers: its bindings, and everything under
-- them when both the claim and the binding inherit, for every user holding the
-- claim directly or through a composition link.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius._settle_claim_diff(
    p_claim_id      UUID
) RETURNS SETOF UUID AS $$
DECLARE
    v_inherits  BOOLEAN;
    v_pair      RECORD;
BEGIN
    SELECT coalesce(c.inherits, FALSE) INTO v_inherits
    FROM claimius.claim c WHERE c.id = p_claim_id;

    FOR v_pair IN
        WITH holders AS (
            SELECT DISTINCT uc.user_id
            FROM claimius.user_claim uc
            WHERE uc.sa_deleted_at IS NULL
              AND (
                  uc.claim_id = p_claim_id
                  OR EXISTS (
                      SELECT 1 FROM claimius.claim_object link
                      WHERE link.claim_id = uc.claim_id
                        AND link.object_type = 'claimius.claim'
                        AND link.object_id = p_claim_id
                        AND link.sa_deleted_at IS NULL
                  )
              )
        ), targets AS (
            SELECT co.object_type, co.object_id
            FROM claimius.claim_object co
            WHERE co.claim_id = p_claim_id
              AND co.sa_deleted_at IS NULL
              AND co.object_type <> 'claimius.claim'

            UNION

            SELECT e.descendant_type, e.descendant_id
            FROM claimius.claim_object co
                JOIN claimius.inheritance_info e
                    ON e.ancestor_type = co.object_type
                   AND e.ancestor_id = co.object_id
                   AND e.depth > 0
            WHERE co.claim_id = p_claim_id
              AND co.sa_deleted_at IS NULL
              AND co.inherits = TRUE
              AND v_inherits
        )
        SELECT h.user_id, t.object_type, t.object_id
        FROM holders h CROSS JOIN targets t
        LOOP
            PERFORM claimius.recompute_user_object(
                    v_pair.user_id, v_pair.object_type, v_pair.object_id);
            RETURN NEXT v_pair.user_id;
        END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._settle_claim_diff(UUID) IS 'Recomputes the objects a claim reaches for every user holding it. Returns the users touched.';

-- ----------------------------------------------------------------------------
-- _settle_claim_diff_for_user
-- The single user slice of _settle_claim_diff. A write settles the acting user
-- and the assigned user through this, so both read their own change back while
-- every other holder waits for the diff to be claimed by a read.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius._settle_claim_diff_for_user(
    p_user_id       UUID,
    p_claim_id      UUID
) RETURNS VOID AS $$
DECLARE
    v_inherits  BOOLEAN;
    v_target    RECORD;
BEGIN
    IF p_user_id IS NULL OR p_claim_id IS NULL THEN
        RETURN;
    END IF;

    SELECT coalesce(c.inherits, FALSE) INTO v_inherits
    FROM claimius.claim c WHERE c.id = p_claim_id;

    FOR v_target IN
        SELECT co.object_type, co.object_id
        FROM claimius.claim_object co
        WHERE co.claim_id = p_claim_id
          AND co.sa_deleted_at IS NULL
          AND co.object_type <> 'claimius.claim'

        UNION

        SELECT e.descendant_type, e.descendant_id
        FROM claimius.claim_object co
            JOIN claimius.inheritance_info e
                ON e.ancestor_type = co.object_type
               AND e.ancestor_id = co.object_id
               AND e.depth > 0
        WHERE co.claim_id = p_claim_id
          AND co.sa_deleted_at IS NULL
          AND co.inherits = TRUE
          AND v_inherits
        LOOP
            PERFORM claimius.recompute_user_object(
                    p_user_id, v_target.object_type, v_target.object_id);
        END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._settle_claim_diff_for_user(UUID, UUID) IS 'Recomputes the objects a claim reaches, for one user only.';
