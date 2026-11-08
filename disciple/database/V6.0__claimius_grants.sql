-- ============================================================================
-- Claimius V6.0 Grants
-- ----------------------------------------------------------------------------
-- Last step of the base set. Defines the role grant helper init calls and
-- runs it at the top level, so every build and every replay of the set
-- leaves the roles holding what init grants them on every table and function
-- the set carries, with no consumer calling an internal function, and brings
-- every audit partition to the grants of the partitioned table. A change to
-- what a role holds is made here, which replays this file.
-- ============================================================================

CREATE OR REPLACE FUNCTION claimius._grant_disciple_roles()
    RETURNS VOID AS $$
BEGIN
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_reader';
    EXECUTE 'REVOKE EXECUTE ON FUNCTION claimius.create_claim, claimius.update_claim, claimius.remove_claim,
             claimius.assign_claim_user, claimius.update_claim_user, claimius.remove_user_claim,
             claimius.assign_claim_object, claimius.update_claim_object, claimius.remove_claim_object,
             claimius.merge_user, claimius.migrate_root,
             claimius.init,
             claimius.init_claimius_tables, claimius.init_claimius_internal,
             claimius.recompute_state
             FROM claimius_reader';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.user_object, claimius.object_info, claimius.user_users,
             claimius.disciple_state, claimius.samna_user, claimius.samna_client,
             claimius.samna_secret, claimius.organization, claimius.location,
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

SELECT claimius._grant_disciple_roles();

SELECT claimius._match_audit_partition_grants(i.inhrelid::REGCLASS)
FROM pg_inherits i
WHERE i.inhparent = 'claimius.audit'::REGCLASS;
