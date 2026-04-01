DROP FUNCTION IF EXISTS samna_auth.create_system_claim CASCADE;

CREATE OR REPLACE FUNCTION samna_auth.create_claim(
    p_name TEXT,
    p_description TEXT,
    p_sa_access_level INTEGER,
    p_sa_owner_id UUID,
    p_sa_root_id UUID,
    p_sa_created_by UUID,
    p_inherits BOOLEAN DEFAULT TRUE,
    p_type samna_auth.claim_type DEFAULT 'user'
) RETURNS UUID AS $$
DECLARE
    v_claim_id UUID;
BEGIN
    INSERT INTO samna_auth.claim (
        name,
        description,
        sa_access_level,
        sa_owner_id,
        sa_root_id,
        sa_created_by,
        inherits,
        type
    ) VALUES (
        p_name,
        p_description,
        p_sa_access_level,
        p_sa_owner_id,
        p_sa_root_id,
        p_sa_created_by,
        p_inherits,
        p_type
    )
    RETURNING id INTO v_claim_id;

    RETURN v_claim_id;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION samna_auth.assign_claim_user(
    p_claim_id UUID,
    p_user_id UUID,
    p_sa_owner_id UUID,
    p_sa_created_by UUID,
    p_reason TEXT DEFAULT NULL,
    p_starts_at TIMESTAMP DEFAULT NULL,
    p_ends_at TIMESTAMP DEFAULT NULL
) RETURNS UUID AS $$
DECLARE
    v_user_claim_id UUID;
BEGIN
    INSERT INTO samna_auth.user_claim (
        claim_id,
        user_id,
        sa_owner_id,
        sa_created_by,
        reason,
        starts_at,
        ends_at
    ) VALUES (
        p_claim_id,
        p_user_id,
        p_sa_owner_id,
        p_sa_created_by,
        p_reason,
        p_starts_at,
        p_ends_at
    )
    RETURNING id INTO v_user_claim_id;

    RETURN v_user_claim_id;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION samna_auth.assign_claim_object(
    p_claim_id UUID,
    p_object_id UUID,
    p_object_type TEXT,
    p_sa_owner_id UUID,
    p_sa_root_id UUID,
    p_sa_created_by UUID,
    p_level INTEGER DEFAULT 0,
    p_inherits BOOLEAN DEFAULT FALSE,
    p_reason TEXT DEFAULT NULL,
    p_ref_id UUID DEFAULT NULL,
    p_function TEXT DEFAULT NULL
) RETURNS UUID AS $$
DECLARE
    v_claim_object_id UUID;
