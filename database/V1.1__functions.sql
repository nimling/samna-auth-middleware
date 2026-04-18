-- ============================================================================
-- CRUD HELPERS (PUBLIC)
-- ============================================================================

-- ============================================================================
-- create_claim (PUBLIC)
-- Creates a new claim and returns its id.
-- ============================================================================
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
        name, description, sa_access_level, sa_owner_id, sa_root_id,
        sa_created_by, inherits, type
    ) VALUES (
        p_name, p_description, p_sa_access_level, p_sa_owner_id, p_sa_root_id,
        p_sa_created_by, p_inherits, p_type
    )
    RETURNING id INTO v_claim_id;
    RETURN v_claim_id;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- assign_claim_user (PUBLIC)
-- Grants a claim to a user and returns the user_claim id.
-- ============================================================================
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
        claim_id, user_id, sa_owner_id, sa_created_by, reason, starts_at, ends_at
    ) VALUES (
        p_claim_id, p_user_id, p_sa_owner_id, p_sa_created_by, p_reason, p_starts_at, p_ends_at
    )
    RETURNING id INTO v_user_claim_id;
    RETURN v_user_claim_id;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- assign_claim_object (PUBLIC)
-- Binds a claim to an object and returns the claim_object id.
-- ============================================================================
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
        claim_id, object_id, object_type, sa_owner_id, sa_root_id, sa_created_by,
        sa_level, inherits, reason, ref_id, function
    ) VALUES (
        p_claim_id, p_object_id, p_object_type, p_sa_owner_id, p_sa_root_id, p_sa_created_by,
        p_level, p_inherits, p_reason, p_ref_id, p_function
    )
    RETURNING id INTO v_claim_object_id;
    RETURN v_claim_object_id;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- remove_user_claim (PUBLIC)
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.remove_user_claim(
    p_user_claim_id UUID,
    p_deleted_by UUID
) RETURNS BOOLEAN AS $$
BEGIN
    UPDATE samna_auth.user_claim
    SET sa_deleted_at = CURRENT_TIMESTAMP
    WHERE id = p_user_claim_id AND sa_deleted_at IS NULL;
    RETURN FOUND;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- remove_claim_object (PUBLIC)
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.remove_claim_object(
    p_claim_object_id UUID,
    p_deleted_by UUID
) RETURNS BOOLEAN AS $$
BEGIN
    UPDATE samna_auth.claim_object
    SET sa_deleted_at = CURRENT_TIMESTAMP
    WHERE id = p_claim_object_id AND sa_deleted_at IS NULL;
    RETURN FOUND;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- update_claim (PUBLIC)
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.update_claim(
    p_claim_id UUID,
    p_name TEXT DEFAULT NULL,
    p_description TEXT DEFAULT NULL,
    p_sa_access_level INTEGER DEFAULT NULL,
    p_inherits BOOLEAN DEFAULT NULL
) RETURNS samna_auth.claim AS $$
DECLARE
    v_claim samna_auth.claim;
BEGIN
    UPDATE samna_auth.claim SET
        name = COALESCE(p_name, name),
        description = COALESCE(p_description, description),
        sa_access_level = COALESCE(p_sa_access_level, sa_access_level),
        inherits = COALESCE(p_inherits, inherits),
        sa_updated_at = CURRENT_TIMESTAMP
    WHERE id = p_claim_id AND sa_deleted_at IS NULL
    RETURNING * INTO v_claim;
    RETURN v_claim;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- update_claim_user (PUBLIC)
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.update_claim_user(
    p_user_claim_id UUID,
    p_reason TEXT DEFAULT NULL,
    p_starts_at TIMESTAMPTZ DEFAULT NULL,
    p_ends_at TIMESTAMPTZ DEFAULT NULL
) RETURNS samna_auth.user_claim AS $$
DECLARE
    v_user_claim samna_auth.user_claim;
BEGIN
    UPDATE samna_auth.user_claim SET
        reason = COALESCE(p_reason, reason),
        starts_at = COALESCE(p_starts_at, starts_at),
        ends_at = COALESCE(p_ends_at, ends_at),
        sa_updated_at = CURRENT_TIMESTAMP
    WHERE id = p_user_claim_id AND sa_deleted_at IS NULL
    RETURNING * INTO v_user_claim;
    RETURN v_user_claim;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- update_claim_object (PUBLIC)
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.update_claim_object(
    p_claim_object_id UUID,
    p_reason TEXT DEFAULT NULL,
    p_inherits BOOLEAN DEFAULT NULL
) RETURNS samna_auth.claim_object AS $$
DECLARE
    v_claim_object samna_auth.claim_object;
BEGIN
    UPDATE samna_auth.claim_object SET
        reason = COALESCE(p_reason, reason),
        inherits = COALESCE(p_inherits, inherits),
        sa_updated_at = CURRENT_TIMESTAMP
    WHERE id = p_claim_object_id AND sa_deleted_at IS NULL
    RETURNING * INTO v_claim_object;
    RETURN v_claim_object;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- remove_claim (PUBLIC)
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.remove_claim(
    p_claim_id UUID,
    p_deleted_by UUID
) RETURNS BOOLEAN AS $$
BEGIN
    UPDATE samna_auth.user_claim SET sa_deleted_at = CURRENT_TIMESTAMP
    WHERE claim_id = p_claim_id AND sa_deleted_at IS NULL;
    UPDATE samna_auth.claim_object SET sa_deleted_at = CURRENT_TIMESTAMP
    WHERE claim_id = p_claim_id AND sa_deleted_at IS NULL;
    UPDATE samna_auth.claim SET sa_deleted_at = CURRENT_TIMESTAMP
    WHERE id = p_claim_id AND sa_deleted_at IS NULL;
    RETURN FOUND;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- HIERARCHY WALKERS (INTERNAL)
