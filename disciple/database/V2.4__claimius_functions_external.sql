DROP FUNCTION IF EXISTS claimius.get_access(UUID, UUID, UUID, TEXT, INTEGER);

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
BEGIN
    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    SELECT s.* INTO v_pair FROM claimius._settle_pair(p_user_id, p_object_type, p_object_id) s;

    IF v_pair.grants IS NOT NULL AND (v_pair.sa_access & p_required_access) = p_required_access THEN
        RETURN QUERY
            SELECT
                claimius.composite_id(p_user_id::TEXT, p_object_type, p_object_id::TEXT),
                best.user_claim_id,
                best.claim_id,
                best.claim_name,
                best.access_bits,
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

    IF NOT v_pair.settled THEN
        RETURN QUERY
            SELECT * FROM claimius.get_direct_access(p_user_id, p_object_id, p_object_type, p_required_access, FALSE, FALSE);
    END IF;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_access(UUID, UUID, TEXT, INTEGER) IS 'Strongest active claim based grant for one (user, object) whose mask covers p_required_access: the user row, then the point settle and the read of the pair with its object_info row in one pass through _settle_pair, and the grant chosen from the pair. Only when the settle was skipped on a lock and the pair gives nothing does it walk the ancestors through get_direct_access. user_claim_id is the actor token for follow-on writes. sa_root_id is the owning organization root, suitable for FK to claimius.organization on follow-on claim and claim_object writes.';

DROP FUNCTION IF EXISTS claimius.get_direct_access(UUID, UUID, UUID, TEXT);

