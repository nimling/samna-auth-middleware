-- ============================================================================
-- Claimius V2.4 Disciple External Functions
-- ----------------------------------------------------------------------------
-- The public read surface on a disciple. A disciple has no app_id: it mirrors
-- exactly one app, its tables carry no app_id column, and the materialized
-- user_object index arrives by sync already scoped. So every read here drops
-- the app_id parameter and every app_id filter.
--
-- Disciple reads are pure. They do not call reconcile_if_pending or
-- check_user_active (both of which write on the prophet). The disciple is a
-- read replica: access is authoritative through the synced user_object, and
-- deactivations arrive as user_object updates from the prophet.
-- ============================================================================

-- get_access (object form)
DROP FUNCTION IF EXISTS claimius.get_access(UUID, UUID, TEXT, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_access(
    p_user_id          UUID,
    p_object_id        UUID,
    p_object_type      TEXT,
    p_required_access  INTEGER DEFAULT 0
) RETURNS TABLE (
                    user_object_id      UUID,
                    user_claim_id       UUID,
                    claim_id            UUID,
                    sa_access           INTEGER,
                    scope               JSONB,
                    sa_owner_id         UUID,
                    sa_root_id          UUID,
                    sa_location_id      UUID,
                    direct_grant        BOOLEAN
                ) AS $$
BEGIN
    RETURN QUERY
        WITH uo AS (
            SELECT u.*
            FROM claimius.user_object u
            WHERE u.user_id = p_user_id
              AND u.object_id = p_object_id
              AND u.object_type = p_object_type
              AND (u.sa_access & p_required_access) = p_required_access
        ),
             surviving AS (
                 SELECT
                     uo.id AS user_object_id,
                     uc.id AS user_claim_id,
                     (g ->> 'claim_id')::UUID AS claim_id,
                     (g ->> 'access')::INTEGER AS access_bits,
                     g -> 'scope' AS grant_scope,
                     uo.sa_owner_id,
                     uo.sa_root_id,
                     uo.sa_location_id,
                     FALSE AS direct_grant
                 FROM uo
                          CROSS JOIN LATERAL jsonb_array_elements(uo.grants) g
                          JOIN claimius.user_claim uc ON uc.claim_id = (g ->> 'claim_id')::UUID
                     AND uc.user_id = p_user_id
                     AND uc.sa_deleted_at IS NULL
                     AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                     AND (uc.ends_at IS NULL OR uc.ends_at > now())
                          JOIN claimius.claim c ON c.id = uc.claim_id
                     AND c.sa_deleted_at IS NULL
                 WHERE ((g ->> 'access')::INTEGER & p_required_access) = p_required_access
             )
        SELECT s.user_object_id, s.user_claim_id, s.claim_id, s.access_bits,
               s.grant_scope, s.sa_owner_id, s.sa_root_id, s.sa_location_id, s.direct_grant
        FROM surviving s
        ORDER BY claimius._popcount(s.access_bits) DESC, s.access_bits DESC
        LIMIT 1;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_access(UUID, UUID, TEXT, INTEGER) IS 'Strongest active claim based grant for one (user, object) whose mask covers p_required_access. user_claim_id is the actor token for follow-on writes.';

-- get_direct_access
DROP FUNCTION IF EXISTS claimius.get_direct_access(UUID, UUID, TEXT);

CREATE OR REPLACE FUNCTION claimius.get_direct_access(
    p_user_id       UUID,
    p_object_id     UUID,
    p_object_type   TEXT
) RETURNS TABLE (
                    user_object_id      UUID,
                    user_claim_id       UUID,
                    claim_id            UUID,
                    sa_access           INTEGER,
                    scope               JSONB,
                    sa_owner_id         UUID,
                    sa_root_id          UUID,
                    sa_location_id      UUID,
                    direct_grant        BOOLEAN
                ) AS $$
BEGIN
    IF p_object_type = 'claimius.samna_user' AND p_object_id = p_user_id THEN
        RETURN QUERY
            SELECT
                claimius.composite_id(p_user_id::TEXT, p_object_type, p_object_id::TEXT),
                uc.id,
                uc.claim_id,
                15,
                NULL::JSONB,
                uc.sa_owner_id,
                c.sa_root_id,
                NULL::UUID,
                TRUE
            FROM claimius.user_claim uc
            JOIN claimius.claim c
              ON c.id = uc.claim_id
             AND c.sa_deleted_at IS NULL
            WHERE uc.user_id = p_user_id
              AND uc.sa_deleted_at IS NULL
              AND (uc.starts_at IS NULL OR uc.starts_at <= now())
              AND (uc.ends_at IS NULL OR uc.ends_at > now())
            ORDER BY uc.sa_created_at ASC
            LIMIT 1;
        RETURN;
    END IF;

    RETURN QUERY
        SELECT
            claimius.composite_id(p_user_id::TEXT, p_object_type, p_object_id::TEXT),
            uc.id,
            c.id,
            (c.sa_access | co.sa_access) & 15,
            co.scope,
            co.sa_owner_id,
            co.sa_root_id,
            NULL::UUID,
            FALSE
        FROM claimius.get_ancestors(p_object_type, p_object_id) a
                 JOIN claimius.claim_object co
                      ON co.object_type = a.object_type
                          AND co.object_id = a.object_id
                          AND co.sa_deleted_at IS NULL
                 JOIN claimius.user_claim uc
                      ON uc.claim_id = co.claim_id
                          AND uc.user_id = p_user_id
                          AND uc.sa_deleted_at IS NULL
                          AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                          AND (uc.ends_at IS NULL OR uc.ends_at > now())
                 JOIN claimius.claim c
                      ON c.id = uc.claim_id
                          AND c.sa_deleted_at IS NULL
                          AND ((c.sa_access | co.sa_access) & 16) = 0
        ORDER BY a.hop ASC, claimius._popcount((c.sa_access | co.sa_access) & 15) DESC, uc.id ASC
        LIMIT 1;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_direct_access(UUID, UUID, TEXT) IS 'Locates the closest, strongest user_claim the user holds related to the given object via ancestor walk. Skips deny bindings.';

-- get_objects
DROP FUNCTION IF EXISTS claimius.get_objects(UUID, TEXT, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_objects(
    p_user_id          UUID,
    p_object_type      TEXT,
    p_required_access  INTEGER DEFAULT 0
) RETURNS TABLE(
                   object_id           UUID,
                   user_claim_id       UUID,
                   sa_access           INTEGER,
                   scope               JSONB,
                   sa_owner_id         UUID,
                   sa_location_id      UUID,
                   sa_root_id          UUID,
                   sa_name             TEXT,
                   sa_description      TEXT,
                   sa_link             TEXT,
                   direct_grant        BOOLEAN
               ) AS $$
BEGIN
    RETURN QUERY
        SELECT
            claimius.user_object.object_id,
            best.uc_id,
            claimius.user_object.sa_access,
            claimius.user_object.scope,
            claimius.user_object.sa_owner_id,
            claimius.user_object.sa_location_id,
            claimius.user_object.sa_root_id,
            claimius.user_object.sa_name,
            claimius.user_object.sa_description,
            claimius.user_object.sa_link,
            (claimius.user_object.direct_grant AND best.uc_id IS NULL)
        FROM claimius.user_object
                 LEFT JOIN LATERAL (
            SELECT uc.id AS uc_id, (g ->> 'access')::INTEGER AS access_bits
            FROM jsonb_array_elements(claimius.user_object.grants) g
                     JOIN claimius.user_claim uc ON uc.claim_id = (g ->> 'claim_id')::UUID
                AND uc.user_id = p_user_id
                AND uc.sa_deleted_at IS NULL
                AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                AND (uc.ends_at IS NULL OR uc.ends_at > now())
                     JOIN claimius.claim c ON c.id = uc.claim_id AND c.sa_deleted_at IS NULL
            WHERE ((g ->> 'access')::INTEGER & p_required_access) = p_required_access
            ORDER BY claimius._popcount((g ->> 'access')::INTEGER) DESC, (g ->> 'access')::INTEGER DESC
            LIMIT 1
            ) best ON TRUE
        WHERE claimius.user_object.user_id = p_user_id
          AND claimius.user_object.object_type = p_object_type
          AND (claimius.user_object.sa_access & p_required_access) = p_required_access
          AND (best.uc_id IS NOT NULL OR claimius.user_object.direct_grant)
        ORDER BY claimius._popcount(claimius.user_object.sa_access) DESC, claimius.user_object.object_id;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_objects IS 'Object ids and metadata for objects whose mask covers p_required_access. user_claim_id is the actor token from the strongest surviving claim grant; null when only direct_grant applies.';

-- get_organizations
DROP FUNCTION IF EXISTS claimius.get_organizations(UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_organizations(
    p_user_id          UUID,
    p_required_access  INTEGER DEFAULT 0
) RETURNS SETOF claimius.organization AS $$
BEGIN
    RETURN QUERY
        SELECT o.*
        FROM claimius.organization o
        WHERE o.sa_deleted_at IS NULL
          AND o.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required_access) g
        );
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_organizations IS 'Organizations whose effective mask covers p_required_access.';

-- get_locations
DROP FUNCTION IF EXISTS claimius.get_locations(UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_locations(
    p_user_id          UUID,
    p_required_access  INTEGER DEFAULT 0
) RETURNS SETOF claimius.location AS $$
BEGIN
    RETURN QUERY
        SELECT l.*
        FROM claimius.location l
        WHERE l.sa_deleted_at IS NULL
          AND l.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.location', p_required_access) g
        );
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_locations IS 'Locations whose effective mask covers p_required_access.';

-- get_users
DROP FUNCTION IF EXISTS claimius.get_users(UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_users(
    p_user_id          UUID,
    p_required_access  INTEGER DEFAULT 0
) RETURNS SETOF claimius.samna_user AS $$
BEGIN
    RETURN QUERY
        SELECT u.*
        FROM claimius.samna_user u
        WHERE u.sa_deleted_at IS NULL
          AND u.status = 'active'
          AND (
            u.user_id = p_user_id
                OR u.user_id IN (
                SELECT target_user_id FROM claimius.user_users
                WHERE viewer_id = p_user_id
            )
                OR u.user_id IN (
                SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.samna_user', p_required_access) g
            )
            );
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_users IS 'Users the calling user can see.';

-- get_claims (overload: by user)
DROP FUNCTION IF EXISTS claimius.get_claims(UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_claims(
    p_user_id          UUID,
    p_required_access  INTEGER DEFAULT 0
) RETURNS SETOF claimius.claim AS $$
BEGIN
    RETURN QUERY
        SELECT c.*
        FROM claimius.claim c
                 JOIN claimius.user_claim uc ON uc.claim_id = c.id
        WHERE uc.user_id = p_user_id
          AND uc.sa_deleted_at IS NULL
          AND (uc.starts_at IS NULL OR uc.starts_at <= now())
          AND (uc.ends_at IS NULL OR uc.ends_at > now())
          AND c.sa_deleted_at IS NULL
          AND (c.sa_access & p_required_access) = p_required_access
        ORDER BY claimius._popcount(c.sa_access) DESC;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_claims(UUID, INTEGER) IS 'All claims the user holds whose mask covers p_required_access.';

-- get_claims (overload: against an object)
CREATE OR REPLACE FUNCTION claimius.get_claims(
    p_user_id       UUID,
    p_object_id     UUID,
    p_object_type   TEXT
) RETURNS SETOF claimius.claim AS $$
BEGIN
    RETURN QUERY
        SELECT DISTINCT c.*
        FROM claimius.user_object uo
                 CROSS JOIN LATERAL jsonb_array_elements(uo.grants) g
                 JOIN claimius.user_claim uc ON uc.claim_id = (g ->> 'claim_id')::UUID
            AND uc.user_id = p_user_id
            AND uc.sa_deleted_at IS NULL
            AND (uc.starts_at IS NULL OR uc.starts_at <= now())
            AND (uc.ends_at IS NULL OR uc.ends_at > now())
                 JOIN claimius.claim c ON c.id = uc.claim_id AND c.sa_deleted_at IS NULL
        WHERE uo.user_id = p_user_id
          AND uo.object_id = p_object_id
          AND uo.object_type = p_object_type
        ORDER BY claimius._popcount(c.sa_access) DESC;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_claims(UUID, UUID, TEXT) IS 'All claims the user holds against one object.';

-- get_access (overload: scope key form)
CREATE OR REPLACE FUNCTION claimius.get_access(
    p_user_id          UUID,
    p_scope_key        TEXT,
    p_required_access  INTEGER DEFAULT 0
) RETURNS TABLE (
                    user_claim_id   UUID,
                    claim_id        UUID,
                    sa_access       INTEGER,
                    scope           JSONB
                ) AS $$
BEGIN
    RETURN QUERY
        SELECT uc.id,
               c.id,
               (c.sa_access | co.sa_access) & 15,
               co.scope
        FROM claimius.user_claim uc
                 JOIN claimius.claim c ON c.id = uc.claim_id
                 JOIN claimius.claim_object co
                      ON co.claim_id = uc.claim_id
        WHERE uc.user_id = p_user_id
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
          AND co.sa_deleted_at IS NULL
          AND (uc.starts_at IS NULL OR uc.starts_at <= now())
          AND (uc.ends_at  IS NULL OR uc.ends_at  >  now())
          AND co.object_id IS NULL
          AND co.scope ? p_scope_key
          AND ((c.sa_access | co.sa_access) & 16) = 0
          AND (((c.sa_access | co.sa_access) & 15) & p_required_access) = p_required_access
        ORDER BY claimius._popcount(((c.sa_access | co.sa_access) & 15)) DESC
        LIMIT 1;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_access(UUID, TEXT, INTEGER) IS 'Strongest active object-less binding whose scope contains p_scope_key and whose mask covers p_required_access.';

-- get_audit
DROP FUNCTION IF EXISTS claimius.get_audit(UUID, INTEGER, INTEGER, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_audit(
    p_user_id          UUID,
    p_required_access  INTEGER DEFAULT 0,
    p_limit            INTEGER DEFAULT 100,
    p_offset           INTEGER DEFAULT 0
) RETURNS SETOF claimius.audit AS $$
BEGIN
    RETURN QUERY
        SELECT a.*
        FROM claimius.audit a
        WHERE a.sa_owner_id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required_access) g
        )
        ORDER BY a.sa_created_at DESC
        LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_audit IS 'Audit rows for organizations the user can see.';

-- get_secrets
DROP FUNCTION IF EXISTS claimius.get_secrets(UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_secrets(
    p_user_id          UUID,
    p_required_access  INTEGER DEFAULT 0
) RETURNS SETOF claimius.samna_secret AS $$
BEGIN
    RETURN QUERY
        SELECT s.*
        FROM claimius.samna_secret s
        WHERE s.sa_deleted_at IS NULL
          AND s.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.samna_secret', p_required_access) g
        );
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_secrets IS 'Secrets whose effective mask covers p_required_access.';

DROP FUNCTION IF EXISTS claimius.get_owner_graph(UUID, UUID, INTEGER);
DROP FUNCTION IF EXISTS claimius.get_claim_graph(UUID, UUID, INTEGER);

-- get_owner_graph
CREATE OR REPLACE FUNCTION claimius.get_owner_graph(
    p_user_id          UUID,
    p_start_id         UUID DEFAULT NULL,
    p_depth            INTEGER DEFAULT 0,
    p_required_access  INTEGER DEFAULT 0
) RETURNS TABLE(object_id UUID, object_type TEXT, label TEXT, parent_id UUID, data JSONB) AS $$
DECLARE
    v_root_ids      UUID[];
    v_table         RECORD;
    v_sql           TEXT;
BEGIN
    IF p_start_id IS NULL THEN
        SELECT array_agg(o.id) INTO v_root_ids
        FROM claimius.organization o
        WHERE o.sa_deleted_at IS NULL
          AND o.sa_owner_id = o.id
          AND o.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required_access) g
        );
    ELSE
        SELECT ARRAY[o.id] INTO v_root_ids
        FROM claimius.organization o
        WHERE o.id = p_start_id
          AND o.sa_deleted_at IS NULL
          AND o.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required_access) g
        );
    END IF;

    IF v_root_ids IS NULL OR array_length(v_root_ids, 1) IS NULL THEN
        RETURN;
    END IF;

    RETURN QUERY
        WITH RECURSIVE org_walk AS (
            SELECT o.id, o.name::TEXT AS name, o.description, o.type::TEXT AS type, o.sa_level,
                   o.sa_owner_id, 0 AS lvl
            FROM claimius.organization o
            WHERE o.sa_deleted_at IS NULL
              AND o.id = ANY(v_root_ids)
            UNION ALL
            SELECT c.id, c.name::TEXT, c.description, c.type::TEXT, c.sa_level,
                   c.sa_owner_id, w.lvl + 1
            FROM claimius.organization c
                     JOIN org_walk w ON c.sa_owner_id = w.id
            WHERE c.sa_deleted_at IS NULL
              AND c.id <> c.sa_owner_id
              AND (p_depth = 0 OR w.lvl + 1 <= p_depth)
        )
        SELECT w.id ,
               'claimius.organization'::TEXT ,
               w.name::TEXT ,
               CASE WHEN w.id = w.sa_owner_id OR (w.lvl = 0 AND p_start_id IS NULL) THEN NULL
                    ELSE w.sa_owner_id
                   END ,
               jsonb_build_object(
                       'access', uo.sa_access, 'scope', uo.scope,
                       'level', w.sa_level,
                       'description', w.description,
                       'type', w.type,
                       'owner_id', w.sa_owner_id
               )
        FROM org_walk w
                 LEFT JOIN claimius.user_object uo
                           ON uo.object_id = w.id
                               AND uo.object_type = 'claimius.organization'
                               AND uo.user_id = p_user_id
        WHERE w.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required_access) g
        );

    CREATE TEMP TABLE IF NOT EXISTS _owner_graph_orgs (org_id UUID PRIMARY KEY) ON COMMIT DROP;
    DELETE FROM _owner_graph_orgs;

    INSERT INTO _owner_graph_orgs(org_id)
    WITH RECURSIVE org_walk AS (
        SELECT o.id, 0 AS lvl
        FROM claimius.organization o
        WHERE o.sa_deleted_at IS NULL
          AND o.id = ANY(v_root_ids)
        UNION ALL
        SELECT c.id, w.lvl + 1
        FROM claimius.organization c
                 JOIN org_walk w ON c.sa_owner_id = w.id
        WHERE c.sa_deleted_at IS NULL
          AND c.id <> c.sa_owner_id
          AND (p_depth = 0 OR w.lvl + 1 <= p_depth)
    )
    SELECT id FROM org_walk;

    RETURN QUERY
        SELECT l.id ,
               'claimius.location'::TEXT ,
               l.name::TEXT ,
               CASE WHEN l.sa_parent_id = l.id THEN l.sa_owner_id ELSE l.sa_parent_id END ,
               jsonb_build_object(
                       'access', uo.sa_access, 'scope', uo.scope,
                       'level', l.sa_level,
                       'description', l.description,
                       'type', l.type,
                       'owner_id', l.sa_owner_id,
                       'longitude', l.longitude,
                       'latitude', l.latitude
               )
        FROM claimius.location l
                 LEFT JOIN claimius.user_object uo
                           ON uo.object_id = l.id
                               AND uo.object_type = 'claimius.location'
                               AND uo.user_id = p_user_id
        WHERE l.sa_deleted_at IS NULL
          AND l.sa_owner_id IN (SELECT org_id FROM _owner_graph_orgs)
          AND l.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.location', p_required_access) g
        );

    FOR v_table IN
        SELECT claimius.table_info.object_type,
               claimius.table_info.has_sa_owner_id,
               claimius.table_info.has_sa_location_id,
               claimius.table_info.has_sa_parent_id,
               claimius.table_info.has_sa_deleted_at
        FROM claimius.table_info
        WHERE (claimius.table_info.has_sa_owner_id OR claimius.table_info.has_sa_location_id)
          AND claimius.table_info.object_type <> 'claimius.organization'
          AND claimius.table_info.object_type <> 'claimius.location'
          AND claimius.table_info.object_type <> 'claimius.samna_user'
        LOOP
            v_sql := format($f$
            SELECT t.id::UUID,
                   %L::TEXT,
                   coalesce(uo.sa_name, t.id::TEXT)::TEXT,
                   CASE
                       %s
                       %s
                       %s
                       ELSE NULL::UUID
                   END,
                   jsonb_build_object(
                       'access', uo.sa_access, 'scope', uo.scope,
                       'level', NULL::INTEGER,
                       'description', uo.sa_description,
                       'owner_id', uo.sa_owner_id,
                       'location_id', uo.sa_location_id
                   )
            FROM %I.%I t
            JOIN claimius.user_object uo
              ON uo.object_id = t.id
             AND uo.object_type = %L
             AND uo.user_id = $1
             AND (uo.sa_access & $2) = $2
            WHERE %s
              AND (
                  %s
                  %s
              )
        $f$,
                            v_table.object_type,
                            CASE WHEN v_table.has_sa_parent_id THEN 'WHEN t.sa_parent_id IS NOT NULL AND t.sa_parent_id <> t.id THEN t.sa_parent_id' ELSE '' END,
                            CASE WHEN v_table.has_sa_location_id THEN 'WHEN t.sa_location_id IS NOT NULL THEN t.sa_location_id' ELSE '' END,
                            CASE WHEN v_table.has_sa_owner_id THEN 'WHEN t.sa_owner_id IS NOT NULL AND t.sa_owner_id <> t.id THEN t.sa_owner_id' ELSE '' END,
                            split_part(v_table.object_type, '.', 1),
                            split_part(v_table.object_type, '.', 2),
                            v_table.object_type,
                            CASE WHEN v_table.has_sa_deleted_at THEN 't.sa_deleted_at IS NULL' ELSE 'TRUE' END,
                            CASE WHEN v_table.has_sa_owner_id THEN 't.sa_owner_id IN (SELECT org_id FROM _owner_graph_orgs)' ELSE 'FALSE' END,
                            CASE WHEN v_table.has_sa_location_id THEN ' OR EXISTS (SELECT 1 FROM claimius.location loc WHERE loc.id = t.sa_location_id AND loc.sa_owner_id IN (SELECT org_id FROM _owner_graph_orgs))' ELSE '' END
                     );

            RETURN QUERY EXECUTE v_sql USING p_user_id, p_required_access;
        END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_owner_graph(UUID, UUID, INTEGER, INTEGER) IS 'Ownership graph (orgs, locations, registered tables with sa_owner_id or sa_location_id) for the user. Optional start org and depth limit.';

