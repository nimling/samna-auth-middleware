ALTER TYPE claimius.app_connection ADD VALUE IF NOT EXISTS 'bff';

ALTER TABLE claimius.samna_app ADD COLUMN IF NOT EXISTS guarded_prefix TEXT NOT NULL DEFAULT '/_guarded';

COMMENT ON COLUMN claimius.samna_app.guarded_prefix IS 'Path prefix holding the gated bundle of a bff app. A request under it requires a session; everything else on that host is public.';

COMMENT ON COLUMN claimius.samna_app.connection IS 'Session delivery mode. direct issues session cookies straight from the callback and the prophet serves the content. code hands an auth code to the app backend for exchange. bff has the prophet answer on the app host, hand the session over through a single use handoff, and serve the app split into a public part and a guarded prefix.';
