-- ============================================================================
-- update_current_timestamp (INTERNAL)
-- Sets sa_updated_at on every UPDATE. Applied to all samna_auth tables.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.update_current_timestamp()
RETURNS TRIGGER AS $$
BEGIN
   NEW.sa_updated_at = CURRENT_TIMESTAMP;
   RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER update_samna_user_sa_updated_at
    BEFORE UPDATE ON samna_auth.samna_user
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();

CREATE OR REPLACE TRIGGER update_claim_sa_updated_at
    BEFORE UPDATE ON samna_auth.claim
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();

CREATE OR REPLACE TRIGGER update_claim_object_sa_updated_at
    BEFORE UPDATE ON samna_auth.claim_object
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();

CREATE OR REPLACE TRIGGER update_user_claim_sa_updated_at
    BEFORE UPDATE ON samna_auth.user_claim
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();

CREATE OR REPLACE TRIGGER update_samna_auth_organization_sa_updated_at
    BEFORE UPDATE ON samna_auth.organization
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();

CREATE OR REPLACE TRIGGER update_samna_auth_location_sa_updated_at
    BEFORE UPDATE ON samna_auth.location
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();

CREATE OR REPLACE TRIGGER update_sa_user_relation_sa_updated_at
    BEFORE UPDATE ON samna_auth.user_relation
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();

-- ============================================================================
-- audit_trigger_function (INTERNAL)
-- Writes audit rows for INSERT/UPDATE on samna_auth tables.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.audit_trigger_function()
    RETURNS TRIGGER AS $$
DECLARE
    operation_type VARCHAR;
    object_owner_id UUID;
    object_created_by UUID;
    raw_query TEXT;
BEGIN
    IF TG_OP = 'INSERT' THEN
        operation_type := 'insert';
    ELSIF TG_OP = 'UPDATE' THEN
        operation_type := 'update';
    END IF;

    raw_query := TG_OP || ' on ' || TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME || ' id=' || NEW.id::text;

    BEGIN
        object_owner_id := NEW.sa_owner_id;
    EXCEPTION WHEN OTHERS THEN
        object_owner_id := NULL;
    END;

    BEGIN
        object_created_by := NEW.sa_created_by;
    EXCEPTION WHEN OTHERS THEN
        object_created_by := NULL;
    END;

    IF object_created_by IS NOT NULL AND object_owner_id IS NOT NULL THEN
        INSERT INTO samna_auth.audit (
            operation,
            type,
            object_type,
            object_id,
            sa_created_by,
            sa_owner_id
        ) VALUES (
            raw_query,
            operation_type,
            TG_TABLE_NAME,
            NEW.id,
            object_created_by,
            object_owner_id
        );
    END IF;

    RETURN NEW;
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'Error in samna_auth audit trigger: %', SQLERRM;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER sa_audit_organization
    AFTER INSERT OR UPDATE ON samna_auth.organization
    FOR EACH ROW EXECUTE FUNCTION samna_auth.audit_trigger_function();

CREATE OR REPLACE TRIGGER sa_audit_location
    AFTER INSERT OR UPDATE ON samna_auth.location
    FOR EACH ROW EXECUTE FUNCTION samna_auth.audit_trigger_function();

CREATE OR REPLACE TRIGGER sa_audit_claim
    AFTER INSERT OR UPDATE ON samna_auth.claim
    FOR EACH ROW EXECUTE FUNCTION samna_auth.audit_trigger_function();

CREATE OR REPLACE TRIGGER sa_audit_claim_object
    AFTER INSERT OR UPDATE ON samna_auth.claim_object
    FOR EACH ROW EXECUTE FUNCTION samna_auth.audit_trigger_function();

CREATE OR REPLACE TRIGGER sa_audit_user_claim
    AFTER INSERT OR UPDATE ON samna_auth.user_claim
    FOR EACH ROW EXECUTE FUNCTION samna_auth.audit_trigger_function();

