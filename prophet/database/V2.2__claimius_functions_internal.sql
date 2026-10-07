DROP FUNCTION IF EXISTS claimius.build_ownership_tree(UUID);
CREATE OR REPLACE FUNCTION claimius.build_ownership_tree(p_root_id UUID, p_node_id UUID DEFAULT NULL)
    RETURNS VOID AS $$
DECLARE
    v_start  UUID := coalesce(p_node_id, p_root_id);
    v_prefix UUID[] := '{}';
BEGIN
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

DROP FUNCTION IF EXISTS claimius._splice_subtree(claimius.tree_type, TEXT, UUID, UUID, UUID);
CREATE OR REPLACE FUNCTION claimius._splice_subtree(
    p_tree_type claimius.tree_type,
    p_node_type TEXT,
    p_node_id   UUID,
    p_root_id   UUID,
    p_parent_id UUID
) RETURNS TABLE (object_type TEXT, object_id UUID) AS $$
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
        WITH s AS MATERIALIZED (
            SELECT s.descendant_type, s.descendant_id
            FROM claimius.inheritance_info s
            WHERE s.tree_type = p_tree_type
              AND s.ancestor_type = p_node_type
              AND s.ancestor_id = p_node_id
        ),
        a AS MATERIALIZED (
            SELECT a.ancestor_type, a.ancestor_id
            FROM claimius.inheritance_info a
            WHERE a.tree_type = p_tree_type
              AND a.descendant_type = p_node_type
              AND a.descendant_id = p_node_id
              AND a.depth > 0
        )
        DELETE FROM claimius.inheritance_info d
            USING s, a
        WHERE d.tree_type = p_tree_type
          AND d.ancestor_type = a.ancestor_type
          AND d.ancestor_id = a.ancestor_id
          AND d.descendant_type = s.descendant_type
          AND d.descendant_id = s.descendant_id;

        WITH s AS MATERIALIZED (
            SELECT s.descendant_type, s.descendant_id, s.depth
            FROM claimius.inheritance_info s
            WHERE s.tree_type = p_tree_type
              AND s.ancestor_type = p_node_type
              AND s.ancestor_id = p_node_id
        ),
        a AS MATERIALIZED (
            SELECT a.ancestor_type, a.ancestor_id, a.depth
            FROM claimius.inheritance_info a
            WHERE a.tree_type = p_tree_type
              AND a.descendant_type = p_node_type
              AND a.descendant_id = p_parent_id
        )
        INSERT INTO claimius.inheritance_info (
            tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
        )
        SELECT p_tree_type, p_root_id, a.ancestor_type, a.ancestor_id, s.descendant_type, s.descendant_id, a.depth + s.depth + 1
        FROM a
            CROSS JOIN s;
        RETURN;
    END IF;

    RETURN QUERY
    WITH s AS MATERIALIZED (
        SELECT s.descendant_type, s.descendant_id
        FROM claimius.inheritance_info s
        WHERE s.tree_type = p_tree_type
          AND s.ancestor_type = p_node_type
          AND s.ancestor_id = p_node_id
    ),
    dropped AS (
        DELETE FROM claimius.inheritance_info d
            USING s
        WHERE d.tree_type = p_tree_type
          AND d.descendant_type = s.descendant_type
          AND d.descendant_id = s.descendant_id
        RETURNING d.descendant_type, d.descendant_id, d.depth
    )
    SELECT DISTINCT x.descendant_type, x.descendant_id
    FROM dropped x
    WHERE x.depth = 0
      AND (x.descendant_type, x.descendant_id) IS DISTINCT FROM (p_node_type, p_node_id);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._splice_subtree(claimius.tree_type, TEXT, UUID, UUID, UUID) IS 'Moves the stored subtree of a node under a new parent, or drops it when the parent is absent and returns the descendants whose self edges it dropped, so the caller raises an area on each.';

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

COMMENT ON FUNCTION claimius._select_owned_rows(TEXT, UUID) IS 'Returns id and sa_owner_id of the live rows of an object type whose owner is in the tree of p_root_id. Dynamic SQL over the registered table.';

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

COMMENT ON FUNCTION claimius._select_located_rows(TEXT, UUID) IS 'Returns id and sa_location_id of the live rows of an object type whose location is in the tree of p_root_id. Dynamic SQL over the registered table.';

CREATE OR REPLACE FUNCTION claimius.app_object_ids(p_app_id UUID, p_object_type TEXT)
    RETURNS SETOF UUID AS $$
    SELECT ao.object_id
    FROM claimius.app_object ao
    WHERE ao.app_id = p_app_id
      AND ao.object_type = p_object_type
      AND ao.sa_deleted_at IS NULL;
$$ LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.app_object_ids(UUID, TEXT) IS 'Object ids of one type linked to an app through app_object.';

CREATE OR REPLACE FUNCTION claimius.apps_for_object(p_object_type TEXT, p_object_id UUID)
    RETURNS SETOF UUID AS $$
    SELECT ao.app_id
    FROM claimius.app_object ao
    WHERE ao.object_type = p_object_type
      AND ao.object_id = p_object_id
      AND ao.sa_deleted_at IS NULL;
$$ LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.apps_for_object(TEXT, UUID) IS 'App ids holding a live app_object link to one object.';

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

DROP FUNCTION IF EXISTS claimius.link_app_object(uuid,text,uuid,uuid);
CREATE OR REPLACE FUNCTION claimius.upsert_app_object(
    p_app_id      UUID,
    p_object_type TEXT,
    p_object_id   UUID,
    p_created_by  UUID DEFAULT NULL
) RETURNS UUID AS $$
DECLARE
    v_id      UUID;
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
        PERFORM claimius._raise_object_diff(p_app_id, p_object_type, p_object_id);
    END IF;

    RETURN v_id;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_app_object(UUID, TEXT, UUID, UUID) IS 'Creates a live app_object link and raises the object area in the app. Idempotent.';