-- get_claim_graph
CREATE OR REPLACE FUNCTION claimius.get_claim_graph(
    p_user_id          UUID,
    p_start_id         UUID DEFAULT NULL,
    p_depth            INTEGER DEFAULT 0,
    p_required_access  INTEGER DEFAULT 0
) RETURNS TABLE(object_id UUID, object_type TEXT, label TEXT, parent_id UUID, data JSONB) AS $$
DECLARE
    v_claim_ids     UUID[];
    v_user_row_id   UUID;
    v_cacheable     BOOLEAN;
    v_graph         JSONB;
BEGIN
    v_cacheable := (p_start_id IS NULL AND p_depth = 0 AND p_required_access = 0);

    IF v_cacheable THEN
        SELECT c.graph INTO v_graph
        FROM claimius.claim_graph_cache c
        WHERE c.user_id = p_user_id;

        IF v_graph IS NOT NULL THEN
            RETURN QUERY
                SELECT (e->>'object_id')::UUID,
                       e->>'object_type',
                       e->>'label',
                       NULLIF(e->>'parent_id', '')::UUID,
                       e->'data'
                FROM jsonb_array_elements(v_graph) AS e;
            RETURN;
        END IF;
    END IF;

    IF p_start_id IS NULL THEN
        SELECT array_agg(uc.claim_id) INTO v_claim_ids
        FROM claimius.user_claim uc
                 JOIN claimius.claim c ON c.id = uc.claim_id
        WHERE uc.user_id = p_user_id
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
          AND (c.sa_access & p_required_access) = p_required_access;
    ELSE
        SELECT array_agg(uc.claim_id) INTO v_claim_ids
        FROM claimius.user_claim uc
                 JOIN claimius.claim c ON c.id = uc.claim_id
        WHERE uc.user_id = p_user_id
          AND uc.claim_id = p_start_id
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
          AND (c.sa_access & p_required_access) = p_required_access;
    END IF;

    IF v_claim_ids IS NULL OR array_length(v_claim_ids, 1) IS NULL THEN
        RETURN;
    END IF;

    SELECT u.id INTO v_user_row_id
    FROM claimius.samna_user u
    WHERE u.user_id = p_user_id
      AND u.sa_deleted_at IS NULL
    LIMIT 1;

    WITH grant_anchors AS (
        SELECT uo.object_id          AS uo_object_id,
               uo.object_type        AS uo_object_type,
               uo.sa_access          AS uo_access,
               uo.scope              AS uo_scope,
               uo.sa_name            AS uo_name,
               uo.sa_description     AS uo_description,
               uo.sa_owner_id        AS uo_owner_id,
               uo.sa_location_id     AS uo_location_id,
               (e->>'claim_id')::UUID  AS claim_id,
               e->>'tree_type'         AS tree_type,
               CASE WHEN e->>'tree_type' = 'direct'
                    THEN uo.object_type
                    ELSE e -> 'cascaded_from' ->> 'type' END  AS anchor_type,
               CASE WHEN e->>'tree_type' = 'direct'
                    THEN uo.object_id
                    ELSE (e -> 'cascaded_from' ->> 'id')::UUID END  AS anchor_id
        FROM claimius.user_object uo
                 CROSS JOIN LATERAL jsonb_array_elements(uo.grants) e
        WHERE uo.user_id = p_user_id
          AND (uo.sa_access & p_required_access) = p_required_access
          AND (e->>'claim_id')::UUID = ANY(v_claim_ids)
          AND (p_depth = 0 OR e->>'tree_type' = 'direct')
    ),
    self_node AS (
        SELECT u.id                                                                     AS object_id,
               'claimius.samna_user'::TEXT                                              AS object_type,
               coalesce(u.first_name || ' ' || u.last_name, u.email, u.user_id::TEXT)::TEXT  AS label,
               NULL::UUID                                                               AS parent_id,
               jsonb_build_object(
                   'access', NULL::INTEGER,
                   'scope', NULL::JSONB,
                   'level', NULL::INTEGER,
                   'user_id', u.user_id,
                   'email', u.email,
                   'external_id', u.external_id,
                   'status', u.status
               )                                                                        AS data
        FROM claimius.samna_user u
        WHERE u.id = v_user_row_id
    ),
    other_users_node AS (
        SELECT u.id                                                                     AS object_id,
               'claimius.samna_user'::TEXT                                              AS object_type,
               coalesce(u.first_name || ' ' || u.last_name, u.email, u.user_id::TEXT)::TEXT  AS label,
               v_user_row_id                                                            AS parent_id,
               jsonb_build_object(
                   'access', uo.sa_access, 'scope', uo.scope,
                   'level', NULL::INTEGER,
                   'user_id', u.user_id,
                   'email', u.email,
                   'external_id', u.external_id,
                   'status', u.status
               )                                                                        AS data
        FROM claimius.samna_user u
                 JOIN claimius.user_users uu
                      ON uu.viewer_id = p_user_id
                          AND uu.target_user_id = u.user_id
                 LEFT JOIN claimius.user_object uo
                      ON uo.object_id = u.id
                          AND uo.object_type = 'claimius.samna_user'
                          AND uo.user_id = p_user_id
        WHERE u.sa_deleted_at IS NULL
          AND u.id <> v_user_row_id
    ),
    user_claim_node AS (
        SELECT uc.id                AS object_id,
               'claimius.user_claim'::TEXT  AS object_type,
               c.name::TEXT         AS label,
               v_user_row_id        AS parent_id,
               jsonb_build_object(
                   'access', NULL::INTEGER,
                   'scope', NULL::JSONB,
                   'level', NULL::INTEGER,
                   'claim_id', uc.claim_id,
                   'starts_at', uc.starts_at,
                   'ends_at', uc.ends_at,
                   'reason', uc.reason
               )                    AS data
        FROM claimius.user_claim uc
                 JOIN claimius.claim c ON c.id = uc.claim_id
        WHERE uc.user_id = p_user_id
          AND uc.sa_deleted_at IS NULL
          AND uc.claim_id = ANY(v_claim_ids)
    ),
    claim_node AS (
        SELECT c.id                 AS object_id,
               'claimius.claim'::TEXT  AS object_type,
               c.name::TEXT         AS label,
               NULL::UUID           AS parent_id,
               jsonb_build_object(
                   'access', c.sa_access,
                   'scope', NULL::JSONB,
                   'level', NULL::INTEGER,
                   'description', c.description,
                   'inherits', c.inherits,
                   'type', c.type
               )                    AS data
        FROM claimius.claim c
        WHERE c.sa_deleted_at IS NULL
          AND c.id = ANY(v_claim_ids)
    ),
    claim_object_node AS (
        SELECT co.id                AS object_id,
               'claimius.claim_object'::TEXT  AS object_type,
               coalesce(uo.sa_name, co.object_id::TEXT, '')::TEXT  AS label,
               co.claim_id          AS parent_id,
               jsonb_build_object(
                   'access', co.sa_access,
                   'scope', co.scope,
                   'level', NULL::INTEGER,
                   'target_object_id', co.object_id,
                   'target_object_type', co.object_type,
                   'inherits', co.inherits
               )                    AS data
        FROM claimius.claim_object co
                 LEFT JOIN claimius.user_object uo
                      ON uo.object_id = co.object_id
                          AND uo.object_type = co.object_type
                          AND uo.user_id = p_user_id
        WHERE co.sa_deleted_at IS NULL
          AND co.claim_id = ANY(v_claim_ids)
    ),
    object_node AS (
        SELECT ga.uo_object_id      AS object_id,
               ga.uo_object_type    AS object_type,
               coalesce(ga.uo_name, ga.uo_object_id::TEXT)::TEXT  AS label,
               co.id                AS parent_id,
               jsonb_build_object(
                   'access', ga.uo_access,
                   'scope', ga.uo_scope,
                   'level', NULL::INTEGER,
                   'description', ga.uo_description,
                   'owner_id', ga.uo_owner_id,
                   'location_id', ga.uo_location_id,
                   'tree_type', ga.tree_type
               )                    AS data
        FROM grant_anchors ga
                 JOIN claimius.claim_object co
                      ON co.claim_id = ga.claim_id
                          AND co.object_type = ga.anchor_type
                          AND co.object_id = ga.anchor_id
                          AND co.sa_deleted_at IS NULL
    ),
    all_nodes AS (
        SELECT * FROM self_node
        UNION ALL
        SELECT * FROM other_users_node
        UNION ALL
        SELECT * FROM user_claim_node
        UNION ALL
        SELECT * FROM claim_node
        UNION ALL
        SELECT * FROM claim_object_node
        UNION ALL
        SELECT * FROM object_node
    )
    SELECT jsonb_agg(jsonb_build_object(
                'object_id', n.object_id,
                'object_type', n.object_type,
                'label', n.label,
                'parent_id', n.parent_id,
                'data', n.data
           )) INTO v_graph
    FROM all_nodes n;

    IF v_graph IS NULL THEN
        v_graph := '[]'::JSONB;
    END IF;

    IF v_cacheable THEN
        INSERT INTO claimius.claim_graph_cache (user_id, graph, sa_updated_at)
        VALUES (p_user_id, v_graph, now())
        ON CONFLICT (user_id) DO UPDATE
            SET graph = EXCLUDED.graph,
                sa_updated_at = EXCLUDED.sa_updated_at;
    END IF;

    RETURN QUERY
        SELECT (e->>'object_id')::UUID,
               e->>'object_type',
               e->>'label',
               NULLIF(e->>'parent_id', '')::UUID,
               e->'data'
        FROM jsonb_array_elements(v_graph) AS e;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_claim_graph(UUID, UUID, INTEGER, INTEGER) IS 'Access graph: user, other users seen, claims, user_claims, claim_object bindings, reachable objects. Optional start claim and depth limit. Default shape served from claimius.claim_graph_cache.';

