-- ============================================================================
-- Claimius V2.6 Disciple Calc Internals (app_id free)
-- ----------------------------------------------------------------------------
-- The app_id free calc engine for the disciple. In production the calc
-- triggers are dormant: sync applies relation tables under replay_mode and the
-- calc functions short circuit on in_replay_mode(). In debug mode (no
-- replay_mode) local writes fire the calc path and materialize user_object so
-- a standalone disciple reads correctly. Authored app_id free throughout.
-- ============================================================================
-- build_ownership_tree
-- Rewrites the ownership tree closure rooted at p_root_id from live data.
-- Includes org-org edges plus edges from every registered table whose rows
-- point at any org in the tree via sa_owner_id.
CREATE OR REPLACE FUNCTION claimius.build_ownership_tree(p_root_id UUID)
    RETURNS VOID AS $$
BEGIN
    DELETE FROM claimius.inheritance_info
    WHERE tree_type = 'ownership' AND root_id = p_root_id;

    -- Org-org edges. Walk down recursively, recording the path of ancestors
    -- to each node, then explode the path into one edge per ancestor.
    INSERT INTO claimius.inheritance_info (
        tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
    )
    WITH RECURSIVE org_paths AS (
        SELECT id AS node_id, ARRAY[id]::UUID[] AS path
        FROM claimius.organization
        WHERE id = p_root_id AND sa_deleted_at IS NULL
        UNION ALL
        SELECT o.id, p.path || o.id
        FROM claimius.organization o
                 JOIN org_paths p ON o.sa_owner_id = p.node_id
        WHERE o.id <> o.sa_owner_id AND o.sa_deleted_at IS NULL
    )
    SELECT 'ownership'::claimius.tree_type, p_root_id,
           'claimius.organization'::TEXT, p.path[i]::UUID,
           'claimius.organization'::TEXT, p.node_id,
           array_length(p.path, 1) - i
    FROM org_paths p
             CROSS JOIN LATERAL generate_subscripts(p.path, 1) AS i;

    -- Registered-table edges. For each row r in any registered table whose
    -- owner is in the org tree, emit:
    --   self-edge (r, r, 0)
    --   plus (ancestor_of_r.sa_owner_id, r, depth + 1) for each org ancestor
    INSERT INTO claimius.inheritance_info (
        tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
    )
    SELECT 'ownership'::claimius.tree_type, p_root_id, t.object_type, r.id, t.object_type, r.id, 0
    FROM claimius.table_info t
             CROSS JOIN LATERAL claimius._select_owned_rows(t.object_type, p_root_id) r
    WHERE t.has_sa_owner_id
      AND t.has_sa_deleted_at
      AND t.object_type <> 'claimius.organization'
    UNION ALL
    SELECT 'ownership'::claimius.tree_type, p_root_id, e.ancestor_type, e.ancestor_id, t.object_type, r.id, e.depth + 1
    FROM claimius.table_info t
             CROSS JOIN LATERAL claimius._select_owned_rows(t.object_type, p_root_id) r
             JOIN claimius.inheritance_info e
                  ON e.tree_type = 'ownership'
                      AND e.root_id = p_root_id
                      AND e.descendant_type = 'claimius.organization'
                      AND e.descendant_id = r.sa_owner_id
    WHERE t.has_sa_owner_id
      AND t.has_sa_deleted_at
      AND t.object_type <> 'claimius.organization';
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.build_ownership_tree(UUID) IS 'Rewrites ownership tree closure rooted at p_root_id from live data.';

-- build_location_tree
-- Rewrites the location tree closure rooted at p_root_id from live data.
-- Includes location-location edges plus edges from every registered table
-- whose rows point at any location in the tree via sa_location_id.
CREATE OR REPLACE FUNCTION claimius.build_location_tree(p_root_id UUID)
    RETURNS VOID AS $$
