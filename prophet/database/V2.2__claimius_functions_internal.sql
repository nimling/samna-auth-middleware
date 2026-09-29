-- ============================================================================
-- Claimius V2.2 Internal Functions
-- ----------------------------------------------------------------------------
-- Functions used internally by triggers, init, and other claimius internals.
-- Not exposed to consumers. These functions assume valid inputs and may
-- raise on misuse.
-- ============================================================================

-- ----------------------------------------------------------------------------
-- Tree builders. Used by init, recompute_state, migrate_root, and the restore
-- of a soft deleted node. Each rewrites the edges in inheritance_info for the
-- given tree from live data, the whole root when p_node_id is NULL or the
-- root itself, otherwise the subtree of p_node_id hung under the stored
-- ancestors of its parent. Self-edges (depth = 0) included so a node is its
-- own ancestor.
-- ----------------------------------------------------------------------------

-- build_ownership_tree
-- Rewrites the ownership tree closure rooted at p_root_id from live data.
-- Includes org-org edges plus edges from every registered table whose rows
-- point at any org in the tree via sa_owner_id.
DROP FUNCTION IF EXISTS claimius.build_ownership_tree(UUID);
CREATE OR REPLACE FUNCTION claimius.build_ownership_tree(p_root_id UUID, p_node_id UUID DEFAULT NULL)
    RETURNS VOID AS $$
DECLARE
    v_start  UUID := coalesce(p_node_id, p_root_id);
    v_prefix UUID[] := '{}';
