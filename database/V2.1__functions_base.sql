-- ============================================================================
-- Claimius V2.1 Base Functions
-- ----------------------------------------------------------------------------
-- The innermost utility functions. These have no dependencies on other
-- claimius functions and are called by everything else.
-- ============================================================================

-- composite_id
-- Joins arbitrary string parts with ":" to form a deterministic composite id.
-- Same inputs produce the same id on every node, so prophet and disciple
-- converge on the same row identity.
CREATE OR REPLACE FUNCTION claimius.composite_id(VARIADIC parts TEXT[])
    RETURNS TEXT AS $$
SELECT array_to_string(parts, ':');
$$ LANGUAGE sql IMMUTABLE;

COMMENT ON FUNCTION claimius.composite_id(TEXT[]) IS 'Deterministic colon joined id. Same inputs always produce same id.';

-- decompose_id
-- Splits a composite id back into its parts. Inverse of composite_id.
CREATE OR REPLACE FUNCTION claimius.decompose_id(p_id TEXT)
    RETURNS TEXT[] AS $$
SELECT string_to_array(p_id, ':');
$$ LANGUAGE sql IMMUTABLE;

COMMENT ON FUNCTION claimius.decompose_id(TEXT) IS 'Inverse of composite_id. Splits on colon.';

-- split_object_type
-- Splits a schema-qualified table name like 'claimius.samna_app' into
-- ['claimius', 'samna_app']. If unqualified, returns ['public', name].
-- This is for object_type strings (schema.table). For composite ids
-- (object_type:uuid), use decompose_id.
CREATE OR REPLACE FUNCTION claimius.split_object_type(p_object_type TEXT)
    RETURNS TEXT[] AS $$
SELECT CASE
           WHEN position('.' IN p_object_type) = 0 THEN ARRAY['public', p_object_type]
           ELSE ARRAY[split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2)]
           END;
$$ LANGUAGE sql IMMUTABLE;

COMMENT ON FUNCTION claimius.split_object_type(TEXT) IS 'Splits a schema.table string into [schema, table]. Defaults to public schema.';

-- update_timestamp
-- Trigger function that sets sa_updated_at to now() on every UPDATE.
CREATE OR REPLACE FUNCTION claimius.update_timestamp()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.sa_updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_timestamp() IS 'Trigger function. Sets sa_updated_at = now() on UPDATE.';

-- get_table_columns
-- Returns the column names for a (schema qualified) table.
CREATE OR REPLACE FUNCTION claimius.get_table_columns(p_object_type TEXT)
    RETURNS TABLE(column_name TEXT) AS $$
DECLARE
    v_schema TEXT;
    v_table TEXT;
    v_parts TEXT[];
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];
    RETURN QUERY
        SELECT c.column_name::TEXT
        FROM information_schema.columns c
        WHERE c.table_schema = v_schema AND c.table_name = v_table;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_table_columns(TEXT) IS 'Returns column names for a schema qualified table.';

-- table_has_column
-- Helper that returns true if the given object_type has the given column.
CREATE OR REPLACE FUNCTION claimius.table_has_column(p_object_type TEXT, p_column TEXT)
    RETURNS BOOLEAN AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM claimius.get_table_columns(p_object_type) WHERE column_name = p_column
    );
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.table_has_column(TEXT, TEXT) IS 'True if the given table has the given column.';

-- write_audit
-- Inserts a row into claimius.audit. Used by audit triggers and by error
-- paths inside other triggers.
-- p_sa_created_by may be NULL for events with no clear actor (system
-- triggers, replay, denied access attempts).
CREATE OR REPLACE FUNCTION claimius.write_audit(
    p_app_id        UUID,
    p_operation     TEXT,
    p_type          TEXT,
    p_object_type   TEXT,
    p_object_id     UUID,
    p_sa_owner_id   UUID,
    p_sa_created_by UUID DEFAULT NULL,
    p_message       TEXT DEFAULT ''
) RETURNS VOID AS $$
BEGIN
    INSERT INTO claimius.audit (
        app_id, operation, type, object_type, object_id,
        sa_owner_id, sa_created_by, message
    ) VALUES (
                 p_app_id, p_operation, p_type, p_object_type, p_object_id,
                 p_sa_owner_id, p_sa_created_by, p_message
             );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.write_audit IS 'Inserts a row into the audit log.';

