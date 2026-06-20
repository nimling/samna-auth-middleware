-- ============================================================================
-- Claimius V2.8 Disciple Write Functions (app_id free)
-- ----------------------------------------------------------------------------
-- The write surface for debug mode local writes. No app_id parameter on any
-- function: the disciple is single app and knows nothing about app_id. In
-- production these run under the prophet over HTTP; locally they fire the calc
-- triggers (dormant under replay_mode during sync).
-- ============================================================================
-- ============================================================================
-- Write functions: claim management
-- ============================================================================

-- create_claim
DROP FUNCTION IF EXISTS claimius.create_claim(UUID, TEXT, TEXT, INTEGER, UUID, UUID, UUID, BOOLEAN, BOOLEAN, claimius.claim_type);

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
    v_row claimius.claim;
BEGIN
    INSERT INTO claimius.claim (
        name, description, sa_access, inherits, type,
        sa_owner_id, sa_root_id, sa_created_by
    ) VALUES (
                 p_name, p_description, p_sa_access, p_inherits, p_type,
                 p_sa_owner_id, p_sa_root_id, p_sa_created_by
             ) RETURNING * INTO v_row;
    RETURN jsonb_build_object('claim', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.create_claim IS 'Creates a new claim. p_sa_access is a bitwise mask: 0x01 owner, 0x02 write, 0x04 read, 0x08 execute, 0x10 deny.';

-- update_claim
DROP FUNCTION IF EXISTS claimius.update_claim(UUID, TEXT, TEXT, INTEGER, BOOLEAN, BOOLEAN);

CREATE OR REPLACE FUNCTION claimius.update_claim(
    p_claim_id          UUID,
    p_name              TEXT DEFAULT NULL,
    p_description       TEXT DEFAULT NULL,
    p_sa_access         INTEGER DEFAULT NULL,
    p_inherits          BOOLEAN DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row claimius.claim;
BEGIN
    UPDATE claimius.claim SET
                              name        = coalesce(p_name, name),
                              description = coalesce(p_description, description),
                              sa_access   = coalesce(p_sa_access, sa_access),
                              inherits    = coalesce(p_inherits, inherits)
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

COMMENT ON FUNCTION claimius.assign_claim_object IS 'Binds a claim. With (object_type, object_id) p_scope keys are field names; without them p_scope keys are custom. p_sa_access is a bitwise mask.';

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
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._bulk_seed_user_object IS 'Seeds user_object for an existing table at registration time.';
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
    p_id                UUID DEFAULT NULL
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
        id, name, description, type,
        sa_level, sa_created_by
    ) VALUES (
                 coalesce(p_id, gen_random_uuid()), p_name, p_description, p_type,
                 0, p_actor_user_claim_id
             )
    RETURNING * INTO v_row;

    RETURN jsonb_build_object('organization', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.create_root_organization(UUID, TEXT, UUID, TEXT, TEXT, UUID) IS 'Creates a new root organization with self referencing sa_owner_id and sa_root_id. Idempotent on (name).';
-- Helper for prophet apps. When a user logs into an app for the first time,
-- creates the per app samna_user row keyed by (user_id, app_id). If the app
-- has a default claim configured (samna_app.claim_id), grants it to the user
-- as part of the same call. Idempotent: re-running returns the existing row
-- and existing user_claim.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.ensure_app_user(
    p_user_id    UUID,
    p_first_name TEXT DEFAULT NULL,
    p_last_name  TEXT DEFAULT NULL,
    p_user_name  TEXT DEFAULT NULL,
    p_user_image TEXT DEFAULT NULL,
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
    WHERE user_id = p_user_id  AND sa_deleted_at IS NULL;

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

    -- Look up the app's default claim
    SELECT a.claim_id, c.sa_owner_id
    INTO v_default_claim_id, v_default_claim_owner
    FROM claimius.samna_app a
             LEFT JOIN claimius.claim c ON c.id = a.claim_id AND c.sa_deleted_at IS NULL
    WHERE a.sa_deleted_at IS NULL
    LIMIT 1;

    -- Grant the default claim if one is configured. Idempotent: if the user
    -- already has a live user_claim for this claim, return the existing one.
    IF v_default_claim_id IS NOT NULL THEN
        SELECT * INTO v_uc_row FROM claimius.user_claim
        WHERE user_id = v_user_row.user_id
          
          AND claim_id = v_default_claim_id
          AND sa_deleted_at IS NULL
        LIMIT 1;

        IF NOT FOUND THEN
            INSERT INTO claimius.user_claim (
                claim_id, user_id, sa_owner_id, sa_created_by
            ) VALUES (
                         v_default_claim_id, v_user_row.user_id,
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

COMMENT ON FUNCTION claimius.ensure_app_user IS 'Creates per app samna_user row and grants the app default claim. Idempotent.';
