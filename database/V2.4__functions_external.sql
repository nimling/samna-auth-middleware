-- ============================================================================
-- Claimius V2.4 External Functions
-- ----------------------------------------------------------------------------
-- The public surface. All consumer reads go through these. Direct table
-- queries are forbidden by the role grant model: claimius_reader and
-- claimius_disciple_client only have EXECUTE on these functions.
--
-- Every read function:
--   1. Calls reconcile_if_pending to drain any pending disciple side queue
--   2. Calls check_user_active which cascades soft delete if applicable
--   3. Filters by sa_deleted_at and time validity (starts_at, ends_at)
--   4. Returns the result
-- ============================================================================

-- ----------------------------------------------------------------------------
-- get_access
-- Returns the active claim grants the user has against one object. Joins
-- user_object grants array against user_claim and claim, filters by
-- sa_deleted_at and time validity. Returns the strongest (lowest sa_access_level)
-- surviving grant.
--
-- Contract: when claim_id is non-null, user_claim_id is also populated. This
-- is the actor token callers use as sa_created_by on follow-on writes.
-- When direct_grant is true and claim_id is null, user_claim_id is also null;
-- callers needing an actor for direct-only access must resolve a fallback
-- user_claim themselves (typically the user's bootstrap user_claim for the app).
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_access(
    p_user_id       UUID,
    p_app_id        UUID,
    p_object_id     UUID,
    p_object_type   TEXT,
    p_min_level     INTEGER DEFAULT 8
) RETURNS TABLE (
                    user_object_id      TEXT,
                    user_claim_id       UUID,
                    claim_id            UUID,
                    sa_access_level     INTEGER,
                    sa_owner_id         UUID,
                    sa_root_id          UUID,
                    sa_location_id      UUID,
                    direct_grant        BOOLEAN
                ) AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        WITH uo AS (
            SELECT u.*
            FROM claimius.user_object u
            WHERE u.user_id = p_user_id
              AND u.app_id = p_app_id
              AND u.object_id = p_object_id
              AND u.object_type = p_object_type
              AND u.sa_access_level <= p_min_level
        ),
             surviving AS (
                 SELECT
                     uo.id AS user_object_id,
                     uc.id AS user_claim_id,
                     (g ->> 'claim_id')::UUID AS claim_id,
                     (g ->> 'level')::INTEGER AS lvl,
                     uo.sa_owner_id,
                     uo.sa_root_id,
                     uo.sa_location_id,
                     FALSE AS direct_grant
                 FROM uo
                          CROSS JOIN LATERAL jsonb_array_elements(uo.grants) g
                          JOIN claimius.user_claim uc ON uc.claim_id = (g ->> 'claim_id')::UUID
                     AND uc.user_id = p_user_id
                     AND uc.app_id = p_app_id
                     AND uc.sa_deleted_at IS NULL
                     AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                     AND (uc.ends_at IS NULL OR uc.ends_at > now())
                          JOIN claimius.claim c ON c.id = uc.claim_id
                     AND c.sa_deleted_at IS NULL
                 UNION ALL
                 SELECT
                     uo.id, NULL::UUID, NULL::UUID, 0,
                     uo.sa_owner_id, uo.sa_root_id, uo.sa_location_id, TRUE
                 FROM uo
                 WHERE uo.direct_grant = TRUE
             )
        SELECT s.user_object_id, s.user_claim_id, s.claim_id, s.lvl,
               s.sa_owner_id, s.sa_root_id, s.sa_location_id, s.direct_grant
        FROM surviving s
        ORDER BY s.lvl ASC
        LIMIT 1;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_access IS 'Strongest active claim based grant for one (user, app, object). user_claim_id is the actor token for follow-on writes.';

-- ----------------------------------------------------------------------------
-- get_direct_access
-- Returns whether the user has direct (creator) access to the object.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_direct_access(
    p_user_id       UUID,
    p_app_id        UUID,
    p_object_id     UUID,
    p_object_type   TEXT
) RETURNS BOOLEAN AS $$
DECLARE
    v_direct BOOLEAN;
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN FALSE;
    END IF;

    SELECT direct_grant INTO v_direct
    FROM claimius.user_object
    WHERE user_id = p_user_id
      AND app_id = p_app_id
      AND object_id = p_object_id
      AND object_type = p_object_type;

    RETURN coalesce(v_direct, FALSE);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_direct_access IS 'True if user holds direct (creator) grant on the object.';

-- ----------------------------------------------------------------------------
-- get_objects
-- Returns object ids of the given object_type that the user can see at or
-- above the requested level.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_objects(
    p_user_id       UUID,
    p_app_id        UUID,
    p_object_type   TEXT,
    p_min_level     INTEGER DEFAULT 8
) RETURNS TABLE(
                   object_id           UUID,
                   user_claim_id       UUID,
                   sa_access_level     INTEGER,
                   sa_owner_id         UUID,
                   sa_location_id      UUID,
                   sa_root_id          UUID,
                   sa_name             TEXT,
                   sa_description      TEXT,
                   sa_link             TEXT,
                   direct_grant        BOOLEAN
               ) AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    -- For each (user, object) row, pick the strongest (lowest level)
    -- surviving claim grant and its user_claim id. If only direct_grant
    -- applies, user_claim_id is null and direct_grant is true.
    RETURN QUERY
        SELECT
            uo.object_id,
            best.user_claim_id,
            uo.sa_access_level,
            uo.sa_owner_id,
            uo.sa_location_id,
            uo.sa_root_id,
            uo.sa_name,
            uo.sa_description,
            uo.sa_link,
            uo.direct_grant AND best.user_claim_id IS NULL AS direct_grant
        FROM claimius.user_object uo
                 LEFT JOIN LATERAL (
            SELECT uc.id AS user_claim_id, (g ->> 'level')::INTEGER AS lvl
            FROM jsonb_array_elements(uo.grants) g
                     JOIN claimius.user_claim uc ON uc.claim_id = (g ->> 'claim_id')::UUID
                AND uc.user_id = p_user_id
                AND uc.app_id = p_app_id
                AND uc.sa_deleted_at IS NULL
                AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                AND (uc.ends_at IS NULL OR uc.ends_at > now())
                     JOIN claimius.claim c ON c.id = uc.claim_id AND c.sa_deleted_at IS NULL
            ORDER BY (g ->> 'level')::INTEGER ASC
            LIMIT 1
            ) best ON TRUE
        WHERE uo.user_id = p_user_id
          AND uo.app_id = p_app_id
          AND uo.object_type = p_object_type
          AND uo.sa_access_level <= p_min_level
          AND (best.user_claim_id IS NOT NULL OR uo.direct_grant)
        ORDER BY uo.sa_access_level ASC, uo.object_id;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_objects IS 'Object ids and metadata for objects the user can see. user_claim_id is the actor token from the strongest surviving claim grant; null when only direct_grant applies.';

-- ----------------------------------------------------------------------------
-- get_organizations
-- Returns organizations the user can see at the requested level.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_organizations(
    p_user_id       UUID,
    p_app_id        UUID,
    p_min_level     INTEGER DEFAULT 8
) RETURNS SETOF claimius.organization AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT o.*
        FROM claimius.organization o
        WHERE o.app_id = p_app_id
          AND o.sa_deleted_at IS NULL
          AND o.id IN (
            SELECT object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.organization', p_min_level)
        );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_organizations IS 'Organizations the user can see at the requested level.';

-- ----------------------------------------------------------------------------
-- get_locations
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_locations(
    p_user_id       UUID,
    p_app_id        UUID,
    p_min_level     INTEGER DEFAULT 8
) RETURNS SETOF claimius.location AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT l.*
        FROM claimius.location l
        WHERE l.app_id = p_app_id
          AND l.sa_deleted_at IS NULL
          AND l.id IN (
            SELECT object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.location', p_min_level)
        );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_locations IS 'Locations the user can see at the requested level.';

-- ----------------------------------------------------------------------------
-- get_users
-- Returns the users this user can see in this app: themselves, users they
-- hold direct claims on, plus users who share any object with them via
-- user_users.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_users(
    p_user_id       UUID,
    p_app_id        UUID,
    p_min_level     INTEGER DEFAULT 8
) RETURNS SETOF claimius.samna_user AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT u.*
        FROM claimius.samna_user u
        WHERE u.app_id = p_app_id
          AND u.sa_deleted_at IS NULL
          AND u.status = 'active'
          AND (
            u.user_id = p_user_id
                OR u.user_id IN (
                SELECT target_user_id FROM claimius.user_users
                WHERE app_id = p_app_id AND viewer_id = p_user_id
            )
                OR u.user_id IN (
                SELECT object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.samna_user', p_min_level)
            )
            );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_users IS 'Users the calling user can see in this app.';

-- ----------------------------------------------------------------------------
-- get_claims (overload: by user)
-- Returns every claim the user holds at the requested level.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_claims(
    p_user_id       UUID,
    p_app_id        UUID,
    p_min_level     INTEGER DEFAULT 8
) RETURNS SETOF claimius.claim AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT c.*
        FROM claimius.claim c
                 JOIN claimius.user_claim uc ON uc.claim_id = c.id
        WHERE uc.user_id = p_user_id
          AND uc.app_id = p_app_id
          AND uc.sa_deleted_at IS NULL
          AND (uc.starts_at IS NULL OR uc.starts_at <= now())
          AND (uc.ends_at IS NULL OR uc.ends_at > now())
          AND c.sa_deleted_at IS NULL
          AND c.sa_access_level <= p_min_level
        ORDER BY c.sa_access_level ASC;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_claims(UUID, UUID, INTEGER) IS 'All claims the user holds at the requested level.';

-- ----------------------------------------------------------------------------
-- get_claims (overload: against an object)
-- Returns every claim the user holds against one object, ordered by level.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_claims(
    p_user_id       UUID,
    p_app_id        UUID,
    p_object_id     UUID,
    p_object_type   TEXT
) RETURNS SETOF claimius.claim AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT DISTINCT c.*
        FROM claimius.user_object uo
                 CROSS JOIN LATERAL jsonb_array_elements(uo.grants) g
                 JOIN claimius.user_claim uc ON uc.claim_id = (g ->> 'claim_id')::UUID
            AND uc.user_id = p_user_id
            AND uc.app_id = p_app_id
            AND uc.sa_deleted_at IS NULL
            AND (uc.starts_at IS NULL OR uc.starts_at <= now())
            AND (uc.ends_at IS NULL OR uc.ends_at > now())
                 JOIN claimius.claim c ON c.id = uc.claim_id AND c.sa_deleted_at IS NULL
        WHERE uo.user_id = p_user_id
          AND uo.app_id = p_app_id
          AND uo.object_id = p_object_id
          AND uo.object_type = p_object_type
        ORDER BY c.sa_access_level ASC;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_claims(UUID, UUID, UUID, TEXT) IS 'All claims the user holds against one object.';

-- ----------------------------------------------------------------------------
-- get_audit
-- Returns audit rows for organizations the user can see.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_audit(
    p_user_id       UUID,
    p_app_id        UUID,
    p_min_level     INTEGER DEFAULT 8,
    p_limit         INTEGER DEFAULT 100,
    p_offset        INTEGER DEFAULT 0
) RETURNS SETOF claimius.audit AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT a.*
        FROM claimius.audit a
        WHERE a.app_id = p_app_id
          AND a.sa_owner_id IN (
            SELECT object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.organization', p_min_level)
        )
        ORDER BY a.sa_created_at DESC
        LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_audit IS 'Audit rows for organizations the user can see.';

-- ----------------------------------------------------------------------------
-- get_secrets
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_secrets(
    p_user_id       UUID,
    p_app_id        UUID,
    p_min_level     INTEGER DEFAULT 8
) RETURNS SETOF claimius.samna_secret AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT s.*
        FROM claimius.samna_secret s
        WHERE s.app_id = p_app_id
          AND s.sa_deleted_at IS NULL
          AND s.id IN (
            SELECT object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.samna_secret', p_min_level)
        );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_secrets IS 'Secrets the user can see at the requested level.';

-- ----------------------------------------------------------------------------
-- get_owner_graph
-- Returns the ownership hierarchy (organizations + locations) the user can
-- see, as a flat node list. For frontend rendering.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_owner_graph(
    p_user_id       UUID,
    p_app_id        UUID,
    p_min_level     INTEGER DEFAULT 30
) RETURNS TABLE(id UUID, label TEXT, type TEXT, parent_id UUID, data JSONB) AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    -- Organizations
    RETURN QUERY
        SELECT o.id, o.name::TEXT, 'organization'::TEXT,
               CASE WHEN o.sa_owner_id = o.id THEN NULL ELSE o.sa_owner_id END,
               jsonb_build_object('description', o.description, 'type', o.type, 'level', o.sa_level)
        FROM claimius.organization o
        WHERE o.app_id = p_app_id
          AND o.sa_deleted_at IS NULL
          AND o.id IN (
            SELECT object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.organization', p_min_level)
        );

    -- Locations
    RETURN QUERY
        SELECT l.id, l.name::TEXT, 'location'::TEXT,
               CASE WHEN l.sa_parent_id = l.id THEN l.sa_owner_id ELSE l.sa_parent_id END,
               jsonb_build_object('description', l.description, 'type', l.type, 'level', l.sa_level,
                                  'longitude', l.longitude, 'latitude', l.latitude)
        FROM claimius.location l
        WHERE l.app_id = p_app_id
          AND l.sa_deleted_at IS NULL
          AND l.id IN (
            SELECT object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.location', p_min_level)
        );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_owner_graph IS 'Ownership graph (organizations + locations) for the user.';

-- ----------------------------------------------------------------------------
-- get_claim_graph
-- Returns the access graph: claim and user_claim nodes, plus the objects
-- those claims grant access to.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_claim_graph(
    p_user_id       UUID,
    p_app_id        UUID,
    p_min_level     INTEGER DEFAULT 30
) RETURNS TABLE(id UUID, label TEXT, type TEXT, parent_id UUID, data JSONB) AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    -- The user themselves
    RETURN QUERY
        SELECT u.id, coalesce(u.first_name || ' ' || u.last_name, u.email, u.user_id::TEXT)::TEXT,
               'user'::TEXT, NULL::UUID,
               jsonb_build_object('user_id', u.user_id, 'email', u.email, 'status', u.status)
        FROM claimius.samna_user u
        WHERE u.user_id = p_user_id AND u.app_id = p_app_id
          AND u.sa_deleted_at IS NULL;

    -- user_claims for this user
    RETURN QUERY
        SELECT uc.id, c.name::TEXT, 'user_claim'::TEXT,
               u.id,
               jsonb_build_object('claim_id', uc.claim_id, 'starts_at', uc.starts_at,
                                  'ends_at', uc.ends_at, 'reason', uc.reason)
        FROM claimius.user_claim uc
                 JOIN claimius.claim c ON c.id = uc.claim_id
                 JOIN claimius.samna_user u ON u.user_id = uc.user_id AND u.app_id = uc.app_id
        WHERE uc.user_id = p_user_id
          AND uc.app_id = p_app_id
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
          AND c.sa_access_level <= p_min_level;

    -- claim nodes referenced
    RETURN QUERY
        SELECT c.id, c.name::TEXT, 'claim'::TEXT, NULL::UUID,
               jsonb_build_object('description', c.description, 'sa_access_level', c.sa_access_level,
                                  'is_deny', c.is_deny, 'inherits', c.inherits, 'type', c.type)
        FROM claimius.claim c
        WHERE c.app_id = p_app_id
          AND c.sa_deleted_at IS NULL
          AND c.id IN (
            SELECT uc.claim_id FROM claimius.user_claim uc
            WHERE uc.user_id = p_user_id AND uc.app_id = p_app_id AND uc.sa_deleted_at IS NULL
        );

    -- objects reachable through those claims
    RETURN QUERY
        SELECT uo.object_id, uo.sa_name, uo.object_type, NULL::UUID,
               jsonb_build_object('description', uo.sa_description, 'sa_access_level', uo.sa_access_level,
                                  'sa_owner_id', uo.sa_owner_id, 'sa_location_id', uo.sa_location_id)
        FROM claimius.user_object uo
        WHERE uo.user_id = p_user_id
          AND uo.app_id = p_app_id
          AND uo.sa_access_level <= p_min_level;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_claim_graph IS 'Access graph (claims, user_claims, reachable objects) for the user.';

-- ----------------------------------------------------------------------------
-- search_objects
-- Full text search over user_object, gated by access. Matches against the
-- precomputed search_vector column.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.search_objects(
    p_user_id       UUID,
    p_app_id        UUID,
    p_query         TEXT,
    p_object_types  TEXT[] DEFAULT NULL,
    p_min_level     INTEGER DEFAULT 8,
    p_limit         INTEGER DEFAULT 20,
    p_offset        INTEGER DEFAULT 0
) RETURNS TABLE(
                   object_id           UUID,
                   object_type         TEXT,
                   user_claim_id       UUID,
                   sa_name             TEXT,
                   sa_description     TEXT,
                   sa_link             TEXT,
                   sa_access_level     INTEGER,
                   sa_owner_id         UUID,
                   sa_location_id      UUID,
                   direct_grant        BOOLEAN,
                   rank                REAL
               ) AS $$
DECLARE
    v_tsquery TSQUERY;
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    v_tsquery := websearch_to_tsquery('simple', p_query);

    RETURN QUERY
        SELECT
            uo.object_id, uo.object_type,
            best.user_claim_id,
            uo.sa_name, uo.sa_description, uo.sa_link,
            uo.sa_access_level, uo.sa_owner_id, uo.sa_location_id,
            uo.direct_grant AND best.user_claim_id IS NULL AS direct_grant,
            ts_rank(uo.search_vector, v_tsquery) AS rank
        FROM claimius.user_object uo
                 LEFT JOIN LATERAL (
            SELECT uc.id AS user_claim_id
            FROM jsonb_array_elements(uo.grants) g
                     JOIN claimius.user_claim uc ON uc.claim_id = (g ->> 'claim_id')::UUID
                AND uc.user_id = p_user_id
                AND uc.app_id = p_app_id
                AND uc.sa_deleted_at IS NULL
                AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                AND (uc.ends_at IS NULL OR uc.ends_at > now())
                     JOIN claimius.claim c ON c.id = uc.claim_id AND c.sa_deleted_at IS NULL
            ORDER BY (g ->> 'level')::INTEGER ASC
            LIMIT 1
            ) best ON TRUE
        WHERE uo.user_id = p_user_id
          AND uo.app_id = p_app_id
          AND uo.sa_access_level <= p_min_level
          AND uo.search_vector @@ v_tsquery
          AND (p_object_types IS NULL OR uo.object_type = ANY(p_object_types))
          AND (best.user_claim_id IS NOT NULL OR uo.direct_grant)
        ORDER BY rank DESC
        LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.search_objects IS 'Full text search over accessible objects. user_claim_id is the actor token from the strongest surviving claim grant per object.';

-- ============================================================================
-- Write functions: claim management
-- ============================================================================

-- create_claim
CREATE OR REPLACE FUNCTION claimius.create_claim(
    p_app_id            UUID,
    p_name              TEXT,
    p_description       TEXT,
    p_sa_access_level   INTEGER,
    p_sa_owner_id       UUID,
    p_sa_root_id        UUID,
    p_sa_created_by     UUID,
    p_inherits          BOOLEAN DEFAULT TRUE,
    p_is_deny           BOOLEAN DEFAULT FALSE,
    p_type              claimius.claim_type DEFAULT 'user'
) RETURNS JSONB AS $$
DECLARE
    v_row claimius.claim;
BEGIN
    INSERT INTO claimius.claim (
        app_id, name, description, sa_access_level, inherits, is_deny, type,
        sa_owner_id, sa_root_id, sa_created_by
    ) VALUES (
                 p_app_id, p_name, p_description, p_sa_access_level, p_inherits, p_is_deny, p_type,
                 p_sa_owner_id, p_sa_root_id, p_sa_created_by
             ) RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.create_claim IS 'Creates a new claim.';

-- update_claim
CREATE OR REPLACE FUNCTION claimius.update_claim(
    p_claim_id          UUID,
    p_name              TEXT DEFAULT NULL,
    p_description       TEXT DEFAULT NULL,
    p_sa_access_level   INTEGER DEFAULT NULL,
    p_inherits          BOOLEAN DEFAULT NULL,
    p_is_deny           BOOLEAN DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row claimius.claim;
BEGIN
    UPDATE claimius.claim SET
                              name            = coalesce(p_name, name),
                              description     = coalesce(p_description, description),
                              sa_access_level = coalesce(p_sa_access_level, sa_access_level),
                              inherits        = coalesce(p_inherits, inherits),
                              is_deny         = coalesce(p_is_deny, is_deny)
    WHERE id = p_claim_id
    RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_claim IS 'Patches selected fields on a claim.';

-- remove_claim
CREATE OR REPLACE FUNCTION claimius.remove_claim(
    p_claim_id      UUID,
    p_deleted_by    UUID
) RETURNS BOOLEAN AS $$
BEGIN
    UPDATE claimius.claim SET sa_deleted_at = now() WHERE id = p_claim_id AND sa_deleted_at IS NULL;
    UPDATE claimius.user_claim SET sa_deleted_at = now() WHERE claim_id = p_claim_id AND sa_deleted_at IS NULL;
    UPDATE claimius.claim_object SET sa_deleted_at = now() WHERE claim_id = p_claim_id AND sa_deleted_at IS NULL;
    RETURN TRUE;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.remove_claim IS 'Soft deletes a claim and its user_claim and claim_object rows.';

-- assign_claim_user
CREATE OR REPLACE FUNCTION claimius.assign_claim_user(
    p_app_id        UUID,
    p_claim_id      UUID,
    p_user_id       UUID,
    p_sa_owner_id   UUID,
    p_sa_created_by UUID,
    p_reason        TEXT DEFAULT NULL,
    p_starts_at     TIMESTAMPTZ DEFAULT NULL,
    p_ends_at       TIMESTAMPTZ DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row claimius.user_claim;
BEGIN
    INSERT INTO claimius.user_claim (
        app_id, claim_id, user_id, reason, starts_at, ends_at,
        sa_owner_id, sa_created_by
    ) VALUES (
                 p_app_id, p_claim_id, p_user_id, p_reason, p_starts_at, p_ends_at,
                 p_sa_owner_id, p_sa_created_by
             ) RETURNING * INTO v_row;
    RETURN jsonb_build_object('user_claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.assign_claim_user IS 'Grants a claim to a user in an app.';

-- update_claim_user
CREATE OR REPLACE FUNCTION claimius.update_claim_user(
    p_user_claim_id UUID,
    p_reason        TEXT DEFAULT NULL,
    p_starts_at     TIMESTAMPTZ DEFAULT NULL,
    p_ends_at       TIMESTAMPTZ DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row claimius.user_claim;
BEGIN
    UPDATE claimius.user_claim SET
                                   reason = coalesce(p_reason, reason),
                                   starts_at = coalesce(p_starts_at, starts_at),
                                   ends_at = coalesce(p_ends_at, ends_at)
    WHERE id = p_user_claim_id
    RETURNING * INTO v_row;
    RETURN jsonb_build_object('user_claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_claim_user IS 'Patches selected fields on a user_claim.';

-- remove_user_claim
CREATE OR REPLACE FUNCTION claimius.remove_user_claim(
    p_user_claim_id UUID,
    p_deleted_by    UUID
) RETURNS BOOLEAN AS $$
BEGIN
    UPDATE claimius.user_claim SET sa_deleted_at = now()
    WHERE id = p_user_claim_id AND sa_deleted_at IS NULL;
    RETURN TRUE;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.remove_user_claim IS 'Soft deletes a user_claim row.';

-- assign_claim_object
CREATE OR REPLACE FUNCTION claimius.assign_claim_object(
    p_app_id        UUID,
    p_claim_id      UUID,
    p_object_id     UUID,
    p_object_type   TEXT,
    p_sa_owner_id   UUID,
    p_sa_root_id    UUID,
    p_sa_created_by UUID,
    p_level         INTEGER DEFAULT 0,
    p_inherits      BOOLEAN DEFAULT TRUE,
    p_reason        TEXT DEFAULT NULL,
    p_ref_id        UUID DEFAULT NULL,
    p_function      TEXT DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row claimius.claim_object;
BEGIN
    INSERT INTO claimius.claim_object (
        app_id, claim_id, object_id, object_type, reason, function,
        sa_level, inherits, ref_id, sa_owner_id, sa_root_id, sa_created_by
    ) VALUES (
                 p_app_id, p_claim_id, p_object_id, p_object_type, p_reason, p_function,
                 p_level, p_inherits, p_ref_id, p_sa_owner_id, p_sa_root_id, p_sa_created_by
             ) RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim_object', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.assign_claim_object IS 'Binds a claim to an object.';

-- update_claim_object
CREATE OR REPLACE FUNCTION claimius.update_claim_object(
    p_claim_object_id UUID,
    p_reason        TEXT DEFAULT NULL,
    p_inherits      BOOLEAN DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row claimius.claim_object;
BEGIN
    UPDATE claimius.claim_object SET
                                     reason = coalesce(p_reason, reason),
                                     inherits = coalesce(p_inherits, inherits)
    WHERE id = p_claim_object_id
    RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim_object', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_claim_object IS 'Patches selected fields on a claim_object.';

-- remove_claim_object
CREATE OR REPLACE FUNCTION claimius.remove_claim_object(
    p_claim_object_id UUID,
    p_deleted_by      UUID
) RETURNS BOOLEAN AS $$
BEGIN
    UPDATE claimius.claim_object SET sa_deleted_at = now()
    WHERE id = p_claim_object_id AND sa_deleted_at IS NULL;
    RETURN TRUE;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.remove_claim_object IS 'Soft deletes a claim_object row.';

-- ============================================================================
-- merge_user
-- Reassigns all data from a source user to a target user. Operates by
-- user_id (not row PK) and unifies across apps. Hard deletes the source
-- samna_user row(s) and migrates user_claim, user_relation, user_field,
-- user_object, object_users, user_users to the target. External tables
-- registered through init_claimius_tables can opt in by passing themselves
-- in p_external_tables; the function will UPDATE their sa_created_by
-- references where applicable.
-- ============================================================================

CREATE OR REPLACE FUNCTION claimius.merge_user(
    p_target_user_id    UUID,
    p_source_user_id    UUID,
    p_acting_user_claim UUID DEFAULT NULL
) RETURNS VOID AS $$
DECLARE
    v_app_row RECORD;
    v_target_app_user RECORD;
    v_source_app_user RECORD;
BEGIN
    -- Migrate per app
    FOR v_app_row IN
        SELECT DISTINCT app_id FROM claimius.samna_user
        WHERE user_id IN (p_target_user_id, p_source_user_id) AND sa_deleted_at IS NULL
        LOOP
            SELECT * INTO v_target_app_user FROM claimius.samna_user
            WHERE user_id = p_target_user_id AND app_id = v_app_row.app_id AND sa_deleted_at IS NULL;
            SELECT * INTO v_source_app_user FROM claimius.samna_user
            WHERE user_id = p_source_user_id AND app_id = v_app_row.app_id AND sa_deleted_at IS NULL;

            IF v_source_app_user.id IS NULL THEN
                CONTINUE;
            END IF;

            IF v_target_app_user.id IS NULL THEN
                -- Target has no row for this app: rename the source row to target user_id
                UPDATE claimius.samna_user
                SET user_id = p_target_user_id, sa_updated_at = now()
                WHERE id = v_source_app_user.id;
            ELSE
                -- Target already has a row for this app. Merge claims/relations/fields.
                UPDATE claimius.user_claim SET user_id = p_target_user_id
                WHERE user_id = p_source_user_id AND app_id = v_app_row.app_id;
                UPDATE claimius.user_relation SET user_id = p_target_user_id
                WHERE user_id = p_source_user_id AND app_id = v_app_row.app_id;
                UPDATE claimius.user_field SET user_id = p_target_user_id
                WHERE user_id = p_source_user_id AND app_id = v_app_row.app_id;

                -- Propagate the source's profile metadata onto the target row.
                -- Replace name, image, phone, and config unconditionally; fill
                -- email only when the target's email is NULL or empty so an
                -- explicitly set target address is never silently overwritten.
                UPDATE claimius.samna_user
                SET first_name    = v_source_app_user.first_name,
                    last_name     = v_source_app_user.last_name,
                    user_name     = v_source_app_user.user_name,
                    user_image    = v_source_app_user.user_image,
                    phone         = v_source_app_user.phone,
                    config        = v_source_app_user.config,
                    email         = CASE
                                        WHEN coalesce(NULLIF(email, ''), NULL) IS NULL
                                            THEN v_source_app_user.email
                                        ELSE email
                        END,
                    sa_updated_at = now()
                WHERE id = v_target_app_user.id;

                -- Hard delete materialized rows for the source; calc functions will
                -- repopulate as transactions touch the target.
                DELETE FROM claimius.user_object
                WHERE user_id = p_source_user_id AND app_id = v_app_row.app_id;
                DELETE FROM claimius.object_users
                WHERE user_id = p_source_user_id AND app_id = v_app_row.app_id;
                DELETE FROM claimius.user_users
                WHERE app_id = v_app_row.app_id
                  AND (viewer_id = p_source_user_id OR target_user_id = p_source_user_id);

                -- Hard delete the source samna_user row
                DELETE FROM claimius.samna_user WHERE id = v_source_app_user.id;
            END IF;

            -- Force recompute of materialized state for the target by touching
            -- every (app, target, object) currently in object_users
            PERFORM claimius._refresh_user_users(v_app_row.app_id, p_target_user_id);

            PERFORM claimius.write_audit(
                    v_app_row.app_id,
                    'merge_user', 'merge', 'claimius.samna_user',
                    p_target_user_id,
                    v_target_app_user.app_id,
                    coalesce(p_acting_user_claim, p_target_user_id),
                    'Merged source ' || p_source_user_id::TEXT || ' into target ' || p_target_user_id::TEXT
                    );
        END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.merge_user IS 'Reassigns all data from source user_id to target user_id across apps.';

-- ============================================================================
-- migrate_root: cross root organization migration. Should be called rarely
-- since calc_hierarchical_access blocks cross root reparenting in the trigger.
-- ============================================================================

CREATE OR REPLACE FUNCTION claimius.migrate_root(
    p_object_type   TEXT,
    p_object_id     UUID,
    p_new_root_id   UUID
) RETURNS VOID AS $$
DECLARE
    v_old_root UUID;
    v_schema TEXT;
    v_table TEXT;
    v_parts TEXT[];
    v_sql TEXT;
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    PERFORM set_config('claimius.replay_mode', 'true', TRUE);

    v_sql := format('SELECT sa_root_id FROM %I.%I WHERE id = $1', v_schema, v_table);
    EXECUTE v_sql INTO v_old_root USING p_object_id;

    -- Walk every descendant in the old tree and rewrite sa_root_id
    EXECUTE format('
        WITH RECURSIVE descendants AS (
            SELECT id FROM %I.%I WHERE id = $1
            UNION ALL
            SELECT t.id FROM %I.%I t JOIN descendants d ON t.sa_parent_id = d.id
            WHERE t.id <> t.sa_parent_id
        )
        UPDATE %I.%I SET sa_root_id = $2 WHERE id IN (SELECT id FROM descendants)
    ', v_schema, v_table, v_schema, v_table, v_schema, v_table)
        USING p_object_id, p_new_root_id;

    PERFORM set_config('claimius.replay_mode', 'false', TRUE);

    -- Rebuild both old and new trees
    IF p_object_type = 'claimius.organization' THEN
        PERFORM claimius.build_ownership_tree(v_old_root);
        PERFORM claimius.build_ownership_tree(p_new_root_id);
    ELSIF p_object_type = 'claimius.location' THEN
        PERFORM claimius.build_location_tree(v_old_root);
        PERFORM claimius.build_location_tree(p_new_root_id);
    ELSE
        PERFORM claimius.build_parenthood_tree(p_object_type, v_old_root);
        PERFORM claimius.build_parenthood_tree(p_object_type, p_new_root_id);
    END IF;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.migrate_root IS 'Admin function: cross root migration. Use only when absolutely required.';

-- ============================================================================
-- Init functions
-- ============================================================================

-- init_claimius_tables
-- Registers external app tables. Called by the implementing app's migration.
-- Idempotent: rebuilds registration state from the input list each call.
-- For tables that already have data, seeds materialized state in bulk
-- under replay_mode (triggers do not fire, no events emitted).
CREATE OR REPLACE FUNCTION claimius.init_claimius_tables(
    VARIADIC p_object_types TEXT[]
) RETURNS VOID AS $$
DECLARE
    v_object_type   TEXT;
    v_old_object_type TEXT;
    v_existing      TEXT[];
    v_new           TEXT[];
    v_to_remove     TEXT[];
BEGIN
    -- Snapshot currently registered external tables
    SELECT array_agg(object_type) INTO v_existing
    FROM claimius.table_info WHERE NOT is_internal;

    v_new := p_object_types;

    -- Tables that were registered before but not in the new list: detach
    IF v_existing IS NOT NULL THEN
        SELECT array_agg(o) INTO v_to_remove
        FROM unnest(v_existing) o
        WHERE NOT (o = ANY(v_new));
    END IF;

    IF v_to_remove IS NOT NULL THEN
        FOREACH v_old_object_type IN ARRAY v_to_remove LOOP
                PERFORM claimius._detach_calc_trigger(v_old_object_type);
                DELETE FROM claimius.table_info WHERE object_type = v_old_object_type;
                DELETE FROM claimius.inheritance_info WHERE object_type = v_old_object_type;
                DELETE FROM claimius.user_object WHERE object_type = v_old_object_type;
                DELETE FROM claimius.object_users WHERE object_type = v_old_object_type;
                DELETE FROM claimius.reconcile_queue WHERE object_type = v_old_object_type;
                RAISE NOTICE 'Deregistered table %', v_old_object_type;
            END LOOP;
    END IF;

    -- Register each new (or existing) table
    FOREACH v_object_type IN ARRAY v_new LOOP
            -- normalize to schema.table
            IF position('.' IN v_object_type) = 0 THEN
                v_object_type := 'public.' || v_object_type;
            END IF;
            PERFORM claimius._register_table_info(v_object_type, FALSE);
            PERFORM claimius._attach_calc_trigger(v_object_type);
            RAISE NOTICE 'Registered table %', v_object_type;
        END LOOP;

    -- Bulk seed materialized state for any registered table with existing data
    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
    BEGIN
        FOREACH v_object_type IN ARRAY v_new LOOP
                IF position('.' IN v_object_type) = 0 THEN
                    v_object_type := 'public.' || v_object_type;
                END IF;
                PERFORM claimius._bulk_seed_user_object(v_object_type);
                RAISE NOTICE 'Seeded user_object for %', v_object_type;
            END LOOP;
    EXCEPTION WHEN OTHERS THEN
        PERFORM set_config('claimius.replay_mode', 'false', TRUE);
        RAISE;
    END;
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.init_claimius_tables IS 'Registers external tables. Idempotent. Called by implementer migrations.';

-- _bulk_seed_user_object
-- For a registered table with existing rows, populates user_object,
-- object_users, and user_users for every (user, object) pair where access
-- exists. Run while replay_mode is true.
CREATE OR REPLACE FUNCTION claimius._bulk_seed_user_object(p_object_type TEXT)
    RETURNS VOID AS $$
DECLARE
    v_schema TEXT;
    v_table TEXT;
    v_parts TEXT[];
    v_row_count INTEGER := 0;
    v_user RECORD;
    v_obj RECORD;
    v_sql TEXT;
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    -- For every live row in the table, find every potentially affected user
    -- and recompute. Triggers are short circuited by replay_mode.
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);
    v_sql := format('SELECT id, app_id FROM %I.%I WHERE sa_deleted_at IS NULL', v_schema, v_table);
    FOR v_obj IN EXECUTE v_sql LOOP
            FOR v_user IN
                SELECT user_id FROM claimius._affected_users_for_object(v_obj.app_id, p_object_type, v_obj.id)
                LOOP
                    PERFORM claimius.recompute_user_object(v_obj.app_id, v_user.user_id, p_object_type, v_obj.id);
                END LOOP;
            v_row_count := v_row_count + 1;
            IF v_row_count % 100 = 0 THEN
                RAISE NOTICE '  ... seeded % rows of %', v_row_count, p_object_type;
            END IF;
        END LOOP;
    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._bulk_seed_user_object IS 'Seeds user_object for an existing table at registration time.';

-- ----------------------------------------------------------------------------
-- init_claimius_internal
-- Registers all claimius.* tables in table_info. Called once at the end of
-- claimius schema bootstrap (VX.0__init.sql). Not callable by implementers.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.init_claimius_internal()
    RETURNS VOID AS $$
DECLARE
    v_internal_tables TEXT[] := ARRAY[
        'claimius.samna_app',
        'claimius.samna_user',
        'claimius.samna_client',
        'claimius.samna_secret',
        'claimius.organization',
        'claimius.location',
        'claimius.claim',
        'claimius.user_claim',
        'claimius.claim_object',
        'claimius.audit',
        'claimius.object_field',
        'claimius.user_field',
        'claimius.user_relation'
        ];
    v_t TEXT;
BEGIN
    FOREACH v_t IN ARRAY v_internal_tables LOOP
            PERFORM claimius._register_table_info(v_t, TRUE);
        END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.init_claimius_internal IS 'Registers internal claimius.* tables in table_info. Called once at schema bootstrap.';

-- ----------------------------------------------------------------------------
-- get_prophet_app
-- Returns the system app row for this deployment. The system app is the
-- one created by init_prophet, identified by its slug stored on
-- prophet_state.system_app_slug. Works on prophet and hybrid deployments.
-- Raises on a pure disciple, where no system app exists.
-- Granted to all roles since this is identity lookup, not user gated.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_prophet_app()
    RETURNS SETOF claimius.samna_app AS $$
DECLARE
    v_slug TEXT;
BEGIN
    SELECT system_app_slug INTO v_slug
    FROM claimius.prophet_state
    WHERE system_app_slug IS NOT NULL
    LIMIT 1;

    IF v_slug IS NULL THEN
        RAISE EXCEPTION 'get_prophet_app: no system app registered. This deployment is a pure disciple, or init_prophet has not run.';
    END IF;

    RETURN QUERY
        SELECT * FROM claimius.samna_app
        WHERE slug = v_slug AND sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_prophet_app IS 'Returns the system app row for this deployment (prophet or hybrid). Raises on pure disciple.';

-- ----------------------------------------------------------------------------
-- get_disciple_app
-- Returns the upstream prophet app row this disciple mirrors, identified
-- by the slug stored on disciple_state.disciple_app_slug. Works on
-- disciple and hybrid deployments. Raises on a pure prophet, where
-- disciple_state has no row.
-- Granted to all roles since this is identity lookup, not user gated.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_disciple_app()
    RETURNS SETOF claimius.samna_app AS $$
DECLARE
    v_slug TEXT;
BEGIN
    SELECT disciple_app_slug INTO v_slug
    FROM claimius.disciple_state
    LIMIT 1;

    IF v_slug IS NULL THEN
        RAISE EXCEPTION 'get_disciple_app: no disciple app registered. This deployment is a pure prophet, or init_disciple has not run.';
    END IF;

    RETURN QUERY
        SELECT * FROM claimius.samna_app
        WHERE slug = v_slug AND sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_disciple_app IS 'Returns the upstream prophet app this disciple mirrors. Raises on pure prophet.';

-- ----------------------------------------------------------------------------
-- init_prophet
-- Bootstrap a fresh claimius deployment as a prophet. Creates the system
-- app, system organization (self rooted), system user, system claim, and
-- wires up the bootstrap user_claim and claim_object. Idempotent.
--
-- Per app cryptographic material is generated externally in Go and passed
-- in. Claimius does not generate keys.
--
-- App and org reference each other via sa_owner_id (app->org) and app_id
-- (org->app). We resolve the chicken-and-egg by inserting both with
-- generated placeholder uuids and then reconciling with UPDATEs.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.init_prophet(
    p_system_app_slug       TEXT,
    p_system_app_name       TEXT,
    p_system_app_private_key  TEXT,
    p_system_app_private_seed TEXT,
    p_system_user_id        UUID DEFAULT gen_random_uuid(),
    p_system_app_redirect_uri  TEXT DEFAULT '',
    p_system_app_sync_uri      TEXT DEFAULT '',
    p_system_app_contact_email TEXT DEFAULT NULL,
    p_system_app_image      TEXT DEFAULT NULL,
    p_system_app_provider_ids UUID[] DEFAULT ARRAY[]::UUID[],
    p_system_app_style      JSONB DEFAULT NULL,
    p_system_app_description TEXT DEFAULT 'System app',
    p_system_org_name       TEXT DEFAULT 'System',
    p_system_claim_name     TEXT DEFAULT 'System Administrator',
    p_system_claim_id       UUID DEFAULT gen_random_uuid(),
    p_default_claim         JSONB DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_app_id            UUID;
    v_org_id            UUID;
    v_claim_id          UUID;
    v_default_claim_id  UUID;
    v_user_claim_id     UUID;
    v_pre_app_id        UUID;
    v_pre_org_id        UUID;
    v_default_name      TEXT;
    v_default_desc      TEXT;
    v_default_level     INTEGER;
BEGIN
    -- Validate the required default claim metadata. The system claim is
    -- created with hardcoded properties; the default claim (granted to every
    -- regular user on first login via ensure_app_user) is implementer defined.
    IF p_default_claim IS NULL THEN
        RAISE EXCEPTION 'init_prophet: p_default_claim is required and must contain name and description.';
    END IF;
    v_default_name  := p_default_claim ->> 'name';
    v_default_desc  := p_default_claim ->> 'description';
    v_default_level := coalesce((p_default_claim ->> 'sa_access_level')::INTEGER, 8);
    IF v_default_name IS NULL OR v_default_name = '' THEN
        RAISE EXCEPTION 'init_prophet: p_default_claim.name is required.';
    END IF;
    IF v_default_desc IS NULL OR v_default_desc = '' THEN
        RAISE EXCEPTION 'init_prophet: p_default_claim.description is required.';
    END IF;

    -- Pre-generate the ids so app and org can reference each other in their
    -- INSERTs even though they create circular references.
    v_pre_app_id := gen_random_uuid();
    v_pre_org_id := gen_random_uuid();

    -- 1. system app, with sa_owner_id pointing at the future system org.
    -- app_secret is intentionally NOT set here. It's the API key that
    -- disciples (or other consumers) authenticate with against this prophet's
    -- sync endpoints, generated externally by the implementing service and
    -- written to samna_app.app_secret on demand. secret_version uses its
    -- schema default until the first secret is issued.
    INSERT INTO claimius.samna_app (
        id, slug, name, description, contact_email, app_image,
        redirect_uri, sync_uri, provider_ids, status,
        private_key, private_seed,
        style, sa_owner_id, sa_created_by
    ) VALUES (
                 v_pre_app_id, p_system_app_slug, p_system_app_name,
                 p_system_app_description, p_system_app_contact_email, p_system_app_image,
                 p_system_app_redirect_uri, p_system_app_sync_uri, p_system_app_provider_ids,
                 'active'::claimius.app_status,
                 p_system_app_private_key, p_system_app_private_seed,
                 p_system_app_style, v_pre_org_id, p_system_user_id
             )
    ON CONFLICT (slug) DO UPDATE SET
                                     name = EXCLUDED.name,
                                     description = EXCLUDED.description,
                                     contact_email = EXCLUDED.contact_email,
                                     sa_updated_at = now()
    RETURNING id INTO v_app_id;

    -- 2. system samna_user
    -- Partial unique index (user_id, app_id) WHERE sa_deleted_at IS NULL means
    -- ON CONFLICT can't infer an arbiter cleanly, so SELECT-then-INSERT.
    IF NOT EXISTS (
        SELECT 1 FROM claimius.samna_user
        WHERE user_id = p_system_user_id AND app_id = v_app_id AND sa_deleted_at IS NULL
    ) THEN
        INSERT INTO claimius.samna_user (user_id, app_id, status, type, first_name, last_name)
        VALUES (p_system_user_id, v_app_id, 'active', 'system', 'System', 'User');
    END IF;

    -- 3. system organization, self rooted, owned by the system app
    SELECT id INTO v_org_id FROM claimius.organization
    WHERE app_id = v_app_id AND type = 'system' AND sa_deleted_at IS NULL
    LIMIT 1;

    IF v_org_id IS NULL THEN
        INSERT INTO claimius.organization (
            id, app_id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by
        ) VALUES (
                     v_pre_org_id, v_app_id, p_system_org_name, 'system',
                     v_pre_org_id, v_pre_org_id, 0, p_system_user_id
                 )
        RETURNING id INTO v_org_id;
    END IF;

    -- 4. reconcile: app's sa_owner_id should match the actual system org id
    UPDATE claimius.samna_app SET sa_owner_id = v_org_id WHERE id = v_app_id AND sa_owner_id <> v_org_id;

    -- 5. ensure inheritance_info row for system org (full unique index, ON CONFLICT works)
    INSERT INTO claimius.inheritance_info (object_type, root_id, type, tree)
    VALUES ('claimius.organization', v_org_id, 'ownership', '{}'::JSONB)
    ON CONFLICT (object_type, root_id, type) DO NOTHING;

    -- 6. system claim (sidelined: granted only to the system user)
    SELECT id INTO v_claim_id FROM claimius.claim
    WHERE app_id = v_app_id AND name = p_system_claim_name AND sa_deleted_at IS NULL;

    IF v_claim_id IS NULL THEN
        INSERT INTO claimius.claim (
            id, app_id, name, description, sa_access_level, inherits, type,
            sa_owner_id, sa_root_id, sa_created_by
        ) VALUES (
                     p_system_claim_id, v_app_id, p_system_claim_name,
                     'Full access to claimius', 0, TRUE, 'system',
                     v_org_id, v_org_id, p_system_user_id
                 )
        RETURNING id INTO v_claim_id;
    END IF;

    -- 6b. default claim (granted to every regular user on first login via
    -- ensure_app_user). No claim_object binding, no inheritance: it is a
    -- "you have an account" marker by default. Implementer defines name,
    -- description, and level via p_default_claim.
    SELECT id INTO v_default_claim_id FROM claimius.claim
    WHERE app_id = v_app_id AND name = v_default_name AND sa_deleted_at IS NULL;

    IF v_default_claim_id IS NULL THEN
        INSERT INTO claimius.claim (
            app_id, name, description, sa_access_level, inherits, is_deny, type,
            sa_owner_id, sa_root_id, sa_created_by
        ) VALUES (
                     v_app_id, v_default_name, v_default_desc, v_default_level,
                     FALSE, FALSE, 'user',
                     v_org_id, v_org_id, p_system_user_id
                 )
        RETURNING id INTO v_default_claim_id;
    END IF;

    -- 7. attach default claim_id to the system app. This is the claim every
    -- regular user receives via ensure_app_user. The system claim stays
    -- sidelined and is granted only to the system user below.
    UPDATE claimius.samna_app SET claim_id = v_default_claim_id
    WHERE id = v_app_id AND (claim_id IS NULL OR claim_id <> v_default_claim_id);

    -- 8. bind claim to system org
    -- claim_object has no unique constraint, so SELECT-then-INSERT.
    IF NOT EXISTS (
        SELECT 1 FROM claimius.claim_object
        WHERE app_id = v_app_id
          AND claim_id = v_claim_id
          AND object_id = v_org_id
          AND object_type = 'claimius.organization'
          AND sa_deleted_at IS NULL
    ) THEN
        INSERT INTO claimius.claim_object (
            app_id, claim_id, object_id, object_type,
            sa_owner_id, sa_root_id, sa_created_by, inherits
        ) VALUES (
                     v_app_id, v_claim_id, v_org_id, 'claimius.organization',
                     v_org_id, v_org_id, p_system_user_id, TRUE
                 );
    END IF;

    -- 9. grant claim to system user
    -- Partial unique index on (claim_id, user_id, app_id) WHERE sa_deleted_at IS NULL.
    -- SELECT-then-INSERT, capturing the id either way for the return jsonb.
    SELECT id INTO v_user_claim_id FROM claimius.user_claim
    WHERE app_id = v_app_id
      AND claim_id = v_claim_id
      AND user_id = p_system_user_id
      AND sa_deleted_at IS NULL
    LIMIT 1;

    IF v_user_claim_id IS NULL THEN
        INSERT INTO claimius.user_claim (
            app_id, claim_id, user_id, sa_owner_id, sa_created_by
        ) VALUES (
                     v_app_id, v_claim_id, p_system_user_id, v_org_id, p_system_user_id
                 )
        RETURNING id INTO v_user_claim_id;
    END IF;

    -- 10. prophet_state row for this app, marked as the system app via
    -- system_app_slug. Partial unique index enforces at most one system row.
    INSERT INTO claimius.prophet_state (app_id, last_applied_seq, system_app_slug)
    VALUES (v_app_id, 0, p_system_app_slug)
    ON CONFLICT (app_id) DO UPDATE SET system_app_slug = EXCLUDED.system_app_slug;

    -- 11. role grants for prophet runtime
    PERFORM claimius._grant_prophet_roles();

    -- Build the result: 6 keys, each named after the table the row lives in.
    -- prophet_state and inheritance_info are internal bookkeeping and are
    -- not returned. Re-running init_prophet is idempotent and returns the
    -- current state of these rows.
    DECLARE
        v_result JSONB;
    BEGIN
        SELECT jsonb_build_object(
                       'samna_app',     to_jsonb(a.*),
                       'organization',  to_jsonb(o.*),
                       'samna_user',    to_jsonb(u.*),
                       'claim',         to_jsonb(c.*),
                       'default_claim', to_jsonb(dc.*),
                       'user_claim',    to_jsonb(uc.*),
                       'claim_object',  to_jsonb(co.*)
               )
        INTO v_result
        FROM claimius.samna_app a
                 LEFT JOIN claimius.organization o ON o.id = v_org_id
                 LEFT JOIN claimius.samna_user u ON u.user_id = p_system_user_id AND u.app_id = v_app_id
                 LEFT JOIN claimius.claim c ON c.id = v_claim_id
                 LEFT JOIN claimius.claim dc ON dc.id = v_default_claim_id
                 LEFT JOIN claimius.user_claim uc ON uc.id = v_user_claim_id
                 LEFT JOIN claimius.claim_object co ON co.app_id = v_app_id AND co.claim_id = v_claim_id
            AND co.object_id = v_org_id AND co.object_type = 'claimius.organization'
            AND co.sa_deleted_at IS NULL
        WHERE a.id = v_app_id;

        RAISE NOTICE 'Prophet initialized: app=% org=% claim=% user=%',
            v_app_id, v_org_id, v_claim_id, p_system_user_id;

        RETURN v_result;
    END;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.init_prophet IS 'Bootstrap function. Sets up system app, org, user, and claim. Idempotent.';

-- ----------------------------------------------------------------------------
-- init_disciple
-- Bootstrap a fresh claimius deployment as a disciple. Records that this
-- instance is a disciple, sets up disciple_state with the slug of the
-- upstream prophet app this disciple mirrors, and grants the
-- disciple_client role. Pass p_disciple_app_slug from an environment
-- variable (e.g. CLAIMIUS_DISCIPLE_APP_SLUG) so the same migration
-- artefact can be deployed against any prophet. The actual data arrives
-- via sync from the prophet.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.init_disciple(
    p_disciple_app_slug TEXT
)
    RETURNS JSONB AS $$
BEGIN
    IF p_disciple_app_slug IS NULL OR p_disciple_app_slug = '' THEN
        RAISE EXCEPTION 'init_disciple: p_disciple_app_slug is required.';
    END IF;

    -- Singleton row in disciple_state. SELECT-then-INSERT, then UPDATE the
    -- slug so re-running init_disciple with a different slug is allowed
    -- (idempotent for the same value, corrective for a wrong one).
    IF NOT EXISTS (SELECT 1 FROM claimius.disciple_state) THEN
        INSERT INTO claimius.disciple_state (last_applied_seq, disciple_app_slug)
        VALUES (0, p_disciple_app_slug);
    ELSE
        UPDATE claimius.disciple_state
        SET disciple_app_slug = p_disciple_app_slug,
            sa_updated_at = now()
        WHERE disciple_app_slug <> p_disciple_app_slug;
    END IF;

    -- Roles: disciple gets reader plus disciple_client
    PERFORM claimius._grant_disciple_roles();

    RAISE NOTICE 'Disciple initialized for app slug %. Sync layer must populate samna_app and other tables from prophet.', p_disciple_app_slug;

    -- Disciples have nothing user meaningful to return: state arrives via
    -- sync. We return an empty jsonb so the function has the same return
    -- type as init_prophet and init_hybrid.
    RETURN '{}'::JSONB;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.init_disciple IS 'Bootstrap as a disciple. Requires the upstream prophet app slug. State arrives from prophet via sync.';

-- ----------------------------------------------------------------------------
-- init_hybrid
-- Hybrid: instance acts as a disciple to an upstream prophet AND as a
-- prophet for downstream disciples. Combines init_prophet and init_disciple
-- behavior plus role grants for both.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.init_hybrid(
    p_system_app_slug          TEXT,
    p_system_app_name          TEXT,
    p_system_app_private_key   TEXT,
    p_system_app_private_seed  TEXT,
    p_disciple_app_slug        TEXT,
    p_system_user_id           UUID DEFAULT gen_random_uuid(),
    p_system_app_redirect_uri  TEXT DEFAULT '',
    p_system_app_sync_uri      TEXT DEFAULT '',
    p_system_app_contact_email TEXT DEFAULT NULL,
    p_system_app_image         TEXT DEFAULT NULL,
    p_system_app_provider_ids  UUID[] DEFAULT ARRAY[]::UUID[],
    p_system_app_style         JSONB DEFAULT NULL,
    p_system_app_description   TEXT DEFAULT 'System app',
    p_system_org_name          TEXT DEFAULT 'System',
    p_system_claim_name        TEXT DEFAULT 'System Administrator',
    p_system_claim_id          UUID DEFAULT gen_random_uuid(),
    p_default_claim            JSONB DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_result JSONB;
BEGIN
    v_result := claimius.init_prophet(
            p_system_app_slug, p_system_app_name,
            p_system_app_private_key, p_system_app_private_seed,
            p_system_user_id,
            p_system_app_redirect_uri, p_system_app_sync_uri, p_system_app_contact_email,
            p_system_app_image, p_system_app_provider_ids, p_system_app_style,
            p_system_app_description, p_system_org_name,
            p_system_claim_name, p_system_claim_id,
            p_default_claim
                );
    PERFORM claimius.init_disciple(p_disciple_app_slug);
    PERFORM claimius._grant_hybrid_roles();
    RAISE NOTICE 'Hybrid initialized.';
    RETURN v_result;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.init_hybrid IS 'Bootstrap as a hybrid (prophet + disciple).';

-- ----------------------------------------------------------------------------
-- Role grant helpers
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius._grant_prophet_roles()
    RETURNS VOID AS $$
BEGIN
    -- Reader: EXECUTE on all functions, then revoke the write functions and
    -- the init/admin ones. Reader sees no tables directly.
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_reader';
    EXECUTE 'REVOKE EXECUTE ON FUNCTION claimius.create_claim, claimius.update_claim, claimius.remove_claim,
             claimius.assign_claim_user, claimius.update_claim_user, claimius.remove_user_claim,
             claimius.assign_claim_object, claimius.update_claim_object, claimius.remove_claim_object,
             claimius.merge_user, claimius.migrate_root,
             claimius.init_prophet, claimius.init_disciple, claimius.init_hybrid,
             claimius.init_claimius_tables, claimius.init_claimius_internal
             FROM claimius_reader';

    -- Writer: EXECUTE on everything, plus INSERT/UPDATE on tables that do
    -- not have dedicated write functions. The prophet's app code is allowed
    -- to write directly to these tables. Tables WITH write functions
    -- (claim, user_claim, claim_object) get only EXECUTE on the function;
    -- direct writes to those tables are not granted.
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_writer';
    EXECUTE 'GRANT INSERT, UPDATE ON
             claimius.samna_app, claimius.samna_user, claimius.samna_client, claimius.samna_secret,
             claimius.organization, claimius.location,
             claimius.object_field, claimius.user_field, claimius.user_relation,
             claimius.prophet_state
             TO claimius_writer';
    EXECUTE 'GRANT SELECT ON ALL TABLES IN SCHEMA claimius TO claimius_writer';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION claimius._grant_disciple_roles()
    RETURNS VOID AS $$
BEGIN
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_reader';
    EXECUTE 'REVOKE EXECUTE ON FUNCTION claimius.create_claim, claimius.update_claim, claimius.remove_claim,
             claimius.assign_claim_user, claimius.update_claim_user, claimius.remove_user_claim,
             claimius.assign_claim_object, claimius.update_claim_object, claimius.remove_claim_object,
             claimius.merge_user, claimius.migrate_root,
             claimius.init_prophet, claimius.init_disciple, claimius.init_hybrid,
             claimius.init_claimius_tables, claimius.init_claimius_internal
             FROM claimius_reader';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.user_object, claimius.object_users, claimius.user_users,
             claimius.disciple_state, claimius.samna_user, claimius.samna_client,
             claimius.samna_secret, claimius.samna_app, claimius.organization, claimius.location,
             claimius.claim, claimius.user_claim, claimius.claim_object,
             claimius.object_field, claimius.user_field, claimius.user_relation,
             claimius.audit, claimius.inheritance_info, claimius.table_info
             TO claimius_disciple_client';
    EXECUTE 'GRANT SELECT ON ALL TABLES IN SCHEMA claimius TO claimius_disciple_client';
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_disciple_client';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION claimius._grant_hybrid_roles()
    RETURNS VOID AS $$
BEGIN
    PERFORM claimius._grant_prophet_roles();
    PERFORM claimius._grant_disciple_roles();
END;
$$ LANGUAGE plpgsql;

-- ----------------------------------------------------------------------------
-- ensure_app_user
-- Helper for prophet apps. When a user logs into an app for the first time,
-- creates the per app samna_user row keyed by (user_id, app_id). If the app
-- has a default claim configured (samna_app.claim_id), grants it to the user
-- as part of the same call. Idempotent: re-running returns the existing row
-- and existing user_claim.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.ensure_app_user(
    p_user_id    UUID,
    p_app_id     UUID,
    p_first_name TEXT DEFAULT NULL,
    p_last_name  TEXT DEFAULT NULL,
    p_user_name  TEXT DEFAULT NULL,
    p_user_image TEXT DEFAULT NULL,
    p_email      TEXT DEFAULT NULL,
    p_phone      TEXT DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_user_row    claimius.samna_user;
    v_uc_row      claimius.user_claim;
    v_default_claim_id UUID;
    v_default_claim_owner UUID;
BEGIN
    -- Find or insert the samna_user row
    SELECT * INTO v_user_row FROM claimius.samna_user
    WHERE user_id = p_user_id AND app_id = p_app_id AND sa_deleted_at IS NULL;

    IF NOT FOUND THEN
        INSERT INTO claimius.samna_user (
            user_id, app_id, first_name, last_name, user_name, user_image,
            email, phone, status, type
        )
        VALUES (
                   p_user_id, p_app_id, p_first_name, p_last_name, p_user_name, p_user_image,
                   p_email, p_phone, 'active', 'user'
               )
        RETURNING * INTO v_user_row;
    END IF;

    -- Look up the app's default claim
    SELECT a.claim_id, c.sa_owner_id
    INTO v_default_claim_id, v_default_claim_owner
    FROM claimius.samna_app a
             LEFT JOIN claimius.claim c ON c.id = a.claim_id AND c.sa_deleted_at IS NULL
    WHERE a.id = p_app_id;

    -- Grant the default claim if one is configured. Idempotent: if the user
    -- already has a live user_claim for this claim, return the existing one.
    IF v_default_claim_id IS NOT NULL THEN
        SELECT * INTO v_uc_row FROM claimius.user_claim
        WHERE user_id = p_user_id
          AND app_id = p_app_id
          AND claim_id = v_default_claim_id
          AND sa_deleted_at IS NULL
        LIMIT 1;

        IF NOT FOUND THEN
            INSERT INTO claimius.user_claim (
                app_id, claim_id, user_id, sa_owner_id, sa_created_by
            ) VALUES (
                         p_app_id, v_default_claim_id, p_user_id,
                         v_default_claim_owner,
                         p_user_id  -- bootstrap exception: actor is the user themselves on first login
                     )
            RETURNING * INTO v_uc_row;
        END IF;

        RETURN jsonb_build_object(
                'samna_user', to_jsonb(v_user_row),
                'user_claim', to_jsonb(v_uc_row)
               );
    END IF;

    -- App has no default claim configured: just return the samna_user row.
    RETURN jsonb_build_object('samna_user', to_jsonb(v_user_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.ensure_app_user IS 'Creates per app samna_user row and grants the app default claim. Idempotent.';