DROP FUNCTION IF EXISTS claimius.unlink_app_object(uuid,text,uuid);
CREATE OR REPLACE FUNCTION claimius.delete_app_object(
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

COMMENT ON FUNCTION claimius.delete_app_object(UUID, TEXT, UUID) IS 'Soft deletes an app_object link and purges the app''s materialized access on the object.';

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
        IF v_has_parent <> v_has_root THEN
            RAISE EXCEPTION 'Table % must have both sa_parent_id and sa_root_id, or neither', p_object_type;
        END IF;

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

    IF NOT v_has_root THEN
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I.%I', v_trigger_name, v_schema, v_table);
        RETURN;
    END IF;

    IF v_has_parent THEN
        v_func_name := 'claimius._set_self_parent_on_insert';
    ELSIF v_has_owner THEN
        v_func_name := 'claimius._set_self_owner_on_insert';
    ELSE
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

CREATE OR REPLACE FUNCTION claimius._user_id_for_user_claim(p_user_claim_id UUID)
    RETURNS TABLE(user_id UUID, app_id UUID) AS $$
BEGIN
    RETURN QUERY
        SELECT uc.user_id, uc.app_id
        FROM claimius.user_claim uc
        WHERE uc.id = p_user_claim_id AND uc.sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._user_id_for_user_claim(UUID) IS 'The user_id and app_id behind a user_claim.id, which is what sa_created_by holds. No row before the grant exists.';

DROP FUNCTION IF EXISTS claimius._best_grant(JSONB, UUID, UUID, INTEGER);
CREATE OR REPLACE FUNCTION claimius._best_grant(
    p_grants          JSONB,
    p_required_access INTEGER
) RETURNS TABLE (
                    user_claim_id    UUID,
                    claim_id         UUID,
                    claim_name       TEXT,
                    access_bits      INTEGER,
                    grant_scope      JSONB,
                    claim_created_at TIMESTAMPTZ,
                    grant_level      INTEGER
                ) LANGUAGE sql STABLE AS $$
    SELECT
        (g ->> 'user_claim_id')::UUID,
        (g ->> 'claim_id')::UUID,
        g ->> 'claim_name',
        (g ->> 'access')::INTEGER,
        g -> 'scope',
        (g ->> 'claim_created_at')::TIMESTAMPTZ,
        coalesce((g ->> 'level')::INTEGER, 0)
    FROM jsonb_array_elements(p_grants) g
    WHERE ((g ->> 'access')::INTEGER & p_required_access) = p_required_access
      AND (g ->> 'user_claim_id') IS NOT NULL
      AND ((g ->> 'starts_at') IS NULL OR (g ->> 'starts_at')::TIMESTAMPTZ <= now())
      AND ((g ->> 'ends_at') IS NULL OR (g ->> 'ends_at')::TIMESTAMPTZ > now())
    ORDER BY claimius._popcount((g ->> 'access')::INTEGER) DESC, (g ->> 'access')::INTEGER DESC,
             coalesce((g ->> 'level')::INTEGER, 0) DESC, (g ->> 'claim_created_at')::TIMESTAMPTZ ASC
    LIMIT 1
$$;

COMMENT ON FUNCTION claimius._best_grant(JSONB, INTEGER) IS 'Strongest derivation in a user_object grants array whose mask covers p_required_access and whose stored window holds at now(). Reads nothing: the grant id, its window, the claim name and its creation time are stored on the derivation, and every write that can change them raises an area covering the pair. Shared by get_access, get_objects and search so every read path resolves the actor token identically.';

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

COMMENT ON FUNCTION claimius._user_claim_grants(UUID, UUID, UUID) IS 'The masks the actor holds on the samna_user rows of a target, or of every user without one, through the actor''s live grants bound on claimius.samna_user, each eff(c, b) & 15 of a non deny binding inside the grant''s window. The third branch of get_user_access and get_users.';

DROP FUNCTION IF EXISTS claimius._apply_denies(JSONB, JSONB, TEXT, UUID);
CREATE OR REPLACE FUNCTION claimius._apply_denies(
    p_grants        JSONB,
    p_denies        JSONB,
    p_created       BOOLEAN,
    p_fields        TEXT[]
) RETURNS JSONB AS $$
DECLARE
    v_field         JSONB;
    v_level         INTEGER;
    v_deny_level    INTEGER;
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

    FOR v_grant IN
        SELECT g FROM jsonb_array_elements(p_grants) g
        UNION ALL
        SELECT jsonb_build_object('access', 15, 'tree_type', 'direct', 'creator', TRUE) WHERE p_created
    LOOP
        v_bits := (v_grant ->> 'access')::INTEGER;
        v_inherited := v_grant ->> 'tree_type' <> 'direct';
        v_level := coalesce((v_grant ->> 'level')::INTEGER, 0);
        v_denied := 0;
        v_field := '{}'::JSONB;
        FOR v_deny IN SELECT * FROM jsonb_array_elements(coalesce(p_denies, '[]'::JSONB)) LOOP
            v_deny_level := coalesce((v_deny ->> 'level')::INTEGER, 0);
            CONTINUE WHEN v_deny_level < v_level
                OR (v_deny_level = v_level AND v_deny ->> 'tree_type' <> 'direct' AND NOT v_inherited);
            IF jsonb_typeof(v_deny -> 'scope') = 'object' THEN
                FOR v_key IN SELECT jsonb_object_keys(v_deny -> 'scope') LOOP
                    CONTINUE WHEN NOT v_key = ANY(p_fields);
                    v_field := v_field || jsonb_build_object(v_key, coalesce((v_field ->> v_key)::INTEGER, 0) | (v_deny ->> 'access')::INTEGER);
                END LOOP;
            ELSE
                v_denied := v_denied | (v_deny ->> 'access')::INTEGER;
            END IF;
        END LOOP;
        v_map := NULL;

        IF jsonb_typeof(v_grant -> 'scope') = 'object' THEN
            v_map := '{}'::JSONB;
            v_access := 0;
            FOR v_key IN SELECT jsonb_object_keys(v_grant -> 'scope') LOOP
                CONTINUE WHEN NOT v_key = ANY(p_fields);
                v_value := claimius._close_access(v_bits & ~(v_denied | coalesce((v_field ->> v_key)::INTEGER, 0)));
                v_map := v_map || jsonb_build_object(v_key, v_value);
                v_access := v_access | v_value;
            END LOOP;
        ELSE
            v_star := claimius._close_access(v_bits & ~v_denied);
            FOR v_key IN SELECT jsonb_object_keys(v_field) LOOP
                v_value := claimius._close_access(v_bits & ~(v_denied | coalesce((v_field ->> v_key)::INTEGER, 0)));
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

COMMENT ON FUNCTION claimius._apply_denies(JSONB, JSONB, BOOLEAN, TEXT[]) IS 'Settles the derivations of one user on one object: a deny of a higher level subtracts from a grant whatever its class; at the same level a direct deny subtracts from every grant and from the creator, another deny from inherited grants, a scoped deny from its fields only; each remainder is closed under implication and a grant left empty is dropped. Returns grants, the creator mask, the mask over all grants and the merged field scope.';

DROP FUNCTION IF EXISTS claimius._build_grants_for_object(UUID, UUID, TEXT, UUID);
DROP FUNCTION IF EXISTS claimius._build_grants_for_objects(UUID, UUID, TEXT, UUID[]);
DROP FUNCTION IF EXISTS claimius._build_grants_for_objects(UUID, UUID, TEXT, UUID[], UUID[]);
CREATE OR REPLACE FUNCTION claimius._build_grants_for_objects(
    p_user_id       UUID,
    p_app_id        UUID,
    p_object_types  TEXT[],
    p_object_ids    UUID[],
    p_cascades      BOOLEAN[],
    p_point         BOOLEAN DEFAULT FALSE
) RETURNS TABLE (object_type TEXT, object_id UUID, live BOOLEAN, grants JSONB, direct_access INTEGER, sa_access INTEGER, scope JSONB) LANGUAGE sql STABLE AS $$
    WITH roots AS MATERIALIZED (
        SELECT r.object_type, r.object_id, bool_or(coalesce(r.cascade, FALSE)) AS cascade
        FROM unnest(p_object_types, p_object_ids, p_cascades) AS r(object_type, object_id, cascade)
        WHERE r.object_type IS NOT NULL AND r.object_id IS NOT NULL
        GROUP BY r.object_type, r.object_id
    ),
    grants_held AS MATERIALIZED (
        SELECT uc.id AS user_claim_id, uc.claim_id, uc.starts_at, uc.ends_at,
               c.sa_access AS claim_access, c.inherits, c.level, c.name AS claim_name, c.sa_created_at AS claim_created_at,
               c.id IS NOT NULL AS claim_live
        FROM claimius.user_claim uc
            LEFT JOIN claimius.claim c ON c.id = uc.claim_id AND c.sa_deleted_at IS NULL
        WHERE uc.user_id = p_user_id
          AND uc.app_id = p_app_id
          AND uc.sa_deleted_at IS NULL
    ),
    held AS MATERIALIZED (
        SELECT g.* FROM grants_held g WHERE g.claim_live
    ),
    linked AS MATERIALIZED (
        SELECT h.user_claim_id, h.claim_id, h.starts_at, h.ends_at, h.claim_access, h.level, h.claim_name, h.claim_created_at,
               link.object_id AS bound_claim_id, link.sa_access AS link_access,
               cb.sa_access AS bound_claim_access, cb.level AS bound_level
        FROM held h
            JOIN claimius.claim_object link
                ON link.claim_id = h.claim_id
               AND link.app_id = p_app_id
               AND link.object_type = 'claimius.claim'
               AND link.sa_deleted_at IS NULL
               AND link.inherits
            JOIN claimius.claim cb ON cb.id = link.object_id AND cb.sa_deleted_at IS NULL AND cb.inherits
    ),
    chains AS MATERIALIZED (
        SELECT r.object_type AS root_type, r.object_id AS root_id, r.cascade,
               e.ancestor_type, e.ancestor_id, e.tree_type, e.depth
        FROM roots r
            JOIN claimius.inheritance_info e
                ON e.descendant_type = r.object_type
               AND e.descendant_id = r.object_id
    ),
    dominated AS MATERIALIZED (
        SELECT c.root_type, c.root_id, c.tree_type
        FROM chains c
            JOIN roots a ON a.cascade AND a.object_type = c.ancestor_type AND a.object_id = c.ancestor_id
        WHERE c.depth > 0 AND c.cascade
    ),
    descents AS MATERIALIZED (
        SELECT r.object_type, r.object_id, x.tree
        FROM roots r
            CROSS JOIN (VALUES ('ownership'::claimius.tree_type), ('location'::claimius.tree_type), ('parenthood'::claimius.tree_type)) AS x(tree)
        WHERE r.cascade
          AND NOT EXISTS (
              SELECT 1 FROM dominated d
              WHERE d.root_type = r.object_type AND d.root_id = r.object_id AND d.tree_type = x.tree)
    ),
    area AS MATERIALIZED (
        SELECT r.object_type, r.object_id, NULL::claimius.tree_type AS via_tree, r.object_type AS root_type, r.object_id AS root_id
        FROM roots r
        UNION ALL
        SELECT e.descendant_type, e.descendant_id, e.tree_type, s.object_type, s.object_id
        FROM descents s
            JOIN claimius.inheritance_info e
                ON e.ancestor_type = s.object_type
               AND e.ancestor_id = s.object_id
               AND e.tree_type = s.tree
               AND e.depth > 0
    ),
    targets AS MATERIALIZED (
        SELECT a.object_type, a.object_id,
               oi.object_id IS NOT NULL AS live,
               oi.sa_created_by, oi.sa_owner_id, oi.sa_location_id,
               ti.has_sa_parent_id AND ti.has_sa_root_id AND a.object_type NOT IN ('claimius.organization', 'claimius.location') AS has_parent_tree,
               oi.object_id IS NOT NULL AND CASE
                   WHEN a.object_type = 'claimius.samna_user' THEN EXISTS (
                        SELECT 1 FROM claimius.samna_user su
                        WHERE su.id = a.object_id
                          AND su.user_id = p_user_id
                          AND su.app_id = p_app_id
                          AND su.sa_deleted_at IS NULL)
                   ELSE oi.sa_created_by = p_user_id
                        OR EXISTS (SELECT 1 FROM grants_held g WHERE g.user_claim_id = oi.sa_created_by)
               END AS made
        FROM (SELECT DISTINCT x.object_type, x.object_id FROM area x) a
            LEFT JOIN claimius.object_info oi ON oi.object_type = a.object_type AND oi.object_id = a.object_id
            LEFT JOIN claimius.table_info ti ON ti.object_type = a.object_type
    ),
    bindings AS MATERIALIZED (
        SELECT co.claim_id, co.object_type, co.object_id, co.sa_access, co.inherits, co.scope
        FROM claimius.claim_object co
        WHERE NOT p_point
          AND co.app_id = p_app_id
          AND co.sa_deleted_at IS NULL
          AND co.object_id IS NOT NULL
          AND co.claim_id IN (SELECT h.claim_id FROM held h UNION SELECT l.bound_claim_id FROM linked l)
        UNION ALL
        SELECT co.claim_id, co.object_type, co.object_id, co.sa_access, co.inherits, co.scope
        FROM (
            SELECT c.ancestor_type AS object_type, c.ancestor_id AS object_id FROM chains c
            UNION
            SELECT t.object_type, t.object_id FROM targets t
        ) n
            JOIN claimius.claim_object co
                ON co.object_type = n.object_type
               AND co.object_id = n.object_id
               AND co.app_id = p_app_id
               AND co.sa_deleted_at IS NULL
        WHERE p_point
    ),
    inside AS MATERIALIZED (
        SELECT b.claim_id, b.sa_access, b.scope, b.object_type, b.object_id,
               e.descendant_type, e.descendant_id, e.tree_type, e.depth
        FROM (
            SELECT DISTINCT b.object_type, b.object_id
            FROM bindings b
                JOIN targets n ON n.object_type = b.object_type AND n.object_id = b.object_id
            WHERE NOT p_point
              AND b.inherits
              AND NOT EXISTS (SELECT 1 FROM roots r WHERE r.cascade AND r.object_type = b.object_type AND r.object_id = b.object_id
                              AND NOT EXISTS (SELECT 1 FROM dominated d WHERE d.root_type = r.object_type AND d.root_id = r.object_id))
        ) n
            JOIN claimius.inheritance_info e
                ON e.ancestor_type = n.object_type
               AND e.ancestor_id = n.object_id
               AND e.depth > 0
            JOIN bindings b ON b.object_type = n.object_type AND b.object_id = n.object_id AND b.inherits
    ),
    pinned AS MATERIALIZED (
        SELECT a.object_type, a.object_id, x.tree
        FROM area a
            JOIN targets t ON t.object_type = a.object_type AND t.object_id = a.object_id
            CROSS JOIN LATERAL (
                SELECT x.tree
                FROM (VALUES ('ownership'::claimius.tree_type), ('location'::claimius.tree_type), ('parenthood'::claimius.tree_type)) AS x(tree)
                WHERE x.tree <> a.via_tree
                  AND CASE x.tree
                          WHEN 'ownership' THEN t.sa_owner_id IS NOT NULL OR a.object_type = 'claimius.organization'
                          WHEN 'location' THEN t.sa_location_id IS NOT NULL OR a.object_type = 'claimius.location'
                          ELSE t.has_parent_tree
                      END
            ) x
        WHERE NOT p_point
          AND a.via_tree IS NOT NULL
    ),
    across AS MATERIALIZED (
        SELECT p.object_type, p.object_id, e.ancestor_type, e.ancestor_id, e.tree_type, e.depth
        FROM pinned p
            JOIN claimius.inheritance_info e
                ON e.descendant_type = p.object_type
               AND e.descendant_id = p.object_id
               AND e.tree_type = p.tree
               AND e.depth > 0
    ),
    reach AS MATERIALIZED (
        SELECT b.claim_id, b.sa_access, b.scope, b.object_type AS from_type, b.object_id AS from_id,
               t.object_type AS target_type, t.object_id AS target_id, 'direct'::TEXT AS tree_type, 0 AS hop
        FROM bindings b
            JOIN targets t ON t.object_type = b.object_type AND t.object_id = b.object_id

        UNION ALL

        SELECT b.claim_id, b.sa_access, b.scope, b.object_type, b.object_id,
               a.object_type, a.object_id, a.via_tree::TEXT, 1
        FROM bindings b
            JOIN area a ON a.root_type = b.object_type AND a.root_id = b.object_id AND a.via_tree IS NOT NULL
        WHERE b.inherits

        UNION ALL

        SELECT b.claim_id, b.sa_access, b.scope, b.object_type, b.object_id,
               a.object_type, a.object_id, c.tree_type::TEXT, c.depth
        FROM chains c
            JOIN bindings b ON b.object_type = c.ancestor_type AND b.object_id = c.ancestor_id AND b.inherits
            JOIN area a ON a.root_type = c.root_type AND a.root_id = c.root_id AND (a.via_tree IS NULL OR a.via_tree = c.tree_type)
        WHERE c.depth > 0

        UNION ALL

        SELECT i.claim_id, i.sa_access, i.scope, i.object_type, i.object_id,
               i.descendant_type, i.descendant_id, i.tree_type::TEXT, i.depth
        FROM inside i
            JOIN targets t ON t.object_type = i.descendant_type AND t.object_id = i.descendant_id

        UNION ALL

        SELECT b.claim_id, b.sa_access, b.scope, b.object_type, b.object_id,
               x.object_type, x.object_id, x.tree_type::TEXT, x.depth
        FROM across x
            JOIN bindings b ON b.object_type = x.ancestor_type AND b.object_id = x.ancestor_id AND b.inherits
    ),
    derivations AS (
        SELECT r.target_type AS object_id_type, r.target_id AS object_id,
               h.user_claim_id, h.claim_id, r.from_type, r.from_id, NULL::UUID AS composed_from, r.tree_type, r.hop,
               CASE WHEN r.target_type = 'claimius.claim' AND r.tree_type = 'direct' THEN 4
                    ELSE claimius._effective_access(h.claim_access, r.sa_access) END AS sa_access,
               r.scope, h.level, h.claim_name, h.claim_created_at, h.starts_at, h.ends_at
        FROM reach r
            JOIN held h ON h.claim_id = r.claim_id
        WHERE (r.tree_type = 'direct' OR h.inherits)
          AND (r.target_type <> 'claimius.claim' OR r.tree_type <> 'direct'
               OR (claimius._effective_access(h.claim_access, r.sa_access) & 16) = 0)

        UNION ALL

        SELECT t.object_type, t.object_id,
               h.user_claim_id, h.claim_id, t.object_type, t.object_id, NULL::UUID, 'direct', 0,
               4, NULL::JSONB, h.level, h.claim_name, h.claim_created_at, h.starts_at, h.ends_at
        FROM targets t
            JOIN held h ON h.claim_id = t.object_id
        WHERE t.object_type = 'claimius.claim'

        UNION ALL

        SELECT r.target_type, r.target_id,
               l.user_claim_id, l.claim_id, r.from_type, r.from_id, l.bound_claim_id, r.tree_type, r.hop,
               CASE WHEN l.level > l.bound_level THEN claimius._effective_access(l.claim_access, l.link_access)
                    WHEN l.bound_level > l.level THEN claimius._effective_access(l.bound_claim_access, r.sa_access)
                    ELSE (claimius._effective_access(l.claim_access, l.link_access)
                            & claimius._effective_access(l.bound_claim_access, r.sa_access) & ~16)
                         | ((claimius._effective_access(l.claim_access, l.link_access)
                            | claimius._effective_access(l.bound_claim_access, r.sa_access)) & 16) END,
               r.scope, greatest(l.level, l.bound_level), l.claim_name, l.claim_created_at, l.starts_at, l.ends_at
        FROM reach r
            JOIN linked l ON l.bound_claim_id = r.claim_id
        WHERE r.target_type <> 'claimius.claim'
    ),
    entries AS (
        SELECT DISTINCT ON (d.object_id_type, d.object_id, d.claim_id, d.composed_from, d.from_type, d.from_id, d.tree_type)
               d.object_id_type AS object_type, d.object_id,
               (d.sa_access & 16) <> 0 AS is_deny,
               d.hop, d.tree_type, d.from_type, d.from_id, d.claim_id,
               jsonb_strip_nulls(jsonb_build_object(
                   'claim_id', d.claim_id,
                   'user_claim_id', d.user_claim_id,
                   'claim_name', d.claim_name,
                   'claim_created_at', d.claim_created_at,
                   'starts_at', d.starts_at,
                   'ends_at', d.ends_at,
                   'access', d.sa_access & CASE WHEN (d.sa_access & 16) <> 0 THEN 15 ELSE ~16 END,
                   'cascaded_from', jsonb_build_object('type', d.from_type, 'id', d.from_id),
                   'composed_from', d.composed_from,
                   'tree_type', d.tree_type,
                   'level', NULLIF(d.level, 0)
               )) || jsonb_build_object('scope', d.scope) AS entry
        FROM derivations d
        ORDER BY d.object_id_type, d.object_id, d.claim_id, d.composed_from, d.from_type, d.from_id, d.tree_type, d.hop
    ),
    grouped AS (
        SELECT x.object_type, x.object_id,
               bool_or(x.live) AS live,
               bool_or(x.made) AS made,
               coalesce(jsonb_agg(x.entry ORDER BY x.hop, x.tree_type, x.from_type, x.from_id, x.claim_id) FILTER (WHERE x.entry IS NOT NULL AND NOT x.is_deny), '[]'::JSONB) AS grants,
               coalesce(jsonb_agg(x.entry ORDER BY x.hop, x.tree_type, x.from_type, x.from_id, x.claim_id) FILTER (WHERE x.entry IS NOT NULL AND x.is_deny), '[]'::JSONB) AS denies,
               bool_or(x.is_deny) AS has_deny,
               bool_or(jsonb_typeof(x.entry -> 'scope') = 'object') AS has_scope,
               coalesce(bit_or((x.entry ->> 'access')::INTEGER) FILTER (WHERE x.entry IS NOT NULL AND NOT x.is_deny), 0) AS plain_access
        FROM (
            SELECT t.object_type, t.object_id, t.live, t.made,
                   NULL::JSONB AS entry, FALSE AS is_deny, 0 AS hop, NULL::TEXT AS tree_type, NULL::TEXT AS from_type, NULL::UUID AS from_id, NULL::UUID AS claim_id
            FROM targets t
            UNION ALL
            SELECT e.object_type, e.object_id, FALSE, FALSE,
                   e.entry, e.is_deny, e.hop, e.tree_type, e.from_type, e.from_id, e.claim_id
            FROM entries e
        ) x
        GROUP BY x.object_type, x.object_id
    ),
    fields AS MATERIALIZED (
        SELECT t.object_type, coalesce(array_agg(a.attname::TEXT), '{}'::TEXT[]) AS names
        FROM (SELECT DISTINCT x.object_type FROM targets x) t
            LEFT JOIN pg_attribute a
                ON a.attrelid = to_regclass(t.object_type)
               AND a.attnum > 0
               AND NOT a.attisdropped
        GROUP BY t.object_type
    )
    SELECT g.object_type, g.object_id, g.live,
           CASE WHEN NOT g.live THEN '[]'::JSONB WHEN g.has_deny OR g.has_scope THEN s.settled -> 'grants' ELSE g.grants END,
           CASE WHEN NOT g.live THEN 0 WHEN g.has_deny OR g.has_scope THEN (s.settled ->> 'direct')::INTEGER WHEN g.made THEN 15 ELSE 0 END,
           CASE WHEN NOT g.live THEN 0 WHEN g.has_deny OR g.has_scope THEN (s.settled ->> 'access')::INTEGER ELSE g.plain_access | CASE WHEN g.made THEN 15 ELSE 0 END END,
           CASE WHEN g.live AND (g.has_deny OR g.has_scope) AND jsonb_typeof(s.settled -> 'scope') = 'object' THEN s.settled -> 'scope' END
    FROM grouped g
        LEFT JOIN fields f ON f.object_type = g.object_type
        LEFT JOIN LATERAL (
            SELECT claimius._apply_denies(g.grants, g.denies, g.made, f.names) AS settled
            WHERE g.has_deny OR g.has_scope
        ) s ON TRUE
$$;

COMMENT ON FUNCTION claimius._build_grants_for_objects(UUID, UUID, TEXT[], UUID[], BOOLEAN[], BOOLEAN) IS 'Grants of one user on a set of roots and, where a root cascades, the descendants reached from it, in one statement over object_info: a derivation per binding on the object, on a root or an ancestor of a root in the tree the object was reached through, on a bound node inside the area descended once, a cascading root whose own descent another root covers among them, and on the object''s own chains in its other trees, held directly or through one composition link. With p_point the bindings are found by one probe per node of the chains instead of the user''s bindings. Reads the creator from object_info; an object with no object_info row is not live and comes back with mask 0, like an object no derivation reaches, so the caller deletes the pair without a second descent. A pair with no deny and no scope settles as the OR of its grants in SQL, the others through _apply_denies.';

DROP FUNCTION IF EXISTS claimius._denormalize_objects(TEXT, UUID[]);
CREATE OR REPLACE FUNCTION claimius._denormalize_objects(
    p_object_type   TEXT,
    p_object_ids    UUID[]
) RETURNS TABLE (
                    id                  UUID,
                    sa_name             TEXT,
                    sa_description      TEXT,
                    sa_link             TEXT,
                    sa_owner_id         UUID,
                    sa_location_id      UUID,
                    sa_root_id          UUID,
                    sa_created_by       UUID,
                    sa_owner_root_id    UUID
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
        'SELECT r.id, %s, %s, NULL::TEXT, %s, %s, coalesce(%s, o.sa_root_id, l.sa_root_id), %s, %s
           FROM %I.%I r
                LEFT JOIN claimius.organization o ON o.id = %s
                LEFT JOIN claimius.location l ON l.id = %s
                LEFT JOIN claimius.organization lo ON lo.id = l.sa_owner_id
          WHERE r.id = ANY($1)%s',
        CASE WHEN v_info.has_name THEN 'r.name::TEXT' ELSE 'NULL::TEXT' END,
        CASE WHEN v_info.has_description THEN 'r.description::TEXT' ELSE 'NULL::TEXT' END,
        v_owner,
        v_loc,
        CASE WHEN v_info.has_sa_root_id THEN 'r.sa_root_id' ELSE 'NULL::UUID' END,
        CASE WHEN v_info.has_sa_created_by THEN 'r.sa_created_by' ELSE 'NULL::UUID' END,
        CASE WHEN p_object_type = 'claimius.organization' THEN 'r.sa_root_id' ELSE 'coalesce(o.sa_root_id, lo.sa_root_id)' END,
        split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2),
        v_owner,
        v_loc,
        CASE WHEN v_info.has_sa_deleted_at THEN ' AND r.sa_deleted_at IS NULL' ELSE '' END
    ) USING p_object_ids;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._denormalize_objects(TEXT, UUID[]) IS 'Reads name, description, owner, location, root, creator and owner root of a set of live rows of one registered table. The root falls back to the organization or location sa_root_id when the row carries no sa_root_id column; the owner root is the organization''s own root, or the root of the owner of the row or of its location.';

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
    INSERT INTO claimius.object_info (object_type, object_id, sa_owner_id, sa_location_id, sa_root_id, sa_name, sa_description, sa_link, sa_created_by, sa_owner_root_id)
    SELECT p_object_type, l.id, l.sa_owner_id, l.sa_location_id, l.sa_root_id, l.sa_name, l.sa_description, l.sa_link, l.sa_created_by, l.sa_owner_root_id
    FROM live l
    ON CONFLICT (object_type, object_id) DO UPDATE SET
        sa_owner_id      = EXCLUDED.sa_owner_id,
        sa_location_id   = EXCLUDED.sa_location_id,
        sa_root_id       = EXCLUDED.sa_root_id,
        sa_name          = EXCLUDED.sa_name,
        sa_description   = EXCLUDED.sa_description,
        sa_link          = EXCLUDED.sa_link,
        sa_created_by    = EXCLUDED.sa_created_by,
        sa_owner_root_id = EXCLUDED.sa_owner_root_id,
        sa_updated_at    = now()
    WHERE (claimius.object_info.sa_owner_id, claimius.object_info.sa_location_id, claimius.object_info.sa_root_id,
           claimius.object_info.sa_name, claimius.object_info.sa_description, claimius.object_info.sa_link,
           claimius.object_info.sa_created_by, claimius.object_info.sa_owner_root_id)
          IS DISTINCT FROM
          (EXCLUDED.sa_owner_id, EXCLUDED.sa_location_id, EXCLUDED.sa_root_id,
           EXCLUDED.sa_name, EXCLUDED.sa_description, EXCLUDED.sa_link,
           EXCLUDED.sa_created_by, EXCLUDED.sa_owner_root_id);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._write_object_info(TEXT, UUID[]) IS 'Writes the object_info rows of a set of objects of one type from the rows themselves: upserts the live ones where a field differs, creator and owner root included, deletes the ones gone or soft deleted.';

