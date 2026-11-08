ALTER TABLE claimius.samna_app ADD COLUMN IF NOT EXISTS cookie_domain TEXT;
ALTER TABLE claimius.samna_app ADD COLUMN IF NOT EXISTS cookie_same_site TEXT;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'samna_app_cookie_same_site_check') THEN
        ALTER TABLE claimius.samna_app ADD CONSTRAINT samna_app_cookie_same_site_check CHECK (cookie_same_site IN ('lax', 'strict', 'none'));
    END IF;
END $$;

COMMENT ON COLUMN claimius.samna_app.cookie_domain IS 'Domain an Initiate app''s session cookies are set on by its disciple. Empty for host only cookies on the host that served the request, a parent domain to share them with the disciple''s host.';
COMMENT ON COLUMN claimius.samna_app.cookie_same_site IS 'SameSite of an Initiate app''s session cookies: lax or strict when the frontend shares a site with the disciple or routes its api through its own host, none when it calls the disciple from another site.';