-- in_replay_mode
-- Returns true if claimius.replay_mode is set on this session. Used by
-- triggers to short circuit during disciple replay.
CREATE OR REPLACE FUNCTION claimius.in_replay_mode()
    RETURNS BOOLEAN AS $$
BEGIN
    RETURN coalesce(current_setting('claimius.replay_mode', TRUE), 'false') = 'true';
EXCEPTION WHEN OTHERS THEN
    RETURN FALSE;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.in_replay_mode() IS 'True when claimius.replay_mode session GUC is "true". Triggers short circuit.';

-- get_subtree
-- Returns (object_type, object_id) pairs from an inheritance_info tree,
-- starting at (p_start_type, p_start_id) and descending.
--   - If start is null: returns everything below the root, root itself excluded.
--   - If start equals the root: returns the root plus the entire tree below it.
--   - Otherwise: looks up the start node inside the tree, returns the start
--     plus its descendants.
-- The root identity is (p_object_type, p_root_id) and is not stored inside
-- the tree jsonb (only descendants are), so the start-equals-root case has
-- to be handled explicitly.
CREATE OR REPLACE FUNCTION claimius.get_subtree(
    p_object_type   TEXT,
    p_root_id       UUID,
    p_start_type    TEXT DEFAULT NULL,
    p_start_id      UUID DEFAULT NULL
) RETURNS TABLE(object_type TEXT, object_id UUID) AS $$
DECLARE
    v_tree          JSONB;
    v_subtree       JSONB;
    v_start_key     TEXT;
BEGIN
    SELECT i.tree INTO v_tree
    FROM claimius.inheritance_info i
    WHERE i.object_type = p_object_type AND i.root_id = p_root_id;

    IF v_tree IS NULL THEN
        RETURN;
    END IF;

    IF p_start_type IS NULL OR p_start_id IS NULL THEN
        v_subtree := v_tree;
    ELSIF p_start_type = p_object_type AND p_start_id = p_root_id THEN
        -- Start at the root: emit the root identity, then walk the full tree.
        RETURN QUERY SELECT p_start_type, p_start_id;
        v_subtree := v_tree;
    ELSE
        v_start_key := claimius.composite_id(p_start_type, p_start_id::TEXT);
        v_subtree := claimius._find_node(v_tree, v_start_key);
        IF v_subtree IS NULL THEN
            RETURN;
        END IF;
        -- include the start node itself
        RETURN QUERY SELECT p_start_type, p_start_id;
        v_subtree := v_subtree -> 'children';
    END IF;

    RETURN QUERY
        SELECT
            (claimius.decompose_id(key))[1]::TEXT,
            (claimius.decompose_id(key))[2]::UUID
        FROM claimius._walk_tree(v_subtree);
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_subtree IS 'Returns (object_type, object_id) descendants from a position in a cached tree.';

-- _find_node
-- Internal helper. Recursively searches a tree jsonb for the node with the
-- given composite key. Returns its value (with level and children) or NULL.
CREATE OR REPLACE FUNCTION claimius._find_node(p_tree JSONB, p_key TEXT)
    RETURNS JSONB AS $$
DECLARE
    v_key       TEXT;
    v_value     JSONB;
    v_found     JSONB;
BEGIN
    IF p_tree IS NULL OR jsonb_typeof(p_tree) <> 'object' THEN
        RETURN NULL;
    END IF;

    -- direct hit
    IF p_tree ? p_key THEN
        RETURN p_tree -> p_key;
    END IF;

    -- recurse into children of each node
    FOR v_key, v_value IN SELECT * FROM jsonb_each(p_tree) LOOP
            IF v_value ? 'children' THEN
                v_found := claimius._find_node(v_value -> 'children', p_key);
                IF v_found IS NOT NULL THEN
                    RETURN v_found;
                END IF;
            END IF;
        END LOOP;

    RETURN NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._find_node(JSONB, TEXT) IS 'Internal. Recursive lookup of a node by key in a tree jsonb.';

-- _walk_tree
-- Internal helper. Returns every key in a tree jsonb, recursively.
CREATE OR REPLACE FUNCTION claimius._walk_tree(p_tree JSONB)
    RETURNS TABLE(key TEXT) AS $$
DECLARE
    v_key       TEXT;
    v_value     JSONB;