CREATE OR REPLACE FUNCTION claimius._settle_lock_timeout()
    RETURNS TEXT AS $$
    SELECT greatest(1, least(
               (extract(epoch FROM current_setting('deadlock_timeout')::INTERVAL) * 50)::INTEGER,
               coalesce(nullif((extract(epoch FROM current_setting('lock_timeout')::INTERVAL) * 1000)::INTEGER, 0), 2147483647)
           ))::TEXT || 'ms'
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius._settle_lock_timeout() IS 'The lock_timeout a read''s settle runs under: a twentieth of deadlock_timeout, or the caller''s lock_timeout when shorter.';

DROP FUNCTION IF EXISTS claimius._lock_all_objects();
DROP FUNCTION IF EXISTS claimius._lock_objects(TEXT[]);
DROP FUNCTION IF EXISTS claimius._object_lock_budget();
DROP FUNCTION IF EXISTS claimius._refresh_user_users(UUID, UUID);

DROP FUNCTION IF EXISTS claimius._direct_grant_for_object(UUID, UUID, TEXT, UUID);
DROP FUNCTION IF EXISTS claimius.recompute_user_object(UUID, UUID, TEXT, UUID);
DROP FUNCTION IF EXISTS claimius.recompute_user_object(UUID, UUID, TEXT, UUID[]);
CREATE OR REPLACE FUNCTION claimius.recompute_user_object(
    p_app_id        UUID,
    p_user_id       UUID,
    p_object_types  TEXT[],
    p_object_ids    UUID[],
    p_cascades      BOOLEAN[] DEFAULT NULL,
    p_settled_txid  XID8 DEFAULT NULL
) RETURNS BOOLEAN AS $$
DECLARE
    v_xmin      XID8 := coalesce(p_settled_txid, pg_snapshot_xmin(pg_current_snapshot()));
    v_point     BOOLEAN;
    v_built     JSONB;
    v_removed   INTEGER;
    v_added     INTEGER;
