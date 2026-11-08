CREATE OR REPLACE VIEW claimius.get_users AS SELECT * FROM claimius.samna_user;
CREATE OR REPLACE VIEW claimius.get_organizations AS SELECT * FROM claimius.organization;
CREATE OR REPLACE VIEW claimius.get_locations AS SELECT * FROM claimius.location;
CREATE OR REPLACE VIEW claimius.get_claims AS SELECT * FROM claimius.claim;
CREATE OR REPLACE VIEW claimius.get_grants AS SELECT * FROM claimius.user_claim;

COMMENT ON VIEW claimius.get_users IS 'Every samna_user row as it stands, soft deleted and inactive rows included, with no reader and no access filter, as a relation a query joins. The read surface of the SamnaUser model: bun selects and belongs-to joins read it through select:claimius.get_users, so app code reaches the table only through a get_ name. Defined here, after every column change of the set, so it carries every column.';
COMMENT ON VIEW claimius.get_organizations IS 'Every organization row as it stands, soft deleted rows included, with no reader and no access filter, as a relation a query joins. The read surface of the Organization model for bun selects and belongs-to joins.';
COMMENT ON VIEW claimius.get_locations IS 'Every location row as it stands, soft deleted rows included, with no reader and no access filter, as a relation a query joins. The read surface of the Location model for bun selects and belongs-to joins.';
COMMENT ON VIEW claimius.get_claims IS 'Every claim row as it stands, soft deleted rows included, with no reader and no access filter, as a relation a query joins. The read surface of the Claim model for bun selects and belongs-to joins.';
COMMENT ON VIEW claimius.get_grants IS 'Every user_claim row as it stands, soft deleted grants and grants outside their window included, with no reader and no access filter, as a relation a query joins. The read surface of the UserClaim model for bun selects and belongs-to joins.';

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
             claimius.update_claim_guest, claimius.insert_code, claimius.delete_code, claimius.update_code_used, claimius.redeem_code,
             claimius.upsert_table_info, claimius.delete_table_info, claimius.upsert_internal_table_info,
             claimius.recompute_state, claimius.prune,
             claimius.upsert_search_source, claimius.delete_search_source
             FROM claimius_reader';

    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_writer';
    EXECUTE 'GRANT INSERT, UPDATE ON
             claimius.samna_app, claimius.samna_user, claimius.samna_client, claimius.samna_secret,
             claimius.organization, claimius.location,
             claimius.object_field, claimius.user_field, claimius.user_relation, claimius.object_asset,
             claimius.notification, claimius.prophet_state, claimius.sync_consumer, claimius.peer_grant, claimius.code
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
             claimius.update_claim_guest, claimius.insert_code, claimius.delete_code, claimius.update_code_used, claimius.redeem_code,
             claimius.upsert_table_info, claimius.delete_table_info, claimius.upsert_internal_table_info,
             claimius.recompute_state, claimius.prune,
             claimius.upsert_search_source, claimius.delete_search_source
             FROM claimius_reader';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.user_object, claimius.object_info, claimius.user_users,
             claimius.disciple_state, claimius.samna_user, claimius.samna_client,
             claimius.samna_secret, claimius.samna_app, claimius.organization, claimius.location,
             claimius.claim, claimius.user_claim, claimius.claim_object,
             claimius.object_field, claimius.user_field, claimius.user_relation, claimius.object_asset,
             claimius.audit, claimius.inheritance_info, claimius.table_info,
             claimius.notification, claimius.claim_graph_cache
             TO claimius_disciple_client';
    EXECUTE 'REVOKE INSERT, UPDATE, DELETE ON claimius.claim_graph_cache FROM claimius_reader';
    EXECUTE 'GRANT SELECT ON claimius.get_users, claimius.get_organizations, claimius.get_locations,
             claimius.get_claims, claimius.get_grants
             TO claimius_reader';
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

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.session TO claimius_writer;

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.session TO claimius_reader;

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.dpop_jti TO claimius_writer;

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.dpop_jti TO claimius_reader;

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.rate_limit TO claimius_writer;

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.rate_limit TO claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.rate_limit_gcra(text, double precision, double precision, double precision, double precision) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.rate_limit_gcra(text, double precision, double precision, double precision, double precision) TO claimius_reader;

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.par_request TO claimius_writer;

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.par_request TO claimius_reader;

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.jwks_cache TO claimius_writer;

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.jwks_cache TO claimius_reader;

GRANT SELECT, INSERT, UPDATE ON claimius.consent TO claimius_writer;

GRANT SELECT ON claimius.consent TO claimius_reader;

GRANT SELECT, INSERT, UPDATE ON claimius.analytics_session TO claimius_writer;

GRANT SELECT ON claimius.analytics_session TO claimius_reader;

GRANT SELECT, INSERT ON claimius.analytics_event TO claimius_writer;

GRANT SELECT ON claimius.analytics_event TO claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_consent(UUID, UUID, TEXT) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.get_consent(UUID, UUID, TEXT) TO claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.insert_consent(UUID, TEXT, JSONB, TEXT, UUID, INET, TEXT) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.consent_allows(UUID, TEXT, UUID, TEXT) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.consent_allows(UUID, TEXT, UUID, TEXT) TO claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.upsert_analytics_session(UUID, TEXT, TEXT, JSONB, UUID, INET, TEXT) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.insert_analytics_events(UUID, TEXT, JSONB) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.capture_user_apps(UUID) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.capture_user_apps(UUID) TO claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.analytics_enabled(UUID) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.analytics_enabled(UUID) TO claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_analytics_events(UUID, UUID, UUID, UUID, TEXT, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.get_analytics_events(UUID, UUID, UUID, UUID, TEXT, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER) TO claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_analytics_sessions(UUID, UUID, UUID, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.get_analytics_sessions(UUID, UUID, UUID, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER) TO claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_analytics_apps(UUID) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.get_analytics_apps(UUID) TO claimius_reader;

GRANT SELECT, INSERT, UPDATE ON claimius.analytics_capture TO claimius_writer;

GRANT SELECT ON claimius.analytics_capture TO claimius_reader;

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.analytics_heat TO claimius_writer;

GRANT SELECT ON claimius.analytics_heat TO claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_analytics_capture(UUID, TEXT) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.get_analytics_capture(UUID, TEXT) TO claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_analytics_captured(UUID, INTERVAL) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.get_analytics_captured(UUID, INTERVAL) TO claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.get_analytics_summary(UUID, UUID, TIMESTAMPTZ, TIMESTAMPTZ) TO claimius_writer;

GRANT EXECUTE ON FUNCTION claimius.get_analytics_summary(UUID, UUID, TIMESTAMPTZ, TIMESTAMPTZ) TO claimius_reader;

GRANT EXECUTE ON FUNCTION claimius.upsert_analytics_capture(UUID, TEXT, TEXT, BYTEA) TO claimius_writer;

