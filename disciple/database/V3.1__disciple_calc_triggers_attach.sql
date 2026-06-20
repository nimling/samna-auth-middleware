-- ============================================================================
-- Claimius V3.1 Disciple Calc Trigger Attachments
-- ----------------------------------------------------------------------------
-- Attaches the calc triggers on internal tables so debug mode local writes
-- materialize user_object. No emit triggers: a disciple consumes sync, never
-- publishes. Under replay_mode (production sync) these calc functions short
-- circuit, so the synced relation tables are applied verbatim.
-- ============================================================================
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
-- samna_app, samna_user, samna_client, samna_secret are non hierarchical.
-- ----------------------------------------------------------------------------

DROP TRIGGER IF EXISTS tg_calc_access ON claimius.organization;
CREATE TRIGGER tg_calc_access AFTER INSERT OR UPDATE OR DELETE ON claimius.organization
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_hierarchical_access();

DROP TRIGGER IF EXISTS tg_calc_access ON claimius.location;
CREATE TRIGGER tg_calc_access AFTER INSERT OR UPDATE OR DELETE ON claimius.location
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_hierarchical_access();

DROP TRIGGER IF EXISTS tg_calc_access ON claimius.samna_user;
CREATE TRIGGER tg_calc_access AFTER INSERT OR UPDATE OR DELETE ON claimius.samna_user
    FOR EACH ROW EXECUTE FUNCTION claimius.calc_object_access();

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
