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
-- Returns the surviving grant the user has against one object whose
-- effective access mask covers all bits in p_required_access. Joins
-- user_object grants array against user_claim and claim, filters by
-- sa_deleted_at and time validity. Returns one row, picking the grant
-- whose access mask carries the most bits set.
--
-- Contract: when claim_id is non-null, user_claim_id is also populated. This
-- is the actor token callers use as sa_created_by on follow-on writes.
-- When direct_grant is true and claim_id is null, user_claim_id is also null;
-- callers needing an actor for direct-only access must resolve a fallback
-- user_claim themselves (typically the user's bootstrap user_claim for the app).
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_access(UUID, UUID, UUID, TEXT, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_access(
    p_user_id          UUID,
    p_app_id           UUID,
    p_object_id        UUID,
    p_object_type      TEXT,
    p_required_access  INTEGER DEFAULT 0
) RETURNS TABLE (
                    user_object_id      UUID,
                    user_claim_id       UUID,
                    claim_id            UUID,
                    claim_name          TEXT,
                    sa_access           INTEGER,
                    scope               JSONB,
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
              AND (u.sa_access & p_required_access) = p_required_access
        )
        SELECT
            uo.id,
            best.user_claim_id,
            best.claim_id,
            best.claim_name,
            best.access_bits,
            best.grant_scope,
            uo.sa_owner_id,
            (SELECT o.sa_root_id FROM claimius.organization o WHERE o.id = uo.sa_owner_id),
            uo.sa_location_id,
            FALSE
        FROM uo
                 CROSS JOIN LATERAL claimius._best_grant(uo.grants, p_user_id, p_app_id, p_required_access) best;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_access(UUID, UUID, UUID, TEXT, INTEGER) IS 'Strongest active claim based grant for one (user, app, object) whose mask covers p_required_access. user_claim_id is the actor token for follow-on writes. sa_root_id is the owning organization root, suitable for FK to claimius.organization on follow-on claim and claim_object writes.';

-- ----------------------------------------------------------------------------
-- get_direct_access
-- Locates the closest, highest-privilege user_claim the calling user holds
-- that's structurally related to the given object. Walks the owner,
-- location, and parenthood ancestor chains; for each ancestor checks
-- whether the user has any user_claim bound to it via claim_object.
-- Returns the same row shape as get_access (user_object_id, user_claim_id,
-- claim_id, sa_access, scope, sa_owner_id, sa_root_id, sa_location_id,
-- direct_grant). Returns no rows when no claim is found anywhere up the
-- chain, which means the user has no access through the structural lookup.
--
-- Use this when get_access returns no row but the caller still expects an
-- actor token via the user's structural relationship to the object. In
-- normal flow, every resource should have a direct claim_object binding
-- (the owner claim rule), so this lookup mostly serves edge cases.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_direct_access(UUID, UUID, UUID, TEXT);

CREATE OR REPLACE FUNCTION claimius.get_direct_access(
    p_user_id       UUID,
    p_app_id        UUID,
    p_object_id     UUID,
    p_object_type   TEXT,
    p_required      INTEGER DEFAULT 0,
    p_raise         BOOLEAN DEFAULT FALSE
) RETURNS TABLE (
                    user_object_id      UUID,
                    user_claim_id       UUID,
                    claim_id            UUID,
                    claim_name          TEXT,
                    sa_access           INTEGER,
                    scope               JSONB,
                    sa_owner_id         UUID,
                    sa_root_id          UUID,
                    sa_location_id      UUID,
                    direct_grant        BOOLEAN
                ) AS $$
DECLARE
    v_exists   BOOLEAN;
    v_has_soft BOOLEAN;
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    IF p_object_type = 'claimius.samna_user' AND p_object_id = p_user_id THEN
        RETURN QUERY
            SELECT
                claimius.composite_id(p_app_id::TEXT, p_user_id::TEXT, p_object_type, p_object_id::TEXT),
                uc.id,
                uc.claim_id,
                'self'::TEXT,
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
              AND uc.app_id = p_app_id
              AND uc.sa_deleted_at IS NULL
              AND (uc.starts_at IS NULL OR uc.starts_at <= now())
              AND (uc.ends_at IS NULL OR uc.ends_at > now())
            ORDER BY uc.sa_created_at ASC
            LIMIT 1;
        RETURN;
    END IF;

    RETURN QUERY
        SELECT
            claimius.composite_id(p_app_id::TEXT, p_user_id::TEXT, p_object_type, p_object_id::TEXT),
            uc.id,
            c.id,
            c.name,
            (c.sa_access | co.sa_access) & 15,
            co.scope,
            co.sa_owner_id,
            co.sa_root_id,
            NULL::UUID,
            FALSE
        FROM claimius.get_ancestors(p_object_type, p_object_id) a
                 JOIN claimius.claim_object co
                      ON co.app_id = p_app_id
                          AND co.object_type = a.object_type
                          AND co.object_id = a.object_id
                          AND co.sa_deleted_at IS NULL
                 JOIN claimius.user_claim uc
                      ON uc.claim_id = co.claim_id
                          AND uc.user_id = p_user_id
                          AND uc.app_id = p_app_id
                          AND uc.sa_deleted_at IS NULL
                          AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                          AND (uc.ends_at IS NULL OR uc.ends_at > now())
                 JOIN claimius.claim c
                      ON c.id = uc.claim_id
                          AND c.sa_deleted_at IS NULL
                          AND ((c.sa_access | co.sa_access) & 16) = 0
                          AND ((((c.sa_access | co.sa_access) & 15) & p_required) = p_required)
        ORDER BY a.hop ASC, claimius._popcount((c.sa_access | co.sa_access) & 15) DESC, uc.id ASC
        LIMIT 1;

    IF FOUND OR NOT p_raise THEN
        RETURN;
    END IF;

    IF p_object_type = 'claimius.samna_user' THEN
        SELECT EXISTS (
            SELECT 1 FROM claimius.samna_user
            WHERE user_id = p_object_id
              AND app_id = p_app_id
              AND sa_deleted_at IS NULL
        ) INTO v_exists;
    ELSE
        SELECT has_sa_deleted_at INTO v_has_soft
        FROM claimius.table_info
        WHERE object_type = p_object_type;

        IF v_has_soft IS TRUE THEN
            EXECUTE format(
                'SELECT EXISTS (SELECT 1 FROM %I.%I WHERE id = $1 AND sa_deleted_at IS NULL)',
                split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2)
            ) USING p_object_id INTO v_exists;
        ELSE
            EXECUTE format(
                'SELECT EXISTS (SELECT 1 FROM %I.%I WHERE id = $1)',
                split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2)
            ) USING p_object_id INTO v_exists;
        END IF;
    END IF;

    IF v_exists THEN
        RAISE EXCEPTION 'no access' USING ERRCODE = 'P0403';
    ELSE
        RAISE EXCEPTION 'not found' USING ERRCODE = 'P0404';
    END IF;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_direct_access(UUID, UUID, UUID, TEXT, INTEGER, BOOLEAN) IS 'Locates the closest, strongest user_claim the user holds related to the given object via ancestor walk. Skips deny bindings.';

-- ----------------------------------------------------------------------------
-- get_objects
-- Returns object ids of the given object_type whose effective access mask
-- covers all bits in p_required_access. The default 0 returns every object
-- the user has any access to.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_objects(UUID, UUID, TEXT, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_objects(
    p_user_id          UUID,
    p_app_id           UUID,
    p_object_type      TEXT,
    p_required_access  INTEGER DEFAULT 0
) RETURNS TABLE(
                   object_id           UUID,
                   user_claim_id       UUID,
                   claim_id            UUID,
                   claim_name          TEXT,
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
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    -- For each (user, object) row, pick the surviving claim grant whose
    -- access mask carries the most bits, restricted to grants that cover
    -- the required access bits. If only direct_grant applies,
    -- user_claim_id is null and direct_grant is true.
    RETURN QUERY
        SELECT
            claimius.user_object.object_id,
            best.user_claim_id,
            best.claim_id,
            COALESCE(best.claim_name, 'direct'),
            claimius.user_object.sa_access,
            claimius.user_object.scope,
            claimius.user_object.sa_owner_id,
            claimius.user_object.sa_location_id,
            (SELECT o.sa_root_id FROM claimius.organization o WHERE o.id = claimius.user_object.sa_owner_id),
            claimius.user_object.sa_name,
            claimius.user_object.sa_description,
            claimius.user_object.sa_link,
            (claimius.user_object.direct_grant AND best.user_claim_id IS NULL)
        FROM claimius.user_object
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_user_id, p_app_id, p_required_access) best ON TRUE
        WHERE claimius.user_object.user_id = p_user_id
          AND claimius.user_object.app_id = p_app_id
          AND claimius.user_object.object_type = p_object_type
          AND (claimius.user_object.sa_access & p_required_access) = p_required_access
          AND (best.user_claim_id IS NOT NULL OR claimius.user_object.direct_grant)
        ORDER BY claimius._popcount(claimius.user_object.sa_access) DESC, claimius.user_object.object_id;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_objects(p_user_id uuid, p_app_id uuid, p_object_type text, p_required_access integer) IS 'Object ids and metadata for objects whose mask covers p_required_access. user_claim_id is the actor token from the strongest surviving claim grant; null when only direct_grant applies. claim_id and claim_name describe the claim that contributed the strongest grant. sa_root_id is derived from the owning organization root.';

-- ----------------------------------------------------------------------------
-- get_organizations
-- Returns organizations whose mask covers p_required_access.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_organizations(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_organizations(
    p_user_id          UUID,
    p_app_id           UUID,
    p_required_access  INTEGER DEFAULT 0
) RETURNS SETOF claimius.organization AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT o.*
        FROM claimius.organization o
        WHERE o.id IN (SELECT claimius.app_object_ids(p_app_id, 'claimius.organization'))
          AND o.sa_deleted_at IS NULL
          AND o.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.organization', p_required_access) g
        );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_organizations(p_user_id uuid, p_app_id uuid, p_required_access integer) IS 'Organizations whose effective mask covers p_required_access.';

-- ----------------------------------------------------------------------------
-- get_locations
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_locations(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_locations(
    p_user_id          UUID,
    p_app_id           UUID,
    p_required_access  INTEGER DEFAULT 0
) RETURNS SETOF claimius.location AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT l.*
        FROM claimius.location l
        WHERE l.id IN (SELECT claimius.app_object_ids(p_app_id, 'claimius.location'))
          AND l.sa_deleted_at IS NULL
          AND l.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.location', p_required_access) g
        );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_locations(p_user_id uuid, p_app_id uuid, p_required_access integer) IS 'Locations whose effective mask covers p_required_access.';

-- ----------------------------------------------------------------------------
-- get_users
-- Returns the users this user can see in this app: themselves, users they
-- hold direct claims on, plus users who share any object with them via
-- user_users.
-- ----------------------------------------------------------------------------

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace WHERE t.typname = 'access_user' AND n.nspname = 'claimius') THEN
        CREATE TYPE claimius.access_user AS (
            target_user_id  UUID,
            access          INTEGER,
            user_claim_id   UUID,
            claim_id        UUID,
            owner_id        UUID,
            root_id         UUID,
            location_id     UUID,
            direct_grant    BOOLEAN,
            claim_name      TEXT
        );
    ELSE
        BEGIN
            ALTER TYPE claimius.access_user ADD ATTRIBUTE claim_name TEXT;
        EXCEPTION
            WHEN duplicate_column THEN NULL;
        END;
    END IF;
END $$;

COMMENT ON TYPE claimius.access_user IS
    'Resolved access of one actor on one target samna_user. access is the OR of three branches: self (0x2F owner|write|read|execute|member), member co-visibility via user_users (0x24 member|read), and direct claim_object on claimius.samna_user (raw sa_access of the strongest surviving grant). claim_name is always populated: self for the self branch, member for the co-visibility branch, the granting claim name for the direct branch. user_claim_id, claim_id, owner_id, root_id, location_id are populated only when the direct branch contributes; otherwise NULL.';

DROP FUNCTION IF EXISTS claimius.get_user_access(UUID, UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_user_access(
    p_actor_id         UUID,
    p_app_id           UUID,
    p_target           TEXT,
    p_required_access  INTEGER DEFAULT 0
) RETURNS SETOF claimius.access_user AS $$
DECLARE
    v_target_user_id UUID;
    v_uuid           UUID;
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_actor_id);

    IF NOT claimius.check_user_active(p_app_id, p_actor_id) THEN
        RETURN;
    END IF;

    IF p_target ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        v_uuid := p_target::UUID;
    END IF;

    SELECT su.user_id INTO v_target_user_id
    FROM claimius.samna_user su
    WHERE su.app_id = p_app_id
      AND su.sa_deleted_at IS NULL
      AND su.status = 'active'
      AND (su.external_id = p_target OR su.user_id = v_uuid OR su.id = v_uuid)
    ORDER BY CASE
                 WHEN su.external_id = p_target THEN 0
                 WHEN su.user_id = v_uuid THEN 1
                 ELSE 2
             END
    LIMIT 1;

    IF v_target_user_id IS NULL THEN
        RETURN;
    END IF;

    RETURN QUERY
        WITH branches AS (
            SELECT
                47::INTEGER       AS access,
                NULL::UUID        AS user_claim_id,
                NULL::UUID        AS claim_id,
                'self'::TEXT      AS claim_name,
                NULL::UUID        AS owner_id,
                NULL::UUID        AS root_id,
                NULL::UUID        AS location_id,
                FALSE             AS direct_grant
            WHERE p_actor_id = v_target_user_id

            UNION ALL

            SELECT
                36::INTEGER,
                NULL::UUID,
                NULL::UUID,
                'member'::TEXT,
                NULL::UUID,
                NULL::UUID,
                NULL::UUID,
                FALSE
            FROM claimius.user_users uu
            WHERE uu.app_id        = p_app_id
              AND uu.viewer_id     = p_actor_id
              AND uu.target_user_id = v_target_user_id

            UNION ALL

            SELECT g.access, g.user_claim_id, g.claim_id, g.claim_name, g.owner_id, g.root_id, NULL::UUID, FALSE
            FROM claimius._user_claim_grants(p_actor_id, p_app_id, v_target_user_id) g
        ),
        merged AS (
            SELECT
                v_target_user_id AS target_user_id,
                COALESCE(bit_or(access), 0) AS access,
                (array_agg(user_claim_id) FILTER (WHERE user_claim_id IS NOT NULL))[1] AS user_claim_id,
                (array_agg(claim_id)      FILTER (WHERE claim_id      IS NOT NULL))[1] AS claim_id,
                COALESCE(
                    (array_agg(claim_name) FILTER (WHERE claim_name IS NOT NULL AND claim_name NOT IN ('self', 'member')))[1],
                    (array_agg(claim_name) FILTER (WHERE claim_name IS NOT NULL))[1]
                ) AS claim_name,
                (array_agg(owner_id)      FILTER (WHERE owner_id      IS NOT NULL))[1] AS owner_id,
                (array_agg(root_id)       FILTER (WHERE root_id       IS NOT NULL))[1] AS root_id,
                (array_agg(location_id)   FILTER (WHERE location_id   IS NOT NULL))[1] AS location_id,
                bool_or(direct_grant) AS direct_grant
            FROM branches
        )
        SELECT
            target_user_id,
            access,
            user_claim_id,
            claim_id,
            owner_id,
            root_id,
            location_id,
            COALESCE(direct_grant, FALSE),
            claim_name
        FROM merged
        WHERE access > 0
          AND (access & p_required_access) = p_required_access;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_user_access(UUID, UUID, TEXT, INTEGER) IS
    'Resolved actor access on one target samna_user. OR of self, member co-visibility, and direct claim branches. Empty result means the required mask is not covered.';

