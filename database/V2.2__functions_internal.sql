-- ============================================================================
-- Claimius V2.2 Internal Functions
-- ----------------------------------------------------------------------------
-- Functions used internally by triggers, init, and other claimius internals.
-- Not exposed to consumers. These functions assume valid inputs and may
-- raise on misuse.
-- ============================================================================

-- ----------------------------------------------------------------------------
-- Tree builders. Used by init, hierarchy reparenting, and bulk operations.
-- ----------------------------------------------------------------------------

-- _tree_row
-- Composite type used internally to assemble tree nodes before constructing
-- the jsonb. Defined here at the top of the file because some functions
-- below reference claimius._tree_row[] in their DECLARE block, which is
-- parsed at CREATE FUNCTION time and so requires the type to already exist.
DO $$
    BEGIN
        IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = '_tree_row' AND typnamespace = (SELECT oid FROM pg_namespace WHERE nspname = 'claimius')) THEN
            CREATE TYPE claimius._tree_row AS (
                                                  object_type TEXT,
                                                  object_id UUID,
                                                  parent_type TEXT,
                                                  parent_id UUID,
                                                  level INTEGER
                                              );
        END IF;
    END $$;

-- build_ownership_tree
-- Walks live data and rebuilds the ownership tree for one root organization.
-- Replaces or creates the inheritance_info row for
-- (claimius.organization, root_id, ownership).
CREATE OR REPLACE FUNCTION claimius.build_ownership_tree(p_root_id UUID)
    RETURNS VOID AS $$
DECLARE
    v_tree JSONB;
BEGIN
    -- Build the tree by recursively walking organization parent chains and
    -- splicing every other registered table's rows under their sa_owner_id.
    -- Construction is bottom up so children are filled before parents.
    WITH RECURSIVE
        org_descendants AS (
            SELECT id, sa_owner_id, 1 AS lvl
            FROM claimius.organization
            WHERE sa_owner_id = p_root_id AND id <> p_root_id AND sa_deleted_at IS NULL
            UNION ALL
            SELECT o.id, o.sa_owner_id, d.lvl + 1
            FROM claimius.organization o
                     JOIN org_descendants d ON o.sa_owner_id = d.id
            WHERE o.id <> o.sa_owner_id AND o.sa_deleted_at IS NULL
        ),
        nodes AS (
            -- organizations under the root
            SELECT 'claimius.organization'::TEXT AS object_type,
                   id AS object_id,
                   sa_owner_id AS parent_id,
                   'claimius.organization'::TEXT AS parent_type,
                   lvl AS level
            FROM org_descendants
            UNION ALL
            -- objects of any registered table that point at any org in this tree
            -- via sa_owner_id (including the root itself)
            SELECT t.object_type,
                   (r.id)::UUID AS object_id,
                   (r.sa_owner_id)::UUID AS parent_id,
                   'claimius.organization'::TEXT AS parent_type,
                   coalesce(d.lvl, 0) + 1 AS level
            FROM claimius.table_info t
                     CROSS JOIN LATERAL claimius._select_owned_rows(t.object_type, p_root_id) r
                     LEFT JOIN org_descendants d ON d.id = r.sa_owner_id
            WHERE t.has_sa_owner_id
              AND t.has_sa_deleted_at
              AND t.object_type <> 'claimius.organization'
        )
    SELECT claimius._build_tree_jsonb(p_root_id, 'claimius.organization', array_agg(ROW(object_type, object_id, parent_type, parent_id, level)::claimius._tree_row))
    INTO v_tree
    FROM nodes;

    INSERT INTO claimius.inheritance_info (object_type, root_id, type, tree)
    VALUES ('claimius.organization', p_root_id, 'ownership', coalesce(v_tree, '{}'::JSONB))
    ON CONFLICT (object_type, root_id, type)
        DO UPDATE SET tree = EXCLUDED.tree, sa_updated_at = now();
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.build_ownership_tree(UUID) IS 'Rebuilds the ownership tree for one organization root from live data.';

-- build_location_tree
-- Walks live data and rebuilds the location tree for one root location.
CREATE OR REPLACE FUNCTION claimius.build_location_tree(p_root_id UUID)
    RETURNS VOID AS $$
DECLARE
    v_tree JSONB;
