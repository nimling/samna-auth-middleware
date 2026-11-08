CREATE UNLOGGED TABLE IF NOT EXISTS claimius.dpop_jti (
    jti        text PRIMARY KEY,
    expires_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_dpop_jti_expires_at ON claimius.dpop_jti (expires_at);

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.dpop_jti TO claimius_reader;
GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.dpop_jti TO claimius_disciple_client;