BEGIN
    DELETE FROM claimius.inheritance_info
    WHERE tree_type = 'location' AND root_id = p_root_id;

    INSERT INTO claimius.inheritance_info (
        tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
    )
    WITH RECURSIVE loc_paths AS (
        SELECT id AS node_id, ARRAY[id]::UUID[] AS path
        FROM claimius.location
        WHERE id = p_root_id AND sa_deleted_at IS NULL
        UNION ALL
        SELECT l.id, p.path || l.id
        FROM claimius.location l
                 JOIN loc_paths p ON l.sa_parent_id = p.node_id
        WHERE l.id <> l.sa_parent_id AND l.sa_deleted_at IS NULL
    )
    SELECT 'location'::claimius.tree_type, p_root_id,
           'claimius.location'::TEXT, p.path[i]::UUID,
           'claimius.location'::TEXT, p.node_id,
           array_length(p.path, 1) - i
    FROM loc_paths p
             CROSS JOIN LATERAL generate_subscripts(p.path, 1) AS i;

    INSERT INTO claimius.inheritance_info (
        tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
    )
    SELECT 'location'::claimius.tree_type, p_root_id, t.object_type, r.id, t.object_type, r.id, 0
    FROM claimius.table_info t
             CROSS JOIN LATERAL claimius._select_located_rows(t.object_type, p_root_id) r
    WHERE t.has_sa_location_id
      AND t.has_sa_deleted_at
    UNION ALL
    SELECT 'location'::claimius.tree_type, p_root_id, e.ancestor_type, e.ancestor_id, t.object_type, r.id, e.depth + 1
    FROM claimius.table_info t
             CROSS JOIN LATERAL claimius._select_located_rows(t.object_type, p_root_id) r
             JOIN claimius.inheritance_info e
                  ON e.tree_type = 'location'
                      AND e.root_id = p_root_id
                      AND e.descendant_type = 'claimius.location'
                      AND e.descendant_id = r.sa_location_id
    WHERE t.has_sa_location_id
      AND t.has_sa_deleted_at;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.build_location_tree(UUID) IS 'Rewrites location tree closure rooted at p_root_id from live data.';

-- build_parenthood_tree
-- Rewrites the parenthood tree closure for one root of a self referencing
-- registered table. Walks the table's sa_parent_id chain only.
CREATE OR REPLACE FUNCTION claimius.build_parenthood_tree(p_object_type TEXT, p_root_id UUID)
    RETURNS VOID AS $$
DECLARE
    v_schema TEXT;
    v_table TEXT;
    v_parts TEXT[];
    v_sql TEXT;
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    DELETE FROM claimius.inheritance_info
    WHERE tree_type = 'parenthood' AND root_id = p_root_id AND ancestor_type = p_object_type;

    v_sql := format($f$
        INSERT INTO claimius.inheritance_info (
            tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
        )
        WITH RECURSIVE node_paths AS (
            SELECT id AS node_id, ARRAY[id]::UUID[] AS path
            FROM %I.%I
            WHERE id = $1 AND sa_deleted_at IS NULL
            UNION ALL
            SELECT t.id, p.path || t.id
            FROM %I.%I t
            JOIN node_paths p ON t.sa_parent_id = p.node_id
            WHERE t.id <> t.sa_parent_id AND t.sa_deleted_at IS NULL
        )
        SELECT 'parenthood'::claimius.tree_type, $1,
               %L::TEXT, p.path[i]::UUID,
               %L::TEXT, p.node_id,
               array_length(p.path, 1) - i
        FROM node_paths p
                 CROSS JOIN LATERAL generate_subscripts(p.path, 1) AS i
    $f$, v_schema, v_table, v_schema, v_table, p_object_type, p_object_type);

    EXECUTE v_sql USING p_root_id;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.build_parenthood_tree(TEXT, UUID) IS 'Rewrites parenthood tree closure for one root of a self referencing registered table.';

-- _select_owned_rows
-- Returns (id, sa_owner_id) for live rows of an object_type whose owner is
-- in the same root tree as p_root_id. Implemented via dynamic SQL.
CREATE OR REPLACE FUNCTION claimius._select_owned_rows(p_object_type TEXT, p_root_id UUID)
    RETURNS TABLE(id UUID, sa_owner_id UUID) AS $$