BEGIN
    WITH RECURSIVE
        loc_descendants AS (
            SELECT id, sa_parent_id, 1 AS lvl
            FROM claimius.location
            WHERE sa_parent_id = p_root_id AND id <> p_root_id AND sa_deleted_at IS NULL
            UNION ALL
            SELECT l.id, l.sa_parent_id, d.lvl + 1
            FROM claimius.location l
                     JOIN loc_descendants d ON l.sa_parent_id = d.id
            WHERE l.id <> l.sa_parent_id AND l.sa_deleted_at IS NULL
        ),
        nodes AS (
            SELECT 'claimius.location'::TEXT AS object_type,
                   id AS object_id,
                   sa_parent_id AS parent_id,
                   'claimius.location'::TEXT AS parent_type,
                   lvl AS level
            FROM loc_descendants
            UNION ALL
            SELECT t.object_type,
                   (r.id)::UUID AS object_id,
                   (r.sa_location_id)::UUID AS parent_id,
                   'claimius.location'::TEXT AS parent_type,
                   coalesce(d.lvl, 0) + 1 AS level
            FROM claimius.table_info t
                     CROSS JOIN LATERAL claimius._select_located_rows(t.object_type, p_root_id) r
                     LEFT JOIN loc_descendants d ON d.id = r.sa_location_id
            WHERE t.has_sa_location_id
              AND t.has_sa_deleted_at
        )
    SELECT claimius._build_tree_jsonb(p_root_id, 'claimius.location', array_agg(ROW(object_type, object_id, parent_type, parent_id, level)::claimius._tree_row))
    INTO v_tree
    FROM nodes;

    INSERT INTO claimius.inheritance_info (object_type, root_id, type, tree)
    VALUES ('claimius.location', p_root_id, 'location', coalesce(v_tree, '{}'::JSONB))
    ON CONFLICT (object_type, root_id, type)
        DO UPDATE SET tree = EXCLUDED.tree, sa_updated_at = now();
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.build_location_tree(UUID) IS 'Rebuilds the location tree for one location root from live data.';

-- build_parenthood_tree
-- Walks live data and rebuilds the parenthood tree for one root row of a
-- self referencing registered table.
CREATE OR REPLACE FUNCTION claimius.build_parenthood_tree(p_object_type TEXT, p_root_id UUID)
    RETURNS VOID AS $$
DECLARE
    v_rows claimius._tree_row[];
    v_tree JSONB;
    v_sql TEXT;
BEGIN
    v_sql := format($f$
        WITH RECURSIVE
        descendants AS (
            SELECT id, sa_parent_id, 1 AS lvl
            FROM %I.%I
            WHERE sa_parent_id = $1 AND id <> $1 AND sa_deleted_at IS NULL
            UNION ALL
            SELECT t.id, t.sa_parent_id, d.lvl + 1
            FROM %I.%I t
            JOIN descendants d ON t.sa_parent_id = d.id
            WHERE t.id <> t.sa_parent_id AND t.sa_deleted_at IS NULL
        )
        SELECT array_agg(ROW(%L::TEXT, id, %L::TEXT, sa_parent_id, lvl)::claimius._tree_row)
        FROM descendants
    $f$,
                    split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2),
                    split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2),
                    p_object_type, p_object_type
             );

    EXECUTE v_sql INTO v_rows USING p_root_id;

    SELECT claimius._build_tree_jsonb(p_root_id, p_object_type, coalesce(v_rows, ARRAY[]::claimius._tree_row[])) INTO v_tree;

    INSERT INTO claimius.inheritance_info (object_type, root_id, type, tree)
    VALUES (p_object_type, p_root_id, 'parenthood', coalesce(v_tree, '{}'::JSONB))
    ON CONFLICT (object_type, root_id, type)
        DO UPDATE SET tree = EXCLUDED.tree, sa_updated_at = now();
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.build_parenthood_tree(TEXT, UUID) IS 'Rebuilds the parenthood tree for one root of a self referencing registered table.';

-- ----------------------------------------------------------------------------
-- Tree builder helpers
-- ----------------------------------------------------------------------------

-- _build_tree_jsonb
-- Assembles a flat array of tree rows into a nested jsonb. Children of the
-- root id sit at the top level; the root itself is not included.
CREATE OR REPLACE FUNCTION claimius._build_tree_jsonb(
    p_root_id       UUID,
    p_root_type     TEXT,
    p_rows          claimius._tree_row[]
) RETURNS JSONB AS $$
DECLARE
    v_node          claimius._tree_row;
    v_result        JSONB := '{}'::JSONB;
    v_by_parent     JSONB := '{}'::JSONB;
    v_parent_key    TEXT;
    v_node_key      TEXT;
    v_root_key      TEXT;
BEGIN
    IF p_rows IS NULL OR array_length(p_rows, 1) IS NULL THEN
        RETURN '{}'::JSONB;
    END IF;

    v_root_key := claimius.composite_id(p_root_type, p_root_id::TEXT);

    -- Group nodes by parent key
    FOREACH v_node IN ARRAY p_rows LOOP
            IF v_node.parent_id IS NULL THEN
                CONTINUE;
            END IF;
            v_parent_key := claimius.composite_id(v_node.parent_type, v_node.parent_id::TEXT);
            v_node_key := claimius.composite_id(v_node.object_type, v_node.object_id::TEXT);
            v_by_parent := jsonb_set(
                    v_by_parent,
                    ARRAY[v_parent_key],
                    coalesce(v_by_parent -> v_parent_key, '{}'::JSONB) ||
                    jsonb_build_object(v_node_key, jsonb_build_object('level', v_node.level, 'children', '{}'::JSONB)),
                    TRUE
                           );
        END LOOP;

    -- Recursively expand each top level child
    v_result := claimius._expand_children(v_root_key, v_by_parent);
    RETURN v_result;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