-- ============================================================================

CREATE OR REPLACE FUNCTION samna_auth.get_table_columns(p_table_name TEXT)
RETURNS TABLE(column_name TEXT) AS $$
DECLARE
    v_schema TEXT; v_table TEXT;
BEGIN
    IF position('.' in p_table_name) > 0 THEN
        v_schema := split_part(p_table_name, '.', 1);
        v_table := split_part(p_table_name, '.', 2);
    ELSE
        v_schema := 'public'; v_table := p_table_name;
    END IF;
    RETURN QUERY
    SELECT c.column_name::TEXT FROM information_schema.columns c
    WHERE c.table_schema = v_schema AND c.table_name = v_table;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.get_child_organizations(start_id uuid)
RETURNS TABLE(id uuid, sa_owner_id uuid, sa_level int, sa_root_id uuid, siblings int) AS $$
BEGIN
    RETURN QUERY
    WITH RECURSIVE children AS (
        SELECT o.id, o.sa_owner_id, o.sa_level, o.sa_root_id,
               CAST(COUNT(o.id) OVER (PARTITION BY o.sa_owner_id) AS int) AS siblings
        FROM samna_auth.organization o
        WHERE o.sa_owner_id = start_id AND o.sa_deleted_at IS NULL
        UNION ALL
        SELECT o.id, o.sa_owner_id, o.sa_level, o.sa_root_id,
               CAST(COUNT(o.id) OVER (PARTITION BY o.sa_owner_id) AS int) AS siblings
        FROM samna_auth.organization o
        INNER JOIN children c ON c.id = o.sa_owner_id WHERE o.sa_deleted_at IS NULL
    )
    SELECT DISTINCT ON (c.id) c.id, c.sa_owner_id, c.sa_level, c.sa_root_id, c.siblings - 1
    FROM children c WHERE c.id != start_id;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.get_parent_organizations(start_id uuid)
RETURNS TABLE(id uuid) STABLE LANGUAGE plpgsql AS $$
BEGIN
    RETURN QUERY
    WITH RECURSIVE parents AS (
        SELECT o.id, o.sa_owner_id FROM samna_auth.organization o
        WHERE o.id = start_id AND o.sa_deleted_at IS NULL
        UNION ALL
        SELECT o.id, o.sa_owner_id FROM samna_auth.organization o
        INNER JOIN parents p ON o.id = p.sa_owner_id WHERE o.sa_deleted_at IS NULL
    )
    SELECT DISTINCT p.id FROM parents p;
END;
$$;

CREATE OR REPLACE FUNCTION samna_auth.get_child_locations(start_id uuid)
RETURNS TABLE(id uuid, sa_parent_id uuid, sa_owner_id uuid, sa_level int, siblings int) AS $$
BEGIN
    RETURN QUERY
    WITH RECURSIVE children AS (
        SELECT l.id, l.sa_parent_id, l.sa_owner_id, l.sa_level,
               CAST(COUNT(l.id) OVER (PARTITION BY l.sa_parent_id) AS int) AS siblings
        FROM samna_auth.location l
        WHERE l.sa_parent_id = start_id AND l.sa_deleted_at IS NULL
        UNION ALL
        SELECT l.id, l.sa_parent_id, l.sa_owner_id, l.sa_level,
               CAST(COUNT(l.id) OVER (PARTITION BY l.sa_parent_id) AS int) AS siblings
        FROM samna_auth.location l
        INNER JOIN children c ON c.id = l.sa_parent_id WHERE l.sa_deleted_at IS NULL
    )
    SELECT DISTINCT ON (c.id) c.id, c.sa_parent_id, c.sa_owner_id, c.sa_level, c.siblings - 1
    FROM children c WHERE c.id != start_id;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.get_parent_locations(start_id uuid)
RETURNS TABLE(id uuid) STABLE LANGUAGE plpgsql AS $$
BEGIN
    RETURN QUERY
    WITH RECURSIVE parents AS (
        SELECT l.id, l.sa_parent_id FROM samna_auth.location l
        WHERE l.id = start_id AND l.sa_deleted_at IS NULL
        UNION ALL
        SELECT l.id, l.sa_parent_id FROM samna_auth.location l
        INNER JOIN parents p ON l.id = p.sa_parent_id WHERE l.sa_deleted_at IS NULL
    )
    SELECT DISTINCT p.id FROM parents p;
END;
$$;

CREATE OR REPLACE FUNCTION samna_auth.get_child_objects(p_table_name text, start_id uuid)
RETURNS TABLE(id uuid, sa_parent_id uuid, sa_owner_id uuid, sa_level int, siblings int) AS $$
DECLARE
    v_schema text; v_table text;
    has_sa_owner_id boolean; has_sa_level boolean;
    owner_col text; level_col text;
