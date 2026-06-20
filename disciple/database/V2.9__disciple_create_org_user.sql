-- ============================================================================
-- Claimius V2.9 Disciple create_organization_user (app_id free)
-- ----------------------------------------------------------------------------
-- The middleware calls create_organization_user; in debug mode it runs here
-- locally. No app_id: the disciple is single app. Calls the disciple write
-- functions (no app_id) which fire calc triggers to materialize access.
-- ============================================================================

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
     WHERE id = p_owner_id AND sa_deleted_at IS NULL;

    IF v_root_id IS NULL THEN
        RAISE EXCEPTION 'owner_id invalid';
    END IF;

    IF p_caller_uc IS NOT NULL THEN
        SELECT user_id INTO v_caller_user_id
          FROM claimius.user_claim
         WHERE id = p_caller_uc AND sa_deleted_at IS NULL;
        IF v_caller_user_id IS NULL THEN
            RAISE EXCEPTION 'caller user_claim invalid';
        END IF;
    END IF;

    IF NOT p_upsert THEN
        IF v_user_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM claimius.samna_user
             WHERE user_id = v_user_id AND sa_deleted_at IS NULL
        ) THEN
            RAISE EXCEPTION 'user_id already exists' USING ERRCODE = '23505';
        END IF;
        IF v_external_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM claimius.samna_user
             WHERE external_id = v_external_id AND sa_deleted_at IS NULL
        ) THEN
            RAISE EXCEPTION 'external_id already exists' USING ERRCODE = '23505';
        END IF;
        INSERT INTO claimius.samna_user (
            user_id, first_name, last_name, user_name, user_image,
            email, phone, external_id, status, type
        ) VALUES (
            COALESCE(v_user_id, gen_random_uuid()),
            NULLIF(p_user ->> 'first_name', ''), NULLIF(p_user ->> 'last_name', ''),
            NULLIF(p_user ->> 'user_name', ''), NULLIF(p_user ->> 'user_image', ''),
            NULLIF(p_user ->> 'email', ''), NULLIF(p_user ->> 'phone', ''),
            v_external_id, COALESCE(NULLIF(p_user ->> 'status', ''), 'active')::claimius.user_status, 'user'
        ) RETURNING * INTO v_user_row;
    ELSE
        INSERT INTO claimius.samna_user (
            user_id, first_name, last_name, user_name, user_image,
            email, phone, external_id, status, type
        ) VALUES (
            COALESCE(v_user_id, gen_random_uuid()),
            NULLIF(p_user ->> 'first_name', ''), NULLIF(p_user ->> 'last_name', ''),
            NULLIF(p_user ->> 'user_name', ''), NULLIF(p_user ->> 'user_image', ''),
            NULLIF(p_user ->> 'email', ''), NULLIF(p_user ->> 'phone', ''),
            v_external_id, COALESCE(NULLIF(p_user ->> 'status', ''), 'active')::claimius.user_status, 'user'
        )
        ON CONFLICT (user_id) WHERE sa_deleted_at IS NULL DO UPDATE SET
            first_name = COALESCE(EXCLUDED.first_name, claimius.samna_user.first_name),
            last_name = COALESCE(EXCLUDED.last_name, claimius.samna_user.last_name),
            user_name = COALESCE(EXCLUDED.user_name, claimius.samna_user.user_name),
            user_image = COALESCE(EXCLUDED.user_image, claimius.samna_user.user_image),
            email = COALESCE(EXCLUDED.email, claimius.samna_user.email),
            phone = COALESCE(EXCLUDED.phone, claimius.samna_user.phone),
            external_id = COALESCE(EXCLUDED.external_id, claimius.samna_user.external_id),
            sa_updated_at = now()
        RETURNING * INTO v_user_row;
    END IF;

    SELECT * INTO v_existing_uc
      FROM claimius.user_claim
     WHERE claim_id = v_claim_id AND user_id = v_user_row.user_id
       AND sa_deleted_at IS NULL LIMIT 1;

    IF v_existing_uc.id IS NOT NULL THEN
        v_claim_user := jsonb_build_object('user_claim', to_jsonb(v_existing_uc));
    ELSE
        v_claim_user := claimius.assign_claim_user(
            p_claim_id => v_claim_id, p_user_id => v_user_row.user_id,
            p_sa_owner_id => p_owner_id, p_sa_created_by => p_caller_uc);
    END IF;

    SELECT * INTO v_existing_virtual
      FROM claimius.claim
     WHERE name = 'user:' || v_user_row.user_id::text
       AND type = 'virtual' AND sa_deleted_at IS NULL LIMIT 1;

    IF v_existing_virtual.id IS NOT NULL THEN
        v_virtual_claim := jsonb_build_object('claim', to_jsonb(v_existing_virtual));
        v_virtual_claim_id := v_existing_virtual.id;
    ELSE
        v_virtual_claim := claimius.create_claim(
            p_name => 'user:' || v_user_row.user_id::text,
            p_description => NULL, p_sa_access => 15, p_sa_owner_id => p_owner_id,
            p_sa_root_id => v_root_id, p_sa_created_by => p_caller_uc, p_inherits => TRUE, p_type => 'virtual');
        v_virtual_claim_id := (v_virtual_claim -> 'claim' ->> 'id')::UUID;
    END IF;

    SELECT * INTO v_existing_obj
      FROM claimius.claim_object
     WHERE claim_id = v_virtual_claim_id AND object_id = v_user_row.user_id
       AND object_type = 'claimius.samna_user' AND sa_deleted_at IS NULL LIMIT 1;

    IF v_existing_obj.id IS NOT NULL THEN
        v_claim_obj := jsonb_build_object('claim_object', to_jsonb(v_existing_obj));
    ELSE
        v_claim_obj := claimius.assign_claim_object(
            p_claim_id => v_virtual_claim_id, p_sa_owner_id => p_owner_id,
            p_sa_root_id => v_root_id, p_sa_created_by => p_caller_uc, p_object_id => v_user_row.user_id,
            p_object_type => 'claimius.samna_user');
    END IF;

    IF v_caller_user_id IS NOT NULL THEN
        SELECT * INTO v_existing_caller
          FROM claimius.user_claim
         WHERE claim_id = v_virtual_claim_id AND user_id = v_caller_user_id
           AND sa_deleted_at IS NULL LIMIT 1;
        IF v_existing_caller.id IS NOT NULL THEN
            v_caller_user_claim := jsonb_build_object('user_claim', to_jsonb(v_existing_caller));
        ELSE
            v_caller_user_claim := claimius.assign_claim_user(
                p_claim_id => v_virtual_claim_id, p_user_id => v_caller_user_id,
                p_sa_owner_id => p_owner_id, p_sa_created_by => p_caller_uc);
        END IF;
    END IF;

    RETURN jsonb_build_object(
        'samna_user', to_jsonb(v_user_row),
        'user_claim', v_claim_user -> 'user_claim',
        'virtual_claim', v_virtual_claim -> 'claim',
        'claim_object', v_claim_obj -> 'claim_object',
        'caller_user_claim', v_caller_user_claim -> 'user_claim'
    );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.create_organization_user IS 'Creates or upserts a samna_user, grants the claim, mints the per user virtual claim and bindings. Disciple single app, no app_id.';
