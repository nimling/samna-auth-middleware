DO $$
    BEGIN
        PERFORM claimius.upsert_internal_table_info();
        PERFORM claimius._attach_self_root_trigger('claimius.samna_app');
        PERFORM claimius._restamp_created_by();
        RAISE NOTICE 'Claimius internal tables registered. Schema ready.';
        RAISE NOTICE 'Implementer must now call: upsert_system_app | upsert_disciple_state | upsert_prophet_peer';
        RAISE NOTICE 'Then: upsert_table_info(VARIADIC <schema.table list>)';
    END $$;

INSERT INTO claimius.samna_user (user_id, status, type, first_name)
SELECT claimius.anonymous_user_id(), 'active', 'anonymous', 'Public'
WHERE EXISTS (SELECT 1 FROM claimius.samna_app a WHERE a.sa_deleted_at IS NULL)
ON CONFLICT (user_id) WHERE sa_deleted_at IS NULL DO NOTHING;

INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
SELECT 'upsert', 'claimius.samna_user', to_jsonb(u) - 'txid', a.app_id
FROM claimius.samna_user u
    CROSS JOIN LATERAL claimius.apps_for_object('claimius.samna_user', u.user_id) AS a(app_id)
WHERE u.sa_deleted_at IS NULL
  AND u.user_id <> claimius.anonymous_user_id()
ORDER BY u.sa_created_at;
