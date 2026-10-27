CREATE OR REPLACE VIEW claimius.get_users AS SELECT * FROM claimius.samna_user;
CREATE OR REPLACE VIEW claimius.get_organizations AS SELECT * FROM claimius.organization;
CREATE OR REPLACE VIEW claimius.get_locations AS SELECT * FROM claimius.location;
CREATE OR REPLACE VIEW claimius.get_claims AS SELECT * FROM claimius.claim;
CREATE OR REPLACE VIEW claimius.get_grants AS SELECT * FROM claimius.user_claim;

COMMENT ON VIEW claimius.get_users IS 'Every samna_user row as it stands, soft deleted and inactive rows included, with no reader and no access filter: the rows of the get_users lookup by id, as a relation a query joins. The read surface of the SamnaUser model: bun selects and belongs-to joins read it through select:claimius.get_users, so app code reaches the table only through a get_ name. Defined here, after every column change of the set, so it carries every column.';
COMMENT ON VIEW claimius.get_organizations IS 'Every organization row as it stands, soft deleted rows included, with no reader and no access filter: the rows of the get_organizations lookup by id, as a relation a query joins. The read surface of the Organization model for bun selects and belongs-to joins.';
COMMENT ON VIEW claimius.get_locations IS 'Every location row as it stands, soft deleted rows included, with no reader and no access filter: the rows of the get_locations lookup by id, as a relation a query joins. The read surface of the Location model for bun selects and belongs-to joins.';
COMMENT ON VIEW claimius.get_claims IS 'Every claim row as it stands, soft deleted rows included, with no reader and no access filter: the rows of the get_claims lookup by id, as a relation a query joins. The read surface of the Claim model for bun selects and belongs-to joins.';
COMMENT ON VIEW claimius.get_grants IS 'Every user_claim row as it stands, soft deleted grants and grants outside their window included, with no reader and no access filter: the rows of the get_grants lookup by id, as a relation a query joins. The read surface of the UserClaim model for bun selects and belongs-to joins.';

CREATE OR REPLACE FUNCTION claimius._grant_disciple_roles()
    RETURNS VOID AS $$
BEGIN
    EXECUTE 'GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA claimius TO claimius_reader';
    EXECUTE 'REVOKE EXECUTE ON FUNCTION claimius.insert_claim, claimius.update_claim, claimius.delete_claim,
             claimius.insert_user_claim, claimius.update_user_claim, claimius.delete_user_claim,
             claimius.insert_claim_object, claimius.update_claim_object, claimius.delete_claim_object,
             claimius.migrate_root,
             claimius.upsert_disciple_state, claimius.insert_secret,
             claimius.update_claim_guest, claimius.insert_code, claimius.delete_code, claimius.redeem_code,
             claimius.upsert_table_info, claimius.delete_table_info, claimius.upsert_internal_table_info,
             claimius.recompute_state, claimius.delete_expired, claimius.update_retention, claimius.upsert_retention,
             claimius.upsert_search_source, claimius.delete_search_source
             FROM claimius_reader';
    EXECUTE 'GRANT INSERT, UPDATE, DELETE ON claimius.user_object, claimius.object_info, claimius.user_users, claimius.user_users_diff,
             claimius.disciple_state, claimius.samna_user, claimius.samna_client,
             claimius.samna_secret, claimius.organization, claimius.location,
             claimius.claim, claimius.user_claim, claimius.claim_object,
             claimius.object_field, claimius.user_field, claimius.user_relation, claimius.object_asset,
             claimius.audit, claimius.inheritance_info, claimius.table_info,
             claimius.notification, claimius.claim_graph_cache, claimius.code
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

SELECT claimius._grant_disciple_roles();

SELECT claimius._match_audit_partition_grants(i.inhrelid::REGCLASS)
FROM pg_inherits i
WHERE i.inhparent = 'claimius.audit'::REGCLASS;