BEGIN
    IF position('.' in p_table_name) > 0 THEN
        v_schema := split_part(p_table_name, '.', 1);
        v_table := split_part(p_table_name, '.', 2);
    ELSE
        v_schema := 'public'; v_table := p_table_name;
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
            FROM %I.%I t WHERE t.sa_parent_id = $1 AND t.sa_deleted_at IS NULL
            UNION ALL
            SELECT t.id, t.sa_parent_id, %s AS sa_owner_id, %s AS sa_level,
                   CAST(COUNT(t.id) OVER (PARTITION BY t.sa_parent_id) AS int) AS siblings
            FROM %I.%I t INNER JOIN children c ON c.id = t.sa_parent_id WHERE t.sa_deleted_at IS NULL
        )
        SELECT DISTINCT ON (c.id) c.id, c.sa_parent_id, c.sa_owner_id, c.sa_level, c.siblings - 1
        FROM children c WHERE c.id != $1',
        owner_col, level_col, v_schema, v_table,
        owner_col, level_col, v_schema, v_table
    ) USING start_id;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.get_parent_objects(p_table_name text, start_id uuid)
RETURNS TABLE(id uuid) AS $$
DECLARE
    v_schema text; v_table text;
BEGIN
    IF position('.' in p_table_name) > 0 THEN
        v_schema := split_part(p_table_name, '.', 1);
        v_table := split_part(p_table_name, '.', 2);
    ELSE
        v_schema := 'public'; v_table := p_table_name;
    END IF;
    RETURN QUERY EXECUTE format('
        WITH RECURSIVE parents AS (
            SELECT t.id, t.sa_parent_id FROM %I.%I t WHERE t.id = $1 AND t.sa_deleted_at IS NULL
            UNION ALL
            SELECT t.id, t.sa_parent_id FROM %I.%I t
            INNER JOIN parents p ON t.id = p.sa_parent_id WHERE t.sa_deleted_at IS NULL
        )
        SELECT DISTINCT p.id FROM parents p',
        v_schema, v_table, v_schema, v_table
    ) USING start_id;
END;
$$ LANGUAGE plpgsql STABLE;

-- ============================================================================
-- REVERSE INDEX HELPERS (INTERNAL)
-- ============================================================================

-- ============================================================================
-- compute_object_claims (INTERNAL)
-- Walks the inheritance graph for one (user, object, type) triple and returns
-- every claim granting access. The only place inheritance traversal happens.
-- Called from: reconcile_user_object, init_user_object.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.compute_object_claims(
    p_user_id UUID,
    p_object_id UUID,
    p_object_type TEXT
) RETURNS TABLE (
    claim_id UUID,
    sa_access_level INTEGER,
    sa_owner_id UUID,
    sa_location_id UUID
) AS $$
DECLARE
    v_table_name TEXT;
    v_schema TEXT; v_table TEXT;
    v_row_owner_id UUID;
    v_row_location_id UUID;
    v_row_parent_id UUID;
    has_sa_location_id BOOLEAN;
    has_sa_parent_id BOOLEAN;
