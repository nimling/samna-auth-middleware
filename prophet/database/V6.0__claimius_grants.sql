CREATE OR REPLACE FUNCTION claimius._grant_prophet_roles()
    RETURNS VOID AS $$
BEGIN
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_reader';
    EXECUTE 'REVOKE EXECUTE ON FUNCTION claimius.insert_claim, claimius.update_claim, claimius.delete_claim,
             claimius.insert_user_claim, claimius.update_user_claim, claimius.delete_user_claim,
             claimius.insert_claim_object, claimius.update_claim_object, claimius.delete_claim_object,
             claimius.merge_user, claimius.migrate_root,
             claimius.upsert_system_app, claimius.upsert_disciple_state, claimius.upsert_prophet_peer, claimius.upsert_app,
             claimius.insert_app_handover, claimius.update_app_handover, claimius.apply_peer_event, claimius.insert_secret, claimius.upsert_peer_grant, claimius.upsert_guest_user,
             claimius.upsert_table_info, claimius.delete_table_info, claimius.upsert_internal_table_info,
             claimius.recompute_state
             FROM claimius_reader';

    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_writer';
    EXECUTE 'GRANT INSERT, UPDATE ON
             claimius.samna_app, claimius.samna_user, claimius.samna_client, claimius.samna_secret,
             claimius.organization, claimius.location,
             claimius.object_field, claimius.user_field, claimius.user_relation,
             claimius.notification, claimius.prophet_state, claimius.sync_consumer, claimius.peer_grant
             TO claimius_writer';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.claim_graph_cache TO claimius_writer';
    EXECUTE 'GRANT UPDATE ON claimius.prophet_peer TO claimius_writer';
    EXECUTE 'REVOKE INSERT, UPDATE, DELETE ON claimius.claim_graph_cache FROM claimius_reader';
    EXECUTE 'GRANT SELECT ON ALL TABLES IN SCHEMA claimius TO claimius_writer';
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._grant_prophet_roles() IS 'The role grants of a prophet, run by V6.0 on every build and replay; never called by a consumer.';

CREATE OR REPLACE FUNCTION claimius._grant_disciple_roles()
    RETURNS VOID AS $$
BEGIN
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_reader';
    EXECUTE 'REVOKE EXECUTE ON FUNCTION claimius.insert_claim, claimius.update_claim, claimius.delete_claim,
             claimius.insert_user_claim, claimius.update_user_claim, claimius.delete_user_claim,
             claimius.insert_claim_object, claimius.update_claim_object, claimius.delete_claim_object,
             claimius.merge_user, claimius.migrate_root,
             claimius.upsert_system_app, claimius.upsert_disciple_state, claimius.upsert_prophet_peer, claimius.upsert_app,
             claimius.insert_app_handover, claimius.update_app_handover, claimius.apply_peer_event, claimius.insert_secret, claimius.upsert_peer_grant, claimius.upsert_guest_user,
             claimius.upsert_table_info, claimius.delete_table_info, claimius.upsert_internal_table_info,
             claimius.recompute_state
             FROM claimius_reader';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.user_object, claimius.object_info, claimius.user_users,
             claimius.disciple_state, claimius.samna_user, claimius.samna_client,
             claimius.samna_secret, claimius.samna_app, claimius.organization, claimius.location,
             claimius.claim, claimius.user_claim, claimius.claim_object,
             claimius.object_field, claimius.user_field, claimius.user_relation,
             claimius.audit, claimius.inheritance_info, claimius.table_info,
             claimius.notification, claimius.claim_graph_cache
             TO claimius_disciple_client';
    EXECUTE 'REVOKE INSERT, UPDATE, DELETE ON claimius.claim_graph_cache FROM claimius_reader';
    EXECUTE 'GRANT SELECT ON ALL TABLES IN SCHEMA claimius TO claimius_disciple_client';
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_disciple_client';
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._grant_disciple_roles() IS 'The role grants of a disciple, run by V6.0 on every build and replay; never called by a consumer.';

DROP FUNCTION IF EXISTS claimius._grant_hybrid_roles();

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM claimius.disciple_state)
        AND NOT EXISTS (SELECT 1 FROM claimius.prophet_state WHERE system_app_slug IS NOT NULL)
    THEN
        PERFORM claimius._grant_disciple_roles();
    ELSE
        PERFORM claimius._grant_prophet_roles();
    END IF;
    PERFORM claimius._match_audit_partition_grants(i.inhrelid::REGCLASS)
    FROM pg_inherits i
    WHERE i.inhparent = 'claimius.audit'::REGCLASS;
END $$;