BEGIN
    INSERT INTO samna_auth.claim_object (
        claim_id,
        object_id,
        object_type,
        sa_owner_id,
        sa_root_id,
        sa_created_by,
        sa_level,
        inherits,
        reason,
        ref_id,
        function
    ) VALUES (
        p_claim_id,
        p_object_id,
        p_object_type,
        p_sa_owner_id,
        p_sa_root_id,
        p_sa_created_by,
        p_level,
        p_inherits,
        p_reason,
        p_ref_id,
        p_function
    )
    RETURNING id INTO v_claim_object_id;

    RETURN v_claim_object_id;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION samna_auth.remove_user_claim(
    p_user_claim_id UUID,
    p_deleted_by UUID
) RETURNS BOOLEAN AS $$
BEGIN
    UPDATE samna_auth.user_claim
    SET sa_deleted_at = CURRENT_TIMESTAMP
    WHERE id = p_user_claim_id
      AND sa_deleted_at IS NULL;

    RETURN FOUND;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION samna_auth.remove_claim_object(
    p_claim_object_id UUID,
    p_deleted_by UUID
) RETURNS BOOLEAN AS $$
BEGIN
    UPDATE samna_auth.claim_object
    SET sa_deleted_at = CURRENT_TIMESTAMP
    WHERE id = p_claim_object_id
      AND sa_deleted_at IS NULL;

    RETURN FOUND;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION samna_auth.remove_claim(
    p_claim_id UUID,
    p_deleted_by UUID
) RETURNS BOOLEAN AS $$
BEGIN
    UPDATE samna_auth.user_claim
    SET sa_deleted_at = CURRENT_TIMESTAMP
    WHERE claim_id = p_claim_id
      AND sa_deleted_at IS NULL;

    UPDATE samna_auth.claim_object
    SET sa_deleted_at = CURRENT_TIMESTAMP
    WHERE claim_id = p_claim_id
      AND sa_deleted_at IS NULL;

    UPDATE samna_auth.claim
    SET sa_deleted_at = CURRENT_TIMESTAMP
    WHERE id = p_claim_id
      AND sa_deleted_at IS NULL;

    RETURN FOUND;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION samna_auth.claim_access(
    p_user_id UUID,
    p_object_id UUID DEFAULT NULL,
    p_min_sa_access_level INTEGER DEFAULT 1
) RETURNS TABLE (
    id UUID,
    claim_id UUID,
    user_id UUID,
    claim_name TEXT,
    claim_description TEXT,
    sa_access_level INTEGER,
    claim_type samna_auth.claim_type,
    sa_created_by UUID,
    claim_sa_created_by UUID,
    claim_sa_owner_id UUID,
    user_sa_owner_id UUID,
    sa_root_id UUID
) AS $$
BEGIN
    IF p_object_id IS NOT NULL THEN
        RETURN QUERY
        SELECT
            ac.id,
            ac.claim_id,
            ac.user_id,
            ac.claim_name,
            ac.claim_description,
            ac.sa_access_level,
            ac.claim_type,
            ac.sa_created_by,
            ac.claim_sa_created_by,
            ac.claim_sa_owner_id,
            ac.user_sa_owner_id,
            ac.sa_root_id
        FROM samna_auth.active_claim ac
        INNER JOIN samna_auth.active_object ao ON ao.claim_id = ac.claim_id
        WHERE ac.user_id = p_user_id
          AND ao.object_id = p_object_id
          AND ac.sa_access_level <= p_min_sa_access_level
        ORDER BY ac.sa_access_level ASC
        LIMIT 1;
    ELSE
        RETURN QUERY
        SELECT
            ac.id,
            ac.claim_id,
            ac.user_id,
            ac.claim_name,
            ac.claim_description,
            ac.sa_access_level,
            ac.claim_type,
            ac.sa_created_by,
            ac.claim_sa_created_by,
            ac.claim_sa_owner_id,
            ac.user_sa_owner_id,
            ac.sa_root_id
        FROM samna_auth.active_claim ac
        INNER JOIN samna_auth.active_object ao ON ao.claim_id = ac.claim_id
        WHERE ac.user_id = p_user_id
          AND ac.sa_access_level <= p_min_sa_access_level
        ORDER BY ac.sa_access_level ASC, ao.sa_level ASC
        LIMIT 1;
    END IF;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.get_child_organizations(start_id uuid)
RETURNS TABLE(id uuid, sa_owner_id uuid, sa_level int, sa_root_id uuid, siblings int)
AS $$
BEGIN
    RETURN QUERY
    WITH RECURSIVE children AS (
        SELECT o.id, o.sa_owner_id, o.sa_level, o.sa_root_id,
               CAST(COUNT(o.id) OVER (PARTITION BY o.sa_owner_id) AS int) AS siblings
        FROM samna_auth.organization o
        WHERE o.sa_owner_id = start_id
          AND o.sa_deleted_at IS NULL
        UNION ALL
        SELECT o.id, o.sa_owner_id, o.sa_level, o.sa_root_id,
               CAST(COUNT(o.id) OVER (PARTITION BY o.sa_owner_id) AS int) AS siblings
        FROM samna_auth.organization o
        INNER JOIN children c ON c.id = o.sa_owner_id
        WHERE o.sa_deleted_at IS NULL
    )
    SELECT DISTINCT ON (c.id) c.id, c.sa_owner_id, c.sa_level, c.sa_root_id, c.siblings - 1
    FROM children c
    WHERE c.id != start_id;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.get_parent_organizations(start_id uuid)
RETURNS TABLE(id uuid)
STABLE
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    WITH RECURSIVE parents AS (
        SELECT o.id, o.sa_owner_id
        FROM samna_auth.organization o
        WHERE o.id = start_id
          AND o.sa_deleted_at IS NULL
        UNION ALL
        SELECT o.id, o.sa_owner_id
        FROM samna_auth.organization o
        INNER JOIN parents p ON o.id = p.sa_owner_id
        WHERE o.sa_deleted_at IS NULL
    )
    SELECT DISTINCT p.id FROM parents p;
