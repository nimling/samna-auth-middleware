-- ============================================================================
-- Claimius V3.0 Triggers
-- ----------------------------------------------------------------------------
-- Attaches the functions defined in V2.x to claimius.* tables. Triggers on
-- registered external tables are attached separately by init_claimius_tables.
--
-- Trigger families on internal tables:
--   tg_update_timestamp     BEFORE UPDATE: maintains sa_updated_at
--   tg_audit                AFTER INSERT/UPDATE: writes to claimius.audit
--   tg_calc_claim_access    AFTER INSERT/UPDATE: on claim, claim_object,
--                           user_claim. Reconciles affected users.
--   tg_calc_access          AFTER INSERT/UPDATE/DELETE: on internal tables
--                           that are themselves access targets (organization,
--                           location, samna_user, etc).
--   tg_calc_trees           AFTER INSERT/UPDATE/DELETE: on owned tables that
--                           are not access targets. Keeps their closure rows.
--   tg_emit_sync_event      AFTER INSERT/UPDATE/DELETE: on user_object,
--                           user_users. Publishes to sync_event.
--   tg_count_user_users_*   AFTER INSERT/DELETE per statement: on user_object.
--                           Counts the pairs entering and leaving into
--                           user_users.
-- ============================================================================

-- ----------------------------------------------------------------------------
-- sa_updated_at maintenance
-- ----------------------------------------------------------------------------

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
-- Claim mutation triggers: maintain user_object when claim/binding/grant changes
-- ----------------------------------------------------------------------------

DROP TRIGGER IF EXISTS tg_calc_claim_access ON claimius.claim;
CREATE TRIGGER tg_calc_claim_access AFTER INSERT OR UPDATE ON claimius.claim
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_claim_access();

DROP TRIGGER IF EXISTS tg_calc_claim_access ON claimius.claim_object;
CREATE TRIGGER tg_calc_claim_access AFTER INSERT OR UPDATE ON claimius.claim_object
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_claim_access();

DROP TRIGGER IF EXISTS tg_calc_claim_access ON claimius.user_claim;
CREATE TRIGGER tg_calc_claim_access AFTER INSERT OR UPDATE ON claimius.user_claim
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_claim_access();

-- ----------------------------------------------------------------------------
-- Internal tables that are themselves access targets get calc_object_access
-- (or calc_hierarchical_access). organization and location are hierarchical.
-- samna_user, samna_client, samna_secret are non hierarchical.
-- ----------------------------------------------------------------------------

DROP TRIGGER IF EXISTS tg_calc_access ON claimius.organization;
DROP TRIGGER IF EXISTS tg_calc_access_insert ON claimius.organization;
DROP TRIGGER IF EXISTS tg_calc_access_update ON claimius.organization;
DROP TRIGGER IF EXISTS tg_calc_access_delete ON claimius.organization;
CREATE TRIGGER tg_calc_access AFTER INSERT OR UPDATE OR DELETE ON claimius.organization
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_hierarchical_access();

DROP TRIGGER IF EXISTS tg_calc_access ON claimius.location;
DROP TRIGGER IF EXISTS tg_calc_access_insert ON claimius.location;
DROP TRIGGER IF EXISTS tg_calc_access_update ON claimius.location;
DROP TRIGGER IF EXISTS tg_calc_access_delete ON claimius.location;
CREATE TRIGGER tg_calc_access AFTER INSERT OR UPDATE OR DELETE ON claimius.location
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_hierarchical_access();

DROP TRIGGER IF EXISTS tg_calc_access ON claimius.samna_user;
CREATE TRIGGER tg_calc_access AFTER INSERT OR UPDATE OR DELETE ON claimius.samna_user
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_object_access();

