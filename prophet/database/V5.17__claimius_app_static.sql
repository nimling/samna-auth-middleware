ALTER TYPE claimius.app_connection ADD VALUE IF NOT EXISTS 'static';

COMMENT ON COLUMN claimius.samna_app.connection IS 'Session delivery mode. direct, code, bff, or static.';