END;
$$;

CREATE OR REPLACE FUNCTION samna_auth.get_child_locations(start_id uuid)
RETURNS TABLE(id uuid, sa_parent_id uuid, sa_owner_id uuid, sa_level int, siblings int)
AS $$
BEGIN
    RETURN QUERY
    WITH RECURSIVE children AS (
        SELECT l.id, l.sa_parent_id, l.sa_owner_id, l.sa_level,
               CAST(COUNT(l.id) OVER (PARTITION BY l.sa_parent_id) AS int) AS siblings
        FROM samna_auth.location l
        WHERE l.sa_parent_id = start_id
          AND l.sa_deleted_at IS NULL
        UNION ALL
        SELECT l.id, l.sa_parent_id, l.sa_owner_id, l.sa_level,
               CAST(COUNT(l.id) OVER (PARTITION BY l.sa_parent_id) AS int) AS siblings
        FROM samna_auth.location l
        INNER JOIN children c ON c.id = l.sa_parent_id
        WHERE l.sa_deleted_at IS NULL
    )
    SELECT DISTINCT ON (c.id) c.id, c.sa_parent_id, c.sa_owner_id, c.sa_level, c.siblings - 1
    FROM children c
    WHERE c.id != start_id;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.get_parent_locations(start_id uuid)
RETURNS TABLE(id uuid)
STABLE
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    WITH RECURSIVE parents AS (
        SELECT l.id, l.sa_parent_id
        FROM samna_auth.location l
        WHERE l.id = start_id
          AND l.sa_deleted_at IS NULL
        UNION ALL
        SELECT l.id, l.sa_parent_id
        FROM samna_auth.location l
        INNER JOIN parents p ON l.id = p.sa_parent_id
        WHERE l.sa_deleted_at IS NULL
    )
    SELECT DISTINCT p.id FROM parents p;
END;
$$;

CREATE OR REPLACE FUNCTION samna_auth.get_child_objects(p_table_name text, start_id uuid)
RETURNS TABLE(id uuid, sa_parent_id uuid, sa_owner_id uuid, sa_level int, siblings int)
AS $$
DECLARE
    v_schema text;
    v_table text;
    has_sa_owner_id boolean;
    has_sa_level boolean;
    owner_col text;
    level_col text;
BEGIN
    IF position('.' in p_table_name) > 0 THEN
        v_schema := split_part(p_table_name, '.', 1);
        v_table := split_part(p_table_name, '.', 2);
    ELSE
        v_schema := 'public';
        v_table := p_table_name;
    END IF;

    SELECT
        EXISTS(SELECT 1 FROM samna_auth.get_table_columns(p_table_name) WHERE column_name = 'sa_owner_id'),
        EXISTS(SELECT 1 FROM samna_auth.get_table_columns(p_table_name) WHERE column_name = 'sa_level')
    INTO has_sa_owner_id, has_sa_level;

    IF has_sa_owner_id THEN owner_col := 't.sa_owner_id'; ELSE owner_col := 'NULL::uuid'; END IF;
    IF has_sa_level THEN level_col := 't.sa_level'; ELSE level_col := '0'; END IF;

    RETURN QUERY EXECUTE format('
        WITH RECURSIVE children AS (
            SELECT t.id, t.sa_parent_id, %s AS sa_owner_id, %s AS sa_level,
                   CAST(COUNT(t.id) OVER (PARTITION BY t.sa_parent_id) AS int) AS siblings
            FROM %I.%I t
            WHERE t.sa_parent_id = $1
              AND t.sa_deleted_at IS NULL
            UNION ALL
            SELECT t.id, t.sa_parent_id, %s AS sa_owner_id, %s AS sa_level,
                   CAST(COUNT(t.id) OVER (PARTITION BY t.sa_parent_id) AS int) AS siblings
            FROM %I.%I t
            INNER JOIN children c ON c.id = t.sa_parent_id
            WHERE t.sa_deleted_at IS NULL
        )
        SELECT DISTINCT ON (c.id) c.id, c.sa_parent_id, c.sa_owner_id, c.sa_level, c.siblings - 1
        FROM children c
        WHERE c.id != $1',
        owner_col, level_col, v_schema, v_table,
        owner_col, level_col, v_schema, v_table
    ) USING start_id;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.get_parent_objects(p_table_name text, start_id uuid)
