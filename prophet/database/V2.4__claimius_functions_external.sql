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
-- whose access mask carries the most bits set. scope is the settled field
-- scope of the pair, NULL when every field carries the same mask.
--
-- Contract: when claim_id is non-null, user_claim_id is also populated. This
-- is the actor token callers use as sa_created_by on follow-on writes.
-- When direct_grant is true and claim_id is null, user_claim_id is also null;
-- callers needing an actor for direct-only access must resolve a fallback
-- user_claim themselves (typically the user's bootstrap user_claim for the app).
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_access(UUID, UUID, UUID, TEXT, INTEGER);

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
                    direct_grant        BOOLEAN,
                    level          INTEGER
                ) AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id, p_object_type, p_object_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        WITH uo AS (
            SELECT u.*, oi.sa_owner_id, oi.sa_location_id
            FROM claimius.user_object u
                LEFT JOIN claimius.object_info oi ON oi.object_type = u.object_type AND oi.object_id = u.object_id
            WHERE u.user_id = p_user_id
              AND u.app_id = p_app_id
              AND u.object_type = p_object_type
              AND u.object_id = p_object_id
              AND (u.sa_access & p_required_access) = p_required_access
        )
        SELECT
            claimius.composite_id(p_app_id::TEXT, p_user_id::TEXT, p_object_type, p_object_id::TEXT),
            best.user_claim_id,
            best.claim_id,
            best.claim_name,
            best.access_bits,
            uo.scope,
            uo.sa_owner_id,
            (SELECT o.sa_root_id FROM claimius.organization o WHERE o.id = uo.sa_owner_id),
            uo.sa_location_id,
            FALSE,
            best.grant_level
        FROM uo
                 CROSS JOIN LATERAL claimius._best_grant(uo.grants, p_user_id, p_app_id, p_required_access) best;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_access(UUID, UUID, UUID, TEXT, INTEGER) IS 'Strongest active claim based grant for one (user, app, object) whose mask covers p_required_access. user_claim_id is the actor token for follow-on writes. sa_root_id is the owning organization root, suitable for FK to claimius.organization on follow-on claim and claim_object writes.';

-- ----------------------------------------------------------------------------
-- get_direct_access
-- Locates the closest, highest-privilege user_claim the calling user holds
-- that's structurally related to the given object. Walks the owner,
-- location, and parenthood ancestor chains; for each ancestor checks
-- whether the user has any user_claim bound to it via claim_object, or
-- holds a claim linking to a claim bound to it. A binding carries its
-- claim mask narrowed by the binding mask, a composed one the bits both
-- sides carry. The user's denies subtract at every required mask: one
-- bound on the object itself from every candidate, one on an ancestor from
-- the candidates found on an ancestor, so a share bound on the object
-- stands over an ancestor deny. What remains is closed under implication.
-- A binding scoped only to fields the type lacks grants nothing.
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

DROP FUNCTION IF EXISTS claimius.get_direct_access(UUID, UUID, UUID, TEXT, INTEGER, BOOLEAN);
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
                    direct_grant        BOOLEAN,
                    level          INTEGER
                ) AS $$
DECLARE
    v_exists   BOOLEAN;
    v_has_soft BOOLEAN;
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id, p_object_type, p_object_id);

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
                TRUE,
                0
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
        WITH anc AS MATERIALIZED (
            SELECT DISTINCT a.object_type, a.object_id, a.hop
            FROM claimius.get_ancestors(p_object_type, p_object_id) a
        ),
        fields AS (
            SELECT coalesce(array_agg(pa.attname::TEXT), '{}'::TEXT[]) AS names
            FROM pg_attribute pa
            WHERE pa.attrelid = to_regclass(p_object_type)
              AND pa.attnum > 0
              AND NOT pa.attisdropped
        ),
        held AS MATERIALIZED (
            SELECT uc.id AS user_claim_id, c.id AS claim_id, c.name, c.sa_access, c.inherits, c.level
            FROM claimius.user_claim uc
                     JOIN claimius.claim c
                          ON c.id = uc.claim_id
                              AND c.sa_deleted_at IS NULL
            WHERE uc.user_id = p_user_id
              AND uc.app_id = p_app_id
              AND uc.sa_deleted_at IS NULL
              AND (uc.starts_at IS NULL OR uc.starts_at <= now())
              AND (uc.ends_at IS NULL OR uc.ends_at > now())
        ),
        derivations AS (
            SELECT h.user_claim_id, h.claim_id, h.name, a.hop,
                   claimius._effective_access(h.sa_access, co.sa_access) AS e,
                   co.scope, co.sa_owner_id, co.sa_root_id, h.level AS lvl
            FROM anc a
                     JOIN claimius.claim_object co
                          ON co.app_id = p_app_id
                              AND co.object_type = a.object_type
                              AND co.object_id = a.object_id
                              AND co.sa_deleted_at IS NULL
                     JOIN held h ON h.claim_id = co.claim_id
            WHERE a.hop = 0 OR (h.inherits AND co.inherits)

            UNION ALL

            SELECT h.user_claim_id, h.claim_id, h.name, a.hop,
                   CASE WHEN h.level > cb.level THEN claimius._effective_access(h.sa_access, link.sa_access)
                        WHEN cb.level > h.level THEN claimius._effective_access(cb.sa_access, co.sa_access)
                        ELSE (claimius._effective_access(h.sa_access, link.sa_access)
                                & claimius._effective_access(cb.sa_access, co.sa_access) & ~16)
                             | ((claimius._effective_access(h.sa_access, link.sa_access)
                                | claimius._effective_access(cb.sa_access, co.sa_access)) & 16) END,
                   co.scope, co.sa_owner_id, co.sa_root_id, greatest(h.level, cb.level)
            FROM anc a
                     JOIN claimius.claim_object co
                          ON co.app_id = p_app_id
                              AND co.object_type = a.object_type
                              AND co.object_id = a.object_id
                              AND co.sa_deleted_at IS NULL
                     JOIN claimius.claim cb
                          ON cb.id = co.claim_id
                              AND cb.sa_deleted_at IS NULL
                              AND cb.inherits
                     JOIN claimius.claim_object link
                          ON link.object_type = 'claimius.claim'
                              AND link.object_id = co.claim_id
                              AND link.app_id = p_app_id
                              AND link.sa_deleted_at IS NULL
                              AND link.inherits
                     JOIN held h ON h.claim_id = link.claim_id
            WHERE (a.hop = 0 OR co.inherits)
              AND p_object_type <> 'claimius.claim'
        ),
        settled AS (
            SELECT d.*,
                   claimius._close_access(d.e & 15 & ~coalesce((
                       SELECT bit_or(x.e & 15)
                       FROM derivations x
                       WHERE (x.e & 16) <> 0
                         AND x.scope IS NULL
                         AND (x.lvl > d.lvl OR (x.lvl = d.lvl AND (x.hop = 0 OR d.hop > 0)))
                   ), 0)) AS access
            FROM derivations d
            WHERE (d.e & 16) = 0
        )
        SELECT
            claimius.composite_id(p_app_id::TEXT, p_user_id::TEXT, p_object_type, p_object_id::TEXT),
            s.user_claim_id,
            s.claim_id,
            s.name,
            s.access,
            s.scope,
            s.sa_owner_id,
            s.sa_root_id,
            NULL::UUID,
            FALSE,
            s.lvl
        FROM settled s
        WHERE s.access <> 0
          AND (s.access & p_required) = p_required
          AND (jsonb_typeof(s.scope) IS DISTINCT FROM 'object' OR EXISTS (
              SELECT 1
              FROM jsonb_object_keys(s.scope) AS k(field), fields f
              WHERE k.field = ANY(f.names)))
        ORDER BY s.hop ASC, claimius._popcount(s.access) DESC, s.user_claim_id ASC
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

        IF v_has_soft IS NULL THEN
            v_exists := FALSE;
        ELSIF v_has_soft THEN
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
$$ LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_direct_access(UUID, UUID, UUID, TEXT, INTEGER, BOOLEAN) IS 'Locates the closest, strongest user_claim the user holds related to the given object via ancestor walk, directly or through composition, with the binding narrowing its claim and the user''s denies subtracted by class. VOLATILE: it settles through reconcile_if_pending.';

-- ----------------------------------------------------------------------------
-- get_objects
-- Returns object ids of the given object_type whose effective access mask
-- covers all bits in p_required_access. The default 0 returns every object
-- the user has any access to. Rows come in object_id order straight from the
-- reader's range of the user_object primary key: p_after starts the page past
-- the last object_id of the previous one and p_limit bounds it, so a page of
-- p rows reads p index entries and sorts nothing. The mask test reads
-- (sa_access & required) >= required, which for a submask of required is
-- the equality, and which the planner estimates as a range rather than a
-- point, so it keeps the ordered range instead of sorting a bitmap scan. The
-- reader's share settles before the page is read and does not depend on it.
-- The owner, location, name, description and link of each row come from
-- object_info by its key, so a rename shows on every reader's next page.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_objects(UUID, UUID, TEXT, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_objects(
    p_user_id          UUID,
    p_app_id           UUID,
    p_object_type      TEXT,
    p_required_access  INTEGER DEFAULT 0,
    p_after            UUID DEFAULT NULL,
    p_limit            INTEGER DEFAULT NULL
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
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id, p_object_type);

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
            oi.sa_owner_id,
            oi.sa_location_id,
            (SELECT o.sa_root_id FROM claimius.organization o WHERE o.id = oi.sa_owner_id),
            oi.sa_name,
            oi.sa_description,
            oi.sa_link,
            (claimius.user_object.direct_grant AND best.user_claim_id IS NULL)
        FROM claimius.user_object
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_user_id, p_app_id, p_required_access) best ON TRUE
                 LEFT JOIN claimius.object_info oi
                           ON oi.object_type = claimius.user_object.object_type
                          AND oi.object_id = claimius.user_object.object_id
        WHERE claimius.user_object.user_id = p_user_id
          AND claimius.user_object.app_id = p_app_id
          AND claimius.user_object.object_type = p_object_type
          AND claimius.user_object.object_id > coalesce(p_after, '00000000-0000-0000-0000-000000000000'::UUID)
          AND (claimius.user_object.sa_access & p_required_access) >= p_required_access
          AND (best.user_claim_id IS NOT NULL OR claimius.user_object.direct_grant)
        ORDER BY claimius.user_object.object_id
        LIMIT p_limit;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_objects(p_user_id uuid, p_app_id uuid, p_object_type text, p_required_access integer, p_after uuid, p_limit integer) IS 'Object ids and metadata for objects whose mask covers p_required_access, in object_id order after p_after, at most p_limit rows. user_claim_id is the actor token from the strongest surviving claim grant; null when only direct_grant applies. claim_id and claim_name describe the claim that contributed the strongest grant. sa_root_id is derived from the owning organization root.';

-- ----------------------------------------------------------------------------
-- get_organizations
-- Returns organizations whose mask covers p_required_access.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_organizations(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_organizations(
    p_user_id          UUID,
    p_app_id           UUID,
    p_required_access  INTEGER DEFAULT 0,
    p_after            UUID DEFAULT NULL,
    p_limit            INTEGER DEFAULT NULL
) RETURNS SETOF claimius.organization AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id, 'claimius.organization');

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT o.*
        FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.organization', p_required_access, p_after, p_limit) g
            JOIN claimius.organization o ON o.id = g.object_id
        WHERE claimius.app_has_object(p_app_id, 'claimius.organization', o.id)
          AND o.sa_deleted_at IS NULL
        ORDER BY o.id;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_organizations(p_user_id uuid, p_app_id uuid, p_required_access integer, p_after uuid, p_limit integer) IS 'Organizations whose effective mask covers p_required_access, one get_objects page in id order.';

-- ----------------------------------------------------------------------------
-- get_locations
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_locations(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_locations(
    p_user_id          UUID,
    p_app_id           UUID,
    p_required_access  INTEGER DEFAULT 0,
    p_after            UUID DEFAULT NULL,
    p_limit            INTEGER DEFAULT NULL
) RETURNS SETOF claimius.location AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id, 'claimius.location');

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT l.*
        FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.location', p_required_access, p_after, p_limit) g
            JOIN claimius.location l ON l.id = g.object_id
        WHERE claimius.app_has_object(p_app_id, 'claimius.location', l.id)
          AND l.sa_deleted_at IS NULL
        ORDER BY l.id;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_locations(p_user_id uuid, p_app_id uuid, p_required_access integer, p_after uuid, p_limit integer) IS 'Locations whose effective mask covers p_required_access, one get_objects page in id order.';

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
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

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
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_users(UUID, UUID, INTEGER) IS
    'Users the calling user can see in this app whose resolved access covers p_required_access. Resolution merges self (0x2F), member co-visibility via user_users (0x24), and direct claim_object on claimius.samna_user.';

GRANT EXECUTE ON FUNCTION claimius.get_access(UUID, UUID, UUID, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_direct_access(UUID, UUID, UUID, TEXT, INTEGER, BOOLEAN)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_objects(UUID, UUID, TEXT, INTEGER, UUID, INTEGER)
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
-- P0404 when the object is absent or soft deleted, its id is malformed or its
-- type is not registered, so the Go side maps one round trip to 200 / 403 /
-- 404. All three are VOLATILE: get_access and get_direct_access settle the
-- pair through reconcile_if_pending, which writes. resolve_access returns no
-- row for a malformed id, so a batch never aborts on one.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.require_access(UUID, UUID, TEXT, INTEGER);

DROP FUNCTION IF EXISTS claimius.resolve_access(UUID, UUID, UUID, TEXT, INTEGER);
DROP FUNCTION IF EXISTS claimius.resolve_access(UUID, UUID, TEXT, INTEGER);
DROP FUNCTION IF EXISTS claimius.resolve_access(UUID, UUID, TEXT, TEXT, INTEGER);
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
                    direct_grant   BOOLEAN,
                    level          INTEGER
                ) AS $$
BEGIN
    IF p_object_type <> 'claimius.samna_user'
        AND p_object_id !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        RETURN;
    END IF;

    IF p_object_type = 'claimius.audit' THEN
        RETURN QUERY
            SELECT a.id, NULL::UUID, NULL::UUID, 'audit'::TEXT,
                   p_required, NULL::JSONB, a.sa_owner_id,
                   (SELECT o.sa_root_id FROM claimius.organization o WHERE o.id = a.sa_owner_id),
                   NULL::UUID, TRUE, 0
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
                   p_required, NULL::JSONB, p_user_id, p_user_id, NULL::UUID, TRUE, 0
            FROM claimius.user_relation r
            WHERE r.id = p_object_id::UUID
              AND r.user_id = p_user_id
              AND r.sa_deleted_at IS NULL;
        RETURN;
    ELSIF p_object_type = 'claimius.samna_user' THEN
        RETURN QUERY
            SELECT u.target_user_id, u.user_claim_id, u.claim_id, u.claim_name, u.access,
                   NULL::JSONB, COALESCE(u.owner_id, p_user_id), COALESCE(u.root_id, p_user_id),
                   u.location_id, u.direct_grant, 0
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
               g.scope, g.sa_owner_id, g.sa_root_id, g.sa_location_id, g.direct_grant, g.level
        FROM claimius.get_direct_access(p_user_id, p_app_id, p_object_id::UUID, p_object_type, p_required) g;
END;
$$ LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = claimius, pg_temp;
DROP FUNCTION IF EXISTS claimius.resolve_access(UUID, TEXT, TEXT, INTEGER);
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
                    direct_grant   BOOLEAN,
                    level          INTEGER
                ) AS $$
    SELECT * FROM claimius.resolve_access(p_user_id, claimius.get_app_id(), p_object_id, p_object_type, p_required);
$$ LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = claimius, pg_temp;


GRANT EXECUTE ON FUNCTION claimius.resolve_access(UUID, TEXT, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.resolve_access(UUID, UUID, TEXT, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

DROP FUNCTION IF EXISTS claimius.mutation_access(UUID, UUID, TEXT, INTEGER);
DROP FUNCTION IF EXISTS claimius.mutation_access(UUID, TEXT, TEXT, INTEGER);
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
                    direct_grant   BOOLEAN,
                    level          INTEGER
                ) AS $$
DECLARE
    v_app_id UUID := claimius.get_app_id();
    v_uuid   UUID;
BEGIN
    IF p_object_type <> 'claimius.samna_user' AND (
        p_object_id !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
        OR (p_object_type NOT IN ('claimius.audit', 'claimius.user_relation')
            AND NOT EXISTS (SELECT 1 FROM claimius.table_info ti WHERE ti.object_type = p_object_type))
    ) THEN
        RAISE EXCEPTION 'not found' USING ERRCODE = 'P0404';
    END IF;

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
$$ LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.mutation_access(UUID, TEXT, TEXT, INTEGER) IS 'Single object access gate. Resolves access through get_access then get_direct_access, returns the strongest AccessClaim row, and RAISEs SQLSTATE P0403 when the object exists but the caller lacks the required access, P0404 when the object is absent or soft deleted, the id is malformed or the type is not registered. SQL embodiment of the Go MutationAccess resolution in one round trip. VOLATILE: it settles through reconcile_if_pending.';

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
          AND EXISTS (
              SELECT 1 FROM claimius.user_claim uc
              WHERE uc.claim_id = c.id
                AND uc.user_id = p_user_id
                AND uc.app_id = p_app_id
                AND uc.sa_deleted_at IS NULL
                AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                AND (uc.ends_at IS NULL OR uc.ends_at > now())
                AND (c.sa_access & p_required_access) = p_required_access
          )
        ORDER BY claimius._popcount(c.sa_access) DESC;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_claims(UUID, UUID, INTEGER) IS 'The claims the user holds through a live user_claim, at the required level. The claims the user can see or manage, held or reached through a cascade, are get_objects of type claimius.claim.';

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
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id, p_object_type, p_object_id);

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
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_claims(UUID, UUID, UUID, TEXT) IS 'All claims the user holds against one object.';