BEGIN
    IF p_app_id IS NULL OR p_user_id IS NULL OR p_object_types IS NULL OR p_object_ids IS NULL
        OR cardinality(p_object_ids) = 0 THEN
        RETURN FALSE;
    END IF;

    v_point := cardinality(p_object_ids) <= 5
               AND NOT EXISTS (SELECT 1 FROM unnest(coalesce(p_cascades, '{}'::BOOLEAN[])) AS c WHERE c);

    SELECT coalesce(jsonb_agg(jsonb_build_object(
               'object_type', b.object_type, 'object_id', b.object_id, 'sa_access', b.sa_access,
               'scope', b.scope, 'grants', b.grants, 'direct_grant', b.direct_access <> 0)), '[]'::JSONB)
    INTO v_built
    FROM claimius._build_grants_for_objects(p_user_id, p_app_id, p_object_types, p_object_ids, p_cascades, v_point) b;

    WITH area AS (
        SELECT b.object_type, b.object_id
        FROM jsonb_to_recordset(v_built) AS b(object_type TEXT, object_id UUID, sa_access INTEGER)
        WHERE b.sa_access = 0
    ),
    gone AS (
        DELETE FROM claimius.user_object uo
        USING area a
        WHERE uo.user_id = p_user_id
          AND uo.app_id = p_app_id
          AND uo.object_type = a.object_type
          AND uo.object_id = a.object_id
        RETURNING 1
    )
    SELECT count(*) INTO v_removed FROM gone;

    WITH changed AS (
        SELECT b.object_type, b.object_id, b.sa_access, b.scope, b.grants, b.direct_grant
        FROM jsonb_to_recordset(v_built) AS b(object_type TEXT, object_id UUID, sa_access INTEGER, scope JSONB, grants JSONB, direct_grant BOOLEAN)
            LEFT JOIN claimius.user_object uo
                ON uo.user_id = p_user_id
               AND uo.app_id = p_app_id
               AND uo.object_type = b.object_type
               AND uo.object_id = b.object_id
        WHERE b.sa_access <> 0
          AND (uo.user_id IS NULL
               OR (uo.sa_access, uo.scope, uo.grants, uo.direct_grant) IS DISTINCT FROM (b.sa_access, b.scope, b.grants, b.direct_grant)
               OR uo.settled_txid < v_xmin)
    ),
    written AS (
        INSERT INTO claimius.user_object (
            app_id, user_id, object_id, object_type,
            sa_access, scope, grants, direct_grant, settled_txid
        )
        SELECT p_app_id, p_user_id, c.object_id, c.object_type,
               c.sa_access, c.scope, c.grants, c.direct_grant, v_xmin
        FROM changed c
        ORDER BY c.object_type, c.object_id
        ON CONFLICT (user_id, app_id, object_type, object_id) DO UPDATE SET
                                       sa_access = EXCLUDED.sa_access,
                                       scope = EXCLUDED.scope,
                                       grants = EXCLUDED.grants,
                                       direct_grant = EXCLUDED.direct_grant,
                                       settled_txid = greatest(claimius.user_object.settled_txid, EXCLUDED.settled_txid),
                                       sa_updated_at = CASE WHEN (claimius.user_object.sa_access, claimius.user_object.scope, claimius.user_object.grants, claimius.user_object.direct_grant)
                                                                 IS DISTINCT FROM (EXCLUDED.sa_access, EXCLUDED.scope, EXCLUDED.grants, EXCLUDED.direct_grant)
                                                            THEN now() ELSE claimius.user_object.sa_updated_at END
        WHERE claimius.user_object.sa_access        IS DISTINCT FROM EXCLUDED.sa_access
           OR claimius.user_object.scope            IS DISTINCT FROM EXCLUDED.scope
           OR claimius.user_object.grants           IS DISTINCT FROM EXCLUDED.grants
           OR claimius.user_object.direct_grant     IS DISTINCT FROM EXCLUDED.direct_grant
           OR claimius.user_object.settled_txid     < EXCLUDED.settled_txid
        RETURNING xmax = 0 AS inserted
    )
    SELECT count(*) FILTER (WHERE inserted) INTO v_added FROM written;

    RETURN v_removed > 0 OR v_added > 0;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.recompute_user_object(UUID, UUID, TEXT[], UUID[], BOOLEAN[], XID8) IS 'Recomputes user_object for one user on a set of roots and, where a root cascades, every descendant reached from it, all types in one pass over object_info. Deletes the pairs of the area that no derivation reaches or whose object is gone, writes the pairs that differ, and moves the mark of every pair of the area to p_settled_txid, the snapshot xmin of the settle that proved it. Locks only the user''s own pairs it writes; a pair entering or leaving raises its user_users_diff row. Returns whether the user''s pair set changed.';

