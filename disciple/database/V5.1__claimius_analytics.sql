-- ============================================================================
-- Claimius V5.1 Analytics replica
-- ----------------------------------------------------------------------------
-- The disciple side of consent and interaction capture. The prophet remains the
-- only writer: a disciple forwards every capture batch and every consent
-- decision there on its app key, and these tables receive the rows back through
-- the sync feed the same way claim, organization and audit arrive.
--
-- app_id is kept here even though a disciple mirrors one app, because a claim
-- on the prophet can grant one app read access to another app's capture, and
-- those rows arrive with the app they were recorded under. The column carries no
-- foreign key for the same reason the rest of the replica carries none: rows
-- land in sync order, not in dependency order.
-- ============================================================================

CREATE TABLE IF NOT EXISTS claimius.consent (
    id             UUID PRIMARY KEY,
    app_id         UUID NOT NULL,
    user_id        UUID,
    anonymous_id   TEXT NOT NULL,
    categories     JSONB NOT NULL DEFAULT '{}'::jsonb,
    policy_version TEXT NOT NULL,
    ip             INET,
    user_agent     TEXT,
    granted_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at     TIMESTAMPTZ,
    sa_created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.consent IS 'Replica of the live consent decisions the prophet holds for every app this disciple may read.';

CREATE INDEX IF NOT EXISTS idx_consent_app_user ON claimius.consent (app_id, user_id) WHERE user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_consent_app_anonymous ON claimius.consent (app_id, anonymous_id);

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.consent TO claimius_writer;
GRANT SELECT ON claimius.consent TO claimius_reader;

CREATE TABLE IF NOT EXISTS claimius.analytics_session (
    id            UUID PRIMARY KEY,
    app_id        UUID NOT NULL,
    user_id       UUID,
    anonymous_id  TEXT NOT NULL,
    session_key   TEXT NOT NULL,
    entry_path    TEXT,
    referrer      TEXT,
    user_agent    TEXT,
    ip            INET,
    language      TEXT,
    timezone      TEXT,
    platform      TEXT,
    screen_width  INTEGER,
    screen_height INTEGER,
    device_pixel_ratio REAL,
    context       JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.analytics_session IS 'Replica of the visit context each captured signal references.';

CREATE INDEX IF NOT EXISTS idx_analytics_session_app_key ON claimius.analytics_session (app_id, session_key);
CREATE INDEX IF NOT EXISTS idx_analytics_session_app_started ON claimius.analytics_session (app_id, started_at DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.analytics_session TO claimius_writer;
GRANT SELECT ON claimius.analytics_session TO claimius_reader;

CREATE TABLE IF NOT EXISTS claimius.analytics_event (
    id          UUID PRIMARY KEY,
    app_id      UUID NOT NULL,
    session_id  UUID NOT NULL,
    user_id     UUID,
    type        TEXT NOT NULL,
    path        TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.analytics_event IS 'Replica of the captured signals, one row per signal, pointer samples included.';

CREATE INDEX IF NOT EXISTS idx_analytics_event_session ON claimius.analytics_event (session_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_analytics_event_app_path ON claimius.analytics_event (app_id, path, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_analytics_event_app_type ON claimius.analytics_event (app_id, type, occurred_at DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.analytics_event TO claimius_writer;
GRANT SELECT ON claimius.analytics_event TO claimius_reader;
