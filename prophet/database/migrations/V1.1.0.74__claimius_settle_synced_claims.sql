-- Settle synced claim areas through access_diff instead of a full recompute.
--
-- The disciple sync layer recomputed access over every registered external
-- table and every row in it whenever a claim payload arrived, wrote each row
-- whether or not it changed, and did it with event emission switched on. One
-- claim binding produced hundreds of thousands of outbox rows nothing consumed.
--
-- Three changes converge on the same shape the rest of claimius already uses:
-- recompute_user_object stops writing a row it did not change, emit_sync_event
-- publishes only where a consumer is registered, and the sync layer marks the
-- claim area in access_diff for the next read to settle.

CREATE OR REPLACE FUNCTION claimius.recompute_user_object(
    p_app_id        UUID,
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

    v_id := claimius.composite_id(p_app_id::TEXT, p_user_id::TEXT, p_object_type, p_object_id::TEXT);

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
            WHERE app_id = p_app_id AND object_id = p_object_id
              AND object_type = p_object_type AND user_id = p_user_id;
            RETURN;
        END IF;
    END IF;

    v_grants := claimius._build_grants_for_object(p_user_id, p_app_id, p_object_type, p_object_id);
    v_direct := claimius._direct_grant_for_object(p_user_id, p_app_id, p_object_type, p_object_id);

    -- No surviving access at all: delete the user_object row and companions
    IF jsonb_array_length(v_grants) = 0 AND NOT v_direct THEN
        DELETE FROM claimius.user_object WHERE id = v_id;
        DELETE FROM claimius.object_users
        WHERE app_id = p_app_id AND object_id = p_object_id
          AND object_type = p_object_type AND user_id = p_user_id;
        RETURN;
    END IF;

    IF v_direct THEN
        v_eff_access := 15;
    ELSE
        v_eff_access := 0;
    END IF;
    FOR v_grant_obj IN SELECT * FROM jsonb_array_elements(v_grants) LOOP
        v_eff_access := v_eff_access | (v_grant_obj ->> 'access')::INTEGER;
    END LOOP;

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

    SELECT * INTO v_norm FROM claimius._denormalize_object(p_object_type, p_object_id);

    v_search_vec :=
            setweight(to_tsvector('simple', coalesce(v_norm.sa_name, '')), 'A') ||
            setweight(to_tsvector('simple', coalesce(v_norm.sa_description, '')), 'B');

    INSERT INTO claimius.user_object (
        id, app_id, user_id, object_id, object_type,
        sa_access, scope, grants, direct_grant,
        sa_owner_id, sa_location_id, sa_root_id,
        sa_name, sa_description, sa_link, search_vector
    ) VALUES (
                 v_id, p_app_id, p_user_id, p_object_id, p_object_type,
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

    INSERT INTO claimius.object_users (id, app_id, object_id, object_type, user_id)
    VALUES (
               claimius.composite_id(p_app_id::TEXT, p_object_type, p_object_id::TEXT, p_user_id::TEXT),
               p_app_id, p_object_id, p_object_type, p_user_id
           )
    ON CONFLICT (id) DO NOTHING;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION claimius.emit_sync_event()
    RETURNS TRIGGER AS $$
DECLARE
    v_op    claimius.sync_operation;
    v_type  TEXT;
    v_payload JSONB;
BEGIN
    IF claimius.in_replay_mode() THEN
        RETURN coalesce(NEW, OLD);
    END IF;

    -- sync_event is an outbox. A deployment with no registered consumer in
    -- prophet_state publishes to nobody, so the row would never be read and
    -- never be trimmed. A pure disciple is exactly that case.
    IF NOT EXISTS (SELECT 1 FROM claimius.prophet_state) THEN
        RETURN coalesce(NEW, OLD);
    END IF;

    v_type := TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME;

    IF TG_OP = 'DELETE' THEN
        v_op := 'delete';
        v_payload := jsonb_build_object('id', (to_jsonb(OLD) ->> 'id'), 'app_id', (to_jsonb(OLD) ->> 'app_id'));
    ELSE
        v_op := 'upsert';
        v_payload := to_jsonb(NEW);
    END IF;

    INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
    VALUES (v_op, v_type, v_payload, (v_payload ->> 'app_id')::uuid);

    RETURN coalesce(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

-- ----------------------------------------------------------------------------
-- trim_sync_event
-- Removes outbox rows every registered consumer has already applied.
-- prophet_state.last_applied_seq is the per app cursor, advanced when an app
-- asks for what comes after it, so a row at or below the minimum across the
-- apps still in contact is owed to nobody. An app whose last contact is older
-- than p_stale_after is treated as gone rather than allowed to pin the
-- watermark forever, which is how an outbox grows without bound while looking
-- healthy. With no registered consumer nothing is published and nothing is
-- removed. Returns the number of rows deleted.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION claimius.trim_sync_event(
    p_stale_after   INTERVAL DEFAULT INTERVAL '7 days'
) RETURNS BIGINT AS $$
DECLARE
    v_watermark BIGINT;
    v_deleted   BIGINT;
BEGIN
    SELECT min(last_applied_seq) INTO v_watermark
    FROM claimius.prophet_state
    WHERE last_contact_at IS NOT NULL
      AND last_contact_at > now() - p_stale_after;

    IF v_watermark IS NULL OR v_watermark <= 0 THEN
        RETURN 0;
    END IF;

    DELETE FROM claimius.sync_event WHERE seq <= v_watermark;
    GET DIAGNOSTICS v_deleted = ROW_COUNT;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.trim_sync_event(INTERVAL) IS 'Deletes sync_event rows at or below the lowest cursor among consumers still in contact. Run on a schedule.';
