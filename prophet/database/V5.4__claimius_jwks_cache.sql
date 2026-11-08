CREATE UNLOGGED TABLE IF NOT EXISTS claimius.jwks_cache (
    cache_key  text PRIMARY KEY,
    body       jsonb NOT NULL,
    expires_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_jwks_cache_expires_at ON claimius.jwks_cache (expires_at);

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.jwks_cache TO claimius_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.jwks_cache TO claimius_reader;