RETURNS TABLE(id uuid)
AS $$
DECLARE
    v_schema text;
    v_table text;
BEGIN
    IF position('.' in p_table_name) > 0 THEN
        v_schema := split_part(p_table_name, '.', 1);
        v_table := split_part(p_table_name, '.', 2);
    ELSE
        v_schema := 'public';
        v_table := p_table_name;
    END IF;

    RETURN QUERY EXECUTE format('
        WITH RECURSIVE parents AS (
            SELECT t.id, t.sa_parent_id
            FROM %I.%I t
            WHERE t.id = $1
              AND t.sa_deleted_at IS NULL
            UNION ALL
            SELECT t.id, t.sa_parent_id
            FROM %I.%I t
            INNER JOIN parents p ON t.id = p.sa_parent_id
            WHERE t.sa_deleted_at IS NULL
        )
        SELECT DISTINCT p.id FROM parents p',
        v_schema, v_table, v_schema, v_table
    ) USING start_id;
END;
$$ LANGUAGE plpgsql STABLE;

DROP FUNCTION IF EXISTS samna_auth.has_inheritance_columns CASCADE;
DROP FUNCTION IF EXISTS samna_auth.has_column CASCADE;
DROP FUNCTION IF EXISTS samna_auth.create_active_object_view CASCADE;
DROP FUNCTION IF EXISTS samna_auth.init_auth_views CASCADE;

CREATE OR REPLACE FUNCTION samna_auth.get_table_columns(p_table_name TEXT)
RETURNS TABLE(column_name TEXT) AS $$
DECLARE
    v_schema TEXT;
    v_table TEXT;
BEGIN
    IF position('.' in p_table_name) > 0 THEN
        v_schema := split_part(p_table_name, '.', 1);
        v_table := split_part(p_table_name, '.', 2);
    ELSE
        v_schema := 'public';
        v_table := p_table_name;
    END IF;

    RETURN QUERY
    SELECT c.column_name::TEXT
    FROM information_schema.columns c
    WHERE c.table_schema = v_schema
      AND c.table_name = v_table;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.build_inheritance_query(
    p_table_name TEXT,
    p_object_type TEXT
) RETURNS TEXT AS $$
DECLARE
    has_sa_parent_id BOOLEAN;
    has_sa_owner_id BOOLEAN;
    has_sa_root_id BOOLEAN;
    has_level BOOLEAN;
    owner_id_col TEXT;
    level_expr TEXT;
    recursive_col TEXT;
    recursive_cte TEXT;
    owner_query TEXT;
    direct_query TEXT;
    v_qualified_table TEXT;
    result_query TEXT;
