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