-- _invalidate_claim_graph_cache
CREATE OR REPLACE FUNCTION claimius._invalidate_claim_graph_cache()
    RETURNS TRIGGER AS $$
DECLARE
    v_user_id   UUID;
BEGIN
    IF TG_TABLE_NAME = 'user_object' THEN
        v_user_id := COALESCE(NEW.user_id, OLD.user_id);
        DELETE FROM claimius.claim_graph_cache WHERE user_id = v_user_id;
    ELSIF TG_TABLE_NAME = 'user_users' THEN
        v_user_id := COALESCE(NEW.viewer_id, OLD.viewer_id);
        DELETE FROM claimius.claim_graph_cache WHERE user_id = v_user_id;
    ELSIF TG_TABLE_NAME = 'samna_user' THEN
        v_user_id := COALESCE(NEW.user_id, OLD.user_id);
        DELETE FROM claimius.claim_graph_cache WHERE user_id = v_user_id;
    ELSIF TG_TABLE_NAME = 'claim' OR TG_TABLE_NAME = 'claim_object' THEN
        DELETE FROM claimius.claim_graph_cache;
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._invalidate_claim_graph_cache IS 'Trigger function. Drops claim_graph_cache rows affected by writes to user_object, user_users, samna_user, claim, claim_object.';