BEGIN
    v_qualified_table := p_table_name;

    SELECT
        EXISTS(SELECT 1 FROM samna_auth.get_table_columns(p_table_name) WHERE column_name = 'sa_parent_id'),
        EXISTS(SELECT 1 FROM samna_auth.get_table_columns(p_table_name) WHERE column_name = 'sa_owner_id'),
        EXISTS(SELECT 1 FROM samna_auth.get_table_columns(p_table_name) WHERE column_name = 'sa_root_id'),
        EXISTS(SELECT 1 FROM samna_auth.get_table_columns(p_table_name) WHERE column_name = 'sa_level')
    INTO has_sa_parent_id, has_sa_owner_id, has_sa_root_id, has_level;

    IF has_sa_owner_id THEN
        owner_id_col := 'sa_owner_id';
    ELSE
        owner_id_col := NULL;
    END IF;

    IF has_level THEN
        level_expr := 'COALESCE(t.sa_level, 0)';
    ELSE
        level_expr := 'co.sa_level';
    END IF;

    direct_query := format('
    SELECT
        t.id AS object_id,
        %L AS object_type,
        co.claim_id,
        %s AS sa_level,
        co.inherits,
        co.sa_root_id,
        t.%I AS sa_owner_id,
        co.ref_id,
        co.reason,
        co.function,
        co.sa_created_at,
        co.sa_updated_at,
        co.sa_deleted_at
    FROM samna_auth.claim_object co
    INNER JOIN %s t ON co.object_id = t.id
    WHERE co.object_type = %L
      AND co.sa_deleted_at IS NULL',
        p_object_type,
        level_expr,
        owner_id_col,
        v_qualified_table,
        p_object_type
    );

    result_query := '(' || direct_query || ')';

    IF owner_id_col IS NOT NULL AND p_object_type != 'organization' THEN
        owner_query := format('
        SELECT
            t.id AS object_id,
            %L AS object_type,
            co.claim_id,
            co.sa_level + 1 AS sa_level,
            co.inherits,
            co.sa_root_id,
            t.%I AS sa_owner_id,
            co.ref_id,
            ''owner-based'' AS reason,
            co.function,
            co.sa_created_at,
            co.sa_updated_at,
            co.sa_deleted_at
        FROM samna_auth.claim_object co
        INNER JOIN %s t ON t.%I = co.object_id
        WHERE co.object_type = ''organization''
          AND co.inherits = TRUE
          AND co.sa_deleted_at IS NULL',
            p_object_type,
            owner_id_col,
            v_qualified_table,
            owner_id_col
        );
        result_query := result_query || ' UNION ALL (' || owner_query || ')';
    END IF;

    IF p_object_type = 'organization' THEN
        recursive_col := 'sa_owner_id';
    ELSIF has_sa_parent_id THEN
        recursive_col := 'sa_parent_id';
    ELSE
        RETURN result_query;
    END IF;

    IF has_level THEN
        recursive_cte := format('
        (WITH RECURSIVE hierarchy AS (
            SELECT
                t.id,
                t.%I AS recursive_ref,
                t.%I AS sa_owner_id,
                t.sa_root_id,
                %s AS sa_level,
                co.claim_id,
                co.ref_id,
                co.function,
                co.sa_created_at,
                co.sa_updated_at,
                co.sa_deleted_at
            FROM %s t
            INNER JOIN samna_auth.claim_object co ON co.object_id = t.id
            WHERE co.object_type = %L
              AND co.inherits = TRUE
              AND co.sa_deleted_at IS NULL

            UNION ALL

            SELECT
                child.id,
                child.%I AS recursive_ref,
                child.%I AS sa_owner_id,
                child.sa_root_id,
                COALESCE(child.sa_level, h.sa_level + 1) AS sa_level,
                h.claim_id,
                h.ref_id,
                h.function,
                h.sa_created_at,
                h.sa_updated_at,
                h.sa_deleted_at
            FROM %s child
            INNER JOIN hierarchy h ON child.%I = h.id
            WHERE child.id != h.id
        )
        SELECT
            h.id AS object_id,
            %L AS object_type,
            h.claim_id,
            h.sa_level,
            TRUE AS inherits,
            h.sa_root_id,
            h.sa_owner_id,
            h.ref_id,
            ''inherited'' AS reason,
            h.function,
            h.sa_created_at,
            h.sa_updated_at,
            h.sa_deleted_at
        FROM hierarchy h
        WHERE h.id NOT IN (
            SELECT co.object_id
            FROM samna_auth.claim_object co
            WHERE co.object_type = %L
              AND co.sa_deleted_at IS NULL
        ))',
            recursive_col,
            owner_id_col,
            level_expr,
            v_qualified_table,
            p_object_type,
            recursive_col,
            owner_id_col,
            v_qualified_table,
            recursive_col,
            p_object_type,
            p_object_type
        );
    ELSE
        recursive_cte := format('
        (WITH RECURSIVE hierarchy AS (
            SELECT
                t.id,
                t.%I AS recursive_ref,
                t.%I AS sa_owner_id,
                t.sa_root_id,
                %s AS sa_level,
                co.claim_id,
                co.ref_id,
                co.function,
                co.sa_created_at,
                co.sa_updated_at,
                co.sa_deleted_at
            FROM %s t
            INNER JOIN samna_auth.claim_object co ON co.object_id = t.id
            WHERE co.object_type = %L
              AND co.inherits = TRUE
              AND co.sa_deleted_at IS NULL

            UNION ALL

            SELECT
                child.id,
                child.%I AS recursive_ref,
                child.%I AS sa_owner_id,
                child.sa_root_id,
                h.sa_level + 1 AS sa_level,
                h.claim_id,
                h.ref_id,
                h.function,
                h.sa_created_at,
                h.sa_updated_at,
                h.sa_deleted_at
            FROM %s child
            INNER JOIN hierarchy h ON child.%I = h.id
            WHERE child.id != h.id
        )
        SELECT
            h.id AS object_id,
            %L AS object_type,
            h.claim_id,
            h.sa_level,
            TRUE AS inherits,
            h.sa_root_id,
            h.sa_owner_id,
            h.ref_id,
            ''inherited'' AS reason,
            h.function,
            h.sa_created_at,
            h.sa_updated_at,
            h.sa_deleted_at
        FROM hierarchy h
        WHERE h.id NOT IN (
            SELECT co.object_id
            FROM samna_auth.claim_object co
            WHERE co.object_type = %L
              AND co.sa_deleted_at IS NULL
        ))',
            recursive_col,
            owner_id_col,
            level_expr,
            v_qualified_table,
            p_object_type,
            recursive_col,
            owner_id_col,
            v_qualified_table,
            recursive_col,
            p_object_type,
            p_object_type
        );
    END IF;

    RETURN result_query || ' UNION ALL ' || recursive_cte;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.init_auth_views(VARIADIC p_table_names TEXT[])
