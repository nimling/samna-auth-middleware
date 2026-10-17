CREATE UNLOGGED TABLE IF NOT EXISTS claimius.cli_login (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    device_code_hash text NOT NULL,
    user_code_hash   text NOT NULL,
    handle_hash      text NOT NULL,
    approved         boolean NOT NULL DEFAULT false,
    refresh_token    text,
    expires_at       timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cli_login_expires_at ON claimius.cli_login (expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cli_login_handle_hash ON claimius.cli_login (handle_hash);

COMMENT ON TABLE claimius.cli_login IS 'An open cli device login on this disciple: the hashed codes the cli and the browser present, and the refresh token the browser approval hands over once.';

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.cli_login TO claimius_reader;
GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.cli_login TO claimius_disciple_client;
