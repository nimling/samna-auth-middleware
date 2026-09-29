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

CREATE OR REPLACE FUNCTION claimius.get_access(
    p_user_id          UUID,
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
    PERFORM claimius.reconcile_if_pending(p_user_id, p_object_type, p_object_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        WITH uo AS (
            SELECT u.*, oi.sa_owner_id, oi.sa_location_id
            FROM claimius.user_object u
                LEFT JOIN claimius.object_info oi ON oi.object_type = u.object_type AND oi.object_id = u.object_id
            WHERE u.user_id = p_user_id
              AND u.object_type = p_object_type
              AND u.object_id = p_object_id
              AND (u.sa_access & p_required_access) = p_required_access
        )
        SELECT
            claimius.composite_id(p_user_id::TEXT, p_object_type, p_object_id::TEXT),
            best.user_claim_id,
            best.claim_id,
            best.claim_name,
            best.access_bits,
            uo.scope,
            uo.sa_owner_id,
            (SELECT o.sa_root_id FROM claimius.organization o WHERE o.id = uo.sa_owner_id),
            uo.sa_location_id,
            FALSE
        FROM uo
                 CROSS JOIN LATERAL claimius._best_grant(uo.grants, p_user_id, p_required_access) best;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_access(UUID, UUID, TEXT, INTEGER) IS 'Strongest active claim based grant for one (user, app, object) whose mask covers p_required_access. user_claim_id is the actor token for follow-on writes. sa_root_id is the owning organization root, suitable for FK to claimius.organization on follow-on claim and claim_object writes.';

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

DROP FUNCTION IF EXISTS claimius.get_direct_access(UUID, UUID, TEXT);
CREATE OR REPLACE FUNCTION claimius.get_direct_access(
    p_user_id       UUID,
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
    PERFORM claimius.reconcile_if_pending(p_user_id, p_object_type, p_object_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    IF p_object_type = 'claimius.samna_user' AND p_object_id = p_user_id THEN
        RETURN QUERY
            SELECT
                claimius.composite_id(p_user_id::TEXT, p_object_type, p_object_id::TEXT),
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
              AND TRUE
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
            SELECT uc.id AS user_claim_id, c.id AS claim_id, c.name, c.sa_access, c.inherits
            FROM claimius.user_claim uc
                     JOIN claimius.claim c
                          ON c.id = uc.claim_id
                              AND c.sa_deleted_at IS NULL
            WHERE uc.user_id = p_user_id
              AND uc.sa_deleted_at IS NULL
              AND (uc.starts_at IS NULL OR uc.starts_at <= now())
              AND (uc.ends_at IS NULL OR uc.ends_at > now())
        ),
        derivations AS (
            SELECT h.user_claim_id, h.claim_id, h.name, a.hop,
                   claimius._effective_access(h.sa_access, co.sa_access) AS e,
                   co.scope, co.sa_owner_id, co.sa_root_id
            FROM anc a
                     JOIN claimius.claim_object co
                          ON TRUE
                              AND co.object_type = a.object_type
                              AND co.object_id = a.object_id
                              AND co.sa_deleted_at IS NULL
                     JOIN held h ON h.claim_id = co.claim_id
            WHERE a.hop = 0 OR (h.inherits AND co.inherits)

            UNION ALL

            SELECT h.user_claim_id, h.claim_id, h.name, a.hop,
                   (claimius._effective_access(h.sa_access, link.sa_access)
                        & claimius._effective_access(cb.sa_access, co.sa_access) & ~16)
                   | ((claimius._effective_access(h.sa_access, link.sa_access)
                        | claimius._effective_access(cb.sa_access, co.sa_access)) & 16),
                   co.scope, co.sa_owner_id, co.sa_root_id
            FROM anc a
                     JOIN claimius.claim_object co
                          ON TRUE
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
                              AND link.sa_deleted_at IS NULL
                              AND link.inherits
                     JOIN held h ON h.claim_id = link.claim_id
            WHERE (a.hop = 0 OR co.inherits)
              AND p_object_type <> 'claimius.claim'
        ),
        denied AS (
            SELECT coalesce(bit_or(d.e & 15) FILTER (WHERE d.hop = 0), 0) AS direct_bits,
                   coalesce(bit_or(d.e & 15), 0) AS all_bits
            FROM derivations d
            WHERE (d.e & 16) <> 0
              AND d.scope IS NULL
        ),
        settled AS (
            SELECT d.*,
                   claimius._close_access(d.e & 15 & ~CASE WHEN d.hop = 0 THEN x.direct_bits ELSE x.all_bits END) AS access
            FROM derivations d
                     CROSS JOIN denied x
            WHERE (d.e & 16) = 0
        )
        SELECT
            claimius.composite_id(p_user_id::TEXT, p_object_type, p_object_id::TEXT),
            s.user_claim_id,
            s.claim_id,
            s.name,
            s.access,
            s.scope,
            s.sa_owner_id,
            s.sa_root_id,
            NULL::UUID,
            FALSE
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
              AND TRUE
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

COMMENT ON FUNCTION claimius.get_direct_access(UUID, UUID, TEXT, INTEGER, BOOLEAN) IS 'Locates the closest, strongest user_claim the user holds related to the given object via ancestor walk, directly or through composition, with the binding narrowing its claim and the user''s denies subtracted by class. VOLATILE: it settles through reconcile_if_pending.';

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
DROP FUNCTION IF EXISTS claimius.get_objects(UUID, TEXT, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_objects(
    p_user_id          UUID,
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
    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

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
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_user_id, p_required_access) best ON TRUE
                 LEFT JOIN claimius.object_info oi
                           ON oi.object_type = claimius.user_object.object_type
                          AND oi.object_id = claimius.user_object.object_id
        WHERE claimius.user_object.user_id = p_user_id
          AND claimius.user_object.object_type = p_object_type
          AND claimius.user_object.object_id > coalesce(p_after, '00000000-0000-0000-0000-000000000000'::UUID)
          AND (claimius.user_object.sa_access & p_required_access) >= p_required_access
          AND (best.user_claim_id IS NOT NULL OR claimius.user_object.direct_grant)
        ORDER BY claimius.user_object.object_id
        LIMIT p_limit;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_objects(p_user_id uuid, p_object_type text, p_required_access integer, p_after uuid, p_limit integer) IS 'Object ids and metadata for objects whose mask covers p_required_access, in object_id order after p_after, at most p_limit rows. user_claim_id is the actor token from the strongest surviving claim grant; null when only direct_grant applies. claim_id and claim_name describe the claim that contributed the strongest grant. sa_root_id is derived from the owning organization root.';

-- ----------------------------------------------------------------------------
-- get_organizations
-- Returns organizations whose mask covers p_required_access.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_organizations(UUID, UUID, INTEGER);
DROP FUNCTION IF EXISTS claimius.get_organizations(UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_organizations(
    p_user_id          UUID,
    p_required_access  INTEGER DEFAULT 0,
    p_after            UUID DEFAULT NULL,
    p_limit            INTEGER DEFAULT NULL
) RETURNS SETOF claimius.organization AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT o.*
        FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required_access, p_after, p_limit) g
            JOIN claimius.organization o ON o.id = g.object_id
        WHERE o.sa_deleted_at IS NULL
        ORDER BY o.id;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_organizations(p_user_id uuid, p_required_access integer, p_after uuid, p_limit integer) IS 'Organizations whose effective mask covers p_required_access, one get_objects page in id order.';

-- ----------------------------------------------------------------------------
-- get_locations
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_locations(UUID, UUID, INTEGER);
DROP FUNCTION IF EXISTS claimius.get_locations(UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_locations(
    p_user_id          UUID,
    p_required_access  INTEGER DEFAULT 0,
    p_after            UUID DEFAULT NULL,
    p_limit            INTEGER DEFAULT NULL
) RETURNS SETOF claimius.location AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT l.*
        FROM claimius.get_objects(p_user_id, 'claimius.location', p_required_access, p_after, p_limit) g
            JOIN claimius.location l ON l.id = g.object_id
        WHERE l.sa_deleted_at IS NULL
        ORDER BY l.id;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_locations(p_user_id uuid, p_required_access integer, p_after uuid, p_limit integer) IS 'Locations whose effective mask covers p_required_access, one get_objects page in id order.';

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

DROP FUNCTION IF EXISTS claimius.get_user_access(UUID, UUID, INTEGER);
CREATE OR REPLACE FUNCTION claimius.get_user_access(
    p_actor_id         UUID,
    p_target           TEXT,
    p_required_access  INTEGER DEFAULT 0
) RETURNS SETOF claimius.access_user AS $$
DECLARE
    v_target_user_id UUID;
    v_uuid           UUID;
BEGIN
    PERFORM claimius.reconcile_if_pending(p_actor_id);

    IF NOT claimius.check_user_active(p_actor_id) THEN
        RETURN;
    END IF;

    IF p_target ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        v_uuid := p_target::UUID;
    END IF;

    SELECT su.user_id INTO v_target_user_id
    FROM claimius.samna_user su
    WHERE TRUE
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
            WHERE TRUE
              AND uu.viewer_id     = p_actor_id
              AND uu.target_user_id = v_target_user_id

            UNION ALL

            SELECT g.access, g.user_claim_id, g.claim_id, g.claim_name, g.owner_id, g.root_id, NULL::UUID, FALSE
            FROM claimius._user_claim_grants(p_actor_id, v_target_user_id) g
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

COMMENT ON FUNCTION claimius.get_user_access(UUID, TEXT, INTEGER) IS
    'Resolved actor access on one target samna_user. OR of self, member co-visibility, and direct claim branches. Empty result means the required mask is not covered.';

DROP FUNCTION IF EXISTS claimius.get_users(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_users(
    p_user_id          UUID,
    p_required_access  INTEGER DEFAULT 0
) RETURNS SETOF claimius.samna_user AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        WITH branches AS (
            SELECT p_user_id AS target_user_id, 47::INTEGER AS access
            WHERE p_user_id IS NOT NULL

            UNION ALL

            SELECT uu.target_user_id, 36::INTEGER
            FROM claimius.user_users uu
            WHERE uu.viewer_id = p_user_id

            UNION ALL

            SELECT g.target_user_id, g.access
            FROM claimius._user_claim_grants(p_user_id, NULL) g
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
        WHERE u.sa_deleted_at IS NULL
          AND u.status        = 'active';
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_users(UUID, INTEGER) IS
    'Users the calling user can see in this app whose resolved access covers p_required_access. Resolution merges self (0x2F), member co-visibility via user_users (0x24), and direct claim_object on claimius.samna_user.';

GRANT EXECUTE ON FUNCTION claimius.get_access(UUID, UUID, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_direct_access(UUID, UUID, TEXT, INTEGER, BOOLEAN)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_objects(UUID, TEXT, INTEGER, UUID, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_user_access(UUID, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_users(UUID, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

-- ----------------------------------------------------------------------------
-- resolve_access
-- Resolves the strongest AccessClaim row the caller has on one object. Special
-- cases claimius.audit, claimius.user_relation, and claimius.samna_user; for
-- everything else falls back to get_access then get_direct_access. Returns no
-- rows when the caller has no path to the object or the id is malformed.
-- VOLATILE: get_access and get_direct_access settle through
-- reconcile_if_pending, which writes.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.resolve_access(UUID, UUID, TEXT, INTEGER);
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
                   NULL::UUID, TRUE
            FROM claimius.audit a
            WHERE a.id = p_object_id::UUID
              AND TRUE
              AND a.sa_owner_id IN (
                  SELECT g.object_id
                  FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required) g
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
            FROM claimius.get_user_access(p_user_id, p_object_id, p_required) u;
        RETURN;
    END IF;

    RETURN QUERY
        SELECT * FROM claimius.get_access(p_user_id, p_object_id::UUID, p_object_type, p_required);
    IF FOUND THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT g.user_object_id, g.user_claim_id, g.claim_id, g.claim_name, g.sa_access,
               g.scope, g.sa_owner_id, g.sa_root_id, g.sa_location_id, g.direct_grant
        FROM claimius.get_direct_access(p_user_id, p_object_id::UUID, p_object_type, p_required) g;
END;
$$ LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = claimius, pg_temp;

GRANT EXECUTE ON FUNCTION claimius.resolve_access(UUID, TEXT, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

-- ----------------------------------------------------------------------------
-- mutation_access
-- Single object access gate. Resolves access through resolve_access, returns
-- the strongest AccessClaim row, and RAISEs SQLSTATE P0403 when the object
-- exists but the caller lacks the required access, P0404 when the object is
-- absent or soft deleted, its id is malformed or its type is not registered.
-- ----------------------------------------------------------------------------

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
        SELECT * FROM claimius.resolve_access(p_user_id, p_object_id, p_object_type, p_required);
    IF FOUND THEN
        RETURN;
    END IF;

    IF p_object_id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        v_uuid := p_object_id::UUID;
    END IF;

    IF p_object_type = 'claimius.samna_user' THEN
        IF EXISTS (
            SELECT 1 FROM claimius.samna_user su
            WHERE TRUE
              AND su.sa_deleted_at IS NULL
              AND (su.external_id = p_object_id OR su.user_id = v_uuid OR su.id = v_uuid)
        ) THEN
            RAISE EXCEPTION 'no access' USING ERRCODE = 'P0403';
        ELSE
            RAISE EXCEPTION 'not found' USING ERRCODE = 'P0404';
        END IF;
    END IF;

    RETURN QUERY
        SELECT * FROM claimius.get_direct_access(p_user_id, v_uuid, p_object_type, p_required, TRUE);
END;
$$ LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.mutation_access(UUID, TEXT, TEXT, INTEGER) IS 'Single object access gate. Resolves access through get_access then get_direct_access, returns the strongest AccessClaim row, and RAISEs SQLSTATE P0403 when the object exists but the caller lacks the required access, P0404 when the object is absent or soft deleted, the id is malformed or the type is not registered. SQL embodiment of the Go MutationAccess resolution in one round trip. VOLATILE: it settles through reconcile_if_pending.';

GRANT EXECUTE ON FUNCTION claimius.mutation_access(UUID, TEXT, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

DROP FUNCTION IF EXISTS claimius.require_access(UUID, UUID, TEXT, INTEGER);

-- ----------------------------------------------------------------------------
-- get_claims (overload: by user)
-- Returns every claim the user holds whose access mask covers
-- p_required_access.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_claims(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_claims(
    p_user_id          UUID,
    p_required_access  INTEGER DEFAULT 0
) RETURNS SETOF claimius.claim AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
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
                    AND TRUE
                    AND uc.sa_deleted_at IS NULL
                    AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                    AND (uc.ends_at IS NULL OR uc.ends_at > now())
                    AND (c.sa_access & p_required_access) = p_required_access
              )
              OR EXISTS (
                  SELECT 1 FROM claimius.user_object uo
                  WHERE TRUE
                    AND uo.user_id = p_user_id
                    AND uo.object_type = 'claimius.claim'
                    AND uo.object_id = c.id
                    AND (uo.sa_access & p_required_access) = p_required_access
              )
          )
        ORDER BY claimius._popcount(c.sa_access) DESC;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_claims(UUID, INTEGER) IS 'All claims the user can read at the required level: either via a direct user_claim grant or via the materialized user_object index (cascade or direct creator grant).';

-- ----------------------------------------------------------------------------
-- get_claims (overload: against an object)
-- Returns every claim the user holds against one object.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_claims(
    p_user_id       UUID,
    p_object_id     UUID,
    p_object_type   TEXT
) RETURNS SETOF claimius.claim AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_user_id, p_object_type, p_object_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
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
                  AND TRUE
                  AND uc.sa_deleted_at IS NULL
                  AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                  AND (uc.ends_at IS NULL OR uc.ends_at > now())
              WHERE uo.user_id = p_user_id
                AND TRUE
                AND uo.object_id = p_object_id
                AND uo.object_type = p_object_type
          )
        ORDER BY claimius._popcount(c.sa_access) DESC;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_claims(UUID, UUID, TEXT) IS 'All claims the user holds against one object.';

-- ----------------------------------------------------------------------------
-- get_scope_access
-- The object-less bindings of the user's live grants whose scope
-- matches a jsonpath, one row per binding. A binding carries its claim mask
-- narrowed by its own, so a NULL binding mask is the claim mask. A deny
-- binding matching the same path subtracts its bits from every other, and
-- what remains is closed under implication. A row whose mask misses
-- p_required_access is dropped. It reads no user_object and settles nothing.
-- ----------------------------------------------------------------------------

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
                    object_inherits BOOLEAN
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
               co.inherits AS binding_inherits
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
          AND jsonb_path_exists(co.scope, p_path)
    ),
    denied AS (
        SELECT coalesce(bit_or(b.e & 15), 0) AS bits
        FROM bound b
        WHERE (b.e & 16) <> 0
    ),
    settled AS (
        SELECT b.*, claimius._close_access(b.e & 15 & ~x.bits) AS access
        FROM bound b
            CROSS JOIN denied x
        WHERE (b.e & 16) = 0
    )
    SELECT s.held_id, s.held_claim_id, s.held_claim_name, s.access, s.binding_owner_id, s.binding_root_id,
           s.binding_id, s.binding_scope, s.binding_access, s.binding_inherits
    FROM settled s
    WHERE s.access <> 0
      AND (s.access & p_required_access) = p_required_access
    ORDER BY s.held_claim_id, s.binding_id
$$;

COMMENT ON FUNCTION claimius.get_scope_access(UUID, JSONPATH, INTEGER) IS 'Object-less bindings of the user''s live grants whose scope matches a jsonpath, one row per binding, each mask the claim mask narrowed by the binding mask, less the bits of the deny bindings matching the same path, closed under implication.';


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
    p_scope_key        TEXT,
    p_required_access  INTEGER DEFAULT 0
) RETURNS TABLE (
                    user_claim_id   UUID,
                    claim_id        UUID,
                    sa_access       INTEGER,
                    scope           JSONB
                ) AS $$
BEGIN
    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT s.user_claim_id, s.claim_id, s.sa_access, s.object_scope
        FROM claimius.get_scope_access(p_user_id, ('$.' || to_jsonb(p_scope_key)::TEXT)::JSONPATH, p_required_access) s
        ORDER BY claimius._popcount(s.sa_access) DESC, s.claim_id
        LIMIT 1;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_access(UUID, TEXT, INTEGER) IS 'Strongest active object-less binding whose scope contains p_scope_key and whose mask covers p_required_access. A NULL binding mask inherits the claim mask, and a deny binding carrying the key subtracts its bits.';

-- ----------------------------------------------------------------------------
-- get_audit
-- Returns audit rows for organizations the user can see.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_audit(UUID, UUID, INTEGER, INTEGER, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_audit(
    p_user_id          UUID,
    p_required_access  INTEGER DEFAULT 0,
    p_limit            INTEGER DEFAULT 100,
    p_offset           INTEGER DEFAULT 0
) RETURNS SETOF claimius.audit AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT a.*
        FROM claimius.audit a
        WHERE TRUE
          AND a.sa_owner_id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required_access) g
        )
        ORDER BY a.sa_created_at DESC
        LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_audit(p_user_id uuid, p_required_access integer, p_limit integer, p_offset integer) IS 'Audit rows for organizations the user can see.';

-- ----------------------------------------------------------------------------
-- get_secrets
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.get_secrets(UUID, UUID, INTEGER);
DROP FUNCTION IF EXISTS claimius.get_secrets(UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_secrets(
    p_user_id          UUID,
    p_required_access  INTEGER DEFAULT 0,
    p_after            UUID DEFAULT NULL,
    p_limit            INTEGER DEFAULT NULL
) RETURNS SETOF claimius.samna_secret AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT s.*
        FROM claimius.get_objects(p_user_id, 'claimius.samna_secret', p_required_access, p_after, p_limit) g
            JOIN claimius.samna_secret s ON s.id = g.object_id
        WHERE s.sa_deleted_at IS NULL
        ORDER BY s.id;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_secrets(p_user_id uuid, p_required_access integer, p_after uuid, p_limit integer) IS 'Secrets whose effective mask covers p_required_access, one get_objects page in id order.';

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

COMMENT ON FUNCTION claimius._window_graph(JSONB, UUID, INTEGER, TEXT, UUID) IS 'The nodes of a graph kept when each parent shows at most p_width children of each type, 0 meaning all, in name order or by the reader''s decayed usage with p_sort ''usage'', continuing after p_next_id when given.';


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

DROP FUNCTION IF EXISTS claimius.get_owner_graph(UUID, UUID, INTEGER, INTEGER);
CREATE OR REPLACE FUNCTION claimius.get_owner_graph(
    p_user_id          UUID,
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

    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
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
        WHERE TRUE
          AND o.sa_deleted_at IS NULL
          AND o.sa_owner_id = o.id  -- root: self referential
          AND o.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required_access) g
        );
    ELSE
        SELECT ARRAY[o.id] INTO v_root_ids
        FROM claimius.organization o
        WHERE TRUE
          AND o.id = p_start_id
          AND o.sa_deleted_at IS NULL
          AND o.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required_access) g
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
            WHERE TRUE
              AND o.sa_deleted_at IS NULL
              AND o.id = ANY(v_root_ids)
            UNION ALL
            SELECT c.id, c.name::TEXT, c.description, c.type::TEXT, c.sa_level,
                   c.sa_owner_id, w.lvl + 1
            FROM claimius.organization c
                     JOIN org_walk w ON c.sa_owner_id = w.id
            WHERE TRUE
              AND c.sa_deleted_at IS NULL
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
                               AND TRUE
        WHERE w.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required_access) g
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
        WHERE TRUE
          AND o.sa_deleted_at IS NULL
          AND o.id = ANY(v_root_ids)
        UNION ALL
        SELECT c.id, w.lvl + 1
        FROM claimius.organization c
                 JOIN org_walk w ON c.sa_owner_id = w.id
        WHERE TRUE
          AND c.sa_deleted_at IS NULL
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
                               AND TRUE
        WHERE TRUE
          AND l.sa_deleted_at IS NULL
          AND l.sa_owner_id IN (SELECT org_id FROM _owner_graph_orgs)
          AND l.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.location', p_required_access) g
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
             AND TRUE
             AND (uo.sa_access & $2) = $2
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

            EXECUTE 'INSERT INTO _owner_graph_rows (object_id, object_type, label, parent_id, data) ' || v_sql USING p_user_id, p_required_access;
        END LOOP;

    IF p_width = 0 AND p_next_id IS NULL THEN
        RETURN QUERY SELECT r.object_id, r.object_type, r.label, r.parent_id, r.data FROM _owner_graph_rows r;
        RETURN;
    END IF;

    SELECT jsonb_agg(to_jsonb(r)) INTO v_rows FROM _owner_graph_rows r;
    RETURN QUERY SELECT * FROM claimius._window_graph(v_rows, p_user_id, p_width, p_sort, p_next_id);
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_owner_graph(UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) IS 'Ownership graph (orgs, locations, registered tables with sa_owner_id or sa_location_id) for the user. Optional start org and depth limit, and a window of p_width children of each type per parent in name or usage order continuing after p_next_id.';

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

DROP FUNCTION IF EXISTS claimius.get_claim_graph(UUID, UUID, INTEGER, INTEGER);
CREATE OR REPLACE FUNCTION claimius.get_claim_graph(
    p_user_id          UUID,
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

    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    v_cacheable := (p_start_id IS NULL AND p_depth = 0 AND p_required_access = 0);

    IF v_cacheable THEN
        SELECT c.graph INTO v_graph
        FROM claimius.claim_graph_cache c
        WHERE c.user_id = p_user_id
          AND NOT EXISTS (
              SELECT 1
              FROM claimius.user_claim uc
              WHERE uc.user_id = p_user_id
                AND (NOT pg_visible_in_snapshot(uc.txid, c.snapshot)
                     OR EXISTS (
                         SELECT 1 FROM claimius.claim cl
                         WHERE cl.id = uc.claim_id AND NOT pg_visible_in_snapshot(cl.txid, c.snapshot))
                     OR EXISTS (
                         SELECT 1 FROM claimius.claim_object co
                         WHERE co.claim_id = uc.claim_id AND NOT pg_visible_in_snapshot(co.txid, c.snapshot))));
    END IF;

    IF v_graph IS NULL THEN
        v_snapshot := pg_current_snapshot();
        IF p_start_id IS NULL THEN
            SELECT array_agg(uc.claim_id) INTO v_claim_ids
            FROM claimius.user_claim uc
                     JOIN claimius.claim c ON c.id = uc.claim_id
            WHERE uc.user_id = p_user_id
              AND TRUE
              AND uc.sa_deleted_at IS NULL
              AND c.sa_deleted_at IS NULL
              AND (p_required_access = 0 OR (c.sa_access & p_required_access) = p_required_access);
        ELSE
            SELECT array_agg(c.id) INTO v_claim_ids
            FROM claimius.claim c
            WHERE c.id = p_start_id
              AND TRUE
              AND c.sa_deleted_at IS NULL
              AND (p_required_access = 0 OR (c.sa_access & p_required_access) = p_required_access);
        END IF;

        IF v_claim_ids IS NULL OR array_length(v_claim_ids, 1) IS NULL THEN
            RETURN;
        END IF;

        SELECT u.id INTO v_user_row_id
        FROM claimius.samna_user u
        WHERE u.user_id = p_user_id AND TRUE
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
              AND TRUE
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
              AND TRUE
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
                              AND TRUE
                              AND uu.target_user_id = u.user_id
                     LEFT JOIN claimius.user_object uo
                          ON uo.object_id = u.id
                              AND uo.object_type = 'claimius.samna_user'
                              AND uo.user_id = p_user_id
                              AND TRUE
            WHERE TRUE
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
              AND TRUE
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
            WHERE TRUE
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
                     LEFT JOIN claimius.object_info oi
                          ON oi.object_type = uo.object_type
                              AND oi.object_id = uo.object_id
                              AND TRUE
            WHERE TRUE
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
                          ON TRUE
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
                     LEFT JOIN claimius.object_info oi
                          ON oi.object_type = uo.object_type
                              AND oi.object_id = uo.object_id
                              AND TRUE
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
            INSERT INTO claimius.claim_graph_cache (user_id, graph, snapshot)
            VALUES (p_user_id, v_graph, v_snapshot)
            ON CONFLICT (user_id) DO UPDATE
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

    RETURN QUERY SELECT * FROM claimius._window_graph(v_graph, p_user_id, p_width, p_sort, p_next_id);
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_claim_graph(UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) IS 'Access graph: user, other users seen, claims, user_claims, claim_object bindings, reachable objects. Optional start claim and depth limit, and a window of p_width children of each type per parent in name or usage order continuing after p_next_id. Default shape served from claimius.claim_graph_cache while every grant, claim and binding of the user was written by a transaction visible in the snapshot taken before the cached graph was built.';

CREATE OR REPLACE FUNCTION claimius._invalidate_claim_graph_cache()
    RETURNS TRIGGER AS $$
DECLARE
    v_user_id   UUID;
BEGIN
    IF TG_TABLE_NAME = 'user_object' THEN
        v_user_id := COALESCE(NEW.user_id, OLD.user_id);
        DELETE FROM claimius.claim_graph_cache
        WHERE TRUE AND user_id = v_user_id;
    ELSIF TG_TABLE_NAME = 'user_users' THEN
        v_user_id := COALESCE(NEW.viewer_id, OLD.viewer_id);
        DELETE FROM claimius.claim_graph_cache
        WHERE TRUE AND user_id = v_user_id;
    ELSIF TG_TABLE_NAME = 'samna_user' THEN
        v_user_id := COALESCE(NEW.user_id, OLD.user_id);
        DELETE FROM claimius.claim_graph_cache
        WHERE TRUE AND user_id = v_user_id;
    ELSIF TG_TABLE_NAME = 'object_info' THEN
        DELETE FROM claimius.claim_graph_cache c
        USING claimius.user_object uo
        WHERE uo.object_type = NEW.object_type
          AND uo.object_id = NEW.object_id
          AND c.user_id = uo.user_id;
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

DROP FUNCTION IF EXISTS claimius.search(UUID, TEXT, TEXT[], INTEGER, UUID[], UUID[], UUID[], UUID, TEXT, UUID[], INTEGER, INTEGER);
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
    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
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
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_user_id, p_required_access) best ON TRUE
                 JOIN claimius.object_info oi
                      ON oi.object_type = claimius.user_object.object_type
                     AND oi.object_id = claimius.user_object.object_id
        WHERE claimius.user_object.user_id = p_user_id
          AND (claimius.user_object.sa_access & p_required_access) = p_required_access
          AND oi.search_vector @@ v_tsquery
          AND (p_object_types IS NULL OR claimius.user_object.object_type = ANY(p_object_types))
          AND (best.user_claim_id IS NOT NULL OR claimius.user_object.direct_grant)
        ORDER BY 12 DESC, 1
        LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.search(UUID, TEXT, TEXT[], INTEGER, INTEGER, INTEGER) IS 'Full text search over accessible objects whose mask covers p_required_access. user_claim_id is the actor token from the strongest surviving claim grant per object.';

CREATE OR REPLACE FUNCTION claimius.search(
    p_user_id            UUID,
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
    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
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
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_user_id, p_required_access) best ON TRUE
                 JOIN claimius.object_info oi
                      ON oi.object_type = claimius.user_object.object_type
                     AND oi.object_id = claimius.user_object.object_id
        WHERE v_include_others
          AND claimius.user_object.user_id = p_user_id
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
            AND TRUE
            AND uc.sa_deleted_at IS NULL
            AND (uc.starts_at IS NULL OR uc.starts_at <= now())
            AND (uc.ends_at IS NULL OR uc.ends_at > now())
        WHERE v_include_claim
          AND NOT v_has_scope
          AND TRUE
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

COMMENT ON FUNCTION claimius.search(UUID, TEXT, TEXT[], INTEGER, UUID[], UUID[], UUID[], UUID, TEXT, INTEGER, INTEGER) IS 'Full text search over accessible objects. Includes claimius.claim rows the user holds, skipped when any subtree or parent filter is set. p_owner_subtree_ids scopes via ownership tree. p_parent_id and p_parent_type scope via location or parenthood tree. total_count carries the un-capped match count for pagination. Rows tie-break on object_id so offset pagination tiles without overlap or skip.';

GRANT EXECUTE ON FUNCTION claimius.search(UUID, TEXT, TEXT[], INTEGER, INTEGER, INTEGER) TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.search(UUID, TEXT, TEXT[], INTEGER, UUID[], UUID[], UUID[], UUID, TEXT, INTEGER, INTEGER) TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

-- ----------------------------------------------------------------------------
-- touch_object_usage
-- Adds counted checks to the pairs they were made on, one call per flush of
-- the middleware's in process counter. Row i of the arrays is one pair and the
-- checks counted on it; repeats of a pair add up. A pair that is not settled,
-- that has no user_object row, is skipped, so usage never makes a pair and
-- never outlives one. The touched pairs are locked in key order first, so two
-- replicas flushing the same pairs wait on each other instead of deadlocking.
-- Every user counted moves its user_activity row to now. Nothing here
-- publishes anywhere: usage is local to this database. Returns the pairs
-- touched.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius.touch_object_usage(
    p_user_ids      UUID[],
    p_object_types  TEXT[],
    p_object_ids    UUID[],
    p_counts        BIGINT[]
) RETURNS INTEGER AS $$
DECLARE
    v_touched   INTEGER;
BEGIN
    WITH used AS MATERIALIZED (
        SELECT u.user_id, u.object_type, u.object_id, sum(u.n) AS n
        FROM unnest(p_user_ids, p_object_types, p_object_ids, p_counts) AS u(user_id, object_type, object_id, n)
        WHERE u.user_id IS NOT NULL
          AND u.object_type IS NOT NULL
          AND u.object_id IS NOT NULL
          AND u.n > 0
        GROUP BY u.user_id, u.object_type, u.object_id
    ),
    locked AS (
        SELECT uo.user_id, uo.object_type, uo.object_id, used.n
        FROM claimius.user_object uo
            JOIN used
                ON used.user_id = uo.user_id
               AND used.object_type = uo.object_type
               AND used.object_id = uo.object_id
        ORDER BY uo.user_id, uo.object_type, uo.object_id
        FOR NO KEY UPDATE OF uo
    ),
    touched AS (
        UPDATE claimius.user_object uo
        SET used_count = uo.used_count + l.n,
            last_used_at = now()
        FROM locked l
        WHERE uo.user_id = l.user_id
          AND uo.object_type = l.object_type
          AND uo.object_id = l.object_id
        RETURNING uo.user_id
    ),
    active AS (
        INSERT INTO claimius.user_activity (user_id, last_used_at)
        SELECT DISTINCT used.user_id, now()
        FROM used
        ORDER BY used.user_id
        ON CONFLICT (user_id) DO UPDATE
            SET last_used_at = EXCLUDED.last_used_at
        RETURNING user_id
    )
    SELECT count(*) INTO v_touched FROM touched;

    RETURN v_touched;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.touch_object_usage(UUID[], TEXT[], UUID[], BIGINT[]) IS 'Adds counted checks to user_object.used_count and moves last_used_at, per (user, type, object) row of the arrays, skipping pairs not settled, and moves each counted user''s user_activity. Local, never synced. Returns the pairs touched.';

GRANT EXECUTE ON FUNCTION claimius.touch_object_usage(UUID[], TEXT[], UUID[], BIGINT[]) TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

-- ============================================================================
-- Write functions: claim management
-- ============================================================================

-- create_claim
DROP FUNCTION IF EXISTS claimius.create_claim(UUID, TEXT, TEXT, INTEGER, UUID, UUID, UUID, BOOLEAN, BOOLEAN, claimius.claim_type);
DROP FUNCTION IF EXISTS claimius.create_claim(UUID, TEXT, TEXT, INTEGER, UUID, UUID, UUID, BOOLEAN, claimius.claim_type);

CREATE OR REPLACE FUNCTION claimius.create_claim(
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
        name, description, sa_access, inherits, type,
        sa_owner_id, sa_root_id, sa_created_by
    ) VALUES (
                 p_name, p_description, v_access, p_inherits, p_type,
                 p_sa_owner_id, p_sa_root_id, p_sa_created_by
             ) RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.create_claim(p_name text, p_description text, p_sa_access integer, p_sa_owner_id uuid, p_sa_root_id uuid, p_sa_created_by uuid, p_inherits boolean, p_type claimius.claim_type) IS 'Creates a new claim. p_sa_access is a bitwise mask: 0x01 owner, 0x02 write, 0x04 read, 0x08 execute, 0x10 deny, 0x20 member. On grant claims with the deny bit unset owner implies write+read+execute, write implies read+execute, execute implies read. Member bit 0x20 stands alone. Deny claims store the supplied bits verbatim so callers can deny any subset of read, write, execute, owner. A mask with no bit besides deny is refused with SQLSTATE 22023.';

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
                              inherits    = coalesce(p_inherits, inherits)
    WHERE id = p_claim_id
    RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_claim(p_claim_id uuid, p_name text, p_description text, p_sa_access integer, p_inherits boolean) IS 'Patches selected fields on a claim. p_sa_access is expanded on grant claims so owner implies write+read+execute, write implies read+execute, execute implies read. Member bit 0x20 stands alone. Deny claims keep the supplied bits verbatim. Empty p_name and zero p_sa_access are treated as preserve so PATCH callers that send only one field do not wipe the others. A mask with no bit besides deny is refused with SQLSTATE 22023.';

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
        claim_id, user_id, reason, starts_at, ends_at,
        sa_owner_id, sa_created_by
    ) VALUES (
                 p_claim_id, p_user_id, p_reason, p_starts_at, p_ends_at,
                 p_sa_owner_id, p_sa_created_by
             ) RETURNING * INTO v_row;
    RETURN jsonb_build_object('user_claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.assign_claim_user(p_claim_id uuid, p_user_id uuid, p_sa_owner_id uuid, p_sa_created_by uuid, p_reason text, p_starts_at timestamp with time zone, p_ends_at timestamp with time zone) IS 'Grants a claim to a user in an app.';

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
        claim_id, object_id, object_type, reason, scope,
        sa_access, inherits, ref_id, sa_owner_id, sa_root_id, sa_created_by
    ) VALUES (
                 p_claim_id, p_object_id, p_object_type, p_reason, p_scope,
                 p_sa_access, p_inherits, p_ref_id, p_sa_owner_id, p_sa_root_id, p_sa_created_by
             ) RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim_object', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.assign_claim_object(p_claim_id uuid, p_sa_owner_id uuid, p_sa_root_id uuid, p_sa_created_by uuid, p_object_id uuid, p_object_type text, p_sa_access integer, p_inherits boolean, p_reason text, p_ref_id uuid, p_scope jsonb) IS 'Binds a claim. With (object_type, object_id) p_scope keys are field names; without them p_scope keys are custom. p_sa_access is a bitwise mask.';

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
-- samna_user row(s) and migrates user_claim, user_relation, user_field and
-- user_object to the target, and user_users follows the deleted pairs.
-- External tables registered through init_claimius_tables can opt in by
-- passing themselves in p_external_tables; the function will UPDATE their
-- sa_created_by references where applicable.
-- ============================================================================

CREATE OR REPLACE FUNCTION claimius.merge_user(
    p_target_user_id    UUID,
    p_source_user_id    UUID,
    p_acting_user_claim UUID DEFAULT NULL
) RETURNS VOID AS $$
DECLARE
    v_target_app_user RECORD;
    v_source_app_user RECORD;
BEGIN
    SELECT * INTO v_target_app_user FROM claimius.samna_user
    WHERE user_id = p_target_user_id AND sa_deleted_at IS NULL;
    SELECT * INTO v_source_app_user FROM claimius.samna_user
    WHERE user_id = p_source_user_id AND sa_deleted_at IS NULL;

    IF v_source_app_user.id IS NULL THEN
        RETURN;
    END IF;

    IF v_target_app_user.id IS NULL THEN
        -- Target has no row: rename the source row to target user_id
        UPDATE claimius.samna_user
        SET user_id = p_target_user_id, sa_updated_at = now()
        WHERE id = v_source_app_user.id;
    ELSE
        -- Target already has a row. Merge claims/relations/fields.
        UPDATE claimius.user_claim SET user_id = p_target_user_id
        WHERE user_id = p_source_user_id;
        UPDATE claimius.user_relation SET user_id = p_target_user_id
        WHERE user_id = p_source_user_id;
        UPDATE claimius.user_field SET user_id = p_target_user_id
        WHERE user_id = p_source_user_id;

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
        WHERE user_id = p_source_user_id;

        -- Hard delete the source samna_user row
        DELETE FROM claimius.samna_user WHERE id = v_source_app_user.id;
    END IF;

    PERFORM claimius.write_audit(
            'merge_user', 'merge', 'claimius.samna_user',
            p_target_user_id,
            p_target_user_id,
            coalesce(p_acting_user_claim, p_target_user_id),
            'Merged source ' || p_source_user_id::TEXT || ' into target ' || p_target_user_id::TEXT
            );
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

COMMENT ON FUNCTION claimius.init_claimius_tables(VARIADIC p_object_types text[]) IS 'Registers external tables. Idempotent. Called by implementer migrations.';

-- _bulk_seed_user_object
-- For a registered table with existing rows, writes the object_info row of
-- every row, populates user_object for every (user, object) pair where
-- access exists, then rebuilds user_users for
-- the users it holds pairs for, since no count moves under replay_mode.
-- Inherits the caller's replay_mode setting; callers run this with
-- replay_mode = true so emit_sync_event short circuits and the bulk seed
-- produces no sync events. Per-row cost is bounded because the helpers
-- _affected_users_for_object and _build_grants_for_objects both use indexed
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
    v_ids UUID[];
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    EXECUTE format('SELECT ARRAY(SELECT r.id FROM %I.%I r)', v_schema, v_table) INTO v_ids;
    PERFORM claimius._write_object_info(p_object_type, v_ids);

    v_sql := format('SELECT id FROM %I.%I WHERE sa_deleted_at IS NULL', v_schema, v_table);
    FOR v_obj IN EXECUTE v_sql LOOP
            FOR v_user IN
                SELECT user_id FROM claimius._affected_users_for_object(p_object_type, v_obj.id)
                LOOP
                    PERFORM claimius.recompute_user_object(v_user.user_id, p_object_type, v_obj.id);
                END LOOP;
            v_row_count := v_row_count + 1;
            IF v_row_count % 100 = 0 THEN
                RAISE NOTICE '  ... seeded % rows of %', v_row_count, p_object_type;
            END IF;
        END LOOP;

    PERFORM claimius._refresh_user_users(s.user_id)
    FROM (
        SELECT DISTINCT user_id
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
-- (name): if a non deleted org with the same name already exists in
-- the app, returns that row instead of creating a new one.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.create_root_organization(
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

    -- Idempotency: existing root org with same (app, name)?
    SELECT * INTO v_row
    FROM claimius.organization
    WHERE TRUE
      AND name = p_name
      AND sa_deleted_at IS NULL
      AND sa_owner_id = id
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

    RETURN jsonb_build_object('organization', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.create_root_organization(TEXT, UUID, TEXT, TEXT, UUID, TEXT) IS 'Creates a new root organization with self referencing sa_owner_id and sa_root_id. Idempotent on (name).';

-- ----------------------------------------------------------------------------
-- init
-- Bootstrap a fresh claimius deployment as a disciple. Ensures the singleton
-- disciple_state cursor row exists and grants the disciple roles. The disciple
-- identifies its app from env only, so no app slug is stored or required. All
-- data arrives via sync from the prophet.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.init(
    p_disciple_app_slug TEXT DEFAULT NULL
)
    RETURNS JSONB AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM claimius.disciple_state) THEN
        INSERT INTO claimius.disciple_state (last_applied_seq)
        VALUES (0);
    END IF;

    PERFORM claimius._grant_disciple_roles();

    RAISE NOTICE 'Disciple initialized. Sync layer must populate all tables from prophet.';

    RETURN '{}'::JSONB;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.init(p_disciple_app_slug text) IS 'Bootstrap as a disciple. Ensures the sync cursor row and grants disciple roles. App identity comes from env. State arrives from prophet via sync.';

-- ----------------------------------------------------------------------------
-- Role grant helpers
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius._grant_disciple_roles()
    RETURNS VOID AS $$
BEGIN
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_reader';
    EXECUTE 'REVOKE EXECUTE ON FUNCTION claimius.create_claim, claimius.update_claim, claimius.remove_claim,
             claimius.assign_claim_user, claimius.update_claim_user, claimius.remove_user_claim,
             claimius.assign_claim_object, claimius.update_claim_object, claimius.remove_claim_object,
             claimius.merge_user, claimius.migrate_root,
             claimius.init,
             claimius.init_claimius_tables, claimius.init_claimius_internal,
             claimius.recompute_state
             FROM claimius_reader';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.user_object, claimius.object_info, claimius.user_users,
             claimius.disciple_state, claimius.samna_user, claimius.samna_client,
             claimius.samna_secret, claimius.organization, claimius.location,
             claimius.claim, claimius.user_claim, claimius.claim_object,
             claimius.object_field, claimius.user_field, claimius.user_relation,
             claimius.audit, claimius.inheritance_info, claimius.table_info,
             claimius.notification, claimius.claim_graph_cache
             TO claimius_disciple_client';
    EXECUTE 'REVOKE INSERT, UPDATE, DELETE ON claimius.claim_graph_cache FROM claimius_reader';
    EXECUTE 'GRANT SELECT ON ALL TABLES IN SCHEMA claimius TO claimius_disciple_client';
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_disciple_client';
END;
$$ LANGUAGE plpgsql;

REVOKE INSERT, UPDATE, DELETE ON claimius.claim_graph_cache FROM claimius_reader;

-- ----------------------------------------------------------------------------
-- ensure_app_user
-- When a user logs in for the first time, upserts the samna_user row. On the
-- disciple there is no app default claim: claim grants arrive via sync, so no
-- user_claim is created here. Idempotent: re-running returns the existing row.
-- ----------------------------------------------------------------------------

DROP FUNCTION IF EXISTS claimius.ensure_app_user(UUID, TEXT, TEXT, TEXT, TEXT, TEXT, TEXT);
CREATE OR REPLACE FUNCTION claimius.ensure_app_user(
    p_user_id    UUID,
    p_first_name TEXT DEFAULT NULL,
    p_last_name  TEXT DEFAULT NULL,
    p_user_name  TEXT DEFAULT NULL,
    p_email      TEXT DEFAULT NULL,
    p_phone      TEXT DEFAULT NULL,
    p_external_id TEXT DEFAULT NULL,
    p_user_image TEXT DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_user_row    claimius.samna_user;
BEGIN
    p_user_id := claimius.ensure_uuid_null(p_user_id);

    SELECT * INTO v_user_row FROM claimius.samna_user
    WHERE user_id = p_user_id AND sa_deleted_at IS NULL;

    IF NOT FOUND THEN
        INSERT INTO claimius.samna_user (
            user_id, first_name, last_name, user_name, user_image,
            email, phone, external_id, status, type
        )
        VALUES (
                   COALESCE(p_user_id, gen_random_uuid()),
                   p_first_name, p_last_name, p_user_name, p_user_image,
                   p_email, p_phone, p_external_id, 'active', 'user'
               )
        RETURNING * INTO v_user_row;
    END IF;

    RETURN jsonb_build_object('samna_user', to_jsonb(v_user_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.ensure_app_user(p_user_id uuid, p_first_name text, p_last_name text, p_user_name text, p_email text, p_phone text, p_external_id text, p_user_image text) IS 'Upserts the samna_user row. Idempotent. Claim grants arrive via sync on the disciple.';

DROP FUNCTION IF EXISTS claimius.get_access(UUID, TEXT, INTEGER);

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

DROP FUNCTION IF EXISTS claimius.get_graph_links_owner(UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID);
DROP FUNCTION IF EXISTS claimius.get_graph_objects(UUID, INTEGER, TEXT[], TEXT[], UUID[], BOOLEAN, BOOLEAN);
DROP FUNCTION IF EXISTS claimius.touch_object_usage(UUID, TEXT, UUID);
DROP FUNCTION IF EXISTS claimius.graph_object_types();
DROP FUNCTION IF EXISTS claimius._prune_graph_rows(JSONB, UUID, UUID, INTEGER, TEXT, UUID);
CREATE OR REPLACE FUNCTION claimius.get_graph_links_owner(
    p_user_id         UUID,
    p_required_access INTEGER DEFAULT 4,
    p_include_types   TEXT[] DEFAULT NULL,
    p_exclude_types   TEXT[] DEFAULT NULL,
    p_start_id        UUID DEFAULT NULL,
    p_depth           INTEGER DEFAULT 0
) RETURNS JSONB AS $$
DECLARE
    v_links   JSONB;
    v_allowed TEXT[] := (SELECT coalesce(array_agg(object_type ORDER BY object_type), ARRAY[]::TEXT[]) FROM claimius.table_info);
BEGIN
    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN jsonb_build_object('links', '[]'::jsonb);
    END IF;

    WITH ancestor_pool AS (
        SELECT uo.object_id, uo.object_type
        FROM claimius.user_object uo
        WHERE uo.user_id = p_user_id
          AND TRUE
          AND (p_required_access = 0 OR (uo.sa_access & p_required_access) = p_required_access)
          AND uo.object_type = ANY(v_allowed)
    ),
    descendants AS (
        SELECT uo.object_id, uo.object_type
        FROM claimius.user_object uo
        WHERE uo.user_id = p_user_id
          AND TRUE
          AND (p_required_access = 0 OR (uo.sa_access & p_required_access) = p_required_access)
          AND uo.object_type = ANY(v_allowed)
          AND claimius.graph_type_passes(uo.object_type, p_include_types, p_exclude_types)

        UNION
        SELECT c.id AS object_id, 'claimius.claim'::TEXT AS object_type
        FROM claimius.claim c
        WHERE TRUE
          AND c.sa_deleted_at IS NULL
          AND claimius.graph_type_passes('claimius.claim', p_include_types, p_exclude_types)
          AND EXISTS (
              SELECT 1 FROM claimius.user_claim uc
              WHERE uc.user_id = p_user_id
                AND TRUE
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
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

GRANT EXECUTE ON FUNCTION claimius.get_graph_links_owner(UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER) TO claimius_reader, claimius_writer, claimius_admin;

DROP FUNCTION IF EXISTS claimius.get_graph_links_claim(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER);

DROP FUNCTION IF EXISTS claimius.get_graph_links_claim(UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID);
CREATE OR REPLACE FUNCTION claimius.get_graph_links_claim(
    p_user_id         UUID,
    p_required_access INTEGER DEFAULT 0,
    p_include_types   TEXT[] DEFAULT NULL,
    p_exclude_types   TEXT[] DEFAULT NULL,
    p_start_id        UUID DEFAULT NULL,
    p_depth           INTEGER DEFAULT 0
) RETURNS JSONB AS $$
DECLARE
    v_user_row_id UUID;
    v_claim_ids   UUID[];
    v_links       JSONB;
BEGIN
    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN jsonb_build_object('links', '[]'::jsonb);
    END IF;

    SELECT u.id INTO v_user_row_id
    FROM claimius.samna_user u
    WHERE u.user_id = p_user_id AND TRUE AND u.sa_deleted_at IS NULL
    LIMIT 1;

    IF p_start_id IS NULL THEN
        SELECT array_agg(uc.claim_id) INTO v_claim_ids
        FROM claimius.user_claim uc
        JOIN claimius.claim c ON c.id = uc.claim_id
        WHERE uc.user_id = p_user_id
          AND TRUE
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
          AND (p_required_access = 0 OR (c.sa_access & p_required_access) = p_required_access);
    ELSE
        SELECT array_agg(c.id) INTO v_claim_ids
        FROM claimius.claim c
        WHERE c.id = p_start_id
          AND TRUE
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
        WHERE TRUE
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
          AND TRUE
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
        WHERE TRUE
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
        JOIN claimius.user_users uu ON uu.viewer_id = p_user_id AND TRUE AND uu.target_user_id = u.user_id
        WHERE v_user_row_id IS NOT NULL
          AND p_start_id IS NULL
          AND TRUE
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
        JOIN claimius.samna_user su ON su.user_id = ur.user_id AND TRUE AND su.sa_deleted_at IS NULL
        JOIN claimius.user_object uo
               ON uo.object_id = ur.object_id AND uo.object_type = ur.object_type
              AND uo.user_id = p_user_id AND TRUE
        WHERE TRUE
          AND p_start_id IS NULL
          AND ur.sa_deleted_at IS NULL
          AND ur.user_id = p_user_id
          AND claimius.graph_type_passes(ur.object_type, p_include_types, p_exclude_types)
    )
    SELECT COALESCE(jsonb_agg(link), '[]'::jsonb) INTO v_links FROM spine;

    RETURN jsonb_build_object('links', v_links);
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

GRANT EXECUTE ON FUNCTION claimius.get_graph_links_claim(UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER) TO claimius_reader, claimius_writer, claimius_admin;

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
               AND TRUE
               AND sa_deleted_at IS NULL
        ) THEN
            RAISE EXCEPTION 'user_id already exists' USING ERRCODE = '23505';
        END IF;

        IF v_external_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM claimius.samna_user
             WHERE external_id = v_external_id
               AND TRUE
               AND sa_deleted_at IS NULL
        ) THEN
            RAISE EXCEPTION 'external_id already exists' USING ERRCODE = '23505';
        END IF;

        INSERT INTO claimius.samna_user (
            user_id, first_name, last_name, user_name, user_image,
            email, phone, external_id, status, type
        ) VALUES (
            COALESCE(v_user_id, gen_random_uuid()),
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
            user_id, first_name, last_name, user_name, user_image,
            email, phone, external_id, status, type
        ) VALUES (
            COALESCE(v_user_id, gen_random_uuid()),
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
        ON CONFLICT (user_id) WHERE sa_deleted_at IS NULL DO UPDATE SET
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
     WHERE TRUE
       AND claim_id = v_claim_id
       AND user_id = v_user_row.user_id
       AND sa_deleted_at IS NULL
     LIMIT 1;

    IF v_existing_uc.id IS NOT NULL THEN
        v_claim_user := jsonb_build_object('user_claim', to_jsonb(v_existing_uc));
    ELSE
        v_claim_user := claimius.assign_claim_user(
            
            p_claim_id      => v_claim_id,
            p_user_id       => v_user_row.user_id,
            p_sa_owner_id   => p_owner_id,
            p_sa_created_by => p_caller_uc
        );
    END IF;

    SELECT * INTO v_existing_virtual
      FROM claimius.claim
     WHERE TRUE
       AND name = 'user:' || v_user_row.user_id::text
       AND type = 'virtual'
       AND sa_deleted_at IS NULL
     LIMIT 1;

    IF v_existing_virtual.id IS NOT NULL THEN
        v_virtual_claim    := jsonb_build_object('claim', to_jsonb(v_existing_virtual));
        v_virtual_claim_id := v_existing_virtual.id;
    ELSE
        v_virtual_claim := claimius.create_claim(
            
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
     WHERE TRUE
       AND claim_id = v_virtual_claim_id
       AND object_id = v_user_row.user_id
       AND object_type = 'claimius.samna_user'
       AND sa_deleted_at IS NULL
     LIMIT 1;

    IF v_existing_obj.id IS NOT NULL THEN
        v_claim_obj := jsonb_build_object('claim_object', to_jsonb(v_existing_obj));
    ELSE
        v_claim_obj := claimius.assign_claim_object(
            
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
         WHERE TRUE
           AND claim_id = v_virtual_claim_id
           AND user_id = v_caller_user_id
           AND sa_deleted_at IS NULL
         LIMIT 1;

        IF v_existing_caller.id IS NOT NULL THEN
            v_caller_user_claim := jsonb_build_object('user_claim', to_jsonb(v_existing_caller));
        ELSE
            v_caller_user_claim := claimius.assign_claim_user(
                
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
    'Disciple side: insert or upsert a samna_user, ensure the supplied claim is granted to the user, ensure a per-user virtual claim with full access is bound to the user, and when p_caller_uc is not null ensure that virtual claim is granted to the caller. p_caller_uc may be null for system-bootstrap callers that have no authenticating user_claim yet; in that case sa_created_by is stamped null and the caller_user_claim grant is skipped. With p_upsert=FALSE default, raises 23505 when user_id or external_id already exists. With p_upsert=TRUE, conflicts on user_id update the row and downstream grants are reused when present. Returns samna_user, user_claim, virtual_claim, claim_object, caller_user_claim.';

GRANT EXECUTE ON FUNCTION claimius.create_organization_user(UUID, UUID, UUID, JSONB, BOOLEAN)
    TO claimius_disciple_client, claimius_writer, claimius_admin;

-- ----------------------------------------------------------------------------
-- Notification writers
-- ----------------------------------------------------------------------------
-- p_app_id is accepted for signature parity with the prophet but ignored here:
-- the disciple holds a single app and its claimius tables carry no app_id.

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
            (user_id, title, description, style, type, link,
             object_id, object_type, sa_created_by, sa_owner_id, repeats_at)
        SELECT uo.user_id, p_title, p_description,
               COALESCE(p_style, '{}'::jsonb), p_type, p_link,
               p_object_id, p_object_type, p_created_by, p_owner_id, p_repeats_at
        FROM claimius.user_object uo
        WHERE uo.object_type = p_object_type
          AND uo.object_id = p_object_id
          AND (uo.sa_access & 4) <> 0
          AND (p_created_by IS NULL OR uo.user_id <> p_created_by)
        RETURNING 1
    )
    SELECT count(*)::INTEGER FROM inserted;
$$ LANGUAGE sql;

COMMENT ON FUNCTION claimius.notify_object(UUID, TEXT, TEXT, JSONB, TEXT, TEXT, TEXT, UUID, UUID, UUID, INTERVAL) IS 'Fans a notification out to every user holding read access on the object through user_object, excluding the triggering user. Returns the number of rows inserted.';

GRANT EXECUTE ON FUNCTION claimius.notify_object(UUID, TEXT, TEXT, JSONB, TEXT, TEXT, TEXT, UUID, UUID, UUID, INTERVAL)
    TO claimius_disciple_client, claimius_writer, claimius_admin;

CREATE OR REPLACE FUNCTION claimius.notify_user(
    p_app_id UUID, p_title TEXT, p_description TEXT, p_style JSONB, p_type TEXT,
    p_link TEXT, p_object_type TEXT, p_object_id UUID, p_created_by UUID,
    p_owner_id UUID, p_repeats_at INTERVAL, p_user_ids UUID[]
) RETURNS INTEGER AS $$
    WITH inserted AS (
        INSERT INTO claimius.notification
            (user_id, title, description, style, type, link,
             object_id, object_type, sa_created_by, sa_owner_id, repeats_at)
        SELECT su.user_id, p_title, p_description,
               COALESCE(p_style, '{}'::jsonb), p_type, p_link,
               p_object_id, p_object_type, p_created_by, p_owner_id, p_repeats_at
        FROM claimius.samna_user su
        WHERE su.user_id = ANY(p_user_ids)
          AND su.status = 'active'
          AND su.sa_deleted_at IS NULL
        RETURNING 1
    )
    SELECT count(*)::INTEGER FROM inserted;
$$ LANGUAGE sql;

COMMENT ON FUNCTION claimius.notify_user(UUID, TEXT, TEXT, JSONB, TEXT, TEXT, TEXT, UUID, UUID, UUID, INTERVAL, UUID[]) IS 'Writes a notification straight to each named active user. Returns the number of rows inserted.';

GRANT EXECUTE ON FUNCTION claimius.notify_user(UUID, TEXT, TEXT, JSONB, TEXT, TEXT, TEXT, UUID, UUID, UUID, INTERVAL, UUID[])
    TO claimius_disciple_client, claimius_writer, claimius_admin;