DECLARE
    v_schema TEXT;
    v_table TEXT;
    v_parts TEXT[];
    v_sql TEXT;
    v_has_deleted BOOLEAN;
    v_deleted_clause TEXT;
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    -- Look up whether this table has sa_deleted_at. Audit and similar
    -- append-only tables don't, so we omit the soft-delete filter for them.
    SELECT has_sa_deleted_at INTO v_has_deleted
    FROM claimius.table_info WHERE object_type = p_object_type;

    v_deleted_clause := CASE WHEN coalesce(v_has_deleted, FALSE)
                                 THEN 'AND r.sa_deleted_at IS NULL'
                             ELSE ''
        END;

    v_sql := format($f$
        WITH RECURSIVE org_tree AS (
            SELECT id FROM claimius.organization WHERE id = $1 AND sa_deleted_at IS NULL
            UNION ALL
            SELECT o.id FROM claimius.organization o JOIN org_tree t ON o.sa_owner_id = t.id
            WHERE o.id <> o.sa_owner_id AND o.sa_deleted_at IS NULL
        )
        SELECT r.id::UUID, r.sa_owner_id::UUID
        FROM %I.%I r
        WHERE r.sa_owner_id IN (SELECT id FROM org_tree)
          %s
    $f$, v_schema, v_table, v_deleted_clause);
    RETURN QUERY EXECUTE v_sql USING p_root_id;
END;
$$ LANGUAGE plpgsql STABLE;

-- _select_located_rows
-- Returns (id, sa_location_id) for live rows of an object_type whose
-- location is in the same root tree as p_root_id.
CREATE OR REPLACE FUNCTION claimius._select_located_rows(p_object_type TEXT, p_root_id UUID)
    RETURNS TABLE(id UUID, sa_location_id UUID) AS $$
DECLARE
    v_schema TEXT;
    v_table TEXT;
    v_parts TEXT[];
    v_sql TEXT;
    v_has_deleted BOOLEAN;
    v_deleted_clause TEXT;
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    SELECT has_sa_deleted_at INTO v_has_deleted
    FROM claimius.table_info WHERE object_type = p_object_type;

    v_deleted_clause := CASE WHEN coalesce(v_has_deleted, FALSE)
                                 THEN 'AND r.sa_deleted_at IS NULL'
                             ELSE ''
        END;

    v_sql := format($f$
        WITH RECURSIVE loc_tree AS (
            SELECT id FROM claimius.location WHERE id = $1 AND sa_deleted_at IS NULL
            UNION ALL
            SELECT l.id FROM claimius.location l JOIN loc_tree t ON l.sa_parent_id = t.id
            WHERE l.id <> l.sa_parent_id AND l.sa_deleted_at IS NULL
        )
        SELECT r.id::UUID, r.sa_location_id::UUID
        FROM %I.%I r
        WHERE r.sa_location_id IN (SELECT id FROM loc_tree)
          %s
    $f$, v_schema, v_table, v_deleted_clause);
    RETURN QUERY EXECUTE v_sql USING p_root_id;
END;
$$ LANGUAGE plpgsql STABLE;
-- _attach_calc_trigger
-- Attaches the appropriate calc trigger to a registered table. Drops any
-- existing trigger first so the operation is idempotent.
CREATE OR REPLACE FUNCTION claimius._attach_calc_trigger(p_object_type TEXT)
    RETURNS VOID AS $$
DECLARE
    v_schema        TEXT;
    v_table         TEXT;
    v_parts         TEXT[];
    v_has_parent    BOOLEAN;
    v_has_root      BOOLEAN;
    v_func_name     TEXT;
    v_trigger_name  TEXT := 'tg_calc_access';
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    SELECT t.has_sa_parent_id, t.has_sa_root_id
    INTO v_has_parent, v_has_root
    FROM claimius.table_info t WHERE t.object_type = p_object_type;

    IF v_has_parent AND v_has_root THEN
        v_func_name := 'claimius.calc_hierarchical_access';
    ELSE
        v_func_name := 'claimius.calc_object_access';
    END IF;

    EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I.%I', v_trigger_name, v_schema, v_table);
    EXECUTE format(
            'CREATE TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I.%I FOR EACH ROW EXECUTE FUNCTION %s()',
            v_trigger_name, v_schema, v_table, v_func_name
            );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._attach_calc_trigger(TEXT) IS 'Attaches the right calc trigger to a registered table.';

-- _detach_calc_trigger
-- Removes the calc trigger from a previously registered table. Used during
-- rebuild when a table is dropped from the input list.
CREATE OR REPLACE FUNCTION claimius._detach_calc_trigger(p_object_type TEXT)
    RETURNS VOID AS $$
DECLARE
    v_parts TEXT[];
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = v_parts[1] AND table_name = v_parts[2]) THEN
        EXECUTE format('DROP TRIGGER IF EXISTS tg_calc_access ON %I.%I', v_parts[1], v_parts[2]);
    END IF;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._detach_calc_trigger(TEXT) IS 'Removes calc trigger from a deregistered table.';