DROP FUNCTION IF EXISTS claimius.get_users(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_users(
    p_user_id          UUID,
    p_app_id           UUID,
    p_required_access  INTEGER DEFAULT 0
) RETURNS SETOF claimius.samna_user AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        WITH branches AS (
            SELECT p_user_id AS target_user_id, 47::INTEGER AS access
            WHERE p_user_id IS NOT NULL

            UNION ALL

            SELECT uu.target_user_id, 36::INTEGER
            FROM claimius.user_users uu
            WHERE uu.app_id    = p_app_id
              AND uu.viewer_id = p_user_id

            UNION ALL

            SELECT g.target_user_id, g.access
            FROM claimius._user_claim_grants(p_user_id, p_app_id, NULL) g
        ),
        resolved AS (
            SELECT target_user_id, bit_or(access) AS access
            FROM branches
            GROUP BY target_user_id
            HAVING (bit_or(access) & p_required_access) = p_required_access
        )
        SELECT u.*
        FROM claimius.samna_user u
        JOIN resolved r ON r.target_user_id = u.user_id
        WHERE u.app_id        = p_app_id
          AND u.sa_deleted_at IS NULL
          AND u.status        = 'active';
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_users(UUID, UUID, INTEGER) IS
    'Users the calling user can see in this app whose resolved access covers p_required_access. Resolution merges self (0x2F), member co-visibility via user_users (0x24), and direct claim_object on claimius.samna_user.';

GRANT EXECUTE ON FUNCTION claimius.get_access(UUID, UUID, UUID, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_direct_access(UUID, UUID, UUID, TEXT, INTEGER, BOOLEAN)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_objects(UUID, UUID, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_user_access(UUID, UUID, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_users(UUID, UUID, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

-- ----------------------------------------------------------------------------
-- resolve_access + mutation_access
-- Single object access gate for the mutation path. resolve_access dispatches
-- audit, user_relation, and samna_user object types to their bespoke lookups
-- and everything else through get_access then get_direct_access. mutation_access
-- wraps it and RAISEs P0403 when the object exists but the caller lacks access,
-- P0404 when the object is absent or soft deleted, so the Go side maps one round
-- trip to 200 / 403 / 404.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.require_access(UUID, UUID, TEXT, INTEGER);

DROP FUNCTION IF EXISTS claimius.resolve_access(UUID, UUID, UUID, TEXT, INTEGER);
DROP FUNCTION IF EXISTS claimius.resolve_access(UUID, UUID, TEXT, INTEGER);
CREATE OR REPLACE FUNCTION claimius.resolve_access(
    p_user_id     UUID,
    p_app_id      UUID,
    p_object_id   TEXT,
    p_object_type TEXT,
    p_required    INTEGER
) RETURNS TABLE (
                    user_object_id UUID,
                    user_claim_id  UUID,
                    claim_id       UUID,
                    claim_name     TEXT,
                    sa_access      INTEGER,
                    scope          JSONB,
                    sa_owner_id    UUID,
                    sa_root_id     UUID,
                    sa_location_id UUID,
                    direct_grant   BOOLEAN
                ) AS $$
BEGIN
    IF p_object_type = 'claimius.audit' THEN
        RETURN QUERY
            SELECT a.id, NULL::UUID, NULL::UUID, 'audit'::TEXT,
                   p_required, NULL::JSONB, a.sa_owner_id,
                   (SELECT o.sa_root_id FROM claimius.organization o WHERE o.id = a.sa_owner_id),
                   NULL::UUID, TRUE
            FROM claimius.audit a
            WHERE a.id = p_object_id::UUID
              AND a.app_id = p_app_id
              AND a.sa_owner_id IN (
                  SELECT g.object_id
                  FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.organization', p_required) g
              );
        RETURN;
    ELSIF p_object_type = 'claimius.user_relation' THEN
        RETURN QUERY
            SELECT r.id, NULL::UUID, NULL::UUID, 'user_relation'::TEXT,
                   p_required, NULL::JSONB, p_user_id, p_user_id, NULL::UUID, TRUE
            FROM claimius.user_relation r
            WHERE r.id = p_object_id::UUID
              AND r.user_id = p_user_id
              AND r.sa_deleted_at IS NULL;
        RETURN;
    ELSIF p_object_type = 'claimius.samna_user' THEN
        RETURN QUERY
            SELECT u.target_user_id, u.user_claim_id, u.claim_id, u.claim_name, u.access,
                   NULL::JSONB, COALESCE(u.owner_id, p_user_id), COALESCE(u.root_id, p_user_id),
                   u.location_id, u.direct_grant
            FROM claimius.get_user_access(p_user_id, p_app_id, p_object_id, p_required) u;
        RETURN;
    END IF;

    RETURN QUERY
        SELECT * FROM claimius.get_access(p_user_id, p_app_id, p_object_id::UUID, p_object_type, p_required);
    IF FOUND THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT g.user_object_id, g.user_claim_id, g.claim_id, g.claim_name, g.sa_access,
               g.scope, g.sa_owner_id, g.sa_root_id, g.sa_location_id, g.direct_grant
        FROM claimius.get_direct_access(p_user_id, p_app_id, p_object_id::UUID, p_object_type, p_required) g;
END;
$$ LANGUAGE plpgsql STABLE;
CREATE OR REPLACE FUNCTION claimius.resolve_access(
    p_user_id     UUID,
    p_object_id   TEXT,
    p_object_type TEXT,
    p_required    INTEGER
) RETURNS TABLE (
                    user_object_id UUID,
                    user_claim_id  UUID,
                    claim_id       UUID,
                    claim_name     TEXT,
                    sa_access      INTEGER,
                    scope          JSONB,
                    sa_owner_id    UUID,
                    sa_root_id     UUID,
                    sa_location_id UUID,
                    direct_grant   BOOLEAN
                ) AS $$
    SELECT * FROM claimius.resolve_access(p_user_id, claimius.get_disciple_app_id(), p_object_id, p_object_type, p_required);
$$ LANGUAGE sql STABLE;


GRANT EXECUTE ON FUNCTION claimius.resolve_access(UUID, TEXT, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.resolve_access(UUID, UUID, TEXT, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

DROP FUNCTION IF EXISTS claimius.mutation_access(UUID, UUID, TEXT, INTEGER);
CREATE OR REPLACE FUNCTION claimius.mutation_access(
    p_user_id     UUID,
    p_object_id   TEXT,
    p_object_type TEXT,
    p_required    INTEGER
) RETURNS TABLE (
                    user_object_id UUID,
                    user_claim_id  UUID,
                    claim_id       UUID,
                    claim_name     TEXT,
                    sa_access      INTEGER,
                    scope          JSONB,
                    sa_owner_id    UUID,
                    sa_root_id     UUID,
                    sa_location_id UUID,
                    direct_grant   BOOLEAN
                ) AS $$
DECLARE
    v_app_id UUID := claimius.get_disciple_app_id();
    v_uuid   UUID;
BEGIN
    RETURN QUERY
        SELECT * FROM claimius.resolve_access(p_user_id, v_app_id, p_object_id, p_object_type, p_required);
    IF FOUND THEN
        RETURN;
    END IF;

    IF p_object_id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        v_uuid := p_object_id::UUID;
    END IF;

    IF p_object_type = 'claimius.samna_user' THEN
        IF EXISTS (
            SELECT 1 FROM claimius.samna_user su
            WHERE su.app_id = v_app_id
              AND su.sa_deleted_at IS NULL
              AND (su.external_id = p_object_id OR su.user_id = v_uuid OR su.id = v_uuid)
        ) THEN
            RAISE EXCEPTION 'no access' USING ERRCODE = 'P0403';
        ELSE
            RAISE EXCEPTION 'not found' USING ERRCODE = 'P0404';
        END IF;
    END IF;

    RETURN QUERY
        SELECT * FROM claimius.get_direct_access(p_user_id, v_app_id, v_uuid, p_object_type, p_required, TRUE);
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.mutation_access(UUID, TEXT, TEXT, INTEGER) IS 'Single object access gate. Resolves access through get_access then get_direct_access, returns the strongest AccessClaim row, and RAISEs SQLSTATE P0403 when the object exists but the caller lacks the required access, P0404 when the object is absent or soft deleted. SQL embodiment of the Go MutationAccess resolution in one round trip.';

GRANT EXECUTE ON FUNCTION claimius.mutation_access(UUID, TEXT, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

-- ----------------------------------------------------------------------------
-- get_claims (overload: by user)
-- Returns every claim the user holds whose access mask covers
-- p_required_access.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_claims(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_claims(
    p_user_id          UUID,
    p_app_id           UUID,
    p_required_access  INTEGER DEFAULT 0
) RETURNS SETOF claimius.claim AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT c.*
        FROM claimius.claim c
        WHERE c.sa_deleted_at IS NULL
          AND (
              EXISTS (
                  SELECT 1 FROM claimius.user_claim uc
                  WHERE uc.claim_id = c.id
                    AND uc.user_id = p_user_id
                    AND uc.app_id = p_app_id
                    AND uc.sa_deleted_at IS NULL
                    AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                    AND (uc.ends_at IS NULL OR uc.ends_at > now())
                    AND (c.sa_access & p_required_access) = p_required_access
              )
              OR EXISTS (
                  SELECT 1 FROM claimius.user_object uo
                  WHERE uo.app_id = p_app_id
                    AND uo.user_id = p_user_id
                    AND uo.object_type = 'claimius.claim'
                    AND uo.object_id = c.id
                    AND (uo.sa_access & p_required_access) = p_required_access
              )
          )
        ORDER BY claimius._popcount(c.sa_access) DESC;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_claims(UUID, UUID, INTEGER) IS 'All claims the user can read at the required level: either via a direct user_claim grant or via the materialized user_object index (cascade or direct creator grant).';

-- ----------------------------------------------------------------------------
-- get_claims (overload: against an object)
-- Returns every claim the user holds against one object.
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
        SELECT c.*
        FROM claimius.claim c
        WHERE c.sa_deleted_at IS NULL
          AND c.id IN (
              SELECT uc.claim_id
              FROM claimius.user_object uo
                       CROSS JOIN LATERAL jsonb_array_elements(uo.grants) g
                       JOIN claimius.user_claim uc ON uc.claim_id = (g ->> 'claim_id')::UUID
                  AND uc.user_id = p_user_id
                  AND uc.app_id = p_app_id
                  AND uc.sa_deleted_at IS NULL
                  AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                  AND (uc.ends_at IS NULL OR uc.ends_at > now())
              WHERE uo.user_id = p_user_id
                AND uo.app_id = p_app_id
                AND uo.object_id = p_object_id
                AND uo.object_type = p_object_type
          )
        ORDER BY claimius._popcount(c.sa_access) DESC;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_claims(UUID, UUID, UUID, TEXT) IS 'All claims the user holds against one object.';

-- ----------------------------------------------------------------------------
-- get_access (overload: scope key form)
-- Strongest active object-less binding whose scope contains p_scope_key and
-- whose mask covers p_required_access. Returns user_claim_id, claim_id,
-- sa_access, scope.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_access(
    p_user_id          UUID,
    p_app_id           UUID,
    p_scope_key        TEXT,
    p_required_access  INTEGER DEFAULT 0
) RETURNS TABLE (
                    user_claim_id   UUID,
                    claim_id        UUID,
                    sa_access       INTEGER,
                    scope           JSONB
                ) AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT uc.id,
               c.id,
               (c.sa_access | co.sa_access) & 15,
               co.scope
        FROM claimius.user_claim uc
                 JOIN claimius.claim c ON c.id = uc.claim_id
                 JOIN claimius.claim_object co
                      ON co.claim_id = uc.claim_id
                     AND co.app_id = uc.app_id
        WHERE uc.user_id = p_user_id
          AND uc.app_id = p_app_id
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
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_access(UUID, UUID, TEXT, INTEGER) IS 'Strongest active object-less binding whose scope contains p_scope_key and whose mask covers p_required_access.';

-- ----------------------------------------------------------------------------
-- get_audit
-- Returns audit rows for organizations the user can see.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_audit(UUID, UUID, INTEGER, INTEGER, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_audit(
    p_user_id          UUID,
    p_app_id           UUID,
    p_required_access  INTEGER DEFAULT 0,
    p_limit            INTEGER DEFAULT 100,
    p_offset           INTEGER DEFAULT 0
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
            SELECT g.object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.organization', p_required_access) g
        )
        ORDER BY a.sa_created_at DESC
        LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_audit(p_user_id uuid, p_app_id uuid, p_required_access integer, p_limit integer, p_offset integer) IS 'Audit rows for organizations the user can see.';

-- ----------------------------------------------------------------------------
-- get_secrets
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_secrets(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_secrets(
    p_user_id          UUID,
    p_app_id           UUID,
    p_required_access  INTEGER DEFAULT 0
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
            SELECT g.object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.samna_secret', p_required_access) g
        );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_secrets(p_user_id uuid, p_app_id uuid, p_required_access integer) IS 'Secrets whose effective mask covers p_required_access.';

-- Drop any stale earlier signatures of the graph functions before redefining.
-- The previous version of these functions had different argument lists; if a
-- migration re-runs against a database that still has the old signature, the
-- bare CREATE OR REPLACE below would create a second overloaded version
-- instead of replacing, and downstream COMMENT/REVOKE statements would not
-- resolve unambiguously.
DROP FUNCTION IF EXISTS claimius.get_owner_graph(UUID, UUID, INTEGER);
DROP FUNCTION IF EXISTS claimius.get_claim_graph(UUID, UUID, INTEGER);

-- ----------------------------------------------------------------------------
-- get_owner_graph
-- Returns the ownership graph the user can see as a flat node list. Covers
-- organizations, locations, AND every registered application table that has
-- sa_owner_id and/or sa_location_id. Each row carries a single canonical
-- parent_id (priority: sa_parent_id, then sa_location_id, then sa_owner_id);
-- secondary relationships ride in the data jsonb so the frontend can draw
-- additional edges if it wants.
--
-- Args:
--   p_user_id   - reading user
--   p_app_id    - app scope
--   p_start_id  - optional organization id; when set, restricts the result to
--                 the subtree rooted at that org (and the org itself). NULL
--                 means every root the user can see.
--   p_depth     - how many levels of descent below the start org. 0 means
--                 unlimited. The org at p_start_id (or each accessible root
--                 when start is NULL) is depth 0; its direct children are
--                 depth 1; and so on.
--   p_required_access - access bit filter passed through to get_objects.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_owner_graph(UUID, UUID, UUID, INTEGER, INTEGER);
CREATE OR REPLACE FUNCTION claimius.get_owner_graph(
    p_user_id          UUID,
    p_app_id           UUID,
    p_start_id         UUID DEFAULT NULL,
    p_depth            INTEGER DEFAULT 0,
    p_required_access  INTEGER DEFAULT 0,
    p_width            INTEGER DEFAULT 0,
    p_sort             TEXT DEFAULT 'name',
    p_next_id          UUID DEFAULT NULL
) RETURNS TABLE(object_id UUID, object_type TEXT, label TEXT, parent_id UUID, data JSONB) AS $$
DECLARE
    v_root_ids      UUID[];
    v_table         RECORD;
    v_sql           TEXT;
    v_next_rank     BIGINT := 0;
    v_next_name     TEXT;
    v_next_type     TEXT;
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    CREATE TEMP TABLE IF NOT EXISTS _owner_graph_rows (
        object_id UUID, object_type TEXT, label TEXT, parent_id UUID, data JSONB
    ) ON COMMIT DROP;
    DELETE FROM _owner_graph_rows;

    -- Resolve the set of root organizations to walk from.
    -- When p_start_id is null we use every root org the user can see.
    -- When p_start_id is provided it must be an org the user can see; if not,
    -- we return empty silently (no rows yielded, no error).
    IF p_start_id IS NULL THEN
        SELECT array_agg(o.id) INTO v_root_ids
        FROM claimius.organization o
        WHERE o.id IN (SELECT claimius.app_object_ids(p_app_id, 'claimius.organization'))
          AND o.sa_deleted_at IS NULL
          AND o.sa_owner_id = o.id  -- root: self referential
          AND o.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.organization', p_required_access) g
        );
    ELSE
        SELECT ARRAY[o.id] INTO v_root_ids
        FROM claimius.organization o
        WHERE o.id IN (SELECT claimius.app_object_ids(p_app_id, 'claimius.organization'))
          AND o.id = p_start_id
          AND o.sa_deleted_at IS NULL
          AND o.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.organization', p_required_access) g
        );
    END IF;

    IF v_root_ids IS NULL OR array_length(v_root_ids, 1) IS NULL THEN
        RETURN;
    END IF;

    -- Organizations: walk each root's ownership tree, depth limited.
    -- Recursive CTE bound to the access filter and the depth limit.
    INSERT INTO _owner_graph_rows
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
              AND c.id <> c.sa_owner_id  -- don't re-walk the root
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
                               AND uo.app_id = p_app_id
        WHERE w.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.organization', p_required_access) g
        );

    -- For every other registered table that has sa_owner_id or sa_location_id,
    -- emit rows whose ownership chain lands inside the org subtree we walked.
    -- The walked org subtree is captured as the same recursive CTE above,
    -- materialized once into a temp set used by every per-table query.
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

    -- Locations: handled explicitly so we keep longitude/latitude in data.
    -- Filtered to locations whose owning org sits in the walked org set.
    INSERT INTO _owner_graph_rows
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
                               AND uo.app_id = p_app_id
        WHERE l.sa_deleted_at IS NULL
          AND l.sa_owner_id IN (SELECT org_id FROM _owner_graph_orgs)
          AND l.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.location', p_required_access) g
        );

    -- Iterate every registered table that participates in ownership/location
    -- and emit rows whose sa_owner_id resolves into the walked org set.
    -- Locations and other tables share the same general shape; we keep
    -- locations separate only because their parent_id falls back to
    -- sa_owner_id when sa_parent_id is self referential.
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
             AND uo.app_id = $2
             AND (uo.sa_access & $3) = $3
            WHERE %s
              AND (
                  %s
                  %s
              )
        $f$,
                            v_table.object_type,
                -- parent_id CASE arms in priority order: parent, location, owner
                            CASE WHEN v_table.has_sa_parent_id THEN 'WHEN t.sa_parent_id IS NOT NULL AND t.sa_parent_id <> t.id THEN t.sa_parent_id' ELSE '' END,
                            CASE WHEN v_table.has_sa_location_id THEN 'WHEN t.sa_location_id IS NOT NULL THEN t.sa_location_id' ELSE '' END,
                            CASE WHEN v_table.has_sa_owner_id THEN 'WHEN t.sa_owner_id IS NOT NULL AND t.sa_owner_id <> t.id THEN t.sa_owner_id' ELSE '' END,
                            split_part(v_table.object_type, '.', 1),
                            split_part(v_table.object_type, '.', 2),
                            v_table.object_type,
                -- soft-delete predicate (table dependent)
                            CASE WHEN v_table.has_sa_deleted_at THEN 't.sa_deleted_at IS NULL' ELSE 'TRUE' END,
                -- ancestry: must root in the walked org set (via owner or via owner of location)
                            CASE WHEN v_table.has_sa_owner_id THEN 't.sa_owner_id IN (SELECT org_id FROM _owner_graph_orgs)' ELSE 'FALSE' END,
                            CASE WHEN v_table.has_sa_location_id THEN ' OR EXISTS (SELECT 1 FROM claimius.location loc WHERE loc.id = t.sa_location_id AND loc.sa_owner_id IN (SELECT org_id FROM _owner_graph_orgs))' ELSE '' END
                     );

            EXECUTE 'INSERT INTO _owner_graph_rows ' || v_sql USING p_user_id, p_app_id, p_required_access;
        END LOOP;

    IF p_width <= 0 THEN
        RETURN QUERY SELECT r.object_id, r.object_type, r.label, r.parent_id, r.data FROM _owner_graph_rows r;
        RETURN;
    END IF;

    IF p_next_id IS NOT NULL AND p_start_id IS NOT NULL THEN
        SELECT coalesce(uo.sa_name, p_next_id::TEXT),
               CASE WHEN p_sort = 'usage' THEN -coalesce(ou.used_count, 0) ELSE 0 END,
               uo.object_type
          INTO v_next_name, v_next_rank, v_next_type
        FROM claimius.user_object uo
        LEFT JOIN claimius.object_usage ou
               ON ou.app_id = p_app_id AND ou.user_id = p_user_id
              AND ou.object_type = uo.object_type AND ou.object_id = uo.object_id
        WHERE uo.user_id = p_user_id AND uo.app_id = p_app_id AND uo.object_id = p_next_id
        LIMIT 1;
        IF v_next_name IS NULL THEN
            v_next_name := p_next_id::TEXT;
            v_next_rank := 0;
        END IF;
    END IF;

    RETURN QUERY
    WITH RECURSIVE ranked AS (
        SELECT r.object_id, r.object_type, r.parent_id,
               CASE WHEN p_sort = 'usage' THEN -coalesce(ou.used_count, 0) ELSE 0 END AS sort_rank,
               coalesce(r.label, r.object_id::TEXT) AS sort_name
        FROM _owner_graph_rows r
        LEFT JOIN claimius.object_usage ou
               ON p_sort = 'usage'
              AND ou.app_id = p_app_id AND ou.user_id = p_user_id
              AND ou.object_type = r.object_type AND ou.object_id = r.object_id
    ),
    windowed AS (
        SELECT rk.*,
               ROW_NUMBER() OVER (
                   PARTITION BY rk.parent_id, rk.object_type
                   ORDER BY rk.sort_rank, rk.sort_name, rk.object_id
               ) AS rn
        FROM ranked rk
        WHERE p_next_id IS NULL OR p_start_id IS NULL OR rk.parent_id IS DISTINCT FROM p_start_id
           OR rk.object_type IS DISTINCT FROM v_next_type
           OR ROW(rk.sort_rank, rk.sort_name, rk.object_id) > ROW(v_next_rank, v_next_name, p_next_id)
    ),
    kept AS (
        SELECT w.object_id, w.object_type
        FROM windowed w
        WHERE w.parent_id IS NULL
           OR NOT EXISTS (SELECT 1 FROM _owner_graph_rows p WHERE p.object_id = w.parent_id)
        UNION ALL
        SELECT c.object_id, c.object_type
        FROM windowed c
        JOIN kept k ON c.parent_id = k.object_id
        WHERE c.rn <= p_width
    )
    SELECT r.object_id, r.object_type, r.label, r.parent_id, r.data
    FROM _owner_graph_rows r
    WHERE EXISTS (SELECT 1 FROM kept k WHERE k.object_id = r.object_id AND k.object_type = r.object_type)
      AND NOT (p_next_id IS NOT NULL AND r.object_id = p_start_id);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.get_owner_graph(UUID, UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) IS 'Ownership graph (orgs, locations, registered tables with sa_owner_id or sa_location_id) for the user. Optional start org and depth limit.';

-- ----------------------------------------------------------------------------
-- get_claim_graph
-- Returns the access graph: the requesting user, every other user this user
-- can see, every claim the user holds (or just the start claim), the
-- user_claim grants, the claim_object connector nodes that bind claims to
-- objects, and the actual objects reachable from those bindings (every
-- registered object_type, served via the user_object materialization).
--
-- Topology:
--   user (parent_id = NULL)
--     |- user_claim   (parent_id = user.id)
--     |     '- (the user_claim points at a claim by claim_id in data)
--     '- (other users seen via user_users; parent_id = the requesting user.id)
--   claim (parent_id = NULL)
--     '- claim_object   (parent_id = claim.id)
--           '- object   (parent_id = claim_object.id)
--
-- Args:
--   p_user_id   - reading user
--   p_app_id    - app scope
--   p_start_id  - optional claim id; when set, restricts the walk to that
--                 single claim (must be one the user holds, otherwise empty).
--                 NULL means every claim the user holds.
--   p_depth     - cascade depth from each claim_object binding. 0 = unlimited
--                 (all objects in user_object reachable through the binding).
--                 1 = directly bound objects only, no cascade descendants.
--                 N = directly bound plus N-1 cascade hops.
--   p_required_access - bit filter; claims and reachable objects whose
--                       effective mask does not cover these bits are excluded.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_claim_graph(UUID, UUID, UUID, INTEGER, INTEGER);
CREATE OR REPLACE FUNCTION claimius.get_claim_graph(
    p_user_id          UUID,
    p_app_id           UUID,
    p_start_id         UUID DEFAULT NULL,
    p_depth            INTEGER DEFAULT 0,
    p_required_access  INTEGER DEFAULT 0,
    p_width            INTEGER DEFAULT 0,
    p_sort             TEXT DEFAULT 'name',
    p_next_id          UUID DEFAULT NULL
) RETURNS TABLE(object_id UUID, object_type TEXT, label TEXT, parent_id UUID, data JSONB) AS $$
DECLARE
    v_claim_ids     UUID[];
    v_user_row_id   UUID;
    v_cacheable     BOOLEAN;
    v_graph         JSONB;
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    v_cacheable := (p_start_id IS NULL AND p_depth = 0 AND p_required_access = 0);

    IF v_cacheable THEN
        SELECT c.graph INTO v_graph
        FROM claimius.claim_graph_cache c
        WHERE c.app_id = p_app_id AND c.user_id = p_user_id;

        IF v_graph IS NOT NULL THEN
            IF p_width > 0 THEN
                RETURN QUERY SELECT * FROM claimius._prune_graph_rows(v_graph, p_user_id, p_app_id, p_start_id, p_width, p_sort, p_next_id);
                RETURN;
            END IF;
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
          AND uc.app_id = p_app_id
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
          AND (p_required_access = 0 OR (c.sa_access & p_required_access) = p_required_access);
    ELSE
        SELECT array_agg(c.id) INTO v_claim_ids
        FROM claimius.claim c
        WHERE c.id = p_start_id
          AND c.app_id = p_app_id
          AND c.sa_deleted_at IS NULL
          AND (p_required_access = 0 OR (c.sa_access & p_required_access) = p_required_access);
    END IF;

    IF v_claim_ids IS NULL OR array_length(v_claim_ids, 1) IS NULL THEN
        RETURN;
    END IF;

    SELECT u.id INTO v_user_row_id
    FROM claimius.samna_user u
    WHERE u.user_id = p_user_id AND u.app_id = p_app_id
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
        WHERE p_start_id IS NULL
          AND uo.user_id = p_user_id
          AND uo.app_id = p_app_id
          AND (p_required_access = 0 OR (uo.sa_access & p_required_access) = p_required_access)
          AND (e->>'claim_id')::UUID = ANY(v_claim_ids)
          AND (p_depth = 0 OR e->>'tree_type' = 'direct')
    ),
    reach AS (
        SELECT DISTINCT ON (co.id, ii.descendant_type, ii.descendant_id)
               co.id                AS co_id,
               ii.descendant_type   AS object_type,
               ii.descendant_id     AS object_id,
               ii.tree_type::TEXT   AS tree_type,
               ii.depth             AS depth,
               coalesce(co.sa_access, c.sa_access) AS access,
               co.scope             AS scope
        FROM claimius.claim_object co
                 JOIN claimius.claim c ON c.id = co.claim_id AND c.sa_deleted_at IS NULL
                 JOIN claimius.inheritance_info ii
                      ON ii.ancestor_type = co.object_type
                          AND ii.ancestor_id = co.object_id
                          AND (ii.depth = 0 OR (c.inherits AND co.inherits AND (p_depth = 0 OR ii.depth <= p_depth)))
        WHERE p_start_id IS NOT NULL
          AND co.app_id = p_app_id
          AND co.sa_deleted_at IS NULL
          AND co.object_id IS NOT NULL
          AND co.object_type <> 'claimius.claim'
          AND co.claim_id = ANY(v_claim_ids)
        ORDER BY co.id, ii.descendant_type, ii.descendant_id, ii.depth
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
          AND p_start_id IS NULL
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
                          AND uu.app_id = p_app_id
                          AND uu.target_user_id = u.user_id
                 LEFT JOIN claimius.user_object uo
                      ON uo.object_id = u.id
                          AND uo.object_type = 'claimius.samna_user'
                          AND uo.user_id = p_user_id
                          AND uo.app_id = p_app_id
        WHERE u.app_id = p_app_id
          AND u.sa_deleted_at IS NULL
          AND u.id <> v_user_row_id
          AND p_start_id IS NULL
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
          AND uc.app_id = p_app_id
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
        WHERE c.app_id = p_app_id
          AND c.sa_deleted_at IS NULL
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
                          AND uo.app_id = p_app_id
        WHERE co.app_id = p_app_id
          AND co.sa_deleted_at IS NULL
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
                      ON co.app_id = p_app_id
                          AND co.claim_id = ga.claim_id
                          AND co.object_type = ga.anchor_type
                          AND co.object_id = ga.anchor_id
                          AND co.sa_deleted_at IS NULL
        UNION ALL
        SELECT r.object_id            AS object_id,
               r.object_type          AS object_type,
               coalesce(uo.sa_name, r.object_id::TEXT)::TEXT  AS label,
               r.co_id                AS parent_id,
               jsonb_build_object(
                   'access', r.access & 15,
                   'deny', (r.access & 16) <> 0,
                   'scope', r.scope,
                   'level', r.depth,
                   'tree_type', r.tree_type
               )                      AS data
        FROM reach r
                 LEFT JOIN claimius.user_object uo
                      ON uo.object_id = r.object_id
                          AND uo.object_type = r.object_type
                          AND uo.user_id = p_user_id
                          AND uo.app_id = p_app_id
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
        INSERT INTO claimius.claim_graph_cache (app_id, user_id, graph, sa_updated_at)
        VALUES (p_app_id, p_user_id, v_graph, now())
        ON CONFLICT (app_id, user_id) DO UPDATE
            SET graph = EXCLUDED.graph,
                sa_updated_at = EXCLUDED.sa_updated_at;
    END IF;

    IF p_width > 0 THEN
        RETURN QUERY SELECT * FROM claimius._prune_graph_rows(v_graph, p_user_id, p_app_id, p_start_id, p_width, p_sort, p_next_id);
        RETURN;
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

COMMENT ON FUNCTION claimius.get_claim_graph(UUID, UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) IS 'Access graph: user, other users seen, claims, user_claims, claim_object bindings, reachable objects. Optional start claim and depth limit. Default shape served from claimius.claim_graph_cache.';

CREATE OR REPLACE FUNCTION claimius._invalidate_claim_graph_cache()
    RETURNS TRIGGER AS $$
DECLARE
    v_app_id    UUID;
    v_user_id   UUID;
BEGIN
    IF TG_TABLE_NAME = 'user_object' THEN
        v_app_id  := COALESCE(NEW.app_id, OLD.app_id);
        v_user_id := COALESCE(NEW.user_id, OLD.user_id);
        DELETE FROM claimius.claim_graph_cache
        WHERE app_id = v_app_id AND user_id = v_user_id;
    ELSIF TG_TABLE_NAME = 'user_users' THEN
        v_app_id  := COALESCE(NEW.app_id, OLD.app_id);
        v_user_id := COALESCE(NEW.viewer_id, OLD.viewer_id);
        DELETE FROM claimius.claim_graph_cache
        WHERE app_id = v_app_id AND user_id = v_user_id;
    ELSIF TG_TABLE_NAME = 'samna_user' THEN
        v_app_id  := COALESCE(NEW.app_id, OLD.app_id);
        v_user_id := COALESCE(NEW.user_id, OLD.user_id);
        DELETE FROM claimius.claim_graph_cache
        WHERE app_id = v_app_id AND user_id = v_user_id;
    ELSIF TG_TABLE_NAME = 'claim' OR TG_TABLE_NAME = 'claim_object' THEN
        v_app_id := COALESCE(NEW.app_id, OLD.app_id);
        DELETE FROM claimius.claim_graph_cache
        WHERE app_id = v_app_id;
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._invalidate_claim_graph_cache() IS 'Trigger function. Drops claim_graph_cache rows affected by writes to user_object, user_users, samna_user, claim, claim_object.';

-- ----------------------------------------------------------------------------
-- search
-- Full text search over accessible objects (anything with a user_object row),
-- gated by access. Matches against the precomputed search_vector column.
-- The search_vector is fed by sa_name, sa_description, sa_link via the
-- denormalization in V2.2.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.search_objects(UUID, UUID, TEXT, TEXT[], INTEGER, INTEGER, INTEGER);
DROP FUNCTION IF EXISTS claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, UUID[], UUID[], INTEGER, INTEGER);

CREATE OR REPLACE FUNCTION claimius.search(
    p_user_id          UUID,
    p_app_id           UUID,
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
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    v_tsquery := websearch_to_tsquery('simple', p_query);

    RETURN QUERY
        SELECT
            claimius.user_object.object_id,
            claimius.user_object.object_type,
            best.user_claim_id,
            claimius.user_object.sa_name,
            claimius.user_object.sa_description,
            claimius.user_object.sa_link,
            claimius.user_object.sa_access,
            claimius.user_object.scope,
            claimius.user_object.sa_owner_id,
            claimius.user_object.sa_location_id,
            (claimius.user_object.direct_grant AND best.user_claim_id IS NULL),
            ts_rank(claimius.user_object.search_vector, v_tsquery)
        FROM claimius.user_object
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_user_id, p_app_id, p_required_access) best ON TRUE
        WHERE claimius.user_object.user_id = p_user_id
          AND claimius.user_object.app_id = p_app_id
          AND (claimius.user_object.sa_access & p_required_access) = p_required_access
          AND claimius.user_object.search_vector @@ v_tsquery
          AND (p_object_types IS NULL OR claimius.user_object.object_type = ANY(p_object_types))
          AND (best.user_claim_id IS NOT NULL OR claimius.user_object.direct_grant)
        ORDER BY ts_rank(claimius.user_object.search_vector, v_tsquery) DESC
        LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, INTEGER, INTEGER) IS 'Full text search over accessible objects whose mask covers p_required_access. user_claim_id is the actor token from the strongest surviving claim grant per object.';

CREATE OR REPLACE FUNCTION claimius.search(
    p_user_id            UUID,
    p_app_id             UUID,
    p_query              TEXT,
    p_object_types       TEXT[] DEFAULT NULL,
    p_required_access    INTEGER DEFAULT 0,
    p_object_ids         UUID[] DEFAULT NULL,
    p_exclude_object_ids UUID[] DEFAULT NULL,
    p_owner_subtree_ids  UUID[] DEFAULT NULL,
    p_parent_id          UUID   DEFAULT NULL,
    p_parent_type        TEXT   DEFAULT NULL,
    p_type_ids           UUID[] DEFAULT NULL,
    p_limit              INTEGER DEFAULT 20,
    p_offset             INTEGER DEFAULT 0
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
                   rank                REAL,
                   total_count         INTEGER
               ) AS $$
DECLARE
    v_tsquery        TSQUERY;
    v_include_claim  BOOLEAN := p_object_types IS NULL OR 'claimius.claim' = ANY(p_object_types);
    v_include_others BOOLEAN := p_object_types IS NULL OR EXISTS (
        SELECT 1 FROM unnest(p_object_types) t WHERE t <> 'claimius.claim'
    );
    v_has_scope      BOOLEAN := p_owner_subtree_ids IS NOT NULL
                              OR p_parent_id IS NOT NULL
                              OR p_type_ids IS NOT NULL;
    v_parent_tree    claimius.tree_type;
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    v_tsquery := websearch_to_tsquery('simple', p_query);

    IF p_parent_type IS NOT NULL THEN
        v_parent_tree := CASE p_parent_type
                            WHEN 'claimius.location' THEN 'location'::claimius.tree_type
                            WHEN 'public.bookable'   THEN 'parenthood'::claimius.tree_type
                         END;
    END IF;

    RETURN QUERY
    WITH combined AS (
        SELECT
            claimius.user_object.object_id           AS object_id,
            claimius.user_object.object_type         AS object_type,
            best.user_claim_id                       AS user_claim_id,
            claimius.user_object.sa_name             AS name,
            claimius.user_object.sa_description      AS description,
            claimius.user_object.sa_link             AS link,
            claimius.user_object.sa_access           AS sa_access,
            claimius.user_object.scope               AS scope,
            claimius.user_object.sa_owner_id         AS owner_id,
            claimius.user_object.sa_location_id      AS location_id,
            (claimius.user_object.direct_grant AND best.user_claim_id IS NULL) AS is_direct_grant,
            ts_rank(claimius.user_object.search_vector, v_tsquery)     AS rank
        FROM claimius.user_object
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_user_id, p_app_id, p_required_access) best ON TRUE
        WHERE v_include_others
          AND claimius.user_object.user_id = p_user_id
          AND claimius.user_object.app_id = p_app_id
          AND (claimius.user_object.sa_access & p_required_access) = p_required_access
          AND claimius.user_object.search_vector @@ v_tsquery
          AND (p_object_types IS NULL OR claimius.user_object.object_type = ANY(p_object_types))
          AND (p_object_ids IS NULL OR claimius.user_object.object_id = ANY(p_object_ids))
          AND (p_exclude_object_ids IS NULL OR NOT (claimius.user_object.object_id = ANY(p_exclude_object_ids)))
          AND (p_owner_subtree_ids IS NULL OR EXISTS (
              SELECT 1 FROM claimius.inheritance_info ii
              WHERE ii.tree_type       = 'ownership'
                AND ii.ancestor_type   = 'claimius.organization'
                AND ii.ancestor_id     = ANY(p_owner_subtree_ids)
                AND ii.descendant_type = claimius.user_object.object_type
                AND ii.descendant_id   = claimius.user_object.object_id
          ))
          AND (p_parent_id IS NULL OR v_parent_tree IS NULL OR EXISTS (
              SELECT 1 FROM claimius.inheritance_info ii
              WHERE ii.tree_type       = v_parent_tree
                AND ii.ancestor_type   = p_parent_type
                AND ii.ancestor_id     = p_parent_id
                AND ii.descendant_type = claimius.user_object.object_type
                AND ii.descendant_id   = claimius.user_object.object_id
          ))
          AND (p_type_ids IS NULL OR (
              claimius.user_object.object_type = 'public.bookable'
              AND EXISTS (
                  SELECT 1 FROM public.bookable b
                  WHERE b.id = claimius.user_object.object_id
                    AND b.type_id = ANY(p_type_ids)
                    AND b.sa_deleted_at IS NULL
              )
          ))
          AND (best.user_claim_id IS NOT NULL OR claimius.user_object.direct_grant)

        UNION ALL

        SELECT
            c.id                                                       AS object_id,
            'claimius.claim'::TEXT                                     AS object_type,
            uc.id                                                      AS user_claim_id,
            c.name                                                     AS name,
            c.description                                              AS description,
            NULL::TEXT                                                 AS link,
            c.sa_access                                                AS sa_access,
            NULL::JSONB                                                AS scope,
            c.sa_owner_id                                              AS owner_id,
            NULL::UUID                                                 AS location_id,
            TRUE                                                       AS is_direct_grant,
            ts_rank(to_tsvector('simple', coalesce(c.name, '') || ' ' || coalesce(c.description, '')), v_tsquery) AS rank
        FROM claimius.claim c
                 JOIN claimius.user_claim uc ON uc.claim_id = c.id
            AND uc.user_id = p_user_id
            AND uc.app_id = p_app_id
            AND uc.sa_deleted_at IS NULL
            AND (uc.starts_at IS NULL OR uc.starts_at <= now())
            AND (uc.ends_at IS NULL OR uc.ends_at > now())
        WHERE v_include_claim
          AND NOT v_has_scope
          AND c.app_id = p_app_id
          AND c.sa_deleted_at IS NULL
          AND (c.sa_access & p_required_access) = p_required_access
          AND to_tsvector('simple', coalesce(c.name, '') || ' ' || coalesce(c.description, '')) @@ v_tsquery
          AND (p_object_ids IS NULL OR c.id = ANY(p_object_ids))
          AND (p_exclude_object_ids IS NULL OR NOT (c.id = ANY(p_exclude_object_ids)))
    )
    SELECT combined.object_id,
           combined.object_type,
           combined.user_claim_id,
           combined.name,
           combined.description,
           combined.link,
           combined.sa_access,
           combined.scope,
           combined.owner_id,
           combined.location_id,
           combined.is_direct_grant,
           combined.rank,
           COUNT(*) OVER ()::INTEGER AS total_count
    FROM combined
    ORDER BY combined.rank DESC, combined.object_id ASC
    LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, UUID[], UUID[], UUID[], UUID, TEXT, UUID[], INTEGER, INTEGER) IS 'Full text search over accessible objects. Includes claimius.claim rows the user holds, skipped when any subtree, parent, or type filter is set. p_owner_subtree_ids scopes via ownership tree. p_parent_id and p_parent_type scope via location or parenthood tree. p_type_ids restricts bookable rows to those whose type_id is in the set. total_count carries the un-capped match count for pagination. Rows tie-break on object_id so offset pagination tiles without overlap or skip.';

GRANT EXECUTE ON FUNCTION claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, INTEGER, INTEGER) TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, UUID[], UUID[], UUID[], UUID, TEXT, UUID[], INTEGER, INTEGER) TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

