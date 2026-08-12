ALTER TYPE claimius.app_connection ADD VALUE IF NOT EXISTS 'bff';

COMMENT ON COLUMN claimius.samna_app.connection IS 'Session delivery mode. direct issues session cookies straight from the callback and the prophet serves the content. code hands an auth code to the app backend for exchange. bff has the prophet answer on the app host and hand the session over through a single use handoff.';
