DO $$
    BEGIN
        PERFORM claimius.upsert_internal_table_info();
        PERFORM claimius._restamp_created_by();
        RAISE NOTICE 'Claimius internal tables registered. Schema ready.';
        RAISE NOTICE 'Implementer must now call: upsert_disciple_state';
        RAISE NOTICE 'Then: upsert_table_info(VARIADIC <schema.table list>)';
    END $$;
