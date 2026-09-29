CREATE OR REPLACE FUNCTION claimius.ensure_app_client(
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
        v_claim_id := (claimius.create_claim(
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
        PERFORM claimius.assign_claim_object(
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

    PERFORM claimius.assign_claim_object(
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

    PERFORM claimius.ensure_app_user(
        p_user_id    => p_client_id,
        p_app_id     => p_app_id,
        p_first_name => p_name,
        p_last_name  => 'Client',
        p_email      => p_name || '@local',
        p_sa_created_by => p_sa_created_by
    );

    PERFORM claimius.assign_claim_user(
        p_app_id        => p_app_id,
        p_claim_id      => v_claim_id,
        p_user_id       => p_client_id,
        p_sa_owner_id   => v_org_id,
        p_sa_created_by => p_sa_created_by,
        p_reason        => 'Grant ' || p_name || ' its capability claim'
    );

    RETURN jsonb_build_object(
        'samna_client', jsonb_build_object('id', v_client_pk, 'client_id', p_client_id),
        'claim', jsonb_build_object('id', v_claim_id, 'name', p_claim_name),
        'organization', jsonb_build_object('id', v_org_id)
    );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.ensure_app_client(uuid, uuid, text, text, text, integer, uuid, text, jsonb) IS 'Idempotently provisions a machine client for an app with one capability claim. p_sa_access is a bitwise mask. p_scope, when given, is bound as an object-less capability payload the consuming service reads through get_access on a scope key, and it is where per operation limits live.';