DROP FUNCTION IF EXISTS claimius.get_direct_access(UUID, UUID, TEXT);
DROP FUNCTION IF EXISTS claimius.get_direct_access(UUID, UUID, TEXT, INTEGER, BOOLEAN);
CREATE OR REPLACE FUNCTION claimius.get_direct_access(
    p_user_id       UUID,
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
        PERFORM claimius.reconcile_if_pending(p_user_id, p_object_type, p_object_id);
    END IF;

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
                TRUE,
                0
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
            SELECT uc.id AS user_claim_id, c.id AS claim_id, c.name, c.sa_access, c.inherits, c.level
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
                   co.scope, co.sa_owner_id, co.sa_root_id, h.level AS lvl
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
                   CASE WHEN h.level > cb.level THEN claimius._effective_access(h.sa_access, link.sa_access)
                        WHEN cb.level > h.level THEN claimius._effective_access(cb.sa_access, co.sa_access)
                        ELSE (claimius._effective_access(h.sa_access, link.sa_access)
                                & claimius._effective_access(cb.sa_access, co.sa_access) & ~16)
                             | ((claimius._effective_access(h.sa_access, link.sa_access)
                                | claimius._effective_access(cb.sa_access, co.sa_access)) & 16) END,
                   co.scope, co.sa_owner_id, co.sa_root_id, greatest(h.level, cb.level)
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
            claimius.composite_id(p_user_id::TEXT, p_object_type, p_object_id::TEXT),
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

COMMENT ON FUNCTION claimius.get_direct_access(UUID, UUID, TEXT, INTEGER, BOOLEAN, BOOLEAN) IS 'Locates the closest, strongest user_claim the user holds related to the given object via ancestor walk, directly or through composition, with the binding narrowing its claim and the user''s denies subtracted by class. The walk of the gate when the point settle was skipped on a lock. VOLATILE: with p_settle it settles through reconcile_if_pending.';

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
          AND claimius.user_object.object_type = p_object_type
          AND claimius.user_object.object_id > coalesce(p_after, '00000000-0000-0000-0000-000000000000'::UUID)
          AND (claimius.user_object.sa_access & p_required_access) >= p_required_access
          AND (best.user_claim_id IS NOT NULL OR claimius.user_object.direct_grant)
        ORDER BY claimius.user_object.object_id
        LIMIT p_limit;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_objects(p_user_id uuid, p_object_type text, p_required_access integer, p_after uuid, p_limit integer) IS 'Object ids and metadata for objects whose mask covers p_required_access, in object_id order after p_after, at most p_limit rows. user_claim_id is the actor token from the strongest surviving claim grant; null when only direct_grant applies. claim_id and claim_name describe the claim that contributed the strongest grant. sa_root_id is derived from the owning organization root.';

DROP FUNCTION IF EXISTS claimius.get_organizations(UUID, UUID, INTEGER);
DROP FUNCTION IF EXISTS claimius.get_organizations(UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_organizations(
    p_user_id          UUID,
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
            FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required_access, p_after, NULL) g
                JOIN claimius.organization o ON o.id = g.object_id
            WHERE o.sa_deleted_at IS NULL
            ORDER BY o.id;
        RETURN;
    END IF;

    LOOP
        SELECT array_agg(g.object_id ORDER BY g.object_id) INTO v_ids
        FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required_access, v_after, p_limit) g;
        EXIT WHEN v_ids IS NULL;

        RETURN QUERY
            SELECT o.*
            FROM claimius.organization o
            WHERE o.id = ANY(v_ids)
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

COMMENT ON FUNCTION claimius.get_organizations(p_user_id uuid, p_required_access integer, p_after uuid, p_limit integer) IS 'Organizations whose effective mask covers p_required_access, in id order after p_after, at most p_limit of them: the reader''s range from get_objects, which settles once, each row kept when live and before the page is cut.';

DROP FUNCTION IF EXISTS claimius.get_locations(UUID, UUID, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_locations(
    p_user_id          UUID,
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
            FROM claimius.get_objects(p_user_id, 'claimius.location', p_required_access, p_after, NULL) g
                JOIN claimius.location l ON l.id = g.object_id
            WHERE l.sa_deleted_at IS NULL
            ORDER BY l.id;
        RETURN;
    END IF;

    LOOP
        SELECT array_agg(g.object_id ORDER BY g.object_id) INTO v_ids
        FROM claimius.get_objects(p_user_id, 'claimius.location', p_required_access, v_after, p_limit) g;
        EXIT WHEN v_ids IS NULL;

        RETURN QUERY
            SELECT l.*
            FROM claimius.location l
            WHERE l.id = ANY(v_ids)
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

COMMENT ON FUNCTION claimius.get_locations(p_user_id uuid, p_required_access integer, p_after uuid, p_limit integer) IS 'Locations whose effective mask covers p_required_access, in id order after p_after, at most p_limit of them: the reader''s range from get_objects, which settles once, each row kept when live and before the page is cut.';

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

    PERFORM claimius._settle_directory(p_actor_id);

    IF p_target ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        v_uuid := p_target::UUID;
    END IF;

    SELECT r.user_id INTO v_target_user_id
    FROM (
        SELECT su.user_id, 0 AS rank FROM claimius.samna_user su
        WHERE su.external_id = p_target AND su.sa_deleted_at IS NULL AND su.status = 'active'
        UNION ALL
        SELECT su.user_id, 1 FROM claimius.samna_user su
        WHERE su.user_id = v_uuid AND su.sa_deleted_at IS NULL AND su.status = 'active'
        UNION ALL
        SELECT su.user_id, 2 FROM claimius.samna_user su
        WHERE su.id = v_uuid AND su.sa_deleted_at IS NULL AND su.status = 'active'
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
                           WHERE uu.id = claimius.composite_id(p_actor_id::TEXT, v_target_user_id::TEXT)), 0)
                     + coalesce((
                           SELECT sum(m.a_now * m.t_now - (m.a_now - m.a_shift) * (m.t_now - m.t_shift))
                           FROM (
                               SELECT coalesce(sum(d.delta) FILTER (WHERE d.user_id = p_actor_id), 0) AS a_shift,
                                      coalesce(sum(d.delta) FILTER (WHERE d.user_id = v_target_user_id), 0) AS t_shift,
                                      EXISTS (SELECT 1 FROM claimius.user_object uo
                                              WHERE uo.user_id = p_actor_id
                                                AND uo.object_type = d.object_type AND uo.object_id = d.object_id)::INTEGER AS a_now,
                                      EXISTS (SELECT 1 FROM claimius.user_object uo
                                              WHERE uo.user_id = v_target_user_id
                                                AND uo.object_type = d.object_type AND uo.object_id = d.object_id)::INTEGER AS t_now
                               FROM claimius.user_users_diff d
                               WHERE d.user_id IN (p_actor_id, v_target_user_id)
                               GROUP BY d.object_type, d.object_id
                           ) m), 0) AS n
            ) shared
            WHERE shared.n > 0
              AND p_actor_id <> v_target_user_id

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
    PERFORM claimius._settle_directory(p_user_id);
    PERFORM claimius._settle_user_users_diff(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        WITH branches AS (
            SELECT p_user_id AS target_user_id, 47::INTEGER AS access
            WHERE p_user_id IS NOT NULL

            UNION ALL

            SELECT uu.target_user_id, 36::INTEGER
            FROM claimius._user_users_for_viewer(p_user_id) uu

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

GRANT EXECUTE ON FUNCTION claimius.get_direct_access(UUID, UUID, TEXT, INTEGER, BOOLEAN, BOOLEAN)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_objects(UUID, TEXT, INTEGER, UUID, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_user_access(UUID, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_users(UUID, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

DROP FUNCTION IF EXISTS claimius.resolve_access(UUID, UUID, TEXT, INTEGER);
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
              AND TRUE
              AND a.sa_owner_id IN (
                  SELECT g.object_id
                  FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required) g
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
            FROM claimius.get_user_access(p_user_id, p_object_id, p_required) u;
        RETURN;
    END IF;

    RETURN QUERY
        SELECT * FROM claimius.get_access(p_user_id, p_object_id::UUID, p_object_type, p_required);
END;
$$ LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.resolve_access(UUID, TEXT, TEXT, INTEGER) IS 'The access verdict on one object of any type: audit, relation and samna_user rows through their own lookups, every other type through get_access, which settles and reads the pair in one pass and walks the ancestors only when the settle was skipped on a lock. No row for a malformed id, so a batch never aborts on one.';

GRANT EXECUTE ON FUNCTION claimius.resolve_access(UUID, TEXT, TEXT, INTEGER)
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
        SELECT * FROM claimius.resolve_access(p_user_id, p_object_id, p_object_type, p_required);
    IF FOUND THEN
        RETURN;
    END IF;

    IF p_object_type = 'claimius.samna_user' THEN
        IF EXISTS (
            SELECT 1 FROM claimius.samna_user su
            WHERE su.sa_deleted_at IS NULL
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

DROP FUNCTION IF EXISTS claimius.require_access(UUID, UUID, TEXT, INTEGER);

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
          AND EXISTS (
              SELECT 1 FROM claimius.user_claim uc
              WHERE uc.claim_id = c.id
                AND uc.user_id = p_user_id
                AND TRUE
                AND uc.sa_deleted_at IS NULL
                AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                AND (uc.ends_at IS NULL OR uc.ends_at > now())
                AND (c.sa_access & p_required_access) = p_required_access
          )
        ORDER BY claimius._popcount(c.sa_access) DESC;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_claims(UUID, INTEGER) IS 'The claims the user holds through a live user_claim, at the required level. The claims the user can see or manage, held or reached through a cascade, are get_objects of type claimius.claim.';

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
              SELECT (g ->> 'claim_id')::UUID
              FROM claimius.user_object uo
                       CROSS JOIN LATERAL jsonb_array_elements(uo.grants) g
              WHERE uo.user_id = p_user_id
                AND uo.object_id = p_object_id
                AND uo.object_type = p_object_type
                AND (g ->> 'user_claim_id') IS NOT NULL
                AND ((g ->> 'starts_at') IS NULL OR (g ->> 'starts_at')::TIMESTAMPTZ <= now())
                AND ((g ->> 'ends_at') IS NULL OR (g ->> 'ends_at')::TIMESTAMPTZ > now())
          )
        ORDER BY claimius._popcount(c.sa_access) DESC;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_claims(UUID, UUID, TEXT) IS 'All claims the user holds against one object.';

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
        WHERE uc.user_id = p_user_id
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

COMMENT ON FUNCTION claimius.get_scope_access(UUID, JSONPATH, INTEGER) IS 'Object-less bindings of the user''s live grants whose scope matches a jsonpath, one row per binding, each mask the claim mask narrowed by the binding mask, less the bits of the deny bindings matching the same path at the level of its claim or above, closed under implication.';

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

CREATE OR REPLACE FUNCTION claimius.get_object_assets(
    p_user_id      UUID,
    p_object_type  TEXT,
    p_object_id    UUID,
    p_kind         TEXT DEFAULT NULL
) RETURNS SETOF claimius.object_asset AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    IF p_object_type = 'claimius.samna_user' THEN
        IF p_object_id IS DISTINCT FROM p_user_id AND NOT EXISTS (
            SELECT 1 FROM claimius.get_user_access(p_user_id, p_object_id::TEXT, 4)
        ) THEN
            RETURN;
        END IF;
    ELSIF NOT EXISTS (SELECT 1 FROM claimius.get_access(p_user_id, p_object_id, p_object_type, 4)) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT l.*
        FROM claimius.object_asset l
        WHERE l.object_type = p_object_type
          AND l.object_id = p_object_id
          AND l.sa_deleted_at IS NULL
          AND (p_kind IS NULL OR l.kind = p_kind)
        ORDER BY l."index", l.sa_created_at;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_object_assets(UUID, TEXT, UUID, TEXT) IS 'Live object_asset links on one object that the reader may read, of one kind when p_kind is given, ordered by index then sa_created_at. A samna_user is readable by the user itself and by a reader get_user_access resolves with READ, the branches get_users decides by; any other object needs READ through get_access.';

GRANT EXECUTE ON FUNCTION claimius.get_object_assets(UUID, TEXT, UUID, TEXT)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.get_users(
    p_user_ids  UUID[],
    p_ids       UUID[]
) RETURNS SETOF claimius.samna_user
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp AS $$
    SELECT u.*
    FROM (
        SELECT s.* FROM claimius.samna_user s WHERE s.user_id = ANY(p_user_ids)
        UNION ALL
        SELECT s.* FROM claimius.samna_user s WHERE p_user_ids IS NULL AND s.id = ANY(p_ids)
    ) u
    WHERE p_ids IS NULL OR u.id = ANY(p_ids)
$$;

COMMENT ON FUNCTION claimius.get_users(UUID[], UUID[]) IS 'The samna_user rows named by user_id in p_user_ids and by row id in p_ids, every given list applying, soft deleted and inactive rows included, with no reader and no access filter. For showing who a row names, such as the creator behind a grant; what a reader may see is get_users(user, required).';

CREATE OR REPLACE FUNCTION claimius.get_organizations(
    p_ids        UUID[],
    p_owner_ids  UUID[]
) RETURNS SETOF claimius.organization
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp AS $$
    SELECT o.*
    FROM (
        SELECT x.* FROM claimius.organization x WHERE x.id = ANY(p_ids)
        UNION ALL
        SELECT x.* FROM claimius.organization x WHERE p_ids IS NULL AND x.sa_owner_id = ANY(p_owner_ids)
    ) o
    WHERE p_owner_ids IS NULL OR o.sa_owner_id = ANY(p_owner_ids)
$$;

COMMENT ON FUNCTION claimius.get_organizations(UUID[], UUID[]) IS 'The organization rows named by id in p_ids and owned by an organization in p_owner_ids, every given list applying, soft deleted rows included, with no reader and no access filter. A root owns itself, so it comes back for its own id in p_owner_ids. For names, roots and trees a caller shows or walks; what a reader may see is get_organizations(user, required).';

CREATE OR REPLACE FUNCTION claimius.get_locations(
    p_ids         UUID[],
    p_parent_ids  UUID[]
) RETURNS SETOF claimius.location
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp AS $$
    SELECT l.*
    FROM (
        SELECT x.* FROM claimius.location x WHERE x.id = ANY(p_ids)
        UNION ALL
        SELECT x.* FROM claimius.location x WHERE p_ids IS NULL AND x.sa_parent_id = ANY(p_parent_ids)
    ) l
    WHERE p_parent_ids IS NULL OR l.sa_parent_id = ANY(p_parent_ids)
$$;

COMMENT ON FUNCTION claimius.get_locations(UUID[], UUID[]) IS 'The location rows named by id in p_ids and whose parent is in p_parent_ids, every given list applying, soft deleted rows included, with no reader and no access filter. A root is its own parent, so it comes back for its own id in p_parent_ids. For names and trees a caller shows or walks; what a reader may see is get_locations(user, required).';

CREATE OR REPLACE FUNCTION claimius.get_claims(
    p_ids        UUID[],
    p_owner_ids  UUID[]
) RETURNS SETOF claimius.claim
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp AS $$
    SELECT c.*
    FROM (
        SELECT x.* FROM claimius.claim x WHERE x.id = ANY(p_ids)
        UNION ALL
        SELECT x.* FROM claimius.claim x WHERE p_ids IS NULL AND x.sa_owner_id = ANY(p_owner_ids)
    ) c
    WHERE p_owner_ids IS NULL OR c.sa_owner_id = ANY(p_owner_ids)
$$;

COMMENT ON FUNCTION claimius.get_claims(UUID[], UUID[]) IS 'The claim rows named by id in p_ids and owned by an organization in p_owner_ids, every given list applying, soft deleted rows included, with no reader and no access filter. For the claim behind a grant or a binding; the claims a reader holds are get_claims(user, required).';

CREATE OR REPLACE FUNCTION claimius.get_grants(
    p_ids        UUID[] DEFAULT NULL,
    p_user_ids   UUID[] DEFAULT NULL,
    p_claim_ids  UUID[] DEFAULT NULL,
    p_owner_ids  UUID[] DEFAULT NULL,
    p_clients    BOOLEAN DEFAULT TRUE
) RETURNS SETOF claimius.user_claim
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp AS $$
    SELECT g.*
    FROM (
        SELECT x.* FROM claimius.user_claim x WHERE x.id = ANY(p_ids)
        UNION ALL
        SELECT x.* FROM claimius.user_claim x WHERE p_ids IS NULL AND x.user_id = ANY(p_user_ids)
        UNION ALL
        SELECT x.* FROM claimius.user_claim x
        WHERE p_ids IS NULL AND p_user_ids IS NULL AND x.claim_id = ANY(p_claim_ids)
        UNION ALL
        SELECT x.* FROM claimius.user_claim x
        WHERE p_ids IS NULL AND p_user_ids IS NULL AND p_claim_ids IS NULL AND x.sa_owner_id = ANY(p_owner_ids)
    ) g
    WHERE (p_user_ids IS NULL OR g.user_id = ANY(p_user_ids))
      AND (p_claim_ids IS NULL OR g.claim_id = ANY(p_claim_ids))
      AND (p_owner_ids IS NULL OR g.sa_owner_id = ANY(p_owner_ids))
      AND (p_clients OR NOT EXISTS (SELECT 1 FROM claimius.samna_client sc WHERE sc.id = g.user_id))
$$;

COMMENT ON FUNCTION claimius.get_grants(UUID[], UUID[], UUID[], UUID[], BOOLEAN) IS 'The user_claim rows named by id in p_ids, held by a user in p_user_ids, granting a claim in p_claim_ids and owned by an organization in p_owner_ids, every given list applying and no list giving no rows. With p_clients false the grants a samna_client holds are left out. Soft deleted grants and grants outside their window are included, with no reader and no access filter: the caller applies sa_deleted_at, starts_at and ends_at. For the creator behind sa_created_by, the users a row was created by, and the holders of a claim or of the grants under an organization.';

CREATE OR REPLACE FUNCTION claimius.get_bindings(
    p_object_type  TEXT DEFAULT NULL,
    p_object_ids   UUID[] DEFAULT NULL,
    p_claim_ids    UUID[] DEFAULT NULL
) RETURNS SETOF claimius.claim_object
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp AS $$
    SELECT b.*
    FROM (
        SELECT x.* FROM claimius.claim_object x
        WHERE x.object_type = p_object_type AND x.object_id = ANY(p_object_ids)
        UNION ALL
        SELECT x.* FROM claimius.claim_object x
        WHERE p_object_ids IS NULL AND x.claim_id = ANY(p_claim_ids)
        UNION ALL
        SELECT x.* FROM claimius.claim_object x
        WHERE p_object_ids IS NULL AND p_claim_ids IS NULL AND x.object_type = p_object_type
    ) b
    WHERE (p_object_type IS NULL OR b.object_type = p_object_type)
      AND (p_claim_ids IS NULL OR b.claim_id = ANY(p_claim_ids))
$$;

COMMENT ON FUNCTION claimius.get_bindings(TEXT, UUID[], UUID[]) IS 'The claim_object rows on objects of p_object_type, on the ids in p_object_ids of that type, and of the claims in p_claim_ids, every given argument applying; ids without a type and no argument at all give no rows. Soft deleted bindings are included, with no reader and no access filter: the caller applies sa_deleted_at and reads the claim through get_claims(ids, owner_ids). With get_grants on the claim ids these are the holders of the bindings on an object.';

CREATE OR REPLACE FUNCTION claimius.get_user_tokens(
    p_user_ids  UUID[]
) RETURNS SETOF claimius.user_token
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp AS $$
    SELECT t.*
    FROM claimius.user_token t
    WHERE t.user_id = ANY(p_user_ids)
$$;

COMMENT ON FUNCTION claimius.get_user_tokens(UUID[]) IS 'The user_token rows of the users in p_user_ids, soft deleted rows included, with no reader and no access filter. Its sa_created_by is the grant that bound the user, the user''s creator.';

CREATE OR REPLACE FUNCTION claimius.get_object_types()
RETURNS TABLE(object_type TEXT)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp AS $$
    SELECT ti.object_type FROM claimius.table_info ti
$$;

COMMENT ON FUNCTION claimius.get_object_types() IS 'The schema qualified type of every registered table, internal ones included.';

CREATE OR REPLACE FUNCTION claimius.get_object_usage(
    p_user_id       UUID,
    p_object_ids    UUID[],
    p_object_types  TEXT[] DEFAULT NULL
) RETURNS TABLE(object_type TEXT, object_id UUID, sa_name TEXT, used_count BIGINT) AS $$
BEGIN
    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT coalesce(t.obj_type, uo.object_type), t.obj_id, oi.sa_name, uo.used_count
        FROM unnest(p_object_ids, p_object_types) AS t(obj_id, obj_type)
                 LEFT JOIN claimius.user_object uo
                           ON uo.user_id = p_user_id
                          AND uo.object_id = t.obj_id
                          AND (t.obj_type IS NULL OR uo.object_type = t.obj_type)
                 LEFT JOIN claimius.object_info oi
                           ON oi.object_type = coalesce(t.obj_type, uo.object_type)
                          AND oi.object_id = t.obj_id;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_object_usage(UUID, UUID[], TEXT[]) IS 'For each object of p_object_ids, typed by the same position of p_object_types or of any type when that is NULL, its object_info name and the reader''s used_count, NULL when the reader holds no pair on it. Settles the reader''s share first, which folds its pending usage. For ordering what a reader sees by its own use.';

GRANT EXECUTE ON FUNCTION claimius.get_users(UUID[], UUID[])
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_organizations(UUID[], UUID[])
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_locations(UUID[], UUID[])
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_claims(UUID[], UUID[])
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_grants(UUID[], UUID[], UUID[], UUID[], BOOLEAN)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_bindings(TEXT, UUID[], UUID[])
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_user_tokens(UUID[])
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_object_types()
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_object_usage(UUID, UUID[], TEXT[])
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

DROP FUNCTION IF EXISTS claimius.get_owner_graph(UUID, UUID, INTEGER);
DROP FUNCTION IF EXISTS claimius.get_claim_graph(UUID, UUID, INTEGER);

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
                SELECT CASE WHEN n.parent_id IN (SELECT x.object_id FROM node x) THEN n.parent_id END AS parent_key
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

DROP FUNCTION IF EXISTS claimius._window_links(JSONB, UUID, INTEGER, TEXT, UUID);
CREATE OR REPLACE FUNCTION claimius._window_links(
    p_links     JSONB,
    p_nodes     JSONB,
    p_user_id   UUID,
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
            p_user_id, p_width, p_sort, p_next_id) w
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

COMMENT ON FUNCTION claimius._window_links(JSONB, JSONB, UUID, INTEGER, TEXT, UUID) IS 'The links of a link graph kept by the window _window_graph gives its nodes, a link staying when its target is kept under its source, with node_ids, the ids of every kept node, p_nodes no link targets windowed as top nodes.';

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

    IF p_start_id IS NULL THEN
        SELECT array_agg(o.id) INTO v_root_ids
        FROM claimius.organization o
        WHERE TRUE
          AND o.sa_deleted_at IS NULL
          AND o.sa_owner_id = o.id
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
                               AND TRUE
        WHERE w.id IN (
            SELECT g.object_id FROM claimius.get_objects(p_user_id, 'claimius.organization', p_required_access) g
        );

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

            EXECUTE 'INSERT INTO _owner_graph_rows (object_id, object_type, label, parent_id, data) ' || v_sql USING p_user_id, p_required_access;
        END LOOP;

    IF p_width = 0 AND p_next_id IS NULL THEN
        RETURN QUERY SELECT r.object_id, r.object_type, r.label, r.parent_id, r.data FROM _owner_graph_rows r;
        RETURN;
    END IF;

    SELECT jsonb_agg(to_jsonb(r)) INTO v_rows FROM _owner_graph_rows r;
    RETURN QUERY SELECT * FROM claimius._window_graph(v_rows, p_user_id, p_width, p_sort, p_next_id);
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp SET plan_cache_mode = force_custom_plan;

COMMENT ON FUNCTION claimius.get_owner_graph(UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) IS 'Ownership graph (orgs, locations, registered tables with sa_owner_id or sa_location_id) for the user. Optional start org and depth limit, and a window of p_width children of each type per parent in name or usage order continuing after p_next_id.';

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
    v_pending       INTEGER;
BEGIN
    IF p_sort IS NULL OR p_sort NOT IN ('name', 'usage') OR p_width IS NULL OR p_width < 0 THEN
        RAISE EXCEPTION 'graph window: p_sort is name or usage and p_width at least 0' USING ERRCODE = '22023';
    END IF;

    PERFORM claimius.reconcile_if_pending(p_user_id);
    PERFORM claimius._settle_directory(p_user_id);
    v_pending := claimius._settle_user_users_diff(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN;
    END IF;

    v_cacheable := (p_start_id IS NULL AND p_depth = 0 AND p_required_access = 0);

    IF v_cacheable AND v_pending = 0 THEN
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
                     JOIN claimius._user_users_for_viewer(p_user_id) uu
                          ON uu.target_user_id = u.user_id
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
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp SET plan_cache_mode = force_custom_plan;

COMMENT ON FUNCTION claimius.get_claim_graph(UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) IS 'Access graph: user, other users seen, claims, user_claims, claim_object bindings, reachable objects. Optional start claim and depth limit, and a window of p_width children of each type per parent in name or usage order continuing after p_next_id. Default shape served from claimius.claim_graph_cache while every grant, claim and binding of the user was written by a transaction visible in the snapshot taken before the cached graph was built.';

CREATE OR REPLACE FUNCTION claimius._invalidate_claim_graph_cache()
    RETURNS TRIGGER AS $$
DECLARE
    v_user_id   UUID;
BEGIN
    IF TG_TABLE_NAME = 'user_object' AND TG_OP = 'UPDATE' THEN
        DELETE FROM claimius.claim_graph_cache c
        USING (SELECT DISTINCT n.user_id
               FROM changed n
                   JOIN before o ON o.user_id = n.user_id AND o.object_type = n.object_type AND o.object_id = n.object_id
               WHERE (n.sa_access, n.grants, n.scope, n.direct_grant) IS DISTINCT FROM (o.sa_access, o.grants, o.scope, o.direct_grant)) u
        WHERE c.user_id = u.user_id;
        RETURN NULL;
    ELSIF TG_TABLE_NAME = 'user_object' THEN
        DELETE FROM claimius.claim_graph_cache c
        USING (SELECT DISTINCT x.user_id FROM changed x) u
        WHERE c.user_id = u.user_id;
        RETURN NULL;
    ELSIF TG_TABLE_NAME = 'user_users' THEN
        DELETE FROM claimius.claim_graph_cache c
        USING (SELECT DISTINCT x.viewer_id FROM changed x) u
        WHERE c.user_id = u.viewer_id;
        RETURN NULL;
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

COMMENT ON FUNCTION claimius._invalidate_claim_graph_cache() IS 'Trigger function. Drops claim_graph_cache rows affected by writes to user_object and user_users, once per statement over its transition table, and by writes to object_info and samna_user, per row. A claim or binding change is caught on read by get_claim_graph.';

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
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_required_access) best ON TRUE
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
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_required_access) best ON TRUE
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
    touched AS (
        INSERT INTO claimius.usage_diff (user_id, object_type, object_id, used_count)
        SELECT used.user_id, used.object_type, used.object_id, used.n
        FROM used
        WHERE EXISTS (
            SELECT 1 FROM claimius.user_object uo
            WHERE uo.user_id = used.user_id
              AND uo.object_type = used.object_type
              AND uo.object_id = used.object_id)
        RETURNING user_id
    ),
    moved AS (
        UPDATE claimius.user_activity a
        SET last_used_at = now()
        WHERE a.user_id IN (
            SELECT x.user_id
            FROM claimius.user_activity x
            WHERE x.user_id IN (SELECT DISTINCT used.user_id FROM used)
            FOR NO KEY UPDATE SKIP LOCKED)
        RETURNING a.user_id
    ),
    active AS (
        INSERT INTO claimius.user_activity (user_id, last_used_at)
        SELECT DISTINCT used.user_id, now()
        FROM used
        WHERE NOT EXISTS (
            SELECT 1 FROM claimius.user_activity x
            WHERE x.user_id = used.user_id)
        ORDER BY used.user_id
        ON CONFLICT (user_id) DO NOTHING
        RETURNING user_id
    )
    SELECT count(*) INTO v_touched FROM touched;

    RETURN v_touched;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.touch_object_usage(UUID[], TEXT[], UUID[], BIGINT[]) IS 'Records counted checks as usage_diff rows, folded into user_object.used_count and last_used_at by the user''s next listing, per (user, type, object) row of the arrays, skipping pairs not settled and locking no pair, and moves each counted user''s user_activity, skipping a row a read holds for its cursor so the flush never waits. Local, never synced. Returns the pairs touched.';

GRANT EXECUTE ON FUNCTION claimius.touch_object_usage(UUID[], TEXT[], UUID[], BIGINT[]) TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

DROP FUNCTION IF EXISTS claimius.create_claim(UUID, TEXT, TEXT, INTEGER, UUID, UUID, UUID, BOOLEAN, BOOLEAN, claimius.claim_type);
DROP FUNCTION IF EXISTS claimius.create_claim(UUID, TEXT, TEXT, INTEGER, UUID, UUID, UUID, BOOLEAN, claimius.claim_type);

DROP FUNCTION IF EXISTS claimius.create_claim(text,text,integer,uuid,uuid,uuid,boolean,claimius.claim_type);
DROP FUNCTION IF EXISTS claimius.insert_claim(text,text,integer,uuid,uuid,uuid,boolean,claimius.claim_type);
CREATE OR REPLACE FUNCTION claimius.insert_claim(
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
        name, description, sa_access, inherits, type,
        sa_owner_id, sa_root_id, sa_created_by, level
    ) VALUES (
                 p_name, p_description, v_access, p_inherits, p_type,
                 p_sa_owner_id, p_sa_root_id, p_sa_created_by, coalesce(p_level, 0)
             ) RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.insert_claim(p_name text, p_description text, p_sa_access integer, p_sa_owner_id uuid, p_sa_root_id uuid, p_sa_created_by uuid, p_inherits boolean, p_type claimius.claim_type, p_level integer) IS 'Creates a new claim. p_sa_access is a bitwise mask: 0x01 owner, 0x02 write, 0x04 read, 0x08 execute, 0x10 deny, 0x20 member. On grant claims with the deny bit unset owner implies write+read+execute, write implies read+execute, execute implies read. Member bit 0x20 stands alone. Deny claims store the supplied bits verbatim so callers can deny any subset of read, write, execute, owner. A mask with no bit besides deny is refused with SQLSTATE 22023.';

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

DROP FUNCTION IF EXISTS claimius.assign_claim_user(uuid,uuid,uuid,uuid,text,timestamp with time zone,timestamp with time zone);
CREATE OR REPLACE FUNCTION claimius.insert_user_claim(
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

COMMENT ON FUNCTION claimius.insert_user_claim(p_claim_id uuid, p_user_id uuid, p_sa_owner_id uuid, p_sa_created_by uuid, p_reason text, p_starts_at timestamp with time zone, p_ends_at timestamp with time zone) IS 'Grants a claim to a user in an app.';

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

DROP FUNCTION IF EXISTS claimius.assign_claim_object(uuid,uuid,uuid,uuid,uuid,text,integer,boolean,text,uuid,jsonb);
CREATE OR REPLACE FUNCTION claimius.insert_claim_object(
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

COMMENT ON FUNCTION claimius.insert_claim_object(p_claim_id uuid, p_sa_owner_id uuid, p_sa_root_id uuid, p_sa_created_by uuid, p_object_id uuid, p_object_type text, p_sa_access integer, p_inherits boolean, p_reason text, p_ref_id uuid, p_scope jsonb) IS 'Binds a claim. With (object_type, object_id) p_scope keys are field names; without them p_scope keys are custom. p_sa_access is a bitwise mask.';

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

DROP FUNCTION IF EXISTS claimius.merge_user(uuid, uuid, uuid);

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
            'SELECT a.user_id, array_agg(r.id ORDER BY r.id) AS object_ids FROM %I.%I r CROSS JOIN LATERAL claimius._affected_users_for_object(%L, r.id) a WHERE r.sa_deleted_at IS NULL GROUP BY a.user_id',
            v_schema, v_table, p_object_type);
    FOR v_user IN EXECUTE v_sql LOOP
        FOR v_at IN 1 .. cardinality(v_user.object_ids) BY 1000 LOOP
            PERFORM claimius.recompute_user_object(v_user.user_id, array_fill(p_object_type, ARRAY[cardinality(v_user.object_ids[v_at : v_at + 999])]), v_user.object_ids[v_at : v_at + 999]);
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

CREATE OR REPLACE FUNCTION claimius._restamp_created_by()
    RETURNS INTEGER AS $$
DECLARE
    v_table   RECORD;
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
            ORDER BY 1
        LOOP
            EXECUTE format($q$
                UPDATE claimius.%1$I r
                SET sa_created_by = s.user_claim_id
                FROM (
                    SELECT t.id, (array_agg(uc.id))[1] AS user_claim_id
                    FROM claimius.%1$I t
                        JOIN claimius.user_claim uc
                            ON uc.user_id = t.sa_created_by
                           AND uc.sa_deleted_at IS NULL
                    WHERE NOT EXISTS (SELECT 1 FROM claimius.user_claim x WHERE x.id = t.sa_created_by)
                      AND EXISTS (SELECT 1 FROM claimius.samna_user u WHERE u.user_id = t.sa_created_by)
                    GROUP BY t.id
                    HAVING count(DISTINCT uc.id) = 1
                ) s
                WHERE r.id = s.id$q$, v_table.table_name);
            GET DIAGNOSTICS v_count = ROW_COUNT;
            v_total := v_total + v_count;
        END LOOP;
    END;
    PERFORM set_config('claimius.replay_mode', v_replay, TRUE);
    RETURN v_total;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._restamp_created_by() IS 'Rewrites each sa_created_by that is the user_id of a samna_user holding exactly one live user_claim to that user_claim.id, under replay_mode, and leaves every other stamp. audit keeps its own. Idempotent. Run by the base set. Returns the rows rewritten.';

DROP FUNCTION IF EXISTS claimius.create_root_organization(text,uuid,text,text,uuid,text);
CREATE OR REPLACE FUNCTION claimius.upsert_root_organization(
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

COMMENT ON FUNCTION claimius.upsert_root_organization(TEXT, UUID, TEXT, TEXT, UUID, TEXT) IS 'Creates a new root organization with self referencing sa_owner_id and sa_root_id. Idempotent on (name).';

DROP FUNCTION IF EXISTS claimius.init(text);
CREATE OR REPLACE FUNCTION claimius.upsert_disciple_state(
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

COMMENT ON FUNCTION claimius.upsert_disciple_state(p_disciple_app_slug text) IS 'Bootstrap as a disciple. Ensures the sync cursor row and grants disciple roles. App identity comes from env. State arrives from prophet via sync.';

DROP FUNCTION IF EXISTS claimius.ensure_app_user(UUID, TEXT, TEXT, TEXT, TEXT, TEXT, TEXT);
DROP FUNCTION IF EXISTS claimius.ensure_app_user(uuid,text,text,text,text,text,text,text);
CREATE OR REPLACE FUNCTION claimius.upsert_app_user(
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

COMMENT ON FUNCTION claimius.upsert_app_user(p_user_id uuid, p_first_name text, p_last_name text, p_user_name text, p_email text, p_phone text, p_external_id text, p_user_image text) IS 'Upserts the samna_user row. Idempotent. Claim grants arrive via sync on the disciple.';

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
DROP FUNCTION IF EXISTS claimius.get_graph_links_owner(UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER);

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
          AND (p_required_access = 0 OR (uo.sa_access & p_required_access) = p_required_access)
          AND (uo.direct_grant OR EXISTS (SELECT 1 FROM claimius._best_grant(uo.grants, p_required_access))));
BEGIN
    IF p_sort IS NULL OR p_sort NOT IN ('name', 'usage') OR p_width IS NULL OR p_width < 0 THEN
        RAISE EXCEPTION 'graph window: p_sort is name or usage and p_width at least 0' USING ERRCODE = '22023';
    END IF;

    IF NOT claimius.check_user_active(p_user_id) THEN
        RETURN jsonb_build_object('links', '[]'::jsonb);
    END IF;

    WITH descendants AS (
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
        RETURN claimius._window_links(v_links, v_nodes, p_user_id, p_width, p_sort, p_next_id);
    END IF;

    RETURN jsonb_build_object('links', v_links);
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp SET plan_cache_mode = force_custom_plan;

COMMENT ON FUNCTION claimius.get_graph_links_owner(UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) IS 'The ownership graph of the objects the user reaches as links: for every object in scope its closest ancestor the user also reaches, tested against the user''s pairs read once into a hashed set, with an optional window of p_width children per parent. Settles nothing.';

GRANT EXECUTE ON FUNCTION claimius.get_graph_links_owner(UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) TO claimius_reader, claimius_writer, claimius_admin;

DROP FUNCTION IF EXISTS claimius.get_graph_links_claim(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER);
DROP FUNCTION IF EXISTS claimius.get_graph_links_claim(UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER);

DROP FUNCTION IF EXISTS claimius.get_graph_links_claim(UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID);
CREATE OR REPLACE FUNCTION claimius.get_graph_links_claim(
    p_user_id         UUID,
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

    PERFORM claimius.reconcile_if_pending(p_user_id);
    PERFORM claimius._settle_directory(p_user_id);
    PERFORM claimius._settle_user_users_diff(p_user_id);

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
        JOIN claimius._user_users_for_viewer(p_user_id) uu ON uu.target_user_id = u.user_id
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

    IF p_width > 0 OR p_next_id IS NOT NULL THEN
        v_nodes := (SELECT coalesce(jsonb_agg(jsonb_build_object('object_type', 'claim', 'object_id', c)), '[]'::jsonb)
                    FROM unnest(v_claim_ids) AS c)
                || CASE WHEN v_user_row_id IS NULL THEN '[]'::jsonb
                        ELSE jsonb_build_array(jsonb_build_object('object_type', 'samna_user', 'object_id', v_user_row_id)) END;
        RETURN claimius._window_links(v_links, v_nodes, p_user_id, p_width, p_sort, p_next_id);
    END IF;

    RETURN jsonb_build_object('links', v_links);
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp SET plan_cache_mode = force_custom_plan;

COMMENT ON FUNCTION claimius.get_graph_links_claim(UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) IS 'The claim graph as links: the user''s grants, the bindings of the claims, the cascades of inheriting bindings, the users the user sees through _user_users_for_viewer after folding its user_users_diff rows, and the user''s relations, with an optional window of p_width children per parent.';

GRANT EXECUTE ON FUNCTION claimius.get_graph_links_claim(UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) TO claimius_reader, claimius_writer, claimius_admin;

DROP FUNCTION IF EXISTS claimius.create_organization_user(UUID, UUID, UUID, JSONB);
DROP FUNCTION IF EXISTS claimius.create_organization_user(UUID, UUID, UUID, JSONB, BOOLEAN);
DROP FUNCTION IF EXISTS claimius.create_organization_user(UUID, UUID, UUID, JSONB, BOOLEAN, JSONB, TEXT);

DROP FUNCTION IF EXISTS claimius.create_organization_user(uuid,uuid,jsonb,boolean,jsonb,text);
CREATE OR REPLACE FUNCTION claimius.insert_organization_user(
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

    IF p_identity IS NOT NULL THEN
        v_identity_id := NULLIF(p_identity ->> 'external_id', '');
        IF v_identity_id IS NULL THEN
            RAISE EXCEPTION 'identity external_id required';
        END IF;
        SELECT * INTO v_token_row
          FROM claimius.user_token
         WHERE TRUE
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
           AND sa_deleted_at IS NULL
    );

    IF NOT p_upsert AND p_identity IS NULL THEN
        IF NOT v_created THEN
            RAISE EXCEPTION 'user_id already exists' USING ERRCODE = '23505';
        END IF;

        IF v_external_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM claimius.samna_user
             WHERE external_id = v_external_id
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
        v_claim_user := claimius.insert_user_claim(
            p_claim_id      => v_claim_id,
            p_user_id       => v_user_row.user_id,
            p_sa_owner_id   => p_owner_id,
            p_sa_created_by => p_caller_uc,
            p_reason        => p_reason
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
        v_virtual_claim := claimius.insert_claim(
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
        v_claim_obj := claimius.insert_claim_object(
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
            v_caller_user_claim := claimius.insert_user_claim(
                p_claim_id      => v_virtual_claim_id,
                p_user_id       => v_caller_user_id,
                p_sa_owner_id   => p_owner_id,
                p_sa_created_by => p_caller_uc
            );
        END IF;
    END IF;

    IF p_identity IS NOT NULL THEN
        INSERT INTO claimius.user_token (
            user_id, id_claim, external_id, type, claim_id, data, sa_owner_id, sa_created_by
        ) VALUES (
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

COMMENT ON FUNCTION claimius.insert_organization_user(UUID, UUID, JSONB, BOOLEAN, JSONB, TEXT) IS
    'Adds a user to an organization of the app this disciple mirrors: inserts or upserts the samna_user, grants the supplied claim with p_reason, ensures a per-user virtual claim with full access bound to the user, and when p_caller_uc is not null grants that virtual claim to the caller. The caller must be a live user_claim. With p_identity, an outside identity {id_claim, external_id, type, data} names the user: a user_token of the owner and external_id reuses its user, the call upserts, and the user_token is written. Without p_identity and with p_upsert=FALSE, raises 23505 when user_id or external_id already exists. Returns samna_user, created, user_claim, virtual_claim, claim_object, caller_user_claim and user_token.';

GRANT EXECUTE ON FUNCTION claimius.insert_organization_user(UUID, UUID, JSONB, BOOLEAN, JSONB, TEXT)
    TO claimius_disciple_client, claimius_writer, claimius_admin;

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
    SELECT claimius.reconcile_if_pending(a.user_id, p_object_type, p_object_id)
    FROM claimius._affected_users_for_object(p_object_type, p_object_id) a
    WHERE p_created_by IS NULL OR a.user_id <> p_created_by;

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

COMMENT ON FUNCTION claimius.notify_object(UUID, TEXT, TEXT, JSONB, TEXT, TEXT, TEXT, UUID, UUID, UUID, INTERVAL) IS 'Fans a notification out to every user holding read access on the object, excluding the triggering user. Settles first the pair of every user who may hold the object, so a reader who has not read it since the last write is reached. Returns the number of rows inserted.';

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

CREATE OR REPLACE FUNCTION claimius.insert_secret(
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
       AND sa_deleted_at IS NULL;
    IF v_claim_id IS NULL THEN
        RAISE EXCEPTION 'created_by invalid';
    END IF;

    SELECT sa_root_id INTO v_root_id
      FROM claimius.organization
     WHERE id = p_sa_owner_id
       AND sa_deleted_at IS NULL;
    IF v_root_id IS NULL THEN
        RAISE EXCEPTION 'owner_id invalid';
    END IF;

    INSERT INTO claimius.samna_secret (key, value, expires_at, sa_owner_id, sa_created_by)
    VALUES (p_key, p_value, p_expires_at, p_sa_owner_id, p_sa_created_by)
    RETURNING * INTO v_secret;

    v_binding := claimius.insert_claim_object(
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

COMMENT ON FUNCTION claimius.insert_secret(TEXT, TEXT, TIMESTAMPTZ, UUID, UUID) IS 'Stores a secret of an app, its value already encrypted under the app seed, owned by an organization, and binds it to the claim of the actor p_sa_created_by with owner, write and read, so the secret is reached through the actor''s claim as on the prophet. Returns samna_secret and claim_object.';
