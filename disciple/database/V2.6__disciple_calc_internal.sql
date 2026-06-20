-- ============================================================================
-- Claimius V2.6 Disciple Calc Internals (app_id free)
-- ----------------------------------------------------------------------------
-- The app_id free calc engine for the disciple. In production the calc
-- triggers are dormant: sync applies relation tables under replay_mode and the
-- calc functions short circuit on in_replay_mode(). In debug mode (no
-- replay_mode) local writes fire the calc path and materialize user_object so
-- a standalone disciple reads correctly. Authored app_id free throughout.
-- ============================================================================
-- build_ownership_tree
-- Rewrites the ownership tree closure rooted at p_root_id from live data.
-- Includes org-org edges plus edges from every registered table whose rows
-- point at any org in the tree via sa_owner_id.
CREATE OR REPLACE FUNCTION claimius.build_ownership_tree(p_root_id UUID)
    RETURNS VOID AS $$
BEGIN
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