BEGIN
    IF p_object_type IN ('organization', 'location') THEN
        v_table_name := 'samna_auth.' || p_object_type;
    ELSE
        v_table_name := p_object_type;
    END IF;

    IF position('.' in v_table_name) > 0 THEN
        v_schema := split_part(v_table_name, '.', 1);
        v_table := split_part(v_table_name, '.', 2);
    ELSE
        v_schema := 'public'; v_table := v_table_name;
    END IF;

    SELECT
        EXISTS(SELECT 1 FROM samna_auth.get_table_columns(v_table_name) WHERE column_name = 'sa_location_id'),
        EXISTS(SELECT 1 FROM samna_auth.get_table_columns(v_table_name) WHERE column_name = 'sa_parent_id')
    INTO has_sa_location_id, has_sa_parent_id;

    EXECUTE format(
        'SELECT t.sa_owner_id, %s, %s FROM %I.%I t WHERE t.id = $1 AND t.sa_deleted_at IS NULL',
        CASE WHEN has_sa_location_id THEN 't.sa_location_id' ELSE 'NULL::uuid' END,
        CASE WHEN has_sa_parent_id THEN 't.sa_parent_id' ELSE 'NULL::uuid' END,
        v_schema, v_table
    ) INTO v_row_owner_id, v_row_location_id, v_row_parent_id USING p_object_id;

    IF v_row_owner_id IS NULL AND p_object_type NOT IN ('organization', 'location') THEN
        RETURN;
    END IF;

    RETURN QUERY
    SELECT samna_auth.claim_object.claim_id, c.sa_access_level, v_row_owner_id, v_row_location_id
    FROM samna_auth.claim_object
    INNER JOIN samna_auth.user_claim ON samna_auth.user_claim.claim_id = samna_auth.claim_object.claim_id AND samna_auth.user_claim.sa_deleted_at IS NULL AND (samna_auth.user_claim.starts_at IS NULL OR samna_auth.user_claim.starts_at <= CURRENT_TIMESTAMP) AND (samna_auth.user_claim.ends_at IS NULL OR samna_auth.user_claim.ends_at >= CURRENT_TIMESTAMP)
    INNER JOIN samna_auth.claim ON samna_auth.claim.id = samna_auth.claim_object.claim_id AND c.sa_deleted_at IS NULL
    WHERE samna_auth.claim_object.object_id = p_object_id AND samna_auth.claim_object.object_type = p_object_type
      AND samna_auth.claim_object.sa_deleted_at IS NULL AND samna_auth.user_claim.user_id = p_user_id;

    IF v_row_owner_id IS NOT NULL AND p_object_type NOT IN ('organization') THEN
        RETURN QUERY
        SELECT samna_auth.claim_object.claim_id, c.sa_access_level, v_row_owner_id, v_row_location_id
        FROM samna_auth.claim_object
        INNER JOIN samna_auth.user_claim ON samna_auth.user_claim.claim_id = samna_auth.claim_object.claim_id AND samna_auth.user_claim.sa_deleted_at IS NULL AND (samna_auth.user_claim.starts_at IS NULL OR samna_auth.user_claim.starts_at <= CURRENT_TIMESTAMP) AND (samna_auth.user_claim.ends_at IS NULL OR samna_auth.user_claim.ends_at >= CURRENT_TIMESTAMP)
    INNER JOIN samna_auth.claim ON samna_auth.claim.id = samna_auth.claim_object.claim_id AND c.sa_deleted_at IS NULL
        WHERE samna_auth.claim_object.object_type = 'organization' AND samna_auth.claim_object.sa_deleted_at IS NULL AND samna_auth.user_claim.user_id = p_user_id
          AND (samna_auth.claim_object.object_id = v_row_owner_id
               OR (samna_auth.claim_object.inherits = TRUE AND samna_auth.claim_object.object_id IN (
                   SELECT po.id FROM samna_auth.get_parent_organizations(v_row_owner_id) po)));
    END IF;

    IF v_row_location_id IS NOT NULL AND p_object_type NOT IN ('organization', 'location') THEN
        RETURN QUERY
        SELECT samna_auth.claim_object.claim_id, c.sa_access_level, v_row_owner_id, v_row_location_id
        FROM samna_auth.claim_object
        INNER JOIN samna_auth.user_claim ON samna_auth.user_claim.claim_id = samna_auth.claim_object.claim_id AND samna_auth.user_claim.sa_deleted_at IS NULL AND (samna_auth.user_claim.starts_at IS NULL OR samna_auth.user_claim.starts_at <= CURRENT_TIMESTAMP) AND (samna_auth.user_claim.ends_at IS NULL OR samna_auth.user_claim.ends_at >= CURRENT_TIMESTAMP)
    INNER JOIN samna_auth.claim ON samna_auth.claim.id = samna_auth.claim_object.claim_id AND c.sa_deleted_at IS NULL
        WHERE samna_auth.claim_object.object_type = 'location' AND samna_auth.claim_object.sa_deleted_at IS NULL AND samna_auth.user_claim.user_id = p_user_id
          AND (samna_auth.claim_object.object_id = v_row_location_id
               OR (samna_auth.claim_object.inherits = TRUE AND samna_auth.claim_object.object_id IN (
                   SELECT pl.id FROM samna_auth.get_parent_locations(v_row_location_id) pl)));
    END IF;

    IF v_row_parent_id IS NOT NULL AND p_object_type NOT IN ('organization', 'location') THEN
        RETURN QUERY
        SELECT samna_auth.claim_object.claim_id, c.sa_access_level, v_row_owner_id, v_row_location_id
        FROM samna_auth.claim_object
        INNER JOIN samna_auth.user_claim ON samna_auth.user_claim.claim_id = samna_auth.claim_object.claim_id AND samna_auth.user_claim.sa_deleted_at IS NULL AND (samna_auth.user_claim.starts_at IS NULL OR samna_auth.user_claim.starts_at <= CURRENT_TIMESTAMP) AND (samna_auth.user_claim.ends_at IS NULL OR samna_auth.user_claim.ends_at >= CURRENT_TIMESTAMP)
    INNER JOIN samna_auth.claim ON samna_auth.claim.id = samna_auth.claim_object.claim_id AND c.sa_deleted_at IS NULL
        WHERE samna_auth.claim_object.object_type = p_object_type AND samna_auth.claim_object.sa_deleted_at IS NULL
          AND samna_auth.user_claim.user_id = p_user_id AND samna_auth.claim_object.inherits = TRUE
          AND samna_auth.claim_object.object_id IN (
              SELECT po.id FROM samna_auth.get_parent_objects(v_table_name, p_object_id) po
              WHERE po.id != p_object_id);
    END IF;
END;
$$ LANGUAGE plpgsql STABLE;

-- ============================================================================
-- find_affected_users_for_row (INTERNAL)
-- Given an object mutation, returns user_ids whose access may be affected.
-- Called from: tg_object_mutation.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.find_affected_users_for_row(
    p_table_name TEXT,
    p_object_type TEXT,
    p_object_id UUID,
    p_sa_owner_id UUID
) RETURNS UUID[] AS $$
DECLARE
    v_users UUID[];
BEGIN
    SELECT ARRAY_AGG(DISTINCT samna_auth.user_object.user_id) INTO v_users
    FROM samna_auth.user_object
    WHERE (samna_auth.user_object.object_id = p_object_id AND samna_auth.user_object.object_type = p_object_type)
       OR (samna_auth.user_object.sa_owner_id = p_sa_owner_id);
    RETURN COALESCE(v_users, ARRAY[]::UUID[]);
END;
$$ LANGUAGE plpgsql STABLE;

-- ============================================================================
-- reconcile_user_object (INTERNAL)
-- Recomputes user_object rows for one (user, object, type) triple.
-- Called from: tg_object_mutation, tg_claim_mutation, reconcile_if_pending.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.reconcile_user_object(
    p_user_id UUID,
    p_object_id UUID,
    p_object_type TEXT
) RETURNS VOID AS $$
DECLARE
    v_computed RECORD;
    v_id TEXT;