-- ============================================================================
-- Write functions: claim management
-- ============================================================================

-- create_claim
DROP FUNCTION IF EXISTS claimius.create_claim(UUID, TEXT, TEXT, INTEGER, UUID, UUID, UUID, BOOLEAN, BOOLEAN, claimius.claim_type);
DROP FUNCTION IF EXISTS claimius.create_claim(UUID, TEXT, TEXT, INTEGER, UUID, UUID, UUID, BOOLEAN, claimius.claim_type);

CREATE OR REPLACE FUNCTION claimius.create_claim(
    p_app_id            UUID,
    p_name              TEXT,
    p_description       TEXT,
    p_sa_access         INTEGER,
    p_sa_owner_id       UUID,
    p_sa_root_id        UUID,
    p_sa_created_by     UUID,
    p_inherits          BOOLEAN DEFAULT TRUE,
    p_type              claimius.claim_type DEFAULT 'user'
) RETURNS JSONB AS $$
DECLARE
    v_row    claimius.claim;
    v_access INTEGER := p_sa_access;
BEGIN
    IF (v_access & 16) = 0 THEN
        IF (v_access & 1) = 1 THEN
            v_access := v_access | 14;
        END IF;
        IF (v_access & 2) = 2 THEN
            v_access := v_access | 12;
        END IF;
        IF (v_access & 8) = 8 THEN
            v_access := v_access | 4;
        END IF;
    END IF;

    INSERT INTO claimius.claim (
        app_id, name, description, sa_access, inherits, type,
        sa_owner_id, sa_root_id, sa_created_by
    ) VALUES (
                 p_app_id, p_name, p_description, v_access, p_inherits, p_type,
                 p_sa_owner_id, p_sa_root_id, p_sa_created_by
             ) RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.create_claim(p_app_id uuid, p_name text, p_description text, p_sa_access integer, p_sa_owner_id uuid, p_sa_root_id uuid, p_sa_created_by uuid, p_inherits boolean, p_type claimius.claim_type) IS 'Creates a new claim. p_sa_access is a bitwise mask: 0x01 owner, 0x02 write, 0x04 read, 0x08 execute, 0x10 deny, 0x20 member. On grant claims with the deny bit unset owner implies write+read+execute, write implies read+execute, execute implies read. Member bit 0x20 stands alone. Deny claims store the supplied bits verbatim so callers can deny any subset of read, write, execute, owner.';