COMMENT ON FUNCTION claimius._build_tree_jsonb IS 'Assembles flat node array into nested tree jsonb.';

-- _expand_children
-- Internal helper for _build_tree_jsonb. Recursively expands children of
-- a parent key from the by_parent grouping.
CREATE OR REPLACE FUNCTION claimius._expand_children(p_parent_key TEXT, p_by_parent JSONB)
    RETURNS JSONB AS $$
DECLARE
    v_children      JSONB;
    v_key           TEXT;
    v_value         JSONB;
    v_result        JSONB := '{}'::JSONB;
BEGIN
    v_children := p_by_parent -> p_parent_key;
    IF v_children IS NULL THEN
        RETURN '{}'::JSONB;
    END IF;

    FOR v_key, v_value IN SELECT * FROM jsonb_each(v_children) LOOP
            v_result := v_result || jsonb_build_object(
                    v_key,
                    v_value || jsonb_build_object('children', claimius._expand_children(v_key, p_by_parent))
                                    );
        END LOOP;

    RETURN v_result;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

COMMENT ON FUNCTION claimius._expand_children(TEXT, JSONB) IS 'Internal recursive expander for tree assembly.';

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

-- _app_id_for_row
-- Returns the app_id for a registered row. Reads the row's app_id column
-- if the table has one; otherwise infers it from claimius.get_app_id().
-- Used by every trigger that needs to scope a row's effects to an app.
CREATE OR REPLACE FUNCTION claimius._app_id_for_row(p_object_type TEXT, p_row JSONB)
    RETURNS UUID AS $$
DECLARE
    v_has_app_id BOOLEAN;
BEGIN
    SELECT t.has_app_id INTO v_has_app_id
    FROM claimius.table_info t
    WHERE t.object_type = p_object_type;

    IF v_has_app_id THEN
        RETURN (p_row ->> 'app_id')::UUID;
    END IF;

    RETURN claimius.get_app_id();
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._app_id_for_row(TEXT, JSONB) IS 'Resolves a row''s app_id: from the row when the table has the column, otherwise from claimius.get_app_id().';

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

