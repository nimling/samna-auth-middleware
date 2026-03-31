CREATE OR REPLACE FUNCTION samna_auth.update_current_timestamp()
RETURNS TRIGGER AS $$
BEGIN
   NEW.sa_updated_at = CURRENT_TIMESTAMP;
   RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER update_samna_user_sa_updated_at
    BEFORE UPDATE ON samna_auth.samna_user
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();

CREATE OR REPLACE TRIGGER update_claim_sa_updated_at
    BEFORE UPDATE ON samna_auth.claim
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();

CREATE OR REPLACE TRIGGER update_claim_object_sa_updated_at
    BEFORE UPDATE ON samna_auth.claim_object
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();

CREATE OR REPLACE TRIGGER update_user_claim_sa_updated_at
    BEFORE UPDATE ON samna_auth.user_claim
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();

CREATE OR REPLACE TRIGGER update_samna_auth_organization_sa_updated_at
    BEFORE UPDATE ON samna_auth.organization
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();

CREATE OR REPLACE TRIGGER update_samna_auth_location_sa_updated_at
    BEFORE UPDATE ON samna_auth.location
    FOR EACH ROW
    EXECUTE FUNCTION samna_auth.update_current_timestamp();