BEGIN
    -- Serialize concurrent rebuilds of the same root. Without this, two
    -- transactions both delete against their own snapshot and collide on
    -- the closure insert with a duplicate inheritance_info_pkey.
    PERFORM pg_advisory_xact_lock(hashtext(p_root_id::TEXT));

    IF v_start = p_root_id THEN
        DELETE FROM claimius.inheritance_info
        WHERE tree_type = 'ownership' AND root_id = p_root_id;
    ELSE
        PERFORM claimius._splice_subtree('ownership', 'claimius.organization', v_start, p_root_id, NULL);
        SELECT array_agg(e.ancestor_id ORDER BY e.depth DESC) INTO v_prefix
        FROM claimius.organization o
                 JOIN claimius.inheritance_info e
                      ON e.tree_type = 'ownership'
                          AND e.descendant_type = 'claimius.organization'
                          AND e.descendant_id = o.sa_owner_id
        WHERE o.id = v_start;
        IF v_prefix IS NULL THEN
            RETURN;
        END IF;
    END IF;

    -- Org-org edges. Walk down recursively, recording the path of ancestors
    -- to each node, then explode the path into one edge per ancestor.
    INSERT INTO claimius.inheritance_info (
        tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
    )
    WITH RECURSIVE org_paths AS (
        SELECT id AS node_id, v_prefix || id AS path
        FROM claimius.organization
        WHERE id = v_start AND sa_deleted_at IS NULL
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
             CROSS JOIN LATERAL claimius._select_owned_rows(t.object_type, v_start) r
    WHERE t.has_sa_owner_id
      AND t.has_sa_deleted_at
      AND t.object_type <> 'claimius.organization'
    UNION ALL
    SELECT 'ownership'::claimius.tree_type, p_root_id, e.ancestor_type, e.ancestor_id, t.object_type, r.id, e.depth + 1
    FROM claimius.table_info t
             CROSS JOIN LATERAL claimius._select_owned_rows(t.object_type, v_start) r
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

COMMENT ON FUNCTION claimius.build_ownership_tree(UUID, UUID) IS 'Rewrites ownership tree closure rooted at p_root_id, or the subtree of p_node_id, from live data.';

-- build_location_tree
-- Rewrites the location tree closure rooted at p_root_id from live data.
-- Includes location-location edges plus edges from every registered table
-- whose rows point at any location in the tree via sa_location_id.
DROP FUNCTION IF EXISTS claimius.build_location_tree(UUID);
CREATE OR REPLACE FUNCTION claimius.build_location_tree(p_root_id UUID, p_node_id UUID DEFAULT NULL)
    RETURNS VOID AS $$
DECLARE
    v_start  UUID := coalesce(p_node_id, p_root_id);
    v_prefix UUID[] := '{}';
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext(p_root_id::TEXT));

    IF v_start = p_root_id THEN
        DELETE FROM claimius.inheritance_info
        WHERE tree_type = 'location' AND root_id = p_root_id;
    ELSE
        PERFORM claimius._splice_subtree('location', 'claimius.location', v_start, p_root_id, NULL);
        SELECT array_agg(e.ancestor_id ORDER BY e.depth DESC) INTO v_prefix
        FROM claimius.location l
                 JOIN claimius.inheritance_info e
                      ON e.tree_type = 'location'
                          AND e.descendant_type = 'claimius.location'
                          AND e.descendant_id = l.sa_parent_id
        WHERE l.id = v_start;
        IF v_prefix IS NULL THEN
            RETURN;
        END IF;
    END IF;

    INSERT INTO claimius.inheritance_info (
        tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
    )
    WITH RECURSIVE loc_paths AS (
        SELECT id AS node_id, v_prefix || id AS path
        FROM claimius.location
        WHERE id = v_start AND sa_deleted_at IS NULL
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
             CROSS JOIN LATERAL claimius._select_located_rows(t.object_type, v_start) r
    WHERE t.has_sa_location_id
      AND t.has_sa_deleted_at
    UNION ALL
    SELECT 'location'::claimius.tree_type, p_root_id, e.ancestor_type, e.ancestor_id, t.object_type, r.id, e.depth + 1
    FROM claimius.table_info t
             CROSS JOIN LATERAL claimius._select_located_rows(t.object_type, v_start) r
             JOIN claimius.inheritance_info e
                  ON e.tree_type = 'location'
                      AND e.root_id = p_root_id
                      AND e.descendant_type = 'claimius.location'
                      AND e.descendant_id = r.sa_location_id
    WHERE t.has_sa_location_id
      AND t.has_sa_deleted_at;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.build_location_tree(UUID, UUID) IS 'Rewrites location tree closure rooted at p_root_id, or the subtree of p_node_id, from live data.';

-- build_parenthood_tree
-- Rewrites the parenthood tree closure for one root of a self referencing
-- registered table. Walks the table's sa_parent_id chain only.
DROP FUNCTION IF EXISTS claimius.build_parenthood_tree(TEXT, UUID);
CREATE OR REPLACE FUNCTION claimius.build_parenthood_tree(p_object_type TEXT, p_root_id UUID, p_node_id UUID DEFAULT NULL)
    RETURNS VOID AS $$
DECLARE
    v_schema TEXT;
    v_table TEXT;
    v_parts TEXT[];
    v_sql TEXT;
    v_start  UUID := coalesce(p_node_id, p_root_id);
    v_parent UUID;
    v_prefix UUID[] := '{}';
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext(p_root_id::TEXT));

    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    IF v_start = p_root_id THEN
        DELETE FROM claimius.inheritance_info
        WHERE tree_type = 'parenthood' AND root_id = p_root_id AND ancestor_type = p_object_type;
    ELSE
        PERFORM claimius._splice_subtree('parenthood', p_object_type, v_start, p_root_id, NULL);
        EXECUTE format('SELECT sa_parent_id FROM %I.%I WHERE id = $1', v_schema, v_table)
            INTO v_parent USING v_start;
        SELECT array_agg(e.ancestor_id ORDER BY e.depth DESC) INTO v_prefix
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'parenthood'
          AND e.descendant_type = p_object_type
          AND e.descendant_id = v_parent;
        IF v_prefix IS NULL THEN
            RETURN;
        END IF;
    END IF;

    v_sql := format($f$
        INSERT INTO claimius.inheritance_info (
            tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
        )
        WITH RECURSIVE node_paths AS (
            SELECT id AS node_id, $2 || id AS path
            FROM %I.%I
            WHERE id = $3 AND sa_deleted_at IS NULL
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

    EXECUTE v_sql USING p_root_id, v_prefix, v_start;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.build_parenthood_tree(TEXT, UUID, UUID) IS 'Rewrites parenthood tree closure for one root, or the subtree of p_node_id, of a self referencing registered table.';

-- _splice_subtree
-- Hangs the stored subtree of one node under a new parent. The rows linking
-- the subtree to the node's former ancestors go, and the rows linking it to
-- the parent's ancestors come: n (D + 1) rows for a subtree of n nodes under
-- a parent at depth D. With no parent, or a parent outside the closure or
-- inside the subtree, every row of the subtree goes, as a rebuild would drop
-- a subtree its root no longer reaches.
CREATE OR REPLACE FUNCTION claimius._splice_subtree(
    p_tree_type claimius.tree_type,
    p_node_type TEXT,
    p_node_id   UUID,
    p_root_id   UUID,
    p_parent_id UUID
) RETURNS VOID AS $$
BEGIN
    IF p_parent_id IS NOT NULL
        AND EXISTS (
            SELECT 1 FROM claimius.inheritance_info p
            WHERE p.tree_type = p_tree_type
              AND p.descendant_type = p_node_type
              AND p.descendant_id = p_parent_id
              AND p.depth = 0
        )
        AND NOT EXISTS (
            SELECT 1 FROM claimius.inheritance_info s
            WHERE s.tree_type = p_tree_type
              AND s.ancestor_type = p_node_type
              AND s.ancestor_id = p_node_id
              AND s.descendant_type = p_node_type
              AND s.descendant_id = p_parent_id
        ) THEN
        DELETE FROM claimius.inheritance_info d
            USING claimius.inheritance_info s, claimius.inheritance_info a
        WHERE s.tree_type = p_tree_type
          AND s.ancestor_type = p_node_type
          AND s.ancestor_id = p_node_id
          AND a.tree_type = p_tree_type
          AND a.descendant_type = p_node_type
          AND a.descendant_id = p_node_id
          AND a.depth > 0
          AND d.tree_type = p_tree_type
          AND d.ancestor_type = a.ancestor_type
          AND d.ancestor_id = a.ancestor_id
          AND d.descendant_type = s.descendant_type
          AND d.descendant_id = s.descendant_id;

        INSERT INTO claimius.inheritance_info (
            tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
        )
        SELECT p_tree_type, p_root_id, a.ancestor_type, a.ancestor_id, s.descendant_type, s.descendant_id, a.depth + s.depth + 1
        FROM claimius.inheritance_info a
                 JOIN claimius.inheritance_info s
                      ON s.tree_type = p_tree_type
                          AND s.ancestor_type = p_node_type
                          AND s.ancestor_id = p_node_id
        WHERE a.tree_type = p_tree_type
          AND a.descendant_type = p_node_type
          AND a.descendant_id = p_parent_id;
        RETURN;
    END IF;

    DELETE FROM claimius.inheritance_info d
        USING claimius.inheritance_info s
    WHERE s.tree_type = p_tree_type
      AND s.ancestor_type = p_node_type
      AND s.ancestor_id = p_node_id
      AND d.tree_type = p_tree_type
      AND d.descendant_type = s.descendant_type
      AND d.descendant_id = s.descendant_id;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._splice_subtree(claimius.tree_type, TEXT, UUID, UUID, UUID) IS 'Moves the stored subtree of a node under a new parent, or drops it when the parent is absent.';

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

-- app_object_ids
-- Object ids of a given type linked to an app through app_object.
CREATE OR REPLACE FUNCTION claimius.app_object_ids(p_app_id UUID, p_object_type TEXT)
    RETURNS SETOF UUID AS $$
    SELECT ao.object_id
    FROM claimius.app_object ao
    WHERE ao.app_id = p_app_id
      AND ao.object_type = p_object_type
      AND ao.sa_deleted_at IS NULL;
$$ LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.app_object_ids(UUID, TEXT) IS 'Object ids of one type linked to an app through app_object.';

-- apps_for_object
-- App ids holding a live link to one object.
CREATE OR REPLACE FUNCTION claimius.apps_for_object(p_object_type TEXT, p_object_id UUID)
    RETURNS SETOF UUID AS $$
    SELECT ao.app_id
    FROM claimius.app_object ao
    WHERE ao.object_type = p_object_type
      AND ao.object_id = p_object_id
      AND ao.sa_deleted_at IS NULL;
$$ LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.apps_for_object(TEXT, UUID) IS 'App ids holding a live app_object link to one object.';

-- app_has_object
CREATE OR REPLACE FUNCTION claimius.app_has_object(p_app_id UUID, p_object_type TEXT, p_object_id UUID)
    RETURNS BOOLEAN AS $$
    SELECT EXISTS (
        SELECT 1
        FROM claimius.app_object ao
        WHERE ao.app_id = p_app_id
          AND ao.object_type = p_object_type
          AND ao.object_id = p_object_id
          AND ao.sa_deleted_at IS NULL
    );
$$ LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.app_has_object(UUID, TEXT, UUID) IS 'True when the app holds a live app_object link to the object.';

-- link_app_object
-- Creates a live app_object link and materializes access for the newly
-- linked (app, object) pair. Idempotent: an existing live link is returned
-- untouched.
CREATE OR REPLACE FUNCTION claimius.link_app_object(
    p_app_id      UUID,
    p_object_type TEXT,
    p_object_id   UUID,
    p_created_by  UUID DEFAULT NULL
) RETURNS UUID AS $$
DECLARE
    v_id      UUID;
    v_user    RECORD;
BEGIN
    SELECT id INTO v_id FROM claimius.app_object
    WHERE app_id = p_app_id
      AND object_type = p_object_type
      AND object_id = p_object_id
      AND sa_deleted_at IS NULL;

    IF v_id IS NOT NULL THEN
        RETURN v_id;
    END IF;

    INSERT INTO claimius.app_object (app_id, object_type, object_id, sa_created_by)
    VALUES (p_app_id, p_object_type, p_object_id, p_created_by)
    RETURNING id INTO v_id;

    IF NOT claimius.in_replay_mode() THEN
        FOR v_user IN
            SELECT user_id FROM claimius._affected_users_for_object(p_app_id, p_object_type, p_object_id)
            LOOP
                PERFORM claimius.recompute_user_object(p_app_id, v_user.user_id, p_object_type, p_object_id);
            END LOOP;
    END IF;

    RETURN v_id;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.link_app_object(UUID, TEXT, UUID, UUID) IS 'Creates a live app_object link and materializes access for the pair. Idempotent.';

-- unlink_app_object
-- Soft deletes a live link and purges the materialized access rows the
-- app held on the object.
CREATE OR REPLACE FUNCTION claimius.unlink_app_object(
    p_app_id      UUID,
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS BOOLEAN AS $$
BEGIN
    UPDATE claimius.app_object
    SET sa_deleted_at = now()
    WHERE app_id = p_app_id
      AND object_type = p_object_type
      AND object_id = p_object_id
      AND sa_deleted_at IS NULL;

    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;

    DELETE FROM claimius.user_object
    WHERE app_id = p_app_id AND object_type = p_object_type AND object_id = p_object_id;

    RETURN TRUE;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.unlink_app_object(UUID, TEXT, UUID) IS 'Soft deletes an app_object link and purges the app''s materialized access on the object.';

-- _app_ids_for_row
-- Returns every app_id a registered row is scoped to. Reads the row's
-- app_id column when the table has one; for internal tables scoped through
-- app_object it returns every linked app, which is empty for a row whose
-- link has not been created yet (the link materializes access when it
-- lands); external tables without app_id fall back to
-- claimius.get_app_id(). Used by every trigger that scopes a row's
-- effects to apps.
CREATE OR REPLACE FUNCTION claimius._app_ids_for_row(p_object_type TEXT, p_row JSONB)
    RETURNS SETOF UUID AS $$
DECLARE
    v_has_app_id BOOLEAN;
    v_is_internal BOOLEAN;
BEGIN
    SELECT t.has_app_id, t.is_internal INTO v_has_app_id, v_is_internal
    FROM claimius.table_info t
    WHERE t.object_type = p_object_type;

    IF v_has_app_id THEN
        RETURN NEXT (p_row ->> 'app_id')::UUID;
        RETURN;
    END IF;

    RETURN QUERY SELECT claimius.apps_for_object(p_object_type, (p_row ->> 'id')::UUID);
    IF FOUND OR v_is_internal THEN
        RETURN;
    END IF;

    RETURN NEXT claimius.get_app_id();
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._app_ids_for_row(TEXT, JSONB) IS 'Resolves every app_id a row is scoped to: the row''s app_id column, its app_object links, or claimius.get_app_id().';

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
    v_has_app_id        BOOLEAN;
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
    v_has_app_id      := claimius.table_has_column(p_object_type, 'app_id');
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

        -- Tables without app_id rely on claimius.get_app_id() at runtime to
        -- determine scope. That works only when the deployment hosts a
        -- single app's data: pure disciple, hybrid, or prophet with only
        -- the system app. On a prophet hosting non system apps,
        -- get_app_id() returns the system app id, which would mis-scope
        -- writes to other hosted apps. Reject the registration in that
        -- case so the implementer adds an app_id column.
        IF NOT v_has_app_id THEN
            IF EXISTS (SELECT 1 FROM claimius.prophet_state WHERE system_app_slug IS NOT NULL)
                AND EXISTS (
                    SELECT 1 FROM claimius.samna_app a
                                      JOIN claimius.prophet_state p ON p.system_app_slug IS NOT NULL
                    WHERE a.slug <> p.system_app_slug
                      AND a.sa_deleted_at IS NULL
                ) THEN
                RAISE EXCEPTION 'Table % has no app_id column but this prophet hosts non system apps; add an app_id column to disambiguate row scope', p_object_type;
            END IF;
        END IF;
    END IF;

    INSERT INTO claimius.table_info (
        schema_name, table_name, is_internal,
        has_sa_owner_id, has_sa_location_id, has_sa_parent_id, has_sa_root_id,
        has_sa_created_by, has_sa_deleted_at, has_sa_created_at, has_sa_updated_at,
        has_app_id, has_name, has_description
    ) VALUES (
                 v_schema, v_table, p_is_internal,
                 v_has_owner, v_has_location, v_has_parent, v_has_root,
                 v_has_created_by, v_has_deleted_at, v_has_created_at, v_has_updated_at,
                 v_has_app_id, v_has_name, v_has_description
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
                                            has_app_id = EXCLUDED.has_app_id,
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
    RETURNS TABLE(user_id UUID, app_id UUID) AS $$
BEGIN
    RETURN QUERY
        SELECT uc.user_id, uc.app_id
        FROM claimius.user_claim uc
        WHERE uc.id = p_user_claim_id AND uc.sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION claimius._best_grant(
    p_grants          JSONB,
    p_user_id         UUID,
    p_app_id          UUID,
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
        AND uc.app_id = p_app_id
        AND uc.sa_deleted_at IS NULL
        AND (uc.starts_at IS NULL OR uc.starts_at <= now())
        AND (uc.ends_at IS NULL OR uc.ends_at > now())
             JOIN claimius.claim c ON c.id = uc.claim_id AND c.sa_deleted_at IS NULL
    WHERE ((g ->> 'access')::INTEGER & p_required_access) = p_required_access
    ORDER BY claimius._popcount((g ->> 'access')::INTEGER) DESC, (g ->> 'access')::INTEGER DESC, c.sa_created_at ASC
    LIMIT 1
$$;

COMMENT ON FUNCTION claimius._best_grant(JSONB, UUID, UUID, INTEGER) IS 'Strongest surviving claim grant in a user_object grants array for one (user, app) whose mask covers p_required_access. Inlinable SRF shared by get_access, get_objects, and search so every read path resolves the actor token identically.';

CREATE OR REPLACE FUNCTION claimius._user_claim_grants(
    p_actor_id       UUID,
    p_app_id         UUID,
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
        claimius._effective_access(c.sa_access, co.sa_access) & 15,
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
      AND uc.app_id = p_app_id
      AND uc.sa_deleted_at IS NULL
      AND (uc.starts_at IS NULL OR uc.starts_at <= now())
      AND (uc.ends_at IS NULL OR uc.ends_at > now())
      AND (claimius._effective_access(c.sa_access, co.sa_access) & 16) = 0
$$;

-- _apply_denies
-- Settles the derivations of one user on one object. A deny subtracts its
-- bits: a deny of class direct, bound or composed on the object itself, from
-- every grant; any other deny, reaching the object through a tree, from the
-- inherited grants only, so a grant bound or composed on the object itself
-- stands over it. A deny with a scope subtracts from the fields it names
-- only. What remains of a grant is closed under implication, and a grant left
-- with nothing is dropped. A scoped grant keeps the fields the type carries,
-- each with its mask; an unscoped grant a scoped deny reaches maps the denied
-- fields, with * for the rest. The creator holds 15, subject to the direct
-- denies. Returns the surviving grants, the creator mask, the mask over every
-- grant and the merged scope: each field that differs from * with its mask,
-- NULL when every field carries the mask of *.
DROP FUNCTION IF EXISTS claimius._apply_denies(JSONB, JSONB, TEXT, UUID);
CREATE OR REPLACE FUNCTION claimius._apply_denies(
    p_grants        JSONB,
    p_denies        JSONB,
    p_created       BOOLEAN,
    p_fields        TEXT[]
) RETURNS JSONB AS $$
DECLARE
    v_direct_bits   INTEGER := 0;
    v_other_bits    INTEGER := 0;
    v_direct_field  JSONB := '{}'::JSONB;
    v_other_field   JSONB := '{}'::JSONB;
    v_deny          JSONB;
    v_grant         JSONB;
    v_key           TEXT;
    v_bits          INTEGER;
    v_inherited     BOOLEAN;
    v_denied        INTEGER;
    v_value         INTEGER;
    v_star          INTEGER;
    v_map           JSONB;
    v_access        INTEGER;
    v_grants        JSONB := '[]'::JSONB;
    v_cells         JSONB := '[]'::JSONB;
    v_direct        INTEGER := 0;
    v_scope         JSONB;
    v_total         INTEGER;
BEGIN
    IF jsonb_array_length(coalesce(p_denies, '[]'::JSONB)) = 0
        AND NOT jsonb_path_exists(p_grants, '$[*] ? (@.scope.type() == "object")')
    THEN
        SELECT coalesce(jsonb_agg(g ORDER BY n), '[]'::JSONB), coalesce(bit_or((g ->> 'access')::INTEGER), 0)
        INTO v_grants, v_total
        FROM jsonb_array_elements(p_grants) WITH ORDINALITY AS e(g, n)
        WHERE (g ->> 'access')::INTEGER <> 0;
        RETURN jsonb_build_object(
            'grants', v_grants,
            'direct', CASE WHEN p_created THEN 15 ELSE 0 END,
            'access', v_total | CASE WHEN p_created THEN 15 ELSE 0 END,
            'scope', NULL);
    END IF;

    FOR v_deny IN SELECT * FROM jsonb_array_elements(coalesce(p_denies, '[]'::JSONB)) LOOP
        v_bits := (v_deny ->> 'access')::INTEGER;
        IF jsonb_typeof(v_deny -> 'scope') = 'object' THEN
            FOR v_key IN SELECT jsonb_object_keys(v_deny -> 'scope') LOOP
                CONTINUE WHEN NOT v_key = ANY(p_fields);
                IF v_deny ->> 'tree_type' = 'direct' THEN
                    v_direct_field := v_direct_field || jsonb_build_object(v_key, coalesce((v_direct_field ->> v_key)::INTEGER, 0) | v_bits);
                ELSE
                    v_other_field := v_other_field || jsonb_build_object(v_key, coalesce((v_other_field ->> v_key)::INTEGER, 0) | v_bits);
                END IF;
            END LOOP;
        ELSIF v_deny ->> 'tree_type' = 'direct' THEN
            v_direct_bits := v_direct_bits | v_bits;
        ELSE
            v_other_bits := v_other_bits | v_bits;
        END IF;
    END LOOP;

    FOR v_grant IN
        SELECT g FROM jsonb_array_elements(p_grants) g
        UNION ALL
        SELECT jsonb_build_object('access', 15, 'tree_type', 'direct', 'creator', TRUE) WHERE p_created
    LOOP
        v_bits := (v_grant ->> 'access')::INTEGER;
        v_inherited := v_grant ->> 'tree_type' <> 'direct';
        v_denied := v_direct_bits | CASE WHEN v_inherited THEN v_other_bits ELSE 0 END;
        v_map := NULL;

        IF jsonb_typeof(v_grant -> 'scope') = 'object' THEN
            v_map := '{}'::JSONB;
            v_access := 0;
            FOR v_key IN SELECT jsonb_object_keys(v_grant -> 'scope') LOOP
                CONTINUE WHEN NOT v_key = ANY(p_fields);
                v_value := claimius._close_access(v_bits & ~(v_denied
                    | coalesce((v_direct_field ->> v_key)::INTEGER, 0)
                    | CASE WHEN v_inherited THEN coalesce((v_other_field ->> v_key)::INTEGER, 0) ELSE 0 END));
                v_map := v_map || jsonb_build_object(v_key, v_value);
                v_access := v_access | v_value;
            END LOOP;
        ELSE
            v_star := claimius._close_access(v_bits & ~v_denied);
            FOR v_key IN SELECT jsonb_object_keys(v_direct_field || CASE WHEN v_inherited THEN v_other_field ELSE '{}'::JSONB END) LOOP
                v_value := claimius._close_access(v_bits & ~(v_denied
                    | coalesce((v_direct_field ->> v_key)::INTEGER, 0)
                    | CASE WHEN v_inherited THEN coalesce((v_other_field ->> v_key)::INTEGER, 0) ELSE 0 END));
                IF v_value <> v_star THEN
                    v_map := coalesce(v_map, '{}'::JSONB) || jsonb_build_object(v_key, v_value);
                END IF;
            END LOOP;
            IF v_map IS NOT NULL THEN
                v_map := v_map || jsonb_build_object('*', v_star);
            END IF;
            v_access := v_star;
        END IF;

        CONTINUE WHEN v_access = 0;

        v_cells := v_cells || jsonb_build_array(jsonb_build_object('access', v_access, 'scope', v_map));
        IF v_grant ? 'creator' THEN
            v_direct := v_access;
        ELSE
            v_grants := v_grants || jsonb_build_array(v_grant || jsonb_build_object('access', v_access, 'scope', v_map));
        END IF;
    END LOOP;

    SELECT coalesce(bit_or(CASE WHEN jsonb_typeof(c -> 'scope') = 'object'
                                THEN coalesce((c -> 'scope' ->> '*')::INTEGER, 0)
                                ELSE (c ->> 'access')::INTEGER END), 0),
           coalesce(bit_or((c ->> 'access')::INTEGER), 0)
    INTO v_star, v_total
    FROM jsonb_array_elements(v_cells) c;

    SELECT jsonb_object_agg(s.field, s.mask)
    INTO v_scope
    FROM (
        SELECT f.field,
               bit_or(CASE WHEN jsonb_typeof(c -> 'scope') <> 'object' THEN (c ->> 'access')::INTEGER
                           WHEN c -> 'scope' ? f.field THEN (c -> 'scope' ->> f.field)::INTEGER
                           ELSE coalesce((c -> 'scope' ->> '*')::INTEGER, 0) END) AS mask
        FROM (
            SELECT DISTINCT k.field
            FROM jsonb_array_elements(v_cells) c
                CROSS JOIN LATERAL jsonb_object_keys(CASE WHEN jsonb_typeof(c -> 'scope') = 'object' THEN c -> 'scope' ELSE '{}'::JSONB END) AS k(field)
            WHERE k.field <> '*'
        ) f
            CROSS JOIN jsonb_array_elements(v_cells) c
        GROUP BY f.field
    ) s
    WHERE s.mask <> v_star;

    RETURN jsonb_build_object(
        'grants', v_grants,
        'direct', v_direct,
        'access', v_total,
        'scope', CASE WHEN v_scope IS NULL THEN NULL ELSE v_scope || jsonb_build_object('*', v_star) END);
END;
$$ LANGUAGE plpgsql IMMUTABLE;

COMMENT ON FUNCTION claimius._apply_denies(JSONB, JSONB, BOOLEAN, TEXT[]) IS 'Settles the derivations of one user on one object: direct denies subtract from every grant and from the creator, other denies from inherited grants, a scoped deny from its fields only; each remainder is closed under implication and a grant left empty is dropped. Returns grants, the creator mask, the mask over all grants and the merged field scope.';

-- _build_grants_for_objects
-- Computes the grants of one user on a set of objects of one type in one
-- statement, walking the claim_objects bound directly, bound on an ancestor
-- through the inheritance trees, or reached through composition: a
-- claim_object whose object_type is 'claimius.claim' links the claim held to
-- a claim whose bindings it takes over, each cascading as that binding does
-- and keeping its class. A binding carries claim & expand(coalesce(binding,
-- claim)); a composed one the grant bits both sides carry. On a claim, a held
-- claim and a link to it each give read. p_created names the objects the
-- user created. Denies, scopes and the creator settle through _apply_denies.
-- An object neither derivation nor creation reaches has no row.
DROP FUNCTION IF EXISTS claimius._build_grants_for_object(UUID, UUID, TEXT, UUID);
DROP FUNCTION IF EXISTS claimius._build_grants_for_objects(UUID, UUID, TEXT, UUID[]);
CREATE OR REPLACE FUNCTION claimius._build_grants_for_objects(
    p_user_id     UUID,
    p_app_id      UUID,
    p_object_type TEXT,
    p_object_ids  UUID[],
    p_created     UUID[] DEFAULT '{}'
) RETURNS TABLE (object_id UUID, grants JSONB, direct_access INTEGER, sa_access INTEGER, scope JSONB) LANGUAGE sql STABLE AS $$
    WITH user_claims AS (
        SELECT uc.claim_id, c.sa_access AS claim_access, c.inherits
        FROM claimius.user_claim uc
            JOIN claimius.claim c ON c.id = uc.claim_id
        WHERE uc.user_id = p_user_id
          AND uc.app_id = p_app_id
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
    ),
    target AS (
        SELECT DISTINCT t.id, t.id = ANY(coalesce(p_created, '{}'::UUID[])) AS created FROM unnest(p_object_ids) AS t(id)
    ),
    fields AS (
        SELECT coalesce(array_agg(a.attname::TEXT), '{}'::TEXT[]) AS names
        FROM pg_attribute a
        WHERE a.attrelid = to_regclass(p_object_type)
          AND a.attnum > 0
          AND NOT a.attisdropped
    ),
    derivations AS (
        SELECT t.id                 AS object_id,
               co.claim_id,
               p_object_type        AS from_type,
               t.id                 AS from_id,
               NULL::UUID           AS composed_from,
               'direct'::TEXT       AS tree_type,
               1                    AS path,
               CASE WHEN p_object_type = 'claimius.claim' THEN 4
                    ELSE claimius._effective_access(uc.claim_access, co.sa_access) END AS sa_access,
               co.scope
        FROM target t
            JOIN claimius.claim_object co
                ON co.object_type = p_object_type
               AND co.object_id = t.id
               AND co.app_id = p_app_id
               AND co.sa_deleted_at IS NULL
            JOIN user_claims uc ON uc.claim_id = co.claim_id
        WHERE p_object_type <> 'claimius.claim'
           OR (claimius._effective_access(uc.claim_access, co.sa_access) & 16) = 0

        UNION ALL

        SELECT t.id, t.id, p_object_type, t.id, NULL::UUID, 'direct'::TEXT, 1, 4, NULL::JSONB
        FROM target t
            JOIN user_claims uc ON uc.claim_id = t.id
        WHERE p_object_type = 'claimius.claim'

        UNION ALL

        SELECT t.id,
               co.claim_id,
               co.object_type,
               co.object_id,
               NULL::UUID,
               e.tree_type::TEXT,
               2,
               claimius._effective_access(uc.claim_access, co.sa_access),
               co.scope
        FROM target t
            JOIN claimius.inheritance_info e
                ON e.descendant_type = p_object_type
               AND e.descendant_id = t.id
               AND e.depth > 0
            JOIN claimius.claim_object co
                ON co.object_type = e.ancestor_type
               AND co.object_id = e.ancestor_id
               AND co.app_id = p_app_id
               AND co.sa_deleted_at IS NULL
               AND co.inherits = TRUE
            JOIN user_claims uc ON uc.claim_id = co.claim_id AND uc.inherits = TRUE

        UNION ALL

        SELECT n.target_id,
               uc.claim_id,
               n.object_type,
               n.object_id,
               cb.id,
               n.tree_type,
               3,
               (claimius._effective_access(uc.claim_access, link.sa_access)
                    & claimius._effective_access(cb.sa_access, co.sa_access) & ~16)
               | ((claimius._effective_access(uc.claim_access, link.sa_access)
                    | claimius._effective_access(cb.sa_access, co.sa_access)) & 16),
               co.scope
        FROM (
            SELECT t.id AS target_id, p_object_type AS object_type, t.id AS object_id, 'direct'::TEXT AS tree_type
            FROM target t
            UNION ALL
            SELECT t.id, e.ancestor_type, e.ancestor_id, e.tree_type::TEXT
            FROM target t
                JOIN claimius.inheritance_info e
                    ON e.descendant_type = p_object_type
                   AND e.descendant_id = t.id
                   AND e.depth > 0
        ) n
            JOIN claimius.claim_object co
                ON co.object_type = n.object_type
               AND co.object_id = n.object_id
               AND co.app_id = p_app_id
               AND co.sa_deleted_at IS NULL
               AND (n.tree_type = 'direct' OR co.inherits = TRUE)
            JOIN claimius.claim cb
                ON cb.id = co.claim_id
               AND cb.sa_deleted_at IS NULL
               AND cb.inherits = TRUE
            JOIN claimius.claim_object link
                ON link.object_type = 'claimius.claim'
               AND link.object_id = co.claim_id
               AND link.app_id = p_app_id
               AND link.sa_deleted_at IS NULL
               AND link.inherits = TRUE
            JOIN user_claims uc ON uc.claim_id = link.claim_id
        WHERE p_object_type <> 'claimius.claim'
    ),
    entries AS (
        SELECT d.object_id,
               (d.sa_access & 16) <> 0 AS is_deny,
               d.path, d.tree_type, d.from_type, d.from_id, d.claim_id,
               jsonb_strip_nulls(jsonb_build_object(
                   'claim_id', d.claim_id,
                   'access', d.sa_access & 15,
                   'cascaded_from', jsonb_build_object('type', d.from_type, 'id', d.from_id),
                   'composed_from', d.composed_from,
                   'tree_type', d.tree_type
               )) || jsonb_build_object('scope', d.scope) AS entry
        FROM derivations d
    ),
    grouped AS (
        SELECT e.object_id,
               coalesce(
                   jsonb_agg(e.entry ORDER BY e.path, e.tree_type, e.from_type, e.from_id, e.claim_id) FILTER (WHERE NOT e.is_deny),
                   '[]'::JSONB
               ) AS grants,
               coalesce(
                   jsonb_agg(e.entry ORDER BY e.path, e.tree_type, e.from_type, e.from_id, e.claim_id) FILTER (WHERE e.is_deny),
                   '[]'::JSONB
               ) AS denies
        FROM entries e
        GROUP BY e.object_id
    )
    SELECT t.id,
           s.settled -> 'grants',
           (s.settled ->> 'direct')::INTEGER,
           (s.settled ->> 'access')::INTEGER,
           CASE WHEN jsonb_typeof(s.settled -> 'scope') = 'object' THEN s.settled -> 'scope' END
    FROM target t
        CROSS JOIN fields f
        LEFT JOIN grouped g ON g.object_id = t.id
        CROSS JOIN LATERAL (
            SELECT claimius._apply_denies(coalesce(g.grants, '[]'::JSONB), coalesce(g.denies, '[]'::JSONB), t.created, f.names) AS settled
        ) s
    WHERE g.object_id IS NOT NULL OR t.created
$$;

COMMENT ON FUNCTION claimius._build_grants_for_objects(UUID, UUID, TEXT, UUID[], UUID[]) IS 'Grants of one user on a set of objects of one type, in one statement, with the creator mask, the mask and the field scope each object settles to. Walks direct, hierarchy cascaded, and composed paths, each binding narrowing its claim, and settles denies, scopes and the creator through _apply_denies.';

-- _denormalize_objects
-- Reads the name, description, sa_owner_id, sa_location_id, sa_root_id and
-- sa_created_by of a set of rows of one registered table in one statement. A
-- row of a table carrying sa_deleted_at is returned only while it is live.
-- Tables that carry no sa_root_id derive the root from the owning
-- organization or, failing that, the parent location.
DROP FUNCTION IF EXISTS claimius._denormalize_object(TEXT, UUID);
CREATE OR REPLACE FUNCTION claimius._denormalize_objects(
    p_object_type   TEXT,
    p_object_ids    UUID[]
) RETURNS TABLE (
                    id              UUID,
                    sa_name         TEXT,
                    sa_description  TEXT,
                    sa_link         TEXT,
                    sa_owner_id     UUID,
                    sa_location_id  UUID,
                    sa_root_id      UUID,
                    sa_created_by   UUID
                ) AS $$
DECLARE
    v_info  claimius.table_info%ROWTYPE;
    v_owner TEXT;
    v_loc   TEXT;
BEGIN
    SELECT ti.* INTO v_info FROM claimius.table_info ti WHERE ti.object_type = p_object_type;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    v_owner := CASE WHEN v_info.has_sa_owner_id THEN 'r.sa_owner_id' ELSE 'NULL::UUID' END;
    v_loc   := CASE WHEN v_info.has_sa_location_id THEN 'r.sa_location_id' ELSE 'NULL::UUID' END;

    RETURN QUERY EXECUTE format(
        'SELECT r.id, %s, %s, NULL::TEXT, %s, %s, coalesce(%s, o.sa_root_id, l.sa_root_id), %s
           FROM %I.%I r
                LEFT JOIN claimius.organization o ON o.id = %s
                LEFT JOIN claimius.location l ON l.id = %s
          WHERE r.id = ANY($1)%s',
        CASE WHEN v_info.has_name THEN 'r.name::TEXT' ELSE 'NULL::TEXT' END,
        CASE WHEN v_info.has_description THEN 'r.description::TEXT' ELSE 'NULL::TEXT' END,
        v_owner,
        v_loc,
        CASE WHEN v_info.has_sa_root_id THEN 'r.sa_root_id' ELSE 'NULL::UUID' END,
        CASE WHEN v_info.has_sa_created_by THEN 'r.sa_created_by' ELSE 'NULL::UUID' END,
        split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2),
        v_owner,
        v_loc,
        CASE WHEN v_info.has_sa_deleted_at THEN ' AND r.sa_deleted_at IS NULL' ELSE '' END
    ) USING p_object_ids;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._denormalize_objects(TEXT, UUID[]) IS 'Reads name, description, owner, location, root and creator of a set of live rows of one registered table. Falls back to organization or location sa_root_id when the row carries no sa_root_id column.';

-- _write_object_info
-- Brings the object_info rows of a set of objects of one registered table in
-- step with the rows themselves: a live row is written when a field differs,
-- a row gone or soft deleted loses its object_info row. Called by the calc
-- triggers on every write of the object and by the bulk paths, so a rename
-- is one row here and no pair.
CREATE OR REPLACE FUNCTION claimius._write_object_info(
    p_object_type   TEXT,
    p_object_ids    UUID[]
) RETURNS VOID AS $$
BEGIN
    IF p_object_type IS NULL OR p_object_ids IS NULL OR cardinality(p_object_ids) = 0 THEN
        RETURN;
    END IF;

    WITH live AS MATERIALIZED (
        SELECT r.* FROM claimius._denormalize_objects(p_object_type, p_object_ids) r
    ),
    gone AS (
        DELETE FROM claimius.object_info oi
        WHERE oi.object_type = p_object_type
          AND oi.object_id = ANY(p_object_ids)
          AND oi.object_id NOT IN (SELECT l.id FROM live l)
    )
    INSERT INTO claimius.object_info (object_type, object_id, sa_owner_id, sa_location_id, sa_root_id, sa_name, sa_description, sa_link)
    SELECT p_object_type, l.id, l.sa_owner_id, l.sa_location_id, l.sa_root_id, l.sa_name, l.sa_description, l.sa_link
    FROM live l
    ON CONFLICT (object_type, object_id) DO UPDATE SET
        sa_owner_id    = EXCLUDED.sa_owner_id,
        sa_location_id = EXCLUDED.sa_location_id,
        sa_root_id     = EXCLUDED.sa_root_id,
        sa_name        = EXCLUDED.sa_name,
        sa_description = EXCLUDED.sa_description,
        sa_link        = EXCLUDED.sa_link,
        sa_updated_at  = now()
    WHERE (claimius.object_info.sa_owner_id, claimius.object_info.sa_location_id, claimius.object_info.sa_root_id,
           claimius.object_info.sa_name, claimius.object_info.sa_description, claimius.object_info.sa_link)
          IS DISTINCT FROM
          (EXCLUDED.sa_owner_id, EXCLUDED.sa_location_id, EXCLUDED.sa_root_id,
           EXCLUDED.sa_name, EXCLUDED.sa_description, EXCLUDED.sa_link);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._write_object_info(TEXT, UUID[]) IS 'Writes the object_info rows of a set of objects of one type from the rows themselves: upserts the live ones where a field differs, deletes the ones gone or soft deleted.';

-- _lock_objects
-- Takes the transaction advisory lock of every object named, one key per
-- object_type:object_id, in sorted key order. A settle takes the locks of
-- every object it will write before its first write, so two settles that
-- overlap acquire their common objects in one order and cannot deadlock. A
-- lock already held is taken again without waiting and without reordering.
CREATE OR REPLACE FUNCTION claimius._lock_objects(p_keys TEXT[])
    RETURNS VOID AS $$
DECLARE
    v_key BIGINT;
BEGIN
    FOR v_key IN
        SELECT DISTINCT hashtextextended(k, 0)
        FROM unnest(p_keys) AS k
        WHERE k IS NOT NULL
        ORDER BY 1
    LOOP
        PERFORM pg_advisory_xact_lock(v_key);
    END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._lock_objects(TEXT[]) IS 'Takes the per object transaction advisory locks of a set of object_type:object_id keys in sorted key order.';

-- recompute_user_object
-- The single function that recomputes user_object for one user on a set of
-- objects of one type: the one pair a check asks about, or the reader's share
-- of an area in one statement per step. It handles insert, update, and delete
-- based on whether grants survive. A creator holds its row through
-- sa_created_by, which is a user_claim.id, less what the denies bound on the
-- row itself take. The pairs it inserts and deletes move
-- user_users through _count_user_users. It takes the per object locks of its
-- objects, sorted, before its first write, which inside a settle are already
-- held. A pair holds only the per user columns; a pair written on an object
-- that has no object_info row yet brings one, so no pair is ever without its
-- object's fields. Returns whether the user's pair set changed.
DROP FUNCTION IF EXISTS claimius.recompute_user_object(UUID, UUID, TEXT, UUID);
CREATE OR REPLACE FUNCTION claimius.recompute_user_object(
    p_app_id        UUID,
    p_user_id       UUID,
    p_object_type   TEXT,
    VARIADIC p_object_ids UUID[]
) RETURNS BOOLEAN AS $$
DECLARE
    v_info      claimius.table_info%ROWTYPE;
    v_ids       UUID[];
    v_pairs     JSONB;
    v_removed   INTEGER;
    v_added     INTEGER;
BEGIN
    IF p_app_id IS NULL OR p_user_id IS NULL OR p_object_type IS NULL OR p_object_ids IS NULL THEN
        RETURN FALSE;
    END IF;

    v_ids := ARRAY(SELECT DISTINCT i FROM unnest(p_object_ids) AS i WHERE i IS NOT NULL);
    IF cardinality(v_ids) = 0 THEN
        RETURN FALSE;
    END IF;

    SELECT ti.* INTO v_info FROM claimius.table_info ti WHERE ti.object_type = p_object_type;
    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;

    IF NOT claimius.in_replay_mode() THEN
        PERFORM claimius._lock_objects(ARRAY(SELECT p_object_type || ':' || i::TEXT FROM unnest(v_ids) AS i));
    END IF;

    WITH target AS (
        SELECT i.id, r.id IS NOT NULL AS live, r.sa_name, r.sa_description, r.sa_link, r.sa_owner_id, r.sa_location_id, r.sa_root_id, r.sa_created_by
        FROM unnest(v_ids) AS i(id)
            LEFT JOIN claimius._denormalize_objects(p_object_type, v_ids) r ON r.id = i.id
        WHERE r.id IS NOT NULL OR NOT v_info.has_sa_deleted_at
    ),
    created AS (
        SELECT t.id
        FROM target t
        WHERE (
            CASE
                WHEN p_object_type = 'claimius.samna_user' THEN EXISTS (
                    SELECT 1 FROM claimius.samna_user su
                    WHERE su.id = t.id
                      AND su.user_id = p_user_id
                      AND su.app_id = p_app_id
                      AND su.sa_deleted_at IS NULL)
                ELSE t.sa_created_by = p_user_id OR EXISTS (
                    SELECT 1 FROM claimius.user_claim uc
                    WHERE uc.id = t.sa_created_by
                      AND uc.user_id = p_user_id
                      AND uc.app_id = p_app_id
                      AND uc.sa_deleted_at IS NULL)
            END
        ) IS TRUE
    ),
    built AS (
        SELECT t.*, g.grants, g.direct_access, g.sa_access AS access, g.scope
        FROM target t
            JOIN claimius._build_grants_for_objects(
                    p_user_id, p_app_id, p_object_type,
                    ARRAY(SELECT x.id FROM target x), ARRAY(SELECT c.id FROM created c)) g ON g.object_id = t.id
    )
    SELECT coalesce(jsonb_agg(jsonb_build_object(
               'object_id', b.id,
               'live', b.live,
               'sa_access', b.access,
               'scope', b.scope,
               'grants', b.grants,
               'direct_grant', b.direct_access <> 0,
               'sa_owner_id', b.sa_owner_id,
               'sa_location_id', b.sa_location_id,
               'sa_root_id', b.sa_root_id,
               'sa_name', b.sa_name,
               'sa_description', b.sa_description,
               'sa_link', b.sa_link
           )), '[]'::JSONB)
    INTO v_pairs
    FROM built b
    WHERE b.access <> 0;

    DELETE FROM claimius.user_object
    WHERE user_id = p_user_id
      AND app_id = p_app_id
      AND object_type = p_object_type
      AND object_id = ANY(v_ids)
      AND object_id NOT IN (SELECT (p ->> 'object_id')::UUID FROM jsonb_array_elements(v_pairs) AS p);
    GET DIAGNOSTICS v_removed = ROW_COUNT;

    INSERT INTO claimius.object_info (object_type, object_id, sa_owner_id, sa_location_id, sa_root_id, sa_name, sa_description, sa_link)
    SELECT p_object_type, p.object_id, p.sa_owner_id, p.sa_location_id, p.sa_root_id, p.sa_name, p.sa_description, p.sa_link
    FROM jsonb_to_recordset(v_pairs) AS p(
        object_id UUID, live BOOLEAN, sa_owner_id UUID, sa_location_id UUID, sa_root_id UUID,
        sa_name TEXT, sa_description TEXT, sa_link TEXT
    )
    WHERE p.live
    ON CONFLICT (object_type, object_id) DO NOTHING;

    WITH written AS (
        INSERT INTO claimius.user_object (
            app_id, user_id, object_id, object_type,
            sa_access, scope, grants, direct_grant
        )
        SELECT p_app_id, p_user_id, p.object_id, p_object_type,
               p.sa_access, p.scope, p.grants, p.direct_grant
        FROM jsonb_to_recordset(v_pairs) AS p(
            object_id UUID, sa_access INTEGER, scope JSONB, grants JSONB, direct_grant BOOLEAN
        )
        ON CONFLICT (user_id, app_id, object_type, object_id) DO UPDATE SET
                                       sa_access = EXCLUDED.sa_access,
                                       scope = EXCLUDED.scope,
                                       grants = EXCLUDED.grants,
                                       direct_grant = EXCLUDED.direct_grant,
                                       sa_updated_at = now()
        WHERE claimius.user_object.sa_access        IS DISTINCT FROM EXCLUDED.sa_access
           OR claimius.user_object.scope            IS DISTINCT FROM EXCLUDED.scope
           OR claimius.user_object.grants           IS DISTINCT FROM EXCLUDED.grants
           OR claimius.user_object.direct_grant     IS DISTINCT FROM EXCLUDED.direct_grant
        RETURNING xmax = 0 AS inserted
    )
    SELECT count(*) FILTER (WHERE inserted) INTO v_added FROM written;

    RETURN v_removed > 0 OR v_added > 0;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.recompute_user_object(UUID, UUID, TEXT, UUID[]) IS 'Recomputes user_object for one user on a set of objects of one type, one pair or a whole share. Purges the pairs whose object is soft deleted or absent or that no grant reaches. Returns whether the user''s pair set changed.';

-- _count_user_users
-- Statement trigger on user_object that keeps user_users by counting. Every
-- pair (u, x) that enters moves sharing_object_count up by one between u and
-- each other user of object x, in both directions; every pair that leaves
-- moves it down, and a row reaching zero is removed. A statement touches the
-- U_x users of each object it names and nothing of either user's other
-- objects. Two pairs entering or leaving on the same object in one statement
-- count once per ordered pair of users. A pair on a claimius.claim moves no
-- count: holding a claim reads the claim, and a claim is a template, not an
-- object two users share. Two transactions adding or removing
-- pairs on one object serialize on an advisory lock per object, held before
-- the other holders are read, so each reads the pairs the other committed
-- and no ordered pair is missed or counted twice. A settle and
-- recompute_user_object hold those locks, taken sorted before their first
-- write, so here they are taken again, which waits on nothing and reorders
-- nothing; any other writer of user_object takes them here. Quiet under
-- replay_mode, where the bulk seed rebuilds it with _refresh_user_users.
CREATE OR REPLACE FUNCTION claimius._count_user_users()
    RETURNS TRIGGER AS $$
BEGIN
    IF claimius.in_replay_mode() THEN
        RETURN NULL;
    END IF;

    PERFORM claimius._lock_objects(ARRAY(SELECT c.object_type || ':' || c.object_id::TEXT FROM changed c WHERE c.object_type <> 'claimius.claim'));

    IF TG_OP = 'INSERT' THEN
        WITH moved AS (
            SELECT c.app_id, d.viewer_id, d.target_user_id
            FROM changed c
                JOIN claimius.user_object o
                    ON o.app_id = c.app_id
                   AND o.object_type = c.object_type
                   AND o.object_id = c.object_id
                   AND o.user_id <> c.user_id
                CROSS JOIN LATERAL (VALUES (c.user_id, o.user_id), (o.user_id, c.user_id)) AS d(viewer_id, target_user_id)
            WHERE c.object_type <> 'claimius.claim'
              AND (d.viewer_id = c.user_id
                   OR NOT EXISTS (SELECT 1 FROM changed n WHERE n.user_id = o.user_id AND n.app_id = o.app_id AND n.object_type = o.object_type AND n.object_id = o.object_id))
        )
        INSERT INTO claimius.user_users (id, app_id, viewer_id, target_user_id, sharing_object_count)
        SELECT claimius.composite_id(m.app_id::TEXT, m.viewer_id::TEXT, m.target_user_id::TEXT),
               m.app_id, m.viewer_id, m.target_user_id, count(*)::INTEGER
        FROM moved m
        GROUP BY m.app_id, m.viewer_id, m.target_user_id
        ON CONFLICT (id) DO UPDATE SET
            sharing_object_count = claimius.user_users.sharing_object_count + EXCLUDED.sharing_object_count,
            last_shared_at       = now();
    ELSE
        WITH moved AS (
            SELECT c.app_id, c.user_id AS viewer_id, o.user_id AS target_user_id
            FROM changed c
                JOIN changed o
                    ON o.app_id = c.app_id
                   AND o.object_type = c.object_type
                   AND o.object_id = c.object_id
                   AND o.user_id <> c.user_id
            WHERE c.object_type <> 'claimius.claim'

            UNION ALL

            SELECT c.app_id, d.viewer_id, d.target_user_id
            FROM changed c
                JOIN claimius.user_object o
                    ON o.app_id = c.app_id
                   AND o.object_type = c.object_type
                   AND o.object_id = c.object_id
                CROSS JOIN LATERAL (VALUES (c.user_id, o.user_id), (o.user_id, c.user_id)) AS d(viewer_id, target_user_id)
            WHERE c.object_type <> 'claimius.claim'
        ),
        delta AS (
            SELECT claimius.composite_id(m.app_id::TEXT, m.viewer_id::TEXT, m.target_user_id::TEXT) AS id,
                   count(*)::INTEGER AS n
            FROM moved m
            GROUP BY m.app_id, m.viewer_id, m.target_user_id
        ),
        gone AS (
            DELETE FROM claimius.user_users uu
            USING delta d
            WHERE uu.id = d.id AND uu.sharing_object_count <= d.n
            RETURNING uu.id
        )
        UPDATE claimius.user_users uu
        SET sharing_object_count = uu.sharing_object_count - d.n
        FROM delta d
        WHERE uu.id = d.id AND uu.sharing_object_count > d.n;
    END IF;

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._count_user_users() IS 'Statement trigger on user_object. Moves user_users.sharing_object_count by one per pair entering or leaving against every other user of the object, both directions, and removes a row at zero. A pair on a claimius.claim moves no count: a claim is a template, not a shared object. Quiet under replay_mode.';

-- _refresh_user_users
-- The bulk rebuild of user_users on both sides of one user from user_object:
-- the rows where it is the viewer and the rows where it is the target. Reads
-- the user's whole share and every other user of each object in it, so it
-- runs only on the bulk paths: the bulk seed, whose pairs are written under
-- replay_mode where no count moves, and rebuild_user_object, which repairs
-- any drift. Rows whose count did not change are not rewritten.
CREATE OR REPLACE FUNCTION claimius._refresh_user_users(p_app_id UUID, p_viewer_id UUID)
    RETURNS VOID AS $$
BEGIN
    WITH shared AS (
        SELECT other.user_id                AS other_id,
               count(*)::INTEGER            AS sharing
        FROM claimius.user_object mine
            JOIN claimius.user_object other
                ON other.app_id = mine.app_id
               AND other.object_type = mine.object_type
               AND other.object_id = mine.object_id
               AND other.user_id <> mine.user_id
        WHERE mine.app_id = p_app_id AND mine.user_id = p_viewer_id
          AND mine.object_type <> 'claimius.claim'
        GROUP BY other.user_id
    ),
    wanted AS (
        SELECT claimius.composite_id(p_app_id::TEXT, p_viewer_id::TEXT, s.other_id::TEXT) AS id,
               p_viewer_id AS viewer_id, s.other_id AS target_user_id, s.sharing
        FROM shared s
        UNION ALL
        SELECT claimius.composite_id(p_app_id::TEXT, s.other_id::TEXT, p_viewer_id::TEXT),
               s.other_id, p_viewer_id, s.sharing
        FROM shared s
    ),
    written AS (
        INSERT INTO claimius.user_users (id, app_id, viewer_id, target_user_id, sharing_object_count)
        SELECT w.id, p_app_id, w.viewer_id, w.target_user_id, w.sharing
        FROM wanted w
        ON CONFLICT (id) DO UPDATE SET
            sharing_object_count = EXCLUDED.sharing_object_count,
            last_shared_at       = now()
        WHERE claimius.user_users.sharing_object_count IS DISTINCT FROM EXCLUDED.sharing_object_count
        RETURNING 1
    )
    DELETE FROM claimius.user_users uu
    WHERE uu.app_id = p_app_id
      AND (uu.viewer_id = p_viewer_id OR uu.target_user_id = p_viewer_id)
      AND uu.id NOT IN (SELECT w.id FROM wanted w);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._refresh_user_users(p_app_id uuid, p_viewer_id uuid) IS 'Bulk rebuild of the user_users rows on both sides of one user in one app from user_object, rewriting only the counts that changed. Pairs on a claimius.claim are not counted. For the bulk paths; every other write is counted by _count_user_users.';


-- _affected_users_for_object
-- Returns the list of users whose access to the given object could change.
-- Includes any user holding a relevant claim plus any user already holding a
-- user_object pair on the object, read through idx_user_object_object_user.
CREATE OR REPLACE FUNCTION claimius._affected_users_for_object(
    p_app_id        UUID,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS TABLE(user_id UUID) AS $$
DECLARE
    v_parts         TEXT[];
    v_has_creator   BOOLEAN;
    v_creator_uc_id UUID;
    v_creator_user  UUID;
BEGIN
    -- The creator of a row holds it through recompute_user_object without
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
                  AND uc.app_id = p_app_id
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

            -- existing pairs on the object
            SELECT uo.user_id FROM claimius.user_object uo
            WHERE uo.app_id = p_app_id AND uo.object_type = p_object_type AND uo.object_id = p_object_id

            UNION

            -- users holding any user_claim in this app whose claim is bound
            -- directly to this object
            SELECT uc.user_id
            FROM claimius.user_claim uc
                     JOIN claimius.claim_object co
                          ON co.claim_id = uc.claim_id
                         AND co.app_id = p_app_id
                         AND co.sa_deleted_at IS NULL
                         AND co.object_type = p_object_type
                         AND co.object_id = p_object_id
            WHERE uc.app_id = p_app_id
              AND uc.sa_deleted_at IS NULL

            UNION

            -- users holding any user_claim whose claim is bound to an ancestor
            -- of this object via the closure index
            SELECT uc.user_id
            FROM claimius.user_claim uc
                     JOIN claimius.claim_object co
                          ON co.claim_id = uc.claim_id
                         AND co.app_id = p_app_id
                         AND co.sa_deleted_at IS NULL
                         AND co.inherits = TRUE
                     JOIN claimius.inheritance_info e
                          ON e.ancestor_type   = co.object_type
                         AND e.ancestor_id     = co.object_id
                         AND e.descendant_type = p_object_type
                         AND e.descendant_id   = p_object_id
                         AND e.depth > 0
            WHERE uc.app_id = p_app_id
              AND uc.sa_deleted_at IS NULL
        )
        SELECT DISTINCT au.user_id FROM all_users au;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._affected_users_for_object(p_app_id uuid, p_object_type text, p_object_id uuid) IS 'Users whose access to the object could change.';

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
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

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
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

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
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

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
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

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
        SELECT DISTINCT uc.app_id, uc.user_id, co.object_type, co.object_id
        FROM claimius.user_claim uc
                 JOIN claimius.claim_object co ON co.claim_id = uc.claim_id
        WHERE uc.sa_deleted_at IS NULL
          AND co.sa_deleted_at IS NULL
    LOOP
        PERFORM claimius.recompute_user_object(v_pair.app_id, v_pair.user_id, v_pair.object_type, v_pair.object_id);
    END LOOP;

    PERFORM claimius._refresh_user_users(s.app_id, s.user_id)
    FROM (SELECT DISTINCT app_id, user_id FROM claimius.user_object) s;
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
    p_app_id        UUID,
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
      AND uc.app_id = p_app_id
      AND uc.sa_deleted_at IS NULL;

    RETURN v_user_id;
EXCEPTION WHEN OTHERS THEN
    RETURN NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._actor_user_for_row(UUID, TEXT, ANYELEMENT) IS 'User behind sa_created_by on a row. The one user a write materializes synchronously.';

-- ----------------------------------------------------------------------------
-- _raise_object_diff
-- Marks the area around one object as needing recalculation. One row, no user
-- enumeration. Repeated changes to the same object collapse onto the same id
-- and add one to its raise_count, superseding every share settled before.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius._raise_object_diff(
    p_app_id        UUID,
    p_object_type   TEXT,
    p_object_id     UUID,
    p_cascade       BOOLEAN DEFAULT FALSE
) RETURNS VOID AS $$
BEGIN
    IF p_app_id IS NULL THEN
        RETURN;
    END IF;

    INSERT INTO claimius.access_diff (id, app_id, kind, object_type, object_id, cascade)
    VALUES (claimius.composite_id('object', p_app_id::TEXT, p_object_type, p_object_id::TEXT),
            p_app_id, 'object', p_object_type, p_object_id, p_cascade)
    ON CONFLICT (id) DO UPDATE
        SET sa_updated_at = now(),
            raise_count = claimius.access_diff.raise_count + 1,
            cascade = claimius.access_diff.cascade OR EXCLUDED.cascade;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._raise_object_diff(UUID, TEXT, UUID, BOOLEAN) IS 'Marks an object area outstanding in access_diff.';

-- ----------------------------------------------------------------------------
-- _raise_claim_diff
-- Marks the area around one claim as needing recalculation, for every holder,
-- or with p_user_id for the one user an assignment of the claim names.
-- ----------------------------------------------------------------------------
DROP FUNCTION IF EXISTS claimius._raise_claim_diff(UUID, UUID);
CREATE OR REPLACE FUNCTION claimius._raise_claim_diff(
    p_app_id        UUID,
    p_claim_id      UUID,
    p_user_id       UUID DEFAULT NULL
) RETURNS VOID AS $$
BEGIN
    IF p_app_id IS NULL OR p_claim_id IS NULL THEN
        RETURN;
    END IF;

    INSERT INTO claimius.access_diff (id, app_id, kind, claim_id, user_id)
    VALUES (CASE WHEN p_user_id IS NULL
                     THEN claimius.composite_id('claim', p_app_id::TEXT, p_claim_id::TEXT)
                     ELSE claimius.composite_id('claim', p_app_id::TEXT, p_claim_id::TEXT, p_user_id::TEXT)
                END,
            p_app_id, 'claim', p_claim_id, p_user_id)
    ON CONFLICT (id) DO UPDATE
        SET sa_updated_at = now(),
            raise_count = claimius.access_diff.raise_count + 1;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._raise_claim_diff(UUID, UUID, UUID) IS 'Marks a claim area outstanding in access_diff, for every holder or for the one assigned user.';

-- ----------------------------------------------------------------------------
-- _settle_object_diff
-- Settles one user's share of an object area, set based: the objects of the
-- area, the object and with cascade its subtree, on which the user holds a
-- pair or which a binding of the user's own claims reaches, plus the object
-- itself so its creator is settled. The per object locks of the whole share
-- are taken once, sorted, before the first write, then one recompute per
-- object type. Returns whether the user's pair set changed.
-- ----------------------------------------------------------------------------
DROP FUNCTION IF EXISTS claimius._settle_object_diff(UUID, TEXT, UUID, BOOLEAN);
CREATE OR REPLACE FUNCTION claimius._settle_object_diff(
    p_app_id        UUID,
    p_user_id       UUID,
    p_object_type   TEXT,
    p_object_id     UUID,
    p_cascade       BOOLEAN DEFAULT FALSE
) RETURNS BOOLEAN AS $$
DECLARE
    v_share     RECORD;
    v_types     TEXT[];
    v_ids       UUID[];
    v_changed   BOOLEAN := FALSE;
BEGIN
    WITH area AS (
        SELECT p_object_type AS object_type, p_object_id AS object_id
        UNION
        SELECT e.descendant_type, e.descendant_id
        FROM claimius.inheritance_info e
        WHERE p_cascade
          AND e.ancestor_type = p_object_type
          AND e.ancestor_id = p_object_id
          AND e.depth > 0
    ),
    bound AS MATERIALIZED (
        SELECT co.object_type, co.object_id, (co.inherits AND c.inherits) AS inherits
        FROM claimius.user_claim uc
            JOIN claimius.claim c ON c.id = uc.claim_id AND c.sa_deleted_at IS NULL
            JOIN claimius.claim_object co
                ON co.claim_id = c.id
               AND co.app_id = p_app_id
               AND co.sa_deleted_at IS NULL
               AND co.object_id IS NOT NULL
        WHERE uc.user_id = p_user_id
          AND uc.app_id = p_app_id
          AND uc.sa_deleted_at IS NULL

        UNION ALL

        SELECT co.object_type, co.object_id, (co.inherits AND cb.inherits)
        FROM claimius.user_claim uc
            JOIN claimius.claim_object link
                ON link.claim_id = uc.claim_id
               AND link.app_id = p_app_id
               AND link.object_type = 'claimius.claim'
               AND link.sa_deleted_at IS NULL
            JOIN claimius.claim_object co
                ON co.claim_id = link.object_id
               AND co.app_id = p_app_id
               AND co.sa_deleted_at IS NULL
               AND co.object_id IS NOT NULL
            JOIN claimius.claim cb ON cb.id = co.claim_id AND cb.sa_deleted_at IS NULL
        WHERE uc.user_id = p_user_id
          AND uc.app_id = p_app_id
          AND uc.sa_deleted_at IS NULL
    )
    SELECT array_agg(a.object_type), array_agg(a.object_id)
    INTO v_types, v_ids
    FROM area a
    WHERE (a.object_type = p_object_type AND a.object_id = p_object_id)
       OR EXISTS (
            SELECT 1 FROM claimius.user_object uo
            WHERE uo.user_id = p_user_id AND uo.app_id = p_app_id AND uo.object_type = a.object_type AND uo.object_id = a.object_id)
       OR EXISTS (
            SELECT 1 FROM bound b
            WHERE b.object_type = a.object_type AND b.object_id = a.object_id)
       OR EXISTS (
            SELECT 1
            FROM claimius.inheritance_info e
                JOIN bound b
                    ON b.inherits
                   AND b.object_type = e.ancestor_type
                   AND b.object_id = e.ancestor_id
            WHERE e.descendant_type = a.object_type
              AND e.descendant_id = a.object_id
              AND e.depth > 0);

    PERFORM claimius._lock_objects(ARRAY(SELECT s.object_type || ':' || s.object_id::TEXT FROM unnest(v_types, v_ids) AS s(object_type, object_id)));

    FOR v_share IN
        SELECT s.object_type, array_agg(s.object_id) AS object_ids
        FROM unnest(v_types, v_ids) AS s(object_type, object_id)
        GROUP BY s.object_type
    LOOP
        v_changed := claimius.recompute_user_object(p_app_id, p_user_id, v_share.object_type, VARIADIC v_share.object_ids) OR v_changed;
    END LOOP;

    RETURN v_changed;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._settle_object_diff(UUID, UUID, TEXT, UUID, BOOLEAN) IS 'Recomputes one user''s share of an object area, set based. Returns whether the user''s pair set changed.';

-- ----------------------------------------------------------------------------
-- _settle_claim_diff
-- Settles one user's share of a claim area, set based: the claim itself; the
-- objects the claim is bound on and, where the binding inherits, their
-- subtrees; the objects bound by the claims it links to and, where that
-- binding cascades, their subtrees; and the user's pairs whose grants name the
-- claim or were composed through it. Bindings and links removed or changed since the area opened, at
-- p_since, count too, so the pairs they used to reach are settled away. The
-- per object locks of the whole share are taken once, sorted, before the
-- first write, then one recompute per object type. Returns whether the
-- user's pair set changed.
-- ----------------------------------------------------------------------------
DROP FUNCTION IF EXISTS claimius._settle_claim_diff(UUID, UUID);
DROP FUNCTION IF EXISTS claimius._settle_claim_diff_for_user(UUID, UUID, UUID);
CREATE OR REPLACE FUNCTION claimius._settle_claim_diff(
    p_app_id        UUID,
    p_user_id       UUID,
    p_claim_id      UUID,
    p_since         TIMESTAMPTZ
) RETURNS BOOLEAN AS $$
DECLARE
    v_share     RECORD;
    v_types     TEXT[];
    v_ids       UUID[];
    v_changed   BOOLEAN := FALSE;
BEGIN
    WITH bound AS MATERIALIZED (
        SELECT co.object_type, co.object_id,
               (co.inherits OR co.sa_updated_at >= p_since) AND (c.inherits OR c.sa_updated_at >= p_since) AS cascades
        FROM claimius.claim_object co
            JOIN claimius.claim c ON c.id = co.claim_id
        WHERE co.claim_id = p_claim_id
          AND co.app_id = p_app_id
          AND co.object_id IS NOT NULL
          AND (co.sa_deleted_at IS NULL OR co.sa_updated_at >= p_since)

        UNION ALL

        SELECT co.object_type, co.object_id,
               (co.inherits OR co.sa_updated_at >= p_since) AND (cb.inherits OR cb.sa_updated_at >= p_since)
        FROM claimius.claim_object link
            JOIN claimius.claim_object co
                ON co.claim_id = link.object_id
               AND co.app_id = p_app_id
               AND co.object_id IS NOT NULL
               AND (co.sa_deleted_at IS NULL OR co.sa_updated_at >= p_since)
            JOIN claimius.claim cb ON cb.id = co.claim_id
        WHERE link.claim_id = p_claim_id
          AND link.app_id = p_app_id
          AND link.object_type = 'claimius.claim'
          AND (link.sa_deleted_at IS NULL OR link.sa_updated_at >= p_since)
    ),
    share AS (
        SELECT 'claimius.claim'::TEXT AS object_type, p_claim_id AS object_id

        UNION

        SELECT b.object_type, b.object_id FROM bound b

        UNION

        SELECT e.descendant_type, e.descendant_id
        FROM bound b
            JOIN claimius.inheritance_info e
                ON e.ancestor_type = b.object_type
               AND e.ancestor_id = b.object_id
               AND e.depth > 0
        WHERE b.cascades

        UNION

        SELECT uo.object_type, uo.object_id
        FROM claimius.user_object uo
        WHERE uo.app_id = p_app_id
          AND uo.user_id = p_user_id
          AND (uo.grants @> jsonb_build_array(jsonb_build_object('claim_id', p_claim_id))
               OR uo.grants @> jsonb_build_array(jsonb_build_object(
                      'composed_from', p_claim_id)))
    )
    SELECT array_agg(s.object_type), array_agg(s.object_id)
    INTO v_types, v_ids
    FROM share s;

    PERFORM claimius._lock_objects(ARRAY(SELECT s.object_type || ':' || s.object_id::TEXT FROM unnest(v_types, v_ids) AS s(object_type, object_id)));

    FOR v_share IN
        SELECT s.object_type, array_agg(s.object_id) AS object_ids
        FROM unnest(v_types, v_ids) AS s(object_type, object_id)
        GROUP BY s.object_type
    LOOP
        v_changed := claimius.recompute_user_object(p_app_id, p_user_id, v_share.object_type, VARIADIC v_share.object_ids) OR v_changed;
    END LOOP;

    RETURN v_changed;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._settle_claim_diff(UUID, UUID, UUID, TIMESTAMPTZ) IS 'Recomputes one user''s share of a claim area, set based. Returns whether the user''s pair set changed.';

-- ----------------------------------------------------------------------------
-- _diff_holders
-- The users an area can reach, the ones whose share of it is theirs to settle.
-- The holders of a claim area are the users holding the claim or a claim
-- linking to it, or the one user an assignment names. The holders of an
-- object area are the users holding a pair on the object or, with cascade, on
-- its subtree, the users holding a claim bound on the object or an ancestor,
-- and the creator of the object. A user can come more than once.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius._diff_holders(
    p_app_id        UUID,
    p_diff_id       UUID
) RETURNS TABLE (user_id UUID) AS $$
    SELECT d.user_id
    FROM claimius.access_diff d
    WHERE d.id = p_diff_id AND d.kind = 'claim' AND d.user_id IS NOT NULL

    UNION ALL

    SELECT uc.user_id
    FROM claimius.access_diff d
        JOIN claimius.user_claim uc
            ON uc.claim_id = d.claim_id
           AND uc.app_id = p_app_id
           AND uc.sa_deleted_at IS NULL
    WHERE d.id = p_diff_id AND d.kind = 'claim' AND d.user_id IS NULL

    UNION ALL

    SELECT uc.user_id
    FROM claimius.access_diff d
        JOIN claimius.claim_object link
            ON link.object_type = 'claimius.claim'
           AND link.object_id = d.claim_id
           AND link.app_id = p_app_id
           AND link.sa_deleted_at IS NULL
        JOIN claimius.user_claim uc
            ON uc.claim_id = link.claim_id
           AND uc.app_id = p_app_id
           AND uc.sa_deleted_at IS NULL
    WHERE d.id = p_diff_id AND d.kind = 'claim' AND d.user_id IS NULL

    UNION ALL

    SELECT uc.user_id
    FROM claimius.access_diff d
        CROSS JOIN LATERAL claimius._denormalize_objects(d.object_type, ARRAY[d.object_id]) r
        JOIN claimius.user_claim uc ON uc.id = r.sa_created_by AND uc.app_id = p_app_id AND uc.sa_deleted_at IS NULL
    WHERE d.id = p_diff_id AND d.kind = 'object'

    UNION ALL

    SELECT uo.user_id
    FROM claimius.access_diff d
        JOIN claimius.user_object uo
            ON uo.app_id = p_app_id
           AND uo.object_type = d.object_type
           AND uo.object_id = d.object_id
    WHERE d.id = p_diff_id AND d.kind = 'object'

    UNION ALL

    SELECT uo.user_id
    FROM claimius.access_diff d
        JOIN claimius.inheritance_info e
            ON e.ancestor_type = d.object_type
           AND e.ancestor_id = d.object_id
           AND e.depth > 0
        JOIN claimius.user_object uo
            ON uo.app_id = p_app_id
           AND uo.object_type = e.descendant_type
           AND uo.object_id = e.descendant_id
    WHERE d.id = p_diff_id AND d.kind = 'object' AND d.cascade

    UNION ALL

    SELECT uc.user_id
    FROM claimius.access_diff d
        CROSS JOIN LATERAL (
            SELECT d.object_type AS object_type, d.object_id AS object_id
            UNION ALL
            SELECT e.ancestor_type, e.ancestor_id
            FROM claimius.inheritance_info e
            WHERE e.descendant_type = d.object_type
              AND e.descendant_id = d.object_id
              AND e.depth > 0
        ) n
        JOIN claimius.claim_object co
            ON co.object_type = n.object_type
           AND co.object_id = n.object_id
           AND co.app_id = p_app_id
           AND co.sa_deleted_at IS NULL
        JOIN claimius.user_claim uc
            ON uc.claim_id = co.claim_id
           AND uc.app_id = p_app_id
           AND uc.sa_deleted_at IS NULL
    WHERE d.id = p_diff_id AND d.kind = 'object'
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius._diff_holders(UUID, UUID) IS 'The users an access_diff area can reach, with repeats: the holders of a claim area, or the pair holders, binding holders and creator of an object area.';

-- ----------------------------------------------------------------------------
-- _clear_diff
-- Deletes an area once every user it can reach has settled its current
-- raise_count. The first holder found unsettled ends the test, and an area
-- another transaction holds is left for the next settle to clear.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius._clear_diff(
    p_app_id        UUID,
    p_diff_id       UUID
) RETURNS BOOLEAN AS $$
DECLARE
    v_diff      claimius.access_diff%ROWTYPE;
BEGIN
    SELECT d.* INTO v_diff FROM claimius.access_diff d WHERE d.id = p_diff_id;
    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;

    IF EXISTS (
        SELECT 1 FROM claimius._diff_holders(p_app_id, v_diff.id) h
        WHERE NOT EXISTS (
            SELECT 1 FROM claimius.access_diff_user du
            WHERE du.diff_id = v_diff.id AND du.user_id = h.user_id AND du.raise_count = v_diff.raise_count)
    ) THEN
        RETURN FALSE;
    END IF;

    DELETE FROM claimius.access_diff
    WHERE id IN (
        SELECT d.id FROM claimius.access_diff d
        WHERE d.id = v_diff.id AND d.raise_count = v_diff.raise_count
        FOR UPDATE SKIP LOCKED
    );
    RETURN FOUND;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._clear_diff(UUID, UUID) IS 'Deletes an access_diff area once every holder has settled its current raise_count. Stops at the first unsettled holder and skips an area another transaction holds.';

-- ----------------------------------------------------------------------------
-- _settle_diff_share
-- Settles one user's share of one area and records it: the object or claim
-- settle, set based, then the done record at the area's raise_count, then the
-- clear. The area is taken FOR KEY SHARE SKIP LOCKED, which a writer raising
-- it again does not wait on, and the share under a transaction advisory lock,
-- so two settles of one share never run at once. A share already settled at
-- the current raise, an area gone or held, and a settle that fails are left
-- as they are and answer FALSE.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius._settle_diff_share(
    p_app_id        UUID,
    p_user_id       UUID,
    p_diff_id       UUID
) RETURNS BOOLEAN AS $$
DECLARE
    v_diff      claimius.access_diff%ROWTYPE;
BEGIN
    SELECT d.* INTO v_diff
    FROM claimius.access_diff d
    WHERE d.id = p_diff_id
      AND d.app_id = p_app_id
      AND NOT EXISTS (
          SELECT 1 FROM claimius.access_diff_user du
          WHERE du.diff_id = d.id AND du.user_id = p_user_id AND du.raise_count = d.raise_count)
    FOR KEY SHARE SKIP LOCKED;
    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;

    IF NOT pg_try_advisory_xact_lock(hashtextextended(v_diff.id::TEXT || ':' || p_user_id::TEXT, 0)) THEN
        RETURN FALSE;
    END IF;

    BEGIN
        IF v_diff.kind = 'object' THEN
            PERFORM claimius._settle_object_diff(
                    p_app_id, p_user_id, v_diff.object_type, v_diff.object_id, v_diff.cascade);
        ELSE
            PERFORM claimius._settle_claim_diff(p_app_id, p_user_id, v_diff.claim_id, v_diff.sa_created_at);
        END IF;

        INSERT INTO claimius.access_diff_user (diff_id, app_id, user_id, raise_count)
        VALUES (v_diff.id, p_app_id, p_user_id, v_diff.raise_count)
        ON CONFLICT (diff_id, user_id) DO UPDATE
            SET raise_count = EXCLUDED.raise_count,
                sa_created_at = now();

        PERFORM claimius._clear_diff(p_app_id, v_diff.id);
    EXCEPTION WHEN OTHERS THEN
        RETURN FALSE;
    END;
    RETURN TRUE;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._settle_diff_share(UUID, UUID, UUID) IS 'Settles and records one user''s share of one access_diff area, then clears the area when no holder is left. FALSE when the share was settled already, the area is gone or held, or the settle failed.';