-- ============================================================================
-- tg_object_mutation (INTERNAL)
-- Attached to every application table registered via init_auth_tables.
-- Recomputes user_object for affected users when sa_* columns change.
-- On error: write succeeds, failed computation queued to reconcile_queue.
-- Skips when samna_auth.replay_mode is set (disciple applying sync payload).
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.tg_object_mutation()
RETURNS TRIGGER AS $$
DECLARE
    v_table text := TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME;
    v_object_type text := TG_TABLE_NAME;
    v_object_id uuid;
    v_user_id uuid;
    v_other_user_id uuid;
    v_affected_users uuid[];
    v_row record;
BEGIN
    IF current_setting('samna_auth.replay_mode', true) = 'true' THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF TG_TABLE_NAME = 'samna_user' THEN
        EXECUTE format('SELECT ($1).user_id') INTO v_object_id USING CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
    ELSE
        v_object_id := CASE WHEN TG_OP = 'DELETE' THEN OLD.id ELSE NEW.id END;
    END IF;

    IF TG_OP = 'UPDATE' THEN
        IF OLD.sa_owner_id IS NOT DISTINCT FROM NEW.sa_owner_id
           AND OLD.sa_deleted_at IS NOT DISTINCT FROM NEW.sa_deleted_at
        THEN
            RETURN NEW;
        END IF;
    END IF;

    BEGIN
        v_affected_users := samna_auth.find_affected_users_for_row(
            v_table,
            v_object_type,
            v_object_id,
            CASE WHEN TG_OP = 'DELETE' THEN OLD.sa_owner_id ELSE NEW.sa_owner_id END
        );

        IF v_affected_users IS NOT NULL THEN
            FOREACH v_user_id IN ARRAY v_affected_users LOOP
                PERFORM samna_auth.reconcile_user_object(
                    v_user_id,
                    v_object_id,
                    v_object_type
                );
            END LOOP;

            IF TG_OP IN ('INSERT', 'UPDATE') THEN
                FOREACH v_user_id IN ARRAY v_affected_users LOOP
                    FOREACH v_other_user_id IN ARRAY v_affected_users LOOP
                        IF v_user_id <> v_other_user_id THEN
                            INSERT INTO samna_auth.user_users (id, viewer_id, target_user_id)
                            VALUES (
                                samna_auth.composite_id(v_user_id::text, v_other_user_id::text),
                                v_user_id,
                                v_other_user_id
                            )
                            ON CONFLICT (id) DO UPDATE SET
                                last_shared_at = now(),
                                sharing_object_count = samna_auth.user_users.sharing_object_count + 1;
                        END IF;
                    END LOOP;
                END LOOP;
            END IF;

            INSERT INTO samna_auth.user_object_event (object_type, obj)
            VALUES (
                v_object_type,
                to_jsonb(CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END)
            );
        END IF;

    EXCEPTION WHEN OTHERS THEN
        RAISE NOTICE 'samna_auth tg_object_mutation error: %. Queuing for reconcile.', SQLERRM;
        BEGIN
            INSERT INTO samna_auth.reconcile_queue (id, user_id, object_id, object_type)
            SELECT
                samna_auth.composite_id(u::text, v_object_id::text, v_object_type),
                u,
                v_object_id,
                v_object_type
            FROM unnest(v_affected_users) u
            ON CONFLICT (id) DO NOTHING;
        EXCEPTION WHEN OTHERS THEN
            RAISE NOTICE 'samna_auth reconcile queue insert failed: %', SQLERRM;
        END;
    END;

    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- tg_claim_mutation (INTERNAL)
-- Attached to user_claim, claim, and claim_object.
-- Recomputes user_object when claims change.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.tg_claim_mutation()
RETURNS TRIGGER AS $$
DECLARE
    v_user_id uuid;
    v_claim_id uuid;
    v_reachable record;
