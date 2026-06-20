-- ============================================================================
-- Claimius V2.1 Disciple Base Functions (app_id free)
-- ----------------------------------------------------------------------------
-- write_audit without app_id. The disciple audit table has no app_id column.
-- Called by the calc trigger functions on their error path.
-- ============================================================================

CREATE OR REPLACE FUNCTION claimius.write_audit(
    p_operation     TEXT,
    p_type          TEXT,
    p_object_type   TEXT,
    p_object_id     UUID,
    p_sa_owner_id   UUID,
    p_sa_created_by UUID DEFAULT NULL,
    p_message       TEXT DEFAULT ''
) RETURNS claimius.audit AS $$
DECLARE
    v_row claimius.audit;
BEGIN
    INSERT INTO claimius.audit (
        operation, type, object_type, object_id,
        sa_owner_id, sa_created_by, message
    ) VALUES (
                 p_operation, p_type, p_object_type, p_object_id,
                 p_sa_owner_id, p_sa_created_by, p_message
             )
    RETURNING * INTO v_row;
    RETURN v_row;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.write_audit IS 'Inserts a row into the audit log and returns it. Disciple: no app_id.';
