-- ============================================================================
-- Claimius V2.5 Disciple Init Functions
-- ----------------------------------------------------------------------------
-- Registration and bootstrap for a disciple. Registration attaches only the
-- self root triggers, never calc triggers, and never bulk seeds user_object:
-- the materialized state arrives by sync. Bootstrap records the upstream
-- prophet app slug and grants the disciple roles.
-- ============================================================================

-- init_claimius_internal
CREATE OR REPLACE FUNCTION claimius.init_claimius_internal()
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
        'claimius.user_relation'
        ];
    v_t TEXT;
BEGIN
    FOREACH v_t IN ARRAY v_internal_tables LOOP
            PERFORM claimius._register_table_info(v_t, TRUE);
        END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.init_claimius_internal IS 'Registers internal claimius.* tables in table_info. Called once at schema bootstrap.';

-- init_claimius_tables
-- Registers external tables and attaches the self root trigger. The disciple
-- does not attach calc triggers and does not bulk seed user_object: access
-- state is materialized on the prophet and applied here by sync.
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
    SELECT array_agg(object_type) INTO v_existing
    FROM claimius.table_info WHERE NOT is_internal;

    v_new := p_object_types;

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
                DELETE FROM claimius.object_users WHERE object_type = v_old_object_type;
                DELETE FROM claimius.reconcile_queue WHERE object_type = v_old_object_type;
                RAISE NOTICE 'Deregistered table %', v_old_object_type;
            END LOOP;
    END IF;

    FOREACH v_object_type IN ARRAY v_new LOOP
            IF position('.' IN v_object_type) = 0 THEN
                v_object_type := 'public.' || v_object_type;
            END IF;
            PERFORM claimius._register_table_info(v_object_type, FALSE);
            PERFORM claimius._attach_calc_trigger(v_object_type);
            PERFORM claimius._attach_self_root_trigger(v_object_type);
            RAISE NOTICE 'Registered table %', v_object_type;
        END LOOP;

    -- Bulk seed materialized state for any registered table with existing data.
    -- Runs under replay_mode so calc stays quiet during the rebuild; the seed
    -- helpers materialize user_object directly.
    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
    BEGIN
        FOREACH v_object_type IN ARRAY v_new LOOP
                IF position('.' IN v_object_type) = 0 THEN
                    v_object_type := 'public.' || v_object_type;
                END IF;
                PERFORM claimius._bulk_seed_user_object(v_object_type);
            END LOOP;
    EXCEPTION WHEN OTHERS THEN
        PERFORM set_config('claimius.replay_mode', 'false', TRUE);
        RAISE;
    END;
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.init_claimius_tables IS 'Registers external tables on a disciple. Attaches calc and self root triggers; bulk seeds. Idempotent.';

-- _grant_disciple_roles
CREATE OR REPLACE FUNCTION claimius._grant_disciple_roles()
    RETURNS VOID AS $$
BEGIN
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_reader';
    EXECUTE 'REVOKE EXECUTE ON FUNCTION
             claimius.init_disciple, claimius.init_claimius_tables, claimius.init_claimius_internal
             FROM claimius_reader';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.user_object, claimius.object_users, claimius.user_users,
             claimius.disciple_state, claimius.samna_user, claimius.samna_client,
             claimius.samna_secret, claimius.samna_app, claimius.organization, claimius.location,
             claimius.claim, claimius.user_claim, claimius.claim_object,
             claimius.object_field, claimius.user_field, claimius.user_relation,
             claimius.audit, claimius.inheritance_info, claimius.table_info,
             claimius.claim_graph_cache
             TO claimius_disciple_client';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.claim_graph_cache TO claimius_reader';
    EXECUTE 'GRANT SELECT ON ALL TABLES IN SCHEMA claimius TO claimius_disciple_client';
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_disciple_client';
END;
$$ LANGUAGE plpgsql;

-- init_disciple
CREATE OR REPLACE FUNCTION claimius.init_disciple(
    p_disciple_app_slug TEXT
)
    RETURNS JSONB AS $$
BEGIN
    IF p_disciple_app_slug IS NULL OR p_disciple_app_slug = '' THEN
        RAISE EXCEPTION 'init_disciple: p_disciple_app_slug is required.';
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

COMMENT ON FUNCTION claimius.init_disciple IS 'Bootstrap as a disciple. Requires the upstream prophet app slug. State arrives from prophet via sync.';