BEGIN
    IF current_setting('samna_auth.replay_mode', true) = 'true' THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    BEGIN
        IF TG_TABLE_NAME = 'user_claim' THEN
            IF TG_OP = 'INSERT' OR (TG_OP = 'UPDATE' AND OLD.sa_deleted_at IS NULL AND NEW.sa_deleted_at IS NOT NULL) THEN
                v_user_id := COALESCE(NEW.user_id, OLD.user_id);
                v_claim_id := COALESCE(NEW.claim_id, OLD.claim_id);

                FOR v_reachable IN
                    SELECT object_id, object_type
                    FROM samna_auth.expand_claim_reachable_objects(v_claim_id)
                LOOP
                    PERFORM samna_auth.reconcile_user_object(v_user_id, v_reachable.object_id, v_reachable.object_type);
                END LOOP;
            END IF;

        ELSIF TG_TABLE_NAME = 'claim' THEN
            IF TG_OP = 'UPDATE' AND OLD.sa_access_level IS DISTINCT FROM NEW.sa_access_level THEN
                UPDATE samna_auth.user_object
                SET sa_access_level = NEW.sa_access_level, sa_updated_at = now()
                WHERE claim_id = NEW.id;
            END IF;

        ELSIF TG_TABLE_NAME = 'claim_object' THEN
            IF TG_OP IN ('INSERT', 'UPDATE') THEN
                FOR v_user_id IN
                    SELECT DISTINCT samna_auth.user_object.user_id FROM samna_auth.user_object WHERE samna_auth.user_object.claim_id = NEW.claim_id
                LOOP
                    PERFORM samna_auth.reconcile_user_object(v_user_id, NEW.object_id, NEW.object_type);
                END LOOP;
            END IF;
        END IF;

        INSERT INTO samna_auth.user_object_event (object_type, obj)
        VALUES (TG_TABLE_NAME, to_jsonb(COALESCE(NEW, OLD)));

    EXCEPTION WHEN OTHERS THEN
        RAISE NOTICE 'samna_auth tg_claim_mutation error: %', SQLERRM;
    END;

    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER sa_claim_mutation_claim
    AFTER INSERT OR UPDATE ON samna_auth.claim
    FOR EACH ROW EXECUTE FUNCTION samna_auth.tg_claim_mutation();

CREATE OR REPLACE TRIGGER sa_claim_mutation_claim_object
    AFTER INSERT OR UPDATE ON samna_auth.claim_object
    FOR EACH ROW EXECUTE FUNCTION samna_auth.tg_claim_mutation();

CREATE OR REPLACE TRIGGER sa_claim_mutation_user_claim
    AFTER INSERT OR UPDATE ON samna_auth.user_claim
    FOR EACH ROW EXECUTE FUNCTION samna_auth.tg_claim_mutation();

CREATE OR REPLACE TRIGGER update_samna_client_sa_updated_at
    BEFORE UPDATE ON samna_auth.samna_client
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();

-- ============================================================================
-- tg_hierarchy_mutation (INTERNAL)
-- Attached to organization and location. Fires when sa_owner_id or
-- sa_parent_id changes, recomputing access for all descendant objects.
-- ============================================================================
CREATE OR REPLACE FUNCTION samna_auth.tg_hierarchy_mutation()
RETURNS TRIGGER AS $$
DECLARE
    v_descendant record;
    v_user_id uuid;
    v_affected_users uuid[];
