ALTER TYPE claimius.app_connection ADD VALUE IF NOT EXISTS 'bff';

COMMENT ON COLUMN claimius.samna_app.connection IS 'Session delivery mode. direct, code, or bff.';