BEGIN
    IF p_tree IS NULL OR jsonb_typeof(p_tree) <> 'object' THEN
        RETURN;
    END IF;

    FOR v_key, v_value IN SELECT * FROM jsonb_each(p_tree) LOOP
            RETURN NEXT;
            IF v_value ? 'children' THEN
                RETURN QUERY SELECT * FROM claimius._walk_tree(v_value -> 'children');
            END IF;
        END LOOP;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._walk_tree(JSONB) IS 'Internal. Recursively yields every key in a tree jsonb.';

-- set_subtree
-- Replaces the subtree at (p_start_type, p_start_id) inside the tree for
-- (p_object_type, p_root_id, p_type) with p_new_subtree. If the start node
-- is not found, raises an error: callers must use add_to_tree to insert a
-- node that does not exist yet.
CREATE OR REPLACE FUNCTION claimius.set_subtree(
    p_object_type   TEXT,
    p_root_id       UUID,
    p_type          claimius.tree_type,
    p_start_type    TEXT,
    p_start_id      UUID,
    p_new_children  JSONB,
    p_level         INTEGER DEFAULT NULL
) RETURNS VOID AS $$
DECLARE
    v_tree          JSONB;
    v_start_key     TEXT;
    v_node_value    JSONB;
    v_new_tree      JSONB;
BEGIN
    SELECT i.tree INTO v_tree
    FROM claimius.inheritance_info i
    WHERE i.object_type = p_object_type
      AND i.root_id = p_root_id
      AND i.type = p_type
        FOR UPDATE;

    IF v_tree IS NULL THEN
        RAISE EXCEPTION 'inheritance_info row missing for (%, %, %)', p_object_type, p_root_id, p_type;
    END IF;

    v_start_key := claimius.composite_id(p_start_type, p_start_id::TEXT);

    -- compose new node value preserving level if not given
    IF p_level IS NULL THEN
        v_node_value := claimius._find_node(v_tree, v_start_key);
        IF v_node_value IS NULL THEN
            v_node_value := jsonb_build_object('level', 1, 'children', p_new_children);
        ELSE
            v_node_value := v_node_value || jsonb_build_object('children', p_new_children);
        END IF;
    ELSE
        v_node_value := jsonb_build_object('level', p_level, 'children', p_new_children);
    END IF;

    v_new_tree := claimius._replace_node(v_tree, v_start_key, v_node_value);

    UPDATE claimius.inheritance_info
    SET tree = v_new_tree, sa_updated_at = now()
    WHERE object_type = p_object_type
      AND root_id = p_root_id
      AND type = p_type;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.set_subtree IS 'Replaces children at a position in a cached tree. Surgical update.';

-- _replace_node
-- Internal helper. Returns a new tree jsonb with the value at p_key
-- replaced with p_new_value. Recursive search.
CREATE OR REPLACE FUNCTION claimius._replace_node(p_tree JSONB, p_key TEXT, p_new_value JSONB)
    RETURNS JSONB AS $$
DECLARE
    v_key       TEXT;
    v_value     JSONB;
    v_result    JSONB := '{}'::JSONB;
    v_replaced  BOOLEAN := FALSE;
BEGIN
    IF p_tree IS NULL OR jsonb_typeof(p_tree) <> 'object' THEN
        RETURN p_tree;
    END IF;

    FOR v_key, v_value IN SELECT * FROM jsonb_each(p_tree) LOOP
            IF v_key = p_key THEN
                v_result := v_result || jsonb_build_object(v_key, p_new_value);
                v_replaced := TRUE;
            ELSIF v_value ? 'children' THEN
                v_result := v_result || jsonb_build_object(
                        v_key,
                        v_value || jsonb_build_object('children', claimius._replace_node(v_value -> 'children', p_key, p_new_value))
                                        );
            ELSE
                v_result := v_result || jsonb_build_object(v_key, v_value);
            END IF;
        END LOOP;

    RETURN v_result;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

COMMENT ON FUNCTION claimius._replace_node(JSONB, TEXT, JSONB) IS 'Internal. Returns tree with value at key replaced.';

-- add_to_tree
-- Adds a node under a parent inside a cached tree. Parent must exist (or be
-- the implicit root, when p_parent_type and p_parent_id are NULL).
CREATE OR REPLACE FUNCTION claimius.add_to_tree(
    p_object_type   TEXT,
    p_root_id       UUID,
    p_type          claimius.tree_type,
    p_parent_type   TEXT,
    p_parent_id     UUID,
    p_node_type     TEXT,
    p_node_id       UUID,
    p_level         INTEGER
) RETURNS VOID AS $$
DECLARE
    v_tree          JSONB;
    v_parent_key    TEXT;
    v_node_key      TEXT;
    v_node_value    JSONB;
    v_parent_node   JSONB;
    v_new_tree      JSONB;
    v_parent_children JSONB;
