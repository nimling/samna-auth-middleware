-- ============================================================================
-- Claimius V0.4 App Identity (prophet)
-- ----------------------------------------------------------------------------
-- get_app_id resolves the deployment own app from prophet_state, seeded from
-- the .env at init. It is defined before the registered tables because their
-- app_id columns DEFAULT to claimius.get_app_id(), and a DEFAULT expression
-- must resolve its function at CREATE TABLE time. The body is plpgsql, so the
-- referenced tables need not exist yet at function creation.
-- ============================================================================

CREATE OR REPLACE FUNCTION claimius.get_app_id()
    RETURNS UUID AS $$
DECLARE
    v_id UUID;
BEGIN
    SELECT a.id INTO v_id
    FROM claimius.prophet_state ps
             JOIN claimius.samna_app a ON a.slug = ps.system_app_slug
    WHERE ps.system_app_slug IS NOT NULL
      AND a.sa_deleted_at IS NULL
    LIMIT 1;

    IF v_id IS NULL THEN
        RAISE EXCEPTION 'get_app_id: deployment has no prophet system app registered.';
    END IF;

    RETURN v_id;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_app_id IS 'Returns the deployment own app id. Prophet/hybrid: system app.';
