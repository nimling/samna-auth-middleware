CREATE UNLOGGED TABLE IF NOT EXISTS claimius.session (
    id         text PRIMARY KEY,
    data       jsonb NOT NULL,
    expires_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_session_expires_at ON claimius.session (expires_at);

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.session TO claimius_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.session TO claimius_reader;