BEGIN
    DELETE FROM samna_auth.user_object
    WHERE user_id = p_user_id AND object_id = p_object_id AND object_type = p_object_type;

    DELETE FROM samna_auth.object_users
    WHERE object_id = p_object_id AND object_type = p_object_type AND user_id = p_user_id;

    FOR v_computed IN
        SELECT * FROM samna_auth.compute_object_claims(p_user_id, p_object_id, p_object_type)
    LOOP
        v_id := samna_auth.composite_id(v_computed.claim_id::text, p_object_id::text, p_object_type);
        INSERT INTO samna_auth.user_object (
            id, user_id, object_id, object_type, claim_id,
            sa_access_level, sa_owner_id, sa_location_id
        ) VALUES (
            v_id, p_user_id, p_object_id, p_object_type, v_computed.claim_id,
            v_computed.sa_access_level, v_computed.sa_owner_id, v_computed.sa_location_id
        ) ON CONFLICT (id) DO UPDATE SET
            sa_access_level = EXCLUDED.sa_access_level,
            sa_owner_id = EXCLUDED.sa_owner_id,
            sa_location_id = EXCLUDED.sa_location_id,
            sa_updated_at = now();
    END LOOP;

    INSERT INTO samna_auth.object_users (id, object_id, object_type, user_id)
    VALUES (
        samna_auth.composite_id(p_object_id::text, p_object_type, p_user_id::text),
        p_object_id, p_object_type, p_user_id
    ) ON CONFLICT (id) DO NOTHING;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- expand_claim_reachable_objects (INTERNAL)
-- Given a claim_id, returns every (object_id, object_type) reachable through
-- its bindings and inheritance.
-- Called from: tg_claim_mutation.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.expand_claim_reachable_objects(
    p_claim_id UUID
) RETURNS TABLE (object_id UUID, object_type TEXT) AS $$
DECLARE
    v_binding RECORD;
BEGIN
    FOR v_binding IN
        SELECT samna_auth.claim_object.object_id, samna_auth.claim_object.object_type, samna_auth.claim_object.inherits
        FROM samna_auth.claim_object
        WHERE samna_auth.claim_object.claim_id = p_claim_id AND samna_auth.claim_object.sa_deleted_at IS NULL
    LOOP
        RETURN QUERY SELECT v_binding.object_id, v_binding.object_type;
        IF v_binding.inherits AND v_binding.object_type = 'organization' THEN
            RETURN QUERY
            SELECT child.id, 'organization'::text
            FROM samna_auth.get_child_organizations(v_binding.object_id) child;
        ELSIF v_binding.inherits AND v_binding.object_type = 'location' THEN
            RETURN QUERY
            SELECT child.id, 'location'::text
            FROM samna_auth.get_child_locations(v_binding.object_id) child;
        END IF;
    END LOOP;
END;
$$ LANGUAGE plpgsql STABLE;

-- ============================================================================
-- reconcile_if_pending (INTERNAL)
-- Checks reconcile_queue for pending entries for a user. If found, reconciles
-- inline before the read returns. Zanzibar pattern: stale reads self heal.
-- Called from: get_access, get_claims, get_objects.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.reconcile_if_pending(p_user_id UUID)
RETURNS VOID AS $$
DECLARE
    v_pending RECORD;
BEGIN
    FOR v_pending IN
        DELETE FROM samna_auth.reconcile_queue
        WHERE user_id = p_user_id RETURNING object_id, object_type
    LOOP
        PERFORM samna_auth.reconcile_user_object(p_user_id, v_pending.object_id, v_pending.object_type);
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- READ FUNCTIONS (PUBLIC)
-- ============================================================================

-- ============================================================================
-- get_access (PUBLIC)
-- Returns the most privileged claim granting a user access to one object.
-- Called from: middleware db package (GetAccess, HasAccess).
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.get_access(
    p_user_id UUID,
    p_object_id UUID,
    p_access_level INTEGER DEFAULT 8
) RETURNS TABLE (
    claim_id UUID,
    sa_access_level INTEGER,
    sa_owner_id UUID,
    sa_location_id UUID
) AS $$
BEGIN
    PERFORM samna_auth.reconcile_if_pending(p_user_id);
    RETURN QUERY
    SELECT samna_auth.user_object.claim_id, samna_auth.user_object.sa_access_level, samna_auth.user_object.sa_owner_id, samna_auth.user_object.sa_location_id
    FROM samna_auth.user_object
    WHERE samna_auth.user_object.user_id = p_user_id AND samna_auth.user_object.object_id = p_object_id
      AND samna_auth.user_object.sa_access_level <= p_access_level
    ORDER BY samna_auth.user_object.sa_access_level ASC LIMIT 1;
END;
$$ LANGUAGE plpgsql STABLE;

-- ============================================================================
-- get_claims (PUBLIC, overload: user + object)
-- Returns all claims a user holds against one object.
-- Called from: middleware db package (GetObjectClaims).
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.get_claims(
    p_user_id UUID,
    p_object_id UUID
) RETURNS SETOF samna_auth.claim AS $$
BEGIN
    RETURN QUERY
    SELECT c.* FROM samna_auth.user_object
    JOIN samna_auth.claim c ON c.id = samna_auth.user_object.claim_id
    WHERE samna_auth.user_object.user_id = p_user_id AND samna_auth.user_object.object_id = p_object_id AND c.sa_deleted_at IS NULL
    ORDER BY samna_auth.user_object.sa_access_level ASC;
END;
$$ LANGUAGE plpgsql STABLE;

-- ============================================================================
-- get_claims (PUBLIC, overload: user + level)
-- Returns all claims a user holds across all objects.
-- Called from: middleware db package (GetClaims, ClaimWhere).
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.get_claims(
    p_user_id UUID,
    p_access_level INTEGER DEFAULT 8
) RETURNS SETOF samna_auth.claim AS $$
BEGIN
    PERFORM samna_auth.reconcile_if_pending(p_user_id);
    RETURN QUERY
    SELECT DISTINCT c.* FROM samna_auth.user_object
    JOIN samna_auth.claim c ON c.id = samna_auth.user_object.claim_id
    WHERE samna_auth.user_object.user_id = p_user_id AND samna_auth.user_object.sa_access_level <= p_access_level AND c.sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE;

