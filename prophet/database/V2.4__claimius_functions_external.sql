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
DECLARE
    v_pair RECORD;
    v_cap  INTEGER := ~0;
BEGIN
    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    IF EXISTS (SELECT 1 FROM claimius.samna_user u WHERE u.user_id = p_user_id AND u.app_id = p_app_id AND u.type = 'guest' AND u.sa_deleted_at IS NULL) THEN
        IF (p_required_access & ~4) <> 0 THEN
            RETURN;
        END IF;
        v_cap := 4;
    END IF;

    SELECT s.* INTO v_pair FROM claimius._settle_pair(p_app_id, p_user_id, p_object_type, p_object_id) s;

    IF v_pair.grants IS NOT NULL AND (v_pair.sa_access & p_required_access) = p_required_access THEN
        RETURN QUERY
            SELECT
                claimius.composite_id(p_app_id::TEXT, p_user_id::TEXT, p_object_type, p_object_id::TEXT),
                best.user_claim_id,
                best.claim_id,
                best.claim_name,
                best.access_bits & v_cap,
                v_pair.scope,
                v_pair.sa_owner_id,
                v_pair.sa_owner_root_id,
                v_pair.sa_location_id,
                FALSE,
                best.grant_level
            FROM claimius._best_grant(v_pair.grants, p_required_access) best;
        IF FOUND THEN
            RETURN;
        END IF;
    END IF;

    IF v_pair.direct_grant AND (v_pair.sa_access & p_required_access) = p_required_access THEN
        RETURN QUERY
            SELECT
                claimius.composite_id(p_app_id::TEXT, p_user_id::TEXT, p_object_type, p_object_id::TEXT),
                NULL::UUID,
                NULL::UUID,
                'direct'::TEXT,
                v_pair.sa_access & v_cap,
                v_pair.scope,
                v_pair.sa_owner_id,
                v_pair.sa_owner_root_id,
                v_pair.sa_location_id,
                TRUE,
                0;
        RETURN;
    END IF;

    IF NOT v_pair.settled THEN
        RETURN QUERY
            SELECT d.user_object_id, d.user_claim_id, d.claim_id, d.claim_name, d.sa_access & v_cap, d.scope, d.sa_owner_id, d.sa_root_id, d.sa_location_id, d.direct_grant, d.level FROM claimius.get_direct_access(p_user_id, p_app_id, p_object_id, p_object_type, p_required_access, FALSE, FALSE) d;
    END IF;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_access(UUID, UUID, UUID, TEXT, INTEGER) IS 'Strongest active claim based grant for one (user, app, object) whose mask covers p_required_access: the user row, then the point settle and the read of the pair with its object_info row in one pass through _settle_pair, and the grant chosen from the pair, or, when no claim grant covers p_required_access, the creator''s direct_grant with no user_claim_id and claim_name direct. Only when the settle was skipped on a lock and the pair gives nothing does it walk the ancestors through get_direct_access. user_claim_id is the actor token for follow-on writes. sa_root_id is the owning organization root, suitable for FK to claimius.organization on follow-on claim and claim_object writes. A user of type guest is capped at READ: a requirement past READ answers nothing and the returned mask keeps the READ bit only.';

DROP FUNCTION IF EXISTS claimius.get_direct_access(UUID, UUID, UUID, TEXT);

DROP FUNCTION IF EXISTS claimius.get_direct_access(UUID, UUID, UUID, TEXT, INTEGER, BOOLEAN);
CREATE OR REPLACE FUNCTION claimius.get_direct_access(
    p_user_id       UUID,
    p_app_id        UUID,
    p_object_id     UUID,
    p_object_type   TEXT,
    p_required      INTEGER DEFAULT 0,
    p_raise         BOOLEAN DEFAULT FALSE,
    p_settle        BOOLEAN DEFAULT TRUE
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
    IF p_settle THEN
        PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id, p_object_type, p_object_id);
    END IF;

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
            SELECT g.grant,
                   (g.grant ->> 'access')::INTEGER AS access,
                   (g.grant ->> 'hop')::INTEGER AS hop,
                   (g.grant ->> 'user_claim_id')::UUID AS user_claim_id
            FROM (
                SELECT jsonb_array_elements(claimius._apply_denies(
                    (SELECT coalesce(jsonb_agg(jsonb_build_object(
                        'user_claim_id', d.user_claim_id, 'claim_id', d.claim_id, 'claim_name', d.name, 'hop', d.hop,
                        'sa_owner_id', d.sa_owner_id, 'sa_root_id', d.sa_root_id, 'level', d.lvl,
                        'access', d.e & ~16, 'scope', d.scope,
                        'tree_type', CASE WHEN d.hop = 0 THEN 'direct' ELSE 'tree' END)), '[]'::JSONB)
                     FROM derivations d WHERE (d.e & 16) = 0),
                    (SELECT coalesce(jsonb_agg(jsonb_build_object(
                        'level', d.lvl, 'access', d.e & 15, 'scope', d.scope,
                        'tree_type', CASE WHEN d.hop = 0 THEN 'direct' ELSE 'tree' END)), '[]'::JSONB)
                     FROM derivations d WHERE (d.e & 16) <> 0),
                    FALSE,
                    (SELECT f.names FROM fields f)
                ) -> 'grants') AS grant
            ) g
        )
        SELECT
            claimius.composite_id(p_app_id::TEXT, p_user_id::TEXT, p_object_type, p_object_id::TEXT),
            s.user_claim_id,
            (s.grant ->> 'claim_id')::UUID,
            s.grant ->> 'claim_name',
            s.access,
            s.grant -> 'scope',
            (s.grant ->> 'sa_owner_id')::UUID,
            (s.grant ->> 'sa_root_id')::UUID,
            NULL::UUID,
            FALSE,
            coalesce((s.grant ->> 'level')::INTEGER, 0)
        FROM settled s
        WHERE s.access <> 0
          AND (s.access & p_required) = p_required
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

COMMENT ON FUNCTION claimius.get_direct_access(UUID, UUID, UUID, TEXT, INTEGER, BOOLEAN, BOOLEAN) IS 'Locates the closest, strongest user_claim the user holds related to the given object via ancestor walk, directly or through composition, with the binding narrowing its claim and the user''s denies subtracted by class. The walk of the gate when the point settle was skipped on a lock. VOLATILE: with p_settle it settles through reconcile_if_pending.';

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
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
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
            oi.sa_owner_root_id,
            oi.sa_name,
            oi.sa_description,
            oi.sa_link,
            (claimius.user_object.direct_grant AND best.user_claim_id IS NULL)
        FROM claimius.user_object
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_required_access) best ON TRUE
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

