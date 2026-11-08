-- ============================================================================
-- Claimius V6.0 Grants
-- ----------------------------------------------------------------------------
-- Last step of the base set. Defines the role grant helpers init calls and
-- runs the one that fits the deployment, a disciple initialized through
-- init_disciple or else a prophet, at the top level, so every build and every
-- replay of the set leaves the roles holding what init grants them on every
-- table and function the set carries, with no consumer calling an internal
-- function, and brings every audit partition to the grants of the
-- partitioned table. A change to what a role holds is made here, which
-- replays this file.
-- ============================================================================

CREATE OR REPLACE FUNCTION claimius._grant_prophet_roles()
    RETURNS VOID AS $$
BEGIN
    -- Reader: EXECUTE on all functions, then revoke the write functions and
    -- the init/admin ones. Reader sees no tables directly.
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_reader';
    EXECUTE 'REVOKE EXECUTE ON FUNCTION claimius.create_claim, claimius.update_claim, claimius.remove_claim,
             claimius.assign_claim_user, claimius.update_claim_user, claimius.remove_user_claim,
             claimius.assign_claim_object, claimius.update_claim_object, claimius.remove_claim_object,
             claimius.merge_user, claimius.migrate_root,
             claimius.init, claimius.init_disciple, claimius.init_hybrid, claimius.create_app,
             claimius.offer_handover, claimius.accept_handover, claimius.apply_peer_event,
             claimius.init_claimius_tables, claimius.init_claimius_internal,
             claimius.recompute_state
             FROM claimius_reader';

    -- Writer: EXECUTE on everything, plus INSERT/UPDATE on tables that do
    -- not have dedicated write functions. The prophet's app code is allowed
    -- to write directly to these tables. Tables WITH write functions
    -- (claim, user_claim, claim_object) get only EXECUTE on the function;
    -- direct writes to those tables are not granted.
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_writer';
    EXECUTE 'GRANT INSERT, UPDATE ON
             claimius.samna_app, claimius.samna_user, claimius.samna_client, claimius.samna_secret,
             claimius.organization, claimius.location,
             claimius.object_field, claimius.user_field, claimius.user_relation,
             claimius.notification, claimius.prophet_state, claimius.sync_consumer
             TO claimius_writer';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.claim_graph_cache TO claimius_writer';
    EXECUTE 'GRANT UPDATE ON claimius.prophet_peer TO claimius_writer';
    EXECUTE 'REVOKE INSERT, UPDATE, DELETE ON claimius.claim_graph_cache FROM claimius_reader';
    EXECUTE 'GRANT SELECT ON ALL TABLES IN SCHEMA claimius TO claimius_writer';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION claimius._grant_disciple_roles()
    RETURNS VOID AS $$
BEGIN
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_reader';
    EXECUTE 'REVOKE EXECUTE ON FUNCTION claimius.create_claim, claimius.update_claim, claimius.remove_claim,
             claimius.assign_claim_user, claimius.update_claim_user, claimius.remove_user_claim,
             claimius.assign_claim_object, claimius.update_claim_object, claimius.remove_claim_object,
             claimius.merge_user, claimius.migrate_root,
             claimius.init, claimius.init_disciple, claimius.init_hybrid, claimius.create_app,
             claimius.offer_handover, claimius.accept_handover, claimius.apply_peer_event,
             claimius.init_claimius_tables, claimius.init_claimius_internal,
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
