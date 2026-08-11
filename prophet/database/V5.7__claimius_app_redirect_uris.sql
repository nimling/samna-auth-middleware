ALTER TABLE claimius.samna_app ADD COLUMN IF NOT EXISTS redirect_uris TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];

UPDATE claimius.samna_app
SET redirect_uris = ARRAY[redirect_uri]
WHERE redirect_uri <> '' AND cardinality(redirect_uris) = 0;

ALTER TABLE claimius.samna_app DROP COLUMN IF EXISTS redirect_uri;

COMMENT ON COLUMN claimius.samna_app.redirect_uris IS 'Registered OAuth redirect URIs. An authorize request matches by exact equality against any entry. The first entry is the primary origin and is the one the cookie domain derives from.';
