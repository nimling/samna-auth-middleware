CREATE UNLOGGED TABLE IF NOT EXISTS claimius.par_request (
    request_uri text PRIMARY KEY,
    payload     jsonb NOT NULL,
    expires_at  timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_par_request_expires_at ON claimius.par_request (expires_at);

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.par_request TO claimius_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.par_request TO claimius_reader;