-- ----------------------------------------------------------------------------
-- get_scope_access
-- The object-less bindings of the user's live grants in the app whose scope
-- matches a jsonpath, one row per binding. A binding carries its claim mask
-- narrowed by its own, so a NULL binding mask is the claim mask. A deny
-- binding matching the same path subtracts its bits from every other, and
-- what remains is closed under implication. A row whose mask misses
-- p_required_access is dropped. It reads no user_object and settles nothing.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_scope_access(UUID, UUID, JSONPATH, INTEGER);
CREATE OR REPLACE FUNCTION claimius.get_scope_access(
    p_user_id          UUID,
    p_app_id           UUID,
    p_path             JSONPATH,
    p_required_access  INTEGER DEFAULT 0
) RETURNS TABLE (
                    user_claim_id   UUID,
                    claim_id        UUID,
                    claim_name      TEXT,
                    sa_access       INTEGER,
                    sa_owner_id     UUID,
                    sa_root_id      UUID,
                    object_row_id   UUID,
                    object_scope    JSONB,
                    object_access   INTEGER,
                    object_inherits BOOLEAN,
                    level           INTEGER
                ) LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp AS $$
    WITH bound AS (
        SELECT uc.id AS held_id,
               c.id AS held_claim_id,
               c.name AS held_claim_name,
               claimius._effective_access(c.sa_access, co.sa_access) AS e,
               co.sa_owner_id AS binding_owner_id,
               co.sa_root_id AS binding_root_id,
               co.id AS binding_id,
               co.scope AS binding_scope,
               co.sa_access AS binding_access,
               co.inherits AS binding_inherits,
               c.level AS lvl
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
          AND jsonb_path_exists(co.scope, p_path)
    ),
    settled AS (
        SELECT b.*, claimius._close_access(b.e & 15 & ~coalesce((
                   SELECT bit_or(x.e & 15)
                   FROM bound x
                   WHERE (x.e & 16) <> 0
                     AND x.lvl >= b.lvl
               ), 0)) AS access
        FROM bound b
        WHERE (b.e & 16) = 0
    )
    SELECT s.held_id, s.held_claim_id, s.held_claim_name, s.access, s.binding_owner_id, s.binding_root_id,
           s.binding_id, s.binding_scope, s.binding_access, s.binding_inherits, s.lvl
    FROM settled s
    WHERE s.access <> 0
      AND (s.access & p_required_access) = p_required_access
    ORDER BY s.held_claim_id, s.binding_id
$$;

COMMENT ON FUNCTION claimius.get_scope_access(UUID, UUID, JSONPATH, INTEGER) IS 'Object-less bindings of the user''s live grants whose scope matches a jsonpath, one row per binding, each mask the claim mask narrowed by the binding mask, less the bits of the deny bindings matching the same path at the level of its claim or above, closed under implication.';

DROP FUNCTION IF EXISTS claimius.get_scope_access(UUID, JSONPATH, INTEGER);
CREATE OR REPLACE FUNCTION claimius.get_scope_access(
    p_user_id          UUID,
    p_path             JSONPATH,
    p_required_access  INTEGER DEFAULT 0
) RETURNS TABLE (
                    user_claim_id   UUID,
                    claim_id        UUID,
                    claim_name      TEXT,
                    sa_access       INTEGER,
                    sa_owner_id     UUID,
                    sa_root_id      UUID,
                    object_row_id   UUID,
                    object_scope    JSONB,
                    object_access   INTEGER,
                    object_inherits BOOLEAN,
                    level           INTEGER
                ) LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp AS $$
    SELECT * FROM claimius.get_scope_access(p_user_id, claimius.get_app_id(), p_path, p_required_access);
$$;

COMMENT ON FUNCTION claimius.get_scope_access(UUID, JSONPATH, INTEGER) IS 'get_scope_access in the system app.';

-- ----------------------------------------------------------------------------
-- get_access (overload: scope key form)
-- Strongest active object-less binding whose scope contains p_scope_key and
-- whose mask covers p_required_access. Returns user_claim_id, claim_id,
-- sa_access, scope. It is get_scope_access at the path of the key.
-- It reads claim, user_claim and the object-less bindings, never user_object
-- or an area a diff can hold open, so it settles nothing.
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
    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT s.user_claim_id, s.claim_id, s.sa_access, s.object_scope
        FROM claimius.get_scope_access(p_user_id, p_app_id, ('$.' || to_jsonb(p_scope_key)::TEXT)::JSONPATH, p_required_access) s
        ORDER BY claimius._popcount(s.sa_access) DESC, s.claim_id
        LIMIT 1;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_access(UUID, UUID, TEXT, INTEGER) IS 'Strongest active object-less binding whose scope contains p_scope_key and whose mask covers p_required_access. A NULL binding mask inherits the claim mask, and a deny binding carrying the key subtracts its bits.';

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
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_audit(p_user_id uuid, p_app_id uuid, p_required_access integer, p_limit integer, p_offset integer) IS 'Audit rows for organizations the user can see.';

-- ----------------------------------------------------------------------------
-- get_secrets
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_secrets(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_secrets(
    p_user_id          UUID,
    p_app_id           UUID,
    p_required_access  INTEGER DEFAULT 0,
    p_after            UUID DEFAULT NULL,
    p_limit            INTEGER DEFAULT NULL
) RETURNS SETOF claimius.samna_secret AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT s.*
        FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.samna_secret', p_required_access, p_after, p_limit) g
            JOIN claimius.samna_secret s ON s.id = g.object_id
        WHERE s.app_id = p_app_id
          AND s.sa_deleted_at IS NULL
        ORDER BY s.id;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_secrets(p_user_id uuid, p_app_id uuid, p_required_access integer, p_after uuid, p_limit integer) IS 'Secrets whose effective mask covers p_required_access, one get_objects page in id order.';

-- Drop any stale earlier signatures of the graph functions before redefining.
-- The previous version of these functions had different argument lists; if a
-- migration re-runs against a database that still has the old signature, the
-- bare CREATE OR REPLACE below would create a second overloaded version
-- instead of replacing, and downstream COMMENT/REVOKE statements would not
-- resolve unambiguously.
DROP FUNCTION IF EXISTS claimius.get_owner_graph(UUID, UUID, INTEGER);
DROP FUNCTION IF EXISTS claimius.get_claim_graph(UUID, UUID, INTEGER);

-- ----------------------------------------------------------------------------
-- _window_graph
-- The window of a graph: the nodes of p_rows kept when every parent shows at
-- most p_width of its children of each type, 0 meaning all of them. A node
-- whose parent is not in the graph is a top node, and the top nodes are the
-- children of no parent. Children come in name order, label then id, or
-- with p_sort 'usage' by the reader's decayed usage of the pair, heaviest
-- first, then name. A
-- node is kept when it is inside its parent's window and its parent is kept.
-- With p_next_id the window continues after that node: its later siblings of
-- its type, p_width of them, with their windowed subtrees. The node itself
-- is not returned, and an id not in the graph returns nothing.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius._window_graph(
    p_rows      JSONB,
    p_user_id   UUID,
    p_app_id    UUID,
    p_width     INTEGER,
    p_sort      TEXT,
    p_next_id   UUID
) RETURNS TABLE(object_id UUID, object_type TEXT, label TEXT, parent_id UUID, data JSONB) AS $$
    WITH RECURSIVE node AS (
        SELECT e.ord,
               (e.value ->> 'object_id')::UUID AS object_id,
               e.value ->> 'object_type' AS object_type,
               e.value ->> 'label' AS label,
               NULLIF(e.value ->> 'parent_id', '')::UUID AS parent_id,
               e.value -> 'data' AS data
        FROM jsonb_array_elements(coalesce(p_rows, '[]'::JSONB)) WITH ORDINALITY AS e(value, ord)
    ),
    ordered AS (
        SELECT n.*,
               p.parent_key,
               row_number() OVER (
                   PARTITION BY p.parent_key, n.object_type
                   ORDER BY CASE WHEN p_sort = 'usage' THEN claimius._decayed_usage(uo.used_count, uo.last_used_at) ELSE 0 END DESC,
                            n.label, n.object_id, n.ord
               ) AS pos
        FROM node n
            CROSS JOIN LATERAL (
                SELECT CASE WHEN EXISTS (SELECT 1 FROM node x WHERE x.object_id = n.parent_id) THEN n.parent_id END AS parent_key
            ) p
            LEFT JOIN claimius.user_object uo
                ON p_sort = 'usage'
               AND uo.user_id = p_user_id
               AND uo.app_id = p_app_id
               AND uo.object_type = n.object_type
               AND uo.object_id = n.object_id
    ),
    anchor AS (
        SELECT o.parent_key, o.object_type, o.pos
        FROM ordered o
        WHERE o.object_id = p_next_id
        ORDER BY o.ord
        LIMIT 1
    ),
    kept AS (
        SELECT o.ord, o.object_id
        FROM ordered o
        WHERE CASE
                  WHEN p_next_id IS NULL THEN o.parent_key IS NULL AND (p_width = 0 OR o.pos <= p_width)
                  ELSE EXISTS (
                      SELECT 1 FROM anchor a
                      WHERE a.parent_key IS NOT DISTINCT FROM o.parent_key
                        AND a.object_type = o.object_type
                        AND o.pos > a.pos
                        AND (p_width = 0 OR o.pos <= a.pos + p_width))
              END

        UNION

        SELECT c.ord, c.object_id
        FROM kept k
            JOIN ordered c ON c.parent_key = k.object_id
        WHERE p_width = 0 OR c.pos <= p_width
    )
    SELECT o.object_id, o.object_type, o.label, o.parent_id, o.data
    FROM ordered o
    WHERE o.ord IN (SELECT k.ord FROM kept k)
    ORDER BY o.ord
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius._window_graph(JSONB, UUID, UUID, INTEGER, TEXT, UUID) IS 'The nodes of a graph kept when each parent shows at most p_width children of each type, 0 meaning all, in name order or by the reader''s decayed usage with p_sort ''usage'', continuing after p_next_id when given.';

-- ----------------------------------------------------------------------------
-- _window_links
-- The window of a link graph, the same window _window_graph gives a node
-- graph: every link target is a node whose parent is the link source, and a
-- source no link targets is a top node. A link is kept when its target is
-- kept under its source. Types are matched back to the registered type they
-- normalize from, labels come from object_info, the claim or the samna_user
-- row, so name and usage order read as they do on the nodes. p_nodes names
-- the graph's nodes by short type and id, and one no link targets is a top
-- node too, so a root or a start that keeps no child is windowed like any
-- other. Returns the kept links and node_ids, the ids of every kept node, the
-- object set of the window.
-- ----------------------------------------------------------------------------
DROP FUNCTION IF EXISTS claimius._window_links(JSONB, UUID, UUID, INTEGER, TEXT, UUID);
CREATE OR REPLACE FUNCTION claimius._window_links(
    p_links     JSONB,
    p_nodes     JSONB,
    p_user_id   UUID,
    p_app_id    UUID,
    p_width     INTEGER,
    p_sort      TEXT,
    p_next_id   UUID
) RETURNS JSONB AS $$
    WITH link AS (
        SELECT l.value AS link,
               l.value ->> 'source_type' AS source_type,
               (l.value ->> 'source_id')::UUID AS source_id,
               l.value ->> 'target_type' AS target_type,
               (l.value ->> 'target_id')::UUID AS target_id
        FROM jsonb_array_elements(coalesce(p_links, '[]'::JSONB)) AS l
    ),
    node AS (
        SELECT DISTINCT target_type AS short_type, target_id AS object_id, source_id AS parent_id FROM link
        UNION
        SELECT DISTINCT s.source_type, s.source_id, NULL::UUID FROM link s
        WHERE NOT EXISTS (SELECT 1 FROM link t WHERE t.target_id = s.source_id AND t.target_type = s.source_type)
        UNION
        SELECT DISTINCT n.value ->> 'object_type', (n.value ->> 'object_id')::UUID, NULL::UUID
        FROM jsonb_array_elements(coalesce(p_nodes, '[]'::JSONB)) AS n
        WHERE NOT EXISTS (
            SELECT 1 FROM link t
            WHERE t.target_id = (n.value ->> 'object_id')::UUID AND t.target_type = n.value ->> 'object_type')
    ),
    typed AS (
        SELECT n.short_type, n.object_id, n.parent_id,
               coalesce((SELECT ti.object_type FROM claimius.table_info ti
                         WHERE claimius.normalize_object_type(ti.object_type) = n.short_type
                         ORDER BY ti.object_type LIMIT 1), n.short_type) AS object_type
        FROM node n
    ),
    kept AS (
        SELECT w.object_id, w.object_type, w.parent_id
        FROM claimius._window_graph(
            (SELECT coalesce(jsonb_agg(jsonb_build_object(
                        'object_id', t.object_id,
                        'object_type', t.object_type,
                        'label', coalesce(
                            (SELECT oi.sa_name FROM claimius.object_info oi WHERE oi.object_type = t.object_type AND oi.object_id = t.object_id),
                            (SELECT c.name FROM claimius.claim c WHERE t.object_type = 'claimius.claim' AND c.id = t.object_id),
                            (SELECT concat_ws(' ', su.first_name, su.last_name) FROM claimius.samna_user su
                             WHERE t.object_type = 'claimius.samna_user' AND su.id = t.object_id),
                            t.object_id::TEXT),
                        'parent_id', t.parent_id)), '[]'::JSONB)
             FROM typed t),
            p_user_id, p_app_id, p_width, p_sort, p_next_id) w
    )
    SELECT jsonb_build_object(
        'links', (
            SELECT coalesce(jsonb_agg(l.link), '[]'::JSONB)
            FROM link l
            WHERE EXISTS (
                SELECT 1 FROM kept k JOIN typed t ON t.object_id = k.object_id AND t.object_type = k.object_type
                WHERE t.short_type = l.target_type
                  AND k.object_id = l.target_id
                  AND k.parent_id IS NOT DISTINCT FROM l.source_id)),
        'node_ids', (
            SELECT coalesce(jsonb_agg(DISTINCT to_jsonb(k.object_id)), '[]'::JSONB)
            FROM kept k))
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius._window_links(JSONB, JSONB, UUID, UUID, INTEGER, TEXT, UUID) IS 'The links of a link graph kept by the window _window_graph gives its nodes, a link staying when its target is kept under its source, with node_ids, the ids of every kept node, p_nodes no link targets windowed as top nodes.';


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
--   p_width, p_sort, p_next_id - the window of _window_graph: at most p_width
--                 children of each type per parent, 0 meaning all, in name or
--                 usage order, continuing after p_next_id.
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
    v_rows          JSONB;
BEGIN
    IF p_sort IS NULL OR p_sort NOT IN ('name', 'usage') OR p_width IS NULL OR p_width < 0 THEN
        RAISE EXCEPTION 'graph window: p_sort is name or usage and p_width at least 0' USING ERRCODE = '22023';
    END IF;

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
    INSERT INTO _owner_graph_rows (object_id, object_type, label, parent_id, data)
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
    INSERT INTO _owner_graph_rows (object_id, object_type, label, parent_id, data)
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
                   coalesce(oi.sa_name, t.id::TEXT)::TEXT,
                   CASE
                       %s
                       %s
                       %s
                       ELSE NULL::UUID
                   END,
                   jsonb_build_object(
                       'access', uo.sa_access, 'scope', uo.scope,
                       'level', NULL::INTEGER,
                       'description', oi.sa_description,
                       'owner_id', oi.sa_owner_id,
                       'location_id', oi.sa_location_id
                   )
            FROM %I.%I t
            JOIN claimius.user_object uo
              ON uo.object_id = t.id
             AND uo.object_type = %L
             AND uo.user_id = $1
             AND uo.app_id = $2
             AND (uo.sa_access & $3) = $3
            LEFT JOIN claimius.object_info oi
              ON oi.object_type = %L
             AND oi.object_id = t.id
            WHERE %s
              AND (
                  %s
                  %s
              )
            ORDER BY oi.sa_owner_id, oi.sa_name, t.id
        $f$,
                            v_table.object_type,
                -- parent_id CASE arms in priority order: parent, location, owner
                            CASE WHEN v_table.has_sa_parent_id THEN 'WHEN t.sa_parent_id IS NOT NULL AND t.sa_parent_id <> t.id THEN t.sa_parent_id' ELSE '' END,
                            CASE WHEN v_table.has_sa_location_id THEN 'WHEN t.sa_location_id IS NOT NULL THEN t.sa_location_id' ELSE '' END,
                            CASE WHEN v_table.has_sa_owner_id THEN 'WHEN t.sa_owner_id IS NOT NULL AND t.sa_owner_id <> t.id THEN t.sa_owner_id' ELSE '' END,
                            split_part(v_table.object_type, '.', 1),
                            split_part(v_table.object_type, '.', 2),
                            v_table.object_type,
                            v_table.object_type,
                -- soft-delete predicate (table dependent)
                            CASE WHEN v_table.has_sa_deleted_at THEN 't.sa_deleted_at IS NULL' ELSE 'TRUE' END,
                -- ancestry: must root in the walked org set (via owner or via owner of location)
                            CASE WHEN v_table.has_sa_owner_id THEN 't.sa_owner_id IN (SELECT org_id FROM _owner_graph_orgs)' ELSE 'FALSE' END,
                            CASE WHEN v_table.has_sa_location_id THEN ' OR EXISTS (SELECT 1 FROM claimius.location loc WHERE loc.id = t.sa_location_id AND loc.sa_owner_id IN (SELECT org_id FROM _owner_graph_orgs))' ELSE '' END
                     );

            EXECUTE 'INSERT INTO _owner_graph_rows (object_id, object_type, label, parent_id, data) ' || v_sql USING p_user_id, p_app_id, p_required_access;
        END LOOP;

    IF p_width = 0 AND p_next_id IS NULL THEN
        RETURN QUERY SELECT r.object_id, r.object_type, r.label, r.parent_id, r.data FROM _owner_graph_rows r;
        RETURN;
    END IF;

    SELECT jsonb_agg(to_jsonb(r)) INTO v_rows FROM _owner_graph_rows r;
    RETURN QUERY SELECT * FROM claimius._window_graph(v_rows, p_user_id, p_app_id, p_width, p_sort, p_next_id);
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_owner_graph(UUID, UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) IS 'Ownership graph (orgs, locations, registered tables with sa_owner_id or sa_location_id) for the user. Optional start org and depth limit, and a window of p_width children of each type per parent in name or usage order continuing after p_next_id.';

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
--   p_width, p_sort, p_next_id - the window of _window_graph: at most p_width
--                 children of each type per parent, 0 meaning all, in name or
--                 usage order, continuing after p_next_id.
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
    v_snapshot      PG_SNAPSHOT;