RETURNS VOID AS $$
DECLARE
    table_name TEXT;
    object_type TEXT;
    union_parts TEXT[] := ARRAY[]::TEXT[];
    query_part TEXT;
    view_sql TEXT;
BEGIN
    EXECUTE 'DROP VIEW IF EXISTS samna_auth.active_object CASCADE';

    query_part := samna_auth.build_inheritance_query('samna_auth.organization', 'organization');
    union_parts := array_append(union_parts, query_part);

    query_part := samna_auth.build_inheritance_query('samna_auth.location', 'location');
    union_parts := array_append(union_parts, query_part);

    FOREACH table_name IN ARRAY p_table_names
    LOOP
        IF position('.' in table_name) > 0 THEN
            object_type := split_part(table_name, '.', 2);
        ELSE
            object_type := table_name;
        END IF;

        query_part := samna_auth.build_inheritance_query(
            table_name,
            object_type
        );
        union_parts := array_append(union_parts, query_part);
    END LOOP;

    view_sql := format('
        CREATE VIEW samna_auth.active_object AS
        %s
    ', array_to_string(union_parts, ' UNION ALL '));

    EXECUTE view_sql;

    RAISE NOTICE 'samna_auth.active_object view created with % tables', array_length(p_table_names, 1) + 2;

    EXECUTE '
        CREATE VIEW samna_auth.active_users AS
        WITH shared_access AS (
            SELECT DISTINCT
                ao.object_id AS access_object_id,
                ac.user_id,
                ac.claim_id,
                ac.sa_access_level,
                ao.sa_owner_id
            FROM samna_auth.active_object ao
            INNER JOIN samna_auth.active_claim ac ON ac.claim_id = ao.claim_id
            WHERE ao.object_type IN (''organization'', ''location'')
        )
        SELECT DISTINCT
            uc.user_id AS target_user_id,
            sa.user_id AS user_id,
            sa.claim_id,
            sa.sa_owner_id,
            sa.sa_access_level,
            uc.sa_created_at,
            uc.sa_updated_at,
            uc.sa_deleted_at
        FROM samna_auth.user_claim uc
        INNER JOIN shared_access sa ON sa.access_object_id = uc.sa_owner_id
        WHERE uc.sa_deleted_at IS NULL
          AND uc.user_id != sa.user_id
    ';

    RAISE NOTICE 'samna_auth.active_users view created';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION samna_auth.get_claims(
    p_user_id UUID,
    p_access_level INTEGER DEFAULT 8
) RETURNS SETOF samna_auth.claim AS $$
BEGIN
    RETURN QUERY
    SELECT DISTINCT c.*
    FROM samna_auth.claim c
    INNER JOIN samna_auth.active_claim ac ON ac.claim_id = c.id
    WHERE ac.user_id = p_user_id
      AND ac.sa_access_level <= p_access_level
      AND c.sa_deleted_at IS NULL

    UNION

    SELECT DISTINCT c.*
    FROM samna_auth.claim c
    INNER JOIN samna_auth.active_object ao ON ao.object_id = c.sa_owner_id AND ao.object_type = 'organization'
    INNER JOIN samna_auth.active_claim ac ON ac.claim_id = ao.claim_id
    WHERE ac.user_id = p_user_id
      AND ac.sa_access_level <= p_access_level
      AND c.sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.get_organizations(
    p_user_id UUID,
    p_access_level INTEGER DEFAULT 8
) RETURNS SETOF samna_auth.organization AS $$
BEGIN
    RETURN QUERY
    SELECT o.*
    FROM samna_auth.organization o
    INNER JOIN samna_auth.active_object ao ON ao.object_id = o.id AND ao.object_type = 'organization'
    INNER JOIN samna_auth.active_claim ac ON ac.claim_id = ao.claim_id
    WHERE ac.user_id = p_user_id
      AND ac.sa_access_level <= p_access_level
      AND o.sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.get_locations(
    p_user_id UUID,
    p_access_level INTEGER DEFAULT 8
) RETURNS SETOF samna_auth.location AS $$
BEGIN
    RETURN QUERY
    SELECT l.*
    FROM samna_auth.location l
    INNER JOIN samna_auth.active_object ao ON ao.object_id = l.id AND ao.object_type = 'location'
    INNER JOIN samna_auth.active_claim ac ON ac.claim_id = ao.claim_id
    WHERE ac.user_id = p_user_id
      AND ac.sa_access_level <= p_access_level
      AND l.sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.get_users(
    p_user_id UUID,
    p_access_level INTEGER DEFAULT 8
) RETURNS SETOF samna_auth.samna_user AS $$
BEGIN
    RETURN QUERY
    SELECT u.*
    FROM samna_auth.samna_user u
    WHERE u.user_id = p_user_id
      AND u.sa_deleted_at IS NULL

    UNION

    SELECT u.*
    FROM samna_auth.samna_user u
    INNER JOIN samna_auth.active_users au ON au.target_user_id = u.user_id
    WHERE au.user_id = p_user_id
      AND au.sa_access_level <= p_access_level
      AND u.sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.get_objects(
    p_user_id UUID,
    p_object_type TEXT,
    p_access_level INTEGER DEFAULT 8
) RETURNS TABLE(object_id UUID) AS $$
BEGIN
    IF p_object_type IN ('organization', 'location', 'user') THEN
        RAISE EXCEPTION 'use get_organizations, get_locations, or get_users for type %', p_object_type;
    END IF;

    RETURN QUERY
    SELECT DISTINCT ao.object_id
    FROM samna_auth.active_object ao
    INNER JOIN samna_auth.active_claim ac ON ac.claim_id = ao.claim_id
    WHERE ac.user_id = p_user_id
      AND ao.object_type = p_object_type
      AND ac.sa_access_level <= p_access_level;
END;
$$ LANGUAGE plpgsql STABLE;

-- get_claim_graph returns the claim graph for a user as a flat list of typed nodes.
-- Resolves claims, user claims, users, organizations, and locations from the auth schema.
-- Domain specific objects (bookable, booking, etc.) are returned as unresolved typed
-- references that consuming services can enrich by joining their own tables.
CREATE OR REPLACE FUNCTION samna_auth.get_claim_graph(
    p_user_id UUID,
    p_access_level INTEGER DEFAULT 30
)
RETURNS TABLE(
    id UUID,
    label TEXT,
    type TEXT,
    data JSONB
) AS $$
WITH accessible_claims AS (
    SELECT c.*
    FROM samna_auth.claim c
    INNER JOIN samna_auth.get_organizations(p_user_id, p_access_level) o ON o.id = c.sa_owner_id
    WHERE c.sa_deleted_at IS NULL
    AND c.type = 'user'
),
accessible_claim_ids AS (
    SELECT ac.id FROM accessible_claims ac
),
accessible_user_claims AS (
    SELECT uc.*
    FROM samna_auth.user_claim uc
    INNER JOIN accessible_claim_ids aci ON aci.id = uc.claim_id
    WHERE uc.sa_deleted_at IS NULL
),
accessible_claim_objects AS (
    SELECT co.*
    FROM samna_auth.claim_object co
    INNER JOIN accessible_claim_ids aci ON aci.id = co.claim_id
    WHERE co.sa_deleted_at IS NULL
)
SELECT
    c.id,
    c.name as label,
    'claim'::TEXT as type,
    to_jsonb(c.*) as data
FROM accessible_claims c

UNION ALL

SELECT
    uc.id,
    uc.reason as label,
    'user_claim'::TEXT as type,
    to_jsonb(uc.*) as data
FROM accessible_user_claims uc

UNION ALL

SELECT DISTINCT ON (u.id)
    u.id,
    CONCAT(u.first_name, ' ', u.last_name) as label,
    'user'::TEXT as type,
    to_jsonb(u.*) as data
FROM samna_auth.get_users(p_user_id, p_access_level) u
WHERE u.user_id IN (
    SELECT DISTINCT auc.user_id FROM accessible_user_claims auc
)

UNION ALL

SELECT DISTINCT ON (o.id)
    o.id,
    o.name as label,
    'organization'::TEXT as type,
    to_jsonb(o.*) as data
FROM samna_auth.get_organizations(p_user_id, p_access_level) o
WHERE o.id IN (
    SELECT sa_owner_id FROM accessible_claims WHERE sa_owner_id IS NOT NULL
    UNION
    SELECT sa_root_id FROM accessible_claims WHERE sa_root_id IS NOT NULL
    UNION
    SELECT auc.sa_owner_id FROM accessible_user_claims auc
        WHERE auc.sa_owner_id IS NOT NULL
    UNION
    SELECT aco.sa_owner_id FROM accessible_claim_objects aco
        WHERE aco.sa_owner_id IS NOT NULL
    UNION
    SELECT aco.object_id FROM accessible_claim_objects aco
        WHERE aco.object_type = 'organization'
)

UNION ALL

SELECT DISTINCT ON (l.id)
    l.id,
    COALESCE(l.name, CONCAT(l.code, ' ', l.code_type), 'location') as label,
    'location'::TEXT as type,
    to_jsonb(l.*) as data
FROM samna_auth.get_locations(p_user_id, p_access_level) l
WHERE l.id IN (
    SELECT aco.object_id FROM accessible_claim_objects aco
        WHERE aco.object_type = 'location'
)

UNION ALL

SELECT
    co.object_id as id,
    COALESCE(co.reason, co.object_type) as label,
    co.object_type::TEXT as type,
    jsonb_build_object('object_id', co.object_id, 'object_type', co.object_type, 'claim_id', co.claim_id) as data
FROM accessible_claim_objects co
WHERE co.object_type NOT IN ('organization', 'location', 'user', 'claim');
$$ LANGUAGE SQL STABLE;

-- get_owner_graph returns the ownership hierarchy for a user as a flat list of typed nodes.
-- Contains organizations and their locations. Consuming services extend this by joining
-- their own domain objects (e.g. bookables) onto the returned organization and location IDs.
CREATE OR REPLACE FUNCTION samna_auth.get_owner_graph(
    p_user_id UUID,
    p_access_level INTEGER DEFAULT 30
)
RETURNS TABLE(
    id UUID,
    label TEXT,
    type TEXT,
    data JSONB
) AS $$
WITH accessible_orgs AS (
    SELECT DISTINCT o.*
    FROM samna_auth.get_organizations(p_user_id, p_access_level) o
),
accessible_locs AS (
    SELECT DISTINCT l.*
    FROM samna_auth.get_locations(p_user_id, p_access_level) l
    WHERE l.sa_owner_id IN (SELECT ao.id FROM accessible_orgs ao)
)
SELECT
    o.id,
    o.name as label,
    'organization'::TEXT as type,
    to_jsonb(o.*) as data
FROM accessible_orgs o

UNION ALL

SELECT DISTINCT ON (l.id)
    l.id,
    COALESCE(l.name, CONCAT(l.code, ' ', l.code_type), 'location') as label,
    'location'::TEXT as type,
    to_jsonb(l.*) as data
FROM accessible_locs l;
$$ LANGUAGE SQL STABLE;