-- ============================================================================
-- get_objects (PUBLIC)
-- Returns object ids of a given type the user can see.
-- Called from: middleware db package (ObjectWhere).
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.get_objects(
    p_user_id UUID,
    p_object_type TEXT,
    p_access_level INTEGER DEFAULT 8
) RETURNS TABLE(object_id UUID) AS $$
BEGIN
    PERFORM samna_auth.reconcile_if_pending(p_user_id);
    RETURN QUERY
    SELECT DISTINCT samna_auth.user_object.object_id FROM samna_auth.user_object
    WHERE samna_auth.user_object.user_id = p_user_id AND samna_auth.user_object.object_type = p_object_type
      AND samna_auth.user_object.sa_access_level <= p_access_level;
END;
$$ LANGUAGE plpgsql STABLE;

-- ============================================================================
-- get_organizations (PUBLIC)
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.get_organizations(
    p_user_id UUID,
    p_access_level INTEGER DEFAULT 8
) RETURNS SETOF samna_auth.organization AS $$
BEGIN
    RETURN QUERY
    SELECT o.* FROM samna_auth.organization o
    WHERE o.sa_deleted_at IS NULL AND o.id IN (
        SELECT samna_auth.user_object.object_id FROM samna_auth.user_object
        WHERE samna_auth.user_object.user_id = p_user_id AND samna_auth.user_object.object_type = 'organization'
          AND samna_auth.user_object.sa_access_level <= p_access_level);
END;
$$ LANGUAGE plpgsql STABLE;

-- ============================================================================
-- get_locations (PUBLIC)
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.get_locations(
    p_user_id UUID,
    p_access_level INTEGER DEFAULT 8
) RETURNS SETOF samna_auth.location AS $$
BEGIN
    RETURN QUERY
    SELECT l.* FROM samna_auth.location l
    WHERE l.sa_deleted_at IS NULL AND l.id IN (
        SELECT samna_auth.user_object.object_id FROM samna_auth.user_object
        WHERE samna_auth.user_object.user_id = p_user_id AND samna_auth.user_object.object_type = 'location'
          AND samna_auth.user_object.sa_access_level <= p_access_level);
END;
$$ LANGUAGE plpgsql STABLE;

-- ============================================================================
-- get_users (PUBLIC)
-- Three sources: self, direct claims on users, shared object history.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.get_users(
    p_user_id UUID,
    p_access_level INTEGER DEFAULT 8
) RETURNS SETOF samna_auth.samna_user AS $$
BEGIN
    RETURN QUERY
    SELECT u.* FROM samna_auth.samna_user u WHERE u.user_id = p_user_id
    UNION
    SELECT u.* FROM samna_auth.samna_user u
    WHERE u.sa_deleted_at IS NULL AND u.id IN (
        SELECT samna_auth.user_object.object_id FROM samna_auth.user_object
        WHERE samna_auth.user_object.user_id = p_user_id AND samna_auth.user_object.object_type = 'user'
          AND samna_auth.user_object.sa_access_level <= p_access_level)
    UNION
    SELECT u.* FROM samna_auth.samna_user u
    JOIN samna_auth.user_users uu ON uu.target_user_id = u.id
    WHERE u.sa_deleted_at IS NULL AND uu.viewer_id = p_user_id;
END;
$$ LANGUAGE plpgsql STABLE;

-- ============================================================================
-- INIT (PUBLIC)
-- ============================================================================

-- ============================================================================
-- init_auth_tables (PUBLIC)
-- Registers application tables with samna_auth. Attaches tg_object_mutation
-- and populates user_object for existing rows.
-- Called from: V1.4__init.sql, middleware db.InitAuthTables.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.init_auth_tables(VARIADIC p_table_names TEXT[])
RETURNS VOID AS $$
DECLARE
    v_table TEXT;
BEGIN
    FOREACH v_table IN ARRAY p_table_names LOOP
        EXECUTE format($f$
            DROP TRIGGER IF EXISTS samna_auth_object_mutation ON %s;
            CREATE TRIGGER samna_auth_object_mutation
            AFTER INSERT OR UPDATE OR DELETE ON %s
            FOR EACH ROW EXECUTE FUNCTION samna_auth.tg_object_mutation();
        $f$, v_table, v_table);
    END LOOP;
    PERFORM samna_auth.init_user_object(p_table_names);
    RAISE NOTICE 'samna_auth initialized for % tables', array_length(p_table_names, 1);
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- init_user_object (INTERNAL)
-- Populates user_object for existing rows. Supports resumption and progress.
-- Called from: init_auth_tables.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.init_user_object(p_table_names TEXT[])
RETURNS VOID AS $$
DECLARE
    v_table TEXT; v_object_type TEXT; v_schema TEXT; v_tbl TEXT;
    v_user_id UUID; v_object_id UUID;
    v_count INT; v_total INT;
