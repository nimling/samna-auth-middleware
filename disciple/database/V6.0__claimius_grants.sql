CREATE OR REPLACE FUNCTION claimius._grant_disciple_roles()
    RETURNS VOID AS $$
BEGIN
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_reader';
    EXECUTE 'REVOKE EXECUTE ON FUNCTION claimius.insert_claim, claimius.update_claim, claimius.delete_claim,
             claimius.insert_user_claim, claimius.update_user_claim, claimius.delete_user_claim,
             claimius.insert_claim_object, claimius.update_claim_object, claimius.delete_claim_object,
             claimius.migrate_root,
             claimius.upsert_disciple_state, claimius.insert_secret,
             claimius.upsert_table_info, claimius.delete_table_info, claimius.upsert_internal_table_info,
             claimius.recompute_state
             FROM claimius_reader';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.user_object, claimius.object_info, claimius.user_users, claimius.user_users_diff,
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

COMMENT ON FUNCTION claimius._grant_disciple_roles() IS 'The role grants of a disciple, run by V6.0 on every build and replay; never called by a consumer.';

SELECT claimius._grant_disciple_roles();

SELECT claimius._match_audit_partition_grants(i.inhrelid::REGCLASS)
FROM pg_inherits i
WHERE i.inhparent = 'claimius.audit'::REGCLASS;