-- _build_grants_for_object
-- Computes the grants jsonb array for one (user, object) pair by walking
-- claim_objects bound directly or via inheritance trees, applying deny
-- semantics. Returns an empty array if no grants survive.
CREATE OR REPLACE FUNCTION claimius._build_grants_for_object(
    p_user_id       UUID,
    p_app_id        UUID,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS JSONB AS $$
DECLARE
    v_grants        JSONB := '[]'::JSONB;
    v_denies        JSONB := '[]'::JSONB;
    v_row           RECORD;
    v_min_deny      INTEGER;
    v_grant_obj     JSONB;
    v_path_min_deny INTEGER;
BEGIN
    -- Step 1: collect every claim_object binding that could grant this user
    -- access to this object, via any path. Each row carries the claim, its
    -- access_level, is_deny, and the cascaded_from anchor (the object the
    -- claim_object was bound to). For non inherited bindings, cascaded_from
    -- equals the object itself.

    FOR v_row IN
        WITH user_claims AS (
            SELECT uc.claim_id, c.sa_access_level, c.is_deny, c.inherits, c.sa_deleted_at
            FROM claimius.user_claim uc
                     JOIN claimius.claim c ON c.id = uc.claim_id
            WHERE uc.user_id = p_user_id
              AND uc.app_id = p_app_id
              AND uc.sa_deleted_at IS NULL
              AND c.sa_deleted_at IS NULL
        ),
             direct_bindings AS (
                 -- claim_object directly on this object
                 SELECT co.claim_id,
                        p_object_type AS cascaded_from_type,
                        p_object_id   AS cascaded_from_id,
                        'direct'::TEXT AS tree_type,
                        uc.sa_access_level,
                        uc.is_deny
                 FROM claimius.claim_object co
                          JOIN user_claims uc ON uc.claim_id = co.claim_id
                 WHERE co.object_type = p_object_type
                   AND co.object_id = p_object_id
                   AND co.app_id = p_app_id
                   AND co.sa_deleted_at IS NULL
             ),
             cascaded_bindings AS (
                 -- claim_object on an ancestor in any of the trees this object
                 -- participates in. We find ancestors by querying inheritance_info
                 -- rows that contain this object as a descendant.
                 SELECT co.claim_id,
                        co.object_type AS cascaded_from_type,
                        co.object_id   AS cascaded_from_id,
                        ii.type::TEXT  AS tree_type,
                        uc.sa_access_level,
                        uc.is_deny
                 FROM claimius.inheritance_info ii
                          CROSS JOIN claimius.claim_object co
                          JOIN user_claims uc ON uc.claim_id = co.claim_id AND uc.inherits = TRUE
                 WHERE co.app_id = p_app_id
                   AND co.sa_deleted_at IS NULL
                   AND co.inherits = TRUE
                   -- this object must be in the subtree rooted at the binding,
                   -- inside this particular inheritance_info tree
                   AND EXISTS (
                     SELECT 1 FROM claimius.get_subtree(
                                           ii.object_type, ii.root_id, co.object_type, co.object_id
                                   ) s
                     WHERE s.object_type = p_object_type AND s.object_id = p_object_id
                 )
             )
        SELECT * FROM direct_bindings
        UNION ALL
        SELECT * FROM cascaded_bindings
        LOOP
            IF v_row.is_deny THEN
                v_denies := v_denies || jsonb_build_array(jsonb_build_object(
                        'claim_id', v_row.claim_id,
                        'level', v_row.sa_access_level,
                        'cascaded_from', claimius.composite_id(v_row.cascaded_from_type, v_row.cascaded_from_id::TEXT),
                        'tree_type', v_row.tree_type
                                                          ));
            ELSE
                v_grants := v_grants || jsonb_build_array(jsonb_build_object(
                        'claim_id', v_row.claim_id,
                        'level', v_row.sa_access_level,
                        'cascaded_from', claimius.composite_id(v_row.cascaded_from_type, v_row.cascaded_from_id::TEXT),
                        'tree_type', v_row.tree_type
                                                          ));
            END IF;
        END LOOP;

    -- Step 2: filter grants by deny rules. A deny at level N on path P
    -- erases grants on path P at level <= N. Paths are identified by
    -- (cascaded_from, tree_type). Direct denies (tree_type = 'direct',
    -- cascaded_from = the object itself) erase all grants regardless of
    -- path.
    IF jsonb_array_length(v_denies) > 0 THEN
        v_grants := claimius._apply_denies(v_grants, v_denies, p_object_type, p_object_id);
    END IF;

    RETURN v_grants;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._build_grants_for_object IS 'Computes grants jsonb for a (user, object) pair, applying deny semantics.';

-- _apply_denies
-- Filters grants by deny rules. Returns a new jsonb array containing only
-- grants that survive all applicable denies.
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
    v_object_key    TEXT;
    v_blocked       BOOLEAN;
BEGIN
    v_object_key := claimius.composite_id(p_object_type, p_object_id::TEXT);

    -- Deny semantics: deny at level N erases grants at level <= N on the
    -- same path. Lower level number = stronger access. So deny at level 3
    -- (admin) erases admin and stronger; level 8 (guest) deny erases all.
    FOR v_grant IN SELECT * FROM jsonb_array_elements(p_grants) LOOP
            v_blocked := FALSE;
            FOR v_deny IN SELECT * FROM jsonb_array_elements(p_denies) LOOP
                -- A direct deny on the object itself blocks any grant on any path
                -- at level <= deny.level.
                    IF (v_deny ->> 'tree_type') = 'direct' AND (v_deny ->> 'cascaded_from') = v_object_key THEN
                        IF (v_grant ->> 'level')::INTEGER <= (v_deny ->> 'level')::INTEGER THEN
                            v_blocked := TRUE;
                            EXIT;
                        END IF;
                        -- A deny on the same (tree_type, cascaded_from) path blocks
                        -- grants on that same path at level <= deny.level.
                    ELSIF (v_deny ->> 'tree_type') = (v_grant ->> 'tree_type')
                        AND (v_deny ->> 'cascaded_from') = (v_grant ->> 'cascaded_from') THEN
                        IF (v_grant ->> 'level')::INTEGER <= (v_deny ->> 'level')::INTEGER THEN
                            v_blocked := TRUE;
                            EXIT;
                        END IF;
                    END IF;
                END LOOP;
            IF NOT v_blocked THEN
                v_result := v_result || jsonb_build_array(v_grant);
            END IF;
        END LOOP;

    RETURN v_result;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

COMMENT ON FUNCTION claimius._apply_denies IS 'Filters grants by deny rules, returning only surviving grants.';

-- _direct_grant_for_object
-- Determines whether the given user holds direct grant on the given object
-- via sa_created_by chain. A direct grant exists when there is a row in
-- the registered table with this id whose sa_created_by maps (via user_claim
-- lookup) to this user_id and app_id, AND no deny at level 0 on this object
-- blocks them.
CREATE OR REPLACE FUNCTION claimius._direct_grant_for_object(
    p_user_id       UUID,
    p_app_id        UUID,
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
    v_creator_app   UUID;
    v_has_deny      BOOLEAN;
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    -- pull the row's sa_created_by
    v_sql := format('SELECT sa_created_by FROM %I.%I WHERE id = $1 AND sa_deleted_at IS NULL', v_schema, v_table);
    EXECUTE v_sql INTO v_creator_uc_id USING p_object_id;

    IF v_creator_uc_id IS NULL THEN
        RETURN FALSE;
    END IF;

    -- bootstrap path: sa_created_by may be a samna_user.user_id during init
    -- (no user_claim exists yet). Match directly first.
    IF v_creator_uc_id = p_user_id THEN
        -- still need to check for deny at level 0
    ELSE
        SELECT uc.user_id, uc.app_id INTO v_creator_user, v_creator_app
        FROM claimius.user_claim uc
        WHERE uc.id = v_creator_uc_id AND uc.sa_deleted_at IS NULL;

        IF v_creator_user IS NULL OR v_creator_user <> p_user_id OR v_creator_app <> p_app_id THEN
            RETURN FALSE;
        END IF;
    END IF;

    -- check for level 0 deny on this object
    SELECT EXISTS (
        SELECT 1
        FROM claimius.user_claim uc
                 JOIN claimius.claim c ON c.id = uc.claim_id
                 JOIN claimius.claim_object co ON co.claim_id = c.id
        WHERE uc.user_id = p_user_id
          AND uc.app_id = p_app_id
          AND uc.sa_deleted_at IS NULL
          AND c.is_deny = TRUE
          AND c.sa_access_level <= 0
          AND c.sa_deleted_at IS NULL
          AND co.app_id = p_app_id
          AND co.sa_deleted_at IS NULL
          AND (
            (co.object_type = p_object_type AND co.object_id = p_object_id)
                OR (co.inherits = TRUE AND c.inherits = TRUE AND EXISTS (
                SELECT 1 FROM claimius.inheritance_info ii
                WHERE EXISTS (
                    SELECT 1 FROM claimius.get_subtree(ii.object_type, ii.root_id, co.object_type, co.object_id) s
                    WHERE s.object_type = p_object_type AND s.object_id = p_object_id
                )
            ))
            )
    ) INTO v_has_deny;

    RETURN NOT v_has_deny;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._direct_grant_for_object IS 'Determines if user has direct (creator) grant, accounting for level 0 denies.';

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

    sa_link := NULL;  -- apps may set this via update_object_metadata; left NULL by default
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._denormalize_object IS 'Reads name/description/owner/location/root from a registered row.';

-- recompute_user_object
-- The single function that recomputes the user_object row for one
-- (app_id, user_id, object_type, object_id) tuple. It handles insert,
-- update, and delete based on whether grants survive. Also maintains the
-- companion object_users and user_users rows.
CREATE OR REPLACE FUNCTION claimius.recompute_user_object(
    p_app_id        UUID,
    p_user_id       UUID,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS VOID AS $$
DECLARE
    v_grants        JSONB;
    v_direct        BOOLEAN;
    v_id            TEXT;
    v_eff_level     INTEGER;
    v_levels        INTEGER[];
    v_norm          RECORD;
    v_search_vec    TSVECTOR;
BEGIN
    v_id := claimius.composite_id(p_app_id::TEXT, p_user_id::TEXT, p_object_type, p_object_id::TEXT);

    v_grants := claimius._build_grants_for_object(p_user_id, p_app_id, p_object_type, p_object_id);
    v_direct := claimius._direct_grant_for_object(p_user_id, p_app_id, p_object_type, p_object_id);

    -- No surviving access at all: delete the user_object row and companions
    IF jsonb_array_length(v_grants) = 0 AND NOT v_direct THEN
        DELETE FROM claimius.user_object WHERE id = v_id;
        DELETE FROM claimius.object_users
        WHERE app_id = p_app_id AND object_id = p_object_id
          AND object_type = p_object_type AND user_id = p_user_id;
        PERFORM claimius._refresh_user_users(p_app_id, p_user_id);
        RETURN;
    END IF;

    -- Compute effective level
    IF v_direct THEN
        v_eff_level := 0;
    ELSE
        SELECT array_agg((g ->> 'level')::INTEGER) INTO v_levels
        FROM jsonb_array_elements(v_grants) g;
        v_eff_level := (SELECT min(x) FROM unnest(v_levels) AS x);
    END IF;

    -- Pull denormalized fields
    SELECT * INTO v_norm FROM claimius._denormalize_object(p_object_type, p_object_id);

    v_search_vec :=
            setweight(to_tsvector('simple', coalesce(v_norm.sa_name, '')), 'A') ||
            setweight(to_tsvector('simple', coalesce(v_norm.sa_description, '')), 'B');

    INSERT INTO claimius.user_object (
        id, app_id, user_id, object_id, object_type,
        sa_access_level, grants, direct_grant,
        sa_owner_id, sa_location_id, sa_root_id,
        sa_name, sa_description, sa_link, search_vector
    ) VALUES (
                 v_id, p_app_id, p_user_id, p_object_id, p_object_type,
                 v_eff_level, v_grants, v_direct,
                 v_norm.sa_owner_id, v_norm.sa_location_id, v_norm.sa_root_id,
                 v_norm.sa_name, v_norm.sa_description, v_norm.sa_link, v_search_vec
             )
    ON CONFLICT (id) DO UPDATE SET
                                   sa_access_level = EXCLUDED.sa_access_level,
                                   grants = EXCLUDED.grants,
                                   direct_grant = EXCLUDED.direct_grant,
                                   sa_owner_id = EXCLUDED.sa_owner_id,
                                   sa_location_id = EXCLUDED.sa_location_id,
                                   sa_root_id = EXCLUDED.sa_root_id,
                                   sa_name = EXCLUDED.sa_name,
                                   sa_description = EXCLUDED.sa_description,
                                   sa_link = EXCLUDED.sa_link,
                                   search_vector = EXCLUDED.search_vector,
                                   sa_updated_at = now();

    -- Maintain object_users
    INSERT INTO claimius.object_users (id, app_id, object_id, object_type, user_id)
    VALUES (
               claimius.composite_id(p_app_id::TEXT, p_object_type, p_object_id::TEXT, p_user_id::TEXT),
               p_app_id, p_object_id, p_object_type, p_user_id
           )
    ON CONFLICT (id) DO UPDATE SET sa_updated_at = now();

    PERFORM claimius._refresh_user_users(p_app_id, p_user_id);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.recompute_user_object IS 'Recomputes user_object plus companions for one (app, user, object) tuple.';

-- _refresh_user_users
-- Recomputes user_users entries for one (app_id, viewer) pair from
-- object_users.
CREATE OR REPLACE FUNCTION claimius._refresh_user_users(p_app_id UUID, p_viewer_id UUID)
    RETURNS VOID AS $$
BEGIN
    -- Remove existing rows for this viewer
    DELETE FROM claimius.user_users
    WHERE app_id = p_app_id AND viewer_id = p_viewer_id;

    -- Insert one row per other user the viewer shares any object with
    INSERT INTO claimius.user_users (id, app_id, viewer_id, target_user_id, sharing_object_count, first_shared_at, last_shared_at)
    SELECT
        claimius.composite_id(p_app_id::TEXT, p_viewer_id::TEXT, target.user_id::TEXT),
        p_app_id,
        p_viewer_id,
        target.user_id,
        count(*),
        min(target.sa_created_at),
        max(target.sa_updated_at)
    FROM claimius.object_users viewer
             JOIN claimius.object_users target
                  ON target.app_id = viewer.app_id
                      AND target.object_id = viewer.object_id
                      AND target.object_type = viewer.object_type
                      AND target.user_id <> viewer.user_id
    WHERE viewer.app_id = p_app_id AND viewer.user_id = p_viewer_id
    GROUP BY target.user_id;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._refresh_user_users IS 'Rebuilds user_users rows for one (app, viewer).';

-- _affected_users_for_object
-- Returns the list of users whose access to the given object could change.
-- Includes any user holding a relevant claim plus any user already in
-- object_users for that object.
CREATE OR REPLACE FUNCTION claimius._affected_users_for_object(
    p_app_id        UUID,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS TABLE(user_id UUID) AS $$
BEGIN
    RETURN QUERY
        WITH all_users AS (
            -- existing object_users
            SELECT ou.user_id FROM claimius.object_users ou
            WHERE ou.app_id = p_app_id AND ou.object_id = p_object_id AND ou.object_type = p_object_type

            UNION

            -- users holding any user_claim in this app whose claim is bound
            -- to this object directly or via inheritance
            SELECT uc.user_id
            FROM claimius.user_claim uc
                     JOIN claimius.claim_object co ON co.claim_id = uc.claim_id
            WHERE uc.app_id = p_app_id
              AND uc.sa_deleted_at IS NULL
              AND co.app_id = p_app_id
              AND co.sa_deleted_at IS NULL
              AND (
                (co.object_type = p_object_type AND co.object_id = p_object_id)
                    OR EXISTS (
                    SELECT 1 FROM claimius.inheritance_info ii
                                      JOIN claimius.get_subtree(ii.object_type, ii.root_id, co.object_type, co.object_id) s ON TRUE
                    WHERE s.object_type = p_object_type AND s.object_id = p_object_id
                )
                )
        )
        SELECT DISTINCT au.user_id FROM all_users au;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._affected_users_for_object IS 'Users whose access to the object could change.';

-- ============================================================================
-- Ancestor walks
-- ============================================================================
-- Three helpers walk an object's structural ancestors. Each returns rows
-- shaped (object_type TEXT, object_id UUID, hop INTEGER) with hop = 0 for
-- the object itself, hop = 1 for its direct parent in that tree, etc.
-- get_ancestors combines all three and tags each row with the tree_type.

-- get_owner_ancestors
-- Walks the ownership chain. For an organization, walks org parents via
-- sa_owner_id. For any other registered object with sa_owner_id, jumps to
-- the owning org and walks the org chain from there.
CREATE OR REPLACE FUNCTION claimius.get_owner_ancestors(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS TABLE(object_type TEXT, object_id UUID, hop INTEGER) AS $$
DECLARE
    v_owner_id UUID;
    v_info     RECORD;
BEGIN
    -- Always include the object itself at hop 0
    RETURN QUERY SELECT p_object_type, p_object_id, 0;

    SELECT t.has_sa_owner_id INTO v_info
    FROM claimius.table_info t
    WHERE t.object_type = p_object_type;

    IF v_info IS NULL OR NOT v_info.has_sa_owner_id THEN
        RETURN;
    END IF;

    IF p_object_type = 'claimius.organization' THEN
        -- Walk org ancestors. Base case: the starting object itself.
        -- Recursion: jump to the parent (sa_owner_id) unless we're already
        -- at the self referential root. Then we trim hop 0 from the result
        -- (the object itself was already returned at line 999).
        RETURN QUERY
            WITH RECURSIVE chain AS (
                SELECT o.id, o.sa_owner_id, 0 AS lvl
                FROM claimius.organization o
                WHERE o.id = p_object_id
                  AND o.sa_deleted_at IS NULL
                UNION ALL
                SELECT parent.id, parent.sa_owner_id, c.lvl + 1
                FROM claimius.organization parent
                         JOIN chain c ON parent.id = c.sa_owner_id
                WHERE parent.sa_deleted_at IS NULL
                  AND c.id <> c.sa_owner_id  -- stop once we've emitted a root
            )
            SELECT 'claimius.organization'::TEXT, c.id, c.lvl
            FROM chain c
            WHERE c.lvl > 0;
    ELSE
        -- Read the object's sa_owner_id and walk the org chain from there
        EXECUTE format(
                'SELECT sa_owner_id FROM %I.%I WHERE id = $1',
                split_part(p_object_type, '.', 1),
                split_part(p_object_type, '.', 2)
                ) INTO v_owner_id USING p_object_id;

        IF v_owner_id IS NULL THEN
            RETURN;
        END IF;

        -- Hop 1 is the direct owning org
        RETURN QUERY SELECT 'claimius.organization'::TEXT, v_owner_id, 1;

        -- Walk the org's ancestor chain
        RETURN QUERY
            WITH RECURSIVE chain AS (
                SELECT o.id, o.sa_owner_id, 2 AS lvl
                FROM claimius.organization o
                WHERE o.id = v_owner_id
                  AND o.sa_deleted_at IS NULL
                  AND o.id <> o.sa_owner_id
                UNION ALL
                SELECT parent.id, parent.sa_owner_id, c.lvl + 1
                FROM claimius.organization parent
                         JOIN chain c ON parent.id = c.sa_owner_id
                WHERE parent.sa_deleted_at IS NULL
                  AND parent.id <> parent.sa_owner_id
            )
            SELECT 'claimius.organization'::TEXT, c.sa_owner_id, c.lvl
            FROM chain c;
    END IF;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_owner_ancestors IS 'Owner chain ancestors of an object, hop 0 = self.';

-- get_location_ancestors
-- Walks the location chain. For an object with sa_location_id, hop 1 is the
-- direct location, hop 2 is its parent location (via location.sa_parent_id),
-- and so on. At each location encountered, also yields that location's
-- owning org plus the org's ancestor chain (so a claim on a parent org of
-- a location grants access via the location too).
CREATE OR REPLACE FUNCTION claimius.get_location_ancestors(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS TABLE(object_type TEXT, object_id UUID, hop INTEGER) AS $$
DECLARE
    v_location_id UUID;
    v_info        RECORD;
BEGIN
    RETURN QUERY SELECT p_object_type, p_object_id, 0;

    SELECT t.has_sa_location_id INTO v_info
    FROM claimius.table_info t
    WHERE t.object_type = p_object_type;

    IF v_info IS NULL OR NOT v_info.has_sa_location_id THEN
        RETURN;
    END IF;

    IF p_object_type = 'claimius.location' THEN
        v_location_id := p_object_id;
    ELSE
        EXECUTE format(
                'SELECT sa_location_id FROM %I.%I WHERE id = $1',
                split_part(p_object_type, '.', 1),
                split_part(p_object_type, '.', 2)
                ) INTO v_location_id USING p_object_id;
    END IF;

    IF v_location_id IS NULL THEN
        RETURN;
    END IF;

    -- Walk the location parent chain. hop offset: if starting object IS a
    -- location, we treat it as hop 0 (already returned), so its parent is
    -- hop 1. Otherwise the direct location is hop 1 and its parent is hop 2.
    RETURN QUERY
        WITH RECURSIVE loc_chain AS (
            SELECT l.id, l.sa_parent_id, l.sa_owner_id,
                   CASE WHEN p_object_type = 'claimius.location' THEN 0 ELSE 1 END AS lvl
            FROM claimius.location l
            WHERE l.id = v_location_id
              AND l.sa_deleted_at IS NULL
            UNION ALL
            SELECT parent.id, parent.sa_parent_id, parent.sa_owner_id, lc.lvl + 1
            FROM claimius.location parent
                     JOIN loc_chain lc ON parent.id = lc.sa_parent_id
            WHERE parent.sa_deleted_at IS NULL
              AND parent.id <> lc.id
        )
        -- Yield each location in the chain (skip hop 0 if we already returned the object)
        SELECT 'claimius.location'::TEXT, lc.id, lc.lvl
        FROM loc_chain lc
        WHERE lc.lvl > 0 OR p_object_type <> 'claimius.location';

    -- For each location in the chain, also yield its owning org and the
    -- org's ancestor chain. We use the location's own hop as the base,
    -- then add 1 for the owning org and continue upward.
    RETURN QUERY
        WITH RECURSIVE loc_chain AS (
            SELECT l.id, l.sa_parent_id, l.sa_owner_id,
                   CASE WHEN p_object_type = 'claimius.location' THEN 0 ELSE 1 END AS lvl
            FROM claimius.location l
            WHERE l.id = v_location_id
              AND l.sa_deleted_at IS NULL
            UNION ALL
            SELECT parent.id, parent.sa_parent_id, parent.sa_owner_id, lc.lvl + 1
            FROM claimius.location parent
                     JOIN loc_chain lc ON parent.id = lc.sa_parent_id
            WHERE parent.sa_deleted_at IS NULL
              AND parent.id <> lc.id
        ),
                       org_chain AS (
                           SELECT lc.sa_owner_id AS id, lc.lvl + 1 AS lvl
                           FROM loc_chain lc
                           WHERE lc.sa_owner_id IS NOT NULL
                           UNION ALL
                           SELECT parent.sa_owner_id, oc.lvl + 1
                           FROM claimius.organization parent
                                    JOIN org_chain oc ON parent.id = oc.id
                           WHERE parent.sa_deleted_at IS NULL
                             AND parent.id <> parent.sa_owner_id
                       )
        SELECT 'claimius.organization'::TEXT, oc.id, oc.lvl
        FROM org_chain oc
        WHERE oc.id IS NOT NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_location_ancestors IS 'Location chain ancestors of an object plus each location''s owning org chain.';

-- get_parenthood_ancestors
-- Walks same-table parents via sa_parent_id, when the object_type has it.
CREATE OR REPLACE FUNCTION claimius.get_parenthood_ancestors(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS TABLE(object_type TEXT, object_id UUID, hop INTEGER) AS $$
DECLARE
    v_info  RECORD;
    v_sql   TEXT;
BEGIN
    RETURN QUERY SELECT p_object_type, p_object_id, 0;

    SELECT t.has_sa_parent_id INTO v_info
    FROM claimius.table_info t
    WHERE t.object_type = p_object_type;

    IF v_info IS NULL OR NOT v_info.has_sa_parent_id THEN
        RETURN;
    END IF;

    v_sql := format($f$
        WITH RECURSIVE chain AS (
            SELECT t.id, t.sa_parent_id, 1 AS lvl
            FROM %I.%I t
            WHERE t.id = $1
              AND t.id <> t.sa_parent_id
              AND t.sa_deleted_at IS NULL
            UNION ALL
            SELECT parent.id, parent.sa_parent_id, c.lvl + 1
            FROM %I.%I parent
            JOIN chain c ON parent.id = c.sa_parent_id
            WHERE parent.id <> parent.sa_parent_id
              AND parent.sa_deleted_at IS NULL
        )
        SELECT %L::TEXT, chain.sa_parent_id, chain.lvl
        FROM chain
    $f$,
                    split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2),
                    split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2),
                    p_object_type
             );

    RETURN QUERY EXECUTE v_sql USING p_object_id;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_parenthood_ancestors IS 'Same-table sa_parent_id ancestors, hop 0 = self.';

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

COMMENT ON FUNCTION claimius.get_ancestors IS 'Combined ancestor walk across owner, location, and parenthood trees.';