-- _user_id_for_user_claim
-- Looks up the user_id for a given user_claim.id. Used to translate
-- sa_created_by into a user reference. Returns NULL if no match (which
-- happens during bootstrap before user_claims exist).
CREATE OR REPLACE FUNCTION claimius._user_id_for_user_claim(p_user_claim_id UUID)
    RETURNS TABLE(user_id UUID) AS $$
BEGIN
    RETURN QUERY
        SELECT uc.user_id
        FROM claimius.user_claim uc
        WHERE uc.id = p_user_claim_id AND uc.sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE;

-- _build_grants_for_object
-- Computes the grants jsonb array for one (user, object) pair by walking
-- claim_objects bound directly or via inheritance trees, applying deny
-- semantics. Returns an empty array if no grants survive.
CREATE OR REPLACE FUNCTION claimius._build_grants_for_object(
    p_user_id       UUID,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS JSONB AS $$
DECLARE
    v_grants        JSONB := '[]'::JSONB;
    v_denies        JSONB := '[]'::JSONB;
    v_row           RECORD;
    v_grant_bits    INTEGER;
    v_deny_bits     INTEGER;
    v_is_deny       BOOLEAN;
BEGIN
    -- Step 1: collect every claim_object binding that could grant this user
    -- access to this object, via any path. Each row carries the claim, its
    -- bitwise access mask (which encodes the deny flag in bit 0x10), the
    -- per binding scope jsonb, and the cascaded_from anchor (the object the
    -- claim_object was bound to). For non inherited bindings, cascaded_from
    -- equals the object itself.

    FOR v_row IN
        WITH user_claims AS (
            SELECT uc.claim_id, c.sa_access AS claim_access, c.inherits, c.sa_deleted_at
            FROM claimius.user_claim uc
                     JOIN claimius.claim c ON c.id = uc.claim_id
            WHERE uc.user_id = p_user_id
              AND TRUE
              AND uc.sa_deleted_at IS NULL
              AND c.sa_deleted_at IS NULL
        ),
             direct_bindings AS (
                 -- claim_object directly on this object
                 SELECT co.claim_id,
                        p_object_type AS cascaded_from_type,
                        p_object_id   AS cascaded_from_id,
                        'direct'::TEXT AS tree_type,
                        (uc.claim_access | COALESCE(co.sa_access, uc.claim_access)) AS sa_access,
                        co.scope
                 FROM claimius.claim_object co
                          JOIN user_claims uc ON uc.claim_id = co.claim_id
                 WHERE co.object_type = p_object_type
                   AND co.object_id = p_object_id
                   AND TRUE
                   AND co.sa_deleted_at IS NULL
             ),
             cascaded_bindings AS (
                 -- claim_object on an ancestor in any closure-table tree this
                 -- object participates in. Direct binding case is excluded by
                 -- depth > 0; direct_bindings handles that path.
                 SELECT co.claim_id,
                        co.object_type AS cascaded_from_type,
                        co.object_id   AS cascaded_from_id,
                        e.tree_type::TEXT AS tree_type,
                        (uc.claim_access | COALESCE(co.sa_access, uc.claim_access)) AS sa_access,
                        co.scope
                 FROM claimius.claim_object co
                          JOIN user_claims uc ON uc.claim_id = co.claim_id AND uc.inherits = TRUE
                          JOIN claimius.inheritance_info e
                              ON e.ancestor_type = co.object_type
                             AND e.ancestor_id   = co.object_id
                             AND e.descendant_type = p_object_type
                             AND e.descendant_id   = p_object_id
                             AND e.depth > 0
                 WHERE TRUE
                   AND co.sa_deleted_at IS NULL
                   AND co.inherits = TRUE
             )
        SELECT * FROM direct_bindings
        UNION ALL
        SELECT * FROM cascaded_bindings
        LOOP
            v_is_deny := (v_row.sa_access & 16) <> 0;
            IF v_is_deny THEN
                v_deny_bits := v_row.sa_access & 15;
                v_denies := v_denies || jsonb_build_array(jsonb_build_object(
                        'claim_id', v_row.claim_id,
                        'access', v_deny_bits,
                        'scope', v_row.scope,
                        'cascaded_from', jsonb_build_object('type', v_row.cascaded_from_type, 'id', v_row.cascaded_from_id),
                        'tree_type', v_row.tree_type
                                                          ));
            ELSE
                v_grant_bits := v_row.sa_access & 15;
                v_grants := v_grants || jsonb_build_array(jsonb_build_object(
                        'claim_id', v_row.claim_id,
                        'access', v_grant_bits,
                        'scope', v_row.scope,
                        'cascaded_from', jsonb_build_object('type', v_row.cascaded_from_type, 'id', v_row.cascaded_from_id),
                        'tree_type', v_row.tree_type
                                                          ));
            END IF;
        END LOOP;

    -- Step 2: subtract deny bits from grants on the same path. A deny on
    -- path P with bits B clears those bits from every grant on path P.
    -- Paths are identified by (cascaded_from, tree_type). Direct denies
    -- (tree_type = 'direct', cascaded_from = the object itself) subtract
    -- bits from all grants regardless of path.
    IF jsonb_array_length(v_denies) > 0 THEN
        v_grants := claimius._apply_denies(v_grants, v_denies, p_object_type, p_object_id);
    END IF;

    RETURN v_grants;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._build_grants_for_object IS 'Computes grants jsonb for a (user, object) pair, with bitwise deny subtraction.';

-- _apply_denies
-- Subtracts deny bits from each grant on the same path. Returns a new
-- jsonb array of grants. Grants whose access mask becomes 0 after
-- subtraction are dropped.
CREATE OR REPLACE FUNCTION claimius._apply_denies(
    p_grants        JSONB,
    p_denies        JSONB,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS JSONB AS $$
DECLARE
    v_result        JSONB := '[]'::JSONB;
    v_grant         JSONB;
    v_deny          JSONB;
    v_object_key    JSONB;
    v_grant_bits    INTEGER;
    v_deny_bits     INTEGER;
BEGIN
    v_object_key := jsonb_build_object('type', p_object_type, 'id', p_object_id);

    FOR v_grant IN SELECT * FROM jsonb_array_elements(p_grants) LOOP
            v_grant_bits := (v_grant ->> 'access')::INTEGER;
            FOR v_deny IN SELECT * FROM jsonb_array_elements(p_denies) LOOP
                v_deny_bits := (v_deny ->> 'access')::INTEGER;
                -- A direct deny on the object itself subtracts from any
                -- grant on any path.
                IF (v_deny ->> 'tree_type') = 'direct' AND (v_deny -> 'cascaded_from') = v_object_key THEN
                    v_grant_bits := v_grant_bits & ~v_deny_bits;
                -- A deny on the same (tree_type, cascaded_from) path
                -- subtracts from grants on that same path.
                ELSIF (v_deny ->> 'tree_type') = (v_grant ->> 'tree_type')
                    AND (v_deny -> 'cascaded_from') = (v_grant -> 'cascaded_from') THEN
                    v_grant_bits := v_grant_bits & ~v_deny_bits;
                END IF;
                EXIT WHEN v_grant_bits = 0;
            END LOOP;
            IF v_grant_bits <> 0 THEN
                v_result := v_result || jsonb_build_array(jsonb_set(v_grant, '{access}', to_jsonb(v_grant_bits)));
            END IF;
        END LOOP;

    RETURN v_result;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

COMMENT ON FUNCTION claimius._apply_denies IS 'Subtracts deny bits from grants on matching paths. Drops grants whose mask becomes 0.';

-- _direct_grant_for_object
-- Determines whether the given user holds direct grant on the given object
-- via sa_created_by chain. A direct grant exists when there is a row in
-- the registered table with this id whose sa_created_by maps (via user_claim
-- lookup) to this user_id and app_id, AND no deny at level 0 on this object
-- blocks them.
CREATE OR REPLACE FUNCTION claimius._direct_grant_for_object(
    p_user_id       UUID,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS BOOLEAN AS $$
DECLARE
    v_schema        TEXT;
    v_table         TEXT;
    v_parts         TEXT[];
    v_sql           TEXT;
    v_creator_uc_id UUID;
    v_creator_user  UUID;
    v_has_deny      BOOLEAN;
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    IF p_object_type = 'claimius.samna_user' THEN
        IF NOT EXISTS (
            SELECT 1 FROM claimius.samna_user
            WHERE id = p_object_id
              AND user_id = p_user_id
              AND TRUE
              AND sa_deleted_at IS NULL
        ) THEN
            RETURN FALSE;
        END IF;
    ELSE
        v_sql := format('SELECT sa_created_by FROM %I.%I WHERE id = $1 AND sa_deleted_at IS NULL', v_schema, v_table);
        EXECUTE v_sql INTO v_creator_uc_id USING p_object_id;

        IF v_creator_uc_id IS NULL THEN
            RETURN FALSE;
        END IF;

        IF v_creator_uc_id <> p_user_id THEN
            SELECT uc.user_id INTO v_creator_user
            FROM claimius.user_claim uc
            WHERE uc.id = v_creator_uc_id AND uc.sa_deleted_at IS NULL;

            IF v_creator_user IS NULL OR v_creator_user <> p_user_id THEN
                RETURN FALSE;
            END IF;
        END IF;
    END IF;

    -- A direct creator's grant is wiped only by a deny that subtracts the
    -- owner bit. Any binding (claim or claim_object) whose effective mask
    -- has the deny bit (0x10) and the owner bit (0x01) on this object or
    -- any of its inherited ancestors revokes the direct grant.
    SELECT EXISTS (
        SELECT 1
        FROM claimius.user_claim uc
                 JOIN claimius.claim c ON c.id = uc.claim_id
                 JOIN claimius.claim_object co ON co.claim_id = c.id
        WHERE uc.user_id = p_user_id
          AND TRUE
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
          AND TRUE
          AND co.sa_deleted_at IS NULL
          AND ((c.sa_access | co.sa_access) & 16) <> 0
          AND ((c.sa_access | co.sa_access) & 1)  <> 0
          AND (
            (co.object_type = p_object_type AND co.object_id = p_object_id)
                OR (co.inherits = TRUE AND c.inherits = TRUE AND EXISTS (
                SELECT 1 FROM claimius.inheritance_info e
                WHERE e.ancestor_type   = co.object_type
                  AND e.ancestor_id     = co.object_id
                  AND e.descendant_type = p_object_type
                  AND e.descendant_id   = p_object_id
                  AND e.depth > 0
            ))
            )
    ) INTO v_has_deny;

    RETURN NOT v_has_deny;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._direct_grant_for_object IS 'Determines if user has direct (creator) grant, accounting for owner bit denies.';

-- _denormalize_object
-- Reads the registered row's name, description, sa_owner_id, sa_location_id,
-- sa_root_id, and produces the denormalized values for user_object.
CREATE OR REPLACE FUNCTION claimius._denormalize_object(
    p_object_type   TEXT,
    p_object_id     UUID,
    OUT sa_name TEXT,
    OUT sa_description TEXT,
    OUT sa_link TEXT,
    OUT sa_owner_id UUID,
    OUT sa_location_id UUID,
    OUT sa_root_id UUID
) AS $$
DECLARE
    v_schema        TEXT;
    v_table         TEXT;
    v_parts         TEXT[];
    v_info          claimius.table_info%ROWTYPE;
    v_sql           TEXT;
    v_select_cols   TEXT;
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];

    SELECT * INTO v_info FROM claimius.table_info WHERE object_type = p_object_type;

    IF v_info IS NULL THEN
        RETURN;
    END IF;

    v_select_cols := format(
            '%s, %s, %s, %s, %s',
            CASE WHEN v_info.has_name THEN 'name::TEXT' ELSE 'NULL::TEXT' END,
            CASE WHEN v_info.has_description THEN 'description::TEXT' ELSE 'NULL::TEXT' END,
            CASE WHEN v_info.has_sa_owner_id THEN 'sa_owner_id' ELSE 'NULL::UUID' END,
            CASE WHEN v_info.has_sa_location_id THEN 'sa_location_id' ELSE 'NULL::UUID' END,
            CASE WHEN v_info.has_sa_root_id THEN 'sa_root_id' ELSE 'NULL::UUID' END
                     );

    v_sql := format('SELECT %s FROM %I.%I WHERE id = $1', v_select_cols, v_schema, v_table);
    EXECUTE v_sql INTO sa_name, sa_description, sa_owner_id, sa_location_id, sa_root_id USING p_object_id;

    sa_link := NULL;  -- apps may set this via update_object_metadata; left NULL by default
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._denormalize_object IS 'Reads name/description/owner/location/root from a registered row.';

-- recompute_user_object
-- The single function that recomputes the user_object row for one
-- (app_id, user_id, object_type, object_id) tuple. It handles insert,
-- update, and delete based on whether grants survive. Also maintains the
-- companion object_users and user_users rows.
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
BEGIN
    v_id := claimius.composite_id(p_user_id::TEXT, p_object_type, p_object_id::TEXT);

    v_grants := claimius._build_grants_for_object(p_user_id, p_object_type, p_object_id);
    v_direct := claimius._direct_grant_for_object(p_user_id, p_object_type, p_object_id);

    -- No surviving access at all: delete the user_object row and companions
    IF jsonb_array_length(v_grants) = 0 AND NOT v_direct THEN
        DELETE FROM claimius.user_object WHERE id = v_id;
        DELETE FROM claimius.object_users
        WHERE TRUE AND object_id = p_object_id
          AND object_type = p_object_type AND user_id = p_user_id;
        PERFORM claimius._refresh_user_users(p_user_id);
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
                                   sa_updated_at = now();

    -- Maintain object_users
    INSERT INTO claimius.object_users (id, object_id, object_type, user_id)
    VALUES (
               claimius.composite_id(p_object_type, p_object_id::TEXT, p_user_id::TEXT),
               p_object_id, p_object_type, p_user_id
           )
    ON CONFLICT (id) DO UPDATE SET sa_updated_at = now();

    PERFORM claimius._refresh_user_users(p_user_id);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.recompute_user_object IS 'Recomputes user_object plus companions for one (app, user, object) tuple.';

-- _refresh_user_users
-- Recomputes user_users entries for one (app_id, viewer) pair from
-- object_users.
CREATE OR REPLACE FUNCTION claimius._refresh_user_users(p_viewer_id UUID)
    RETURNS VOID AS $$
BEGIN
    -- Remove existing rows for this viewer
    DELETE FROM claimius.user_users
    WHERE TRUE AND viewer_id = p_viewer_id;

    -- Insert one row per other user the viewer shares any object with
    INSERT INTO claimius.user_users (id, viewer_id, target_user_id, sharing_object_count, first_shared_at, last_shared_at)
    SELECT
        claimius.composite_id(p_viewer_id::TEXT, target.user_id::TEXT),
        p_viewer_id,
        target.user_id,
        count(*),
        min(target.sa_created_at),
        max(target.sa_updated_at)
    FROM claimius.object_users viewer
             JOIN claimius.object_users target
                  ON TRUE
                      AND target.object_id = viewer.object_id
                      AND target.object_type = viewer.object_type
                      AND target.user_id <> viewer.user_id
    WHERE TRUE AND viewer.user_id = p_viewer_id
    GROUP BY target.user_id;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._refresh_user_users IS 'Rebuilds user_users rows for one (app, viewer).';

-- _affected_users_for_object
-- Returns the list of users whose access to the given object could change.
-- Includes any user holding a relevant claim plus any user already in
-- object_users for that object.
CREATE OR REPLACE FUNCTION claimius._affected_users_for_object(
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS TABLE(user_id UUID) AS $$
BEGIN
    RETURN QUERY
        WITH all_users AS (
            -- existing object_users
            SELECT ou.user_id FROM claimius.object_users ou
            WHERE TRUE AND ou.object_id = p_object_id AND ou.object_type = p_object_type

            UNION

            -- users holding any user_claim in this app whose claim is bound
            -- directly to this object
            SELECT uc.user_id
            FROM claimius.user_claim uc
                     JOIN claimius.claim_object co
                          ON co.claim_id = uc.claim_id
                         AND TRUE
                         AND co.sa_deleted_at IS NULL
                         AND co.object_type = p_object_type
                         AND co.object_id = p_object_id
            WHERE TRUE
              AND uc.sa_deleted_at IS NULL

            UNION

            -- users holding any user_claim whose claim is bound to an ancestor
            -- of this object via the closure index
            SELECT uc.user_id
            FROM claimius.user_claim uc
                     JOIN claimius.claim_object co
                          ON co.claim_id = uc.claim_id
                         AND TRUE
                         AND co.sa_deleted_at IS NULL
                         AND co.inherits = TRUE
                     JOIN claimius.inheritance_info e
                          ON e.ancestor_type   = co.object_type
                         AND e.ancestor_id     = co.object_id
                         AND e.descendant_type = p_object_type
                         AND e.descendant_id   = p_object_id
                         AND e.depth > 0
            WHERE TRUE
              AND uc.sa_deleted_at IS NULL
        )
        SELECT DISTINCT au.user_id FROM all_users au;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius._affected_users_for_object IS 'Users whose access to the object could change.';
