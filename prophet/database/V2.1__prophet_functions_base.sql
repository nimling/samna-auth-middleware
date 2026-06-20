-- ============================================================================
-- Claimius V2.1 Prophet Base Functions
-- ----------------------------------------------------------------------------
-- write_audit carries app_id and is only called by prophet write paths (calc
-- triggers, merge_user). The disciple receives audit rows via sync and never
-- calls write_audit, so it has no disciple variant. The remaining base
-- helpers are app_id free and live in the shared set.
-- ============================================================================

-- write_audit
-- Inserts a row into claimius.audit. Used by audit triggers and by error
-- paths inside other triggers.
-- p_sa_created_by may be NULL for events with no clear actor (system
-- triggers, replay, denied access attempts).
DROP FUNCTION IF EXISTS claimius.write_audit(UUID, TEXT, TEXT, TEXT, UUID, UUID, UUID, TEXT);

CREATE OR REPLACE FUNCTION claimius.write_audit(
    p_app_id        UUID,
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
        app_id, operation, type, object_type, object_id,
        sa_owner_id, sa_created_by, message
    ) VALUES (
                 p_app_id, p_operation, p_type, p_object_type, p_object_id,
                 p_sa_owner_id, p_sa_created_by, p_message
             )
    RETURNING * INTO v_row;
    RETURN v_row;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.write_audit IS 'Inserts a row into the audit log and returns it.';