-- search
DROP FUNCTION IF EXISTS claimius.search(UUID, TEXT, TEXT[], INTEGER, INTEGER, INTEGER);

CREATE OR REPLACE FUNCTION claimius.search(
    p_user_id          UUID,
    p_query            TEXT,
    p_object_types     TEXT[] DEFAULT NULL,
    p_required_access  INTEGER DEFAULT 0,
    p_limit            INTEGER DEFAULT 20,
    p_offset           INTEGER DEFAULT 0
) RETURNS TABLE(
                   object_id           UUID,
                   object_type         TEXT,
                   user_claim_id       UUID,
                   name                TEXT,
                   description         TEXT,
                   link                TEXT,
                   sa_access           INTEGER,
                   scope               JSONB,
                   owner_id            UUID,
                   location_id         UUID,
                   is_direct_grant     BOOLEAN,
                   rank                REAL
               ) AS $$
DECLARE
    v_tsquery TSQUERY;
BEGIN
    v_tsquery := websearch_to_tsquery('simple', p_query);

    RETURN QUERY
        SELECT
            claimius.user_object.object_id,
            claimius.user_object.object_type,
            best.uc_id,
            claimius.user_object.sa_name,
            claimius.user_object.sa_description,
            claimius.user_object.sa_link,
            claimius.user_object.sa_access,
            claimius.user_object.scope,
            claimius.user_object.sa_owner_id,
            claimius.user_object.sa_location_id,
            (claimius.user_object.direct_grant AND best.uc_id IS NULL),
            ts_rank(claimius.user_object.search_vector, v_tsquery)
        FROM claimius.user_object
                 LEFT JOIN LATERAL (
            SELECT uc.id AS uc_id
            FROM jsonb_array_elements(claimius.user_object.grants) g
                     JOIN claimius.user_claim uc ON uc.claim_id = (g ->> 'claim_id')::UUID
                AND uc.user_id = p_user_id
                AND uc.sa_deleted_at IS NULL
                AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                AND (uc.ends_at IS NULL OR uc.ends_at > now())
                     JOIN claimius.claim c ON c.id = uc.claim_id AND c.sa_deleted_at IS NULL
            WHERE ((g ->> 'access')::INTEGER & p_required_access) = p_required_access
            ORDER BY claimius._popcount((g ->> 'access')::INTEGER) DESC
            LIMIT 1
            ) best ON TRUE
        WHERE claimius.user_object.user_id = p_user_id
          AND (claimius.user_object.sa_access & p_required_access) = p_required_access
          AND claimius.user_object.search_vector @@ v_tsquery
          AND (p_object_types IS NULL OR claimius.user_object.object_type = ANY(p_object_types))
          AND (best.uc_id IS NOT NULL OR claimius.user_object.direct_grant)
        ORDER BY ts_rank(claimius.user_object.search_vector, v_tsquery) DESC
        LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.search(UUID, TEXT, TEXT[], INTEGER, INTEGER, INTEGER) IS 'Full text search over accessible objects whose mask covers p_required_access. user_claim_id is the actor token from the strongest surviving claim grant per object.';