BEGIN
    SELECT i.tree INTO v_tree
    FROM claimius.inheritance_info i
    WHERE i.object_type = p_object_type
      AND i.root_id = p_root_id
      AND i.type = p_type
        FOR UPDATE;

    IF v_tree IS NULL THEN
        RAISE EXCEPTION 'inheritance_info row missing for (%, %, %)', p_object_type, p_root_id, p_type;
    END IF;

    v_node_key := claimius.composite_id(p_node_type, p_node_id::TEXT);
    v_node_value := jsonb_build_object('level', p_level, 'children', '{}'::JSONB);

    IF p_parent_type IS NULL OR p_parent_id IS NULL THEN
        -- adding to the implicit root level (top of jsonb)
        v_new_tree := v_tree || jsonb_build_object(v_node_key, v_node_value);
    ELSE
        v_parent_key := claimius.composite_id(p_parent_type, p_parent_id::TEXT);
        v_parent_node := claimius._find_node(v_tree, v_parent_key);
        IF v_parent_node IS NULL THEN
            RAISE EXCEPTION 'parent node % not found in tree (%, %, %)', v_parent_key, p_object_type, p_root_id, p_type;
        END IF;
        v_parent_children := coalesce(v_parent_node -> 'children', '{}'::JSONB);
        v_parent_children := v_parent_children || jsonb_build_object(v_node_key, v_node_value);
        v_new_tree := claimius._replace_node(v_tree, v_parent_key, v_parent_node || jsonb_build_object('children', v_parent_children));
    END IF;

    UPDATE claimius.inheritance_info
    SET tree = v_new_tree, sa_updated_at = now()
    WHERE object_type = p_object_type
      AND root_id = p_root_id
      AND type = p_type;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.add_to_tree IS 'Adds a node under a parent in a cached tree.';

-- remove_from_tree
-- Removes a node and all its descendants from a cached tree.
CREATE OR REPLACE FUNCTION claimius.remove_from_tree(
    p_object_type   TEXT,
    p_root_id       UUID,
    p_type          claimius.tree_type,
    p_node_type     TEXT,
    p_node_id       UUID
) RETURNS VOID AS $$
DECLARE
    v_tree          JSONB;
    v_node_key      TEXT;
    v_new_tree      JSONB;
BEGIN
    SELECT i.tree INTO v_tree
    FROM claimius.inheritance_info i
    WHERE i.object_type = p_object_type
      AND i.root_id = p_root_id
      AND i.type = p_type
        FOR UPDATE;

    IF v_tree IS NULL THEN
        RETURN;
    END IF;

    v_node_key := claimius.composite_id(p_node_type, p_node_id::TEXT);
    v_new_tree := claimius._delete_node(v_tree, v_node_key);

    UPDATE claimius.inheritance_info
    SET tree = v_new_tree, sa_updated_at = now()
    WHERE object_type = p_object_type
      AND root_id = p_root_id
      AND type = p_type;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.remove_from_tree IS 'Removes a node and its descendants from a cached tree.';

-- _delete_node
-- Internal helper. Returns a new tree jsonb with the entry at p_key removed
-- wherever it occurs.
CREATE OR REPLACE FUNCTION claimius._delete_node(p_tree JSONB, p_key TEXT)
    RETURNS JSONB AS $$
DECLARE
    v_key       TEXT;
    v_value     JSONB;
    v_result    JSONB := '{}'::JSONB;
BEGIN
    IF p_tree IS NULL OR jsonb_typeof(p_tree) <> 'object' THEN
        RETURN p_tree;
    END IF;

    FOR v_key, v_value IN SELECT * FROM jsonb_each(p_tree) LOOP
            IF v_key = p_key THEN
                CONTINUE;
            ELSIF v_value ? 'children' THEN
                v_result := v_result || jsonb_build_object(
                        v_key,
                        v_value || jsonb_build_object('children', claimius._delete_node(v_value -> 'children', p_key))
                                        );
            ELSE
                v_result := v_result || jsonb_build_object(v_key, v_value);
            END IF;
        END LOOP;

    RETURN v_result;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

COMMENT ON FUNCTION claimius._delete_node(JSONB, TEXT) IS 'Internal. Returns tree with key removed wherever it appears.';