BEGIN
    FOREACH v_table IN ARRAY p_table_names LOOP
        IF position('.' in v_table) > 0 THEN
            v_schema := split_part(v_table, '.', 1);
            v_tbl := split_part(v_table, '.', 2);
            v_object_type := v_tbl;
        ELSE
            v_schema := 'public'; v_tbl := v_table; v_object_type := v_table;
        END IF;
        v_count := 0;
        EXECUTE format('SELECT COUNT(*) FROM %I.%I WHERE sa_deleted_at IS NULL', v_schema, v_tbl)
        INTO v_total;
        FOR v_object_id IN
            EXECUTE format('SELECT id FROM %I.%I WHERE sa_deleted_at IS NULL', v_schema, v_tbl)
        LOOP
            FOR v_user_id IN
                SELECT DISTINCT samna_auth.user_claim.user_id FROM samna_auth.user_claim WHERE samna_auth.user_claim.sa_deleted_at IS NULL
            LOOP
                BEGIN
                    PERFORM samna_auth.reconcile_user_object(v_user_id, v_object_id, v_object_type);
                EXCEPTION WHEN OTHERS THEN
                    RAISE NOTICE 'init_user_object: error for user=%, object=%, type=%: %',
                        v_user_id, v_object_id, v_object_type, SQLERRM;
                END;
            END LOOP;
            v_count := v_count + 1;
            IF v_count % 100 = 0 THEN
                RAISE NOTICE 'init_user_object: % — %/% objects', v_object_type, v_count, v_total;
            END IF;
        END LOOP;
        RAISE NOTICE 'init_user_object: % complete — % objects', v_object_type, v_total;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- GRAPH FUNCTIONS (PUBLIC)
-- ============================================================================

-- ============================================================================
-- get_claim_graph (PUBLIC)
-- Returns the claim graph for a user as a flat list of typed nodes.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.get_claim_graph(
    p_user_id UUID,
    p_access_level INTEGER DEFAULT 30
)
RETURNS TABLE(id UUID, label TEXT, type TEXT, data JSONB) AS $$
WITH accessible_claims AS (
    SELECT c.* FROM samna_auth.claim c
    INNER JOIN samna_auth.get_organizations(p_user_id, p_access_level) o ON o.id = c.sa_owner_id
    WHERE c.sa_deleted_at IS NULL AND c.type = 'user'
),
accessible_claim_ids AS (SELECT ac.id FROM accessible_claims ac),
accessible_user_claims AS (
    SELECT samna_auth.user_claim.* FROM samna_auth.user_claim
    INNER JOIN accessible_claim_ids aci ON aci.id = samna_auth.user_claim.claim_id
    WHERE samna_auth.user_claim.sa_deleted_at IS NULL
),
accessible_claim_objects AS (
    SELECT samna_auth.claim_object.* FROM samna_auth.claim_object
    INNER JOIN accessible_claim_ids aci ON aci.id = samna_auth.claim_object.claim_id
    WHERE samna_auth.claim_object.sa_deleted_at IS NULL
)
SELECT c.id, c.name as label, 'claim'::TEXT as type, to_jsonb(c.*) as data FROM accessible_claims c
UNION ALL
SELECT samna_auth.user_claim.id, samna_auth.user_claim.reason as label, 'user_claim'::TEXT as type, to_jsonb(samna_auth.user_claim.*) as data FROM accessible_user_claims uc
UNION ALL
SELECT DISTINCT ON (u.id) u.id, CONCAT(u.first_name, ' ', u.last_name) as label,
    'user'::TEXT as type, to_jsonb(u.*) as data
FROM samna_auth.get_users(p_user_id, p_access_level) u
WHERE u.user_id IN (SELECT DISTINCT auc.user_id FROM accessible_user_claims auc)
UNION ALL
SELECT DISTINCT ON (o.id) o.id, o.name as label, 'organization'::TEXT as type, to_jsonb(o.*) as data
FROM samna_auth.get_organizations(p_user_id, p_access_level) o
WHERE o.id IN (
    SELECT sa_owner_id FROM accessible_claims WHERE sa_owner_id IS NOT NULL
    UNION SELECT sa_root_id FROM accessible_claims WHERE sa_root_id IS NOT NULL
    UNION SELECT auc.sa_owner_id FROM accessible_user_claims auc WHERE auc.sa_owner_id IS NOT NULL
    UNION SELECT aco.sa_owner_id FROM accessible_claim_objects aco WHERE aco.sa_owner_id IS NOT NULL
    UNION SELECT aco.object_id FROM accessible_claim_objects aco WHERE aco.object_type = 'organization')
UNION ALL
SELECT DISTINCT ON (l.id) l.id,
    COALESCE(l.name, CONCAT(l.code, ' ', l.code_type), 'location') as label,
    'location'::TEXT as type, to_jsonb(l.*) as data
FROM samna_auth.get_locations(p_user_id, p_access_level) l
WHERE l.id IN (SELECT aco.object_id FROM accessible_claim_objects aco WHERE aco.object_type = 'location')
UNION ALL
SELECT samna_auth.claim_object.object_id as id, COALESCE(samna_auth.claim_object.reason, samna_auth.claim_object.object_type) as label,
    samna_auth.claim_object.object_type::TEXT as type,
    jsonb_build_object('object_id', samna_auth.claim_object.object_id, 'object_type', samna_auth.claim_object.object_type, 'claim_id', samna_auth.claim_object.claim_id) as data
FROM accessible_claim_objects co
WHERE samna_auth.claim_object.object_type NOT IN ('organization', 'location', 'user', 'claim');
$$ LANGUAGE SQL STABLE;