BEGIN
    IF p_sort IS NULL OR p_sort NOT IN ('name', 'usage') OR p_width IS NULL OR p_width < 0 THEN
        RAISE EXCEPTION 'graph window: p_sort is name or usage and p_width at least 0' USING ERRCODE = '22023';
    END IF;

    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    v_cacheable := (p_start_id IS NULL AND p_depth = 0 AND p_required_access = 0);

    IF v_cacheable THEN
        SELECT c.graph INTO v_graph
        FROM claimius.claim_graph_cache c
        WHERE c.app_id = p_app_id
          AND c.user_id = p_user_id
          AND NOT EXISTS (
              SELECT 1
              FROM claimius.user_claim uc
              WHERE uc.user_id = p_user_id
                AND uc.app_id = p_app_id
                AND (NOT pg_visible_in_snapshot(uc.txid, c.snapshot)
                     OR EXISTS (
                         SELECT 1 FROM claimius.claim cl
                         WHERE cl.id = uc.claim_id AND NOT pg_visible_in_snapshot(cl.txid, c.snapshot))
                     OR EXISTS (
                         SELECT 1 FROM claimius.claim_object co
                         WHERE co.claim_id = uc.claim_id AND co.app_id = p_app_id AND NOT pg_visible_in_snapshot(co.txid, c.snapshot))));
    END IF;

    IF v_graph IS NULL THEN
        v_snapshot := pg_current_snapshot();
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
                   oi.sa_name            AS uo_name,
                   oi.sa_description     AS uo_description,
                   oi.sa_owner_id        AS uo_owner_id,
                   oi.sa_location_id     AS uo_location_id,
                   (e->>'claim_id')::UUID  AS claim_id,
                   e->>'tree_type'         AS tree_type,
                   CASE WHEN e->>'tree_type' = 'direct'
                        THEN uo.object_type
                        ELSE e -> 'cascaded_from' ->> 'type' END  AS anchor_type,
                   CASE WHEN e->>'tree_type' = 'direct'
                        THEN uo.object_id
                        ELSE (e -> 'cascaded_from' ->> 'id')::UUID END  AS anchor_id
            FROM claimius.user_object uo
                     LEFT JOIN claimius.object_info oi
                          ON oi.object_type = uo.object_type
                              AND oi.object_id = uo.object_id
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
                   claimius._effective_access(c.sa_access, co.sa_access) AS access,
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
                   coalesce(oi.sa_name, co.object_id::TEXT, '')::TEXT  AS label,
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
                     LEFT JOIN claimius.object_info oi
                          ON oi.object_type = uo.object_type
                              AND oi.object_id = uo.object_id
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
                   coalesce(oi.sa_name, r.object_id::TEXT)::TEXT  AS label,
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
                     LEFT JOIN claimius.object_info oi
                          ON oi.object_type = uo.object_type
                              AND oi.object_id = uo.object_id
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
            INSERT INTO claimius.claim_graph_cache (app_id, user_id, graph, snapshot)
            VALUES (p_app_id, p_user_id, v_graph, v_snapshot)
            ON CONFLICT (app_id, user_id) DO UPDATE
                SET graph = EXCLUDED.graph,
                    snapshot = EXCLUDED.snapshot;
        END IF;
    END IF;

    IF p_width = 0 AND p_next_id IS NULL THEN
        RETURN QUERY
            SELECT (e->>'object_id')::UUID,
                   e->>'object_type',
                   e->>'label',
                   NULLIF(e->>'parent_id', '')::UUID,
                   e->'data'
            FROM jsonb_array_elements(v_graph) AS e;
        RETURN;
    END IF;

    RETURN QUERY SELECT * FROM claimius._window_graph(v_graph, p_user_id, p_app_id, p_width, p_sort, p_next_id);
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_claim_graph(UUID, UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) IS 'Access graph: user, other users seen, claims, user_claims, claim_object bindings, reachable objects. Optional start claim and depth limit, and a window of p_width children of each type per parent in name or usage order continuing after p_next_id. Default shape served from claimius.claim_graph_cache while every grant, claim and binding of the user was written by a transaction visible in the snapshot taken before the cached graph was built.';

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
    ELSIF TG_TABLE_NAME = 'object_info' THEN
        DELETE FROM claimius.claim_graph_cache c
        USING claimius.samna_app a
            JOIN claimius.user_object uo
                ON uo.app_id = a.id
               AND uo.object_type = NEW.object_type
               AND uo.object_id = NEW.object_id
        WHERE c.app_id = uo.app_id AND c.user_id = uo.user_id;
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._invalidate_claim_graph_cache() IS 'Trigger function. Drops claim_graph_cache rows affected by writes to user_object, object_info, user_users, samna_user. A claim or binding change is caught on read by get_claim_graph.';

-- ----------------------------------------------------------------------------
-- search
-- Full text search over accessible objects (anything with a user_object row),
-- gated by access. Matches against the search_vector of the object's one
-- object_info row, generated from its name and description, so a rename is
-- searchable by every reader at once.
-- A row ranks by its text match plus the reader's decayed usage of the
-- pair, so among equal matches what the reader uses most and lately comes
-- first.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.search_objects(UUID, UUID, TEXT, TEXT[], INTEGER, INTEGER, INTEGER);
DROP FUNCTION IF EXISTS claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, UUID[], UUID[], INTEGER, INTEGER);

DROP FUNCTION IF EXISTS claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, UUID[], UUID[], UUID[], UUID, TEXT, UUID[], INTEGER, INTEGER);
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
            oi.sa_name,
            oi.sa_description,
            oi.sa_link,
            claimius.user_object.sa_access,
            claimius.user_object.scope,
            oi.sa_owner_id,
            oi.sa_location_id,
            (claimius.user_object.direct_grant AND best.user_claim_id IS NULL),
            (ts_rank(oi.search_vector, v_tsquery) + claimius._decayed_usage(claimius.user_object.used_count, claimius.user_object.last_used_at))::REAL
        FROM claimius.user_object
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_user_id, p_app_id, p_required_access) best ON TRUE
                 JOIN claimius.object_info oi
                      ON oi.object_type = claimius.user_object.object_type
                     AND oi.object_id = claimius.user_object.object_id
        WHERE claimius.user_object.user_id = p_user_id
          AND claimius.user_object.app_id = p_app_id
          AND (claimius.user_object.sa_access & p_required_access) = p_required_access
          AND oi.search_vector @@ v_tsquery
          AND (p_object_types IS NULL OR claimius.user_object.object_type = ANY(p_object_types))
          AND (best.user_claim_id IS NOT NULL OR claimius.user_object.direct_grant)
        ORDER BY 12 DESC, 1
        LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

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
                              OR p_parent_id IS NOT NULL;
    v_parent_tree    claimius.tree_type;
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    v_tsquery := websearch_to_tsquery('simple', p_query);

    IF p_parent_type = 'claimius.location' THEN
        v_parent_tree := 'location'::claimius.tree_type;
    ELSIF p_parent_type IS NOT NULL AND EXISTS (
        SELECT 1 FROM claimius.table_info ti WHERE ti.object_type = p_parent_type AND ti.has_sa_parent_id
    ) THEN
        v_parent_tree := 'parenthood'::claimius.tree_type;
    END IF;

    RETURN QUERY
    WITH combined AS (
        SELECT
            claimius.user_object.object_id           AS object_id,
            claimius.user_object.object_type         AS object_type,
            best.user_claim_id                       AS user_claim_id,
            oi.sa_name             AS name,
            oi.sa_description      AS description,
            oi.sa_link             AS link,
            claimius.user_object.sa_access           AS sa_access,
            claimius.user_object.scope               AS scope,
            oi.sa_owner_id         AS owner_id,
            oi.sa_location_id      AS location_id,
            (claimius.user_object.direct_grant AND best.user_claim_id IS NULL) AS is_direct_grant,
            (ts_rank(oi.search_vector, v_tsquery) + claimius._decayed_usage(claimius.user_object.used_count, claimius.user_object.last_used_at))::REAL AS rank
        FROM claimius.user_object
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_user_id, p_app_id, p_required_access) best ON TRUE
                 JOIN claimius.object_info oi
                      ON oi.object_type = claimius.user_object.object_type
                     AND oi.object_id = claimius.user_object.object_id
        WHERE v_include_others
          AND claimius.user_object.user_id = p_user_id
          AND claimius.user_object.app_id = p_app_id
          AND (claimius.user_object.sa_access & p_required_access) = p_required_access
          AND oi.search_vector @@ v_tsquery
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
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, UUID[], UUID[], UUID[], UUID, TEXT, INTEGER, INTEGER) IS 'Full text search over accessible objects. Includes claimius.claim rows the user holds, skipped when any subtree or parent filter is set. p_owner_subtree_ids scopes via ownership tree. p_parent_id and p_parent_type scope via location or parenthood tree. total_count carries the un-capped match count for pagination. Rows tie-break on object_id so offset pagination tiles without overlap or skip.';

GRANT EXECUTE ON FUNCTION claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, INTEGER, INTEGER) TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, UUID[], UUID[], UUID[], UUID, TEXT, INTEGER, INTEGER) TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

-- ----------------------------------------------------------------------------
-- touch_object_usage
-- Adds counted checks to the pairs they were made on, one call per flush of
-- the middleware's in process counter. Row i of the arrays is one pair and the
-- checks counted on it; repeats of a pair add up. A pair that is not settled,
-- that has no user_object row, is skipped, so usage never makes a pair and
-- never outlives one. The touched pairs are locked in key order first, so two
-- replicas flushing the same pairs wait on each other instead of deadlocking.
-- Every user and app counted moves its user_activity row to now. Nothing here
-- publishes to sync_event: usage is local to this database. Returns the pairs
-- touched.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius.touch_object_usage(
    p_user_ids      UUID[],
    p_app_ids       UUID[],
    p_object_types  TEXT[],
    p_object_ids    UUID[],
    p_counts        BIGINT[]
) RETURNS INTEGER AS $$
DECLARE
    v_touched   INTEGER;
BEGIN
    WITH used AS MATERIALIZED (
        SELECT u.user_id, u.app_id, u.object_type, u.object_id, sum(u.n) AS n
        FROM unnest(p_user_ids, p_app_ids, p_object_types, p_object_ids, p_counts) AS u(user_id, app_id, object_type, object_id, n)
        WHERE u.user_id IS NOT NULL
          AND u.app_id IS NOT NULL
          AND u.object_type IS NOT NULL
          AND u.object_id IS NOT NULL
          AND u.n > 0
        GROUP BY u.user_id, u.app_id, u.object_type, u.object_id
    ),
    locked AS (
        SELECT uo.user_id, uo.app_id, uo.object_type, uo.object_id, used.n
        FROM claimius.user_object uo
            JOIN used
                ON used.user_id = uo.user_id
               AND used.app_id = uo.app_id
               AND used.object_type = uo.object_type
               AND used.object_id = uo.object_id
        ORDER BY uo.user_id, uo.app_id, uo.object_type, uo.object_id
        FOR NO KEY UPDATE OF uo
    ),
    touched AS (
        UPDATE claimius.user_object uo
        SET used_count = uo.used_count + l.n,
            last_used_at = now()
        FROM locked l
        WHERE uo.user_id = l.user_id
          AND uo.app_id = l.app_id
          AND uo.object_type = l.object_type
          AND uo.object_id = l.object_id
        RETURNING uo.user_id
    ),
    active AS (
        INSERT INTO claimius.user_activity (user_id, app_id, last_used_at)
        SELECT DISTINCT used.user_id, used.app_id, now()
        FROM used
        ORDER BY used.user_id, used.app_id
        ON CONFLICT (user_id, app_id) DO UPDATE
            SET last_used_at = EXCLUDED.last_used_at
        RETURNING user_id
    )
    SELECT count(*) INTO v_touched FROM touched;

    RETURN v_touched;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.touch_object_usage(UUID[], UUID[], TEXT[], UUID[], BIGINT[]) IS 'Adds counted checks to user_object.used_count and moves last_used_at, per (user, app, type, object) row of the arrays, skipping pairs not settled, and moves each counted user''s user_activity for the app. Local, never synced. Returns the pairs touched.';

CREATE OR REPLACE FUNCTION claimius.touch_object_usage(
    p_user_ids      UUID[],
    p_object_types  TEXT[],
    p_object_ids    UUID[],
    p_counts        BIGINT[]
) RETURNS INTEGER AS $$
    SELECT claimius.touch_object_usage(
        p_user_ids,
        array_fill(claimius.get_app_id(), ARRAY[coalesce(cardinality(p_user_ids), 0)]),
        p_object_types, p_object_ids, p_counts);
$$ LANGUAGE sql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.touch_object_usage(UUID[], TEXT[], UUID[], BIGINT[]) IS 'touch_object_usage for the deployment''s own app, get_app_id().';

GRANT EXECUTE ON FUNCTION claimius.touch_object_usage(UUID[], UUID[], TEXT[], UUID[], BIGINT[]) TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.touch_object_usage(UUID[], TEXT[], UUID[], BIGINT[]) TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

-- ============================================================================
-- Write functions: claim management
-- ============================================================================

-- insert_claim
DROP FUNCTION IF EXISTS claimius.create_claim(UUID, TEXT, TEXT, INTEGER, UUID, UUID, UUID, BOOLEAN, BOOLEAN, claimius.claim_type);
DROP FUNCTION IF EXISTS claimius.create_claim(UUID, TEXT, TEXT, INTEGER, UUID, UUID, UUID, BOOLEAN, claimius.claim_type);

DROP FUNCTION IF EXISTS claimius.create_claim(uuid,text,text,integer,uuid,uuid,uuid,boolean,claimius.claim_type);
DROP FUNCTION IF EXISTS claimius.insert_claim(uuid,text,text,integer,uuid,uuid,uuid,boolean,claimius.claim_type);
CREATE OR REPLACE FUNCTION claimius.insert_claim(
    p_app_id            UUID,
    p_name              TEXT,
    p_description       TEXT,
    p_sa_access         INTEGER,
    p_sa_owner_id       UUID,
    p_sa_root_id        UUID,
    p_sa_created_by     UUID,
    p_inherits          BOOLEAN DEFAULT TRUE,
    p_type              claimius.claim_type DEFAULT 'user',
    p_level             INTEGER DEFAULT 0
) RETURNS JSONB AS $$
DECLARE
    v_row    claimius.claim;
    v_access INTEGER := p_sa_access;
BEGIN
    IF coalesce(v_access, 0) & ~16 = 0 THEN
        RAISE EXCEPTION 'claim mask % carries no bit besides deny', v_access USING ERRCODE = '22023';
    END IF;

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
        sa_owner_id, sa_root_id, sa_created_by, level
    ) VALUES (
                 p_app_id, p_name, p_description, v_access, p_inherits, p_type,
                 p_sa_owner_id, p_sa_root_id, p_sa_created_by, coalesce(p_level, 0)
             ) RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.insert_claim(p_app_id uuid, p_name text, p_description text, p_sa_access integer, p_sa_owner_id uuid, p_sa_root_id uuid, p_sa_created_by uuid, p_inherits boolean, p_type claimius.claim_type, p_level integer) IS 'Creates a new claim. p_sa_access is a bitwise mask: 0x01 owner, 0x02 write, 0x04 read, 0x08 execute, 0x10 deny, 0x20 member. On grant claims with the deny bit unset owner implies write+read+execute, write implies read+execute, execute implies read. Member bit 0x20 stands alone. Deny claims store the supplied bits verbatim so callers can deny any subset of read, write, execute, owner. A mask with no bit besides deny is refused with SQLSTATE 22023.';

-- update_claim
DROP FUNCTION IF EXISTS claimius.update_claim(UUID, TEXT, TEXT, INTEGER, BOOLEAN, BOOLEAN);
DROP FUNCTION IF EXISTS claimius.update_claim(UUID, TEXT, TEXT, INTEGER, BOOLEAN);
DROP FUNCTION IF EXISTS claimius.update_claim(uuid,text,text,integer,boolean);

