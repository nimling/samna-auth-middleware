CREATE OR REPLACE FUNCTION claimius.recompute_user_object(
    p_user_id       UUID,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS VOID AS $$
DECLARE
    v_grants        JSONB;
    v_direct        BOOLEAN;
    v_id            UUID;
    v_eff_access    INTEGER;
    v_eff_scope     JSONB;
    v_grant_obj     JSONB;
    v_any_unbounded BOOLEAN;
    v_norm          RECORD;
    v_search_vec    TSVECTOR;
    v_has_soft      BOOLEAN;
    v_obj_gone      BOOLEAN;
BEGIN
    IF p_object_type IS NULL OR p_object_id IS NULL THEN
        RETURN;
    END IF;

    v_id := claimius.composite_id(p_user_id::TEXT, p_object_type, p_object_id::TEXT);

    IF to_regclass(p_object_type) IS NULL THEN
        RETURN;
    END IF;

    SELECT has_sa_deleted_at INTO v_has_soft
    FROM claimius.table_info
    WHERE object_type = p_object_type;

    IF v_has_soft IS TRUE THEN
        EXECUTE format(
            'SELECT NOT EXISTS (SELECT 1 FROM %I.%I WHERE id = $1 AND sa_deleted_at IS NULL)',
            split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2)
        ) USING p_object_id INTO v_obj_gone;

        IF v_obj_gone THEN
            DELETE FROM claimius.user_object WHERE id = v_id;
            DELETE FROM claimius.object_users
            WHERE object_id = p_object_id
              AND object_type = p_object_type AND user_id = p_user_id;
            RETURN;
        END IF;
    END IF;

    v_grants := claimius._build_grants_for_object(p_user_id, p_object_type, p_object_id);
    v_direct := claimius._direct_grant_for_object(p_user_id, p_object_type, p_object_id);

    -- No surviving access at all: delete the user_object row and companions
    IF jsonb_array_length(v_grants) = 0 AND NOT v_direct THEN
        DELETE FROM claimius.user_object WHERE id = v_id;
        DELETE FROM claimius.object_users
        WHERE TRUE AND object_id = p_object_id
          AND object_type = p_object_type AND user_id = p_user_id;
        RETURN;
    END IF;

    -- Effective access mask: bitwise OR over all surviving grants. Direct
    -- creator contributes the full operator mask (owner | write | read |
    -- execute). Deny semantics already applied in _build_grants_for_object.
    IF v_direct THEN
        v_eff_access := 15;
    ELSE
        v_eff_access := 0;
    END IF;
    FOR v_grant_obj IN SELECT * FROM jsonb_array_elements(v_grants) LOOP
        v_eff_access := v_eff_access | (v_grant_obj ->> 'access')::INTEGER;
    END LOOP;

    -- Effective scope: any grant without a scope grants the whole object,
    -- so the merged scope is NULL. Otherwise union the keys of every
    -- grant's scope object.
    v_any_unbounded := v_direct;
    v_eff_scope := NULL;
    IF NOT v_any_unbounded THEN
        FOR v_grant_obj IN SELECT * FROM jsonb_array_elements(v_grants) LOOP
            IF (v_grant_obj -> 'scope') IS NULL OR jsonb_typeof(v_grant_obj -> 'scope') = 'null' THEN
                v_any_unbounded := TRUE;
                EXIT;
            END IF;
        END LOOP;
    END IF;
    IF NOT v_any_unbounded THEN
        v_eff_scope := '{}'::jsonb;
        FOR v_grant_obj IN SELECT * FROM jsonb_array_elements(v_grants) LOOP
            v_eff_scope := v_eff_scope || coalesce(v_grant_obj -> 'scope', '{}'::jsonb);
        END LOOP;
    END IF;

    -- Pull denormalized fields
    SELECT * INTO v_norm FROM claimius._denormalize_object(p_object_type, p_object_id);

    v_search_vec :=
            setweight(to_tsvector('simple', coalesce(v_norm.sa_name, '')), 'A') ||
            setweight(to_tsvector('simple', coalesce(v_norm.sa_description, '')), 'B');

    INSERT INTO claimius.user_object (
        id, user_id, object_id, object_type,
        sa_access, scope, grants, direct_grant,
        sa_owner_id, sa_location_id, sa_root_id,
        sa_name, sa_description, sa_link, search_vector
    ) VALUES (
                 v_id, p_user_id, p_object_id, p_object_type,
                 v_eff_access, v_eff_scope, v_grants, v_direct,
                 v_norm.sa_owner_id, v_norm.sa_location_id, v_norm.sa_root_id,
                 v_norm.sa_name, v_norm.sa_description, v_norm.sa_link, v_search_vec
             )
    ON CONFLICT (id) DO UPDATE SET
                                   sa_access = EXCLUDED.sa_access,
                                   scope = EXCLUDED.scope,
                                   grants = EXCLUDED.grants,
                                   direct_grant = EXCLUDED.direct_grant,
                                   sa_owner_id = EXCLUDED.sa_owner_id,
                                   sa_location_id = EXCLUDED.sa_location_id,
                                   sa_root_id = EXCLUDED.sa_root_id,
                                   sa_name = EXCLUDED.sa_name,
                                   sa_description = EXCLUDED.sa_description,
                                   sa_link = EXCLUDED.sa_link,
                                   search_vector = EXCLUDED.search_vector,
                                   sa_updated_at = now()
    WHERE claimius.user_object.sa_access        IS DISTINCT FROM EXCLUDED.sa_access
       OR claimius.user_object.scope            IS DISTINCT FROM EXCLUDED.scope
       OR claimius.user_object.grants           IS DISTINCT FROM EXCLUDED.grants
       OR claimius.user_object.direct_grant     IS DISTINCT FROM EXCLUDED.direct_grant
       OR claimius.user_object.sa_owner_id      IS DISTINCT FROM EXCLUDED.sa_owner_id
       OR claimius.user_object.sa_location_id   IS DISTINCT FROM EXCLUDED.sa_location_id
       OR claimius.user_object.sa_root_id       IS DISTINCT FROM EXCLUDED.sa_root_id
       OR claimius.user_object.sa_name          IS DISTINCT FROM EXCLUDED.sa_name
       OR claimius.user_object.sa_description   IS DISTINCT FROM EXCLUDED.sa_description
       OR claimius.user_object.sa_link          IS DISTINCT FROM EXCLUDED.sa_link
       OR claimius.user_object.search_vector    IS DISTINCT FROM EXCLUDED.search_vector;

    -- Maintain object_users
    INSERT INTO claimius.object_users (id, object_id, object_type, user_id)
    VALUES (
               claimius.composite_id(p_object_type, p_object_id::TEXT, p_user_id::TEXT),
               p_object_id, p_object_type, p_user_id
           )
    ON CONFLICT (id) DO NOTHING;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION claimius._object_has_type(
    p_object_type   TEXT,
    p_object_id     UUID,
    p_type_ids      UUID[]
) RETURNS BOOLEAN AS $$
DECLARE
    v_has_soft  BOOLEAN;
    v_matches   BOOLEAN;