DROP TRIGGER IF EXISTS tg_cascade_user_soft_delete ON claimius.samna_user;
CREATE TRIGGER tg_cascade_user_soft_delete
    AFTER UPDATE OF sa_deleted_at ON claimius.samna_user
    FOR EACH ROW
    WHEN (OLD.sa_deleted_at IS DISTINCT FROM NEW.sa_deleted_at)
    EXECUTE FUNCTION claimius._cascade_user_soft_delete_trg();

DROP TRIGGER IF EXISTS tg_calc_access ON claimius.samna_client;
CREATE TRIGGER tg_calc_access AFTER INSERT OR UPDATE OR DELETE ON claimius.samna_client
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_object_access();

DROP TRIGGER IF EXISTS tg_calc_access ON claimius.samna_secret;
CREATE TRIGGER tg_calc_access AFTER INSERT OR UPDATE OR DELETE ON claimius.samna_secret
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_object_access();

DROP TRIGGER IF EXISTS tg_calc_access ON claimius.user_relation;
CREATE TRIGGER tg_calc_access AFTER INSERT OR UPDATE OR DELETE ON claimius.user_relation
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_object_access();

DROP TRIGGER IF EXISTS tg_calc_access ON claimius.user_field;
CREATE TRIGGER tg_calc_access AFTER INSERT OR UPDATE OR DELETE ON claimius.user_field
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_object_access();

DROP TRIGGER IF EXISTS tg_calc_access ON claimius.object_field;
CREATE TRIGGER tg_calc_access AFTER INSERT OR UPDATE OR DELETE ON claimius.object_field
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_object_access();

DROP TRIGGER IF EXISTS tg_calc_access ON claimius.claim;
CREATE TRIGGER tg_calc_access AFTER INSERT OR UPDATE OR DELETE ON claimius.claim
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_object_access();

DROP TRIGGER IF EXISTS tg_calc_access ON claimius.user_token;
CREATE TRIGGER tg_calc_access AFTER INSERT OR UPDATE OR DELETE ON claimius.user_token
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_object_access();

-- ----------------------------------------------------------------------------
-- Owned tables that are not access targets keep their ownership closure rows.
-- ----------------------------------------------------------------------------

DROP TRIGGER IF EXISTS tg_calc_trees ON claimius.user_claim;
CREATE TRIGGER tg_calc_trees AFTER INSERT OR UPDATE OR DELETE ON claimius.user_claim
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_object_trees();

DROP TRIGGER IF EXISTS tg_calc_trees ON claimius.claim_object;
CREATE TRIGGER tg_calc_trees AFTER INSERT OR UPDATE OR DELETE ON claimius.claim_object
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_object_trees();

-- ----------------------------------------------------------------------------
-- Sync event publishers
-- ----------------------------------------------------------------------------

DROP TRIGGER IF EXISTS tg_emit_sync_event ON claimius.user_object;
CREATE TRIGGER tg_emit_sync_event AFTER INSERT OR UPDATE OR DELETE ON claimius.user_object
    FOR EACH ROW EXECUTE FUNCTION claimius.emit_sync_event();

DROP TRIGGER IF EXISTS tg_emit_sync_event ON claimius.user_users;
CREATE TRIGGER tg_emit_sync_event AFTER INSERT OR UPDATE OR DELETE ON claimius.user_users
    FOR EACH ROW EXECUTE FUNCTION claimius.emit_sync_event();

-- ----------------------------------------------------------------------------
-- user_users counting
-- ----------------------------------------------------------------------------

DROP TRIGGER IF EXISTS tg_count_user_users_insert ON claimius.user_object;
CREATE TRIGGER tg_count_user_users_insert AFTER INSERT ON claimius.user_object
    REFERENCING NEW TABLE AS changed
    FOR EACH STATEMENT EXECUTE FUNCTION claimius._count_user_users();

DROP TRIGGER IF EXISTS tg_count_user_users_delete ON claimius.user_object;
CREATE TRIGGER tg_count_user_users_delete AFTER DELETE ON claimius.user_object
    REFERENCING OLD TABLE AS changed
    FOR EACH STATEMENT EXECUTE FUNCTION claimius._count_user_users();

