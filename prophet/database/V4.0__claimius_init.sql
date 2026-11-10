DO $$
    BEGIN
        PERFORM claimius.upsert_internal_table_info();
        PERFORM claimius._attach_self_root_trigger('claimius.samna_app');
        PERFORM claimius._restamp_created_by();
        RAISE NOTICE 'Claimius internal tables registered. Schema ready.';
        RAISE NOTICE 'Implementer must now call: upsert_system_app | upsert_disciple_state | upsert_prophet_peer';
        RAISE NOTICE 'Then: upsert_table_info(VARIADIC <schema.table list>)';
    END $$;

INSERT INTO claimius.samna_user (user_id, app_id, status, type, first_name)
SELECT claimius.anonymous_user_id(), a.id, 'active', 'anonymous', 'Public'
FROM claimius.samna_app a
WHERE a.sa_deleted_at IS NULL
ON CONFLICT (user_id, app_id) WHERE sa_deleted_at IS NULL DO NOTHING;