BEGIN
    IF current_setting('samna_auth.replay_mode', true) = 'true' THEN
        RETURN NEW;
    END IF;

    IF TG_OP != 'UPDATE' THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF TG_TABLE_NAME = 'organization' THEN
        IF OLD.sa_owner_id IS NOT DISTINCT FROM NEW.sa_owner_id THEN
            RETURN NEW;
        END IF;
    ELSIF TG_TABLE_NAME = 'location' THEN
        IF OLD.sa_parent_id IS NOT DISTINCT FROM NEW.sa_parent_id THEN
            RETURN NEW;
        END IF;
    END IF;

    BEGIN
        IF TG_TABLE_NAME = 'organization' THEN
            FOR v_descendant IN
                SELECT child.id FROM samna_auth.get_child_organizations(NEW.id) child
            LOOP
                v_affected_users := ARRAY(
                    SELECT DISTINCT samna_auth.user_object.user_id
                    FROM samna_auth.user_object
                    WHERE samna_auth.user_object.object_id = v_descendant.id AND samna_auth.user_object.object_type = 'organization'
                );
                IF v_affected_users IS NOT NULL THEN
                    FOREACH v_user_id IN ARRAY v_affected_users LOOP
                        PERFORM samna_auth.reconcile_user_object(v_user_id, v_descendant.id, 'organization');
                    END LOOP;
                END IF;
            END LOOP;

        ELSIF TG_TABLE_NAME = 'location' THEN
            FOR v_descendant IN
                SELECT child.id FROM samna_auth.get_child_locations(NEW.id) child
            LOOP
                v_affected_users := ARRAY(
                    SELECT DISTINCT samna_auth.user_object.user_id
                    FROM samna_auth.user_object
                    WHERE samna_auth.user_object.object_id = v_descendant.id AND samna_auth.user_object.object_type = 'location'
                );
                IF v_affected_users IS NOT NULL THEN
                    FOREACH v_user_id IN ARRAY v_affected_users LOOP
                        PERFORM samna_auth.reconcile_user_object(v_user_id, v_descendant.id, 'location');
                    END LOOP;
                END IF;
            END LOOP;
        END IF;

        INSERT INTO samna_auth.user_object_event (object_type, obj)
        VALUES (TG_TABLE_NAME, to_jsonb(NEW));

    EXCEPTION WHEN OTHERS THEN
        RAISE NOTICE 'samna_auth tg_hierarchy_mutation error: %', SQLERRM;
    END;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER sa_hierarchy_mutation_organization
    AFTER UPDATE OF sa_owner_id ON samna_auth.organization
    FOR EACH ROW EXECUTE FUNCTION samna_auth.tg_hierarchy_mutation();

CREATE OR REPLACE TRIGGER sa_hierarchy_mutation_location
    AFTER UPDATE OF sa_parent_id ON samna_auth.location
    FOR EACH ROW EXECUTE FUNCTION samna_auth.tg_hierarchy_mutation();

CREATE OR REPLACE FUNCTION samna_auth.tg_search_index()
RETURNS TRIGGER AS $$
DECLARE
    v_vector TSVECTOR;
    v_object_id UUID;
BEGIN
    IF TG_TABLE_NAME = 'samna_user' THEN
        EXECUTE format('SELECT ($1).user_id') INTO v_object_id USING CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
    ELSE
        v_object_id := CASE WHEN TG_OP = 'DELETE' THEN OLD.id ELSE NEW.id END;
    END IF;

    IF TG_OP = 'DELETE' THEN
        DELETE FROM samna_auth.search_index
        WHERE object_id = v_object_id AND object_type = TG_TABLE_NAME;
        RETURN OLD;
    END IF;

    IF TG_TABLE_NAME = 'samna_user' THEN
        v_vector := setweight(to_tsvector('english', COALESCE(NEW.first_name, '') || ' ' || COALESCE(NEW.last_name, '')), 'A') ||
                    setweight(to_tsvector('english', COALESCE(NEW.email, '')), 'B');
    ELSE
        v_vector := setweight(to_tsvector('english', COALESCE(NEW.name, '')), 'A') ||
                    setweight(to_tsvector('english', COALESCE(NEW.description, '')), 'B');
    END IF;

    INSERT INTO samna_auth.search_index (object_id, object_type, search_vector)
    VALUES (v_object_id, TG_TABLE_NAME, v_vector)
    ON CONFLICT (object_id, object_type) DO UPDATE
    SET search_vector = EXCLUDED.search_vector;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER sa_search_index_samna_user
    AFTER INSERT OR UPDATE ON samna_auth.samna_user
    FOR EACH ROW EXECUTE FUNCTION samna_auth.tg_search_index();

CREATE OR REPLACE TRIGGER sa_search_index_claim
    AFTER INSERT OR UPDATE ON samna_auth.claim
    FOR EACH ROW EXECUTE FUNCTION samna_auth.tg_search_index();

CREATE OR REPLACE TRIGGER sa_search_index_organization
    AFTER INSERT OR UPDATE ON samna_auth.organization
    FOR EACH ROW EXECUTE FUNCTION samna_auth.tg_search_index();

CREATE OR REPLACE TRIGGER sa_search_index_location
    AFTER INSERT OR UPDATE ON samna_auth.location
    FOR EACH ROW EXECUTE FUNCTION samna_auth.tg_search_index();