DROP TRIGGER IF EXISTS tg_emit_sync_event ON claimius.samna_user;
CREATE TRIGGER tg_emit_sync_event AFTER INSERT OR UPDATE OR DELETE ON claimius.samna_user
    FOR EACH ROW EXECUTE FUNCTION claimius.emit_sync_event();

DROP TRIGGER IF EXISTS tg_emit_sync_event ON claimius.samna_client;
CREATE TRIGGER tg_emit_sync_event AFTER INSERT OR UPDATE OR DELETE ON claimius.samna_client
    FOR EACH ROW EXECUTE FUNCTION claimius.emit_sync_event();

DROP TRIGGER IF EXISTS tg_emit_sync_event ON claimius.samna_secret;
CREATE TRIGGER tg_emit_sync_event AFTER INSERT OR UPDATE OR DELETE ON claimius.samna_secret
    FOR EACH ROW EXECUTE FUNCTION claimius.emit_sync_event();

DROP TRIGGER IF EXISTS tg_emit_sync_event ON claimius.organization;
CREATE TRIGGER tg_emit_sync_event AFTER INSERT OR UPDATE OR DELETE ON claimius.organization
    FOR EACH ROW EXECUTE FUNCTION claimius.emit_sync_event();

DROP TRIGGER IF EXISTS tg_emit_sync_event ON claimius.location;
CREATE TRIGGER tg_emit_sync_event AFTER INSERT OR UPDATE OR DELETE ON claimius.location
    FOR EACH ROW EXECUTE FUNCTION claimius.emit_sync_event();

DROP TRIGGER IF EXISTS tg_emit_sync_event ON claimius.claim;
CREATE TRIGGER tg_emit_sync_event AFTER INSERT OR UPDATE OR DELETE ON claimius.claim
    FOR EACH ROW EXECUTE FUNCTION claimius.emit_sync_event();

DROP TRIGGER IF EXISTS tg_emit_sync_event ON claimius.user_claim;
CREATE TRIGGER tg_emit_sync_event AFTER INSERT OR UPDATE OR DELETE ON claimius.user_claim
    FOR EACH ROW EXECUTE FUNCTION claimius.emit_sync_event();

DROP TRIGGER IF EXISTS tg_emit_sync_event ON claimius.claim_object;
CREATE TRIGGER tg_emit_sync_event AFTER INSERT OR UPDATE OR DELETE ON claimius.claim_object
    FOR EACH ROW EXECUTE FUNCTION claimius.emit_sync_event();

DROP TRIGGER IF EXISTS tg_emit_sync_event ON claimius.object_field;
CREATE TRIGGER tg_emit_sync_event AFTER INSERT OR UPDATE OR DELETE ON claimius.object_field
    FOR EACH ROW EXECUTE FUNCTION claimius.emit_sync_event();

DROP TRIGGER IF EXISTS tg_emit_sync_event ON claimius.user_field;
CREATE TRIGGER tg_emit_sync_event AFTER INSERT OR UPDATE OR DELETE ON claimius.user_field
    FOR EACH ROW EXECUTE FUNCTION claimius.emit_sync_event();

DROP TRIGGER IF EXISTS tg_emit_sync_event ON claimius.user_relation;
CREATE TRIGGER tg_emit_sync_event AFTER INSERT OR UPDATE OR DELETE ON claimius.user_relation
    FOR EACH ROW EXECUTE FUNCTION claimius.emit_sync_event();

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

-- ----------------------------------------------------------------------------
-- Notification
-- ----------------------------------------------------------------------------

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.notification;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.notification
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

DROP TRIGGER IF EXISTS tg_notify_notification ON claimius.notification;
CREATE TRIGGER tg_notify_notification AFTER INSERT ON claimius.notification
    FOR EACH ROW EXECUTE FUNCTION claimius.notify_notification();