CREATE OR REPLACE FUNCTION claimius.recompute_user_object(
    p_app_id        UUID,
    p_user_id       UUID,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS BOOLEAN AS $$
    SELECT claimius.recompute_user_object(p_app_id, p_user_id, ARRAY[p_object_type], ARRAY[p_object_id])
$$ LANGUAGE sql;

COMMENT ON FUNCTION claimius.recompute_user_object(UUID, UUID, TEXT, UUID) IS 'The single object form of recompute_user_object: one root without cascade, the point settle of one pair, the call a consumer seed makes.';

CREATE OR REPLACE FUNCTION claimius._raise_user_users_diff()
    RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO claimius.user_users_diff (app_id, user_id, object_type, object_id, delta)
    SELECT c.app_id, c.user_id, c.object_type, c.object_id, CASE WHEN TG_OP = 'INSERT' THEN 1 ELSE -1 END
    FROM changed c
    WHERE c.object_type <> 'claimius.claim'
      AND c.app_id IN (
          SELECT a.app_id FROM claimius.user_activity a
          WHERE a.directory_at > now() - INTERVAL '30 days');
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._raise_user_users_diff() IS 'Statement trigger on user_object. Raises one user_users_diff row per pair inserted (+1) or deleted (-1) on an object that is not a claimius.claim, while the app has a viewer who read the directory within 30 days, one range of the viewer index per statement. Locks nothing.';

CREATE OR REPLACE FUNCTION claimius._settle_user_users_diff(
    p_app_id        UUID,
    p_viewer_id     UUID DEFAULT NULL,
    p_limit         INTEGER DEFAULT NULL
) RETURNS INTEGER AS $$
DECLARE
    v_lock_timeout  TEXT := current_setting('lock_timeout');
    v_touched       INTEGER := 0;
    v_folded        INTEGER := 0;
    v_ids           UUID[];
    v_apps          UUID[];
    v_viewers       UUID[];
    v_targets       UUID[];
    v_counts        INTEGER[];
BEGIN
    PERFORM set_config('lock_timeout', claimius._settle_lock_timeout(), TRUE);
    BEGIN
        WITH touched AS MATERIALIZED (
            SELECT o.app_id, o.object_type, o.object_id
            FROM (
                SELECT d.app_id, d.object_type, d.object_id, min(d.id) AS first_id
                FROM claimius.user_users_diff d
                WHERE p_viewer_id IS NULL
                  AND (p_app_id IS NULL OR d.app_id = p_app_id)
                GROUP BY d.app_id, d.object_type, d.object_id
                ORDER BY first_id
                LIMIT p_limit
            ) o
            UNION
            SELECT d.app_id, d.object_type, d.object_id
            FROM claimius.user_users_diff d
            WHERE p_viewer_id IS NOT NULL
              AND d.app_id = p_app_id
              AND d.user_id = p_viewer_id
            UNION
            SELECT d.app_id, d.object_type, d.object_id
            FROM claimius.user_object uo
                JOIN claimius.user_users_diff d
                    ON d.app_id = uo.app_id
                   AND d.object_type = uo.object_type
                   AND d.object_id = uo.object_id
            WHERE p_viewer_id IS NOT NULL
              AND uo.app_id = p_app_id
              AND uo.user_id = p_viewer_id
              AND uo.object_type <> 'claimius.claim'
        ),
        seen AS MATERIALIZED (
            SELECT d.app_id, d.object_type, d.object_id, d.id,
                   count(*) OVER (PARTITION BY d.app_id, d.object_type, d.object_id) AS n_seen
            FROM touched t
                JOIN claimius.user_users_diff d
                    ON d.app_id = t.app_id
                   AND d.object_type = t.object_type
                   AND d.object_id = t.object_id
        ),
        taken AS MATERIALIZED (
            SELECT d.app_id, d.object_type, d.object_id, d.id, s.n_seen
            FROM seen s
                JOIN claimius.user_users_diff d
                    ON d.app_id = s.app_id
                   AND d.object_type = s.object_type
                   AND d.object_id = s.object_id
                   AND d.id = s.id
            FOR UPDATE OF d SKIP LOCKED
        ),
        whole AS MATERIALIZED (
            SELECT k.app_id, k.object_type, k.object_id, k.id
            FROM (
                SELECT t.*, count(*) OVER (PARTITION BY t.app_id, t.object_type, t.object_id) AS n_taken
                FROM taken t
            ) k
            WHERE k.n_taken = k.n_seen
        ),
        folded AS MATERIALIZED (
            DELETE FROM claimius.user_users_diff d
            USING whole w
            WHERE d.app_id = w.app_id
              AND d.object_type = w.object_type
              AND d.object_id = w.object_id
              AND d.id = w.id
            RETURNING d.app_id, d.object_type, d.object_id, d.user_id, d.delta
        ),
        objects AS MATERIALIZED (
            SELECT DISTINCT f.app_id, f.object_type, f.object_id
            FROM folded f
        ),
        holder AS MATERIALIZED (
            SELECT h.app_id, h.object_type, h.object_id, h.user_id,
                   max(h.now_held) AS now_held,
                   max(h.now_held) - sum(h.shift) AS was_held,
                   bool_or(h.moved) AS moved
            FROM (
                SELECT o.app_id, o.object_type, o.object_id, uo.user_id, 1 AS now_held, 0 AS shift, FALSE AS moved
                FROM objects o
                    JOIN claimius.user_object uo
                        ON uo.app_id = o.app_id
                       AND uo.object_type = o.object_type
                       AND uo.object_id = o.object_id
                UNION ALL
                SELECT f.app_id, f.object_type, f.object_id, f.user_id, 0, f.delta, TRUE
                FROM folded f
            ) h
            GROUP BY h.app_id, h.object_type, h.object_id, h.user_id
        ),
        per_object AS (
            SELECT h.app_id, h.object_type, h.object_id,
                   array_agg(h.user_id) AS users, array_agg(h.now_held) AS nows, array_agg(h.was_held) AS wases,
                   array_agg(h.user_id) FILTER (WHERE h.moved) AS moved_users,
                   array_agg(h.now_held) FILTER (WHERE h.moved) AS moved_nows,
                   array_agg(h.was_held) FILTER (WHERE h.moved) AS moved_wases
            FROM holder h
            GROUP BY h.app_id, h.object_type, h.object_id
        ),
        shift AS (
            SELECT s.app_id, s.viewer_id, s.target_user_id, sum(s.n)::INTEGER AS n
            FROM (
                SELECT p.app_id, a.u AS viewer_id, b.u AS target_user_id, a.n * b.n - a.w * b.w AS n
                FROM per_object p
                    CROSS JOIN LATERAL unnest(p.moved_users, p.moved_nows, p.moved_wases) AS a(u, n, w)
                    CROSS JOIN LATERAL unnest(p.users, p.nows, p.wases) AS b(u, n, w)
                WHERE b.u <> a.u
                UNION ALL
                SELECT p.app_id, b.u, a.u, a.n * b.n - a.w * b.w
                FROM per_object p
                    CROSS JOIN LATERAL unnest(p.moved_users, p.moved_nows, p.moved_wases) AS a(u, n, w)
                    CROSS JOIN LATERAL unnest(p.users, p.nows, p.wases) AS b(u, n, w)
                WHERE b.u <> a.u
                  AND NOT (b.u = ANY(p.moved_users))
            ) s
            GROUP BY s.app_id, s.viewer_id, s.target_user_id
        )
        SELECT (SELECT count(*) FROM touched)::INTEGER,
               (SELECT count(*) FROM objects)::INTEGER,
               array_agg(claimius.composite_id(s.app_id::TEXT, s.viewer_id::TEXT, s.target_user_id::TEXT)),
               array_agg(s.app_id), array_agg(s.viewer_id), array_agg(s.target_user_id), array_agg(s.n)
        INTO v_touched, v_folded, v_ids, v_apps, v_viewers, v_targets, v_counts
        FROM shift s
        WHERE s.n <> 0
          AND EXISTS (
              SELECT 1 FROM claimius.user_activity a
              WHERE a.app_id = s.app_id AND a.user_id = s.viewer_id AND a.directory_at IS NOT NULL);

        IF v_ids IS NOT NULL THEN
            INSERT INTO claimius.user_users (id, app_id, viewer_id, target_user_id, sharing_object_count)
            SELECT u.id, u.app_id, u.viewer_id, u.target_user_id, u.n
            FROM unnest(v_ids, v_apps, v_viewers, v_targets, v_counts) AS u(id, app_id, viewer_id, target_user_id, n)
            ORDER BY u.id
            ON CONFLICT (id) DO UPDATE SET
                sharing_object_count = claimius.user_users.sharing_object_count + EXCLUDED.sharing_object_count,
                last_shared_at       = CASE WHEN EXCLUDED.sharing_object_count > 0 THEN now() ELSE claimius.user_users.last_shared_at END;

            DELETE FROM claimius.user_users
            WHERE id = ANY(v_ids)
              AND sharing_object_count <= 0;
        END IF;
    EXCEPTION WHEN lock_not_available OR deadlock_detected THEN
        v_folded := 0;
    END;
    PERFORM set_config('lock_timeout', v_lock_timeout, TRUE);
    RETURN greatest(v_touched - v_folded, 0);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._settle_user_users_diff(UUID, UUID, INTEGER) IS 'Folds user_users_diff into user_users per object: the objects a read of p_viewer_id needs, or the oldest p_limit objects of p_app_id or of every app. An object is folded only by the one statement that takes every row of it, FOR UPDATE SKIP LOCKED, so it waits on no writer and no other fold, and it writes rows for viewers only, the users with directory_at set, keeping their user_users equal to the count over the pairs less the rows still pending. Returns the objects left pending.';

CREATE OR REPLACE FUNCTION claimius._user_users_for_viewer(
    p_app_id        UUID,
    p_viewer_id     UUID
) RETURNS TABLE (target_user_id UUID, sharing_object_count INTEGER) AS $$
    WITH touched AS MATERIALIZED (
        SELECT d.object_type, d.object_id
        FROM claimius.user_users_diff d
        WHERE d.app_id = p_app_id
          AND d.user_id = p_viewer_id
        UNION
        SELECT d.object_type, d.object_id
        FROM claimius.user_object uo
            JOIN claimius.user_users_diff d
                ON d.app_id = uo.app_id
               AND d.object_type = uo.object_type
               AND d.object_id = uo.object_id
        WHERE uo.app_id = p_app_id
          AND uo.user_id = p_viewer_id
          AND uo.object_type <> 'claimius.claim'
    ),
    holder AS MATERIALIZED (
        SELECT h.object_type, h.object_id, h.user_id,
               max(h.now_held) AS now_held,
               max(h.now_held) - sum(h.shift) AS was_held
        FROM (
            SELECT t.object_type, t.object_id, uo.user_id, 1 AS now_held, 0 AS shift
            FROM touched t
                JOIN claimius.user_object uo
                    ON uo.app_id = p_app_id
                   AND uo.object_type = t.object_type
                   AND uo.object_id = t.object_id
            UNION ALL
            SELECT t.object_type, t.object_id, d.user_id, 0, d.delta
            FROM touched t
                JOIN claimius.user_users_diff d
                    ON d.app_id = p_app_id
                   AND d.object_type = t.object_type
                   AND d.object_id = t.object_id
        ) h
        GROUP BY h.object_type, h.object_id, h.user_id
    ),
    per_object AS (
        SELECT h.object_type, h.object_id,
               array_agg(h.user_id) AS users, array_agg(h.now_held) AS nows, array_agg(h.was_held) AS wases,
               max(h.now_held) FILTER (WHERE h.user_id = p_viewer_id) AS viewer_now,
               max(h.was_held) FILTER (WHERE h.user_id = p_viewer_id) AS viewer_was
        FROM holder h
        GROUP BY h.object_type, h.object_id
    ),
    counted AS (
        SELECT b.u AS target_user_id, p.viewer_now * b.n - p.viewer_was * b.w AS n
        FROM per_object p
            CROSS JOIN LATERAL unnest(p.users, p.nows, p.wases) AS b(u, n, w)
        WHERE p.viewer_now IS NOT NULL
          AND b.u <> p_viewer_id
        UNION ALL
        SELECT uu.target_user_id, uu.sharing_object_count
        FROM claimius.user_users uu
        WHERE uu.app_id = p_app_id
          AND uu.viewer_id = p_viewer_id
    )
    SELECT c.target_user_id, sum(c.n)::INTEGER
    FROM counted c
    GROUP BY c.target_user_id
    HAVING sum(c.n) > 0
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius._user_users_for_viewer(UUID, UUID) IS 'The users the viewer shares an object that is not a claim with and the count, exact for the statement''s snapshot: the stored user_users range plus the moves of the user_users_diff rows still pending on the viewer''s objects. Read after _settle_directory made the user a viewer.';

CREATE OR REPLACE FUNCTION claimius._settle_directory(
    p_app_id        UUID,
    p_user_id       UUID
) RETURNS VOID AS $$
DECLARE
    v_at    TIMESTAMPTZ;
BEGIN
    SELECT a.directory_at INTO v_at
    FROM claimius.user_activity a
    WHERE a.user_id = p_user_id AND a.app_id = p_app_id;

    IF v_at IS NOT NULL AND v_at > now() - INTERVAL '30 days' THEN
        IF v_at < now() - INTERVAL '1 day' THEN
            UPDATE claimius.user_activity SET directory_at = now()
            WHERE user_id = p_user_id AND app_id = p_app_id;
        END IF;
        RETURN;
    END IF;

    DELETE FROM claimius.user_users WHERE app_id = p_app_id AND viewer_id = p_user_id;

    WITH mine AS MATERIALIZED (
        SELECT o.object_type, o.object_id
        FROM (
            SELECT uo.object_type, uo.object_id
            FROM claimius.user_object uo
            WHERE uo.user_id = p_user_id AND uo.app_id = p_app_id AND uo.object_type <> 'claimius.claim'
            UNION
            SELECT d.object_type, d.object_id
            FROM claimius.user_users_diff d
            WHERE d.app_id = p_app_id AND d.user_id = p_user_id
        ) o
    ),
    pending AS MATERIALIZED (
        SELECT d.object_type, d.object_id, d.user_id, d.delta
        FROM mine m
            JOIN claimius.user_users_diff d
                ON d.app_id = p_app_id AND d.object_type = m.object_type AND d.object_id = m.object_id
        FOR KEY SHARE OF d
    ),
    holder AS MATERIALIZED (
        SELECT h.object_type, h.object_id, h.user_id, max(h.now_held) - sum(h.shift) AS held
        FROM (
            SELECT m.object_type, m.object_id, uo.user_id, 1 AS now_held, 0 AS shift
            FROM mine m
                JOIN claimius.user_object uo
                    ON uo.app_id = p_app_id AND uo.object_type = m.object_type AND uo.object_id = m.object_id
            UNION ALL
            SELECT p.object_type, p.object_id, p.user_id, 0, p.delta
            FROM pending p
        ) h
        GROUP BY h.object_type, h.object_id, h.user_id
    ),
    per_object AS (
        SELECT h.object_type, h.object_id,
               array_agg(h.user_id) AS users, array_agg(h.held) AS helds,
               max(h.held) FILTER (WHERE h.user_id = p_user_id) AS viewer_held
        FROM holder h
        GROUP BY h.object_type, h.object_id
    ),
    counted AS (
        SELECT b.user_id AS target_user_id, sum(p.viewer_held * b.held)::INTEGER AS n
        FROM per_object p
            CROSS JOIN LATERAL unnest(p.users, p.helds) AS b(user_id, held)
        WHERE p.viewer_held IS NOT NULL
          AND b.user_id <> p_user_id
        GROUP BY b.user_id
        HAVING sum(p.viewer_held * b.held) > 0
    )
    INSERT INTO claimius.user_users (id, app_id, viewer_id, target_user_id, sharing_object_count)
    SELECT claimius.composite_id(p_app_id::TEXT, p_user_id::TEXT, c.target_user_id::TEXT), p_app_id, p_user_id, c.target_user_id, c.n
    FROM counted c
    ORDER BY 1;

    INSERT INTO claimius.user_activity (user_id, app_id, directory_at)
    VALUES (p_user_id, p_app_id, now())
    ON CONFLICT (user_id, app_id) DO UPDATE SET directory_at = now();
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._settle_directory(UUID, UUID) IS 'Makes a user a viewer: on the first directory read, or the first after 30 days away, deletes its user_users rows and builds them from its pairs as the count over the pairs less the user_users_diff rows still pending on its objects, which it holds FOR KEY SHARE so no fold moves them meanwhile, then stamps directory_at. Later reads refresh the stamp at most once a day. The fold keeps the rows from then on.';

CREATE OR REPLACE FUNCTION claimius._settle_usage_diff(
    p_app_id        UUID,
    p_user_id       UUID DEFAULT NULL,
    p_limit         INTEGER DEFAULT NULL
) RETURNS INTEGER AS $$
DECLARE
    v_lock_timeout  TEXT := current_setting('lock_timeout');
    v_folded        INTEGER := 0;
BEGIN
    PERFORM set_config('lock_timeout', claimius._settle_lock_timeout(), TRUE);
    BEGIN
        WITH taken AS (
            DELETE FROM claimius.usage_diff d
            WHERE (d.app_id, d.user_id, d.id) IN (
                SELECT x.app_id, x.user_id, x.id
                FROM claimius.usage_diff x
                WHERE (p_app_id IS NULL OR x.app_id = p_app_id)
                  AND (p_user_id IS NULL OR x.user_id = p_user_id)
                ORDER BY x.id
                LIMIT p_limit
                FOR UPDATE SKIP LOCKED)
            RETURNING d.app_id, d.user_id, d.object_type, d.object_id, d.used_count, d.used_at
        ),
        summed AS (
            SELECT t.app_id, t.user_id, t.object_type, t.object_id, sum(t.used_count) AS n, max(t.used_at) AS at
            FROM taken t
            GROUP BY t.app_id, t.user_id, t.object_type, t.object_id
        ),
        touched AS (
            UPDATE claimius.user_object uo
            SET used_count = uo.used_count + s.n,
                last_used_at = greatest(coalesce(uo.last_used_at, s.at), s.at)
            FROM summed s
            WHERE uo.user_id = s.user_id
              AND uo.app_id = s.app_id
              AND uo.object_type = s.object_type
              AND uo.object_id = s.object_id
            RETURNING 1
        )
        SELECT count(*) INTO v_folded FROM touched;
    EXCEPTION WHEN lock_not_available OR deadlock_detected THEN
        v_folded := 0;
    END;
    PERFORM set_config('lock_timeout', v_lock_timeout, TRUE);
    RETURN v_folded;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._settle_usage_diff(UUID, UUID, INTEGER) IS 'Folds usage_diff into user_object.used_count and last_used_at: the rows of one user in one app, or the oldest p_limit rows of p_app_id or of every app. Rows are taken FOR UPDATE SKIP LOCKED and summed, so concurrent folds commute and never wait on each other; a pair held past _settle_lock_timeout gives the fold up and its rows stay. Returns the pairs moved.';

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
    IF p_object_type <> 'claimius.samna_user' THEN
        SELECT ti.has_sa_created_by INTO v_has_creator
        FROM claimius.table_info ti
        WHERE ti.object_type = p_object_type;

        IF v_has_creator IS TRUE THEN
            v_parts := claimius.split_object_type(p_object_type);
            EXECUTE format('SELECT sa_created_by FROM %I.%I WHERE id = $1', v_parts[1], v_parts[2])
                INTO v_creator_uc_id USING p_object_id;

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
            SELECT v_creator_user AS user_id
            WHERE v_creator_user IS NOT NULL

            UNION

            SELECT uo.user_id FROM claimius.user_object uo
            WHERE uo.app_id = p_app_id AND uo.object_type = p_object_type AND uo.object_id = p_object_id

            UNION

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

CREATE OR REPLACE FUNCTION claimius.get_location_ancestors(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS TABLE(object_type TEXT, object_id UUID, hop INTEGER) AS $$
BEGIN
    RETURN QUERY
        SELECT e.ancestor_type, e.ancestor_id, e.depth
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'location'
          AND e.descendant_type = p_object_type
          AND e.descendant_id   = p_object_id
          AND e.ancestor_type   = 'claimius.location';

    IF p_object_type <> 'claimius.location' THEN
        RETURN QUERY SELECT p_object_type, p_object_id, 0;
    END IF;

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

COMMENT ON FUNCTION claimius.apply_row_delta(text, uuid, jsonb, jsonb) IS 'Brings the closure rows of one registered row in line with a change given as its old and new json: drops the rows where it is the descendant and attaches it again under its live owner, pin and parent. For a caller that wrote the row with the triggers off.';

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

COMMENT ON FUNCTION claimius.rebuild_tree(uuid) IS 'Rebuilds the ownership, location and parenthood closure rows of one root from the live rows. Repair only; every write keeps the closure incrementally.';

CREATE OR REPLACE FUNCTION claimius.rebuild_user_object()
 RETURNS void
 LANGUAGE plpgsql
AS $function$
DECLARE
    v_pair RECORD;
BEGIN
    DELETE FROM claimius.user_object;
    DELETE FROM claimius.user_users_diff;
    DELETE FROM claimius.user_users;

    FOR v_pair IN
        SELECT x.app_id, x.user_id, array_agg(x.object_type) AS object_types, array_agg(x.object_id) AS object_ids
        FROM (
            SELECT DISTINCT uc.app_id, uc.user_id, co.object_type, co.object_id
            FROM claimius.user_claim uc
                     JOIN claimius.claim_object co ON co.claim_id = uc.claim_id AND co.object_id IS NOT NULL
            WHERE uc.sa_deleted_at IS NULL
              AND co.sa_deleted_at IS NULL
        ) x
        GROUP BY x.app_id, x.user_id
    LOOP
        PERFORM claimius.recompute_user_object(v_pair.app_id, v_pair.user_id, v_pair.object_types, v_pair.object_ids);
    END LOOP;

    PERFORM claimius._settle_user_users_diff(NULL);
END;
$function$
;

COMMENT ON FUNCTION claimius.rebuild_user_object() IS 'Repair: empties user_object, user_users_diff and user_users together, recomputes the pairs of every holder on every object a live binding names directly, one recompute per holder, and folds every user_users_diff row. Descendants reached only through a cascade fill on the next settle that covers them.';

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
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._actor_user_for_row(UUID, TEXT, ANYELEMENT) IS 'User behind sa_created_by on a row. The one user a write materializes synchronously.';

DROP FUNCTION IF EXISTS claimius._raise_object_diff(UUID, TEXT, UUID, BOOLEAN);
CREATE OR REPLACE FUNCTION claimius._raise_object_diff(
    p_app_id        UUID,
    p_object_type   TEXT,
    p_object_id     UUID,
    p_cascade       BOOLEAN DEFAULT FALSE,
    p_root_id       UUID DEFAULT NULL
) RETURNS VOID AS $$
DECLARE
    v_root UUID := p_root_id;
BEGIN
    IF p_app_id IS NULL OR p_object_type IS NULL OR p_object_id IS NULL THEN
        RETURN;
    END IF;

    IF v_root IS NULL THEN
        SELECT oi.sa_owner_root_id INTO v_root
        FROM claimius.object_info oi
        WHERE oi.object_type = p_object_type AND oi.object_id = p_object_id;
    END IF;

    INSERT INTO claimius.access_diff (id, app_id, kind, object_type, object_id, cascade, root_id, txid)
    VALUES (claimius.composite_id('object', p_app_id::TEXT, p_object_type, p_object_id::TEXT),
            p_app_id, 'object', p_object_type, p_object_id, coalesce(p_cascade, FALSE), v_root, pg_current_xact_id())
    ON CONFLICT (id) DO UPDATE
        SET sa_updated_at = now(),
            txid = pg_current_xact_id(),
            cascade = claimius.access_diff.cascade OR EXCLUDED.cascade,
            root_id = coalesce(EXCLUDED.root_id, claimius.access_diff.root_id);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._raise_object_diff(UUID, TEXT, UUID, BOOLEAN, UUID) IS 'Raises the object area of an object in an app: one row keyed on the object, its txid set to the raising transaction on every raise, cascade kept once set, and the owner root from the caller or from object_info so readers of that root find it.';

DROP FUNCTION IF EXISTS claimius._raise_claim_diff(UUID, UUID);
DROP FUNCTION IF EXISTS claimius._raise_claim_diff(UUID, UUID, UUID);
CREATE OR REPLACE FUNCTION claimius._raise_claim_diff(
    p_app_id        UUID,
    p_claim_id      UUID,
    p_user_id       UUID DEFAULT NULL,
    p_cascade       BOOLEAN DEFAULT NULL
) RETURNS VOID AS $$
BEGIN
    IF p_app_id IS NULL OR p_claim_id IS NULL THEN
        RETURN;
    END IF;

    IF p_user_id IS NOT NULL THEN
        INSERT INTO claimius.access_diff (id, app_id, kind, claim_id, user_id, txid)
        VALUES (claimius.composite_id('assignment', p_app_id::TEXT, p_claim_id::TEXT, p_user_id::TEXT),
                p_app_id, 'assignment', p_claim_id, p_user_id, pg_current_xact_id())
        ON CONFLICT (id) DO UPDATE
            SET sa_updated_at = now(),
                txid = pg_current_xact_id();
        RETURN;
    END IF;

    PERFORM claimius._raise_object_diff(p_app_id, b.object_type, b.object_id, b.cascade)
    FROM (
        SELECT co.object_type, co.object_id, bool_or(co.inherits AND coalesce(p_cascade, c.inherits)) AS cascade
        FROM claimius.claim c
            JOIN claimius.claim_object co
                ON co.claim_id = c.id
               AND co.app_id = p_app_id
               AND co.sa_deleted_at IS NULL
               AND co.object_id IS NOT NULL
               AND co.object_type <> 'claimius.claim'
        WHERE c.id = p_claim_id
        GROUP BY co.object_type, co.object_id

        UNION ALL

        SELECT co.object_type, co.object_id, bool_or(co.inherits AND cb.inherits)
        FROM claimius.claim_object link
            JOIN claimius.claim cb ON cb.id = link.object_id
            JOIN claimius.claim_object co
                ON co.claim_id = cb.id
               AND co.app_id = p_app_id
               AND co.sa_deleted_at IS NULL
               AND co.object_id IS NOT NULL
               AND co.object_type <> 'claimius.claim'
        WHERE link.claim_id = p_claim_id
          AND link.app_id = p_app_id
          AND link.object_type = 'claimius.claim'
          AND link.sa_deleted_at IS NULL
        GROUP BY co.object_type, co.object_id
    ) b;

    PERFORM claimius._raise_object_diff(p_app_id, 'claimius.claim', p_claim_id, FALSE);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._raise_claim_diff(UUID, UUID, UUID, BOOLEAN) IS 'Raises what a change to a claim reaches: with p_user_id the one assignment area of that user, otherwise one object area per live binding of the claim and of the claims it links to, with cascade where the binding inherits and the claim inherits before or after the change, and one on the claim object itself. A check then finds the change on a node of the object''s chain and nothing per held claim.';

DROP FUNCTION IF EXISTS claimius._settle_object_diff(UUID, TEXT, UUID, BOOLEAN);
DROP FUNCTION IF EXISTS claimius._settle_object_diff(UUID, UUID, TEXT, UUID, BOOLEAN);
DROP FUNCTION IF EXISTS claimius._settle_object_diff(UUID, UUID, TEXT[], UUID[], BOOLEAN[]);
DROP FUNCTION IF EXISTS claimius._settle_claim_diff(UUID, UUID);
DROP FUNCTION IF EXISTS claimius._settle_claim_diff_for_user(UUID, UUID, UUID);
DROP FUNCTION IF EXISTS claimius._settle_claim_diff(UUID, UUID, UUID, TIMESTAMPTZ);
DROP FUNCTION IF EXISTS claimius._diff_holders(UUID, UUID);
DROP FUNCTION IF EXISTS claimius._diff_holders(UUID, UUID[]);
DROP FUNCTION IF EXISTS claimius._clear_diff(UUID, UUID);
DROP FUNCTION IF EXISTS claimius._clear_diff(UUID, UUID[]);
DROP FUNCTION IF EXISTS claimius._settle_diff_share(UUID, UUID);
DROP FUNCTION IF EXISTS claimius._settle_diff_share(UUID, UUID, UUID);
DROP FUNCTION IF EXISTS claimius._settle_diff_share(UUID, UUID, UUID[]);
CREATE OR REPLACE FUNCTION claimius._settle_diff_share(
    p_app_id        UUID,
    p_user_id       UUID
) RETURNS BOOLEAN AS $$
DECLARE
    v_xmin      XID8 := pg_snapshot_xmin(pg_current_snapshot());
    v_cursor    XID8;
    v_trimmed   XID8;
    v_types     TEXT[];
    v_ids       UUID[];
    v_cascades  BOOLEAN[];
    v_full      BOOLEAN;
    v_areas     BIGINT;
    v_reach     BIGINT;
BEGIN
    BEGIN
        SELECT a.settled_txid INTO v_cursor
        FROM claimius.user_activity a
        WHERE a.user_id = p_user_id AND a.app_id = p_app_id;
        v_cursor := coalesce(v_cursor, '0'::XID8);

        SELECT ps.access_trimmed_txid INTO v_trimmed
        FROM claimius.prophet_state ps
        WHERE ps.app_id = p_app_id;
        v_trimmed := coalesce(v_trimmed, '0'::XID8);

        IF v_cursor = '0'::XID8 OR v_cursor < v_trimmed THEN
            v_full := TRUE;
        ELSE
            SELECT count(*) INTO v_areas
            FROM claimius.access_diff d
            WHERE d.txid >= v_cursor AND d.app_id = p_app_id;
            IF v_areas = 0 THEN
                RETURN TRUE;
            END IF;
            SELECT count(*) INTO v_reach
            FROM (
                SELECT 1 FROM claimius.user_object uo
                WHERE uo.user_id = p_user_id AND uo.app_id = p_app_id
                LIMIT 2 * v_areas) r;
            v_full := v_reach < 2 * v_areas;
        END IF;

        WITH grants_held AS MATERIALIZED (
            SELECT uc.id AS user_claim_id, uc.claim_id, c.inherits, c.sa_root_id AS claim_root, c.id IS NOT NULL AS claim_live
            FROM claimius.user_claim uc
                LEFT JOIN claimius.claim c ON c.id = uc.claim_id AND c.sa_deleted_at IS NULL
            WHERE uc.user_id = p_user_id
              AND uc.app_id = p_app_id
              AND uc.sa_deleted_at IS NULL
        ),
        claims AS MATERIALIZED (
            SELECT g.claim_id, g.inherits FROM grants_held g WHERE g.claim_live
            UNION
            SELECT link.object_id, cb.inherits
            FROM grants_held g
                JOIN claimius.claim_object link
                    ON link.claim_id = g.claim_id
                   AND link.app_id = p_app_id
                   AND link.object_type = 'claimius.claim'
                   AND link.sa_deleted_at IS NULL
                   AND link.inherits
                JOIN claimius.claim cb ON cb.id = link.object_id AND cb.sa_deleted_at IS NULL AND cb.inherits
            WHERE g.claim_live
        ),
        bound AS MATERIALIZED (
            SELECT co.object_type, co.object_id, bool_or(co.inherits AND cl.inherits) AS cascades, max(oi.sa_owner_root_id::TEXT)::UUID AS root_id
            FROM claims cl
                JOIN claimius.claim_object co
                    ON co.claim_id = cl.claim_id
                   AND co.app_id = p_app_id
                   AND co.sa_deleted_at IS NULL
                   AND co.object_id IS NOT NULL
                LEFT JOIN claimius.object_info oi ON oi.object_type = co.object_type AND oi.object_id = co.object_id
            GROUP BY co.object_type, co.object_id
        ),
        roots AS MATERIALIZED (
            SELECT DISTINCT r.root_id
            FROM (
                SELECT b.root_id FROM bound b
                UNION ALL
                SELECT g.claim_root FROM grants_held g
                UNION ALL
                SELECT x.root_id
                FROM grants_held g
                    CROSS JOIN LATERAL (
                        WITH RECURSIVE walk AS (
                            SELECT (SELECT oi.sa_owner_root_id FROM claimius.object_info oi
                                    WHERE oi.sa_created_by = g.user_claim_id AND oi.sa_owner_root_id IS NOT NULL
                                    ORDER BY oi.sa_owner_root_id LIMIT 1) AS root_id
                            UNION ALL
                            SELECT (SELECT oi.sa_owner_root_id FROM claimius.object_info oi
                                    WHERE oi.sa_created_by = g.user_claim_id AND oi.sa_owner_root_id > w.root_id
                                    ORDER BY oi.sa_owner_root_id LIMIT 1)
                            FROM walk w
                            WHERE w.root_id IS NOT NULL
                        )
                        SELECT w.root_id FROM walk w WHERE w.root_id IS NOT NULL
                    ) x
                UNION ALL
                SELECT NULL::UUID
            ) r
        ),
        areas AS MATERIALIZED (
            SELECT d.kind, d.object_type, d.object_id, d.cascade, d.claim_id
            FROM roots r
                JOIN claimius.access_diff d
                    ON d.app_id = p_app_id
                   AND d.root_id IS NOT DISTINCT FROM r.root_id
                   AND d.txid >= v_cursor
            WHERE NOT v_full
              AND d.kind = 'object'
            UNION ALL
            SELECT d.kind, d.object_type, d.object_id, d.cascade, d.claim_id
            FROM claimius.access_diff d
            WHERE NOT v_full
              AND d.app_id = p_app_id
              AND d.kind = 'assignment'
              AND d.user_id = p_user_id
              AND d.txid >= v_cursor
        ),
        strong AS MATERIALIZED (
            SELECT a.object_type, a.object_id, a.cascade,
                   EXISTS (SELECT 1 FROM claimius.user_object uo
                           WHERE uo.user_id = p_user_id AND uo.app_id = p_app_id
                             AND uo.object_type = a.object_type AND uo.object_id = a.object_id)
                   OR EXISTS (SELECT 1 FROM bound b WHERE b.object_type = a.object_type AND b.object_id = a.object_id)
                   OR EXISTS (SELECT 1 FROM claimius.object_info oi
                                  JOIN grants_held g ON g.user_claim_id = oi.sa_created_by
                              WHERE oi.object_type = a.object_type AND oi.object_id = a.object_id)
                   OR EXISTS (SELECT 1 FROM claimius.inheritance_info e
                                  JOIN bound b ON b.cascades AND b.object_type = e.ancestor_type AND b.object_id = e.ancestor_id
                              WHERE e.descendant_type = a.object_type AND e.descendant_id = a.object_id AND e.depth > 0) AS relevant
            FROM areas a
            WHERE a.kind = 'object'
        ),
        share AS (
            SELECT s.object_type, s.object_id, s.cascade
            FROM strong s
            WHERE s.relevant

            UNION ALL

            SELECT b.object_type, b.object_id, b.cascades
            FROM strong s
                JOIN claimius.inheritance_info e
                    ON e.ancestor_type = s.object_type AND e.ancestor_id = s.object_id AND e.depth > 0
                JOIN bound b ON b.object_type = e.descendant_type AND b.object_id = e.descendant_id
            WHERE NOT s.relevant
              AND s.cascade

            UNION ALL

            SELECT co.object_type, co.object_id, co.inherits AND c.inherits
            FROM areas a
                JOIN claimius.claim c ON c.id = a.claim_id
                JOIN claimius.claim_object co
                    ON co.claim_id = c.id
                   AND co.app_id = p_app_id
                   AND co.sa_deleted_at IS NULL
                   AND co.object_id IS NOT NULL
            WHERE a.kind = 'assignment'

            UNION ALL

            SELECT co.object_type, co.object_id, co.inherits AND cb.inherits
            FROM areas a
                JOIN claimius.claim_object link
                    ON link.claim_id = a.claim_id
                   AND link.app_id = p_app_id
                   AND link.object_type = 'claimius.claim'
                   AND link.sa_deleted_at IS NULL
                JOIN claimius.claim cb ON cb.id = link.object_id
                JOIN claimius.claim_object co
                    ON co.claim_id = cb.id
                   AND co.app_id = p_app_id
                   AND co.sa_deleted_at IS NULL
                   AND co.object_id IS NOT NULL
            WHERE a.kind = 'assignment'

            UNION ALL

            SELECT 'claimius.claim', a.claim_id, FALSE
            FROM areas a
            WHERE a.kind = 'assignment'

            UNION ALL

            SELECT b.object_type, b.object_id, b.cascades
            FROM bound b
            WHERE v_full

            UNION ALL

            SELECT uo.object_type, uo.object_id, FALSE
            FROM claimius.user_object uo
            WHERE v_full
              AND uo.user_id = p_user_id
              AND uo.app_id = p_app_id

            UNION ALL

            SELECT oi.object_type, oi.object_id, FALSE
            FROM grants_held g
                JOIN claimius.object_info oi ON oi.sa_created_by = g.user_claim_id
            WHERE v_full

            UNION ALL

            SELECT 'claimius.claim', g.claim_id, FALSE
            FROM grants_held g
            WHERE v_full
        )
        SELECT array_agg(s.object_type), array_agg(s.object_id), array_agg(s.cascade)
        INTO v_types, v_ids, v_cascades
        FROM share s;

        IF v_types IS NOT NULL THEN
            PERFORM claimius.recompute_user_object(p_app_id, p_user_id, v_types, v_ids, v_cascades, v_xmin);
        END IF;

        INSERT INTO claimius.user_activity (user_id, app_id, settled_txid, settled_at)
        VALUES (p_user_id, p_app_id, v_xmin, now())
        ON CONFLICT (user_id, app_id) DO UPDATE
            SET settled_txid = greatest(claimius.user_activity.settled_txid, EXCLUDED.settled_txid),
                settled_at = now();
    EXCEPTION WHEN lock_not_available OR deadlock_detected THEN
        RETURN FALSE;
    END;
    RETURN TRUE;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._settle_diff_share(UUID, UUID) IS 'Settles one user''s share of every area raised since its cursor, in one pass: the object areas of the roots of its bindings, its claims and the objects it created, kept when the user holds a pair on the node, the node or an ancestor is bound by a claim it holds or links to, or the node is its own creation, and for a cascade area the user is bound inside it, the bound nodes standing in for the node; its assignment areas as the bindings of the claim and of the claims it links to with the claim object; and its whole reach, from its bindings, its pairs, its claims and its creations with no area read, when it has no cursor, its cursor is below the trimmed position, or the areas since its cursor in the app number at least half its pairs, counted up to twice the areas. Recomputes the share with the snapshot xmin taken before the areas were read and moves the cursor to it. Runs under the reader''s share lock; a lock wait past the settle timeout or a deadlock leaves the cursor and returns false.';
