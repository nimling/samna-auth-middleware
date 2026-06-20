-- ============================================================================
-- Claimius V4.0 Disciple Init
-- ----------------------------------------------------------------------------
-- Registers the internal claimius.* tables in table_info, completing schema
-- setup. After this the implementer calls init_disciple(<prophet app slug>)
-- and init_claimius_tables(...) with their app's table list.
-- ============================================================================

DO $$
    BEGIN
        PERFORM claimius.init_claimius_internal();
        RAISE NOTICE 'Claimius disciple internal tables registered. Schema ready.';
        RAISE NOTICE 'Implementer must now call: init_disciple(<prophet app slug>)';
        RAISE NOTICE 'Then: init_claimius_tables(VARIADIC <schema.table list>)';
    END $$;