CREATE OR REPLACE FUNCTION claimius.update_claim(
    p_claim_id          UUID,
    p_name              TEXT DEFAULT NULL,
    p_description       TEXT DEFAULT NULL,
    p_sa_access         INTEGER DEFAULT NULL,
    p_inherits          BOOLEAN DEFAULT NULL,
    p_level             INTEGER DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row    claimius.claim;
    v_access INTEGER := p_sa_access;
BEGIN
    IF v_access IS NOT NULL AND v_access <> 0 AND v_access & ~16 = 0 THEN
        RAISE EXCEPTION 'claim mask % carries no bit besides deny', v_access USING ERRCODE = '22023';
    END IF;

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
                              inherits    = coalesce(p_inherits, inherits),
                              level       = coalesce(p_level, level)
    WHERE id = p_claim_id
    RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_claim(p_claim_id uuid, p_name text, p_description text, p_sa_access integer, p_inherits boolean, p_level integer) IS 'Patches selected fields on a claim. p_sa_access is expanded on grant claims so owner implies write+read+execute, write implies read+execute, execute implies read. Member bit 0x20 stands alone. Deny claims keep the supplied bits verbatim. Empty p_name and zero p_sa_access are treated as preserve so PATCH callers that send only one field do not wipe the others. A mask with no bit besides deny is refused with SQLSTATE 22023.';

-- delete_claim
DROP FUNCTION IF EXISTS claimius.remove_claim(UUID, UUID);

DROP FUNCTION IF EXISTS claimius.remove_claim(uuid,uuid);
CREATE OR REPLACE FUNCTION claimius.delete_claim(
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

COMMENT ON FUNCTION claimius.delete_claim(p_claim_id uuid, p_deleted_by uuid) IS 'Soft deletes a claim, its user_claim, its claim_object, and any inbound composition links pointing at it. Returns the deleted claim row.';

-- insert_user_claim
DROP FUNCTION IF EXISTS claimius.assign_claim_user(uuid,uuid,uuid,uuid,uuid,text,timestamp with time zone,timestamp with time zone);
CREATE OR REPLACE FUNCTION claimius.insert_user_claim(
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

COMMENT ON FUNCTION claimius.insert_user_claim(p_app_id uuid, p_claim_id uuid, p_user_id uuid, p_sa_owner_id uuid, p_sa_created_by uuid, p_reason text, p_starts_at timestamp with time zone, p_ends_at timestamp with time zone) IS 'Grants a claim to a user in an app.';

-- update_user_claim
DROP FUNCTION IF EXISTS claimius.update_claim_user(uuid,text,timestamp with time zone,timestamp with time zone);
CREATE OR REPLACE FUNCTION claimius.update_user_claim(
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

COMMENT ON FUNCTION claimius.update_user_claim(p_user_claim_id uuid, p_reason text, p_starts_at timestamp with time zone, p_ends_at timestamp with time zone) IS 'Patches selected fields on a user_claim.';

-- delete_user_claim
DROP FUNCTION IF EXISTS claimius.remove_user_claim(UUID, UUID);

DROP FUNCTION IF EXISTS claimius.remove_user_claim(uuid,uuid);
CREATE OR REPLACE FUNCTION claimius.delete_user_claim(
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

COMMENT ON FUNCTION claimius.delete_user_claim(p_user_claim_id uuid, p_deleted_by uuid) IS 'Soft deletes a user_claim row. Returns the deleted user_claim row.';

-- insert_claim_object
DROP FUNCTION IF EXISTS claimius.assign_claim_object(UUID, UUID, UUID, TEXT, UUID, UUID, UUID, INTEGER, BOOLEAN, TEXT, UUID, TEXT);

DROP FUNCTION IF EXISTS claimius.assign_claim_object(uuid,uuid,uuid,uuid,uuid,uuid,text,integer,boolean,text,uuid,jsonb);
CREATE OR REPLACE FUNCTION claimius.insert_claim_object(
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

COMMENT ON FUNCTION claimius.insert_claim_object(p_app_id uuid, p_claim_id uuid, p_sa_owner_id uuid, p_sa_root_id uuid, p_sa_created_by uuid, p_object_id uuid, p_object_type text, p_sa_access integer, p_inherits boolean, p_reason text, p_ref_id uuid, p_scope jsonb) IS 'Binds a claim. With (object_type, object_id) p_scope keys are field names; without them p_scope keys are custom. p_sa_access is a bitwise mask.';

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

-- delete_claim_object
DROP FUNCTION IF EXISTS claimius.remove_claim_object(UUID, UUID);

DROP FUNCTION IF EXISTS claimius.remove_claim_object(uuid,uuid);
CREATE OR REPLACE FUNCTION claimius.delete_claim_object(
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

COMMENT ON FUNCTION claimius.delete_claim_object(p_claim_object_id uuid, p_deleted_by uuid) IS 'Soft deletes a claim_object row. Returns the deleted claim_object row.';

-- ============================================================================
-- merge_user
-- Reassigns all data from a source user to a target user. Operates by
-- user_id (not row PK) and unifies across apps. Hard deletes the source
-- samna_user row(s) and migrates user_claim, user_relation, user_field and
-- user_object to the target, and user_users follows the deleted pairs.
-- External tables registered through upsert_table_info can opt in by
-- passing themselves in p_external_tables; the function will UPDATE their
-- sa_created_by references where applicable.
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
                UPDATE claimius.user_claim SET user_id = p_target_user_id
                WHERE user_id = p_source_user_id AND app_id = v_app_row.app_id;
                UPDATE claimius.user_relation SET user_id = p_target_user_id
                WHERE user_id = p_source_user_id AND app_id = v_app_row.app_id;
                UPDATE claimius.user_field SET user_id = p_target_user_id
                WHERE user_id = p_source_user_id AND app_id = v_app_row.app_id;
            ELSE
                -- Target already has a row for this app. Merge claims/relations/fields.
                UPDATE claimius.user_claim s SET sa_deleted_at = now()
                WHERE s.user_id = p_source_user_id AND s.app_id = v_app_row.app_id AND s.sa_deleted_at IS NULL
                  AND EXISTS (
                      SELECT 1 FROM claimius.user_claim t
                      WHERE t.user_id = p_target_user_id AND t.app_id = s.app_id AND t.claim_id = s.claim_id AND t.sa_deleted_at IS NULL
                  );
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
                    user_image    = v_source_app_user.user_image,
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
                -- repopulate as transactions touch the target. _count_user_users
                -- takes the source out of user_users with its pairs.
                DELETE FROM claimius.user_object
                WHERE user_id = p_source_user_id AND app_id = v_app_row.app_id;

                -- Hard delete the source samna_user row
                DELETE FROM claimius.samna_user WHERE id = v_source_app_user.id;
            END IF;

            PERFORM claimius.insert_audit(
                    v_app_row.app_id,
                    'merge_user', 'merge', 'claimius.samna_user',
                    p_target_user_id,
                    v_app_row.app_id,
                    coalesce(p_acting_user_claim, p_target_user_id),
                    'Merged source ' || p_source_user_id::TEXT || ' into target ' || p_target_user_id::TEXT
                    );
        END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.merge_user(p_target_user_id uuid, p_source_user_id uuid, p_acting_user_claim uuid) IS 'Reassigns all data from source user_id to target user_id across apps.';

-- ============================================================================
-- migrate_root: cross root migration. Should be called rarely since
-- calc_hierarchical_access blocks cross root reparenting in the trigger.
-- Hangs the object, with its subtree, directly under p_new_root_id, a root of
-- the same type. The subtree is walked through the object's own parent
-- column, sa_owner_id for an organization and sa_parent_id otherwise, and
-- every node in it takes the new root, the object the new root as parent, in
-- one statement the trigger lets through under claimius.migrating_root. The
-- trigger keeps the closure incrementally and raises the object area of each
-- node it moves, so every holder on either side settles. For an organization
-- the rows of registered tables it owns whose sa_root_id names the old
-- organization root, those with no parenthood of their own, take the new
-- one, and the moved organizations join the apps of the new root.
-- ============================================================================

CREATE OR REPLACE FUNCTION claimius.migrate_root(
    p_object_type   TEXT,
    p_object_id     UUID,
    p_new_root_id   UUID
) RETURNS VOID AS $$
DECLARE
    v_parts   TEXT[] := claimius.split_object_type(p_object_type);
    v_parent  TEXT := CASE WHEN p_object_type = 'claimius.organization' THEN 'sa_owner_id' ELSE 'sa_parent_id' END;
    v_old_root UUID;
    v_is_root BOOLEAN;
    v_moved   UUID[];
    v_table   RECORD;
    v_app_id  UUID;
BEGIN
    EXECUTE format('SELECT sa_root_id FROM %I.%I WHERE id = $1 AND sa_deleted_at IS NULL', v_parts[1], v_parts[2])
        INTO v_old_root USING p_object_id;
    IF v_old_root IS NULL THEN
        RAISE EXCEPTION 'migrate_root: % % not found', p_object_type, p_object_id USING ERRCODE = '22023';
    END IF;

    EXECUTE format('SELECT %1$I = id AND sa_root_id = id FROM %2$I.%3$I WHERE id = $1 AND sa_deleted_at IS NULL', v_parent, v_parts[1], v_parts[2])
        INTO v_is_root USING p_new_root_id;
    IF v_is_root IS NOT TRUE THEN
        RAISE EXCEPTION 'migrate_root: % is not a live root of %', p_new_root_id, p_object_type USING ERRCODE = '22023';
    END IF;

    EXECUTE format('
        WITH RECURSIVE subtree AS (
            SELECT id FROM %1$I.%2$I WHERE id = $1
            UNION
            SELECT t.id FROM %1$I.%2$I t JOIN subtree s ON t.%3$I = s.id
            WHERE t.id <> t.%3$I
        )
        SELECT array_agg(id) FROM subtree', v_parts[1], v_parts[2], v_parent)
        INTO v_moved USING p_object_id;
    IF p_new_root_id = ANY(v_moved) THEN
        RAISE EXCEPTION 'migrate_root: % lies inside the subtree of %', p_new_root_id, p_object_id USING ERRCODE = '22023';
    END IF;

    PERFORM set_config('claimius.migrating_root', 'on', TRUE);

    EXECUTE format('
        UPDATE %1$I.%2$I
        SET sa_root_id = $2,
            %3$I = CASE WHEN id = $3 THEN $2 ELSE %3$I END
        WHERE id = ANY($1)', v_parts[1], v_parts[2], v_parent)
        USING v_moved, p_new_root_id, p_object_id;

    IF p_object_type = 'claimius.organization' THEN
        FOR v_table IN
            SELECT ti.schema_name, ti.table_name
            FROM claimius.table_info ti
            WHERE ti.has_sa_owner_id
              AND ti.has_sa_root_id
              AND NOT ti.has_sa_parent_id
              AND ti.object_type <> 'claimius.organization'
        LOOP
            EXECUTE format('UPDATE %I.%I SET sa_root_id = $1 WHERE sa_owner_id = ANY($2) AND sa_root_id = $3', v_table.schema_name, v_table.table_name)
                USING p_new_root_id, v_moved, v_old_root;
        END LOOP;

        FOR v_app_id IN SELECT claimius.apps_for_object('claimius.organization', p_new_root_id) LOOP
            PERFORM claimius.upsert_app_organization(v_app_id, p_object_id, TRUE, NULL);
        END LOOP;
    END IF;

    PERFORM set_config('claimius.migrating_root', 'off', TRUE);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.migrate_root(p_object_type text, p_object_id uuid, p_new_root_id uuid) IS 'Admin function: hangs an object with its subtree under a root of another tree, walking organizations through sa_owner_id, moving the closure incrementally, rerooting the owned rows without parenthood and raising the areas it moves. Use only when absolutely required.';

-- ============================================================================
-- Init functions
-- ============================================================================

-- upsert_table_info
-- Registers external app tables. Called by the implementing app's migration.
-- Idempotent: rebuilds registration state from the input list each call.
-- For tables that already have data, seeds materialized state in bulk
-- under replay_mode (triggers do not fire, no events emitted).
DROP FUNCTION IF EXISTS claimius.init_claimius_tables(text[]);
CREATE OR REPLACE FUNCTION claimius.upsert_table_info(
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
                DELETE FROM claimius.object_info WHERE object_type = v_old_object_type;
                DELETE FROM claimius.reconcile_queue WHERE object_type = v_old_object_type;
                RAISE NOTICE 'Deregistered table %', v_old_object_type;
            END LOOP;
    END IF;

    -- Register each new (or existing) table
    FOREACH v_object_type IN ARRAY v_new LOOP
            IF position('.' IN v_object_type) = 0 THEN
                RAISE EXCEPTION 'object type % is not schema qualified', v_object_type;
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

COMMENT ON FUNCTION claimius.upsert_table_info(VARIADIC p_object_types text[]) IS 'Registers external tables. Idempotent. Called by implementer migrations.';

-- _bulk_seed_user_object
-- For a registered table with existing rows, writes the object_info row of
-- every row, populates user_object for every (user, object) pair where
-- access exists, then rebuilds user_users for
-- the users it holds pairs for, since no count moves under replay_mode.
-- Inherits the caller's replay_mode setting; callers run this with
-- replay_mode = true so emit_sync_event short circuits and the bulk seed
-- produces no sync events. The pairs are collected in one statement and grouped by
-- user, and each user's objects are recomputed in chunks of 1000 per call,
-- so the fixed cost of a recompute is paid once per user and chunk, not once
-- per pair.
CREATE OR REPLACE FUNCTION claimius._bulk_seed_user_object(p_object_type TEXT)
    RETURNS VOID AS $$
DECLARE
    v_schema TEXT;
    v_table TEXT;
    v_parts TEXT[];
    v_user RECORD;
    v_at INTEGER;
    v_sql TEXT;
    v_ids UUID[];
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    EXECUTE format('SELECT ARRAY(SELECT r.id FROM %I.%I r)', v_schema, v_table) INTO v_ids;
    PERFORM claimius._write_object_info(p_object_type, v_ids);

    v_sql := format(
            'SELECT p.app_id, a.user_id, array_agg(r.id ORDER BY r.id) AS object_ids FROM %I.%I r CROSS JOIN LATERAL claimius._app_ids_for_row(%L, to_jsonb(r)) p(app_id) CROSS JOIN LATERAL claimius._affected_users_for_object(p.app_id, %L, r.id) a WHERE r.sa_deleted_at IS NULL GROUP BY p.app_id, a.user_id',
            v_schema, v_table, p_object_type, p_object_type);
    FOR v_user IN EXECUTE v_sql LOOP
        FOR v_at IN 1 .. cardinality(v_user.object_ids) BY 1000 LOOP
            PERFORM claimius.recompute_user_object(v_user.app_id, v_user.user_id, p_object_type, VARIADIC v_user.object_ids[v_at : v_at + 999]);
        END LOOP;
    END LOOP;

    PERFORM claimius._refresh_user_users(s.app_id, s.user_id)
    FROM (
        SELECT DISTINCT app_id, user_id
        FROM claimius.user_object
        WHERE object_type = p_object_type
    ) s;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._bulk_seed_user_object(p_object_type text) IS 'Seeds object_info and user_object for an existing table at registration time.';

-- ----------------------------------------------------------------------------
-- recompute_state
-- Rebuilds inheritance_info trees and user_object materialization for every
-- registered table from live data. Idempotent. Use after a bulk write that
-- ran with claimius.replay_mode = 'true', or any time materialization needs
-- to be reconciled with current rows.
--
-- Self contained: reads the registry from claimius.table_info; takes no
-- arguments; assumes upsert_internal_table_info() and upsert_table_info(...)
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
-- upsert_internal_table_info
-- Registers all claimius.* tables in table_info. Called once at the end of
-- claimius schema bootstrap (VX.0__init.sql). Not callable by implementers.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.init_claimius_internal();
CREATE OR REPLACE FUNCTION claimius.upsert_internal_table_info()
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

COMMENT ON FUNCTION claimius.upsert_internal_table_info() IS 'Registers internal claimius.* tables in table_info. Called once at schema bootstrap.';

-- ----------------------------------------------------------------------------
-- _restamp_created_by
-- Rewrites a sa_created_by an older set wrote as a user id to the
-- user_claim.id it stands for, wherever that is unambiguous: the stamp is the
-- user_id of a samna_user of the row's app that holds exactly one live
-- user_claim in that app. The row's app is its app_id, the app itself for a
-- samna_app, and the apps an organization or location is linked to. Every
-- other stamp is left as it is, one that already names a user_claim.id
-- included, so a second run rewrites nothing. The rows are written under
-- replay_mode: a recompute finds the same user as the creator either way, so
-- no pair moves, and no event is emitted, since every set replays its own.
-- audit is an append only record and keeps what it holds. Run by the base
-- set on every replay. Returns the rows rewritten.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius._restamp_created_by()
    RETURNS INTEGER AS $$
DECLARE
    v_table   RECORD;
    v_apps    TEXT;
    v_count   INTEGER;
    v_total   INTEGER := 0;
    v_replay  TEXT := coalesce(nullif(current_setting('claimius.replay_mode', TRUE), ''), 'false');
BEGIN
    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
    BEGIN
        FOR v_table IN
            SELECT ti.object_type, ti.table_name
            FROM claimius.table_info ti
            WHERE ti.schema_name = 'claimius'
              AND ti.has_sa_created_by
              AND ti.object_type <> 'claimius.audit'
            UNION
            SELECT 'claimius.app_object', 'app_object'
            ORDER BY 1
        LOOP
            v_apps := CASE
                WHEN claimius.table_has_column(v_table.object_type, 'app_id') THEN 'ARRAY[t.app_id]'
                WHEN v_table.object_type = 'claimius.samna_app' THEN 'ARRAY[t.id]'
                ELSE format('ARRAY(SELECT claimius.apps_for_object(%L, t.id))', v_table.object_type)
            END;
            EXECUTE format($q$
                UPDATE claimius.%1$I r
                SET sa_created_by = s.user_claim_id
                FROM (
                    SELECT t.id, (array_agg(uc.id))[1] AS user_claim_id
                    FROM claimius.%1$I t
                        CROSS JOIN LATERAL unnest(%2$s) AS a(app_id)
                        JOIN claimius.samna_user u
                            ON u.user_id = t.sa_created_by
                           AND u.app_id = a.app_id
                        JOIN claimius.user_claim uc
                            ON uc.user_id = u.user_id
                           AND uc.app_id = a.app_id
                           AND uc.sa_deleted_at IS NULL
                    WHERE NOT EXISTS (SELECT 1 FROM claimius.user_claim x WHERE x.id = t.sa_created_by)
                    GROUP BY t.id
                    HAVING count(DISTINCT uc.id) = 1
                ) s
                WHERE r.id = s.id$q$, v_table.table_name, v_apps);
            GET DIAGNOSTICS v_count = ROW_COUNT;
            v_total := v_total + v_count;
        END LOOP;
    EXCEPTION WHEN OTHERS THEN
        PERFORM set_config('claimius.replay_mode', v_replay, TRUE);
        RAISE;
    END;
    PERFORM set_config('claimius.replay_mode', v_replay, TRUE);
    RETURN v_total;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._restamp_created_by() IS 'Rewrites each sa_created_by that is the user_id of a samna_user holding exactly one live user_claim in the row''s app to that user_claim.id, under replay_mode, and leaves every other stamp. audit keeps its own. Idempotent. Run by the base set. Returns the rows rewritten.';

-- ----------------------------------------------------------------------------
-- upsert_root_organization
-- Creates a new root organization (one with no parent in the ownership
-- tree). Two phase self reference is handled internally so callers do not
-- need to know the convention.
--
-- Returns the inserted row as jsonb keyed by table name. Idempotent on
-- (app_id, name): if a non deleted org with the same name already exists in
-- the app, returns that row instead of creating a new one.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.create_root_organization(uuid,text,uuid,text,text,uuid,text);
CREATE OR REPLACE FUNCTION claimius.upsert_root_organization(
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

    PERFORM claimius.upsert_app_object(p_app_id, 'claimius.organization', v_row.id, p_actor_user_claim_id);

    RETURN jsonb_build_object('organization', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_root_organization(UUID, TEXT, UUID, TEXT, TEXT, UUID, TEXT) IS 'Creates a new root organization with self referencing sa_owner_id and sa_root_id, linked to the app through app_object. Idempotent on (app, name).';

-- ----------------------------------------------------------------------------
-- upsert_app_organization
-- Links an app to an existing organization through app_object. With
-- p_cascade the link extends to every organization and location in the
-- org's ownership tree, read from inheritance_info. Additive and
-- idempotent: existing links stay untouched, nothing is ever unlinked.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.link_app_organization(uuid,uuid,boolean,uuid);
CREATE OR REPLACE FUNCTION claimius.upsert_app_organization(
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

    PERFORM claimius.upsert_app_object(p_app_id, 'claimius.organization', v_org.id, p_linked_by);
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
                PERFORM claimius.upsert_app_object(p_app_id, v_desc.descendant_type, v_desc.descendant_id, p_linked_by);
                v_linked := v_linked + 1;
            END LOOP;
    END IF;

    RETURN jsonb_build_object(
            'organization', to_jsonb(v_org),
            'linked_count', v_linked
           );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_app_organization(UUID, UUID, BOOLEAN, UUID) IS 'Links an app to an existing organization, cascading over its ownership tree to organizations and locations. Additive and idempotent.';

-- ----------------------------------------------------------------------------
-- init
-- Bootstrap a fresh claimius deployment as a prophet. Creates the system
-- app, system organization (self rooted), system user, system claim, and
-- wires up the bootstrap user_claim and claim_object. Idempotent.
--
-- The system user's user_claim is the actor of every row init writes. Its id
-- is settled first, the live one on a rerun or a fresh one the user_claim row
-- takes once its claim exists, and every sa_created_by carries it. The
-- user_claim lands before the bindings, so each binding is judged against the
-- access the system user holds as creator of the org and the app.
--
-- Per app cryptographic material is generated externally in Go and passed
-- in. Claimius does not generate keys.
--
-- App and org reference each other via sa_owner_id (app->org) and app_id
-- (org->app). We resolve the chicken-and-egg by inserting both with
-- generated placeholder uuids and then reconciling with UPDATEs.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.init(text,text,text,text,uuid,text,text,text,text,uuid[],jsonb,text,text,text,uuid,jsonb);
CREATE OR REPLACE FUNCTION claimius.upsert_system_app(
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

    SELECT uc.id INTO v_user_claim_id
    FROM claimius.user_claim uc
        JOIN claimius.samna_app a ON a.id = uc.app_id
        JOIN claimius.claim c ON c.id = uc.claim_id AND c.sa_deleted_at IS NULL
    WHERE a.slug = p_system_app_slug
      AND c.name = p_system_claim_name
      AND uc.user_id = p_system_user_id
      AND uc.sa_deleted_at IS NULL
    LIMIT 1;
    v_user_claim_id := coalesce(v_user_claim_id, gen_random_uuid());

    -- Validate the required default claim metadata. The system claim is
    -- created with hardcoded properties; the default claim (granted to every
    -- regular user on first login via upsert_app_user) is implementer defined.
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
                 p_system_app_style, v_pre_org_id, v_user_claim_id
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
                     v_pre_org_id, v_pre_org_id, 0, v_user_claim_id
                 )
        RETURNING id INTO v_org_id;
    END IF;

    PERFORM claimius.upsert_app_object(v_app_id, 'claimius.organization', v_org_id, v_user_claim_id);

    -- 4. reconcile: app's sa_owner_id should match the actual system org id
    UPDATE claimius.samna_app SET sa_owner_id = v_org_id WHERE id = v_app_id AND sa_owner_id <> v_org_id;

    -- 5. ensure ownership tree closure self-edge for the system org, and
    --    attach the app written before the org that owns it
    INSERT INTO claimius.inheritance_info (
        tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
    ) VALUES (
        'ownership', v_org_id, 'claimius.organization', v_org_id, 'claimius.organization', v_org_id, 0
    ) ON CONFLICT DO NOTHING;
    PERFORM claimius._attach_object_to_trees('claimius.samna_app', a.id, to_jsonb(a))
    FROM claimius.samna_app a
    WHERE a.id = v_app_id;

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
                     v_org_id, v_org_id, v_user_claim_id
                 )
        RETURNING id INTO v_claim_id;
    END IF;

    -- 6b. default claim (granted to every regular user on first login via
    -- upsert_app_user). No claim_object binding, no inheritance: it is a
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
                     v_org_id, v_org_id, v_user_claim_id
                 )
        RETURNING id INTO v_default_claim_id;
    END IF;

    -- 7. attach default claim_id to the system app. This is the claim every
    -- regular user receives via upsert_app_user. The system claim stays
    -- sidelined and is granted only to the system user below.
    UPDATE claimius.samna_app SET claim_id = v_default_claim_id
    WHERE id = v_app_id AND (claim_id IS NULL OR claim_id <> v_default_claim_id);

    -- 8. grant claim to system user, under the id every row above carries
    -- Partial unique index on (claim_id, user_id, app_id) WHERE sa_deleted_at IS NULL.
    IF NOT EXISTS (
        SELECT 1 FROM claimius.user_claim
        WHERE app_id = v_app_id
          AND claim_id = v_claim_id
          AND user_id = p_system_user_id
          AND sa_deleted_at IS NULL
    ) THEN
        INSERT INTO claimius.user_claim (
            id, app_id, claim_id, user_id, sa_owner_id, sa_created_by
        ) VALUES (
                     v_user_claim_id, v_app_id, v_claim_id, p_system_user_id, v_org_id, v_user_claim_id
                 );
    END IF;
    PERFORM claimius.recompute_user_object(v_app_id, p_system_user_id, 'claimius.organization', v_org_id);
    PERFORM claimius.recompute_user_object(v_app_id, p_system_user_id, 'claimius.samna_app', v_app_id);

    -- 9. bind claim to system org
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
                     v_org_id, v_org_id, v_user_claim_id, TRUE
                 );
    END IF;

    -- 9b. bind claim to system app directly. Every resource (except users
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
                     v_org_id, v_org_id, v_user_claim_id, FALSE
                 );
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

COMMENT ON FUNCTION claimius.upsert_system_app(p_system_app_slug text, p_system_app_name text, p_system_app_private_key text, p_system_app_private_seed text, p_system_user_id uuid, p_system_app_redirect_uri text, p_system_app_sync_uri text, p_system_app_contact_email text, p_system_app_image text, p_system_app_provider_ids uuid[], p_system_app_style jsonb, p_system_app_description text, p_system_org_name text, p_system_claim_name text, p_system_claim_id uuid, p_default_claim jsonb) IS 'Bootstrap function. Sets up system app, org, user, and claim, every row stamped with the system user''s user_claim.id. Idempotent.';

-- ----------------------------------------------------------------------------
-- upsert_disciple_state
-- Bootstrap a fresh claimius deployment as a disciple. Records that this
-- instance is a disciple, sets up disciple_state with the slug of the
-- upstream prophet app this disciple mirrors, and grants the
-- disciple_client role. Pass p_disciple_app_slug from an environment
-- variable (e.g. CLAIMIUS_DISCIPLE_APP_SLUG) so the same migration
-- artefact can be deployed against any prophet. The actual data arrives
-- via sync from the prophet.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.init_disciple(text);
CREATE OR REPLACE FUNCTION claimius.upsert_disciple_state(
    p_disciple_app_slug TEXT
)
    RETURNS JSONB AS $$
BEGIN
    IF p_disciple_app_slug IS NULL OR p_disciple_app_slug = '' THEN
        RAISE EXCEPTION 'upsert_disciple_state: p_disciple_app_slug is required.';
    END IF;

    -- Singleton row in disciple_state. SELECT-then-INSERT, then UPDATE the
    -- slug so re-running upsert_disciple_state with a different slug is allowed
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
    -- type as init and upsert_prophet_peer.
    RETURN '{}'::JSONB;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_disciple_state(p_disciple_app_slug text) IS 'Bootstrap as a disciple. Requires the upstream prophet app slug. State arrives from prophet via sync.';

-- ----------------------------------------------------------------------------
-- upsert_prophet_peer
-- Hybrid: a prophet that is home for its own system app and mirrors one app
-- of an upstream prophet as its peer, the way prophets federate. Runs init,
-- then registers the upstream in prophet_peer, from where the peer pull
-- consumes the upstream's feed of that app under replay_mode.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.init_hybrid(TEXT, TEXT, TEXT, TEXT, TEXT, UUID, TEXT, TEXT, TEXT, TEXT, UUID[], JSONB, TEXT, TEXT, TEXT, UUID, JSONB);

DROP FUNCTION IF EXISTS claimius.init_hybrid(text,text,text,text,text,text,text,text,uuid,text,text,text,text,uuid[],jsonb,text,text,text,uuid,jsonb);
DROP FUNCTION IF EXISTS claimius.upsert_prophet_peer(text,text,text,text,text,text,text,text,uuid,text,text,text,text,uuid[],jsonb,text,text,text,uuid,jsonb);
CREATE OR REPLACE FUNCTION claimius.upsert_prophet_peer(
    p_system_app_slug          TEXT,
    p_system_app_name          TEXT,
    p_system_app_private_key   TEXT,
    p_system_app_private_seed  TEXT,
    p_upstream_prophet         TEXT,
    p_upstream_endpoint        TEXT,
    p_upstream_app_slug        TEXT,
    p_upstream_app_secret      TEXT,
    p_upstream_peer_secret     TEXT,
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
    IF coalesce(p_upstream_prophet, '') = '' OR coalesce(p_upstream_endpoint, '') = ''
        OR coalesce(p_upstream_app_slug, '') = '' OR coalesce(p_upstream_app_secret, '') = '' OR coalesce(p_upstream_peer_secret, '') = '' THEN
        RAISE EXCEPTION 'upsert_prophet_peer: the upstream prophet, its endpoint, the mirrored app slug, its secret and the peer secret are required.';
    END IF;
    IF p_upstream_prophet = p_system_app_slug THEN
        RAISE EXCEPTION 'upsert_prophet_peer: the upstream prophet % is this prophet.', p_upstream_prophet;
    END IF;

    v_result := claimius.upsert_system_app(
            p_system_app_slug, p_system_app_name,
            p_system_app_private_key, p_system_app_private_seed,
            p_system_user_id,
            p_system_app_redirect_uri, p_system_app_sync_uri, p_system_app_contact_email,
            p_system_app_image, p_system_app_provider_ids, p_system_app_style,
            p_system_app_description, p_system_org_name,
            p_system_claim_name, p_system_claim_id,
            p_default_claim
                );

    INSERT INTO claimius.prophet_peer (prophet, endpoint, app_slug, app_secret, peer_secret, sa_created_by)
    VALUES (p_upstream_prophet, p_upstream_endpoint, p_upstream_app_slug, p_upstream_app_secret, p_upstream_peer_secret,
            (v_result -> 'user_claim' ->> 'id')::UUID)
    ON CONFLICT (app_slug) DO UPDATE SET
        prophet    = EXCLUDED.prophet,
        endpoint   = EXCLUDED.endpoint,
        app_secret = EXCLUDED.app_secret,
        peer_secret = EXCLUDED.peer_secret,
        status     = 'active';

    RAISE NOTICE 'Hybrid initialized, mirroring app % from prophet %.', p_upstream_app_slug, p_upstream_prophet;
    RETURN v_result;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_prophet_peer(p_system_app_slug text, p_system_app_name text, p_system_app_private_key text, p_system_app_private_seed text, p_upstream_prophet text, p_upstream_endpoint text, p_upstream_app_slug text, p_upstream_app_secret text, p_upstream_peer_secret text, p_system_user_id uuid, p_system_app_redirect_uri text, p_system_app_sync_uri text, p_system_app_contact_email text, p_system_app_image text, p_system_app_provider_ids uuid[], p_system_app_style jsonb, p_system_app_description text, p_system_org_name text, p_system_claim_name text, p_system_claim_id uuid, p_default_claim jsonb) IS 'Bootstrap as a hybrid: a prophet mirroring one app of an upstream prophet registered as its peer.';

-- ----------------------------------------------------------------------------
-- upsert_app
-- Registers a new app on an existing prophet, owned by an existing
-- organization, under the authority of an actor of another app: the
-- user_claim.id the actor acts under, stamped into every sa_created_by. The
-- actor must hold owner on the owning organization in its own app, the rule
-- the first binding of an app is judged by. Links the organization and its
-- tree to the app, creates the owner claim bound on the organization and on
-- the app and grants it to the actor's user in the app, whose samna_user row
-- is copied from the actor's app, and creates the default claim, read by
-- default, bound on the app and on the organization and set as the app's
-- claim_id. Idempotent on slug and on each claim's name. Returns jsonb keyed
-- by table: samna_app, organization, samna_user, claim, default_claim,
-- user_claim, claim_object.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.create_app(uuid,text,text,uuid,text,text,text[],text,text,text,integer,jsonb,uuid);
CREATE OR REPLACE FUNCTION claimius.upsert_app(
    p_actor_user_claim_id UUID,
    p_slug                TEXT,
    p_name                TEXT,
    p_org_id              UUID,
    p_private_key         TEXT,
    p_private_seed        TEXT,
    p_redirect_uris       TEXT[]  DEFAULT ARRAY[]::TEXT[],
    p_sync_uri            TEXT    DEFAULT '',
    p_description         TEXT    DEFAULT '',
    p_connection          TEXT    DEFAULT 'code',
    p_owner_access        INTEGER DEFAULT 15,
    p_default_claim       JSONB   DEFAULT NULL,
    p_app_id              UUID    DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_actor             claimius.user_claim;
    v_org               claimius.organization;
    v_app               claimius.samna_app;
    v_claim_id          UUID;
    v_default_claim_id  UUID;
    v_user_claim_id     UUID;
    v_result            JSONB;
BEGIN
    SELECT * INTO v_actor
    FROM claimius.user_claim
    WHERE id = p_actor_user_claim_id AND sa_deleted_at IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'upsert_app: actor % is not a live user_claim', p_actor_user_claim_id USING ERRCODE = '22023';
    END IF;

    SELECT * INTO v_org
    FROM claimius.organization
    WHERE id = p_org_id AND sa_deleted_at IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'upsert_app: organization % not found', p_org_id USING ERRCODE = '22023';
    END IF;

    INSERT INTO claimius.samna_app (
        id, slug, name, description, redirect_uris, sync_uri, status, connection,
        private_key, private_seed, sa_owner_id, sa_created_by
    ) VALUES (
        coalesce(claimius.ensure_uuid_null(p_app_id), gen_random_uuid()), p_slug, p_name, p_description,
        p_redirect_uris, p_sync_uri, 'active', p_connection::claimius.app_connection,
        p_private_key, p_private_seed, v_org.id, p_actor_user_claim_id
    )
    ON CONFLICT (slug) DO UPDATE SET
        name          = EXCLUDED.name,
        description   = EXCLUDED.description,
        redirect_uris = EXCLUDED.redirect_uris,
        sync_uri      = EXCLUDED.sync_uri,
        connection    = EXCLUDED.connection,
        private_key   = EXCLUDED.private_key,
        private_seed  = EXCLUDED.private_seed,
        sa_updated_at = now()
    RETURNING * INTO v_app;

    IF v_app.sa_owner_id <> v_org.id THEN
        RAISE EXCEPTION 'upsert_app: app % is owned by organization %, not %', p_slug, v_app.sa_owner_id, v_org.id USING ERRCODE = '22023';
    END IF;

    INSERT INTO claimius.prophet_state (app_id, last_applied_seq)
    VALUES (v_app.id, 0)
    ON CONFLICT (app_id) DO NOTHING;

    IF NOT EXISTS (
        SELECT 1 FROM claimius.samna_user
        WHERE user_id = v_actor.user_id AND app_id = v_app.id AND sa_deleted_at IS NULL
    ) THEN
        INSERT INTO claimius.samna_user (user_id, app_id, first_name, last_name, user_name, user_image, email, phone, status, type)
        SELECT v_actor.user_id, v_app.id, su.first_name, su.last_name, su.user_name, su.user_image, su.email, su.phone, 'active', su.type
        FROM claimius.samna_user su
        WHERE su.user_id = v_actor.user_id AND su.app_id = v_actor.app_id AND su.sa_deleted_at IS NULL
        LIMIT 1;
        IF NOT FOUND THEN
            INSERT INTO claimius.samna_user (user_id, app_id, status, type)
            VALUES (v_actor.user_id, v_app.id, 'active', 'system');
        END IF;
    END IF;

    PERFORM claimius.upsert_app_organization(v_app.id, v_org.id, TRUE, p_actor_user_claim_id);

    SELECT id INTO v_claim_id FROM claimius.claim
    WHERE app_id = v_app.id AND name = p_name || ' Owner' AND sa_deleted_at IS NULL
    LIMIT 1;
    IF v_claim_id IS NULL THEN
        v_claim_id := (claimius.insert_claim(
            p_app_id        => v_app.id,
            p_name          => p_name || ' Owner',
            p_description   => 'Owner claim for the ' || p_slug || ' app',
            p_sa_access     => p_owner_access,
            p_sa_owner_id   => v_org.id,
            p_sa_root_id    => v_org.sa_root_id,
            p_sa_created_by => p_actor_user_claim_id,
            p_inherits      => TRUE,
            p_type          => 'system'
        ) -> 'claim' ->> 'id')::UUID;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM claimius.claim_object
        WHERE app_id = v_app.id AND claim_id = v_claim_id
          AND object_id = v_org.id AND object_type = 'claimius.organization'
          AND sa_deleted_at IS NULL
    ) THEN
        PERFORM claimius.insert_claim_object(
            p_app_id        => v_app.id,
            p_claim_id      => v_claim_id,
            p_sa_owner_id   => v_org.id,
            p_sa_root_id    => v_org.sa_root_id,
            p_sa_created_by => p_actor_user_claim_id,
            p_object_id     => v_org.id,
            p_object_type   => 'claimius.organization',
            p_inherits      => TRUE
        );
    END IF;

    SELECT id INTO v_user_claim_id FROM claimius.user_claim
    WHERE app_id = v_app.id AND claim_id = v_claim_id
      AND user_id = v_actor.user_id AND sa_deleted_at IS NULL
    LIMIT 1;
    IF v_user_claim_id IS NULL THEN
        v_user_claim_id := (claimius.insert_user_claim(
            p_app_id        => v_app.id,
            p_claim_id      => v_claim_id,
            p_user_id       => v_actor.user_id,
            p_sa_owner_id   => v_org.id,
            p_sa_created_by => p_actor_user_claim_id
        ) -> 'user_claim' ->> 'id')::UUID;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM claimius.claim_object
        WHERE app_id = v_app.id AND claim_id = v_claim_id
          AND object_id = v_app.id AND object_type = 'claimius.samna_app'
          AND sa_deleted_at IS NULL
    ) THEN
        PERFORM claimius.insert_claim_object(
            p_app_id        => v_app.id,
            p_claim_id      => v_claim_id,
            p_sa_owner_id   => v_org.id,
            p_sa_root_id    => v_org.sa_root_id,
            p_sa_created_by => p_actor_user_claim_id,
            p_object_id     => v_app.id,
            p_object_type   => 'claimius.samna_app',
            p_inherits      => FALSE
        );
    END IF;

    SELECT id INTO v_default_claim_id FROM claimius.claim
    WHERE app_id = v_app.id
      AND name = coalesce(p_default_claim ->> 'name', p_name || ' Reader')
      AND sa_deleted_at IS NULL
    LIMIT 1;
    IF v_default_claim_id IS NULL THEN
        v_default_claim_id := (claimius.insert_claim(
            p_app_id        => v_app.id,
            p_name          => coalesce(p_default_claim ->> 'name', p_name || ' Reader'),
            p_description   => coalesce(p_default_claim ->> 'description', 'Reader claim for the ' || p_slug || ' app'),
            p_sa_access     => coalesce((p_default_claim ->> 'sa_access')::INTEGER, 4),
            p_sa_owner_id   => v_org.id,
            p_sa_root_id    => v_org.sa_root_id,
            p_sa_created_by => p_actor_user_claim_id,
            p_inherits      => FALSE,
            p_type          => 'system'
        ) -> 'claim' ->> 'id')::UUID;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM claimius.claim_object
        WHERE app_id = v_app.id AND claim_id = v_default_claim_id
          AND object_id = v_app.id AND object_type = 'claimius.samna_app'
          AND sa_deleted_at IS NULL
    ) THEN
        PERFORM claimius.insert_claim_object(
            p_app_id        => v_app.id,
            p_claim_id      => v_default_claim_id,
            p_sa_owner_id   => v_org.id,
            p_sa_root_id    => v_org.sa_root_id,
            p_sa_created_by => p_actor_user_claim_id,
            p_object_id     => v_app.id,
            p_object_type   => 'claimius.samna_app',
            p_inherits      => FALSE
        );
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM claimius.claim_object
        WHERE app_id = v_app.id AND claim_id = v_default_claim_id
          AND object_id = v_org.id AND object_type = 'claimius.organization'
          AND sa_deleted_at IS NULL
    ) THEN
        PERFORM claimius.insert_claim_object(
            p_app_id        => v_app.id,
            p_claim_id      => v_default_claim_id,
            p_sa_owner_id   => v_org.id,
            p_sa_root_id    => v_org.sa_root_id,
            p_sa_created_by => p_actor_user_claim_id,
            p_object_id     => v_org.id,
            p_object_type   => 'claimius.organization',
            p_inherits      => TRUE
        );
    END IF;

    UPDATE claimius.samna_app SET claim_id = v_default_claim_id
    WHERE id = v_app.id AND claim_id IS DISTINCT FROM v_default_claim_id;

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
        LEFT JOIN claimius.organization o ON o.id = v_org.id
        LEFT JOIN claimius.samna_user u ON u.user_id = v_actor.user_id AND u.app_id = a.id AND u.sa_deleted_at IS NULL
        LEFT JOIN claimius.claim c ON c.id = v_claim_id
        LEFT JOIN claimius.claim dc ON dc.id = v_default_claim_id
        LEFT JOIN claimius.user_claim uc ON uc.id = v_user_claim_id
        LEFT JOIN claimius.claim_object co ON co.app_id = a.id AND co.claim_id = v_claim_id
            AND co.object_id = v_org.id AND co.object_type = 'claimius.organization'
            AND co.sa_deleted_at IS NULL
    WHERE a.id = v_app.id;

    RETURN v_result;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_app(UUID, TEXT, TEXT, UUID, TEXT, TEXT, TEXT[], TEXT, TEXT, TEXT, INTEGER, JSONB, UUID) IS 'Registers a new app on an existing prophet, owned by an existing organization the actor holds owner on in its own app. Every row is stamped with the actor''s user_claim.id. Links the organization tree, creates and binds the owner claim and grants it to the actor''s user in the app, and creates and binds the default claim. Idempotent on slug. Returns jsonb keyed by table.';

-- ----------------------------------------------------------------------------
-- upsert_app_user
-- Helper for prophet apps. When a user logs into an app for the first time,
-- creates the per app samna_user row keyed by (user_id, app_id). If the app
-- has a default claim configured (samna_app.claim_id), grants it to the user
-- as part of the same call. The user_claim is stamped with p_sa_created_by,
-- the acting user_claim.id when a caller grants it, or else with its own id:
-- a first login is the assignment acting on its own authority. Idempotent:
-- re-running returns the existing row and existing user_claim.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.ensure_app_user(UUID, UUID, TEXT, TEXT, TEXT, TEXT, TEXT, TEXT);
DROP FUNCTION IF EXISTS claimius.ensure_app_user(UUID, UUID, TEXT, TEXT, TEXT, TEXT, TEXT, TEXT, TEXT);
DROP FUNCTION IF EXISTS claimius.ensure_app_user(uuid,uuid,text,text,text,text,text,text,text,uuid);
CREATE OR REPLACE FUNCTION claimius.upsert_app_user(
    p_user_id    UUID,
    p_app_id     UUID,
    p_first_name TEXT DEFAULT NULL,
    p_last_name  TEXT DEFAULT NULL,
    p_user_name  TEXT DEFAULT NULL,
    p_email      TEXT DEFAULT NULL,
    p_phone      TEXT DEFAULT NULL,
    p_external_id TEXT DEFAULT NULL,
    p_user_image TEXT DEFAULT NULL,
    p_sa_created_by UUID DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_user_row    claimius.samna_user;
    v_uc_row      claimius.user_claim;
    v_uc_id       UUID := gen_random_uuid();
    v_default_claim_id UUID;
    v_default_claim_owner UUID;
BEGIN
    p_user_id := claimius.ensure_uuid_null(p_user_id);

    -- Find or insert the samna_user row
    SELECT * INTO v_user_row FROM claimius.samna_user
    WHERE user_id = p_user_id AND app_id = p_app_id AND sa_deleted_at IS NULL;

    IF NOT FOUND THEN
        INSERT INTO claimius.samna_user (
            user_id, app_id, first_name, last_name, user_name, user_image,
            email, phone, external_id, status, type
        )
        VALUES (
                   COALESCE(p_user_id, gen_random_uuid()),
                   p_app_id, p_first_name, p_last_name, p_user_name, p_user_image,
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
                id, app_id, claim_id, user_id, sa_owner_id, sa_created_by
            ) VALUES (
                         v_uc_id, p_app_id, v_default_claim_id, v_user_row.user_id,
                         v_default_claim_owner,
                         coalesce(p_sa_created_by, v_uc_id)
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

COMMENT ON FUNCTION claimius.upsert_app_user(p_user_id uuid, p_app_id uuid, p_first_name text, p_last_name text, p_user_name text, p_email text, p_phone text, p_external_id text, p_user_image text, p_sa_created_by uuid) IS 'Creates per app samna_user row and grants the app default claim, stamped with the acting user_claim.id p_sa_created_by or else its own id. Idempotent.';

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
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

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
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

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
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

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
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

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
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;


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
DECLARE
    v_norm TEXT := claimius.normalize_object_type(p_object_type);
BEGIN
    IF p_include_types IS NOT NULL AND array_length(p_include_types, 1) IS NOT NULL THEN
        IF NOT (p_object_type = ANY(p_include_types) OR v_norm = ANY(p_include_types)) THEN
            RETURN FALSE;
        END IF;
    END IF;
    IF p_exclude_types IS NOT NULL AND array_length(p_exclude_types, 1) IS NOT NULL THEN
        IF p_object_type = ANY(p_exclude_types) OR v_norm = ANY(p_exclude_types) THEN
            RETURN FALSE;
        END IF;
    END IF;
    RETURN TRUE;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

GRANT EXECUTE ON FUNCTION claimius.graph_type_passes(TEXT, TEXT[], TEXT[]) TO claimius_reader, claimius_writer, claimius_admin;

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

DROP FUNCTION IF EXISTS claimius.get_graph_links_owner(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER);

DROP FUNCTION IF EXISTS claimius.get_graph_links_owner(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID);
DROP FUNCTION IF EXISTS claimius.get_graph_objects(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID[], BOOLEAN, BOOLEAN);
DROP FUNCTION IF EXISTS claimius.touch_object_usage(UUID, UUID, TEXT, UUID);
DROP FUNCTION IF EXISTS claimius.graph_object_types();
DROP FUNCTION IF EXISTS claimius._prune_graph_rows(JSONB, UUID, UUID, UUID, INTEGER, TEXT, UUID);
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
    v_links   JSONB;
    v_nodes   JSONB;
    v_allowed TEXT[] := (SELECT coalesce(array_agg(object_type ORDER BY object_type), ARRAY[]::TEXT[]) FROM claimius.table_info);
BEGIN
    IF p_sort IS NULL OR p_sort NOT IN ('name', 'usage') OR p_width IS NULL OR p_width < 0 THEN
        RAISE EXCEPTION 'graph window: p_sort is name or usage and p_width at least 0' USING ERRCODE = '22023';
    END IF;

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN jsonb_build_object('links', '[]'::jsonb);
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
    )), '[]'::jsonb),
           CASE WHEN p_width > 0 OR p_next_id IS NOT NULL THEN (
               SELECT coalesce(jsonb_agg(DISTINCT jsonb_build_object(
                          'object_type', claimius.normalize_object_type(s.object_type),
                          'object_id', s.object_id)), '[]'::jsonb)
               FROM in_scope s) END
    INTO v_links, v_nodes FROM edges;

    IF p_width > 0 OR p_next_id IS NOT NULL THEN
        RETURN claimius._window_links(v_links, v_nodes, p_user_id, p_app_id, p_width, p_sort, p_next_id);
    END IF;

    RETURN jsonb_build_object('links', v_links);
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

GRANT EXECUTE ON FUNCTION claimius.get_graph_links_owner(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) TO claimius_reader, claimius_writer, claimius_admin;

DROP FUNCTION IF EXISTS claimius.get_graph_links_claim(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER);

DROP FUNCTION IF EXISTS claimius.get_graph_links_claim(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID);
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
BEGIN
    IF p_sort IS NULL OR p_sort NOT IN ('name', 'usage') OR p_width IS NULL OR p_width < 0 THEN
        RAISE EXCEPTION 'graph window: p_sort is name or usage and p_width at least 0' USING ERRCODE = '22023';
    END IF;

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

    WITH cascade_edges AS (
        SELECT DISTINCT ON (co.id, ii.descendant_type, ii.descendant_id, ii.tree_type)
               co.id AS co_id,
               co.claim_id AS claim_id,
               co.reason AS reason,
               ii.descendant_type AS target_type,
               ii.descendant_id AS target_id,
               ii.tree_type::TEXT AS tree_type,
               ii.depth AS depth
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

    IF p_width > 0 OR p_next_id IS NOT NULL THEN
        v_nodes := (SELECT coalesce(jsonb_agg(jsonb_build_object('object_type', 'claim', 'object_id', c)), '[]'::jsonb)
                    FROM unnest(v_claim_ids) AS c)
                || CASE WHEN v_user_row_id IS NULL THEN '[]'::jsonb
                        ELSE jsonb_build_array(jsonb_build_object('object_type', 'samna_user', 'object_id', v_user_row_id)) END;
        RETURN claimius._window_links(v_links, v_nodes, p_user_id, p_app_id, p_width, p_sort, p_next_id);
    END IF;

    RETURN jsonb_build_object('links', v_links);
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

GRANT EXECUTE ON FUNCTION claimius.get_graph_links_claim(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) TO claimius_reader, claimius_writer, claimius_admin;

DROP FUNCTION IF EXISTS claimius.create_organization_user(UUID, UUID, UUID, JSONB);
DROP FUNCTION IF EXISTS claimius.create_organization_user(UUID, UUID, UUID, JSONB, BOOLEAN);

DROP FUNCTION IF EXISTS claimius.create_organization_user(uuid,uuid,uuid,jsonb,boolean,jsonb,text);
CREATE OR REPLACE FUNCTION claimius.insert_organization_user(
    p_app_id     UUID,
    p_caller_uc  UUID,
    p_owner_id   UUID,
    p_user       JSONB,
    p_upsert     BOOLEAN DEFAULT FALSE,
    p_identity   JSONB DEFAULT NULL,
    p_reason     TEXT DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_user_id           UUID;
    v_external_id       TEXT;
    v_identity_id       TEXT;
    v_claim_id          UUID;
    v_root_id           UUID;
    v_created           BOOLEAN;
    v_user_row          claimius.samna_user;
    v_token_row         claimius.user_token;
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

    IF p_app_id IS NULL THEN
        RAISE EXCEPTION 'app_id required';
    END IF;
    IF v_claim_id IS NULL THEN
        RAISE EXCEPTION 'claim_id required';
    END IF;
    IF p_owner_id IS NULL THEN
        RAISE EXCEPTION 'owner_id required';
    END IF;

    SELECT sa_root_id INTO v_root_id
      FROM claimius.organization
     WHERE id = p_owner_id
       AND sa_deleted_at IS NULL
       AND claimius.app_has_object(p_app_id, 'claimius.organization', id);

    IF v_root_id IS NULL THEN
        RAISE EXCEPTION 'owner_id invalid';
    END IF;

    IF p_caller_uc IS NOT NULL THEN
        SELECT user_id INTO v_caller_user_id
          FROM claimius.user_claim
         WHERE id = p_caller_uc
           AND app_id = p_app_id
           AND sa_deleted_at IS NULL;

        IF v_caller_user_id IS NULL THEN
            RAISE EXCEPTION 'caller user_claim invalid';
        END IF;
    END IF;

    IF p_identity IS NOT NULL THEN
        v_identity_id := NULLIF(p_identity ->> 'external_id', '');
        IF v_identity_id IS NULL THEN
            RAISE EXCEPTION 'identity external_id required';
        END IF;
        SELECT * INTO v_token_row
          FROM claimius.user_token
         WHERE app_id = p_app_id
           AND sa_owner_id = p_owner_id
           AND external_id = v_identity_id
           AND sa_deleted_at IS NULL
         LIMIT 1;
        IF v_token_row.user_id IS NOT NULL THEN
            v_user_id := v_token_row.user_id;
        END IF;
    END IF;

    v_created := v_user_id IS NULL OR NOT EXISTS (
        SELECT 1 FROM claimius.samna_user
         WHERE user_id = v_user_id
           AND app_id = p_app_id
           AND sa_deleted_at IS NULL
    );

    IF NOT p_upsert AND p_identity IS NULL THEN
        IF NOT v_created THEN
            RAISE EXCEPTION 'user_id already exists' USING ERRCODE = '23505';
        END IF;

        IF v_external_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM claimius.samna_user
             WHERE external_id = v_external_id
               AND app_id = p_app_id
               AND sa_deleted_at IS NULL
        ) THEN
            RAISE EXCEPTION 'external_id already exists' USING ERRCODE = '23505';
        END IF;

        INSERT INTO claimius.samna_user (
            user_id, app_id, first_name, last_name, user_name, user_image,
            email, phone, external_id, status, type
        ) VALUES (
            COALESCE(v_user_id, gen_random_uuid()),
            p_app_id,
            NULLIF(p_user ->> 'first_name', ''),
            NULLIF(p_user ->> 'last_name', ''),
            NULLIF(p_user ->> 'user_name', ''),
            NULLIF(p_user ->> 'user_image', ''),
            NULLIF(p_user ->> 'email', ''),
            NULLIF(p_user ->> 'phone', ''),
            v_external_id,
            COALESCE(NULLIF(p_user ->> 'status', ''), 'active')::claimius.user_status,
            'user'
        )
        RETURNING * INTO v_user_row;
    ELSE
        INSERT INTO claimius.samna_user (
            user_id, app_id, first_name, last_name, user_name, user_image,
            email, phone, external_id, status, type
        ) VALUES (
            COALESCE(v_user_id, gen_random_uuid()),
            p_app_id,
            NULLIF(p_user ->> 'first_name', ''),
            NULLIF(p_user ->> 'last_name', ''),
            NULLIF(p_user ->> 'user_name', ''),
            NULLIF(p_user ->> 'user_image', ''),
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
            user_image    = COALESCE(EXCLUDED.user_image, claimius.samna_user.user_image),
            email         = COALESCE(EXCLUDED.email, claimius.samna_user.email),
            phone         = COALESCE(EXCLUDED.phone, claimius.samna_user.phone),
            external_id   = COALESCE(EXCLUDED.external_id, claimius.samna_user.external_id),
            sa_updated_at = now()
        RETURNING * INTO v_user_row;
    END IF;

    SELECT * INTO v_existing_uc
      FROM claimius.user_claim
     WHERE app_id = p_app_id
       AND claim_id = v_claim_id
       AND user_id = v_user_row.user_id
       AND sa_deleted_at IS NULL
     LIMIT 1;

    IF v_existing_uc.id IS NOT NULL THEN
        v_claim_user := jsonb_build_object('user_claim', to_jsonb(v_existing_uc));
    ELSE
        v_claim_user := claimius.insert_user_claim(
            p_app_id        => p_app_id,
            p_claim_id      => v_claim_id,
            p_user_id       => v_user_row.user_id,
            p_sa_owner_id   => p_owner_id,
            p_sa_created_by => p_caller_uc,
            p_reason        => p_reason
        );
    END IF;

    SELECT * INTO v_existing_virtual
      FROM claimius.claim
     WHERE app_id = p_app_id
       AND name = 'user:' || v_user_row.user_id::text
       AND type = 'virtual'
       AND sa_deleted_at IS NULL
     LIMIT 1;

    IF v_existing_virtual.id IS NOT NULL THEN
        v_virtual_claim    := jsonb_build_object('claim', to_jsonb(v_existing_virtual));
        v_virtual_claim_id := v_existing_virtual.id;
    ELSE
        v_virtual_claim := claimius.insert_claim(
            p_app_id        => p_app_id,
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
     WHERE app_id = p_app_id
       AND claim_id = v_virtual_claim_id
       AND object_id = v_user_row.user_id
       AND object_type = 'claimius.samna_user'
       AND sa_deleted_at IS NULL
     LIMIT 1;

    IF v_existing_obj.id IS NOT NULL THEN
        v_claim_obj := jsonb_build_object('claim_object', to_jsonb(v_existing_obj));
    ELSE
        v_claim_obj := claimius.insert_claim_object(
            p_app_id        => p_app_id,
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
         WHERE app_id = p_app_id
           AND claim_id = v_virtual_claim_id
           AND user_id = v_caller_user_id
           AND sa_deleted_at IS NULL
         LIMIT 1;

        IF v_existing_caller.id IS NOT NULL THEN
            v_caller_user_claim := jsonb_build_object('user_claim', to_jsonb(v_existing_caller));
        ELSE
            v_caller_user_claim := claimius.insert_user_claim(
                p_app_id        => p_app_id,
                p_claim_id      => v_virtual_claim_id,
                p_user_id       => v_caller_user_id,
                p_sa_owner_id   => p_owner_id,
                p_sa_created_by => p_caller_uc
            );
        END IF;
    END IF;

    IF p_identity IS NOT NULL THEN
        INSERT INTO claimius.user_token (
            app_id, user_id, id_claim, external_id, type, claim_id, data, sa_owner_id, sa_created_by
        ) VALUES (
            p_app_id,
            v_user_row.user_id,
            COALESCE(NULLIF(p_identity ->> 'id_claim', ''), 'sub'),
            v_identity_id,
            COALESCE(NULLIF(p_identity ->> 'type', ''), 'token'),
            v_claim_id,
            p_identity -> 'data',
            p_owner_id,
            p_caller_uc
        )
        ON CONFLICT (app_id, sa_owner_id, external_id) WHERE sa_deleted_at IS NULL DO UPDATE SET
            user_id       = EXCLUDED.user_id,
            id_claim      = EXCLUDED.id_claim,
            type          = EXCLUDED.type,
            claim_id      = EXCLUDED.claim_id,
            data          = EXCLUDED.data,
            sa_updated_at = now()
        RETURNING * INTO v_token_row;
    END IF;

    RETURN jsonb_build_object(
        'samna_user',        to_jsonb(v_user_row),
        'created',           v_created,
        'user_claim',        v_claim_user        -> 'user_claim',
        'virtual_claim',     v_virtual_claim     -> 'claim',
        'claim_object',      v_claim_obj         -> 'claim_object',
        'caller_user_claim', v_caller_user_claim -> 'user_claim',
        'user_token',        CASE WHEN p_identity IS NULL THEN NULL ELSE to_jsonb(v_token_row) END
    );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.insert_organization_user(UUID, UUID, UUID, JSONB, BOOLEAN, JSONB, TEXT) IS
    'Adds a user to an organization of an app: inserts or upserts the samna_user, grants the supplied claim with p_reason, ensures a per-user virtual claim with full access bound to the user, and when p_caller_uc is not null grants that virtual claim to the caller. The organization must be linked to the app and the caller must be a live user_claim of the app. With p_identity, an outside identity {id_claim, external_id, type, data} names the user: a user_token of the app, owner and external_id reuses its user, the call upserts, and the user_token is written. Without p_identity and with p_upsert=FALSE, raises 23505 when user_id or external_id already exists in the app. Returns samna_user, created, user_claim, virtual_claim, claim_object, caller_user_claim and user_token.';

GRANT EXECUTE ON FUNCTION claimius.insert_organization_user(UUID, UUID, UUID, JSONB, BOOLEAN, JSONB, TEXT)
    TO claimius_disciple_client, claimius_writer, claimius_admin;

ALTER TABLE claimius.user_token ALTER COLUMN app_id SET DEFAULT claimius.get_app_id();
ALTER TABLE claimius.audit ALTER COLUMN app_id SET DEFAULT claimius.get_app_id();

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
    SELECT claimius.reconcile_if_pending(p_app_id, a.user_id, p_object_type, p_object_id)
    FROM claimius._affected_users_for_object(p_app_id, p_object_type, p_object_id) a
    WHERE p_created_by IS NULL OR a.user_id <> p_created_by;

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

COMMENT ON FUNCTION claimius.notify_object(UUID, TEXT, TEXT, JSONB, TEXT, TEXT, TEXT, UUID, UUID, UUID, INTERVAL) IS 'Fans a notification out to every user holding read access on the object in the app, excluding the triggering user. Settles first the pair of every user who may hold the object, so a reader who has not read it since the last write is reached. Returns the number of rows inserted.';

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
-- count_sync_source_rows
-- The source rows of one app the snapshot ships: the rows of the mirrored
-- tables carrying the app id and the organizations and locations linked to
-- it. With p_limit the count stops there, so a caller comparing it against a
-- smaller number pays for that number of rows and not for the app.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius.count_sync_source_rows(
    p_app_id    UUID,
    p_limit     BIGINT DEFAULT NULL
) RETURNS BIGINT AS $$
DECLARE
    v_table TEXT;
    v_total BIGINT := 0;
    v_n     BIGINT;
BEGIN
    FOREACH v_table IN ARRAY ARRAY['samna_user', 'claim', 'user_claim', 'claim_object', 'samna_client',
                                   'samna_secret', 'object_field', 'user_field', 'user_relation'] LOOP
        EXIT WHEN p_limit IS NOT NULL AND v_total >= p_limit;
        EXECUTE format('SELECT count(*) FROM (SELECT 1 FROM claimius.%I WHERE app_id = $1 LIMIT $2) t', v_table)
            INTO v_n USING p_app_id, p_limit - v_total;
        v_total := v_total + v_n;
    END LOOP;

    IF p_limit IS NULL OR v_total < p_limit THEN
        SELECT count(*) INTO v_n
        FROM (SELECT 1 FROM claimius.app_object ao
              WHERE ao.app_id = p_app_id
                AND ao.object_type IN ('claimius.organization', 'claimius.location')
                AND ao.sa_deleted_at IS NULL
              LIMIT p_limit - v_total) t;
        v_total := v_total + v_n;
    END IF;

    RETURN v_total;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.count_sync_source_rows(UUID, BIGINT) IS 'Source rows of one app the sync snapshot ships, counted up to p_limit when given.';

-- ----------------------------------------------------------------------------
-- update_sync_cursor
-- Records that one consumer of an app applied everything up to the position
-- (p_txid, p_seq), which it proves by asking for what comes after it. The
-- app's prophet_state row carries the lowest cursor among its consumers in
-- contact and the contact itself.
-- ----------------------------------------------------------------------------
DROP FUNCTION IF EXISTS claimius.update_sync_cursor(UUID, UUID, BIGINT);

CREATE OR REPLACE FUNCTION claimius.update_sync_cursor(
    p_app_id        UUID,
    p_consumer_id   UUID,
    p_txid          BIGINT,
    p_seq           BIGINT
) RETURNS VOID AS $$
    INSERT INTO claimius.sync_consumer (app_id, consumer_id, last_applied_txid, last_applied_seq, last_contact_at)
    VALUES (p_app_id, p_consumer_id, p_txid::TEXT::XID8, p_seq, now())
    ON CONFLICT (app_id, consumer_id) DO UPDATE SET
        last_applied_txid = EXCLUDED.last_applied_txid,
        last_applied_seq  = EXCLUDED.last_applied_seq,
        last_contact_at   = EXCLUDED.last_contact_at;

    INSERT INTO claimius.prophet_state (app_id, last_applied_txid, last_applied_seq, sync_mode, last_contact_at, next_attempt_at)
    VALUES (p_app_id, p_txid::TEXT::XID8, p_seq, 'active', now(), now())
    ON CONFLICT (app_id) DO UPDATE SET
        (last_applied_txid, last_applied_seq) = (
            SELECT c.last_applied_txid, c.last_applied_seq FROM claimius.sync_consumer c
            WHERE c.app_id = p_app_id AND c.last_contact_at > now() - INTERVAL '7 days'
            ORDER BY c.last_applied_txid, c.last_applied_seq
            LIMIT 1),
        sync_mode        = 'active',
        sync_failures    = 0,
        last_contact_at  = now(),
        next_attempt_at  = now(),
        sa_updated_at    = now();
$$ LANGUAGE sql;

COMMENT ON FUNCTION claimius.update_sync_cursor(UUID, UUID, BIGINT, BIGINT) IS 'Moves one consumer''s (txid, seq) cursor for an app and records the contact on the app''s prophet_state row.';

-- ----------------------------------------------------------------------------
-- trim_sync_event
-- Removes outbox rows every consumer has already applied, and caps each app's
-- outbox at its source rows. sync_consumer holds one (txid, seq) cursor per
-- consumer, advanced when it asks for what comes after it, so a row at or
-- below the lowest cursor across the consumers still in contact is owed to
-- nobody. A consumer whose last contact is older than p_stale_after is
-- treated as gone rather than allowed to pin the watermark forever. Past
-- that, an app's events beyond as many as it has source rows are removed: a
-- consumer further behind is cheaper served by the snapshot. The highest
-- position removed per app is kept in prophet_state (trimmed_txid,
-- trimmed_seq), and check_sync_cursor sends a consumer below it to the
-- snapshot. Returns the number of rows deleted.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius.trim_sync_event(
    p_stale_after   INTERVAL DEFAULT INTERVAL '7 days'
) RETURNS BIGINT AS $$
DECLARE
    v_watermark RECORD;
    v_deleted   BIGINT := 0;
    v_n         BIGINT;
    v_app       UUID;
    v_cut       RECORD;
BEGIN
    SELECT c.last_applied_txid AS txid, c.last_applied_seq AS seq INTO v_watermark
    FROM claimius.sync_consumer c
    WHERE c.last_contact_at > now() - p_stale_after
    ORDER BY c.last_applied_txid, c.last_applied_seq
    LIMIT 1;

    IF FOUND AND v_watermark.txid > '0'::XID8 THEN
        WITH gone AS (
            DELETE FROM claimius.sync_event e WHERE (e.txid, e.seq) <= (v_watermark.txid, v_watermark.seq)
            RETURNING e.app_id, e.txid, e.seq
        ),
        marked AS (
            UPDATE claimius.prophet_state ps
            SET (trimmed_txid, trimmed_seq) = (
                SELECT t.txid, t.seq FROM (VALUES (ps.trimmed_txid, ps.trimmed_seq), (g.txid, g.seq)) t (txid, seq)
                ORDER BY t.txid DESC, t.seq DESC LIMIT 1)
            FROM (SELECT DISTINCT ON (app_id) app_id, txid, seq FROM gone
                  WHERE app_id IS NOT NULL ORDER BY app_id, txid DESC, seq DESC) g
            WHERE ps.app_id = g.app_id
        )
        SELECT count(*) INTO v_n FROM gone;
        v_deleted := v_n;
    END IF;

    FOR v_app IN SELECT ps.app_id FROM claimius.prophet_state ps LOOP
        SELECT e.txid, e.seq INTO v_cut
        FROM claimius.sync_event e
        WHERE e.app_id = v_app
        ORDER BY e.txid DESC, e.seq DESC
        OFFSET claimius.count_sync_source_rows(v_app) LIMIT 1;
        CONTINUE WHEN NOT FOUND;

        DELETE FROM claimius.sync_event e WHERE e.app_id = v_app AND (e.txid, e.seq) <= (v_cut.txid, v_cut.seq);
        GET DIAGNOSTICS v_n = ROW_COUNT;
        UPDATE claimius.prophet_state ps
        SET (trimmed_txid, trimmed_seq) = (v_cut.txid, v_cut.seq)
        WHERE ps.app_id = v_app AND (ps.trimmed_txid, ps.trimmed_seq) < (v_cut.txid, v_cut.seq);
        v_deleted := v_deleted + v_n;
    END LOOP;

    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.trim_sync_event(INTERVAL) IS 'Deletes sync_event rows at or below the lowest (txid, seq) cursor among consumers still in contact, then each app''s events beyond as many as its source rows, recording the highest position removed per app in prophet_state (trimmed_txid, trimmed_seq). Run on a schedule.';

-- ----------------------------------------------------------------------------
-- insert_app_handover
-- The home of an app offers it to a peer prophet. From the offer on this
-- prophet decides none of the app's writes, so the peer, which mirrors the
-- app and receives the offer on its feed, holds every row the app will have
-- when it accepts. An offer is not withdrawn: a withdrawal racing the
-- acceptance would leave the app two homes.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.offer_handover(uuid,text,uuid);
CREATE OR REPLACE FUNCTION claimius.insert_app_handover(
    p_app_id        UUID,
    p_to_prophet    TEXT,
    p_sa_created_by UUID
) RETURNS JSONB AS $$
DECLARE
    v_local TEXT := (SELECT slug FROM claimius.get_app());
    v_app   claimius.samna_app;
    v_home  TEXT;
    v_row   claimius.app_handover;
BEGIN
    SELECT * INTO v_app FROM claimius.samna_app WHERE id = p_app_id AND sa_deleted_at IS NULL FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'insert_app_handover: app % not found', p_app_id USING ERRCODE = 'P0404';
    END IF;

    v_home := claimius.get_app_home(p_app_id);
    IF v_home IS NOT NULL THEN
        RAISE EXCEPTION 'insert_app_handover: app % is decided by prophet %, only its home offers it', v_app.slug, v_home
            USING ERRCODE = 'P0409', HINT = v_home;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM claimius.prophet_peer WHERE prophet = p_to_prophet AND prophet <> v_local AND status = 'active') THEN
        RAISE EXCEPTION 'insert_app_handover: % is not an active peer of this prophet', p_to_prophet USING ERRCODE = 'P0409';
    END IF;

    INSERT INTO claimius.app_handover (app_id, from_prophet, to_prophet, sa_created_by)
    VALUES (p_app_id, v_local, p_to_prophet, p_sa_created_by)
    RETURNING * INTO v_row;

    PERFORM claimius.insert_audit(p_app_id, 'insert', 'info', 'claimius.app_handover', v_row.id, v_app.sa_owner_id,
                                 p_sa_created_by, format('app %s offered to prophet %s', v_app.slug, p_to_prophet));

    RETURN jsonb_build_object('app_handover', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.insert_app_handover(UUID, TEXT, UUID) IS 'Offers an app this prophet is home for to an active peer. The app''s writes are refused here from the offer on. Returns {"app_handover": {...}}.';

-- ----------------------------------------------------------------------------
-- update_app_handover
-- The peer an app was offered to takes it. The offer reached this prophet
-- on the app's feed from its home, so the acceptance is only possible here
-- for an app this prophet mirrors from the prophet that offered it. The
-- acceptance makes this prophet the home and travels back on the app's feed
-- from here, where the old home learns it.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.accept_handover(uuid,uuid);
CREATE OR REPLACE FUNCTION claimius.update_app_handover(
    p_handover_id   UUID,
    p_sa_created_by UUID
) RETURNS JSONB AS $$
DECLARE
    v_local TEXT := (SELECT slug FROM claimius.get_app());
    v_row   claimius.app_handover;
    v_app   claimius.samna_app;
BEGIN
    SELECT * INTO v_row FROM claimius.app_handover WHERE id = p_handover_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'update_app_handover: handover % not found', p_handover_id USING ERRCODE = 'P0404';
    END IF;

    SELECT * INTO v_app FROM claimius.samna_app WHERE id = v_row.app_id FOR UPDATE;

    IF v_row.status <> 'offered' THEN
        RAISE EXCEPTION 'update_app_handover: handover of app % is %', v_app.slug, v_row.status USING ERRCODE = 'P0409';
    END IF;
    IF v_row.to_prophet <> v_local THEN
        RAISE EXCEPTION 'update_app_handover: app % is offered to prophet %, not this one', v_app.slug, v_row.to_prophet
            USING ERRCODE = 'P0409', HINT = v_row.to_prophet;
    END IF;
    IF v_app.home_prophet IS DISTINCT FROM v_row.from_prophet THEN
        RAISE EXCEPTION 'update_app_handover: app % is not mirrored from prophet %', v_app.slug, v_row.from_prophet
            USING ERRCODE = 'P0409';
    END IF;

    UPDATE claimius.app_handover SET status = 'accepted' WHERE id = v_row.id RETURNING * INTO v_row;
    UPDATE claimius.samna_app SET home_prophet = NULL WHERE id = v_row.app_id;

    PERFORM claimius.insert_audit(v_row.app_id, 'update', 'info', 'claimius.app_handover', v_row.id, v_app.sa_owner_id,
                                 p_sa_created_by, format('app %s accepted from prophet %s', v_app.slug, v_row.from_prophet));

    RETURN jsonb_build_object('app_handover', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_app_handover(UUID, UUID) IS 'Accepts a handover offered to this prophet by the home of an app it mirrors, and makes this prophet the app''s home. Returns {"app_handover": {...}}.';

-- ----------------------------------------------------------------------------
-- _check_peer_row
-- Whether a row a peer's feed carries may apply here. Ids are random, so a
-- row id held here by another app, or by an app another prophet decides, is
-- a collision: the row is refused rather than merged. NULL when the row may
-- apply, otherwise the reason.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius._check_peer_row(
    p_prophet       TEXT,
    p_app_slug      TEXT,
    p_event_type    TEXT,
    p_payload       JSONB
) RETURNS TEXT AS $$
DECLARE
    v_id        UUID := (p_payload ->> 'id')::UUID;
    v_app       claimius.samna_app;
    v_home      TEXT;
    v_other     UUID;
BEGIN
    SELECT * INTO v_app FROM claimius.samna_app WHERE slug = p_app_slug;
    v_home := claimius.get_app_home(v_app.id);

    IF p_event_type = 'claimius.samna_app' THEN
        IF p_payload ->> 'slug' IS DISTINCT FROM p_app_slug THEN
            RETURN format('the feed of app %s carries app %s', p_app_slug, p_payload ->> 'slug');
        END IF;
        IF v_app.id IS NOT NULL AND v_app.id <> v_id THEN
            RETURN format('slug %s is held here by app %s', p_app_slug, v_app.id);
        END IF;
        IF v_app.id IS NULL AND EXISTS (SELECT 1 FROM claimius.samna_app WHERE id = v_id) THEN
            RETURN format('id %s is held here by another app', v_id);
        END IF;
    ELSIF v_app.id IS NULL THEN
        RETURN format('app %s is not mirrored here', p_app_slug);
    END IF;

    IF v_app.id IS NOT NULL AND v_home IS DISTINCT FROM p_prophet THEN
        RETURN format('app %s is decided by %s, not prophet %s', p_app_slug, coalesce(v_home, 'this prophet'), p_prophet);
    END IF;

    IF p_event_type IN ('claimius.organization', 'claimius.location') THEN
        SELECT ao.app_id INTO v_other
        FROM claimius.app_object ao
        WHERE ao.object_type = p_event_type
          AND ao.object_id = v_id
          AND ao.sa_deleted_at IS NULL
          AND claimius.get_app_home(ao.app_id) IS DISTINCT FROM p_prophet
        LIMIT 1;
        IF v_other IS NOT NULL THEN
            RETURN format('%s %s is held here by app %s of another home', p_event_type, v_id, v_other);
        END IF;
    ELSIF p_event_type NOT IN ('claimius.samna_app', 'claimius.app_handover') THEN
        EXECUTE format('SELECT app_id FROM claimius.%I WHERE id = $1', split_part(p_event_type, '.', 2))
            INTO v_other USING v_id;
        IF v_other IS NOT NULL AND v_other <> v_app.id THEN
            RETURN format('%s %s is held here by app %s', p_event_type, v_id, v_other);
        END IF;
    ELSIF p_event_type = 'claimius.app_handover' THEN
        SELECT h.app_id INTO v_other FROM claimius.app_handover h WHERE h.id = v_id;
        IF v_other IS NOT NULL AND v_other <> v_app.id THEN
            RETURN format('%s %s is held here by app %s', p_event_type, v_id, v_other);
        END IF;
    END IF;

    RETURN NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._check_peer_row(TEXT, TEXT, TEXT, JSONB) IS 'Reason a row from a peer''s feed of an app is refused, or NULL when it may apply: the app must be decided by that peer, and the row id must not be held here by another app or by an app another prophet decides.';

-- ----------------------------------------------------------------------------
-- apply_peer_event
-- Applies one row of a peer's feed of an app this prophet mirrors, under
-- replay_mode, in the shape the feed carries it. A colliding row is refused
-- and recorded in peer_collision. An organization or location is linked to
-- the mirrored app; its delete is the app's unlink. The app row keeps the
-- peer as its home. A handover this prophet offered comes back accepted on
-- the new home's feed, and moves the app's home there. Returns false when
-- the row was refused.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.apply_peer_event(
    p_prophet       TEXT,
    p_app_slug      TEXT,
    p_operation     TEXT,
    p_event_type    TEXT,
    p_payload       JSONB
) RETURNS BOOLEAN AS $$
DECLARE
    v_local     TEXT := (SELECT slug FROM claimius.get_app());
    v_payload   JSONB := p_payload - 'txid';
    v_id        UUID := (p_payload ->> 'id')::UUID;
    v_table     TEXT := split_part(p_event_type, '.', 2);
    v_app_id    UUID;
    v_held      claimius.app_handover;
    v_reason    TEXT;
    v_cols      TEXT;
    v_new       TEXT;
    v_old       TEXT;
    v_row       JSONB;
    v_changed   BOOLEAN := TRUE;
BEGIN
    IF NOT claimius.in_replay_mode() THEN
        RAISE EXCEPTION 'apply_peer_event: a peer''s feed applies under replay_mode';
    END IF;
    IF p_event_type NOT IN ('claimius.samna_app', 'claimius.app_handover', 'claimius.organization', 'claimius.location',
                            'claimius.samna_user', 'claimius.claim', 'claimius.user_claim', 'claimius.claim_object',
                            'claimius.samna_client', 'claimius.samna_secret', 'claimius.object_field',
                            'claimius.user_field', 'claimius.user_relation') THEN
        RAISE EXCEPTION 'apply_peer_event: unknown event type %', p_event_type;
    END IF;
    IF p_operation NOT IN ('upsert', 'delete') THEN
        RAISE EXCEPTION 'apply_peer_event: unknown operation %', p_operation;
    END IF;

    IF p_event_type = 'claimius.app_handover' THEN
        SELECT * INTO v_held FROM claimius.app_handover WHERE id = v_id;
        IF FOUND AND v_held.from_prophet = v_local AND v_held.to_prophet = p_prophet
            AND v_held.status = 'offered' AND p_payload ->> 'status' = 'accepted' THEN
            UPDATE claimius.app_handover SET status = 'accepted', sa_updated_at = (p_payload ->> 'sa_updated_at')::TIMESTAMPTZ
            WHERE id = v_id;
            UPDATE claimius.samna_app SET home_prophet = p_prophet WHERE id = v_held.app_id;
            RETURN TRUE;
        END IF;
        IF FOUND AND v_held.status = p_payload ->> 'status' AND v_held.from_prophet = p_payload ->> 'from_prophet'
            AND v_held.to_prophet = p_payload ->> 'to_prophet' AND v_held.app_id = (p_payload ->> 'app_id')::UUID THEN
            RETURN TRUE;
        END IF;
        IF FOUND THEN
            v_reason := format('handover %s held here as %s from %s to %s', v_id, v_held.status, v_held.from_prophet, v_held.to_prophet);
        ELSIF p_payload ->> 'from_prophet' IS DISTINCT FROM p_prophet THEN
            v_reason := format('prophet %s carries a handover from %s', p_prophet, p_payload ->> 'from_prophet');
        END IF;
    END IF;

    v_reason := coalesce(v_reason, claimius._check_peer_row(p_prophet, p_app_slug, p_event_type, v_payload));
    IF v_reason IS NOT NULL THEN
        INSERT INTO claimius.peer_collision (prophet, app_slug, object_type, object_id, reason, payload)
        VALUES (p_prophet, p_app_slug, p_event_type, v_id, v_reason, p_payload);
        RETURN FALSE;
    END IF;

    SELECT id INTO v_app_id FROM claimius.samna_app WHERE slug = p_app_slug;

    IF p_operation = 'delete' THEN
        IF v_table IN ('organization', 'location') THEN
            PERFORM claimius.delete_app_object(v_app_id, p_event_type, v_id);
            RETURN TRUE;
        END IF;
        EXECUTE format('DELETE FROM claimius.%I t WHERE t.id = $1 RETURNING to_jsonb(t)', v_table) INTO v_row USING v_id;
        v_changed := v_row IS NOT NULL;
    ELSE
        IF v_table = 'samna_app' THEN
            v_payload := v_payload || jsonb_build_object('home_prophet', p_prophet);
        ELSIF v_table = 'samna_user' THEN
            UPDATE claimius.samna_user SET sa_deleted_at = now()
            WHERE user_id = (v_payload ->> 'user_id')::UUID
              AND app_id = v_app_id
              AND id <> v_id
              AND sa_deleted_at IS NULL;
        ELSIF v_table IN ('organization', 'location') THEN
            PERFORM claimius.upsert_app_object(v_app_id, p_event_type, v_id, (v_payload ->> 'sa_created_by')::UUID);
        END IF;

        SELECT string_agg(quote_ident(a.attname), ', ' ORDER BY a.attnum),
               string_agg('EXCLUDED.' || quote_ident(a.attname), ', ' ORDER BY a.attnum),
               string_agg('t.' || quote_ident(a.attname), ', ' ORDER BY a.attnum)
        INTO v_cols, v_new, v_old
        FROM pg_attribute a
        WHERE a.attrelid = format('claimius.%I', v_table)::REGCLASS
          AND a.attnum > 0
          AND NOT a.attisdropped
          AND a.attgenerated = ''
          AND v_payload ? a.attname::TEXT;

        EXECUTE format(
            'INSERT INTO claimius.%1$I AS t (%2$s) SELECT %2$s FROM jsonb_populate_record(NULL::claimius.%1$I, $1) '
            'ON CONFLICT (id) DO UPDATE SET (%2$s) = ROW(%3$s) WHERE ROW(%4$s) IS DISTINCT FROM ROW(%3$s) '
            'RETURNING to_jsonb(t)',
            v_table, v_cols, v_new, v_old)
            INTO v_row USING v_payload;
        v_changed := v_row IS NOT NULL;
    END IF;

    IF NOT v_changed THEN
        RETURN TRUE;
    END IF;

    IF v_table = 'claim' THEN
        PERFORM claimius._raise_claim_diff(v_app_id, v_id);
    ELSIF v_table = 'user_claim' THEN
        PERFORM claimius._raise_claim_diff(v_app_id, (v_row ->> 'claim_id')::UUID, (v_row ->> 'user_id')::UUID);
    ELSIF v_table = 'claim_object' THEN
        PERFORM claimius._raise_claim_diff(v_app_id, (v_row ->> 'claim_id')::UUID);
        IF v_row ->> 'object_type' = 'claimius.claim' THEN
            PERFORM claimius._raise_claim_diff(v_app_id, (v_row ->> 'object_id')::UUID);
        END IF;
    END IF;

    RETURN TRUE;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.apply_peer_event(TEXT, TEXT, TEXT, TEXT, JSONB) IS 'Applies one row of a peer''s feed of a mirrored app under replay_mode, refusing and recording a colliding id in peer_collision. Returns false for a refused row.';

CREATE OR REPLACE FUNCTION claimius.insert_secret(
    p_app_id        UUID,
    p_key           TEXT,
    p_value         TEXT,
    p_expires_at    TIMESTAMPTZ,
    p_sa_owner_id   UUID,
    p_sa_created_by UUID
) RETURNS JSONB AS $$
DECLARE
    v_claim_id UUID;
    v_root_id  UUID;
    v_secret   claimius.samna_secret;
    v_binding  JSONB;
BEGIN
    SELECT claim_id INTO v_claim_id
      FROM claimius.user_claim
     WHERE id = p_sa_created_by
       AND app_id = p_app_id
       AND sa_deleted_at IS NULL;
    IF v_claim_id IS NULL THEN
        RAISE EXCEPTION 'created_by invalid';
    END IF;

    SELECT sa_root_id INTO v_root_id
      FROM claimius.organization
     WHERE id = p_sa_owner_id
       AND sa_deleted_at IS NULL
       AND claimius.app_has_object(p_app_id, 'claimius.organization', id);
    IF v_root_id IS NULL THEN
        RAISE EXCEPTION 'owner_id invalid';
    END IF;

    INSERT INTO claimius.samna_secret (app_id, key, value, expires_at, sa_owner_id, sa_created_by)
    VALUES (p_app_id, p_key, p_value, p_expires_at, p_sa_owner_id, p_sa_created_by)
    RETURNING * INTO v_secret;

    v_binding := claimius.insert_claim_object(
        p_app_id        => p_app_id,
        p_claim_id      => v_claim_id,
        p_sa_owner_id   => p_sa_owner_id,
        p_sa_root_id    => v_root_id,
        p_sa_created_by => p_sa_created_by,
        p_object_id     => v_secret.id,
        p_object_type   => 'claimius.samna_secret',
        p_sa_access     => 7,
        p_inherits      => TRUE
    );

    RETURN jsonb_build_object(
        'samna_secret', to_jsonb(v_secret),
        'claim_object', v_binding -> 'claim_object'
    );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.insert_secret(UUID, TEXT, TEXT, TIMESTAMPTZ, UUID, UUID) IS 'Stores a secret of an app, its value already encrypted under the app seed, owned by an organization linked to the app, and binds it to the claim of the actor p_sa_created_by with owner, write and read, so the secret is reached through the actor''s claim on the prophet and on every disciple of the app. Returns samna_secret and claim_object.';

CREATE OR REPLACE FUNCTION claimius.upsert_peer_grant(
    p_app_id        UUID,
    p_prophet       TEXT,
    p_secret        TEXT,
    p_sa_created_by UUID
) RETURNS JSONB AS $$
DECLARE
    v_home TEXT;
    v_row  claimius.peer_grant;
BEGIN
    IF coalesce(p_prophet, '') = '' OR coalesce(p_secret, '') = '' THEN
        RAISE EXCEPTION 'upsert_peer_grant: the peer prophet and its secret are required';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM claimius.samna_app WHERE id = p_app_id AND sa_deleted_at IS NULL) THEN
        RAISE EXCEPTION 'upsert_peer_grant: app % not found', p_app_id USING ERRCODE = 'P0404';
    END IF;
    v_home := claimius.get_app_home(p_app_id);
    IF v_home IS NOT NULL THEN
        RAISE EXCEPTION 'upsert_peer_grant: app % is decided by prophet %, only its home grants peers', p_app_id, v_home
            USING ERRCODE = 'P0409', HINT = v_home;
    END IF;

    INSERT INTO claimius.peer_grant (app_id, prophet, secret_hash, sa_created_by)
    VALUES (p_app_id, p_prophet, encode(digest(p_secret, 'sha256'), 'hex'), p_sa_created_by)
    ON CONFLICT (app_id, prophet) WHERE sa_deleted_at IS NULL DO UPDATE SET
        secret_hash   = EXCLUDED.secret_hash,
        status        = 'active',
        sa_updated_at = now()
    RETURNING * INTO v_row;

    RETURN jsonb_build_object('peer_grant', to_jsonb(v_row) - 'secret_hash');
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_peer_grant(UUID, TEXT, TEXT, UUID) IS 'Grants a peer prophet the app this prophet is home for, keyed by a random peer secret stored as its SHA-256. The peer keeps the raw secret in its prophet_peer.peer_secret and presents it to be served the feed and the snapshot as a peer. A second call for the same peer replaces the secret. Returns {"peer_grant": {...}} without the hash.';

CREATE OR REPLACE FUNCTION claimius.get_peer_grant(
    p_app_id UUID,
    p_secret TEXT
) RETURNS SETOF claimius.peer_grant AS $$
    SELECT *
    FROM claimius.peer_grant
    WHERE app_id = p_app_id
      AND secret_hash = encode(digest(coalesce(p_secret, ''), 'sha256'), 'hex')
      AND status = 'active'
      AND sa_deleted_at IS NULL
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_peer_grant(UUID, TEXT) IS 'The live grant of the app whose peer secret is p_secret, or no row. A prophet serves an app as a peer only to a pull presenting such a secret.';

CREATE OR REPLACE FUNCTION claimius.upsert_guest_user(
    p_app_id        UUID,
    p_claim_id      UUID,
    p_actor_user_id UUID,
    p_guest_user_id UUID
) RETURNS JSONB AS $$
DECLARE
    v_claim    claimius.claim;
    v_actor_uc UUID;
    v_user     claimius.samna_user;
    v_grant    claimius.user_claim;
BEGIN
    SELECT * INTO v_claim
      FROM claimius.claim
     WHERE id = p_claim_id
       AND app_id = p_app_id
       AND sa_deleted_at IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'upsert_guest_user: claim % not found in the app', p_claim_id USING ERRCODE = 'P0404';
    END IF;

    SELECT a.user_claim_id INTO v_actor_uc
      FROM claimius.get_access(p_actor_user_id, p_app_id, p_claim_id, 'claimius.claim', 1) a
     LIMIT 1;
    IF v_actor_uc IS NULL THEN
        SELECT a.user_claim_id INTO v_actor_uc
          FROM claimius.get_direct_access(p_actor_user_id, p_app_id, p_claim_id, 'claimius.claim', 1) a
         LIMIT 1;
    END IF;
    IF v_actor_uc IS NULL THEN
        RAISE EXCEPTION 'upsert_guest_user: the actor does not own claim %', p_claim_id USING ERRCODE = 'P0403';
    END IF;

    INSERT INTO claimius.samna_user (user_id, app_id, first_name, status, type)
    VALUES (p_guest_user_id, p_app_id, 'Guest', 'active', 'guest')
    ON CONFLICT (user_id, app_id) WHERE sa_deleted_at IS NULL DO UPDATE SET
        sa_updated_at = claimius.samna_user.sa_updated_at
    RETURNING * INTO v_user;

    SELECT * INTO v_grant
      FROM claimius.user_claim
     WHERE app_id = p_app_id
       AND claim_id = p_claim_id
       AND user_id = p_guest_user_id
       AND sa_deleted_at IS NULL
     LIMIT 1;
    IF NOT FOUND THEN
        v_grant := jsonb_populate_record(NULL::claimius.user_claim, claimius.insert_user_claim(
            p_app_id        => p_app_id,
            p_claim_id      => p_claim_id,
            p_user_id       => p_guest_user_id,
            p_sa_owner_id   => v_claim.sa_owner_id,
            p_sa_created_by => v_actor_uc
        ) -> 'user_claim');
    END IF;

    RETURN jsonb_build_object(
        'samna_user', to_jsonb(v_user),
        'user_claim', to_jsonb(v_grant)
    );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_guest_user(UUID, UUID, UUID, UUID) IS 'Ensures the guest user of a claim in an app, a samna_user of type guest, and its grant of the claim, made under the grant by which the actor owns the claim. The guest and its grant are written on the prophet, which alone signs the guest token, and reach the app''s disciples through the feed. Raises P0404 for a claim outside the app and P0403 when the actor does not own it. Returns samna_user and user_claim.';