-- update_claim
DROP FUNCTION IF EXISTS claimius.update_claim(UUID, TEXT, TEXT, INTEGER, BOOLEAN, BOOLEAN);
DROP FUNCTION IF EXISTS claimius.update_claim(UUID, TEXT, TEXT, INTEGER, BOOLEAN);

CREATE OR REPLACE FUNCTION claimius.update_claim(
    p_claim_id          UUID,
    p_name              TEXT DEFAULT NULL,
    p_description       TEXT DEFAULT NULL,
    p_sa_access         INTEGER DEFAULT NULL,
    p_inherits          BOOLEAN DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row    claimius.claim;
    v_access INTEGER := p_sa_access;
BEGIN
    IF v_access IS NOT NULL AND (v_access & 16) = 0 THEN
        IF (v_access & 1) = 1 THEN
            v_access := v_access | 14;
        END IF;
        IF (v_access & 2) = 2 THEN
            v_access := v_access | 12;
        END IF;
        IF (v_access & 8) = 8 THEN
            v_access := v_access | 4;
        END IF;
    END IF;

    UPDATE claimius.claim SET
                              name        = coalesce(NULLIF(p_name, ''), name),
                              description = coalesce(p_description, description),
                              sa_access   = coalesce(NULLIF(v_access, 0), sa_access),
                              inherits    = coalesce(p_inherits, inherits)
    WHERE id = p_claim_id
    RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_claim(p_claim_id uuid, p_name text, p_description text, p_sa_access integer, p_inherits boolean) IS 'Patches selected fields on a claim. p_sa_access is expanded on grant claims so owner implies write+read+execute, write implies read+execute, execute implies read. Member bit 0x20 stands alone. Deny claims keep the supplied bits verbatim. Empty p_name and zero p_sa_access are treated as preserve so PATCH callers that send only one field do not wipe the others.';

-- remove_claim
DROP FUNCTION IF EXISTS claimius.remove_claim(UUID, UUID);

CREATE OR REPLACE FUNCTION claimius.remove_claim(
    p_claim_id   UUID,
    p_deleted_by UUID
) RETURNS SETOF claimius.claim AS $$
BEGIN
    UPDATE claimius.claim_object
       SET sa_deleted_at = now()
     WHERE object_type   = 'claimius.claim'
       AND object_id     = p_claim_id
       AND sa_deleted_at IS NULL;

    UPDATE claimius.user_claim SET sa_deleted_at = now() WHERE claim_id = p_claim_id AND sa_deleted_at IS NULL;
    UPDATE claimius.claim_object SET sa_deleted_at = now() WHERE claim_id = p_claim_id AND sa_deleted_at IS NULL;
    RETURN QUERY
    UPDATE claimius.claim SET sa_deleted_at = now()
    WHERE id = p_claim_id AND sa_deleted_at IS NULL
    RETURNING *;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.remove_claim(p_claim_id uuid, p_deleted_by uuid) IS 'Soft deletes a claim, its user_claim, its claim_object, and any inbound composition links pointing at it. Returns the deleted claim row.';

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

COMMENT ON FUNCTION claimius.assign_claim_user(p_app_id uuid, p_claim_id uuid, p_user_id uuid, p_sa_owner_id uuid, p_sa_created_by uuid, p_reason text, p_starts_at timestamp with time zone, p_ends_at timestamp with time zone) IS 'Grants a claim to a user in an app.';

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

COMMENT ON FUNCTION claimius.update_claim_user(p_user_claim_id uuid, p_reason text, p_starts_at timestamp with time zone, p_ends_at timestamp with time zone) IS 'Patches selected fields on a user_claim.';

-- remove_user_claim
DROP FUNCTION IF EXISTS claimius.remove_user_claim(UUID, UUID);

CREATE OR REPLACE FUNCTION claimius.remove_user_claim(
    p_user_claim_id UUID,
    p_deleted_by    UUID
) RETURNS SETOF claimius.user_claim AS $$
BEGIN
    RETURN QUERY
    UPDATE claimius.user_claim SET sa_deleted_at = now()
    WHERE id = p_user_claim_id AND sa_deleted_at IS NULL
    RETURNING *;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.remove_user_claim(p_user_claim_id uuid, p_deleted_by uuid) IS 'Soft deletes a user_claim row. Returns the deleted user_claim row.';

-- assign_claim_object
DROP FUNCTION IF EXISTS claimius.assign_claim_object(UUID, UUID, UUID, TEXT, UUID, UUID, UUID, INTEGER, BOOLEAN, TEXT, UUID, TEXT);

CREATE OR REPLACE FUNCTION claimius.assign_claim_object(
    p_app_id        UUID,
    p_claim_id      UUID,
    p_sa_owner_id   UUID,
    p_sa_root_id    UUID,
    p_sa_created_by UUID,
    p_object_id     UUID DEFAULT NULL,
    p_object_type   TEXT DEFAULT NULL,
    p_sa_access     INTEGER DEFAULT NULL,
    p_inherits      BOOLEAN DEFAULT TRUE,
    p_reason        TEXT DEFAULT NULL,
    p_ref_id        UUID DEFAULT NULL,
    p_scope         JSONB DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row claimius.claim_object;
BEGIN
    INSERT INTO claimius.claim_object (
        app_id, claim_id, object_id, object_type, reason, scope,
        sa_access, inherits, ref_id, sa_owner_id, sa_root_id, sa_created_by
    ) VALUES (
                 p_app_id, p_claim_id, p_object_id, p_object_type, p_reason, p_scope,
                 p_sa_access, p_inherits, p_ref_id, p_sa_owner_id, p_sa_root_id, p_sa_created_by
             ) RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim_object', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.assign_claim_object(p_app_id uuid, p_claim_id uuid, p_sa_owner_id uuid, p_sa_root_id uuid, p_sa_created_by uuid, p_object_id uuid, p_object_type text, p_sa_access integer, p_inherits boolean, p_reason text, p_ref_id uuid, p_scope jsonb) IS 'Binds a claim. With (object_type, object_id) p_scope keys are field names; without them p_scope keys are custom. p_sa_access is a bitwise mask.';

-- update_claim_object
DROP FUNCTION IF EXISTS claimius.update_claim_object(UUID, TEXT, BOOLEAN);

CREATE OR REPLACE FUNCTION claimius.update_claim_object(
    p_claim_object_id UUID,
    p_reason        TEXT DEFAULT NULL,
    p_inherits      BOOLEAN DEFAULT NULL,
    p_sa_access     INTEGER DEFAULT NULL,
    p_scope         JSONB DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row claimius.claim_object;
BEGIN
    UPDATE claimius.claim_object SET
                                     reason    = coalesce(p_reason, reason),
                                     inherits  = coalesce(p_inherits, inherits),
                                     sa_access = coalesce(p_sa_access, sa_access),
                                     scope     = coalesce(p_scope, scope)
    WHERE id = p_claim_object_id
    RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim_object', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_claim_object(p_claim_object_id uuid, p_reason text, p_inherits boolean, p_sa_access integer, p_scope jsonb) IS 'Patches selected fields on a claim_object.';

-- remove_claim_object
DROP FUNCTION IF EXISTS claimius.remove_claim_object(UUID, UUID);

CREATE OR REPLACE FUNCTION claimius.remove_claim_object(
    p_claim_object_id UUID,
    p_deleted_by      UUID
) RETURNS SETOF claimius.claim_object AS $$
BEGIN
    RETURN QUERY
    UPDATE claimius.claim_object SET sa_deleted_at = now()
    WHERE id = p_claim_object_id AND sa_deleted_at IS NULL
    RETURNING *;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.remove_claim_object(p_claim_object_id uuid, p_deleted_by uuid) IS 'Soft deletes a claim_object row. Returns the deleted claim_object row.';

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
                -- external_id: source value wins when set; source NULL leaves
                -- the target's value intact.
                UPDATE claimius.samna_user
                SET first_name    = v_source_app_user.first_name,
                    last_name     = v_source_app_user.last_name,
                    user_name     = v_source_app_user.user_name,
                    phone         = v_source_app_user.phone,
                    config        = v_source_app_user.config,
                    external_id   = coalesce(v_source_app_user.external_id, external_id),
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

COMMENT ON FUNCTION claimius.merge_user(p_target_user_id uuid, p_source_user_id uuid, p_acting_user_claim uuid) IS 'Reassigns all data from source user_id to target user_id across apps.';

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

COMMENT ON FUNCTION claimius.migrate_root(p_object_type text, p_object_id uuid, p_new_root_id uuid) IS 'Admin function: cross root migration. Use only when absolutely required.';

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
                PERFORM claimius._detach_self_root_trigger(v_old_object_type);
                DELETE FROM claimius.table_info WHERE object_type = v_old_object_type;
                DELETE FROM claimius.inheritance_info
                 WHERE ancestor_type = v_old_object_type
                    OR descendant_type = v_old_object_type;
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
            PERFORM claimius._attach_self_root_trigger(v_object_type);
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

COMMENT ON FUNCTION claimius.init_claimius_tables(VARIADIC p_object_types text[]) IS 'Registers external tables. Idempotent. Called by implementer migrations.';

-- _bulk_seed_user_object
-- For a registered table with existing rows, populates user_object,
-- object_users, and user_users for every (user, object) pair where access
-- exists. Inherits the caller's replay_mode setting; callers run this with
-- replay_mode = true so emit_sync_event short circuits and the bulk seed
-- produces no sync events. Per-row cost is bounded because the helpers
-- _affected_users_for_object and _build_grants_for_object both use indexed
-- joins against the inheritance_info closure table.
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

    v_sql := format(
            'SELECT r.id, a AS app_id FROM %I.%I r CROSS JOIN LATERAL claimius._app_ids_for_row(%L, to_jsonb(r)) a WHERE r.sa_deleted_at IS NULL',
            v_schema, v_table, p_object_type);
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

    PERFORM claimius._refresh_user_users(s.app_id, s.user_id)
    FROM (
        SELECT DISTINCT app_id, user_id
        FROM claimius.object_users
        WHERE object_type = p_object_type
    ) s;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._bulk_seed_user_object(p_object_type text) IS 'Seeds user_object for an existing table at registration time.';

-- ----------------------------------------------------------------------------
-- recompute_state
-- Rebuilds inheritance_info trees and user_object materialization for every
-- registered table from live data. Idempotent. Use after a bulk write that
-- ran with claimius.replay_mode = 'true', or any time materialization needs
-- to be reconciled with current rows.
--
-- Self contained: reads the registry from claimius.table_info; takes no
-- arguments; assumes init_claimius_internal() and init_claimius_tables(...)
-- have already registered the relevant tables. If table_info is empty the
-- function is a no-op.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius.recompute_state()
    RETURNS VOID AS $$
DECLARE
    v_root    UUID;
    v_table   RECORD;
    v_sql     TEXT;
    v_id      UUID;
BEGIN
    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
    BEGIN
        FOR v_root IN
            SELECT id FROM claimius.organization
            WHERE sa_owner_id = id AND sa_deleted_at IS NULL
            LOOP
                PERFORM claimius.build_ownership_tree(v_root);
            END LOOP;

        FOR v_root IN
            SELECT id FROM claimius.location
            WHERE sa_parent_id = id AND sa_deleted_at IS NULL
            LOOP
                PERFORM claimius.build_location_tree(v_root);
            END LOOP;

        FOR v_table IN
            SELECT object_type, schema_name, table_name
            FROM claimius.table_info
            WHERE has_sa_parent_id
              AND has_sa_root_id
              AND object_type NOT IN ('claimius.organization', 'claimius.location')
            LOOP
                v_sql := format(
                        'SELECT id FROM %I.%I WHERE sa_parent_id = id AND sa_deleted_at IS NULL',
                        v_table.schema_name, v_table.table_name
                         );
                FOR v_id IN EXECUTE v_sql LOOP
                        PERFORM claimius.build_parenthood_tree(v_table.object_type, v_id);
                    END LOOP;
            END LOOP;

        FOR v_table IN
            SELECT ti.object_type
            FROM claimius.table_info ti
            WHERE ti.has_sa_deleted_at
              AND EXISTS (
                SELECT 1 FROM claimius.claim_object co
                WHERE co.object_type = ti.object_type
                  AND co.sa_deleted_at IS NULL
            )
            LOOP
                PERFORM claimius._bulk_seed_user_object(v_table.object_type);
            END LOOP;
    EXCEPTION WHEN OTHERS THEN
        PERFORM set_config('claimius.replay_mode', 'false', TRUE);
        RAISE;
    END;
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.recompute_state() IS 'Rebuilds inheritance_info and user_object for every registered table from live data. Idempotent. Run after bulk writes that bypassed calc triggers.';

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
        'claimius.user_relation',
        'claimius.user_token'
        ];
    v_t TEXT;
BEGIN
    FOREACH v_t IN ARRAY v_internal_tables LOOP
            PERFORM claimius._register_table_info(v_t, TRUE);
        END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.init_claimius_internal() IS 'Registers internal claimius.* tables in table_info. Called once at schema bootstrap.';

-- ----------------------------------------------------------------------------
-- create_root_organization
-- Creates a new root organization (one with no parent in the ownership
-- tree). Two phase self reference is handled internally so callers do not
-- need to know the convention.
--
-- Returns the inserted row as jsonb keyed by table name. Idempotent on
-- (app_id, name): if a non deleted org with the same name already exists in
-- the app, returns that row instead of creating a new one.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.create_root_organization(
    p_app_id            UUID,
    p_name              TEXT,
    p_actor_user_claim_id UUID,
    p_description       TEXT DEFAULT NULL,
    p_type              TEXT DEFAULT 'standard',
    p_id                UUID DEFAULT NULL,
    p_external_id       TEXT DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row   claimius.organization;
BEGIN
    p_id := claimius.ensure_uuid_null(p_id);

    -- Idempotency: existing root org with same name linked to this app?
    SELECT * INTO v_row
    FROM claimius.organization
    WHERE name = p_name
      AND sa_deleted_at IS NULL
      AND sa_owner_id = id
      AND claimius.app_has_object(p_app_id, 'claimius.organization', id)
    LIMIT 1;

    IF v_row.id IS NOT NULL THEN
        RETURN jsonb_build_object('organization', to_jsonb(v_row));
    END IF;

    INSERT INTO claimius.organization (
        id, name, description, type, external_id,
        sa_level, sa_created_by
    ) VALUES (
                 coalesce(p_id, gen_random_uuid()), p_name, p_description, p_type, p_external_id,
                 0, p_actor_user_claim_id
             )
    RETURNING * INTO v_row;

    PERFORM claimius.link_app_object(p_app_id, 'claimius.organization', v_row.id, p_actor_user_claim_id);

    RETURN jsonb_build_object('organization', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.create_root_organization(UUID, TEXT, UUID, TEXT, TEXT, UUID, TEXT) IS 'Creates a new root organization with self referencing sa_owner_id and sa_root_id, linked to the app through app_object. Idempotent on (app, name).';

-- ----------------------------------------------------------------------------
-- link_app_organization
-- Links an app to an existing organization through app_object. With
-- p_cascade the link extends to every organization and location in the
-- org's ownership tree, read from inheritance_info. Additive and
-- idempotent: existing links stay untouched, nothing is ever unlinked.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.link_app_organization(
    p_app_id    UUID,
    p_org_id    UUID,
    p_cascade   BOOLEAN DEFAULT TRUE,
    p_linked_by UUID DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_org      claimius.organization;
    v_linked   INTEGER := 0;
    v_desc     RECORD;
BEGIN
    SELECT * INTO v_org
    FROM claimius.organization
    WHERE id = p_org_id AND sa_deleted_at IS NULL;

    IF v_org.id IS NULL THEN
        RAISE EXCEPTION 'Organization % not found', p_org_id;
    END IF;

    PERFORM claimius.link_app_object(p_app_id, 'claimius.organization', v_org.id, p_linked_by);
    v_linked := v_linked + 1;

    IF p_cascade THEN
        FOR v_desc IN
            SELECT DISTINCT e.descendant_type, e.descendant_id
            FROM claimius.inheritance_info e
            WHERE e.tree_type = 'ownership'
              AND e.ancestor_type = 'claimius.organization'
              AND e.ancestor_id = v_org.id
              AND e.depth > 0
              AND e.descendant_type IN ('claimius.organization', 'claimius.location')
            LOOP
                PERFORM claimius.link_app_object(p_app_id, v_desc.descendant_type, v_desc.descendant_id, p_linked_by);
                v_linked := v_linked + 1;
            END LOOP;
    END IF;

    RETURN jsonb_build_object(
            'organization', to_jsonb(v_org),
            'linked_count', v_linked
           );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.link_app_organization(UUID, UUID, BOOLEAN, UUID) IS 'Links an app to an existing organization, cascading over its ownership tree to organizations and locations. Additive and idempotent.';

-- ----------------------------------------------------------------------------
-- init
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

CREATE OR REPLACE FUNCTION claimius.init(
    p_system_app_slug       TEXT,
    p_system_app_name       TEXT,
    p_system_app_private_key  TEXT,
    p_system_app_private_seed TEXT,
    p_system_user_id        UUID DEFAULT NULL,
    p_system_app_redirect_uri  TEXT DEFAULT '',
    p_system_app_sync_uri      TEXT DEFAULT '',
    p_system_app_contact_email TEXT DEFAULT NULL,
    p_system_app_image      TEXT DEFAULT NULL,
    p_system_app_provider_ids UUID[] DEFAULT ARRAY[]::UUID[],
    p_system_app_style      JSONB DEFAULT NULL,
    p_system_app_description TEXT DEFAULT 'System app',
    p_system_org_name       TEXT DEFAULT 'System',
    p_system_claim_name     TEXT DEFAULT 'System Administrator',
    p_system_claim_id       UUID DEFAULT NULL,
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
    p_system_user_id  := COALESCE(claimius.ensure_uuid_null(p_system_user_id),  gen_random_uuid());
    p_system_claim_id := COALESCE(claimius.ensure_uuid_null(p_system_claim_id), gen_random_uuid());

    -- Validate the required default claim metadata. The system claim is
    -- created with hardcoded properties; the default claim (granted to every
    -- regular user on first login via ensure_app_user) is implementer defined.
    IF p_default_claim IS NULL THEN
        RAISE EXCEPTION 'init: p_default_claim is required and must contain name and description.';
    END IF;
    v_default_name  := p_default_claim ->> 'name';
    v_default_desc  := p_default_claim ->> 'description';
    v_default_level := coalesce((p_default_claim ->> 'sa_access')::INTEGER, 4);
    IF v_default_name IS NULL OR v_default_name = '' THEN
        RAISE EXCEPTION 'init: p_default_claim.name is required.';
    END IF;
    IF v_default_desc IS NULL OR v_default_desc = '' THEN
        RAISE EXCEPTION 'init: p_default_claim.description is required.';
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
        redirect_uris, sync_uri, provider_ids, status,
        private_key, private_seed,
        style, sa_owner_id, sa_created_by
    ) VALUES (
                 v_pre_app_id, p_system_app_slug, p_system_app_name,
                 p_system_app_description, p_system_app_contact_email, p_system_app_image,
                 CASE WHEN p_system_app_redirect_uri = '' THEN ARRAY[]::TEXT[] ELSE ARRAY[p_system_app_redirect_uri] END,
                 p_system_app_sync_uri, p_system_app_provider_ids,
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
    WHERE type = 'system'
      AND sa_deleted_at IS NULL
      AND claimius.app_has_object(v_app_id, 'claimius.organization', id)
    LIMIT 1;

    IF v_org_id IS NULL THEN
        INSERT INTO claimius.organization (
            id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by
        ) VALUES (
                     v_pre_org_id, p_system_org_name, 'system',
                     v_pre_org_id, v_pre_org_id, 0, p_system_user_id
                 )
        RETURNING id INTO v_org_id;
    END IF;

    PERFORM claimius.link_app_object(v_app_id, 'claimius.organization', v_org_id, p_system_user_id);

    -- 4. reconcile: app's sa_owner_id should match the actual system org id
    UPDATE claimius.samna_app SET sa_owner_id = v_org_id WHERE id = v_app_id AND sa_owner_id <> v_org_id;

    -- 5. ensure ownership tree closure self-edge for the system org
    INSERT INTO claimius.inheritance_info (
        tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
    ) VALUES (
        'ownership', v_org_id, 'claimius.organization', v_org_id, 'claimius.organization', v_org_id, 0
    ) ON CONFLICT DO NOTHING;

    -- 6. system claim (sidelined: granted only to the system user)
    SELECT id INTO v_claim_id FROM claimius.claim
    WHERE app_id = v_app_id AND name = p_system_claim_name AND sa_deleted_at IS NULL;

    IF v_claim_id IS NULL THEN
        INSERT INTO claimius.claim (
            id, app_id, name, description, sa_access, inherits, type,
            sa_owner_id, sa_root_id, sa_created_by
        ) VALUES (
                     p_system_claim_id, v_app_id, p_system_claim_name,
                     'Full access to claimius', 15, TRUE, 'system',
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
            app_id, name, description, sa_access, inherits, type,
            sa_owner_id, sa_root_id, sa_created_by
        ) VALUES (
                     v_app_id, v_default_name, v_default_desc, v_default_level,
                     FALSE, 'user',
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

    -- 8b. bind claim to system app directly. Every resource (except users
    -- and the claim itself) carries at least one direct claim_object grant
    -- so that access never depends solely on cascade. inherits = FALSE
    -- because the org binding above already cascades to the app via the
    -- ownership tree; this is an owner anchor, not a cascade source.
    IF NOT EXISTS (
        SELECT 1 FROM claimius.claim_object
        WHERE app_id = v_app_id
          AND claim_id = v_claim_id
          AND object_id = v_app_id
          AND object_type = 'claimius.samna_app'
          AND sa_deleted_at IS NULL
    ) THEN
        INSERT INTO claimius.claim_object (
            app_id, claim_id, object_id, object_type,
            sa_owner_id, sa_root_id, sa_created_by, inherits
        ) VALUES (
                     v_app_id, v_claim_id, v_app_id, 'claimius.samna_app',
                     v_org_id, v_org_id, p_system_user_id, FALSE
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
    -- not returned. Re-running init is idempotent and returns the
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

COMMENT ON FUNCTION claimius.init(p_system_app_slug text, p_system_app_name text, p_system_app_private_key text, p_system_app_private_seed text, p_system_user_id uuid, p_system_app_redirect_uri text, p_system_app_sync_uri text, p_system_app_contact_email text, p_system_app_image text, p_system_app_provider_ids uuid[], p_system_app_style jsonb, p_system_app_description text, p_system_org_name text, p_system_claim_name text, p_system_claim_id uuid, p_default_claim jsonb) IS 'Bootstrap function. Sets up system app, org, user, and claim. Idempotent.';

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
    -- type as init and init_hybrid.
    RETURN '{}'::JSONB;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.init_disciple(p_disciple_app_slug text) IS 'Bootstrap as a disciple. Requires the upstream prophet app slug. State arrives from prophet via sync.';

-- ----------------------------------------------------------------------------
-- init_hybrid
-- Hybrid: instance acts as a disciple to an upstream prophet AND as a
-- prophet for downstream disciples. Combines init and init_disciple
-- behavior plus role grants for both.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.init_hybrid(
    p_system_app_slug          TEXT,
    p_system_app_name          TEXT,
    p_system_app_private_key   TEXT,
    p_system_app_private_seed  TEXT,
    p_disciple_app_slug        TEXT,
    p_system_user_id           UUID DEFAULT NULL,
    p_system_app_redirect_uri  TEXT DEFAULT '',
    p_system_app_sync_uri      TEXT DEFAULT '',
    p_system_app_contact_email TEXT DEFAULT NULL,
    p_system_app_image         TEXT DEFAULT NULL,
    p_system_app_provider_ids  UUID[] DEFAULT ARRAY[]::UUID[],
    p_system_app_style         JSONB DEFAULT NULL,
    p_system_app_description   TEXT DEFAULT 'System app',
    p_system_org_name          TEXT DEFAULT 'System',
    p_system_claim_name        TEXT DEFAULT 'System Administrator',
    p_system_claim_id          UUID DEFAULT NULL,
    p_default_claim            JSONB DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_result JSONB;
BEGIN
    v_result := claimius.init(
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

COMMENT ON FUNCTION claimius.init_hybrid(p_system_app_slug text, p_system_app_name text, p_system_app_private_key text, p_system_app_private_seed text, p_disciple_app_slug text, p_system_user_id uuid, p_system_app_redirect_uri text, p_system_app_sync_uri text, p_system_app_contact_email text, p_system_app_image text, p_system_app_provider_ids uuid[], p_system_app_style jsonb, p_system_app_description text, p_system_org_name text, p_system_claim_name text, p_system_claim_id uuid, p_default_claim jsonb) IS 'Bootstrap as a hybrid (prophet + disciple).';

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
             claimius.init, claimius.init_disciple, claimius.init_hybrid,
             claimius.init_claimius_tables, claimius.init_claimius_internal,
             claimius.recompute_state
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
             claimius.notification, claimius.prophet_state
             TO claimius_writer';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.claim_graph_cache TO claimius_writer';
    EXECUTE 'GRANT SELECT, INSERT, UPDATE ON claimius.object_usage TO claimius_writer';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.claim_graph_cache TO claimius_reader';
    EXECUTE 'GRANT SELECT, INSERT, UPDATE ON claimius.object_usage TO claimius_reader';
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
             claimius.init, claimius.init_disciple, claimius.init_hybrid,
             claimius.init_claimius_tables, claimius.init_claimius_internal,
             claimius.recompute_state
             FROM claimius_reader';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.user_object, claimius.object_users, claimius.user_users,
             claimius.disciple_state, claimius.samna_user, claimius.samna_client,
             claimius.samna_secret, claimius.samna_app, claimius.organization, claimius.location,
             claimius.claim, claimius.user_claim, claimius.claim_object,
             claimius.object_field, claimius.user_field, claimius.user_relation,
             claimius.audit, claimius.inheritance_info, claimius.table_info,
             claimius.notification, claimius.claim_graph_cache, claimius.object_usage
             TO claimius_disciple_client';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.claim_graph_cache TO claimius_reader';
    EXECUTE 'GRANT SELECT, INSERT, UPDATE ON claimius.object_usage TO claimius_reader';
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
    p_email      TEXT DEFAULT NULL,
    p_phone      TEXT DEFAULT NULL,
    p_external_id TEXT DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_user_row    claimius.samna_user;
    v_uc_row      claimius.user_claim;
    v_default_claim_id UUID;
    v_default_claim_owner UUID;
BEGIN
    p_user_id := claimius.ensure_uuid_null(p_user_id);

    -- Find or insert the samna_user row
    SELECT * INTO v_user_row FROM claimius.samna_user
    WHERE user_id = p_user_id AND app_id = p_app_id AND sa_deleted_at IS NULL;

    IF NOT FOUND THEN
        INSERT INTO claimius.samna_user (
            user_id, app_id, first_name, last_name, user_name,
            email, phone, external_id, status, type
        )
        VALUES (
                   COALESCE(p_user_id, gen_random_uuid()),
                   p_app_id, p_first_name, p_last_name, p_user_name,
                   p_email, p_phone, p_external_id, 'active', 'user'
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
        WHERE user_id = v_user_row.user_id
          AND app_id = p_app_id
          AND claim_id = v_default_claim_id
          AND sa_deleted_at IS NULL
        LIMIT 1;

        IF NOT FOUND THEN
            INSERT INTO claimius.user_claim (
                app_id, claim_id, user_id, sa_owner_id, sa_created_by
            ) VALUES (
                         p_app_id, v_default_claim_id, v_user_row.user_id,
                         v_default_claim_owner,
                         v_user_row.user_id  -- bootstrap exception: actor is the user themselves on first login
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

COMMENT ON FUNCTION claimius.ensure_app_user(p_user_id uuid, p_app_id uuid, p_first_name text, p_last_name text, p_user_name text, p_email text, p_phone text, p_external_id text) IS 'Creates per app samna_user row and grants the app default claim. Idempotent.';

DROP FUNCTION IF EXISTS claimius.get_access(UUID, UUID, TEXT, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_owner_descendants(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS TABLE(object_type TEXT, object_id UUID, hop INTEGER) AS $$
BEGIN
    RETURN QUERY
        SELECT e.descendant_type, e.descendant_id, e.depth
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'ownership'
          AND e.ancestor_type = p_object_type
          AND e.ancestor_id   = p_object_id
        ORDER BY e.depth;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_owner_descendants(p_object_type text, p_object_id uuid) IS 'Ownership tree descendants of an object, hop 0 = self.';


CREATE OR REPLACE FUNCTION claimius.get_location_descendants(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS TABLE(object_type TEXT, object_id UUID, hop INTEGER) AS $$
BEGIN
    RETURN QUERY
        SELECT e.descendant_type, e.descendant_id, e.depth
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'location'
          AND e.ancestor_type = p_object_type
          AND e.ancestor_id   = p_object_id;

    IF p_object_type <> 'claimius.location' THEN
        IF NOT EXISTS (
            SELECT 1 FROM claimius.inheritance_info e
            WHERE e.tree_type = 'location'
              AND e.ancestor_type = p_object_type
              AND e.ancestor_id   = p_object_id
              AND e.depth = 0
        ) THEN
            RETURN QUERY SELECT p_object_type, p_object_id, 0;
        END IF;
    END IF;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_location_descendants(p_object_type text, p_object_id uuid) IS 'Location tree descendants of an object across every registered table located in the subtree.';


CREATE OR REPLACE FUNCTION claimius.get_parenthood_descendants(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS TABLE(object_type TEXT, object_id UUID, hop INTEGER) AS $$
BEGIN
    RETURN QUERY
        SELECT e.descendant_type, e.descendant_id, e.depth
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'parenthood'
          AND e.ancestor_type = p_object_type
          AND e.ancestor_id   = p_object_id
        ORDER BY e.depth;

    IF NOT EXISTS (
        SELECT 1 FROM claimius.inheritance_info e
        WHERE e.tree_type = 'parenthood'
          AND e.ancestor_type = p_object_type
          AND e.ancestor_id   = p_object_id
          AND e.depth = 0
    ) THEN
        RETURN QUERY SELECT p_object_type, p_object_id, 0;
    END IF;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_parenthood_descendants(p_object_type text, p_object_id uuid) IS 'Parenthood tree descendants via sa_parent_id, hop 0 = self.';


CREATE OR REPLACE FUNCTION claimius.get_descendants(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS TABLE(object_type TEXT, object_id UUID, hop INTEGER, tree_type TEXT) AS $$
BEGIN
    RETURN QUERY
        SELECT d.object_type, d.object_id, d.hop, 'ownership'::TEXT
        FROM claimius.get_owner_descendants(p_object_type, p_object_id) d;

    RETURN QUERY
        SELECT d.object_type, d.object_id, d.hop, 'location'::TEXT
        FROM claimius.get_location_descendants(p_object_type, p_object_id) d;

    RETURN QUERY
        SELECT d.object_type, d.object_id, d.hop, 'parenthood'::TEXT
        FROM claimius.get_parenthood_descendants(p_object_type, p_object_id) d;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_descendants(p_object_type text, p_object_id uuid) IS 'Combined descendant walk across owner, location, and parenthood trees.';


GRANT EXECUTE ON FUNCTION claimius.get_owner_descendants(TEXT, UUID)       TO claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_location_descendants(TEXT, UUID)    TO claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_parenthood_descendants(TEXT, UUID)  TO claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_descendants(TEXT, UUID)             TO claimius_reader;


CREATE OR REPLACE FUNCTION claimius.get_claim_for_user(
    p_user_claim_id UUID
) RETURNS SETOF claimius.claim AS $$
BEGIN
    RETURN QUERY
        SELECT c.*
        FROM claimius.claim c
                 JOIN claimius.user_claim uc ON uc.claim_id = c.id
        WHERE uc.id = p_user_claim_id
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql;


CREATE OR REPLACE FUNCTION claimius.normalize_object_type(p_object_type TEXT)
RETURNS TEXT AS $$
    SELECT CASE
        WHEN p_object_type IS NULL THEN NULL
        WHEN position('.' IN p_object_type) = 0 THEN p_object_type
        ELSE split_part(p_object_type, '.', 2)
    END;
$$ LANGUAGE sql IMMUTABLE;

GRANT EXECUTE ON FUNCTION claimius.normalize_object_type(TEXT) TO claimius_reader, claimius_writer, claimius_admin;

CREATE OR REPLACE FUNCTION claimius.graph_type_passes(
    p_object_type     TEXT,
    p_include_types   TEXT[],
    p_exclude_types   TEXT[]
) RETURNS BOOLEAN AS $$
    SELECT (p_include_types IS NULL
            OR array_length(p_include_types, 1) IS NULL
            OR p_object_type = ANY(p_include_types)
            OR claimius.normalize_object_type(p_object_type) = ANY(p_include_types))
       AND NOT (p_exclude_types IS NOT NULL
            AND array_length(p_exclude_types, 1) IS NOT NULL
            AND (p_object_type = ANY(p_exclude_types)
                 OR claimius.normalize_object_type(p_object_type) = ANY(p_exclude_types)));
$$ LANGUAGE sql IMMUTABLE;

GRANT EXECUTE ON FUNCTION claimius.graph_type_passes(TEXT, TEXT[], TEXT[]) TO claimius_reader, claimius_writer, claimius_admin;

CREATE OR REPLACE FUNCTION claimius._prune_graph_rows(
    p_graph    JSONB,
    p_user_id  UUID,
    p_app_id   UUID,
    p_start_id UUID,
    p_width    INTEGER,
    p_sort     TEXT,
    p_next_id  UUID
) RETURNS TABLE(object_id UUID, object_type TEXT, label TEXT, parent_id UUID, data JSONB) AS $$
BEGIN
    RETURN QUERY
    WITH RECURSIVE nodes AS (
        SELECT (e->>'object_id')::UUID       AS object_id,
               e->>'object_type'             AS object_type,
               e->>'label'                   AS label,
               NULLIF(e->>'parent_id', '')::UUID AS parent_id,
               e->'data'                     AS data
        FROM jsonb_array_elements(p_graph) AS e
    ),
    ranked AS (
        SELECT n.*,
               CASE WHEN p_sort = 'usage' THEN -coalesce(ou.used_count, 0) ELSE 0 END AS sort_rank,
               coalesce(n.label, n.object_id::TEXT) AS sort_name
        FROM nodes n
        LEFT JOIN claimius.object_usage ou
               ON p_sort = 'usage'
              AND ou.app_id = p_app_id AND ou.user_id = p_user_id
              AND ou.object_type = n.object_type AND ou.object_id = n.object_id
    ),
    next_tuple AS (
        SELECT rk.sort_rank, rk.sort_name, rk.object_type
        FROM ranked rk
        WHERE p_next_id IS NOT NULL AND rk.object_id = p_next_id
        LIMIT 1
    ),
    windowed AS (
        SELECT rk.*,
               ROW_NUMBER() OVER (
                   PARTITION BY rk.parent_id, rk.object_type
                   ORDER BY rk.sort_rank, rk.sort_name, rk.object_id
               ) AS rn
        FROM ranked rk
        WHERE p_next_id IS NULL OR p_start_id IS NULL OR rk.parent_id IS DISTINCT FROM p_start_id
           OR rk.object_type IS DISTINCT FROM (SELECT nt.object_type FROM next_tuple nt)
           OR ROW(rk.sort_rank, rk.sort_name, rk.object_id) > (SELECT ROW(nt.sort_rank, nt.sort_name, p_next_id) FROM next_tuple nt)
    ),
    kept AS (
        SELECT w.object_id, w.object_type
        FROM windowed w
        WHERE w.parent_id IS NULL
           OR NOT EXISTS (SELECT 1 FROM nodes p WHERE p.object_id = w.parent_id)
        UNION ALL
        SELECT c.object_id, c.object_type
        FROM windowed c
        JOIN kept k ON c.parent_id = k.object_id
        WHERE c.rn <= p_width
    )
    SELECT n.object_id, n.object_type, n.label, n.parent_id, n.data
    FROM nodes n
    WHERE EXISTS (SELECT 1 FROM kept k WHERE k.object_id = n.object_id AND k.object_type = n.object_type)
      AND NOT (p_next_id IS NOT NULL AND n.object_id = p_start_id);
END;
$$ LANGUAGE plpgsql STABLE;

GRANT EXECUTE ON FUNCTION claimius._prune_graph_rows(JSONB, UUID, UUID, UUID, INTEGER, TEXT, UUID) TO claimius_reader, claimius_writer, claimius_admin;

CREATE OR REPLACE FUNCTION claimius.touch_object_usage(
    p_app_id      UUID,
    p_user_id     UUID,
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS VOID AS $$
    INSERT INTO claimius.object_usage (app_id, user_id, object_type, object_id, used_count, last_used_at)
    VALUES (p_app_id, p_user_id, p_object_type, p_object_id, 1, now())
    ON CONFLICT (app_id, user_id, object_type, object_id)
    DO UPDATE SET used_count = claimius.object_usage.used_count + 1, last_used_at = now();
$$ LANGUAGE sql;

GRANT EXECUTE ON FUNCTION claimius.touch_object_usage(UUID, UUID, TEXT, UUID) TO claimius_reader, claimius_writer, claimius_admin, claimius_disciple_client;


CREATE OR REPLACE FUNCTION claimius.graph_object_types()
    RETURNS TEXT[] AS $$
    SELECT ARRAY[
        'claimius.organization',
        'claimius.location',
        'claimius.samna_user',
        'claimius.samna_secret',
        'claimius.claim',
        'public.bookable',
        'public.bookable_type',
        'public.timeslot',
        'public.asset',
        'public.code',
        'public.capability',
        'public.action'
    ]::TEXT[];
$$ LANGUAGE sql IMMUTABLE;

GRANT EXECUTE ON FUNCTION claimius.graph_object_types() TO claimius_reader, claimius_writer, claimius_admin;

CREATE OR REPLACE FUNCTION claimius.merge_graphs(p_a JSONB, p_b JSONB)
RETURNS JSONB AS $$
DECLARE
    v_a      JSONB := COALESCE(p_a, '{}'::jsonb);
    v_b      JSONB := COALESCE(p_b, '{}'::jsonb);
    v_a_obj  JSONB := COALESCE(v_a -> 'objects', '{}'::jsonb);
    v_b_obj  JSONB := COALESCE(v_b -> 'objects', '{}'::jsonb);
    v_objs   JSONB := v_a_obj;
    v_links  JSONB;
    v_type   TEXT;
BEGIN
    FOR v_type IN SELECT jsonb_object_keys(v_b_obj) LOOP
        v_objs := jsonb_set(
            v_objs,
            ARRAY[v_type],
            COALESCE(v_objs -> v_type, '{}'::jsonb) || (v_b_obj -> v_type)
        );
    END LOOP;

    WITH all_links AS (
        SELECT l FROM jsonb_array_elements(COALESCE(v_a -> 'links', '[]'::jsonb)) AS l
        UNION
        SELECT l FROM jsonb_array_elements(COALESCE(v_b -> 'links', '[]'::jsonb)) AS l
    )
    SELECT COALESCE(jsonb_agg(l), '[]'::jsonb) INTO v_links FROM all_links;

    RETURN jsonb_build_object('objects', v_objs, 'links', v_links);
END;
$$ LANGUAGE plpgsql IMMUTABLE;

GRANT EXECUTE ON FUNCTION claimius.merge_graphs(JSONB, JSONB) TO claimius_reader, claimius_writer, claimius_admin;

DROP FUNCTION IF EXISTS claimius.get_graph_objects(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID[]);

CREATE OR REPLACE FUNCTION claimius.get_graph_objects(
    p_user_id         UUID,
    p_app_id          UUID,
    p_required_access INTEGER DEFAULT 4,
    p_include_types   TEXT[] DEFAULT NULL,
    p_exclude_types   TEXT[] DEFAULT NULL,
    p_object_ids      UUID[] DEFAULT NULL,
    p_with_image      BOOLEAN DEFAULT FALSE,
    p_compact         BOOLEAN DEFAULT FALSE
) RETURNS JSONB AS $$
DECLARE
    v_result JSONB;
    v_allowed TEXT[] := claimius.graph_object_types();
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN '{}'::jsonb;
    END IF;

    WITH visible AS (
        SELECT uo.object_id, uo.object_type
        FROM claimius.user_object uo
        WHERE uo.user_id = p_user_id
          AND uo.app_id = p_app_id
          AND (p_required_access = 0 OR (uo.sa_access & p_required_access) = p_required_access)
          AND (p_object_ids IS NULL OR uo.object_id = ANY(p_object_ids))
          AND uo.object_type = ANY(v_allowed)
          AND claimius.graph_type_passes(uo.object_type, p_include_types, p_exclude_types)
    ),
    user_ids AS (
        SELECT u.id
        FROM claimius.samna_user u
        WHERE u.app_id = p_app_id
          AND u.sa_deleted_at IS NULL
          AND claimius.graph_type_passes('claimius.samna_user', p_include_types, p_exclude_types)
          AND (
              EXISTS (SELECT 1 FROM visible v WHERE v.object_id = u.id AND v.object_type = 'claimius.samna_user')
              OR u.user_id = p_user_id
              OR EXISTS (
                  SELECT 1 FROM claimius.user_users uu
                  WHERE uu.viewer_id = p_user_id
                    AND uu.app_id = p_app_id
                    AND uu.target_user_id = u.user_id
              )
          )
    ),
    claim_ids AS (
        SELECT c.id
        FROM claimius.claim c
        WHERE c.app_id = p_app_id
          AND c.sa_deleted_at IS NULL
          AND claimius.graph_type_passes('claimius.claim', p_include_types, p_exclude_types)
          AND (
              EXISTS (SELECT 1 FROM visible v WHERE v.object_id = c.id AND v.object_type = 'claimius.claim')
              OR EXISTS (
                  SELECT 1 FROM claimius.user_claim uc
                  WHERE uc.user_id = p_user_id
                    AND uc.app_id = p_app_id
                    AND uc.claim_id = c.id
                    AND uc.sa_deleted_at IS NULL
              )
          )
    ),
    image_for AS (
        SELECT v.object_id, v.object_type,
               jsonb_build_object('id', a.id, 'url', a.blob_url, 'mime_type', a.mime_type) AS image
        FROM visible v
        JOIN LATERAL (
            SELECT a.id, a.blob_url, a.mime_type
            FROM public.get_assets(v.object_type, v.object_id, 'image/*') ga
            JOIN public.asset a ON a.id = ga.asset_id AND a.sa_deleted_at IS NULL
            ORDER BY ga.level, ga.hop, ga.index
            LIMIT 1
        ) a ON TRUE
        WHERE p_with_image
    ),
    rows AS (
        SELECT 'organization'::TEXT AS norm_type, o.id AS object_id,
               CASE WHEN p_compact
                    THEN jsonb_build_object('name', o.name)
                    ELSE jsonb_build_object(
                        'name',        o.name,
                        'description', o.description,
                        'type',        o.type,
                        'level',       o.sa_level,
                        'external_id', o.external_id
                    )
               END AS base
        FROM claimius.organization o
        JOIN visible v ON v.object_id = o.id AND v.object_type = 'claimius.organization'
        WHERE o.sa_deleted_at IS NULL

        UNION ALL
        SELECT 'location', l.id,
               CASE WHEN p_compact
                    THEN jsonb_build_object('name', l.name)
                    ELSE jsonb_build_object(
                        'name',        l.name,
                        'description', l.description,
                        'type',        l.type,
                        'level',       l.sa_level,
                        'longitude',   l.longitude,
                        'latitude',    l.latitude,
                        'external_id', l.external_id,
                        'code_type',   l.code_type,
                        'code',        l.code
                    )
               END
        FROM claimius.location l
        JOIN visible v ON v.object_id = l.id AND v.object_type = 'claimius.location'
        WHERE l.sa_deleted_at IS NULL

        UNION ALL
        SELECT 'samna_user', u.id,
               CASE WHEN p_compact
                    THEN jsonb_build_object('name', coalesce(nullif(trim(coalesce(u.first_name,'') || ' ' || coalesce(u.last_name,'')), ''), u.email, u.user_id::TEXT))
                    ELSE jsonb_build_object(
                        'name',        coalesce(nullif(trim(coalesce(u.first_name,'') || ' ' || coalesce(u.last_name,'')), ''), u.email, u.user_id::TEXT),
                        'first_name',  u.first_name,
                        'last_name',   u.last_name,
                        'user_name',   u.user_name,
                        'phone',       u.phone,
                        'email',       u.email,
                        'external_id', u.external_id,
                        'status',      u.status,
                        'type',        u.type,
                        'config',      u.config,
                        'user_id',     u.user_id,
                        'self',        (u.user_id = p_user_id)
                    )
               END
        FROM claimius.samna_user u
        JOIN user_ids ui ON ui.id = u.id
        WHERE u.sa_deleted_at IS NULL

        UNION ALL
        SELECT 'samna_secret', s.id,
               CASE WHEN p_compact
                    THEN jsonb_build_object('name', s.key)
                    ELSE jsonb_build_object('name', s.key, 'expires_at', s.expires_at)
               END
        FROM claimius.samna_secret s
        JOIN visible v ON v.object_id = s.id AND v.object_type = 'claimius.samna_secret'
        WHERE s.sa_deleted_at IS NULL

        UNION ALL
        SELECT 'claim', c.id,
               CASE WHEN p_compact
                    THEN jsonb_build_object('name', c.name)
                    ELSE jsonb_build_object(
                        'name',        c.name,
                        'description', c.description,
                        'access',      c.sa_access,
                        'inherits',    c.inherits,
                        'type',        c.type,
                        'is_deny',     (c.sa_access & 16) <> 0
                    )
               END
        FROM claimius.claim c
        JOIN claim_ids ci ON ci.id = c.id
        WHERE c.sa_deleted_at IS NULL

        UNION ALL
        SELECT 'bookable', b.id,
               CASE WHEN p_compact
                    THEN jsonb_build_object('name', b.name)
                    ELSE jsonb_build_object('name', b.name, 'description', b.description)
               END
        FROM public.bookable b
        JOIN visible v ON v.object_id = b.id AND v.object_type = 'public.bookable'
        WHERE b.sa_deleted_at IS NULL

        UNION ALL
        SELECT 'bookable_type', bt.id,
               CASE WHEN p_compact
                    THEN jsonb_build_object('name', bt.name)
                    ELSE jsonb_build_object('name', bt.name, 'description', bt.description, 'keywords', to_jsonb(bt.keywords))
               END
        FROM public.bookable_type bt
        JOIN visible v ON v.object_id = bt.id AND v.object_type = 'public.bookable_type'
        WHERE bt.sa_deleted_at IS NULL

        UNION ALL
        SELECT 'timeslot', t.id,
               CASE WHEN p_compact
                    THEN jsonb_build_object('name', t.name)
                    ELSE jsonb_build_object('name', t.name, 'description', t.description, 'schedule', t.schedule)
               END
        FROM public.timeslot t
        JOIN visible v ON v.object_id = t.id AND v.object_type = 'public.timeslot'
        WHERE t.sa_deleted_at IS NULL

        UNION ALL
        SELECT 'asset', a.id,
               CASE WHEN p_compact
                    THEN jsonb_build_object('name', a.name)
                    ELSE jsonb_build_object(
                        'name',        a.name,
                        'description', a.description,
                        'mime_type',   a.mime_type,
                        'status',      a.status,
                        'blob_url',    a.blob_url,
                        'banner_id',   a.banner_id,
                        'icon',        a.icon
                    )
               END
        FROM public.asset a
        JOIN visible v ON v.object_id = a.id AND v.object_type = 'public.asset'
        WHERE a.sa_deleted_at IS NULL

        UNION ALL
        SELECT 'code', c.id,
               CASE WHEN p_compact
                    THEN jsonb_build_object('name', c.name)
                    ELSE jsonb_build_object(
                        'name',        c.name,
                        'description', c.description,
                        'value',       c.value,
                        'data',        c.data,
                        'styling',     c.styling,
                        'expires_at',  c.expires_at
                    )
               END
        FROM public.code c
        JOIN visible v ON v.object_id = c.id AND v.object_type = 'public.code'
        WHERE c.sa_deleted_at IS NULL

        UNION ALL
        SELECT 'capability', cp.id,
               CASE WHEN p_compact
                    THEN jsonb_build_object('name', cp.name)
                    ELSE jsonb_build_object(
                        'name',        cp.name,
                        'description', cp.description,
                        'locale',      cp.locale,
                        'value',       cp.value,
                        'render',      cp.render
                    )
               END
        FROM public.capability cp
        JOIN visible v ON v.object_id = cp.id AND v.object_type = 'public.capability'
        WHERE cp.sa_deleted_at IS NULL

        UNION ALL
        SELECT 'action', ac.id,
               CASE WHEN p_compact
                    THEN jsonb_build_object('name', ac.name)
                    ELSE jsonb_build_object(
                        'name',             ac.name,
                        'description',      ac.description,
                        'trigger',          ac.trigger,
                        'code',             ac.code,
                        'public',           ac.public,
                        'continue_on_fail', ac.continue_on_fail,
                        'dedup_mode',       ac.dedup_mode,
                        'input',            ac.input
                    )
               END
        FROM public.action ac
        JOIN visible v ON v.object_id = ac.id AND v.object_type = 'public.action'
        WHERE ac.sa_deleted_at IS NULL
    ),
    enriched AS (
        SELECT r.norm_type, r.object_id,
               r.base || COALESCE(jsonb_build_object('image', img.image), '{}'::jsonb) AS data
        FROM rows r
        LEFT JOIN image_for img ON img.object_id = r.object_id
        WHERE r.norm_type IS NOT NULL
    ),
    grouped AS (
        SELECT norm_type, jsonb_object_agg(object_id::TEXT, data) AS objects
        FROM enriched
        GROUP BY norm_type
    )
    SELECT COALESCE(jsonb_object_agg(norm_type, objects), '{}'::jsonb) INTO v_result FROM grouped;

    RETURN v_result;
END;
$$ LANGUAGE plpgsql STABLE;

GRANT EXECUTE ON FUNCTION claimius.get_graph_objects(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID[], BOOLEAN, BOOLEAN) TO claimius_reader, claimius_writer, claimius_admin;

DROP FUNCTION IF EXISTS claimius.get_graph_links_owner(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_graph_links_owner(
    p_user_id         UUID,
    p_app_id          UUID,
    p_required_access INTEGER DEFAULT 4,
    p_include_types   TEXT[] DEFAULT NULL,
    p_exclude_types   TEXT[] DEFAULT NULL,
    p_start_id        UUID DEFAULT NULL,
    p_depth           INTEGER DEFAULT 0,
    p_width           INTEGER DEFAULT 0,
    p_sort            TEXT DEFAULT 'name',
    p_next_id         UUID DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_links     JSONB;
    v_nodes     JSONB;
    v_allowed   TEXT[] := claimius.graph_object_types();
    v_next_rank BIGINT := 0;
    v_next_name TEXT;
    v_next_type TEXT;
BEGIN
    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN jsonb_build_object('links', '[]'::jsonb);
    END IF;

    IF p_width > 0 THEN
        IF p_next_id IS NOT NULL THEN
            SELECT coalesce(uo.sa_name, p_next_id::TEXT),
                   CASE WHEN p_sort = 'usage' THEN -coalesce(ou.used_count, 0) ELSE 0 END,
                   uo.object_type
              INTO v_next_name, v_next_rank, v_next_type
            FROM claimius.user_object uo
            LEFT JOIN claimius.object_usage ou
                   ON ou.app_id = p_app_id AND ou.user_id = p_user_id
                  AND ou.object_type = uo.object_type AND ou.object_id = uo.object_id
            WHERE uo.user_id = p_user_id AND uo.app_id = p_app_id AND uo.object_id = p_next_id
            LIMIT 1;
            IF v_next_name IS NULL THEN
                v_next_name := p_next_id::TEXT;
                v_next_rank := 0;
            END IF;
        END IF;

        WITH RECURSIVE scope AS (
            SELECT uo.object_id, uo.object_type, uo.sa_name
            FROM claimius.user_object uo
            WHERE uo.user_id = p_user_id
              AND uo.app_id = p_app_id
              AND (p_required_access = 0 OR (uo.sa_access & p_required_access) = p_required_access)
              AND uo.object_type = ANY(v_allowed)
              AND claimius.graph_type_passes(uo.object_type, p_include_types, p_exclude_types)
        ),
        roots AS (
            SELECT s.object_id, s.object_type
            FROM scope s
            WHERE (p_start_id IS NOT NULL AND s.object_id = p_start_id)
               OR (p_start_id IS NULL AND NOT EXISTS (
                     SELECT 1 FROM claimius.inheritance_info pii
                     JOIN scope ps ON ps.object_id = pii.ancestor_id AND ps.object_type = pii.ancestor_type
                     WHERE pii.descendant_id = s.object_id
                       AND pii.descendant_type = s.object_type
                       AND pii.depth = 1
                  ))
        ),
        walk AS (
            SELECT r.object_id, r.object_type,
                   NULL::TEXT AS source_type, NULL::UUID AS source_id,
                   NULL::TEXT AS relation_raw, 0 AS lvl
            FROM roots r
            UNION ALL
            SELECT c.object_id, c.object_type, w.object_type, w.object_id, c.relation_raw, w.lvl + 1
            FROM walk w
            CROSS JOIN LATERAL (
                SELECT x.object_id, x.object_type, x.relation_raw
                FROM (
                    SELECT s.object_id, s.object_type, ii.tree_type::TEXT AS relation_raw,
                           ROW_NUMBER() OVER (
                               PARTITION BY s.object_type
                               ORDER BY CASE WHEN p_sort = 'usage' THEN -coalesce(ou.used_count, 0) ELSE 0 END,
                                        coalesce(s.sa_name, s.object_id::TEXT), s.object_id
                           ) AS rn
                    FROM claimius.inheritance_info ii
                    JOIN scope s ON s.object_id = ii.descendant_id AND s.object_type = ii.descendant_type
                    LEFT JOIN claimius.object_usage ou
                           ON p_sort = 'usage'
                          AND ou.app_id = p_app_id AND ou.user_id = p_user_id
                          AND ou.object_type = s.object_type AND ou.object_id = s.object_id
                    WHERE ii.ancestor_id = w.object_id
                      AND ii.ancestor_type = w.object_type
                      AND ii.depth = 1
                      AND (w.lvl > 0 OR p_next_id IS NULL OR p_start_id IS NULL
                           OR s.object_type IS DISTINCT FROM v_next_type
                           OR ROW(CASE WHEN p_sort = 'usage' THEN -coalesce(ou.used_count, 0) ELSE 0 END,
                                  coalesce(s.sa_name, s.object_id::TEXT), s.object_id)
                            > ROW(v_next_rank, v_next_name, p_next_id))
                ) x
                WHERE x.rn <= p_width
            ) c
            WHERE p_depth = 0 OR w.lvl < p_depth
        )
        SELECT COALESCE(jsonb_agg(jsonb_build_object(
                   'source_type', claimius.normalize_object_type(w.source_type),
                   'source_id',   w.source_id,
                   'target_type', claimius.normalize_object_type(w.object_type),
                   'target_id',   w.object_id,
                   'relation',    CASE w.relation_raw WHEN 'ownership' THEN 'owner' ELSE w.relation_raw END
               )) FILTER (WHERE w.source_id IS NOT NULL), '[]'::jsonb),
               COALESCE(jsonb_agg(DISTINCT to_jsonb(w.object_id))
                   FILTER (WHERE NOT (p_next_id IS NOT NULL AND w.lvl = 0)), '[]'::jsonb)
          INTO v_links, v_nodes
        FROM walk w;

        RETURN jsonb_build_object('links', v_links, 'node_ids', v_nodes);
    END IF;

    WITH ancestor_pool AS (
        SELECT uo.object_id, uo.object_type
        FROM claimius.user_object uo
        WHERE uo.user_id = p_user_id
          AND uo.app_id = p_app_id
          AND (p_required_access = 0 OR (uo.sa_access & p_required_access) = p_required_access)
          AND uo.object_type = ANY(v_allowed)
    ),
    descendants AS (
        SELECT uo.object_id, uo.object_type
        FROM claimius.user_object uo
        WHERE uo.user_id = p_user_id
          AND uo.app_id = p_app_id
          AND (p_required_access = 0 OR (uo.sa_access & p_required_access) = p_required_access)
          AND uo.object_type = ANY(v_allowed)
          AND claimius.graph_type_passes(uo.object_type, p_include_types, p_exclude_types)

        UNION
        SELECT c.id AS object_id, 'claimius.claim'::TEXT AS object_type
        FROM claimius.claim c
        WHERE c.app_id = p_app_id
          AND c.sa_deleted_at IS NULL
          AND claimius.graph_type_passes('claimius.claim', p_include_types, p_exclude_types)
          AND EXISTS (
              SELECT 1 FROM claimius.user_claim uc
              WHERE uc.user_id = p_user_id
                AND uc.app_id = p_app_id
                AND uc.claim_id = c.id
                AND uc.sa_deleted_at IS NULL
          )
    ),
    in_scope AS (
        SELECT d.object_id, d.object_type
        FROM descendants d
        WHERE p_start_id IS NULL
           OR d.object_id = p_start_id
           OR EXISTS (
               SELECT 1 FROM claimius.inheritance_info ii
               WHERE ii.ancestor_id = p_start_id
                 AND ii.descendant_id = d.object_id
                 AND ii.descendant_type = d.object_type
                 AND ii.depth >= 1
                 AND (p_depth = 0 OR ii.depth <= p_depth)
           )
    ),
    direct_edges AS (
        SELECT ii.tree_type,
               ii.ancestor_type   AS source_type,
               ii.ancestor_id     AS source_id,
               ii.descendant_type AS target_type,
               ii.descendant_id   AS target_id
        FROM claimius.inheritance_info ii
        JOIN ancestor_pool ap ON ap.object_id = ii.ancestor_id   AND ap.object_type = ii.ancestor_type
        JOIN in_scope      vt ON vt.object_id = ii.descendant_id AND vt.object_type = ii.descendant_type
        WHERE ii.depth = 1
    ),
    fallback_edges AS (
        SELECT DISTINCT ON (ii.tree_type, ii.descendant_type, ii.descendant_id)
               ii.tree_type,
               ii.ancestor_type   AS source_type,
               ii.ancestor_id     AS source_id,
               ii.descendant_type AS target_type,
               ii.descendant_id   AS target_id
        FROM claimius.inheritance_info ii
        JOIN ancestor_pool ap ON ap.object_id = ii.ancestor_id   AND ap.object_type = ii.ancestor_type
        JOIN in_scope      vt ON vt.object_id = ii.descendant_id AND vt.object_type = ii.descendant_type
        WHERE ii.depth > 1
          AND NOT EXISTS (
              SELECT 1 FROM direct_edges de
              WHERE de.tree_type   = ii.tree_type
                AND de.target_type = ii.descendant_type
                AND de.target_id   = ii.descendant_id
          )
        ORDER BY ii.tree_type, ii.descendant_type, ii.descendant_id, ii.depth ASC
    ),
    edges AS (
        SELECT source_type, source_id, target_type, target_id, tree_type::TEXT AS relation_raw
        FROM direct_edges
        UNION ALL
        SELECT source_type, source_id, target_type, target_id, tree_type::TEXT AS relation_raw
        FROM fallback_edges
    )
    SELECT COALESCE(jsonb_agg(jsonb_build_object(
        'source_type', claimius.normalize_object_type(source_type),
        'source_id',   source_id,
        'target_type', claimius.normalize_object_type(target_type),
        'target_id',   target_id,
        'relation',    CASE relation_raw WHEN 'ownership' THEN 'owner' ELSE relation_raw END
    )), '[]'::jsonb) INTO v_links FROM edges;

    RETURN jsonb_build_object('links', v_links);
END;
$$ LANGUAGE plpgsql STABLE;

GRANT EXECUTE ON FUNCTION claimius.get_graph_links_owner(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) TO claimius_reader, claimius_writer, claimius_admin;

DROP FUNCTION IF EXISTS claimius.get_graph_links_claim(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_graph_links_claim(
    p_user_id         UUID,
    p_app_id          UUID,
    p_required_access INTEGER DEFAULT 0,
    p_include_types   TEXT[] DEFAULT NULL,
    p_exclude_types   TEXT[] DEFAULT NULL,
    p_start_id        UUID DEFAULT NULL,
    p_depth           INTEGER DEFAULT 0,
    p_width           INTEGER DEFAULT 0,
    p_sort            TEXT DEFAULT 'name',
    p_next_id         UUID DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_user_row_id UUID;
    v_claim_ids   UUID[];
    v_links       JSONB;
    v_nodes       JSONB;
    v_next_rank   BIGINT := 0;
    v_next_name   TEXT;
    v_next_type   TEXT;
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN jsonb_build_object('links', '[]'::jsonb);
    END IF;

    SELECT u.id INTO v_user_row_id
    FROM claimius.samna_user u
    WHERE u.user_id = p_user_id AND u.app_id = p_app_id AND u.sa_deleted_at IS NULL
    LIMIT 1;

    IF p_start_id IS NULL THEN
        SELECT array_agg(uc.claim_id) INTO v_claim_ids
        FROM claimius.user_claim uc
        JOIN claimius.claim c ON c.id = uc.claim_id
        WHERE uc.user_id = p_user_id
          AND uc.app_id = p_app_id
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
          AND (p_required_access = 0 OR (c.sa_access & p_required_access) = p_required_access);
    ELSE
        SELECT array_agg(c.id) INTO v_claim_ids
        FROM claimius.claim c
        WHERE c.id = p_start_id
          AND c.app_id = p_app_id
          AND c.sa_deleted_at IS NULL
          AND (p_required_access = 0 OR (c.sa_access & p_required_access) = p_required_access);
    END IF;

    IF v_claim_ids IS NULL THEN
        v_claim_ids := ARRAY[]::UUID[];
    END IF;

    IF p_width = 0 AND p_next_id IS NULL THEN
        WITH cascade_edges AS (
            SELECT DISTINCT ON (co.id, ii.descendant_type, ii.descendant_id, ii.tree_type)
                   co.id AS co_id,
                   co.claim_id AS claim_id,
                   co.reason AS reason,
                   ii.descendant_type AS target_type,
                   ii.descendant_id AS target_id,
                   ii.tree_type::TEXT AS tree_type
            FROM claimius.claim_object co
            JOIN claimius.claim c ON c.id = co.claim_id AND c.sa_deleted_at IS NULL
            JOIN claimius.inheritance_info ii
                   ON ii.ancestor_type = co.object_type
                  AND ii.ancestor_id   = co.object_id
                  AND ii.depth >= 1
                  AND (p_depth = 0 OR ii.depth <= p_depth)
            WHERE co.app_id = p_app_id
              AND co.sa_deleted_at IS NULL
              AND co.inherits = TRUE
              AND c.inherits = TRUE
              AND co.claim_id = ANY(v_claim_ids)
              AND claimius.graph_type_passes(ii.descendant_type, p_include_types, p_exclude_types)
            ORDER BY co.id, ii.descendant_type, ii.descendant_id, ii.tree_type, ii.depth ASC
        ),
        spine AS (
            SELECT jsonb_build_object(
                'source_type',  'samna_user',
                'source_id',    v_user_row_id,
                'target_type',  'claim',
                'target_id',    uc.claim_id,
                'relation',     'holds',
                'relation_id',  uc.id,
                'relation_type','user_claim',
                'reason',       uc.reason,
                'starts_at',    uc.starts_at,
                'ends_at',      uc.ends_at
            ) AS link
            FROM claimius.user_claim uc
            WHERE v_user_row_id IS NOT NULL
              AND uc.user_id = p_user_id
              AND uc.app_id = p_app_id
              AND uc.sa_deleted_at IS NULL
              AND uc.claim_id = ANY(v_claim_ids)

            UNION ALL
            SELECT jsonb_build_object(
                'source_type',  'claim',
                'source_id',    co.claim_id,
                'target_type',  claimius.normalize_object_type(co.object_type),
                'target_id',    co.object_id,
                'relation',     'direct',
                'relation_id',  co.id,
                'relation_type','claim_object',
                'reason',       co.reason
            )
            FROM claimius.claim_object co
            WHERE co.app_id = p_app_id
              AND co.sa_deleted_at IS NULL
              AND co.claim_id = ANY(v_claim_ids)
              AND co.object_id IS NOT NULL
              AND claimius.graph_type_passes(co.object_type, p_include_types, p_exclude_types)

            UNION ALL
            SELECT jsonb_build_object(
                'source_type',  'claim',
                'source_id',    ce.claim_id,
                'target_type',  claimius.normalize_object_type(ce.target_type),
                'target_id',    ce.target_id,
                'relation',     CASE ce.tree_type WHEN 'ownership' THEN 'owner' ELSE ce.tree_type END,
                'relation_id',  ce.co_id,
                'relation_type','claim_object',
                'reason',       ce.reason
            )
            FROM cascade_edges ce

            UNION ALL
            SELECT jsonb_build_object(
                'source_type', 'samna_user',
                'source_id',   v_user_row_id,
                'target_type', 'samna_user',
                'target_id',   u.id,
                'relation',    'visibility'
            )
            FROM claimius.samna_user u
            JOIN claimius.user_users uu ON uu.viewer_id = p_user_id AND uu.app_id = p_app_id AND uu.target_user_id = u.user_id
            WHERE v_user_row_id IS NOT NULL
              AND p_start_id IS NULL
              AND u.app_id = p_app_id
              AND u.sa_deleted_at IS NULL
              AND u.id <> v_user_row_id

            UNION ALL
            SELECT jsonb_build_object(
                'source_type',  'samna_user',
                'source_id',    su.id,
                'target_type',  claimius.normalize_object_type(ur.object_type),
                'target_id',    ur.object_id,
                'relation',     'user_relation',
                'relation_id',  ur.id,
                'relation_type','user_relation',
                'reason',       ur.description,
                'pinned',       ur.pinned,
                'priority',     ur.priority,
                'used_at',      ur.used_at
            )
            FROM claimius.user_relation ur
            JOIN claimius.samna_user su ON su.user_id = ur.user_id AND su.app_id = p_app_id AND su.sa_deleted_at IS NULL
            JOIN claimius.user_object uo
                   ON uo.object_id = ur.object_id AND uo.object_type = ur.object_type
                  AND uo.user_id = p_user_id AND uo.app_id = p_app_id
            WHERE ur.app_id = p_app_id
              AND p_start_id IS NULL
              AND ur.sa_deleted_at IS NULL
              AND ur.user_id = p_user_id
              AND claimius.graph_type_passes(ur.object_type, p_include_types, p_exclude_types)
        )
        SELECT COALESCE(jsonb_agg(link), '[]'::jsonb) INTO v_links FROM spine;

        RETURN jsonb_build_object('links', v_links);
    END IF;

    IF p_next_id IS NOT NULL AND p_start_id IS NOT NULL THEN
        SELECT coalesce(uo.sa_name, p_next_id::TEXT),
               CASE WHEN p_sort = 'usage' THEN -coalesce(ou.used_count, 0) ELSE 0 END,
               uo.object_type
          INTO v_next_name, v_next_rank, v_next_type
        FROM claimius.user_object uo
        LEFT JOIN claimius.object_usage ou
               ON ou.app_id = p_app_id AND ou.user_id = p_user_id
              AND ou.object_type = uo.object_type AND ou.object_id = uo.object_id
        WHERE uo.user_id = p_user_id AND uo.app_id = p_app_id AND uo.object_id = p_next_id
        LIMIT 1;
        IF v_next_name IS NULL THEN
            v_next_name := p_next_id::TEXT;
            v_next_rank := 0;
        END IF;
    END IF;

    WITH direct_bindings AS (
        SELECT co.id, co.claim_id, co.object_type, co.object_id, co.reason, co.inherits,
               ROW_NUMBER() OVER (
                   PARTITION BY co.claim_id, co.object_type
                   ORDER BY CASE WHEN p_sort = 'usage' THEN -coalesce(ou.used_count, 0) ELSE 0 END,
                            coalesce(uo.sa_name, co.object_id::TEXT), co.object_id
               ) AS rn
        FROM claimius.claim_object co
        LEFT JOIN claimius.user_object uo
               ON uo.user_id = p_user_id AND uo.app_id = p_app_id
              AND uo.object_type = co.object_type AND uo.object_id = co.object_id
        LEFT JOIN claimius.object_usage ou
               ON p_sort = 'usage'
              AND ou.app_id = p_app_id AND ou.user_id = p_user_id
              AND ou.object_type = co.object_type AND ou.object_id = co.object_id
        WHERE co.app_id = p_app_id
          AND co.sa_deleted_at IS NULL
          AND co.claim_id = ANY(v_claim_ids)
          AND co.object_id IS NOT NULL
          AND claimius.graph_type_passes(co.object_type, p_include_types, p_exclude_types)
          AND (p_next_id IS NULL OR p_start_id IS NULL
               OR co.object_type IS DISTINCT FROM v_next_type
               OR ROW(CASE WHEN p_sort = 'usage' THEN -coalesce(ou.used_count, 0) ELSE 0 END,
                      coalesce(uo.sa_name, co.object_id::TEXT), co.object_id)
                > ROW(v_next_rank, v_next_name, p_next_id))
    ),
    kept_bindings AS (
        SELECT * FROM direct_bindings WHERE p_width = 0 OR rn <= p_width
    ),
    cascade_dedup AS (
        SELECT DISTINCT ON (kb.id, ii.descendant_type, ii.descendant_id, ii.tree_type)
               kb.id AS co_id,
               kb.claim_id AS claim_id,
               kb.reason AS reason,
               ii.descendant_type AS target_type,
               ii.descendant_id AS target_id,
               ii.tree_type::TEXT AS tree_type
        FROM kept_bindings kb
        JOIN claimius.claim c ON c.id = kb.claim_id AND c.sa_deleted_at IS NULL AND c.inherits = TRUE
        JOIN claimius.inheritance_info ii
               ON ii.ancestor_type = kb.object_type
              AND ii.ancestor_id   = kb.object_id
              AND ii.depth >= 1
              AND (p_depth = 0 OR ii.depth <= p_depth)
        WHERE kb.inherits = TRUE
          AND claimius.graph_type_passes(ii.descendant_type, p_include_types, p_exclude_types)
        ORDER BY kb.id, ii.descendant_type, ii.descendant_id, ii.tree_type, ii.depth ASC
    ),
    cascade_edges AS (
        SELECT cd.*,
               ROW_NUMBER() OVER (
                   PARTITION BY cd.co_id, cd.target_type
                   ORDER BY CASE WHEN p_sort = 'usage' THEN -coalesce(ou.used_count, 0) ELSE 0 END,
                            coalesce(uo.sa_name, cd.target_id::TEXT), cd.target_id
               ) AS crn
        FROM cascade_dedup cd
        LEFT JOIN claimius.user_object uo
               ON uo.user_id = p_user_id AND uo.app_id = p_app_id
              AND uo.object_type = cd.target_type AND uo.object_id = cd.target_id
        LEFT JOIN claimius.object_usage ou
               ON p_sort = 'usage'
              AND ou.app_id = p_app_id AND ou.user_id = p_user_id
              AND ou.object_type = cd.target_type AND ou.object_id = cd.target_id
    ),
    kept_cascades AS (
        SELECT * FROM cascade_edges WHERE p_width = 0 OR crn <= p_width
    ),
    spine AS (
        SELECT jsonb_build_object(
            'source_type',  'samna_user',
            'source_id',    v_user_row_id,
            'target_type',  'claim',
            'target_id',    uc.claim_id,
            'relation',     'holds',
            'relation_id',  uc.id,
            'relation_type','user_claim',
            'reason',       uc.reason,
            'starts_at',    uc.starts_at,
            'ends_at',      uc.ends_at
        ) AS link
        FROM (
            SELECT uc.*,
                   ROW_NUMBER() OVER (ORDER BY c.name, uc.claim_id) AS hrn
            FROM claimius.user_claim uc
            JOIN claimius.claim c ON c.id = uc.claim_id
            WHERE v_user_row_id IS NOT NULL
              AND uc.user_id = p_user_id
              AND uc.app_id = p_app_id
              AND uc.sa_deleted_at IS NULL
              AND uc.claim_id = ANY(v_claim_ids)
        ) uc
        WHERE p_width = 0 OR uc.hrn <= p_width

        UNION ALL
        SELECT jsonb_build_object(
            'source_type',  'claim',
            'source_id',    kb.claim_id,
            'target_type',  claimius.normalize_object_type(kb.object_type),
            'target_id',    kb.object_id,
            'relation',     'direct',
            'relation_id',  kb.id,
            'relation_type','claim_object',
            'reason',       kb.reason
        )
        FROM kept_bindings kb

        UNION ALL
        SELECT jsonb_build_object(
            'source_type',  'claim',
            'source_id',    kc.claim_id,
            'target_type',  claimius.normalize_object_type(kc.target_type),
            'target_id',    kc.target_id,
            'relation',     CASE kc.tree_type WHEN 'ownership' THEN 'owner' ELSE kc.tree_type END,
            'relation_id',  kc.co_id,
            'relation_type','claim_object',
            'reason',       kc.reason
        )
        FROM kept_cascades kc

        UNION ALL
        SELECT jsonb_build_object(
            'source_type', 'samna_user',
            'source_id',   v_user_row_id,
            'target_type', 'samna_user',
            'target_id',   u.id,
            'relation',    'visibility'
        )
        FROM (
            SELECT u.id,
                   ROW_NUMBER() OVER (
                       ORDER BY coalesce(nullif(trim(coalesce(u.first_name,'') || ' ' || coalesce(u.last_name,'')), ''), u.email, u.user_id::TEXT), u.id
                   ) AS vrn
            FROM claimius.samna_user u
            JOIN claimius.user_users uu ON uu.viewer_id = p_user_id AND uu.app_id = p_app_id AND uu.target_user_id = u.user_id
            WHERE v_user_row_id IS NOT NULL
              AND p_start_id IS NULL
              AND u.app_id = p_app_id
              AND u.sa_deleted_at IS NULL
              AND u.id <> v_user_row_id
        ) u
        WHERE p_width = 0 OR u.vrn <= p_width

        UNION ALL
        SELECT jsonb_build_object(
            'source_type',  'samna_user',
            'source_id',    su.id,
            'target_type',  claimius.normalize_object_type(ur.object_type),
            'target_id',    ur.object_id,
            'relation',     'user_relation',
            'relation_id',  ur.id,
            'relation_type','user_relation',
            'reason',       ur.description,
            'pinned',       ur.pinned,
            'priority',     ur.priority,
            'used_at',      ur.used_at
        )
        FROM (
            SELECT ur.*,
                   ROW_NUMBER() OVER (
                       ORDER BY coalesce(uo.sa_name, ur.object_id::TEXT), ur.object_id
                   ) AS rrn
            FROM claimius.user_relation ur
            JOIN claimius.user_object uo
                   ON uo.object_id = ur.object_id AND uo.object_type = ur.object_type
                  AND uo.user_id = p_user_id AND uo.app_id = p_app_id
            WHERE ur.app_id = p_app_id
              AND p_start_id IS NULL
              AND ur.sa_deleted_at IS NULL
              AND ur.user_id = p_user_id
              AND claimius.graph_type_passes(ur.object_type, p_include_types, p_exclude_types)
        ) ur
        JOIN claimius.samna_user su ON su.user_id = ur.user_id AND su.app_id = p_app_id AND su.sa_deleted_at IS NULL
        WHERE p_width = 0 OR ur.rrn <= p_width
    ),
    node_pool AS (
        SELECT kb.object_id AS node_id FROM kept_bindings kb
        UNION
        SELECT kc.target_id FROM kept_cascades kc
    )
    SELECT COALESCE(jsonb_agg(link), '[]'::jsonb),
           (SELECT COALESCE(jsonb_agg(to_jsonb(node_id)), '[]'::jsonb) FROM node_pool)
      INTO v_links, v_nodes
    FROM spine;

    IF p_width > 0 THEN
        RETURN jsonb_build_object('links', v_links, 'node_ids', v_nodes);
    END IF;

    RETURN jsonb_build_object('links', v_links);
END;
$$ LANGUAGE plpgsql STABLE;

GRANT EXECUTE ON FUNCTION claimius.get_graph_links_claim(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) TO claimius_reader, claimius_writer, claimius_admin;

DROP FUNCTION IF EXISTS claimius.create_organization_user(UUID, UUID, UUID, JSONB);
DROP FUNCTION IF EXISTS claimius.create_organization_user(UUID, UUID, UUID, JSONB, BOOLEAN);

CREATE OR REPLACE FUNCTION claimius.create_organization_user(
    p_caller_uc  UUID,
    p_caller_cid UUID,
    p_owner_id   UUID,
    p_user       JSONB,
    p_upsert     BOOLEAN DEFAULT FALSE
) RETURNS JSONB AS $$
DECLARE
    v_app_id            UUID := claimius.get_disciple_app_id();
    v_user_id           UUID;
    v_external_id       TEXT;
    v_claim_id          UUID;
    v_root_id           UUID;
    v_user_row          claimius.samna_user;
    v_caller_user_id    UUID;
    v_existing_uc       claimius.user_claim;
    v_existing_virtual  claimius.claim;
    v_existing_caller   claimius.user_claim;
    v_existing_obj      claimius.claim_object;
    v_claim_user        JSONB;
    v_virtual_claim     JSONB;
    v_virtual_claim_id  UUID;
    v_claim_obj         JSONB;
    v_caller_user_claim JSONB;
BEGIN
    v_user_id     := NULLIF(NULLIF(p_user ->> 'user_id', ''), '00000000-0000-0000-0000-000000000000')::UUID;
    v_claim_id    := NULLIF(p_user ->> 'claim_id', '')::UUID;
    v_external_id := NULLIF(p_user ->> 'external_id', '');

    IF v_claim_id IS NULL THEN
        RAISE EXCEPTION 'claim_id required';
    END IF;
    IF p_owner_id IS NULL THEN
        RAISE EXCEPTION 'owner_id required';
    END IF;

    SELECT sa_root_id INTO v_root_id
      FROM claimius.organization
     WHERE id = p_owner_id
       AND sa_deleted_at IS NULL;

    IF v_root_id IS NULL THEN
        RAISE EXCEPTION 'owner_id invalid';
    END IF;

    IF p_caller_uc IS NOT NULL THEN
        SELECT user_id INTO v_caller_user_id
          FROM claimius.user_claim
         WHERE id = p_caller_uc
           AND sa_deleted_at IS NULL;

        IF v_caller_user_id IS NULL THEN
            RAISE EXCEPTION 'caller user_claim invalid';
        END IF;
    END IF;

    IF NOT p_upsert THEN
        IF v_user_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM claimius.samna_user
             WHERE user_id = v_user_id
               AND app_id = v_app_id
               AND sa_deleted_at IS NULL
        ) THEN
            RAISE EXCEPTION 'user_id already exists' USING ERRCODE = '23505';
        END IF;

        IF v_external_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM claimius.samna_user
             WHERE external_id = v_external_id
               AND app_id = v_app_id
               AND sa_deleted_at IS NULL
        ) THEN
            RAISE EXCEPTION 'external_id already exists' USING ERRCODE = '23505';
        END IF;

        INSERT INTO claimius.samna_user (
            user_id, app_id, first_name, last_name, user_name,
            email, phone, external_id, status, type
        ) VALUES (
            COALESCE(v_user_id, gen_random_uuid()),
            v_app_id,
            NULLIF(p_user ->> 'first_name', ''),
            NULLIF(p_user ->> 'last_name', ''),
            NULLIF(p_user ->> 'user_name', ''),
            NULLIF(p_user ->> 'email', ''),
            NULLIF(p_user ->> 'phone', ''),
            v_external_id,
            COALESCE(NULLIF(p_user ->> 'status', ''), 'active')::claimius.user_status,
            'user'
        )
        RETURNING * INTO v_user_row;
    ELSE
        INSERT INTO claimius.samna_user (
            user_id, app_id, first_name, last_name, user_name,
            email, phone, external_id, status, type
        ) VALUES (
            COALESCE(v_user_id, gen_random_uuid()),
            v_app_id,
            NULLIF(p_user ->> 'first_name', ''),
            NULLIF(p_user ->> 'last_name', ''),
            NULLIF(p_user ->> 'user_name', ''),
            NULLIF(p_user ->> 'email', ''),
            NULLIF(p_user ->> 'phone', ''),
            v_external_id,
            COALESCE(NULLIF(p_user ->> 'status', ''), 'active')::claimius.user_status,
            'user'
        )
        ON CONFLICT (user_id, app_id) WHERE sa_deleted_at IS NULL DO UPDATE SET
            first_name    = COALESCE(EXCLUDED.first_name, claimius.samna_user.first_name),
            last_name     = COALESCE(EXCLUDED.last_name, claimius.samna_user.last_name),
            user_name     = COALESCE(EXCLUDED.user_name, claimius.samna_user.user_name),
            email         = COALESCE(EXCLUDED.email, claimius.samna_user.email),
            phone         = COALESCE(EXCLUDED.phone, claimius.samna_user.phone),
            external_id   = COALESCE(EXCLUDED.external_id, claimius.samna_user.external_id),
            sa_updated_at = now()
        RETURNING * INTO v_user_row;
    END IF;

    SELECT * INTO v_existing_uc
      FROM claimius.user_claim
     WHERE app_id = v_app_id
       AND claim_id = v_claim_id
       AND user_id = v_user_row.user_id
       AND sa_deleted_at IS NULL
     LIMIT 1;

    IF v_existing_uc.id IS NOT NULL THEN
        v_claim_user := jsonb_build_object('user_claim', to_jsonb(v_existing_uc));
    ELSE
        v_claim_user := claimius.assign_claim_user(
            p_app_id        => v_app_id,
            p_claim_id      => v_claim_id,
            p_user_id       => v_user_row.user_id,
            p_sa_owner_id   => p_owner_id,
            p_sa_created_by => p_caller_uc
        );
    END IF;

    SELECT * INTO v_existing_virtual
      FROM claimius.claim
     WHERE app_id = v_app_id
       AND name = 'user:' || v_user_row.user_id::text
       AND type = 'virtual'
       AND sa_deleted_at IS NULL
     LIMIT 1;

    IF v_existing_virtual.id IS NOT NULL THEN
        v_virtual_claim    := jsonb_build_object('claim', to_jsonb(v_existing_virtual));
        v_virtual_claim_id := v_existing_virtual.id;
    ELSE
        v_virtual_claim := claimius.create_claim(
            p_app_id        => v_app_id,
            p_name          => 'user:' || v_user_row.user_id::text,
            p_description   => NULL,
            p_sa_access     => 15,
            p_sa_owner_id   => p_owner_id,
            p_sa_root_id    => v_root_id,
            p_sa_created_by => p_caller_uc,
            p_inherits      => TRUE,
            p_type          => 'virtual'
        );
        v_virtual_claim_id := (v_virtual_claim -> 'claim' ->> 'id')::UUID;
    END IF;

    SELECT * INTO v_existing_obj
      FROM claimius.claim_object
     WHERE app_id = v_app_id
       AND claim_id = v_virtual_claim_id
       AND object_id = v_user_row.user_id
       AND object_type = 'claimius.samna_user'
       AND sa_deleted_at IS NULL
     LIMIT 1;

    IF v_existing_obj.id IS NOT NULL THEN
        v_claim_obj := jsonb_build_object('claim_object', to_jsonb(v_existing_obj));
    ELSE
        v_claim_obj := claimius.assign_claim_object(
            p_app_id        => v_app_id,
            p_claim_id      => v_virtual_claim_id,
            p_sa_owner_id   => p_owner_id,
            p_sa_root_id    => v_root_id,
            p_sa_created_by => p_caller_uc,
            p_object_id     => v_user_row.user_id,
            p_object_type   => 'claimius.samna_user'
        );
    END IF;

    IF v_caller_user_id IS NOT NULL THEN
        SELECT * INTO v_existing_caller
          FROM claimius.user_claim
         WHERE app_id = v_app_id
           AND claim_id = v_virtual_claim_id
           AND user_id = v_caller_user_id
           AND sa_deleted_at IS NULL
         LIMIT 1;

        IF v_existing_caller.id IS NOT NULL THEN
            v_caller_user_claim := jsonb_build_object('user_claim', to_jsonb(v_existing_caller));
        ELSE
            v_caller_user_claim := claimius.assign_claim_user(
                p_app_id        => v_app_id,
                p_claim_id      => v_virtual_claim_id,
                p_user_id       => v_caller_user_id,
                p_sa_owner_id   => p_owner_id,
                p_sa_created_by => p_caller_uc
            );
        END IF;
    END IF;

    RETURN jsonb_build_object(
        'samna_user',        to_jsonb(v_user_row),
        'user_claim',        v_claim_user        -> 'user_claim',
        'virtual_claim',     v_virtual_claim     -> 'claim',
        'claim_object',      v_claim_obj         -> 'claim_object',
        'caller_user_claim', v_caller_user_claim -> 'user_claim'
    );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.create_organization_user(UUID, UUID, UUID, JSONB, BOOLEAN) IS
    'Disciple side: insert or upsert a samna_user, ensure the supplied claim is granted to the user, ensure a per-user virtual claim with full access is bound to the user, and when p_caller_uc is not null ensure that virtual claim is granted to the caller. p_caller_uc may be null for system-bootstrap callers that have no authenticating user_claim yet; in that case sa_created_by is stamped null and the caller_user_claim grant is skipped. With p_upsert=FALSE default, raises 23505 when user_id or external_id already exists in the app scope. With p_upsert=TRUE, conflicts on user_id and app_id update the row and downstream grants are reused when present. Returns samna_user, user_claim, virtual_claim, claim_object, caller_user_claim.';

GRANT EXECUTE ON FUNCTION claimius.create_organization_user(UUID, UUID, UUID, JSONB, BOOLEAN)
    TO claimius_disciple_client, claimius_writer, claimius_admin;

ALTER TABLE claimius.user_token ALTER COLUMN app_id SET DEFAULT claimius.get_disciple_app_id();

-- ----------------------------------------------------------------------------
-- Notification writers
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.notify_notification() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('claimius_notification',
        json_build_object('user_id', NEW.user_id, 'id', NEW.id)::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.notify_notification() IS 'Trigger function. Wakes the live layer with the recipient and row id on the claimius_notification channel after an insert.';

CREATE OR REPLACE FUNCTION claimius.notify_object(
    p_app_id UUID, p_title TEXT, p_description TEXT, p_style JSONB, p_type TEXT,
    p_link TEXT, p_object_type TEXT, p_object_id UUID, p_created_by UUID,
    p_owner_id UUID, p_repeats_at INTERVAL
) RETURNS INTEGER AS $$
    WITH inserted AS (
        INSERT INTO claimius.notification
            (app_id, user_id, title, description, style, type, link,
             object_id, object_type, sa_created_by, sa_owner_id, repeats_at)
        SELECT p_app_id, uo.user_id, p_title, p_description,
               COALESCE(p_style, '{}'::jsonb), p_type, p_link,
               p_object_id, p_object_type, p_created_by, p_owner_id, p_repeats_at
        FROM claimius.user_object uo
        WHERE uo.app_id = p_app_id
          AND uo.object_type = p_object_type
          AND uo.object_id = p_object_id
          AND (uo.sa_access & 4) <> 0
          AND (p_created_by IS NULL OR uo.user_id <> p_created_by)
        RETURNING 1
    )
    SELECT count(*)::INTEGER FROM inserted;
$$ LANGUAGE sql;

COMMENT ON FUNCTION claimius.notify_object(UUID, TEXT, TEXT, JSONB, TEXT, TEXT, TEXT, UUID, UUID, UUID, INTERVAL) IS 'Fans a notification out to every user holding read access on the object through user_object, excluding the triggering user. Returns the number of rows inserted.';

GRANT EXECUTE ON FUNCTION claimius.notify_object(UUID, TEXT, TEXT, JSONB, TEXT, TEXT, TEXT, UUID, UUID, UUID, INTERVAL)
    TO claimius_writer, claimius_admin;

CREATE OR REPLACE FUNCTION claimius.notify_user(
    p_app_id UUID, p_title TEXT, p_description TEXT, p_style JSONB, p_type TEXT,
    p_link TEXT, p_object_type TEXT, p_object_id UUID, p_created_by UUID,
    p_owner_id UUID, p_repeats_at INTERVAL, p_user_ids UUID[]
) RETURNS INTEGER AS $$
    WITH inserted AS (
        INSERT INTO claimius.notification
            (app_id, user_id, title, description, style, type, link,
             object_id, object_type, sa_created_by, sa_owner_id, repeats_at)
        SELECT p_app_id, su.user_id, p_title, p_description,
               COALESCE(p_style, '{}'::jsonb), p_type, p_link,
               p_object_id, p_object_type, p_created_by, p_owner_id, p_repeats_at
        FROM claimius.samna_user su
        WHERE su.user_id = ANY(p_user_ids)
          AND su.app_id = p_app_id
          AND su.status = 'active'
          AND su.sa_deleted_at IS NULL
        RETURNING 1
    )
    SELECT count(*)::INTEGER FROM inserted;
$$ LANGUAGE sql;

COMMENT ON FUNCTION claimius.notify_user(UUID, TEXT, TEXT, JSONB, TEXT, TEXT, TEXT, UUID, UUID, UUID, INTERVAL, UUID[]) IS 'Writes a notification straight to each named active user. Returns the number of rows inserted.';

GRANT EXECUTE ON FUNCTION claimius.notify_user(UUID, TEXT, TEXT, JSONB, TEXT, TEXT, TEXT, UUID, UUID, UUID, INTERVAL, UUID[])
    TO claimius_writer, claimius_admin;

-- ----------------------------------------------------------------------------
-- trim_sync_event
-- Removes outbox rows every registered consumer has already applied.
-- prophet_state.last_applied_seq is the per app cursor, advanced when an app
-- asks for what comes after it, so a row at or below the minimum across the
-- apps still in contact is owed to nobody. An app whose last contact is older
-- than p_stale_after is treated as gone rather than allowed to pin the
-- watermark forever, which is how an outbox grows without bound while looking
-- healthy. With no registered consumer nothing is published and nothing is
-- removed. Returns the number of rows deleted.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius.trim_sync_event(
    p_stale_after   INTERVAL DEFAULT INTERVAL '7 days'
) RETURNS BIGINT AS $$
DECLARE
    v_watermark BIGINT;
    v_deleted   BIGINT;
BEGIN
    SELECT min(last_applied_seq) INTO v_watermark
    FROM claimius.prophet_state
    WHERE last_contact_at IS NOT NULL
      AND last_contact_at > now() - p_stale_after;

    IF v_watermark IS NULL OR v_watermark <= 0 THEN
        RETURN 0;
    END IF;

    DELETE FROM claimius.sync_event WHERE seq <= v_watermark;
    GET DIAGNOSTICS v_deleted = ROW_COUNT;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.trim_sync_event(INTERVAL) IS 'Deletes sync_event rows at or below the lowest cursor among consumers still in contact. Run on a schedule.';

ALTER FUNCTION claimius.get_graph_links_owner(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) SET plan_cache_mode = force_custom_plan;
ALTER FUNCTION claimius.get_graph_links_claim(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) SET plan_cache_mode = force_custom_plan;
ALTER FUNCTION claimius.get_owner_graph(UUID, UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) SET plan_cache_mode = force_custom_plan;
ALTER FUNCTION claimius.get_claim_graph(UUID, UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) SET plan_cache_mode = force_custom_plan;
ALTER FUNCTION claimius._prune_graph_rows(JSONB, UUID, UUID, UUID, INTEGER, TEXT, UUID) SET plan_cache_mode = force_custom_plan;