-- ============================================================================
-- get_owner_graph (PUBLIC)
-- Returns the ownership hierarchy for a user as a flat list of typed nodes.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.get_owner_graph(
    p_user_id UUID,
    p_access_level INTEGER DEFAULT 30
)
RETURNS TABLE(id UUID, label TEXT, type TEXT, data JSONB) AS $$
WITH accessible_orgs AS (
    SELECT DISTINCT o.* FROM samna_auth.get_organizations(p_user_id, p_access_level) o
),
accessible_locs AS (
    SELECT DISTINCT l.* FROM samna_auth.get_locations(p_user_id, p_access_level) l
    WHERE l.sa_owner_id IN (SELECT ao.id FROM accessible_orgs ao)
)
SELECT o.id, o.name as label, 'organization'::TEXT as type, to_jsonb(o.*) as data FROM accessible_orgs o
UNION ALL
SELECT DISTINCT ON (l.id) l.id,
    COALESCE(l.name, CONCAT(l.code, ' ', l.code_type), 'location') as label,
    'location'::TEXT as type, to_jsonb(l.*) as data
FROM accessible_locs l;
$$ LANGUAGE SQL STABLE;

-- ============================================================================
-- search_objects (PUBLIC)
-- Full text search across user_object for a given user.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.search_objects(
    p_user_id UUID,
    p_query TEXT,
    p_object_types TEXT[] DEFAULT NULL,
    p_access_level INTEGER DEFAULT 8,
    p_limit INTEGER DEFAULT 20,
    p_offset INTEGER DEFAULT 0
) RETURNS TABLE (
    object_id UUID,
    object_type TEXT,
    sa_name TEXT,
    sa_description TEXT,
    sa_link TEXT,
    sa_access_level INTEGER,
    sa_owner_id UUID,
    sa_location_id UUID,
    rank REAL
) AS $$
BEGIN
    RETURN QUERY
    SELECT samna_auth.user_object.object_id, samna_auth.user_object.object_type, samna_auth.user_object.sa_name, samna_auth.user_object.sa_description, samna_auth.user_object.sa_link,
           samna_auth.user_object.sa_access_level, samna_auth.user_object.sa_owner_id, samna_auth.user_object.sa_location_id,
           ts_rank(samna_auth.user_object.search_vector, plainto_tsquery('english', p_query)) AS rank
    FROM samna_auth.user_object
    WHERE samna_auth.user_object.user_id = p_user_id
      AND samna_auth.user_object.sa_access_level <= p_access_level
      AND (p_object_types IS NULL OR samna_auth.user_object.object_type = ANY(p_object_types))
      AND samna_auth.user_object.search_vector @@ plainto_tsquery('english', p_query)
    ORDER BY rank DESC
    LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION samna_auth.search(
    p_user_id UUID,
    p_query TEXT,
    p_object_types TEXT[] DEFAULT NULL,
    p_access_level INTEGER DEFAULT 8,
    p_limit INTEGER DEFAULT 20,
    p_offset INTEGER DEFAULT 0
) RETURNS TABLE (id UUID, object_type TEXT, name TEXT, description TEXT, rank REAL) AS $$
DECLARE
    v_tsquery tsquery := websearch_to_tsquery('english', p_query);
BEGIN
    RETURN QUERY

    SELECT samna_auth.organization.id, 'organization'::text,
           samna_auth.organization.name::text, samna_auth.organization.description::text,
           ts_rank(samna_auth.organization.search_vector, v_tsquery)::real
    FROM samna_auth.organization
    WHERE samna_auth.organization.id IN (SELECT samna_auth.get_objects(p_user_id, 'organization', p_access_level))
      AND samna_auth.organization.sa_deleted_at IS NULL
      AND samna_auth.organization.search_vector @@ v_tsquery
      AND (p_object_types IS NULL OR 'organization' = ANY(p_object_types))

    UNION ALL

    SELECT samna_auth.location.id, 'location'::text,
           samna_auth.location.name::text, samna_auth.location.description::text,
           ts_rank(samna_auth.location.search_vector, v_tsquery)::real
    FROM samna_auth.location
    WHERE samna_auth.location.id IN (SELECT samna_auth.get_objects(p_user_id, 'location', p_access_level))
      AND samna_auth.location.sa_deleted_at IS NULL
      AND samna_auth.location.search_vector @@ v_tsquery
      AND (p_object_types IS NULL OR 'location' = ANY(p_object_types))

    UNION ALL

    SELECT samna_auth.samna_user.id, 'user'::text,
           (COALESCE(samna_auth.samna_user.first_name, '') || ' ' || COALESCE(samna_auth.samna_user.last_name, ''))::text,
           samna_auth.samna_user.email::text,
           ts_rank(samna_auth.samna_user.search_vector, v_tsquery)::real
    FROM samna_auth.samna_user
    WHERE samna_auth.samna_user.user_id IN (SELECT samna_auth.get_objects(p_user_id, 'user', p_access_level))
      AND samna_auth.samna_user.sa_deleted_at IS NULL
      AND samna_auth.samna_user.search_vector @@ v_tsquery
      AND (p_object_types IS NULL OR 'user' = ANY(p_object_types))

    UNION ALL

    SELECT samna_auth.claim.id, 'claim'::text,
           samna_auth.claim.name::text, samna_auth.claim.description::text,
           ts_rank(samna_auth.claim.search_vector, v_tsquery)::real
    FROM samna_auth.claim
    WHERE samna_auth.claim.id IN (SELECT samna_auth.get_objects(p_user_id, 'claim', p_access_level))
      AND samna_auth.claim.sa_deleted_at IS NULL
      AND samna_auth.claim.search_vector @@ v_tsquery
      AND (p_object_types IS NULL OR 'claim' = ANY(p_object_types))

    ORDER BY rank DESC
    LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql STABLE;
