-- ============================================================================
-- Claimius V4.0 Init
-- ----------------------------------------------------------------------------
-- Final step in claimius's own migration set. Registers the internal
-- claimius.* tables in table_info, completing schema setup, and rewrites the
-- sa_created_by an older set stamped with a user id where it is unambiguous.
--
-- After this file runs, the schema is "ready" for the implementing app to:
--   1. Run upsert_disciple_state
--   2. Run upsert_table_info(...) with their app's table list
--
-- The implementer's migration tooling is responsible for those calls. They
-- are NOT included in claimius's own migrations.
-- ============================================================================

DO $$
    BEGIN
        PERFORM claimius.upsert_internal_table_info();
        PERFORM claimius._restamp_created_by();
        RAISE NOTICE 'Claimius internal tables registered. Schema ready.';
        RAISE NOTICE 'Implementer must now call: upsert_disciple_state';
        RAISE NOTICE 'Then: upsert_table_info(VARIADIC <schema.table list>)';
    END $$;