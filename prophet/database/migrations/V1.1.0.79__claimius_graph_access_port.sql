CREATE TABLE IF NOT EXISTS claimius.object_usage (
    app_id       UUID NOT NULL,
    user_id      UUID NOT NULL,
    object_type  TEXT NOT NULL,
    object_id    UUID NOT NULL,
    used_count   BIGINT NOT NULL DEFAULT 0,
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (app_id, user_id, object_type, object_id)
);

CREATE INDEX IF NOT EXISTS idx_object_usage_user_count ON claimius.object_usage (app_id, user_id, used_count DESC);

GRANT SELECT, INSERT, UPDATE ON claimius.object_usage TO claimius_writer;
GRANT SELECT, INSERT, UPDATE ON claimius.object_usage TO claimius_reader;
GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.object_usage TO claimius_disciple_client;

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

GRANT EXECUTE ON FUNCTION claimius.get_direct_access(UUID, UUID, UUID, TEXT, INTEGER, BOOLEAN)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

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

GRANT EXECUTE ON FUNCTION claimius.get_user_access(UUID, UUID, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;

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

GRANT EXECUTE ON FUNCTION claimius.mutation_access(UUID, TEXT, TEXT, INTEGER)
    TO claimius_disciple_client, claimius_writer, claimius_admin, claimius_reader;


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

ALTER FUNCTION claimius._prune_graph_rows(JSONB, UUID, UUID, UUID, INTEGER, TEXT, UUID) SET plan_cache_mode = force_custom_plan;


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

ALTER FUNCTION claimius.get_graph_links_owner(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) SET plan_cache_mode = force_custom_plan;

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

ALTER FUNCTION claimius.get_graph_links_claim(UUID, UUID, INTEGER, TEXT[], TEXT[], UUID, INTEGER, INTEGER, TEXT, UUID) SET plan_cache_mode = force_custom_plan;

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

ALTER FUNCTION claimius.get_owner_graph(UUID, UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) SET plan_cache_mode = force_custom_plan;

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

ALTER FUNCTION claimius.get_claim_graph(UUID, UUID, UUID, INTEGER, INTEGER, INTEGER, TEXT, UUID) SET plan_cache_mode = force_custom_plan;

