-- ============================================================================
-- Claimius V3.0 Disciple Triggers
-- ----------------------------------------------------------------------------
-- A disciple maintains timestamps, fills self referencing roots on insert,
-- and invalidates the claim graph cache. It has no calc triggers and no sync
-- event emitters: access state is computed on the prophet and applied here by
-- sync under replay_mode.
-- ============================================================================

-- ----------------------------------------------------------------------------
-- sa_updated_at maintenance
-- ----------------------------------------------------------------------------

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.samna_app;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.samna_app
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.samna_user;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.samna_user
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.samna_client;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.samna_client
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.samna_secret;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.samna_secret
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.organization;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.organization
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.location;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.location
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.claim;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.claim
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.user_claim;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.user_claim
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.claim_object;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.claim_object
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.audit;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.audit
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.user_relation;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.user_relation
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.object_field;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.object_field
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.user_field;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.user_field
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.table_info;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.table_info
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

-- ----------------------------------------------------------------------------
-- Self root triggers: BEFORE INSERT, set self referencing root values when
-- callers haven't supplied them. Applies to organization and location.
-- ----------------------------------------------------------------------------

DROP TRIGGER IF EXISTS tg_self_root ON claimius.organization;
CREATE TRIGGER tg_self_root BEFORE INSERT ON claimius.organization
    FOR EACH ROW EXECUTE FUNCTION claimius._set_self_owner_on_insert();

DROP TRIGGER IF EXISTS tg_self_root ON claimius.location;
CREATE TRIGGER tg_self_root BEFORE INSERT ON claimius.location
    FOR EACH ROW EXECUTE FUNCTION claimius._set_self_root_on_insert();

-- ----------------------------------------------------------------------------
-- Claim graph cache invalidation. Drops claim_graph_cache rows whenever a
-- write touches data that feeds the cached graph.
-- ----------------------------------------------------------------------------

DROP TRIGGER IF EXISTS tg_invalidate_claim_graph_cache ON claimius.user_object;
CREATE TRIGGER tg_invalidate_claim_graph_cache
    AFTER INSERT OR UPDATE OR DELETE ON claimius.user_object
    FOR EACH ROW EXECUTE FUNCTION claimius._invalidate_claim_graph_cache();

DROP TRIGGER IF EXISTS tg_invalidate_claim_graph_cache ON claimius.user_users;
CREATE TRIGGER tg_invalidate_claim_graph_cache
    AFTER INSERT OR UPDATE OR DELETE ON claimius.user_users
    FOR EACH ROW EXECUTE FUNCTION claimius._invalidate_claim_graph_cache();

DROP TRIGGER IF EXISTS tg_invalidate_claim_graph_cache ON claimius.samna_user;
CREATE TRIGGER tg_invalidate_claim_graph_cache
    AFTER INSERT OR UPDATE OR DELETE ON claimius.samna_user
    FOR EACH ROW EXECUTE FUNCTION claimius._invalidate_claim_graph_cache();

DROP TRIGGER IF EXISTS tg_invalidate_claim_graph_cache ON claimius.claim;
CREATE TRIGGER tg_invalidate_claim_graph_cache
    AFTER INSERT OR UPDATE OR DELETE ON claimius.claim
    FOR EACH ROW EXECUTE FUNCTION claimius._invalidate_claim_graph_cache();

DROP TRIGGER IF EXISTS tg_invalidate_claim_graph_cache ON claimius.claim_object;
CREATE TRIGGER tg_invalidate_claim_graph_cache
    AFTER INSERT OR UPDATE OR DELETE ON claimius.claim_object
    FOR EACH ROW EXECUTE FUNCTION claimius._invalidate_claim_graph_cache();