BEGIN
    IF to_regclass(p_object_type) IS NULL OR NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = split_part(p_object_type, '.', 1)
          AND table_name   = split_part(p_object_type, '.', 2)
          AND column_name  = 'type_id'
    ) THEN
        RETURN FALSE;
    END IF;

    SELECT has_sa_deleted_at INTO v_has_soft
    FROM claimius.table_info
    WHERE object_type = p_object_type;

    EXECUTE format(
        'SELECT EXISTS (SELECT 1 FROM %I.%I WHERE id = $1 AND type_id = ANY($2)%s)',
        split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2),
        CASE WHEN v_has_soft IS TRUE THEN ' AND sa_deleted_at IS NULL' ELSE '' END
    ) USING p_object_id, p_type_ids INTO v_matches;

    RETURN v_matches;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._object_has_type(TEXT, UUID, UUID[]) IS 'Predicate: true when the registered row carries a type_id in the given set. False when the table or its type_id column does not exist on this database.';

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
    PERFORM claimius.reconcile_if_pending(p_user_id);

    IF NOT claimius.check_user_active(p_user_id) THEN
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
                 LEFT JOIN LATERAL claimius._best_grant(claimius.user_object.grants, p_user_id, p_required_access) best ON TRUE
        WHERE v_include_others
          AND claimius.user_object.user_id = p_user_id
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
          AND (p_type_ids IS NULL OR claimius._object_has_type(claimius.user_object.object_type, claimius.user_object.object_id, p_type_ids))
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
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.search(UUID, TEXT, TEXT[], INTEGER, UUID[], UUID[], UUID[], UUID, TEXT, UUID[], INTEGER, INTEGER) IS 'Full text search over accessible objects. Includes claimius.claim rows the user holds, skipped when any subtree, parent, or type filter is set. p_owner_subtree_ids scopes via ownership tree. p_parent_id and p_parent_type scope via location or parenthood tree. p_type_ids restricts rows to those whose type_id is in the set. total_count carries the un-capped match count for pagination. Rows tie-break on object_id so offset pagination tiles without overlap or skip.';

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
    PERFORM claimius.reconcile_if_pending(p_user_id);

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
        SELECT
            claimius.composite_id(p_user_id::TEXT, p_object_type, p_object_id::TEXT),
            uc.id,
            c.id,
            c.name,
            (c.sa_access | coalesce(co.sa_access, 0)) & 15,
            co.scope,
            co.sa_owner_id,
            co.sa_root_id,
            NULL::UUID,
            FALSE
        FROM claimius.get_ancestors(p_object_type, p_object_id) a
                 JOIN claimius.claim_object co
                      ON TRUE
                          AND co.object_type = a.object_type
                          AND co.object_id = a.object_id
                          AND co.sa_deleted_at IS NULL
                 JOIN claimius.user_claim uc
                      ON uc.claim_id = co.claim_id
                          AND uc.user_id = p_user_id
                          AND TRUE
                          AND uc.sa_deleted_at IS NULL
                          AND (uc.starts_at IS NULL OR uc.starts_at <= now())
                          AND (uc.ends_at IS NULL OR uc.ends_at > now())
                 JOIN claimius.claim c
                      ON c.id = uc.claim_id
                          AND c.sa_deleted_at IS NULL
                          AND ((c.sa_access | coalesce(co.sa_access, 0)) & 16) = 0
                          AND ((((c.sa_access | coalesce(co.sa_access, 0)) & 15) & p_required) = p_required)
                          AND (a.hop = 0 OR (c.inherits AND co.inherits))
        WHERE NOT EXISTS (
            SELECT 1
            FROM claimius.get_ancestors(p_object_type, p_object_id) da
                     JOIN claimius.claim_object dco
                          ON dco.object_type = da.object_type
                              AND dco.object_id = da.object_id
                              AND dco.sa_deleted_at IS NULL
                     JOIN claimius.user_claim duc
                          ON duc.claim_id = dco.claim_id
                              AND duc.user_id = p_user_id
                              AND duc.sa_deleted_at IS NULL
                              AND (duc.starts_at IS NULL OR duc.starts_at <= now())
                              AND (duc.ends_at IS NULL OR duc.ends_at > now())
                     JOIN claimius.claim dc
                          ON dc.id = duc.claim_id
                              AND dc.sa_deleted_at IS NULL
            WHERE ((dc.sa_access | coalesce(dco.sa_access, 0)) & 16) <> 0
              AND (da.hop = 0 OR (dc.inherits AND dco.inherits))
              AND (((dc.sa_access | coalesce(dco.sa_access, 0)) & 15) & p_required) <> 0
        )
        ORDER BY a.hop ASC, claimius._popcount((c.sa_access | coalesce(co.sa_access, 0)) & 15) DESC, uc.id ASC
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
