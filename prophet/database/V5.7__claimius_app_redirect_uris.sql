ALTER TABLE claimius.samna_app ADD COLUMN IF NOT EXISTS redirect_uris TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'claimius' AND table_name = 'samna_app' AND column_name = 'redirect_uri'
    ) THEN
        UPDATE claimius.samna_app
        SET redirect_uris = ARRAY[redirect_uri]
        WHERE redirect_uri <> '' AND cardinality(redirect_uris) = 0;

        ALTER TABLE claimius.samna_app DROP COLUMN redirect_uri;
    END IF;
END $$;

COMMENT ON COLUMN claimius.samna_app.redirect_uris IS 'Registered OAuth redirect URIs. An authorize request matches by exact equality against any entry. The first entry is the primary origin and is the one the cookie domain derives from.';