DROP FUNCTION IF EXISTS claimius.get_organizations(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_organizations(
    p_user_id          UUID,
    p_app_id           UUID,
    p_required_access  INTEGER DEFAULT 0,
    p_after            UUID DEFAULT NULL,
    p_limit            INTEGER DEFAULT NULL
) RETURNS SETOF claimius.organization AS $$
DECLARE
    v_after UUID := p_after;
    v_left  INTEGER := p_limit;
    v_ids   UUID[];
    v_n     INTEGER;
BEGIN
    IF p_limit IS NULL THEN
        RETURN QUERY
            SELECT o.*
            FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.organization', p_required_access, p_after, NULL) g
                JOIN claimius.organization o ON o.id = g.object_id
            WHERE claimius.app_has_object(p_app_id, 'claimius.organization', o.id)
              AND o.sa_deleted_at IS NULL
            ORDER BY o.id;
        RETURN;
    END IF;

    LOOP
        SELECT array_agg(g.object_id ORDER BY g.object_id) INTO v_ids
        FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.organization', p_required_access, v_after, p_limit) g;
        EXIT WHEN v_ids IS NULL;

        RETURN QUERY
            SELECT o.*
            FROM claimius.organization o
            WHERE o.id = ANY(v_ids)
              AND claimius.app_has_object(p_app_id, 'claimius.organization', o.id)
              AND o.sa_deleted_at IS NULL
            ORDER BY o.id
            LIMIT v_left;
        GET DIAGNOSTICS v_n = ROW_COUNT;
        v_left := v_left - v_n;
        EXIT WHEN v_left <= 0 OR cardinality(v_ids) < p_limit;
        v_after := v_ids[cardinality(v_ids)];
    END LOOP;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_organizations(p_user_id uuid, p_app_id uuid, p_required_access integer, p_after uuid, p_limit integer) IS 'Organizations whose effective mask covers p_required_access, in id order after p_after, at most p_limit of them: the reader''s range from get_objects, which settles once, each row kept when live and linked to the app before the page is cut.';

DROP FUNCTION IF EXISTS claimius.get_locations(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_locations(
    p_user_id          UUID,
    p_app_id           UUID,
    p_required_access  INTEGER DEFAULT 0,
    p_after            UUID DEFAULT NULL,
    p_limit            INTEGER DEFAULT NULL
) RETURNS SETOF claimius.location AS $$
DECLARE
    v_after UUID := p_after;
    v_left  INTEGER := p_limit;
    v_ids   UUID[];
    v_n     INTEGER;
BEGIN
    IF p_limit IS NULL THEN
        RETURN QUERY
            SELECT l.*
            FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.location', p_required_access, p_after, NULL) g
                JOIN claimius.location l ON l.id = g.object_id
            WHERE claimius.app_has_object(p_app_id, 'claimius.location', l.id)
              AND l.sa_deleted_at IS NULL
            ORDER BY l.id;
        RETURN;
    END IF;

    LOOP
        SELECT array_agg(g.object_id ORDER BY g.object_id) INTO v_ids
        FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.location', p_required_access, v_after, p_limit) g;
        EXIT WHEN v_ids IS NULL;

        RETURN QUERY
            SELECT l.*
            FROM claimius.location l
            WHERE l.id = ANY(v_ids)
              AND claimius.app_has_object(p_app_id, 'claimius.location', l.id)
              AND l.sa_deleted_at IS NULL
            ORDER BY l.id
            LIMIT v_left;
        GET DIAGNOSTICS v_n = ROW_COUNT;
        v_left := v_left - v_n;
        EXIT WHEN v_left <= 0 OR cardinality(v_ids) < p_limit;
        v_after := v_ids[cardinality(v_ids)];
    END LOOP;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_locations(p_user_id uuid, p_app_id uuid, p_required_access integer, p_after uuid, p_limit integer) IS 'Locations whose effective mask covers p_required_access, in id order after p_after, at most p_limit of them: the reader''s range from get_objects, which settles once, each row kept when live and linked to the app before the page is cut.';

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

    PERFORM claimius._settle_directory(p_app_id, p_actor_id);

    IF p_target ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        v_uuid := p_target::UUID;
    END IF;

    SELECT r.user_id INTO v_target_user_id
    FROM (
        SELECT su.user_id, 0 AS rank FROM claimius.samna_user su
        WHERE su.app_id = p_app_id AND su.external_id = p_target AND su.sa_deleted_at IS NULL AND su.status = 'active'
        UNION ALL
        SELECT su.user_id, 1 FROM claimius.samna_user su
        WHERE su.user_id = v_uuid AND su.app_id = p_app_id AND su.sa_deleted_at IS NULL AND su.status = 'active'
        UNION ALL
        SELECT su.user_id, 2 FROM claimius.samna_user su
        WHERE su.id = v_uuid AND su.app_id = p_app_id AND su.sa_deleted_at IS NULL AND su.status = 'active'
    ) r
    ORDER BY r.rank
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
            FROM (
                SELECT coalesce((
                           SELECT uu.sharing_object_count FROM claimius.user_users uu
                           WHERE uu.id = claimius.composite_id(p_app_id::TEXT, p_actor_id::TEXT, v_target_user_id::TEXT)), 0)
                     + coalesce((
                           SELECT sum(m.a_now * m.t_now - (m.a_now - m.a_shift) * (m.t_now - m.t_shift))
                           FROM (
                               SELECT coalesce(sum(d.delta) FILTER (WHERE d.user_id = p_actor_id), 0) AS a_shift,
                                      coalesce(sum(d.delta) FILTER (WHERE d.user_id = v_target_user_id), 0) AS t_shift,
                                      EXISTS (SELECT 1 FROM claimius.user_object uo
                                              WHERE uo.user_id = p_actor_id AND uo.app_id = p_app_id
                                                AND uo.object_type = d.object_type AND uo.object_id = d.object_id)::INTEGER AS a_now,
                                      EXISTS (SELECT 1 FROM claimius.user_object uo
                                              WHERE uo.user_id = v_target_user_id AND uo.app_id = p_app_id
                                                AND uo.object_type = d.object_type AND uo.object_id = d.object_id)::INTEGER AS t_now
                               FROM claimius.user_users_diff d
                               WHERE d.app_id = p_app_id
                                 AND d.user_id IN (p_actor_id, v_target_user_id)
                               GROUP BY d.object_type, d.object_id
                           ) m), 0) AS n
            ) shared
            WHERE shared.n > 0
              AND p_actor_id <> v_target_user_id

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
    PERFORM claimius._settle_directory(p_app_id, p_user_id);
    PERFORM claimius._settle_user_users_diff(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        WITH branches AS (
            SELECT p_user_id AS target_user_id, 47::INTEGER AS access
            WHERE p_user_id IS NOT NULL

            UNION ALL

            SELECT uu.target_user_id, 36::INTEGER
            FROM claimius._user_users_for_viewer(p_app_id, p_user_id) uu

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

GRANT EXECUTE ON FUNCTION claimius.get_direct_access(UUID, UUID, UUID, TEXT, INTEGER, BOOLEAN, BOOLEAN)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_objects(UUID, UUID, TEXT, INTEGER, UUID, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_user_access(UUID, UUID, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_users(UUID, UUID, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

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
END;
$$ LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.resolve_access(UUID, UUID, TEXT, TEXT, INTEGER) IS 'The access verdict on one object of any type: audit, relation and samna_user rows through their own lookups, every other type through get_access, which settles and reads the pair in one pass and walks the ancestors only when the settle was skipped on a lock. No row for a malformed id, so a batch never aborts on one.';
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

COMMENT ON FUNCTION claimius.resolve_access(UUID, TEXT, TEXT, INTEGER) IS 'resolve_access for the deployment''s own app, get_app_id().';

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
    v_app_id   UUID := claimius.get_app_id();
    v_uuid     UUID;
    v_exists   BOOLEAN;
    v_has_soft BOOLEAN;
BEGIN
    IF p_object_type <> 'claimius.samna_user' AND (
        p_object_id !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
        OR (p_object_type NOT IN ('claimius.audit', 'claimius.user_relation')
            AND NOT EXISTS (SELECT 1 FROM claimius.table_info ti WHERE ti.object_type = p_object_type))
    ) THEN
        RAISE EXCEPTION 'not found' USING ERRCODE = 'P0404';
    END IF;

    IF p_object_id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        v_uuid := p_object_id::UUID;
    END IF;

    RETURN QUERY
        SELECT * FROM claimius.resolve_access(p_user_id, v_app_id, p_object_id, p_object_type, p_required);
    IF FOUND THEN
        RETURN;
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

    SELECT has_sa_deleted_at INTO v_has_soft
    FROM claimius.table_info
    WHERE object_type = p_object_type;

    IF v_has_soft IS NULL THEN
        v_exists := FALSE;
    ELSIF v_has_soft THEN
        EXECUTE format(
            'SELECT EXISTS (SELECT 1 FROM %I.%I WHERE id = $1 AND sa_deleted_at IS NULL)',
            split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2)
        ) USING v_uuid INTO v_exists;
    ELSE
        EXECUTE format(
            'SELECT EXISTS (SELECT 1 FROM %I.%I WHERE id = $1)',
            split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2)
        ) USING v_uuid INTO v_exists;
    END IF;

    IF v_exists THEN
        RAISE EXCEPTION 'no access' USING ERRCODE = 'P0403';
    ELSE
        RAISE EXCEPTION 'not found' USING ERRCODE = 'P0404';
    END IF;
END;
$$ LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.mutation_access(UUID, TEXT, TEXT, INTEGER) IS 'Single object access gate. Resolves through resolve_access, one point settle and one read of the pair, and on a miss answers from one existence probe: SQLSTATE P0403 when the object exists but the caller lacks the required access, P0404 when the object is absent or soft deleted, the id is malformed or the type is not registered. Only when the settle was skipped on a lock does get_access walk the ancestors. SQL embodiment of the Go MutationAccess resolution in one round trip. VOLATILE: it settles through _settle_pair.';

GRANT EXECUTE ON FUNCTION claimius.mutation_access(UUID, TEXT, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

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

COMMENT ON FUNCTION claimius.get_claims(UUID, UUID, INTEGER) IS 'The claims the user holds through a live user_claim, whose mask covers the required access. The claims the user can see or manage, held or reached through a cascade, are get_objects of type claimius.claim.';

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
              SELECT (g ->> 'claim_id')::UUID
              FROM claimius.user_object uo
                       CROSS JOIN LATERAL jsonb_array_elements(uo.grants) g
              WHERE uo.user_id = p_user_id
                AND uo.app_id = p_app_id
                AND uo.object_id = p_object_id
                AND uo.object_type = p_object_type
                AND (g ->> 'user_claim_id') IS NOT NULL
                AND ((g ->> 'starts_at') IS NULL OR (g ->> 'starts_at')::TIMESTAMPTZ <= now())
                AND ((g ->> 'ends_at') IS NULL OR (g ->> 'ends_at')::TIMESTAMPTZ > now())
          )
        ORDER BY claimius._popcount(c.sa_access) DESC;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_claims(UUID, UUID, UUID, TEXT) IS 'All claims the user holds against one object.';

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
        UNION ALL
        SELECT uc.id,
               c.id,
               c.name,
               CASE WHEN c.level > cb.level THEN claimius._effective_access(c.sa_access, link.sa_access)
                    WHEN cb.level > c.level THEN claimius._effective_access(cb.sa_access, co.sa_access)
                    ELSE (claimius._effective_access(c.sa_access, link.sa_access)
                            & claimius._effective_access(cb.sa_access, co.sa_access) & ~16)
                         | ((claimius._effective_access(c.sa_access, link.sa_access)
                            | claimius._effective_access(cb.sa_access, co.sa_access)) & 16) END,
               co.sa_owner_id,
               co.sa_root_id,
               co.id,
               co.scope,
               co.sa_access,
               co.inherits,
               greatest(c.level, cb.level)
        FROM claimius.user_claim uc
                 JOIN claimius.claim c ON c.id = uc.claim_id
                 JOIN claimius.claim_object link
                      ON link.claim_id = c.id
                     AND link.app_id = uc.app_id
                     AND link.object_type = 'claimius.claim'
                     AND link.inherits
                     AND link.sa_deleted_at IS NULL
                 JOIN claimius.claim cb ON cb.id = link.object_id AND cb.inherits AND cb.sa_deleted_at IS NULL
                 JOIN claimius.claim_object co
                      ON co.claim_id = cb.id
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

COMMENT ON FUNCTION claimius.get_scope_access(UUID, UUID, JSONPATH, INTEGER) IS 'Object-less bindings of the user''s live grants whose scope matches a jsonpath, one row per binding, each mask the claim mask narrowed by the binding mask, less the bits of the deny bindings matching the same path whose level is not lower than the claim''s, closed under implication.';

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

CREATE OR REPLACE FUNCTION claimius.get_object_assets(
    p_user_id      UUID,
    p_app_id       UUID,
    p_object_type  TEXT,
    p_object_id    UUID,
    p_kind         TEXT DEFAULT NULL
) RETURNS SETOF claimius.object_asset AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    IF p_object_type = 'claimius.samna_user' THEN
        IF p_object_id IS DISTINCT FROM p_user_id AND NOT EXISTS (
            SELECT 1 FROM claimius.get_user_access(p_user_id, p_app_id, p_object_id::TEXT, 4)
        ) THEN
            RETURN;
        END IF;
    ELSIF NOT EXISTS (SELECT 1 FROM claimius.get_access(p_user_id, p_app_id, p_object_id, p_object_type, 4)) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT l.*
        FROM claimius.object_asset l
        WHERE l.object_type = p_object_type
          AND l.object_id = p_object_id
          AND l.sa_deleted_at IS NULL
          AND (p_kind IS NULL OR l.kind = p_kind)
          AND p_app_id IN (SELECT claimius.apps_for_object(l.object_type, l.object_id, l.app_id))
        ORDER BY l."index", l.sa_created_at;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_object_assets(UUID, UUID, TEXT, UUID, TEXT) IS 'Live object_asset links on one object that the app sees and the reader may read, of one kind when p_kind is given, ordered by index then sa_created_at. A samna_user is readable by the user itself and by a reader get_user_access resolves with READ, the branches get_users decides by; any other object needs READ through get_access.';

GRANT EXECUTE ON FUNCTION claimius.get_object_assets(UUID, UUID, TEXT, UUID, TEXT)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

DROP FUNCTION IF EXISTS claimius.get_owner_graph(UUID, UUID, INTEGER);
DROP FUNCTION IF EXISTS claimius.get_claim_graph(UUID, UUID, INTEGER);

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
                SELECT CASE WHEN n.parent_id IN (SELECT x.object_id FROM node x) THEN n.parent_id END AS parent_key
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
        WHERE (s.source_id, s.source_type) NOT IN (SELECT t.target_id, t.target_type FROM link t)
        UNION
        SELECT DISTINCT n.value ->> 'object_type', (n.value ->> 'object_id')::UUID, NULL::UUID
        FROM jsonb_array_elements(coalesce(p_nodes, '[]'::JSONB)) AS n
        WHERE ((n.value ->> 'object_id')::UUID, n.value ->> 'object_type') NOT IN (SELECT t.target_id, t.target_type FROM link t)
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

    IF p_start_id IS NULL THEN
        SELECT array_agg(o.id) INTO v_root_ids
        FROM claimius.organization o
        WHERE o.id IN (SELECT claimius.app_object_ids(p_app_id, 'claimius.organization'))
          AND o.sa_deleted_at IS NULL
          AND o.sa_owner_id = o.id
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
                               AND uo.app_id = p_app_id
        WHERE w.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, p_app_id, 'claimius.organization', p_required_access) g
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
                            CASE WHEN v_table.has_sa_parent_id THEN 'WHEN t.sa_parent_id IS NOT NULL AND t.sa_parent_id <> t.id THEN t.sa_parent_id' ELSE '' END,
                            CASE WHEN v_table.has_sa_location_id THEN 'WHEN t.sa_location_id IS NOT NULL THEN t.sa_location_id' ELSE '' END,
                            CASE WHEN v_table.has_sa_owner_id THEN 'WHEN t.sa_owner_id IS NOT NULL AND t.sa_owner_id <> t.id THEN t.sa_owner_id' ELSE '' END,
                            split_part(v_table.object_type, '.', 1),
                            split_part(v_table.object_type, '.', 2),
                            v_table.object_type,
                            v_table.object_type,
                            CASE WHEN v_table.has_sa_deleted_at THEN 't.sa_deleted_at IS NULL' ELSE 'TRUE' END,
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
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp SET plan_cache_mode = force_custom_plan;

COMMENT ON FUNCTION claimius.get_owner_graph(UUID, UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) IS 'Ownership graph (orgs, locations, registered tables with sa_owner_id or sa_location_id) for the user. Optional start org and depth limit, and a window of p_width children of each type per parent in name or usage order continuing after p_next_id.';

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
    v_pending       INTEGER;
BEGIN
    IF p_sort IS NULL OR p_sort NOT IN ('name', 'usage') OR p_width IS NULL OR p_width < 0 THEN
        RAISE EXCEPTION 'graph window: p_sort is name or usage and p_width at least 0' USING ERRCODE = '22023';
    END IF;

    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);
    PERFORM claimius._settle_directory(p_app_id, p_user_id);
    v_pending := claimius._settle_user_users_diff(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    v_cacheable := (p_start_id IS NULL AND p_depth = 0 AND p_required_access = 0);

    IF v_cacheable AND v_pending = 0 THEN
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
                     JOIN claimius._user_users_for_viewer(p_app_id, p_user_id) uu
                          ON uu.target_user_id = u.user_id
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
            DECLARE
                v_lock_timeout TEXT := current_setting('lock_timeout');
            BEGIN
                PERFORM set_config('lock_timeout', claimius._settle_lock_timeout(), TRUE);
                INSERT INTO claimius.claim_graph_cache (app_id, user_id, graph, snapshot)
                VALUES (p_app_id, p_user_id, v_graph, v_snapshot)
                ON CONFLICT (app_id, user_id) DO UPDATE
                    SET graph = EXCLUDED.graph,
                        snapshot = EXCLUDED.snapshot;
                PERFORM set_config('lock_timeout', v_lock_timeout, TRUE);
            EXCEPTION WHEN lock_not_available OR deadlock_detected THEN
                PERFORM set_config('lock_timeout', v_lock_timeout, TRUE);
            END;
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
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp SET plan_cache_mode = force_custom_plan;

COMMENT ON FUNCTION claimius.get_claim_graph(UUID, UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) IS 'Access graph: user, other users seen, claims, user_claims, claim_object bindings, reachable objects. Optional start claim and depth limit, and a window of p_width children of each type per parent in name or usage order continuing after p_next_id. Default shape served from claimius.claim_graph_cache while every grant, claim and binding of the user was written by a transaction visible in the snapshot taken before the cached graph was built. The cache row is written under the settle lock_timeout and skipped when a writer holds it, so a read never waits on a writer.';

CREATE OR REPLACE FUNCTION claimius._invalidate_claim_graph_cache()
    RETURNS TRIGGER AS $$
DECLARE
    v_app_id    UUID;
    v_user_id   UUID;
BEGIN
    IF TG_TABLE_NAME = 'user_object' AND TG_OP = 'UPDATE' THEN
        DELETE FROM claimius.claim_graph_cache c
        USING (SELECT DISTINCT n.app_id, n.user_id
               FROM changed n
                   JOIN before o ON o.user_id = n.user_id AND o.app_id = n.app_id AND o.object_type = n.object_type AND o.object_id = n.object_id
               WHERE (n.sa_access, n.grants, n.scope, n.direct_grant) IS DISTINCT FROM (o.sa_access, o.grants, o.scope, o.direct_grant)) u
        WHERE c.app_id = u.app_id AND c.user_id = u.user_id;
        RETURN NULL;
    ELSIF TG_TABLE_NAME = 'user_object' THEN
        DELETE FROM claimius.claim_graph_cache c
        USING (SELECT DISTINCT x.app_id, x.user_id FROM changed x) u
        WHERE c.app_id = u.app_id AND c.user_id = u.user_id;
        RETURN NULL;
    ELSIF TG_TABLE_NAME = 'user_users' THEN
        DELETE FROM claimius.claim_graph_cache c
        USING (SELECT DISTINCT x.app_id, x.viewer_id FROM changed x) u
        WHERE c.app_id = u.app_id AND c.user_id = u.viewer_id;
        RETURN NULL;
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

COMMENT ON FUNCTION claimius._invalidate_claim_graph_cache() IS 'Trigger function. Drops claim_graph_cache rows affected by writes to user_object and user_users, once per statement over its transition table, and by writes to object_info and samna_user, per row. A claim or binding change is caught on read by get_claim_graph.';

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
    v_tsquery   TSQUERY;
    v_has_query BOOLEAN := FALSE;
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    IF btrim(coalesce(p_query, '')) <> '' THEN
        v_tsquery   := to_tsquery('simple', regexp_replace(websearch_to_tsquery('simple', p_query)::TEXT, '''([^'']+)''', '''\1'':*', 'g'));
        v_has_query := numnode(v_tsquery) > 0;
    END IF;

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
            ((CASE WHEN v_has_query THEN ts_rank(oi.search_vector, v_tsquery) ELSE 0 END) + claimius._decayed_usage(claimius.user_object.used_count, claimius.user_object.last_used_at))::REAL
        FROM claimius.user_object
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_required_access) best ON TRUE
                 JOIN claimius.object_info oi
                      ON oi.object_type = claimius.user_object.object_type
                     AND oi.object_id = claimius.user_object.object_id
        WHERE claimius.user_object.user_id = p_user_id
          AND claimius.user_object.app_id = p_app_id
          AND (claimius.user_object.sa_access & p_required_access) = p_required_access
          AND (NOT v_has_query OR oi.search_vector @@ v_tsquery)
          AND (p_object_types IS NULL OR claimius.user_object.object_type = ANY(p_object_types))
          AND (best.user_claim_id IS NOT NULL OR claimius.user_object.direct_grant)
        ORDER BY 12 DESC, 1
        LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, INTEGER, INTEGER) IS 'Full text search over accessible objects whose mask covers p_required_access. An empty query lists them, ranked by the reader''s decayed usage. user_claim_id is the actor token from the strongest surviving claim grant per object.';

CREATE OR REPLACE FUNCTION claimius._search_scope_smaller(
    p_app_id       UUID,
    p_user_id      UUID,
    p_object_types TEXT[],
    p_tree         claimius.tree_type,
    p_scope_type   TEXT,
    p_scope_ids    UUID[]
) RETURNS BOOLEAN AS $$
DECLARE
    v_k     INTEGER := 64;
    v_pairs INTEGER;
    v_scope INTEGER;
BEGIN
    LOOP
        SELECT count(*) INTO v_pairs FROM (
            SELECT 1 FROM claimius.user_object uo
            WHERE uo.user_id = p_user_id
              AND uo.app_id = p_app_id
              AND (p_object_types IS NULL OR uo.object_type = ANY(p_object_types))
            LIMIT v_k
        ) x;
        SELECT count(*) INTO v_scope FROM (
            SELECT 1 FROM claimius.inheritance_info ii
            WHERE ii.ancestor_type = p_scope_type
              AND ii.ancestor_id   = ANY(p_scope_ids)
              AND ii.tree_type     = p_tree
              AND (p_object_types IS NULL OR ii.descendant_type = ANY(p_object_types))
            LIMIT v_k
        ) x;
        IF v_pairs < v_k OR v_scope < v_k THEN
            RETURN v_scope <= v_pairs;
        END IF;
        v_k := v_k * 4;
    END LOOP;
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius._search_scope_smaller(UUID, UUID, TEXT[], claimius.tree_type, TEXT, UUID[]) IS 'True when the scope''s closure rows are no more than the reader''s pairs of the types. Counts both up to k, from 64 and four times larger per round, and stops at the first round where one falls short, so the cost is linear in the smaller of the two.';

CREATE OR REPLACE FUNCTION claimius._search_text_smaller(
    p_app_id       UUID,
    p_user_id      UUID,
    p_object_types TEXT[],
    p_tsquery      TSQUERY
) RETURNS BOOLEAN AS $$
DECLARE
    v_plan    JSONB;
    v_matches NUMERIC;
    v_pairs   NUMERIC;
BEGIN
    EXECUTE 'EXPLAIN (FORMAT JSON) SELECT 1 FROM claimius.object_info oi WHERE oi.search_vector @@ $1 AND ($2::TEXT[] IS NULL OR oi.object_type = ANY($2))'
        INTO v_plan USING p_tsquery, p_object_types;
    v_matches := (v_plan -> 0 -> 'Plan' ->> 'Plan Rows')::NUMERIC;
    EXECUTE 'EXPLAIN (FORMAT JSON) SELECT 1 FROM claimius.user_object uo WHERE uo.user_id = $1 AND uo.app_id = $3 AND ($2::TEXT[] IS NULL OR uo.object_type = ANY($2))'
        INTO v_plan USING p_user_id, p_object_types, p_app_id;
    v_pairs := (v_plan -> 0 -> 'Plan' ->> 'Plan Rows')::NUMERIC;
    RETURN v_matches < v_pairs;
END;
$$ LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius._search_text_smaller(UUID, UUID, TEXT[], TSQUERY) IS 'True when the planner''s estimate of the objects matching the query is below its estimate of the reader''s pairs of the types, so search reads the query''s postings in idx_object_info_search and probes the reader''s pair per match instead of reading the reader''s range. Both are plan estimates, so the choice reads no index entry.';

CREATE OR REPLACE FUNCTION claimius._search_source_hits(
    p_query        TEXT,
    p_object_types TEXT[],
    p_scope_ids    UUID[]
) RETURNS claimius.search_hit[] AS $$
DECLARE
    v_source RECORD;
    v_part   claimius.search_hit[];
    v_hits   claimius.search_hit[] := ARRAY[]::claimius.search_hit[];
BEGIN
    FOR v_source IN
        SELECT ss.object_type, ss.fn::oid::regproc AS fn
        FROM claimius.search_source ss
        WHERE p_object_types IS NULL OR ss.object_type = ANY(p_object_types)
        ORDER BY ss.object_type
    LOOP
        EXECUTE format(
            'SELECT coalesce(array_agg(ROW(%L, s.object_id, least(greatest(s.score, 0), 1))::claimius.search_hit), ARRAY[]::claimius.search_hit[]) FROM %s($1, $2, $3) s',
            v_source.object_type, v_source.fn
        ) INTO v_part USING v_source.object_type, p_query, p_scope_ids;
        v_hits := v_hits || v_part;
    END LOOP;
    RETURN v_hits;
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius._search_source_hits(TEXT, TEXT[], UUID[]) IS 'The rows every registered search source of the requested types returns for the query and the scope ids, each score clamped to [0, 1]. The caller keeps a row only where the reader holds a pair on it.';

CREATE OR REPLACE FUNCTION claimius.upsert_search_source(
    p_object_type TEXT,
    p_fn          REGPROCEDURE
) RETURNS claimius.search_source AS $$
DECLARE
    v_row claimius.search_source;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM claimius.table_info ti WHERE ti.object_type = p_object_type) THEN
        RAISE EXCEPTION 'object type % is not registered', p_object_type USING ERRCODE = 'P0404';
    END IF;
    IF (SELECT ARRAY(SELECT t::regtype::text FROM unnest(p.proargtypes) t) FROM pg_proc p WHERE p.oid = p_fn) <> ARRAY['text', 'text', 'uuid[]'] THEN
        RAISE EXCEPTION 'search source % must take (text, text, uuid[])', p_fn USING ERRCODE = '22023';
    END IF;
    INSERT INTO claimius.search_source (object_type, fn)
    VALUES (p_object_type, p_fn)
    ON CONFLICT (object_type) DO UPDATE SET fn = EXCLUDED.fn, sa_updated_at = now()
    RETURNING * INTO v_row;
    RETURN v_row;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.upsert_search_source(TEXT, REGPROCEDURE) IS 'Registers the search source of a registered object type: a function (object_type text, query text, scope_ids uuid[]) returning (object_id uuid, score real) over the host''s own rows of that type. search adds its rows for a non empty query, each kept only where the reader holds a pair on it, with the pair''s grant and field scope.';

CREATE OR REPLACE FUNCTION claimius.delete_search_source(p_object_type TEXT) RETURNS VOID AS $$
BEGIN
    DELETE FROM claimius.search_source WHERE object_type = p_object_type;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.delete_search_source(TEXT) IS 'Removes the search source of an object type.';

GRANT EXECUTE ON FUNCTION claimius.upsert_search_source(TEXT, REGPROCEDURE), claimius.delete_search_source(TEXT) TO claimius_admin;

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
#variable_conflict use_column
DECLARE
    v_tsquery        TSQUERY;
    v_has_query      BOOLEAN := FALSE;
    v_include_claim  BOOLEAN := p_object_types IS NULL OR 'claimius.claim' = ANY(p_object_types);
    v_include_others BOOLEAN := p_object_types IS NULL OR EXISTS (
        SELECT 1 FROM unnest(p_object_types) t WHERE t <> 'claimius.claim'
    );
    v_parent_tree    claimius.tree_type;
    v_scope_tree     claimius.tree_type;
    v_scope_type     TEXT;
    v_scope_ids      UUID[];
    v_from_scope     BOOLEAN := FALSE;
    v_from_text      BOOLEAN := FALSE;
    v_hits           claimius.search_hit[] := ARRAY[]::claimius.search_hit[];
BEGIN
    PERFORM claimius.reconcile_if_pending(p_app_id, p_user_id);

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN;
    END IF;

    IF btrim(coalesce(p_query, '')) <> '' THEN
        v_tsquery   := to_tsquery('simple', regexp_replace(websearch_to_tsquery('simple', p_query)::TEXT, '''([^'']+)''', '''\1'':*', 'g'));
        v_has_query := numnode(v_tsquery) > 0;
    END IF;

    IF p_parent_type = 'claimius.location' THEN
        v_parent_tree := 'location'::claimius.tree_type;
    ELSIF p_parent_type IS NOT NULL AND EXISTS (
        SELECT 1 FROM claimius.table_info ti WHERE ti.object_type = p_parent_type AND ti.has_sa_parent_id
    ) THEN
        v_parent_tree := 'parenthood'::claimius.tree_type;
    END IF;

    IF p_owner_subtree_ids IS NOT NULL THEN
        v_scope_tree := 'ownership'::claimius.tree_type;
        v_scope_type := 'claimius.organization';
        v_scope_ids  := p_owner_subtree_ids;
    ELSIF p_parent_id IS NOT NULL AND v_parent_tree IS NOT NULL THEN
        v_scope_tree := v_parent_tree;
        v_scope_type := p_parent_type;
        v_scope_ids  := ARRAY[p_parent_id];
    END IF;

    IF v_scope_tree IS NOT NULL AND v_include_others THEN
        v_from_scope := claimius._search_scope_smaller(p_app_id, p_user_id, p_object_types, v_scope_tree, v_scope_type, v_scope_ids);
    END IF;

    IF v_has_query AND v_include_others AND NOT v_from_scope THEN
        v_from_text := claimius._search_text_smaller(p_app_id, p_user_id, p_object_types, v_tsquery);
    END IF;

    IF v_has_query AND v_include_others THEN
        v_hits := claimius._search_source_hits(p_query, p_object_types, v_scope_ids);
    END IF;

    RETURN QUERY
    WITH scoped AS (
        SELECT DISTINCT ii.descendant_type AS object_type, ii.descendant_id AS object_id
        FROM claimius.inheritance_info ii
        WHERE v_from_scope
          AND ii.ancestor_type = v_scope_type
          AND ii.ancestor_id   = ANY(v_scope_ids)
          AND ii.tree_type     = v_scope_tree
          AND (p_object_types IS NULL OR ii.descendant_type = ANY(p_object_types))
    ),
    pairs AS (
        SELECT uo.*
        FROM scoped s
        JOIN claimius.user_object uo
          ON uo.user_id     = p_user_id
         AND uo.app_id      = p_app_id
         AND uo.object_type = s.object_type
         AND uo.object_id   = s.object_id
        WHERE v_from_scope
        UNION ALL
        SELECT uo.*
        FROM claimius.object_info mi
        JOIN claimius.user_object uo
          ON uo.user_id     = p_user_id
         AND uo.app_id      = p_app_id
         AND uo.object_type = mi.object_type
         AND uo.object_id   = mi.object_id
        WHERE v_from_text
          AND mi.search_vector @@ v_tsquery
          AND (p_object_types IS NULL OR mi.object_type = ANY(p_object_types))
        UNION ALL
        SELECT uo.*
        FROM claimius.user_object uo
        WHERE NOT v_from_scope
          AND NOT v_from_text
          AND v_include_others
          AND uo.user_id = p_user_id
          AND uo.app_id = p_app_id
          AND (p_object_types IS NULL OR uo.object_type = ANY(p_object_types))
    ),
    objects AS (
        SELECT
            uo.object_id,
            uo.object_type,
            best.user_claim_id,
            oi.sa_name,
            oi.sa_description,
            oi.sa_link,
            uo.sa_access,
            uo.scope,
            oi.sa_owner_id,
            oi.sa_location_id,
            (uo.direct_grant AND best.user_claim_id IS NULL) AS is_direct_grant,
            ((CASE WHEN v_has_query THEN ts_rank(oi.search_vector, v_tsquery) ELSE 0 END)
                + claimius._decayed_usage(uo.used_count, uo.last_used_at))::REAL AS rank
        FROM pairs uo
                 LEFT JOIN LATERAL claimius._best_grant(uo.grants, p_required_access) best ON TRUE
                 JOIN claimius.object_info oi
                      ON oi.object_type = uo.object_type
                     AND oi.object_id   = uo.object_id
        WHERE (uo.sa_access & p_required_access) = p_required_access
          AND (NOT v_has_query OR oi.search_vector @@ v_tsquery)
          AND (best.user_claim_id IS NOT NULL OR uo.direct_grant)
    ),
    sourced AS (
        SELECT
            uo.object_id,
            uo.object_type,
            best.user_claim_id,
            oi.sa_name,
            oi.sa_description,
            oi.sa_link,
            uo.sa_access,
            uo.scope,
            oi.sa_owner_id,
            oi.sa_location_id,
            (uo.direct_grant AND best.user_claim_id IS NULL) AS is_direct_grant,
            (h.score + claimius._decayed_usage(uo.used_count, uo.last_used_at))::REAL AS rank
        FROM unnest(v_hits) h
                 JOIN claimius.user_object uo
                      ON uo.user_id     = p_user_id
                     AND uo.app_id      = p_app_id
                     AND uo.object_type = h.object_type
                     AND uo.object_id   = h.object_id
                 LEFT JOIN LATERAL claimius._best_grant(uo.grants, p_required_access) best ON TRUE
                 LEFT JOIN claimius.object_info oi
                      ON oi.object_type = uo.object_type
                     AND oi.object_id   = uo.object_id
        WHERE (uo.sa_access & p_required_access) = p_required_access
          AND (best.user_claim_id IS NOT NULL OR uo.direct_grant)
          AND (v_scope_tree IS NULL OR EXISTS (
              SELECT 1 FROM claimius.inheritance_info ii
              WHERE ii.descendant_type = uo.object_type
                AND ii.descendant_id   = uo.object_id
                AND ii.tree_type       = v_scope_tree
                AND ii.ancestor_type   = v_scope_type
                AND ii.ancestor_id     = ANY(v_scope_ids)
          ))
    ),
    claims AS (
        SELECT
            c.id,
            'claimius.claim'::TEXT,
            uc.id,
            c.name,
            c.description,
            NULL::TEXT,
            c.sa_access,
            NULL::JSONB,
            c.sa_owner_id,
            NULL::UUID,
            TRUE,
            (CASE WHEN v_has_query
                  THEN ts_rank(to_tsvector('simple', coalesce(c.name, '') || ' ' || coalesce(c.description, '')), v_tsquery)
                  ELSE 0 END)::REAL
        FROM claimius.claim c
                 JOIN claimius.user_claim uc ON uc.claim_id = c.id
            AND uc.user_id = p_user_id
            AND uc.app_id = p_app_id
            AND uc.sa_deleted_at IS NULL
            AND (uc.starts_at IS NULL OR uc.starts_at <= now())
            AND (uc.ends_at IS NULL OR uc.ends_at > now())
        WHERE v_include_claim
          AND v_scope_tree IS NULL
          AND c.app_id = p_app_id
          AND c.sa_deleted_at IS NULL
          AND (c.sa_access & p_required_access) = p_required_access
          AND (NOT v_has_query OR to_tsvector('simple', coalesce(c.name, '') || ' ' || coalesce(c.description, '')) @@ v_tsquery)
    ),
    combined AS (
        SELECT * FROM objects
        UNION ALL
        SELECT * FROM sourced
        UNION ALL
        SELECT * FROM claims
    ),
    kept AS (
        SELECT DISTINCT ON (cb.object_type, cb.object_id) cb.*
        FROM combined cb
        WHERE (p_object_ids IS NULL OR cb.object_id = ANY(p_object_ids))
          AND (p_exclude_object_ids IS NULL OR NOT (cb.object_id = ANY(p_exclude_object_ids)))
          AND (v_from_scope OR p_owner_subtree_ids IS NULL OR cb.object_type = 'claimius.claim' OR EXISTS (
              SELECT 1 FROM claimius.inheritance_info ii
              WHERE ii.descendant_type = cb.object_type
                AND ii.descendant_id   = cb.object_id
                AND ii.tree_type       = 'ownership'
                AND ii.ancestor_type   = 'claimius.organization'
                AND ii.ancestor_id     = ANY(p_owner_subtree_ids)
          ))
          AND (p_parent_id IS NULL OR v_parent_tree IS NULL OR (v_from_scope AND p_owner_subtree_ids IS NULL) OR EXISTS (
              SELECT 1 FROM claimius.inheritance_info ii
              WHERE ii.descendant_type = cb.object_type
                AND ii.descendant_id   = cb.object_id
                AND ii.tree_type       = v_parent_tree
                AND ii.ancestor_type   = p_parent_type
                AND ii.ancestor_id     = p_parent_id
          ))
        ORDER BY cb.object_type, cb.object_id, cb.rank DESC
    )
    SELECT k.object_id,
           k.object_type,
           k.user_claim_id,
           k.sa_name,
           k.sa_description,
           k.sa_link,
           k.sa_access,
           k.scope,
           k.sa_owner_id,
           k.sa_location_id,
           k.is_direct_grant,
           k.rank,
           COUNT(*) OVER ()::INTEGER
    FROM kept k
    ORDER BY k.rank DESC, k.object_id ASC
    LIMIT p_limit OFFSET p_offset;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, UUID[], UUID[], UUID[], UUID, TEXT, INTEGER, INTEGER) IS 'Search over accessible objects. An empty query lists every accessible object in scope, ranked by the reader''s decayed usage. A query matches object_info.search_vector and the registered search sources, ranked by text match or source score plus decayed usage. With an owner subtree or a parent the read starts from the smaller of the reader''s pairs and the scope''s closure rows, chosen by _search_scope_smaller, and a query without one from the query''s postings in idx_object_info_search when _search_text_smaller estimates them fewer than the reader''s pairs. Includes claimius.claim rows the user holds, skipped when an owner subtree, or a parent whose type has a tree, is set. total_count carries the un-capped match count. Rows tie-break on object_id so offset pagination tiles without overlap or skip.';

GRANT EXECUTE ON FUNCTION claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, INTEGER, INTEGER) TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.search(UUID, UUID, TEXT, TEXT[], INTEGER, UUID[], UUID[], UUID[], UUID, TEXT, INTEGER, INTEGER) TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

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
    touched AS (
        INSERT INTO claimius.usage_diff (app_id, user_id, object_type, object_id, used_count)
        SELECT used.app_id, used.user_id, used.object_type, used.object_id, used.n
        FROM used
        WHERE EXISTS (
            SELECT 1 FROM claimius.user_object uo
            WHERE uo.user_id = used.user_id
              AND uo.app_id = used.app_id
              AND uo.object_type = used.object_type
              AND uo.object_id = used.object_id)
        RETURNING user_id
    ),
    moved AS (
        UPDATE claimius.user_activity a
        SET last_used_at = now()
        WHERE (a.user_id, a.app_id) IN (
            SELECT x.user_id, x.app_id
            FROM claimius.user_activity x
            WHERE (x.user_id, x.app_id) IN (SELECT DISTINCT used.user_id, used.app_id FROM used)
            FOR NO KEY UPDATE SKIP LOCKED)
        RETURNING a.user_id, a.app_id
    ),
    active AS (
        INSERT INTO claimius.user_activity (user_id, app_id, last_used_at)
        SELECT DISTINCT used.user_id, used.app_id, now()
        FROM used
        WHERE NOT EXISTS (
            SELECT 1 FROM claimius.user_activity x
            WHERE x.user_id = used.user_id
              AND x.app_id = used.app_id)
        ORDER BY used.user_id, used.app_id
        ON CONFLICT (user_id, app_id) DO NOTHING
        RETURNING user_id
    )
    SELECT count(*) INTO v_touched FROM touched;

    RETURN v_touched;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.touch_object_usage(UUID[], UUID[], TEXT[], UUID[], BIGINT[]) IS 'Records counted checks as usage_diff rows, folded into user_object.used_count and last_used_at by the user''s next listing, per (user, app, type, object) row of the arrays, skipping pairs not settled and locking no pair, and moves each counted user''s user_activity for the app, skipping a row a read holds for its cursor so the flush never waits. Local, never synced. Returns the pairs touched.';

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

DROP FUNCTION IF EXISTS claimius.update_claim(UUID, TEXT, TEXT, INTEGER, BOOLEAN, BOOLEAN);
DROP FUNCTION IF EXISTS claimius.update_claim(UUID, TEXT, TEXT, INTEGER, BOOLEAN);
DROP FUNCTION IF EXISTS claimius.update_claim(uuid,text,text,integer,boolean);

DROP FUNCTION IF EXISTS claimius.update_claim(UUID, TEXT, TEXT, INTEGER, BOOLEAN, INTEGER);
DROP FUNCTION IF EXISTS claimius.update_claim(UUID, TEXT, TEXT, INTEGER, BOOLEAN, INTEGER, claimius.claim_type);

CREATE OR REPLACE FUNCTION claimius.update_claim(
    p_claim_id          UUID,
    p_name              TEXT DEFAULT NULL,
    p_description       TEXT DEFAULT NULL,
    p_sa_access         INTEGER DEFAULT NULL,
    p_inherits          BOOLEAN DEFAULT NULL,
    p_level             INTEGER DEFAULT NULL,
    p_type              claimius.claim_type DEFAULT NULL,
    p_actor_user_claim_id UUID DEFAULT NULL
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

    PERFORM set_config('claimius.actor', coalesce(p_actor_user_claim_id::TEXT, ''), TRUE);
    UPDATE claimius.claim SET
                              name        = coalesce(NULLIF(p_name, ''), name),
                              description = coalesce(p_description, description),
                              sa_access   = coalesce(NULLIF(v_access, 0), sa_access),
                              inherits    = coalesce(p_inherits, inherits),
                              level       = coalesce(p_level, level)
                              , type        = coalesce(p_type, type)
    WHERE id = p_claim_id
    RETURNING * INTO v_row;
    PERFORM set_config('claimius.actor', '', TRUE);
    RETURN jsonb_build_object('claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_claim(p_claim_id uuid, p_name text, p_description text, p_sa_access integer, p_inherits boolean, p_level integer, p_type claimius.claim_type, p_actor_user_claim_id uuid) IS 'Patches selected fields on a claim, type included. p_sa_access is expanded on grant claims so owner implies write+read+execute, write implies read+execute, execute implies read. Member bit 0x20 stands alone. Deny claims keep the supplied bits verbatim. Empty p_name and zero p_sa_access are treated as preserve so PATCH callers that send only one field do not wipe the others. A mask with no bit besides deny is refused with SQLSTATE 22023.';

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

                UPDATE claimius.samna_user
                SET first_name    = v_source_app_user.first_name,
                    last_name     = v_source_app_user.last_name,
                    user_name     = v_source_app_user.user_name,
                    user_image    = v_source_app_user.user_image,
                    profile       = v_source_app_user.profile,
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

                DELETE FROM claimius.user_object
                WHERE user_id = p_source_user_id AND app_id = v_app_row.app_id;

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

    UPDATE claimius.object_asset SET object_id = p_target_user_id
    WHERE object_type = 'claimius.samna_user'
      AND object_id = p_source_user_id;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.merge_user(p_target_user_id uuid, p_source_user_id uuid, p_acting_user_claim uuid) IS 'Merges the source user_id into the target in every app the source has a row in: moves user_claim, user_relation and user_field, moves the object_asset links on the source user to the target, published to the apps that see the target, retires a source grant of a claim the target already holds, copies the source profile onto an existing target row and removes the source row with its user_object pairs. Prophet set only; samna_auth owns the merge flow.';

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

DROP FUNCTION IF EXISTS claimius.init_claimius_tables(text[]);
CREATE OR REPLACE FUNCTION claimius.upsert_table_info(
    VARIADIC p_object_types TEXT[]
) RETURNS VOID AS $$
DECLARE
    v_object_type   TEXT;
BEGIN
    FOREACH v_object_type IN ARRAY p_object_types LOOP
            IF position('.' IN v_object_type) = 0 THEN
                RAISE EXCEPTION 'object type % is not schema qualified', v_object_type;
            END IF;
            PERFORM claimius._register_table_info(v_object_type, FALSE);
            PERFORM claimius._attach_calc_trigger(v_object_type);
            PERFORM claimius._attach_self_root_trigger(v_object_type);
            RAISE NOTICE 'Registered table %', v_object_type;
        END LOOP;

    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
    BEGIN
        FOREACH v_object_type IN ARRAY p_object_types LOOP
                PERFORM claimius._bulk_seed_user_object(v_object_type);
                RAISE NOTICE 'Seeded user_object for %', v_object_type;
            END LOOP;
    END;
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_table_info(VARIADIC p_object_types text[]) IS 'Registers the named external tables: validates their sa_ columns, attaches the calc and self root triggers and seeds their object_info and user_object rows. Idempotent. Called by implementer migrations.';

CREATE OR REPLACE FUNCTION claimius.delete_table_info(
    VARIADIC p_object_types TEXT[]
) RETURNS VOID AS $$
DECLARE
    v_object_type   TEXT;
BEGIN
    FOREACH v_object_type IN ARRAY p_object_types LOOP
            IF NOT EXISTS (
                SELECT 1 FROM claimius.table_info
                WHERE object_type = v_object_type AND NOT is_internal
            ) THEN
                RAISE EXCEPTION 'object type % is not a registered external table', v_object_type;
            END IF;
            PERFORM claimius._detach_calc_trigger(v_object_type);
            PERFORM claimius._detach_self_root_trigger(v_object_type);
            DELETE FROM claimius.table_info WHERE object_type = v_object_type;
            DELETE FROM claimius.inheritance_info
             WHERE ancestor_type = v_object_type
                OR descendant_type = v_object_type;
            DELETE FROM claimius.user_object WHERE object_type = v_object_type;
            DELETE FROM claimius.object_info WHERE object_type = v_object_type;
            RAISE NOTICE 'Deregistered table %', v_object_type;
        END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_table_info(VARIADIC p_object_types text[]) IS 'Deregisters the named external tables: detaches their triggers and deletes their table_info, inheritance_info, user_object and object_info rows. Raises on a table that is not registered. Called by implementer migrations when a table leaves claimius.';


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
            PERFORM claimius.recompute_user_object(v_user.app_id, v_user.user_id, array_fill(p_object_type, ARRAY[cardinality(v_user.object_ids[v_at : v_at + 999])]), v_user.object_ids[v_at : v_at + 999]);
        END LOOP;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._bulk_seed_user_object(p_object_type text) IS 'Seeds object_info and user_object for an existing table at registration time.';

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
    END;
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.recompute_state() IS 'Rebuilds inheritance_info and user_object for every registered table from live data. Idempotent. Run after bulk writes that bypassed calc triggers.';

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
        'claimius.user_identity'
        ];
    v_t TEXT;
BEGIN
    FOREACH v_t IN ARRAY v_internal_tables LOOP
            PERFORM claimius._register_table_info(v_t, TRUE);
        END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_internal_table_info() IS 'Registers internal claimius.* tables in table_info. Called once at schema bootstrap.';

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
    END;
    PERFORM set_config('claimius.replay_mode', v_replay, TRUE);
    RETURN v_total;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._restamp_created_by() IS 'Rewrites each sa_created_by that is the user_id of a samna_user holding exactly one live user_claim in the row''s app to that user_claim.id, under replay_mode, and leaves every other stamp. audit keeps its own. Idempotent. Run by the base set. Returns the rows rewritten.';

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
    v_default_access     INTEGER;
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

    IF p_default_claim IS NULL THEN
        RAISE EXCEPTION 'init: p_default_claim is required and must contain name and description.';
    END IF;
    v_default_name  := p_default_claim ->> 'name';
    v_default_desc  := p_default_claim ->> 'description';
    v_default_access := coalesce((p_default_claim ->> 'sa_access')::INTEGER, 4);
    IF v_default_name IS NULL OR v_default_name = '' THEN
        RAISE EXCEPTION 'init: p_default_claim.name is required.';
    END IF;
    IF v_default_desc IS NULL OR v_default_desc = '' THEN
        RAISE EXCEPTION 'init: p_default_claim.description is required.';
    END IF;

    v_pre_app_id := gen_random_uuid();
    v_pre_org_id := gen_random_uuid();

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

    IF NOT EXISTS (
        SELECT 1 FROM claimius.samna_user
        WHERE user_id = p_system_user_id AND app_id = v_app_id AND sa_deleted_at IS NULL
    ) THEN
        INSERT INTO claimius.samna_user (user_id, app_id, status, type, first_name, last_name)
        VALUES (p_system_user_id, v_app_id, 'active', 'system', 'System', 'User');
    END IF;

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

    UPDATE claimius.samna_app SET sa_owner_id = v_org_id WHERE id = v_app_id AND sa_owner_id <> v_org_id;

    INSERT INTO claimius.inheritance_info (
        tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
    ) VALUES (
        'ownership', v_org_id, 'claimius.organization', v_org_id, 'claimius.organization', v_org_id, 0
    ) ON CONFLICT DO NOTHING;
    PERFORM claimius._attach_object_to_trees('claimius.samna_app', a.id, to_jsonb(a))
    FROM claimius.samna_app a
    WHERE a.id = v_app_id;

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

    SELECT id INTO v_default_claim_id FROM claimius.claim
    WHERE app_id = v_app_id AND name = v_default_name AND sa_deleted_at IS NULL;

    IF v_default_claim_id IS NULL THEN
        INSERT INTO claimius.claim (
            app_id, name, description, sa_access, inherits, type,
            sa_owner_id, sa_root_id, sa_created_by
        ) VALUES (
                     v_app_id, v_default_name, v_default_desc, v_default_access,
                     FALSE, 'user',
                     v_org_id, v_org_id, v_user_claim_id
                 )
        RETURNING id INTO v_default_claim_id;
    END IF;

    UPDATE claimius.samna_app SET claim_id = v_default_claim_id
    WHERE id = v_app_id AND (claim_id IS NULL OR claim_id <> v_default_claim_id);

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
    PERFORM claimius.recompute_user_object(v_app_id, p_system_user_id, ARRAY['claimius.organization', 'claimius.samna_app'], ARRAY[v_org_id, v_app_id]);

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

    INSERT INTO claimius.prophet_state (app_id, last_applied_seq, system_app_slug)
    VALUES (v_app_id, 0, p_system_app_slug)
    ON CONFLICT (app_id) DO UPDATE SET system_app_slug = EXCLUDED.system_app_slug;

    PERFORM claimius._grant_prophet_roles();

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

DROP FUNCTION IF EXISTS claimius.init_disciple(text);
CREATE OR REPLACE FUNCTION claimius.upsert_disciple_state(
    p_disciple_app_slug TEXT
)
    RETURNS JSONB AS $$
BEGIN
    IF p_disciple_app_slug IS NULL OR p_disciple_app_slug = '' THEN
        RAISE EXCEPTION 'upsert_disciple_state: p_disciple_app_slug is required.';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM claimius.disciple_state) THEN
        INSERT INTO claimius.disciple_state (last_applied_seq, disciple_app_slug)
        VALUES (0, p_disciple_app_slug);
    ELSE
        UPDATE claimius.disciple_state
        SET disciple_app_slug = p_disciple_app_slug,
            sa_updated_at = now()
        WHERE disciple_app_slug <> p_disciple_app_slug;
    END IF;

    PERFORM claimius._grant_disciple_roles();

    RAISE NOTICE 'Disciple initialized for app slug %. Sync layer must populate samna_app and other tables from prophet.', p_disciple_app_slug;

    RETURN '{}'::JSONB;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_disciple_state(p_disciple_app_slug text) IS 'Bootstrap as a disciple. Requires the upstream prophet app slug. State arrives from prophet via sync.';

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

DROP FUNCTION IF EXISTS claimius.create_app(uuid,text,text,uuid,text,text,text[],text,text,text,integer,jsonb,uuid);
DROP FUNCTION IF EXISTS claimius.upsert_app(uuid,text,text,uuid,text,text,text[],text,text,text,integer,jsonb,uuid);
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
    p_app_id              UUID    DEFAULT NULL,
    p_parent_id           UUID    DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_actor             claimius.user_claim;
    v_org               claimius.organization;
    v_parent            claimius.samna_app;
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

    IF p_parent_id IS NOT NULL THEN
        SELECT * INTO v_parent
        FROM claimius.samna_app
        WHERE id = p_parent_id AND sa_deleted_at IS NULL;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'upsert_app: parent app % not found', p_parent_id USING ERRCODE = '22023';
        END IF;
        IF v_parent.sa_owner_id <> v_org.id THEN
            RAISE EXCEPTION 'upsert_app: parent app % is owned by organization %, not %', v_parent.slug, v_parent.sa_owner_id, v_org.id USING ERRCODE = '22023';
        END IF;
    END IF;

    INSERT INTO claimius.samna_app (
        id, slug, name, description, redirect_uris, sync_uri, status, connection,
        private_key, private_seed, sa_owner_id, sa_created_by, sa_parent_id
    ) VALUES (
        coalesce(claimius.ensure_uuid_null(p_app_id), gen_random_uuid()), p_slug, p_name, p_description,
        p_redirect_uris, p_sync_uri, 'active', p_connection::claimius.app_connection,
        p_private_key, p_private_seed, v_org.id, p_actor_user_claim_id, p_parent_id
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
    IF v_app.sa_parent_id <> coalesce(p_parent_id, v_app.id) THEN
        RAISE EXCEPTION 'upsert_app: app % has parent %, not %', p_slug, v_app.sa_parent_id, coalesce(p_parent_id, v_app.id) USING ERRCODE = '22023';
    END IF;

    IF p_parent_id IS NOT NULL THEN
        RETURN jsonb_build_object('samna_app', to_jsonb(v_app));
    END IF;

    INSERT INTO claimius.prophet_state (app_id, last_applied_seq)
    VALUES (v_app.id, 0)
    ON CONFLICT (app_id) DO NOTHING;

    IF NOT EXISTS (
        SELECT 1 FROM claimius.samna_user
        WHERE user_id = v_actor.user_id AND app_id = v_app.id AND sa_deleted_at IS NULL
    ) THEN
        INSERT INTO claimius.samna_user (user_id, app_id, first_name, last_name, user_name, user_image, profile, email, phone, status, type)
        SELECT v_actor.user_id, v_app.id, su.first_name, su.last_name, su.user_name, su.user_image, su.profile, su.email, su.phone, 'active', su.type
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

COMMENT ON FUNCTION claimius.upsert_app(UUID, TEXT, TEXT, UUID, TEXT, TEXT, TEXT[], TEXT, TEXT, TEXT, INTEGER, JSONB, UUID, UUID) IS 'Registers a new app on an existing prophet, owned by an existing organization the actor holds owner on in its own app. Every row is stamped with the actor''s user_claim.id. Links the organization tree, creates and binds the owner claim and grants it to the actor''s user in the app, and creates and binds the default claim. With p_parent_id it registers an Initiate app of that parent, owned by the same organization, and creates nothing else: users, claims, organizations and the sync resolve on the root. Idempotent on slug; never reparents. Returns jsonb keyed by table.';

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

    SELECT a.claim_id, c.sa_owner_id
    INTO v_default_claim_id, v_default_claim_owner
    FROM claimius.samna_app a
             LEFT JOIN claimius.claim c ON c.id = a.claim_id AND c.sa_deleted_at IS NULL
    WHERE a.id = p_app_id;

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

COMMENT ON FUNCTION claimius.get_claim_for_user(UUID) IS 'The claim a user_claim grants, when both are live.';

CREATE OR REPLACE FUNCTION claimius.normalize_object_type(p_object_type TEXT)
RETURNS TEXT AS $$
    SELECT CASE
        WHEN p_object_type IS NULL THEN NULL
        WHEN position('.' IN p_object_type) = 0 THEN p_object_type
        ELSE split_part(p_object_type, '.', 2)
    END;
$$ LANGUAGE sql IMMUTABLE;

COMMENT ON FUNCTION claimius.normalize_object_type(TEXT) IS 'The wire form of an object type: the claimius schema prefix stripped, any other kept.';

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

COMMENT ON FUNCTION claimius.graph_type_passes(TEXT, TEXT[], TEXT[]) IS 'True when an object type passes the include and exclude filters of a graph call.';

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

COMMENT ON FUNCTION claimius.merge_graphs(JSONB, JSONB) IS 'Merges two graph documents: the objects maps per type, and the union of the two link lists.';

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
    v_reach   UUID[] := ARRAY(
        SELECT claimius.composite_id(uo.object_type, uo.object_id::TEXT)
        FROM claimius.user_object uo
        WHERE uo.user_id = p_user_id
          AND uo.app_id = p_app_id
          AND (p_required_access = 0 OR (uo.sa_access & p_required_access) = p_required_access)
          AND (uo.direct_grant OR EXISTS (SELECT 1 FROM claimius._best_grant(uo.grants, p_required_access))));
BEGIN
    IF p_sort IS NULL OR p_sort NOT IN ('name', 'usage') OR p_width IS NULL OR p_width < 0 THEN
        RAISE EXCEPTION 'graph window: p_sort is name or usage and p_width at least 0' USING ERRCODE = '22023';
    END IF;

    IF NOT claimius.check_user_active(p_app_id, p_user_id) THEN
        RETURN jsonb_build_object('links', '[]'::jsonb);
    END IF;

    WITH descendants AS (
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
    in_scope AS MATERIALIZED (
        SELECT d.object_id, d.object_type
        FROM descendants d
        WHERE p_start_id IS NULL
           OR d.object_id = p_start_id
           OR (d.object_type, d.object_id) IN (
               SELECT ii.descendant_type, ii.descendant_id
               FROM claimius.inheritance_info ii
               WHERE ii.ancestor_type = ANY(v_allowed)
                 AND ii.ancestor_id = p_start_id
                 AND ii.depth >= 1
                 AND (p_depth = 0 OR ii.depth <= p_depth))
    ),
    edges AS (
        SELECT r.source_type, r.source_id, r.target_type, r.target_id, r.tree_type::TEXT AS relation_raw
        FROM (
            SELECT ii.tree_type,
                   ii.ancestor_type   AS source_type,
                   ii.ancestor_id     AS source_id,
                   ii.descendant_type AS target_type,
                   ii.descendant_id   AS target_id,
                   ii.depth,
                   row_number() OVER (PARTITION BY ii.tree_type, ii.descendant_type, ii.descendant_id ORDER BY ii.depth) AS rank
            FROM in_scope vt
                JOIN claimius.inheritance_info ii
                    ON ii.descendant_type = vt.object_type
                   AND ii.descendant_id = vt.object_id
                   AND ii.depth >= 1
            WHERE ii.ancestor_type = ANY(v_allowed)
              AND claimius.composite_id(ii.ancestor_type, ii.ancestor_id::TEXT) = ANY(v_reach)
        ) r
        WHERE r.depth = 1 OR r.rank = 1
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
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp SET plan_cache_mode = force_custom_plan;

COMMENT ON FUNCTION claimius.get_graph_links_owner(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) IS 'The ownership graph of the objects the user reaches as links: for every object in scope its closest ancestor the user also reaches, tested against the user''s pairs read once into a hashed set, with an optional window of p_width children per parent. Settles nothing.';

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
    PERFORM claimius._settle_directory(p_app_id, p_user_id);
    PERFORM claimius._settle_user_users_diff(p_app_id, p_user_id);

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
        JOIN claimius._user_users_for_viewer(p_app_id, p_user_id) uu ON uu.target_user_id = u.user_id
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
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp SET plan_cache_mode = force_custom_plan;

COMMENT ON FUNCTION claimius.get_graph_links_claim(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) IS 'The claim graph as links: the user''s grants, the bindings of the claims, the cascades of inheriting bindings, the users the user sees through _user_users_for_viewer after folding its user_users_diff rows, and the user''s relations, with an optional window of p_width children per parent.';

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
    v_token_row         claimius.user_identity;
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
          FROM claimius.user_identity
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
        INSERT INTO claimius.user_identity (
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
        ON CONFLICT (sa_owner_id, external_id) WHERE sa_deleted_at IS NULL DO UPDATE SET
            app_id        = EXCLUDED.app_id,
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
        'user_identity',        CASE WHEN p_identity IS NULL THEN NULL ELSE to_jsonb(v_token_row) END
    );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.insert_organization_user(UUID, UUID, UUID, JSONB, BOOLEAN, JSONB, TEXT) IS
    'Adds a user to an organization of an app: inserts or upserts the samna_user, grants the supplied claim with p_reason, ensures a per-user virtual claim with full access bound to the user, and when p_caller_uc is not null grants that virtual claim to the caller. The organization must be linked to the app and the caller must be a live user_claim of the app. With p_identity, an outside identity {id_claim, external_id, type, data} names the user: a user_identity of the app, owner and external_id reuses its user, the call upserts, and the user_identity is written. Without p_identity and with p_upsert=FALSE, raises 23505 when user_id or external_id already exists in the app. Returns samna_user, created, user_claim, virtual_claim, claim_object, caller_user_claim and user_identity.';

GRANT EXECUTE ON FUNCTION claimius.insert_organization_user(UUID, UUID, UUID, JSONB, BOOLEAN, JSONB, TEXT)
    TO claimius_disciple_client, claimius_writer, claimius_admin;

ALTER TABLE claimius.user_identity ALTER COLUMN app_id SET DEFAULT claimius.get_app_id();
ALTER TABLE claimius.audit ALTER COLUMN app_id SET DEFAULT claimius.get_app_id();

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
                                   'samna_secret', 'object_field', 'user_field', 'user_relation', 'code'] LOOP
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

    IF p_limit IS NULL OR v_total < p_limit THEN
        SELECT count(*) INTO v_n
        FROM (SELECT 1 FROM claimius.object_asset oa
              WHERE p_app_id IN (SELECT claimius.apps_for_object(oa.object_type, oa.object_id, oa.app_id))
              LIMIT p_limit - v_total) t;
        v_total := v_total + v_n;
    END IF;

    RETURN v_total;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.count_sync_source_rows(UUID, BIGINT) IS 'Source rows of one app the sync snapshot ships, object_asset links published to it included, counted up to p_limit when given.';

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
    ELSIF p_event_type = 'claimius.object_asset' THEN
        SELECT oa.app_id INTO v_other FROM claimius.object_asset oa WHERE oa.id = v_id;
        IF v_other IS NOT NULL AND v_other IS DISTINCT FROM (p_payload ->> 'app_id')::UUID THEN
            RETURN format('%s %s is held here with home app %s', p_event_type, v_id, v_other);
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

COMMENT ON FUNCTION claimius._check_peer_row(TEXT, TEXT, TEXT, JSONB) IS 'Reason a row from a peer''s feed of an app is refused, or NULL when it may apply: the app must be decided by that peer, and the row id must not be held here by another app or by an app another prophet decides. An object_asset link names its home app, which may be one this prophet does not mirror, so it is refused only when its id is held here with another home.';

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
                            'claimius.user_field', 'claimius.user_relation', 'claimius.object_asset', 'claimius.code') THEN
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
    ELSIF v_table = 'claim_object' AND (v_row ->> 'object_type' = 'claimius.claim' OR v_row ->> 'object_id' IS NULL) THEN
        PERFORM claimius._raise_claim_diff(v_app_id, (v_row ->> 'claim_id')::UUID);
        IF v_row ->> 'object_type' = 'claimius.claim' THEN
            PERFORM claimius._raise_claim_diff(v_app_id, (v_row ->> 'object_id')::UUID);
        END IF;
    ELSIF v_table = 'claim_object' THEN
        PERFORM claimius._raise_object_diff(v_app_id, v_row ->> 'object_type', (v_row ->> 'object_id')::UUID, TRUE);
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

CREATE OR REPLACE FUNCTION claimius._check_code_owner(
    p_app_id        UUID,
    p_claim_id      UUID,
    p_actor_user_id UUID
) RETURNS UUID AS $$
DECLARE
    v_actor_uc UUID;
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM claimius.claim
        WHERE id = p_claim_id AND app_id = p_app_id AND sa_deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'claim % not found in the app', p_claim_id USING ERRCODE = 'P0404';
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
        RAISE EXCEPTION 'the actor does not own claim %', p_claim_id USING ERRCODE = 'P0403';
    END IF;
    RETURN v_actor_uc;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._check_code_owner(UUID, UUID, UUID) IS 'The grant by which the actor owns a claim of the app, the check every code and guest write runs. Raises P0404 for a claim outside the app and P0403 when the actor does not own it.';

CREATE OR REPLACE FUNCTION claimius.update_claim_guest(
    p_app_id        UUID,
    p_claim_id      UUID,
    p_actor_user_id UUID,
    p_guest_user_id UUID,
    p_mode          TEXT
) RETURNS JSONB AS $$
DECLARE
    v_guest JSONB;
    v_claim claimius.claim;
BEGIN
    IF p_mode NOT IN ('open', 'code', 'off') THEN
        RAISE EXCEPTION 'update_claim_guest: unknown mode %', p_mode USING ERRCODE = '22023';
    END IF;
    PERFORM claimius._check_code_owner(p_app_id, p_claim_id, p_actor_user_id);

    v_guest := claimius.upsert_guest_user(p_app_id, p_claim_id, p_actor_user_id, p_guest_user_id);

    UPDATE claimius.claim SET guest_mode = p_mode
     WHERE id = p_claim_id AND app_id = p_app_id
    RETURNING * INTO v_claim;

    RETURN v_guest || jsonb_build_object('claim', to_jsonb(v_claim) - 'txid');
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_claim_guest(UUID, UUID, UUID, UUID, TEXT) IS 'Makes a claim a guest claim: ensures its guest and the guest''s grant through upsert_guest_user and sets guest_mode to open, code or off. off leaves the guest and its grant in place and only stops open and code resolution. Raises P0404 for a claim outside the app, P0403 when the actor does not own it and 22023 for an unknown mode. Returns claim, samna_user and user_claim.';

CREATE OR REPLACE FUNCTION claimius.insert_code(
    p_app_id        UUID,
    p_actor_user_id UUID,
    p_claim_id      UUID,
    p_hash          TEXT,
    p_kind          TEXT,
    p_object_id     UUID DEFAULT NULL,
    p_object_type   TEXT DEFAULT NULL,
    p_data          JSONB DEFAULT NULL,
    p_starts_at     TIMESTAMPTZ DEFAULT NULL,
    p_ends_at       TIMESTAMPTZ DEFAULT NULL,
    p_count         INTEGER DEFAULT 0
) RETURNS JSONB AS $$
DECLARE
    v_actor_uc UUID;
    v_claim    claimius.claim;
    v_row      claimius.code;
BEGIN
    IF p_hash IS NULL OR p_hash !~ '^[0-9a-f]{64}$' THEN
        RAISE EXCEPTION 'insert_code: the hash must be 64 lowercase hex characters' USING ERRCODE = '22023';
    END IF;
    IF p_starts_at IS NOT NULL AND p_ends_at IS NOT NULL AND p_ends_at <= p_starts_at THEN
        RAISE EXCEPTION 'insert_code: ends_at must follow starts_at' USING ERRCODE = '22023';
    END IF;
    v_actor_uc := claimius._check_code_owner(p_app_id, p_claim_id, p_actor_user_id);

    SELECT * INTO v_claim FROM claimius.claim WHERE id = p_claim_id;
    IF p_kind = 'guest' AND v_claim.guest_mode = 'off' THEN
        RAISE EXCEPTION 'insert_code: claim % is not a guest claim', p_claim_id USING ERRCODE = '22023';
    END IF;

    INSERT INTO claimius.code (app_id, hash, kind, claim_id, object_id, object_type, data,
                               starts_at, ends_at, count, sa_owner_id, sa_created_by)
    VALUES (p_app_id, p_hash, p_kind, p_claim_id, p_object_id, p_object_type, coalesce(p_data, '{}'::JSONB),
            p_starts_at, p_ends_at, coalesce(p_count, 0), v_claim.sa_owner_id, v_actor_uc)
    RETURNING * INTO v_row;

    PERFORM claimius.insert_audit(p_app_id, 'insert code', 'create', 'claimius.claim', v_row.claim_id,
                                  v_claim.sa_owner_id, v_actor_uc,
                                  format('%s code %s on claim %s', p_kind, v_row.id, p_claim_id));

    RETURN jsonb_build_object('code', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.insert_code(UUID, UUID, UUID, TEXT, TEXT, UUID, TEXT, JSONB, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER) IS 'Stores an access code of a claim by the SHA-256 of its value, never the value, made under the grant by which the actor owns the claim. A guest code needs a claim whose guest_mode is not off. Raises P0404 for a claim outside the app, P0403 when the actor does not own it and 22023 for a malformed hash, window or kind. Returns code.';

CREATE OR REPLACE FUNCTION claimius.delete_code(
    p_app_id        UUID,
    p_actor_user_id UUID,
    p_code_id       UUID
) RETURNS JSONB AS $$
DECLARE
    v_row      claimius.code;
    v_actor_uc UUID;
BEGIN
    SELECT * INTO v_row FROM claimius.code
     WHERE id = p_code_id AND app_id = p_app_id AND sa_deleted_at IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'code % not found in the app', p_code_id USING ERRCODE = 'P0404';
    END IF;
    v_actor_uc := claimius._check_code_owner(p_app_id, v_row.claim_id, p_actor_user_id);

    UPDATE claimius.code SET revoked_at = coalesce(revoked_at, now())
     WHERE id = p_code_id
    RETURNING * INTO v_row;

    PERFORM claimius.insert_audit(p_app_id, 'revoke code', 'delete', 'claimius.claim', v_row.claim_id,
                                  v_row.sa_owner_id, v_actor_uc,
                                  format('%s code %s on claim %s', v_row.kind, v_row.id, v_row.claim_id));

    RETURN jsonb_build_object('code', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_code(UUID, UUID, UUID) IS 'Revokes a code: sets revoked_at, after which it never resolves, on the prophet and on every disciple once the feed carries the row. The row stays for the owners of the claim to list. Raises P0404 for an unknown code and P0403 when the actor does not own its claim. Returns code.';

CREATE OR REPLACE FUNCTION claimius.get_codes(
    p_app_id   UUID,
    p_claim_id UUID
) RETURNS SETOF claimius.code AS $$
    SELECT *
    FROM claimius.code
    WHERE (p_app_id IS NULL OR app_id = p_app_id)
      AND (p_claim_id IS NULL OR claim_id = p_claim_id)
      AND sa_deleted_at IS NULL
    ORDER BY sa_created_at DESC, id
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_codes(UUID, UUID) IS 'The codes of one claim of the app, revoked ones included, newest first; a NULL claim lists every code of the app and a NULL app every code of the claim, or of the prophet. The caller checks the reader owns each claim.';

CREATE OR REPLACE FUNCTION claimius.get_code(
    p_app_id UUID,
    p_hash   TEXT
) RETURNS JSONB AS $$
    SELECT to_jsonb(c) || jsonb_build_object(
               'guest_mode', cl.guest_mode,
               'guest_user_id', g.user_id,
               'guest_user_claim_id', g.id)
    FROM claimius.code c
    JOIN claimius.claim cl ON cl.id = c.claim_id AND cl.sa_deleted_at IS NULL
    LEFT JOIN LATERAL (
        SELECT uc.id, uc.user_id
          FROM claimius.user_claim uc
          JOIN claimius.samna_user u ON u.user_id = uc.user_id AND u.app_id = uc.app_id
         WHERE uc.claim_id = c.claim_id
                 AND uc.app_id = c.app_id
           AND uc.sa_deleted_at IS NULL
           AND u.type = 'guest'
           AND u.sa_deleted_at IS NULL
         ORDER BY uc.sa_created_at
         LIMIT 1
    ) g ON TRUE
    WHERE (p_app_id IS NULL OR c.app_id = p_app_id)
      AND c.hash = p_hash
      AND c.sa_deleted_at IS NULL
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_code(UUID, TEXT) IS 'The code whose value hashes to p_hash, in the app or in any app when p_app_id is NULL, as JSON with its app_id, with guest_mode, the mode of its claim, and guest_user_id and guest_user_claim_id, the guest holding the claim and its grant, or NULL. The window, the count and revoked_at are the caller''s to check; update_code_used and redeem_code check them again atomically.';

CREATE OR REPLACE FUNCTION claimius.update_code_used(
    p_app_id UUID,
    p_hash   TEXT
) RETURNS SETOF claimius.code AS $$
    UPDATE claimius.code
       SET used_count = used_count + 1
     WHERE app_id = p_app_id
       AND hash = p_hash
       AND sa_deleted_at IS NULL
       AND revoked_at IS NULL
       AND (starts_at IS NULL OR starts_at <= now())
       AND (ends_at IS NULL OR ends_at > now())
       AND (count = 0 OR used_count < count)
    RETURNING *
$$ LANGUAGE sql;

COMMENT ON FUNCTION claimius.update_code_used(UUID, TEXT) IS 'Takes one use of a live code in one statement: no row when the code is unknown, revoked, outside its window or used up, so two concurrent uses of the last one never both pass.';

CREATE OR REPLACE FUNCTION claimius.redeem_code(
    p_app_id  UUID,
    p_hash    TEXT,
    p_user_id UUID
) RETURNS JSONB AS $$
DECLARE
    v_code  claimius.code;
    v_grant claimius.user_claim;
BEGIN
    SELECT * INTO v_code FROM claimius.code
     WHERE (p_app_id IS NULL OR app_id = p_app_id) AND hash = p_hash AND sa_deleted_at IS NULL
     FOR UPDATE;
    IF NOT FOUND OR v_code.kind <> 'invite' THEN
        RAISE EXCEPTION 'redeem_code: no invite code' USING ERRCODE = 'P0404';
    END IF;

    SELECT * INTO v_grant FROM claimius.user_claim
     WHERE app_id = v_code.app_id AND claim_id = v_code.claim_id AND user_id = p_user_id AND sa_deleted_at IS NULL
     LIMIT 1;
    IF FOUND THEN
        RETURN jsonb_build_object('code', to_jsonb(v_code), 'user_claim', to_jsonb(v_grant) - 'txid');
    END IF;

    UPDATE claimius.code
       SET used_count = used_count + 1
     WHERE id = v_code.id
       AND revoked_at IS NULL
       AND (starts_at IS NULL OR starts_at <= now())
       AND (ends_at IS NULL OR ends_at > now())
       AND (count = 0 OR used_count < count)
    RETURNING * INTO v_code;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'redeem_code: the code is revoked, outside its window or used up' USING ERRCODE = 'P0403';
    END IF;

    v_grant := jsonb_populate_record(NULL::claimius.user_claim, claimius.insert_user_claim(
        p_app_id        => v_code.app_id,
        p_claim_id      => v_code.claim_id,
        p_user_id       => p_user_id,
        p_sa_owner_id   => v_code.sa_owner_id,
        p_sa_created_by => v_code.sa_created_by,
        p_reason        => format('invite code %s', v_code.id),
        p_starts_at     => v_code.starts_at,
        p_ends_at       => v_code.ends_at
    ) -> 'user_claim');

    PERFORM claimius.insert_audit(v_code.app_id, 'redeem code', 'update', 'claimius.claim', v_code.claim_id,
                                  v_code.sa_owner_id, v_grant.id,
                                  format('invite code %s on claim %s redeemed by %s', v_code.id, v_code.claim_id, p_user_id));

    RETURN jsonb_build_object('code', to_jsonb(v_code), 'user_claim', to_jsonb(v_grant) - 'txid');
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.redeem_code(UUID, TEXT, UUID) IS 'Redeems an invite code for a signed in user in one transaction, in the app or in any app when p_app_id is NULL: locks the code, takes one use only when it is live and not used up, and grants its claim to the user under the creator''s grant, the code window as the grant window, with an audit row naming the code id, never the value. A user already holding the claim gets the grant back and spends no use. Raises P0404 for no invite code and P0403 for one revoked, outside its window or used up. Returns code and user_claim.';

CREATE OR REPLACE FUNCTION claimius.prune(p_settings JSONB)
    RETURNS BIGINT AS $$
DECLARE
    v_row     RECORD;
    v_n       BIGINT;
    v_deleted BIGINT := 0;
BEGIN
    FOR v_row IN
        SELECT t.table_name, t.column_name,
               coalesce((p_settings ->> (t.table_name || '_days'))::INTEGER, (p_settings ->> (t.kind || '_days'))::INTEGER, t.days) AS days,
               coalesce((p_settings ->> (t.kind || '_size_mb'))::BIGINT, (p_settings ->> 'size_mb')::BIGINT, 4096) * 1024 * 1024 AS limit_bytes
        FROM (VALUES
            ('audit',             'sa_created_at', 'audit',        730),
            ('analytics_event',   'occurred_at',   'analytics',    365),
            ('analytics_session', 'last_seen_at',  'analytics',    365),
            ('analytics_capture', 'captured_at',   'analytics',    365),
            ('analytics_heat',    'day',           'analytics',    365),
            ('notification',      'closed_at',     'notification', 365),
            ('notification',      'sa_deleted_at', 'notification', 365),
            ('session',           'expires_at',    'token',          7),
            ('dpop_jti',          'expires_at',    'token',          7),
            ('jwks_cache',        'expires_at',    'token',          7),
            ('par_request',       'expires_at',    'token',          7),
            ('rate_limit',        'tat',           'token',          7),
            ('code',              'ends_at',       'token',          7),
            ('code',              'revoked_at',    'token',          7),
            ('consent',           'revoked_at',    'log',          180),
            ('peer_collision',    'sa_created_at', 'log',          180)
        ) AS t (table_name, column_name, kind, days)
    LOOP
        CONTINUE WHEN v_row.days <= 0 OR pg_total_relation_size(('claimius.' || v_row.table_name)::REGCLASS) < v_row.limit_bytes;
        EXECUTE format('DELETE FROM claimius.%I WHERE %I < now() - make_interval(days => $1)', v_row.table_name, v_row.column_name)
            USING v_row.days;
        GET DIAGNOSTICS v_n = ROW_COUNT;
        v_deleted := v_deleted + v_n;
    END LOOP;
    IF to_regproc('public.prune') IS NOT NULL THEN
        EXECUTE 'SELECT public.prune($1)' INTO v_n USING p_settings;
        v_deleted := v_deleted + coalesce(v_n, 0);
    END IF;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.prune(JSONB) IS 'Retention. Every runtime table belongs to a kind, audit, analytics, notification, token or log. p_settings sets days per table (<table>_days) or per kind (<kind>_days), and a size per kind (<kind>_size_mb) or for all (size_mb, 4096). A table under its size is left alone; over it, every row whose time column is older than its days is deleted, and 0 days leaves a table alone. A host with runtime tables of its own defines public.prune(JSONB) of the same shape, and it runs after.';

CREATE OR REPLACE FUNCTION claimius.rate_limit_gcra(
    p_key    text,
    p_rate   double precision,
    p_burst  double precision,
    p_period double precision,
    p_cost   double precision
) RETURNS TABLE (
    allowed     boolean,
    remaining   integer,
    retry_after double precision,
    reset_after double precision
) AS $$
DECLARE
    v_emission     double precision := p_period / p_rate;
    v_increment    double precision := (p_period / p_rate) * p_cost;
    v_burst_offset double precision := (p_period / p_rate) * p_burst;
    v_now          double precision := extract(epoch from clock_timestamp());
    v_tat          double precision;
    v_new_tat      double precision;
    v_allow_at     double precision;
    v_diff         double precision;
    v_remaining    double precision;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext(p_key));

    SELECT extract(epoch from tat) INTO v_tat
    FROM claimius.rate_limit WHERE key = p_key;

    IF v_tat IS NULL THEN
        v_tat := v_now;
    END IF;
    v_tat := greatest(v_tat, v_now);

    v_new_tat := v_tat + v_increment;
    v_allow_at := v_new_tat - v_burst_offset;
    v_diff := v_now - v_allow_at;
    v_remaining := floor(v_diff / v_emission);

    IF v_remaining < 0 THEN
        allowed := false;
        remaining := 0;
        retry_after := -v_diff;
        reset_after := v_tat - v_now;
        RETURN NEXT;
        RETURN;
    END IF;

    reset_after := v_new_tat - v_now;
    IF reset_after > 0 THEN
        INSERT INTO claimius.rate_limit (key, tat)
        VALUES (p_key, to_timestamp(v_new_tat))
        ON CONFLICT (key) DO UPDATE SET tat = to_timestamp(v_new_tat);
    END IF;

    allowed := true;
    remaining := v_remaining::integer;
    retry_after := -1;
    RETURN NEXT;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.rate_limit_gcra(text, double precision, double precision, double precision, double precision) IS 'Generic cell rate limiter over the unlogged rate_limit table, serialized per key: advances the theoretical arrival time of p_key by p_cost at p_rate per p_period with a burst of p_burst, and returns whether the request is allowed, the requests remaining, and the seconds to retry and to reset.';

CREATE OR REPLACE FUNCTION claimius.get_sync_seq(p_app_id UUID)
    RETURNS TABLE (txid BIGINT, seq BIGINT) AS $$
    SELECT coalesce(max(h.txid), 0), coalesce(max(h.seq), 0)
    FROM (
        SELECT e.txid::TEXT::BIGINT AS txid, e.seq
        FROM claimius.sync_event e
        WHERE e.app_id = p_app_id
        ORDER BY e.txid DESC, e.seq DESC
        LIMIT 1
    ) h;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_sync_seq(UUID) IS 'Highest (txid, seq) position written for one app. A routed write answers with it, and the disciple that routed the write reads its own write once its cursor reaches it.';

CREATE OR REPLACE FUNCTION claimius.check_sync_cursor(p_app_id UUID, p_txid BIGINT, p_seq BIGINT)
    RETURNS TEXT AS $$
DECLARE
    v_issued    BIGINT;
    v_trimmed   RECORD;
    v_events    BIGINT;
    v_rows      BIGINT;
    v_txid      XID8 := p_txid::TEXT::XID8;
BEGIN
    v_issued := coalesce(pg_sequence_last_value(pg_get_serial_sequence('claimius.sync_event', 'seq')::REGCLASS), 0);
    IF p_seq > v_issued OR v_txid > pg_snapshot_xmax(pg_current_snapshot()) THEN
        RETURN 'ahead';
    END IF;

    SELECT ps.trimmed_txid, ps.trimmed_seq INTO v_trimmed FROM claimius.prophet_state ps WHERE ps.app_id = p_app_id;
    IF FOUND AND (v_txid, p_seq) < (v_trimmed.trimmed_txid, v_trimmed.trimmed_seq) THEN
        RETURN 'trimmed';
    END IF;

    SELECT count(*) INTO v_events
    FROM (SELECT 1 FROM claimius.sync_event e WHERE e.app_id = p_app_id AND (e.txid, e.seq) > (v_txid, p_seq) LIMIT 1001) b;
    IF v_events = 0 THEN
        RETURN NULL;
    END IF;
    IF claimius.count_sync_source_rows(p_app_id, v_events) < v_events THEN
        RETURN 'backlog';
    END IF;
    IF v_events <= 1000 THEN
        RETURN NULL;
    END IF;

    v_rows := claimius.count_sync_source_rows(p_app_id);
    SELECT count(*) INTO v_events
    FROM (SELECT 1 FROM claimius.sync_event e WHERE e.app_id = p_app_id AND (e.txid, e.seq) > (v_txid, p_seq) LIMIT v_rows + 1) b;
    IF v_events > v_rows THEN
        RETURN 'backlog';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.check_sync_cursor(UUID, BIGINT, BIGINT) IS 'Whether a consumer at a (txid, seq) cursor must take the snapshot instead of the feed. NULL when the feed serves it; ahead when the cursor is above every sequence or transaction the prophet issued; trimmed when events after the cursor were trimmed; backlog when the app''s events after the cursor outnumber its source rows.';

CREATE OR REPLACE FUNCTION claimius.check_sync_home(p_app_id UUID, p_prophet TEXT)
    RETURNS TEXT AS $$
    SELECT CASE
        WHEN a.home_prophet IS NOT NULL THEN a.home_prophet
        WHEN coalesce(p_prophet, '') IN ('', l.slug) THEN NULL
        WHEN EXISTS (
            SELECT 1 FROM claimius.app_handover h
            WHERE h.app_id = a.id
              AND h.from_prophet = p_prophet
              AND h.to_prophet = l.slug
              AND h.status = 'accepted'
        ) THEN NULL
        ELSE p_prophet
    END
    FROM claimius.samna_app a
        CROSS JOIN (SELECT slug FROM claimius.get_app()) l
    WHERE a.id = p_app_id;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.check_sync_home(UUID, TEXT) IS 'Whether this prophet serves the feed of an app to a consumer bound to p_prophet. NULL when it does: it is the app''s home and the consumer is unbound, bound here, or bound to a prophet that handed the app over here and saw it accepted. Otherwise the slug of the prophet the consumer stays with: the app''s home, or the one it is bound to.';

CREATE OR REPLACE FUNCTION claimius.get_peer_snapshot(p_app_id UUID)
    RETURNS TABLE (event_type TEXT, payload JSONB) AS $$
    SELECT s.event_type, s.payload
    FROM (
        SELECT 1 AS step, 0 AS level, 'claimius.samna_app' AS event_type, to_jsonb(a) - 'txid' AS payload, a.sa_created_at AS created
        FROM claimius.samna_app a WHERE a.id = p_app_id
        UNION ALL
        SELECT 2, 0, 'claimius.app_handover', to_jsonb(h) - 'txid', h.sa_created_at
        FROM claimius.app_handover h WHERE h.app_id = p_app_id
        UNION ALL
        SELECT 3, o.sa_level, 'claimius.organization', to_jsonb(o) - 'txid', o.sa_created_at
        FROM claimius.organization o WHERE o.id IN (SELECT claimius.app_object_ids(p_app_id, 'claimius.organization'))
        UNION ALL
        SELECT 4, l.sa_level, 'claimius.location', to_jsonb(l) - 'txid', l.sa_created_at
        FROM claimius.location l WHERE l.id IN (SELECT claimius.app_object_ids(p_app_id, 'claimius.location'))
        UNION ALL
        SELECT 5, 0, 'claimius.samna_user', to_jsonb(u) - 'txid', u.sa_created_at
        FROM claimius.samna_user u WHERE u.app_id = p_app_id
        UNION ALL
        SELECT 6, 0, 'claimius.claim', to_jsonb(c) - 'txid', c.sa_created_at
        FROM claimius.claim c WHERE c.app_id = p_app_id
        UNION ALL
        SELECT 7, 0, 'claimius.user_claim', to_jsonb(uc) - 'txid', uc.sa_created_at
        FROM claimius.user_claim uc WHERE uc.app_id = p_app_id
        UNION ALL
        SELECT 8, 0, 'claimius.claim_object', to_jsonb(co) - 'txid', co.sa_created_at
        FROM claimius.claim_object co WHERE co.app_id = p_app_id
        UNION ALL
        SELECT 9, 0, 'claimius.samna_client', to_jsonb(sc) - 'txid', sc.sa_created_at
        FROM claimius.samna_client sc WHERE sc.app_id = p_app_id
        UNION ALL
        SELECT 10, 0, 'claimius.samna_secret', to_jsonb(ss) - 'txid', ss.sa_created_at
        FROM claimius.samna_secret ss WHERE ss.app_id = p_app_id
        UNION ALL
        SELECT 11, 0, 'claimius.object_field', to_jsonb(f) - 'txid', f.sa_created_at
        FROM claimius.object_field f WHERE f.app_id = p_app_id
        UNION ALL
        SELECT 12, 0, 'claimius.user_field', to_jsonb(uf) - 'txid', uf.sa_created_at
        FROM claimius.user_field uf WHERE uf.app_id = p_app_id
        UNION ALL
        SELECT 13, 0, 'claimius.user_relation', to_jsonb(r) - 'txid', r.sa_created_at
        FROM claimius.user_relation r WHERE r.app_id = p_app_id
        UNION ALL
        SELECT 14, 0, 'claimius.object_asset', to_jsonb(oa) - 'txid', oa.sa_created_at
        FROM claimius.object_asset oa
        WHERE p_app_id IN (SELECT claimius.apps_for_object(oa.object_type, oa.object_id, oa.app_id))
        UNION ALL
        SELECT 15, 0, 'claimius.code', to_jsonb(cd), cd.sa_created_at
        FROM claimius.code cd WHERE cd.app_id = p_app_id
    ) s
    ORDER BY s.step, s.level, s.created;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_peer_snapshot(UUID) IS 'Every source row of one app in the shape its feed events carry, for a peer prophet mirroring the app: the app row and its handovers first, parents before children, so each row applies against rows already applied. The object_asset links follow, every link apps_for_object publishes to the app, and the codes of the app come last, after the claims they open.';

CREATE OR REPLACE FUNCTION claimius.get_consent(
    p_app_id       UUID,
    p_user_id      UUID DEFAULT NULL,
    p_anonymous_id TEXT DEFAULT NULL
) RETURNS SETOF claimius.consent AS $$
    SELECT *
    FROM claimius.consent
    WHERE revoked_at IS NULL
      AND (
          (p_user_id IS NOT NULL AND user_id = p_user_id)
          OR (p_anonymous_id IS NOT NULL AND user_id IS NULL AND anonymous_id = p_anonymous_id AND app_id = p_app_id)
      )
    ORDER BY (app_id = p_app_id) DESC, granted_at DESC
    LIMIT 1;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_consent(UUID, UUID, TEXT) IS 'Strongest live consent for an identity. A known user matches across apps, preferring the asking app. An anonymous visitor matches within the app only.';

CREATE OR REPLACE FUNCTION claimius.insert_consent(
    p_app_id         UUID,
    p_anonymous_id   TEXT,
    p_categories     JSONB,
    p_policy_version TEXT,
    p_user_id        UUID DEFAULT NULL,
    p_ip             INET DEFAULT NULL,
    p_user_agent     TEXT DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row claimius.consent;
BEGIN
    UPDATE claimius.consent
    SET revoked_at = now(), sa_updated_at = now()
    WHERE app_id = p_app_id
      AND revoked_at IS NULL
      AND (
          (p_user_id IS NOT NULL AND user_id = p_user_id)
          OR (p_user_id IS NULL AND user_id IS NULL AND anonymous_id = p_anonymous_id)
      );

    INSERT INTO claimius.consent (
        app_id, user_id, anonymous_id, categories, policy_version, ip, user_agent
    ) VALUES (
        p_app_id, p_user_id, p_anonymous_id, COALESCE(p_categories, '{}'::jsonb), p_policy_version, p_ip, p_user_agent
    )
    RETURNING * INTO v_row;

    RETURN jsonb_build_object('consent', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.insert_consent(UUID, TEXT, JSONB, TEXT, UUID, INET, TEXT) IS 'Records a consent decision, revoking the previous live one for the same identity.';

CREATE OR REPLACE FUNCTION claimius.consent_allows(
    p_app_id       UUID,
    p_category     TEXT,
    p_user_id      UUID DEFAULT NULL,
    p_anonymous_id TEXT DEFAULT NULL
) RETURNS BOOLEAN AS $$
    SELECT COALESCE(
        (SELECT (categories ->> p_category)::boolean
         FROM claimius.get_consent(p_app_id, p_user_id, p_anonymous_id)),
        FALSE
    );
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.consent_allows(UUID, TEXT, UUID, TEXT) IS 'True when the identity granted one category. Absent consent is a denial.';

CREATE OR REPLACE FUNCTION claimius.upsert_analytics_session(
    p_app_id       UUID,
    p_anonymous_id TEXT,
    p_session_key  TEXT,
    p_context      JSONB DEFAULT '{}'::jsonb,
    p_user_id      UUID DEFAULT NULL,
    p_ip           INET DEFAULT NULL,
    p_user_agent   TEXT DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row claimius.analytics_session;
BEGIN
    IF NOT claimius.consent_allows(p_app_id, 'analytics', p_user_id, p_anonymous_id) THEN
        RETURN jsonb_build_object('analytics_session', NULL);
    END IF;

    INSERT INTO claimius.analytics_session (
        app_id, user_id, anonymous_id, session_key, entry_path, referrer,
        user_agent, ip, language, timezone, platform,
        screen_width, screen_height, device_pixel_ratio, context
    ) VALUES (
        p_app_id, p_user_id, p_anonymous_id, p_session_key,
        p_context ->> 'entry_path',
        p_context ->> 'referrer',
        p_user_agent,
        p_ip,
        p_context ->> 'language',
        p_context ->> 'timezone',
        p_context ->> 'platform',
        (p_context ->> 'screen_width')::integer,
        (p_context ->> 'screen_height')::integer,
        (p_context ->> 'device_pixel_ratio')::real,
        p_context
    )
    ON CONFLICT (app_id, session_key) DO UPDATE
        SET last_seen_at = now(),
            user_id      = COALESCE(EXCLUDED.user_id, claimius.analytics_session.user_id)
    RETURNING * INTO v_row;

    RETURN jsonb_build_object('analytics_session', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_analytics_session(UUID, TEXT, TEXT, JSONB, UUID, INET, TEXT) IS 'Opens or touches the session row for a visit. Returns a null body when the analytics category is not granted.';

CREATE OR REPLACE FUNCTION claimius.insert_analytics_events(
    p_app_id      UUID,
    p_session_key TEXT,
    p_events      JSONB
) RETURNS INTEGER AS $$
DECLARE
    v_session claimius.analytics_session;
    v_written INTEGER;
BEGIN
    SELECT * INTO v_session
    FROM claimius.analytics_session
    WHERE app_id = p_app_id AND session_key = p_session_key;

    IF NOT FOUND THEN
        RETURN 0;
    END IF;

    IF NOT claimius.consent_allows(p_app_id, 'analytics', v_session.user_id, v_session.anonymous_id) THEN
        RETURN 0;
    END IF;

    INSERT INTO claimius.analytics_event (app_id, session_id, user_id, type, path, payload, occurred_at)
    SELECT p_app_id,
           v_session.id,
           v_session.user_id,
           e ->> 'type',
           COALESCE(e ->> 'path', ''),
           COALESCE(e -> 'payload', '{}'::jsonb),
           least(greatest(coalesce(to_timestamp((e ->> 'occurred_at')::double precision / 1000.0), now()), now() - INTERVAL '1 day'), now())
    FROM jsonb_array_elements(p_events) AS e
    WHERE e ->> 'type' IS NOT NULL;

    GET DIAGNOSTICS v_written = ROW_COUNT;

    UPDATE claimius.analytics_session
    SET last_seen_at = now()
    WHERE id = v_session.id;

    RETURN v_written;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.insert_analytics_events(UUID, TEXT, JSONB) IS 'Writes a batch against an open session, each occurred_at clamped into the day before now so a browser clock cannot place a signal outside the days the fold and the retention cover. Returns 0 and writes nothing when consent is absent or the session is unknown.';

CREATE OR REPLACE FUNCTION claimius.capture_user_apps(p_user_id UUID)
    RETURNS SETOF UUID AS $$
    WITH granted AS (
        SELECT c.app_id, co.scope
        FROM claimius.user_claim uc
            JOIN claimius.claim c ON c.id = uc.claim_id
            JOIN claimius.claim_object co ON co.claim_id = c.id
        WHERE uc.user_id = p_user_id
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
          AND co.sa_deleted_at IS NULL
          AND (uc.starts_at IS NULL OR uc.starts_at <= now())
          AND (uc.ends_at IS NULL OR uc.ends_at > now())
          AND co.object_id IS NULL
          AND jsonb_path_exists(co.scope, '$.analytics'::jsonpath)
          AND (claimius._effective_access(c.sa_access, co.sa_access) & 16) = 0
          AND (claimius._effective_access(c.sa_access, co.sa_access) & 4) = 4
    )
    SELECT app_id FROM granted
    UNION
    SELECT named::uuid
    FROM granted, jsonb_array_elements_text(
        CASE WHEN jsonb_typeof(scope -> 'analytics' -> 'app') = 'array'
             THEN scope -> 'analytics' -> 'app'
             ELSE '[]'::jsonb END) AS named
    WHERE named ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$';
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.capture_user_apps(UUID) IS 'Producer apps whose capture one person may read: the app each live analytics grant sits in, plus every app id named in that grant''s scope.';

CREATE OR REPLACE FUNCTION claimius.analytics_enabled(p_app_id UUID)
    RETURNS BOOLEAN AS $$
    SELECT EXISTS (
        SELECT 1
        FROM claimius.user_claim uc
            JOIN claimius.claim c ON c.id = uc.claim_id
            JOIN claimius.claim_object co ON co.claim_id = uc.claim_id
        WHERE c.app_id = p_app_id
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
          AND co.sa_deleted_at IS NULL
          AND (uc.starts_at IS NULL OR uc.starts_at <= now())
          AND (uc.ends_at IS NULL OR uc.ends_at > now())
          AND co.object_id IS NULL
          AND jsonb_path_exists(co.scope, '$.analytics'::jsonpath)
          AND (claimius._effective_access(c.sa_access, co.sa_access) & 16) = 0
    );
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.analytics_enabled(UUID) IS 'True when the app holds a live object-less grant carrying the analytics scope. A NULL claim_object access inherits the claim mask.';

CREATE OR REPLACE FUNCTION claimius.get_analytics_events(
    p_user_id      UUID,
    p_producer_app UUID DEFAULT NULL,
    p_session_id   UUID DEFAULT NULL,
    p_visitor      UUID DEFAULT NULL,
    p_type         TEXT DEFAULT NULL,
    p_path         TEXT DEFAULT NULL,
    p_from         TIMESTAMPTZ DEFAULT NULL,
    p_to           TIMESTAMPTZ DEFAULT NULL,
    p_limit        INTEGER DEFAULT 100,
    p_offset       INTEGER DEFAULT 0
) RETURNS SETOF claimius.analytics_event AS $$
    SELECT e.*
    FROM claimius.analytics_event e
    WHERE e.app_id IN (SELECT claimius.capture_user_apps(p_user_id))
      AND (p_producer_app IS NULL OR e.app_id = p_producer_app)
      AND (p_session_id IS NULL OR e.session_id = p_session_id)
      AND (p_visitor IS NULL OR e.user_id = p_visitor)
      AND (p_type IS NULL OR e.type = p_type)
      AND (p_path IS NULL OR e.path = p_path)
      AND (p_from IS NULL OR e.occurred_at >= p_from)
      AND (p_to IS NULL OR e.occurred_at < p_to)
    ORDER BY e.occurred_at DESC
    LIMIT p_limit OFFSET p_offset;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_events(UUID, UUID, UUID, UUID, TEXT, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER) IS 'Capture read for one person, across every producer app their analytics grant covers. app_id names the producer and never decides access.';

CREATE OR REPLACE FUNCTION claimius.get_analytics_sessions(
    p_user_id      UUID,
    p_producer_app UUID DEFAULT NULL,
    p_visitor      UUID DEFAULT NULL,
    p_anonymous_id TEXT DEFAULT NULL,
    p_from         TIMESTAMPTZ DEFAULT NULL,
    p_to           TIMESTAMPTZ DEFAULT NULL,
    p_limit        INTEGER DEFAULT 100,
    p_offset       INTEGER DEFAULT 0
) RETURNS SETOF claimius.analytics_session AS $$
    SELECT s.*
    FROM claimius.analytics_session s
    WHERE s.app_id IN (SELECT claimius.capture_user_apps(p_user_id))
      AND (p_producer_app IS NULL OR s.app_id = p_producer_app)
      AND (p_visitor IS NULL OR s.user_id = p_visitor)
      AND (p_anonymous_id IS NULL OR s.anonymous_id = p_anonymous_id)
      AND (p_from IS NULL OR s.started_at >= p_from)
      AND (p_to IS NULL OR s.started_at < p_to)
    ORDER BY s.started_at DESC
    LIMIT p_limit OFFSET p_offset;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_sessions(UUID, UUID, UUID, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER) IS 'Visit read for one person, scoped exactly like get_analytics_events.';

CREATE OR REPLACE FUNCTION claimius.get_analytics_apps(p_user_id UUID)
    RETURNS TABLE (
        id        UUID,
        slug      TEXT,
        name      TEXT,
        app_image TEXT,
        origin    TEXT,
        sessions  BIGINT,
        events    BIGINT,
        visitors  BIGINT,
        last_seen TIMESTAMPTZ
    ) AS $$
    SELECT a.id,
           a.slug,
           a.name,
           a.app_image,
           substring(a.redirect_uris[1] FROM '^[a-zA-Z][a-zA-Z0-9+.-]*://[^/]+'),
           COUNT(DISTINCT s.id),
           COALESCE(SUM((SELECT COUNT(*) FROM claimius.analytics_event e WHERE e.session_id = s.id)), 0),
           COUNT(DISTINCT COALESCE(s.user_id::text, s.anonymous_id)),
           MAX(s.last_seen_at)
    FROM claimius.samna_app a
        LEFT JOIN claimius.analytics_session s ON s.app_id = a.id
    WHERE a.id IN (SELECT claimius.capture_user_apps(p_user_id))
      AND a.sa_deleted_at IS NULL
    GROUP BY a.id, a.slug, a.name, a.app_image, a.redirect_uris
    ORDER BY a.name;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_apps(UUID) IS 'The producer apps one person may read, named, with the volume captured against each and the origin its pages are served from. A disciple carries no samna_app replica, so this is where a capture reader learns what an app_id is and where to load the page a heat map overlays.';

CREATE OR REPLACE FUNCTION claimius.upsert_app_client(
    p_app_id        UUID,
    p_client_id     UUID,
    p_secret_hash   TEXT,
    p_name          TEXT,
    p_claim_name    TEXT,
    p_sa_access     INTEGER,
    p_sa_created_by UUID,
    p_description   TEXT DEFAULT '',
    p_scope         JSONB DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_org_id     UUID;
    v_claim_id   UUID;
    v_client_pk  UUID;
BEGIN
    SELECT sa_owner_id INTO v_org_id
    FROM claimius.samna_app
    WHERE id = p_app_id AND sa_deleted_at IS NULL;

    IF v_org_id IS NULL THEN
        RAISE EXCEPTION 'app % not found', p_app_id;
    END IF;

    SELECT id INTO v_claim_id
    FROM claimius.claim
    WHERE app_id = p_app_id AND name = p_claim_name AND sa_deleted_at IS NULL
    LIMIT 1;

    IF v_claim_id IS NULL THEN
        v_claim_id := (claimius.insert_claim(
            p_app_id        => p_app_id,
            p_name          => p_claim_name,
            p_description   => p_description,
            p_sa_access     => p_sa_access,
            p_sa_owner_id   => v_org_id,
            p_sa_root_id    => v_org_id,
            p_sa_created_by => p_sa_created_by,
            p_inherits      => TRUE,
            p_type          => 'service'
        ) -> 'claim' ->> 'id')::UUID;
    END IF;

    IF p_scope IS NOT NULL THEN
        PERFORM claimius.insert_claim_object(
            p_app_id        => p_app_id,
            p_claim_id      => v_claim_id,
            p_sa_owner_id   => v_org_id,
            p_sa_root_id    => v_org_id,
            p_sa_created_by => p_sa_created_by,
            p_sa_access     => p_sa_access,
            p_inherits      => FALSE,
            p_reason        => p_claim_name || ' capability grant',
            p_scope         => p_scope
        );
    END IF;

    SELECT id INTO v_client_pk
    FROM claimius.samna_client
    WHERE client_id = p_client_id AND sa_deleted_at IS NULL
    LIMIT 1;

    IF v_client_pk IS NULL THEN
        v_client_pk := gen_random_uuid();
        INSERT INTO claimius.samna_client (
            id, name, description, app_id, client_id, secret_hash, status,
            sa_owner_id, sa_created_by
        ) VALUES (
            v_client_pk, p_name, p_description, p_app_id, p_client_id, p_secret_hash,
            'active', v_org_id, p_sa_created_by
        );
    ELSE
        UPDATE claimius.samna_client
        SET name = p_name, description = p_description, secret_hash = p_secret_hash,
            sa_updated_at = now()
        WHERE id = v_client_pk;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM claimius.claim_object
        WHERE app_id = p_app_id AND claim_id = v_claim_id
          AND object_type = 'claimius.samna_client' AND object_id = v_client_pk
          AND sa_deleted_at IS NULL
    ) THEN
        PERFORM claimius.insert_claim_object(
            p_app_id        => p_app_id,
            p_claim_id      => v_claim_id,
            p_object_id     => v_client_pk,
            p_object_type   => 'claimius.samna_client',
            p_sa_owner_id   => v_org_id,
            p_sa_root_id    => v_org_id,
            p_sa_created_by => p_sa_created_by,
            p_inherits      => FALSE,
            p_reason        => p_claim_name || ' binds to the client row'
        );
    END IF;

    PERFORM claimius.upsert_app_user(
        p_user_id    => p_client_id,
        p_app_id     => p_app_id,
        p_first_name => p_name,
        p_last_name  => 'Client',
        p_email      => p_name || '@local',
        p_sa_created_by => p_sa_created_by
    );

    IF NOT EXISTS (
        SELECT 1 FROM claimius.user_claim
        WHERE app_id = p_app_id AND claim_id = v_claim_id AND user_id = p_client_id
          AND sa_deleted_at IS NULL
    ) THEN
        PERFORM claimius.insert_user_claim(
            p_app_id        => p_app_id,
            p_claim_id      => v_claim_id,
            p_user_id       => p_client_id,
            p_sa_owner_id   => v_org_id,
            p_sa_created_by => p_sa_created_by,
            p_reason        => 'Grant ' || p_name || ' its capability claim'
        );
    END IF;

    RETURN jsonb_build_object(
        'samna_client', jsonb_build_object('id', v_client_pk, 'client_id', p_client_id),
        'claim', jsonb_build_object('id', v_claim_id, 'name', p_claim_name),
        'organization', jsonb_build_object('id', v_org_id)
    );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_app_client(uuid, uuid, text, text, text, integer, uuid, text, jsonb) IS 'Idempotently provisions a machine client for an app with one capability claim. p_sa_access is a bitwise mask. p_scope, when given, is bound as an object-less capability payload the consuming service reads through get_access on a scope key, and it is where per operation limits live.';

CREATE OR REPLACE FUNCTION claimius.analytics_bucket_size() RETURNS INTEGER AS $$
    SELECT 16;
$$ LANGUAGE sql IMMUTABLE;

COMMENT ON FUNCTION claimius.analytics_bucket_size() IS 'Cell edge in pixels at capture scale. Well under the blur radius the overlay draws with, so the rendered field is unchanged by the fold.';

CREATE OR REPLACE FUNCTION claimius.analytics_event_weight(p_type TEXT) RETURNS INTEGER AS $$
    SELECT CASE WHEN p_type = 'click' THEN 4 ELSE 1 END;
$$ LANGUAGE sql IMMUTABLE;

COMMENT ON FUNCTION claimius.analytics_event_weight(TEXT) IS 'What one signal contributes to a cell. Matches the weight the capture script and the overlay already agree on.';
CREATE OR REPLACE FUNCTION claimius.get_analytics_heat(
    p_app_id         UUID,
    p_path           TEXT,
    p_viewport_width INTEGER DEFAULT NULL,
    p_type           TEXT DEFAULT NULL,
    p_from           DATE DEFAULT NULL,
    p_to             DATE DEFAULT NULL
) RETURNS TABLE (x INTEGER, y INTEGER, weight BIGINT, sessions BIGINT) AS $$
    SELECT
        h.bucket_x * claimius.analytics_bucket_size(),
        h.bucket_y * claimius.analytics_bucket_size(),
        sum(h.weight)::bigint,
        max(h.sessions)
    FROM claimius.analytics_heat h
    WHERE h.app_id = p_app_id
      AND h.path = p_path
      AND (p_viewport_width IS NULL OR h.viewport_width = p_viewport_width)
      AND (p_type IS NULL OR h.type = p_type)
      AND (p_from IS NULL OR h.day >= p_from)
      AND (p_to IS NULL OR h.day <= p_to)
    GROUP BY 1, 2;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_heat(UUID, TEXT, INTEGER, TEXT, DATE, DATE) IS 'The heat field for a page, already in page coordinates at capture scale, so the caller passes the rows straight to the overlay.';

CREATE OR REPLACE FUNCTION claimius.delete_analytics_event(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_before DATE;
    v_rows   BIGINT;
BEGIN
    IF p_keep IS NULL OR p_keep < INTERVAL '1 day' THEN
        RAISE EXCEPTION 'delete_analytics_event keeps at least a day, got %', p_keep USING ERRCODE = '22023';
    END IF;
    v_before := ((now() - p_keep) AT TIME ZONE 'UTC')::date;

    DELETE FROM claimius.analytics_event
    WHERE (occurred_at AT TIME ZONE 'UTC')::date < v_before;
    GET DIAGNOSTICS v_rows = ROW_COUNT;
    RETURN v_rows;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_analytics_event(INTERVAL) IS 'Drops raw signal from every UTC day before the day p_keep ago, folding those days first so the heat field keeps everything it had. The cutoff is a day boundary at least a day back, because folding an unfinished day would discard the rest of it. The analytics_event retention prune.';

CREATE OR REPLACE FUNCTION claimius.upsert_analytics_heat(
    p_from   DATE,
    p_to     DATE,
    p_app_id UUID DEFAULT NULL
) RETURNS BIGINT AS $$
DECLARE
    v_cells BIGINT;
BEGIN
    INSERT INTO claimius.analytics_heat AS h (
        app_id, path, viewport_width, type, bucket_x, bucket_y, day, weight, sessions
    )
    WITH signals AS (
        SELECT
            e.app_id,
            e.path,
            (e.payload ->> 'viewport_width')::int AS viewport_width,
            e.type,
            floor((e.payload ->> 'page_x')::numeric / claimius.analytics_bucket_size())::int AS bucket_x,
            floor((e.payload ->> 'page_y')::numeric / claimius.analytics_bucket_size())::int AS bucket_y,
            (e.occurred_at AT TIME ZONE 'UTC')::date AS day,
            claimius.analytics_event_weight(e.type) AS weight,
            e.session_id
        FROM claimius.analytics_event e
        WHERE (p_from IS NULL OR (e.occurred_at AT TIME ZONE 'UTC')::date >= p_from)
          AND (p_to IS NULL OR (e.occurred_at AT TIME ZONE 'UTC')::date <= p_to)
          AND (p_app_id IS NULL OR e.app_id = p_app_id)
          AND e.type <> 'heat'
          AND e.payload ? 'page_x'
          AND e.payload ? 'page_y'
          AND e.payload ? 'viewport_width'
        UNION ALL
        SELECT
            e.app_id,
            e.path,
            (e.payload ->> 'viewport_width')::int,
            COALESCE(e.payload ->> 'source', 'pointer'),
            (c ->> 0)::int,
            (c ->> 1)::int,
            (e.occurred_at AT TIME ZONE 'UTC')::date,
            (c ->> 2)::int * claimius.analytics_event_weight(COALESCE(e.payload ->> 'source', 'pointer')),
            e.session_id
        FROM claimius.analytics_event e
            CROSS JOIN LATERAL jsonb_array_elements(e.payload -> 'cells') AS c
        WHERE (p_from IS NULL OR (e.occurred_at AT TIME ZONE 'UTC')::date >= p_from)
          AND (p_to IS NULL OR (e.occurred_at AT TIME ZONE 'UTC')::date <= p_to)
          AND (p_app_id IS NULL OR e.app_id = p_app_id)
          AND e.type = 'heat'
          AND jsonb_typeof(e.payload -> 'cells') = 'array'
          AND e.payload ? 'viewport_width'
    )
    SELECT
        s.app_id,
        s.path,
        s.viewport_width,
        s.type,
        s.bucket_x,
        s.bucket_y,
        s.day,
        sum(s.weight),
        count(DISTINCT s.session_id)
    FROM signals s
    GROUP BY 1, 2, 3, 4, 5, 6, 7
    ON CONFLICT (app_id, path, viewport_width, type, day, bucket_x, bucket_y) DO UPDATE SET
        weight   = EXCLUDED.weight,
        sessions = EXCLUDED.sessions
    WHERE h.weight IS DISTINCT FROM EXCLUDED.weight
       OR h.sessions IS DISTINCT FROM EXCLUDED.sessions;

    GET DIAGNOSTICS v_cells = ROW_COUNT;
    RETURN v_cells;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_analytics_heat(DATE, DATE, UUID) IS 'Folds every positioned signal and every browser folded heat event on a range of whole days into its cell and returns the cells written. A day is the unit because a cell is replaced rather than added to.';

CREATE OR REPLACE FUNCTION claimius.get_analytics_capture(
    p_app_id UUID,
    p_path   TEXT
) RETURNS SETOF claimius.analytics_capture AS $$
    SELECT *
    FROM claimius.analytics_capture
    WHERE app_id = p_app_id
      AND path = p_path;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_capture(UUID, TEXT) IS 'The snapshot held for a page.';

CREATE OR REPLACE FUNCTION claimius.get_analytics_captured(
    p_app_id  UUID,
    p_max_age INTERVAL DEFAULT '7 days'
) RETURNS SETOF TEXT AS $$
    SELECT path
    FROM claimius.analytics_capture
    WHERE app_id = p_app_id
      AND captured_at > now() - p_max_age
    ORDER BY path;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_captured(UUID, INTERVAL) IS 'The paths an app holds a fresh snapshot for. The consent answer carries the list so a browser knows which pages need no capture without asking per view.';

CREATE OR REPLACE FUNCTION claimius.get_analytics_summary(
    p_user_id      UUID,
    p_producer_app UUID,
    p_from         TIMESTAMPTZ DEFAULT NULL,
    p_to           TIMESTAMPTZ DEFAULT NULL
) RETURNS JSONB AS $$
    WITH visits AS (
        SELECT s.id,
               COALESCE(s.user_id::text, s.anonymous_id) AS actor,
               s.user_id,
               s.anonymous_id,
               s.ip,
               s.user_agent,
               s.started_at
        FROM claimius.analytics_session s
        WHERE s.app_id = p_producer_app
          AND p_producer_app IN (SELECT claimius.capture_user_apps(p_user_id))
          AND (p_from IS NULL OR s.started_at >= p_from)
          AND (p_to IS NULL OR s.started_at < p_to)
    ),
    signals AS (
        SELECT e.session_id,
               e.type,
               e.path,
               (e.payload ->> 'viewport_width')::int AS viewport_width
        FROM claimius.analytics_event e
            JOIN visits v ON v.id = e.session_id
        WHERE e.type <> 'heat'
    ),
    actor_paths AS (
        SELECT v.actor,
               g.path,
               row_number() OVER (PARTITION BY v.actor ORDER BY count(*) DESC, g.path) AS rank
        FROM signals g
            JOIN visits v ON v.id = g.session_id
        GROUP BY v.actor, g.path
    ),
    actors AS (
        SELECT v.actor AS id,
               max(v.user_id::text) AS user_id,
               max(v.anonymous_id) AS anonymous_id,
               max(v.ip::text) AS ip,
               max(v.user_agent) AS user_agent,
               count(DISTINCT v.id) AS sessions,
               count(g.session_id) AS events,
               max(v.started_at) AS last_seen
        FROM visits v
            LEFT JOIN signals g ON g.session_id = v.id
        GROUP BY v.actor
    ),
    paths AS (
        SELECT g.path,
               count(*) AS events,
               count(DISTINCT g.session_id) AS sessions,
               mode() WITHIN GROUP (ORDER BY g.viewport_width) AS viewport_width
        FROM signals g
        GROUP BY g.path
    )
    SELECT jsonb_build_object(
        'actors', COALESCE((
            SELECT jsonb_agg(jsonb_build_object(
                'id', a.id,
                'user_id', a.user_id,
                'anonymous_id', a.anonymous_id,
                'ip', a.ip,
                'user_agent', a.user_agent,
                'sessions', a.sessions,
                'events', a.events,
                'last_seen', a.last_seen,
                'path', (SELECT ap.path FROM actor_paths ap WHERE ap.actor = a.id AND ap.rank = 1)
            ) ORDER BY a.events DESC, a.last_seen DESC)
            FROM actors a
        ), '[]'::jsonb),
        'paths', COALESCE((
            SELECT jsonb_agg(jsonb_build_object(
                'path', p.path,
                'events', p.events,
                'sessions', p.sessions,
                'viewport_width', p.viewport_width
            ) ORDER BY p.events DESC, p.path)
            FROM paths p
        ), '[]'::jsonb),
        'types', COALESCE((
            SELECT jsonb_agg(DISTINCT g.type)
            FROM signals g
        ), '[]'::jsonb)
    );
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_summary(UUID, UUID, TIMESTAMPTZ, TIMESTAMPTZ) IS 'Who visited a producer app, which paths they touched and at what width, and which signal kinds exist, for one reader, scoped exactly like get_analytics_events.';

CREATE OR REPLACE FUNCTION claimius.upsert_analytics_capture(
    p_app_id       UUID,
    p_path         TEXT,
    p_content_type TEXT,
    p_body         BYTEA
) RETURNS claimius.analytics_capture AS $$
    INSERT INTO claimius.analytics_capture (app_id, path, content_type, body)
    VALUES (p_app_id, p_path, p_content_type, p_body)
    ON CONFLICT (app_id, path) DO UPDATE SET
        content_type = EXCLUDED.content_type,
        body         = EXCLUDED.body,
        captured_at  = now()
    RETURNING *;
$$ LANGUAGE sql;

COMMENT ON FUNCTION claimius.upsert_analytics_capture(UUID, TEXT, TEXT, BYTEA) IS 'Stores the snapshot of a page, replacing the one held for the same app and path.';

CREATE OR REPLACE FUNCTION claimius.get_sync_events(
    p_app_id    UUID,
    p_txid      BIGINT,
    p_seq       BIGINT,
    p_limit     INTEGER DEFAULT 1000,
    p_peer      BOOLEAN DEFAULT FALSE
) RETURNS TABLE (
    txid        BIGINT,
    seq         BIGINT,
    operation   claimius.sync_operation,
    event_type  TEXT,
    payload     JSONB,
    written_at  TIMESTAMPTZ
) AS $$
    SELECT e.txid::TEXT::BIGINT, e.seq, e.operation, e.event_type,
           CASE WHEN NOT p_peer AND e.event_type = 'claimius.organization' THEN e.payload - 'private_seed'
                WHEN NOT p_peer AND e.event_type = 'claimius.user_identity' THEN e.payload - 'data'
                ELSE e.payload END,
           e.written_at
    FROM claimius.sync_event e
    WHERE e.app_id = p_app_id
      AND (e.txid, e.seq) > (p_txid::TEXT::XID8, p_seq)
      AND (e.txid < pg_snapshot_xmin(pg_current_snapshot()) OR e.txid = pg_current_xact_id_if_assigned())
      AND e.event_type NOT IN ('claimius.user_object', 'claimius.user_users', 'claimius.user_users_diff')
      AND (p_peer OR e.event_type <> 'claimius.app_handover')
    ORDER BY e.txid ASC, e.seq ASC
    LIMIT p_limit;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_sync_events(UUID, BIGINT, BIGINT, INTEGER, BOOLEAN) IS 'Delta feed of source rows for one app after a (txid, seq) cursor, in (txid, seq) order. Serves only events whose transaction is below the xmin of the reading snapshot, or the reader''s own. Every event written later carries a txid at or above that xmin, so the cursor never passes an event still to be served, whatever order sequences commit in. Organization and location events are written once per linked app, so other apps'' shared hierarchy rows never leave the prophet. Handover events travel to peer prophets only, and an organization''s private_seed, the key of its apps'' app_secret, and a user_identity''s data, the claims of the token it was issued from, travel to peer prophets only.';

CREATE OR REPLACE FUNCTION claimius._check_profile_item(
    p_key  TEXT,
    p_item JSONB
) RETURNS VOID AS $$
BEGIN
    IF p_key !~ '^[a-z][a-z0-9_.]{0,127}$' THEN
        RAISE EXCEPTION 'profile entry refused: key % must be lower case letters, digits, _ and . starting with a letter', p_key USING ERRCODE = '22023';
    END IF;
    IF p_item IS NULL OR jsonb_typeof(p_item) <> 'object'
       OR jsonb_typeof(p_item -> 'schema') <> 'object' OR NOT (p_item -> 'schema') ? 'type'
       OR NOT p_item ? 'value' THEN
        RAISE EXCEPTION 'profile entry refused: % must be an object with a schema object carrying a type, and a value', p_key USING ERRCODE = '22023';
    END IF;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

COMMENT ON FUNCTION claimius._check_profile_item(TEXT, JSONB) IS 'The shape of one profile entry: a key of lower case letters, digits, _ and ., and an object {"schema": {"type": ...}, "value": ...}. The schema vocabulary and the value against the schema are checked in Go by shared.ProfileSchema on both roles before the call. Raises 22023 otherwise.';

CREATE OR REPLACE FUNCTION claimius.upsert_profile_item(
    p_app_id  UUID,
    p_user_id UUID,
    p_field   TEXT,
    p_item    JSONB
) RETURNS JSONB AS $$
DECLARE
    v_slug    TEXT;
    v_key     TEXT;
    v_profile JSONB;
BEGIN
    SELECT a.slug INTO v_slug FROM claimius.samna_app a WHERE a.id = p_app_id AND a.sa_deleted_at IS NULL;
    IF v_slug IS NULL OR NOT EXISTS (
        SELECT 1 FROM claimius.samna_user u
        WHERE u.user_id = p_user_id AND u.app_id = p_app_id AND u.sa_deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'user % has no live row in app %', p_user_id, p_app_id USING ERRCODE = 'P0404';
    END IF;
    v_key := v_slug || '.' || p_field;
    PERFORM claimius._check_profile_item(v_key, p_item);

    UPDATE claimius.samna_user
       SET profile = profile || jsonb_build_object(v_key, p_item),
           sa_updated_at = now()
     WHERE user_id = p_user_id AND sa_deleted_at IS NULL;

    SELECT u.profile INTO v_profile FROM claimius.samna_user u WHERE u.user_id = p_user_id AND u.app_id = p_app_id;
    RETURN v_profile;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_profile_item(UUID, UUID, TEXT, JSONB) IS 'Writes one entry of an app into the public profile of one of its users under the key <app slug>.<field>, {"schema": {...}, "value": ...}, on every row of the user so the sync carries it to every app that sees the user. The key namespace keeps an app to its own entries. Returns the profile. P0404 when the user has no live row in the app, 22023 for a malformed field or item.';

CREATE OR REPLACE FUNCTION claimius.delete_profile_item(
    p_app_id  UUID,
    p_user_id UUID,
    p_field   TEXT DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_slug    TEXT;
    v_profile JSONB;
BEGIN
    SELECT a.slug INTO v_slug FROM claimius.samna_app a WHERE a.id = p_app_id AND a.sa_deleted_at IS NULL;
    IF v_slug IS NULL OR NOT EXISTS (
        SELECT 1 FROM claimius.samna_user u
        WHERE u.user_id = p_user_id AND u.app_id = p_app_id AND u.sa_deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'user % has no live row in app %', p_user_id, p_app_id USING ERRCODE = 'P0404';
    END IF;

    UPDATE claimius.samna_user u
       SET profile = CASE
                         WHEN p_field IS NULL THEN (SELECT coalesce(jsonb_object_agg(e.key, e.value), '{}'::jsonb)
                                                    FROM jsonb_each(u.profile) e
                                                    WHERE e.key NOT LIKE v_slug || '.%')
                         ELSE u.profile - (v_slug || '.' || p_field)
                     END,
           sa_updated_at = now()
     WHERE u.user_id = p_user_id AND u.sa_deleted_at IS NULL;

    SELECT u.profile INTO v_profile FROM claimius.samna_user u WHERE u.user_id = p_user_id AND u.app_id = p_app_id;
    RETURN v_profile;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_profile_item(UUID, UUID, TEXT) IS 'Removes one entry of the app, <app slug>.<field>, from the public profile of one of its users, or every entry of the app when no field is given, on every row of the user. Returns the profile. P0404 when the user has no live row in the app.';

CREATE OR REPLACE FUNCTION claimius.update_profile(
    p_user_id UUID,
    p_items   JSONB
) RETURNS JSONB AS $$
DECLARE
    v_entry   RECORD;
    v_profile JSONB;
BEGIN
    IF p_items IS NULL OR jsonb_typeof(p_items) <> 'object' THEN
        RAISE EXCEPTION 'profile refused: the items must be an object of key to entry' USING ERRCODE = '22023';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM claimius.samna_user u WHERE u.user_id = p_user_id AND u.sa_deleted_at IS NULL) THEN
        RAISE EXCEPTION 'user % has no live row', p_user_id USING ERRCODE = 'P0404';
    END IF;
    FOR v_entry IN SELECT e.key, e.value FROM jsonb_each(p_items) e LOOP
        IF position('.' IN v_entry.key) > 0 THEN
            RAISE EXCEPTION 'profile refused: % is an app''s key, written through upsert_profile_item', v_entry.key USING ERRCODE = '22023';
        END IF;
        IF jsonb_typeof(v_entry.value) <> 'null' THEN
            PERFORM claimius._check_profile_item(v_entry.key, v_entry.value);
        END IF;
    END LOOP;

    UPDATE claimius.samna_user u
       SET profile = (SELECT coalesce(jsonb_object_agg(e.key, e.value), '{}'::jsonb)
                      FROM jsonb_each(u.profile || p_items) e
                      WHERE jsonb_typeof(e.value) <> 'null'),
           sa_updated_at = now()
     WHERE u.user_id = p_user_id AND u.sa_deleted_at IS NULL;

    SELECT u.profile INTO v_profile FROM claimius.samna_user u WHERE u.user_id = p_user_id AND u.sa_deleted_at IS NULL ORDER BY u.sa_updated_at DESC LIMIT 1;
    RETURN v_profile;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_profile(UUID, JSONB) IS 'Writes the user''s own entries of the public profile, keys without a dot, each {"schema": {...}, "value": ...}; a key whose entry is null is removed. Written on every row of the user so the sync carries it. The prophet calls it for the signed in user; an app''s entries are written through upsert_profile_item. Returns the profile. P0404 for a user with no live row, 22023 for a malformed entry.';

CREATE OR REPLACE FUNCTION claimius.get_secret_value(
    p_user_id    UUID,
    p_secret_id  UUID
) RETURNS TABLE (value TEXT, app_id UUID) AS $$
DECLARE
    v_secret claimius.samna_secret;
BEGIN
    SELECT s.* INTO v_secret
    FROM claimius.samna_secret s
    WHERE s.id = p_secret_id
      AND s.sa_deleted_at IS NULL;

    IF NOT FOUND OR NOT claimius.check_user_active(v_secret.app_id, p_user_id) THEN
        RETURN;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM claimius.get_access(p_user_id, v_secret.app_id, p_secret_id, 'claimius.samna_secret', 4)) THEN
        RETURN;
    END IF;

    RETURN QUERY SELECT v_secret.value, v_secret.app_id;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_secret_value(p_user_id uuid, p_secret_id uuid) IS 'The stored value of one secret, still ciphertext under the seed of the app that wrote it, with that app''s id, when p_user_id is active in the app and reaches the secret with read through get_access: the claim of its creator that insert_secret bound, or any claim the user holds on it. No row for an inactive user, a user without read, an unknown and a deleted secret. The seed never reaches the database; the service opens the value in process with the seed of app_id.';

