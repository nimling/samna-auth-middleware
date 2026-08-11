-- ============================================================================
-- Claimius V5.6 Analytics
-- ----------------------------------------------------------------------------
-- Consent and interaction capture. The prophet is the only writer: every app
-- forwards its consent decisions and capture batches here on its own app key,
-- nim.io and nimployer alike. Disciples carry the replica in V5.1 and receive
-- these rows through the sync feed like every other claimius table, so an app
-- granted a claim over another app's capture reads it from its own replica.
--
-- claimius.consent carries one live decision per app and identity. A known
-- user resolves across every app on the prophet, so one decision follows the
-- person through every samna_auth app. An anonymous visitor resolves on the
-- durable anonymous_id the browser holds, with ip and user agent recorded as
-- evidence of the decision rather than as part of its key.
--
-- claimius.analytics_session holds the durable context of one visit.
-- claimius.analytics_event is append only and holds one row per captured
-- signal, pointer samples included. post_analytics_events refuses to write a
-- row when the analytics category is not granted, so consent is enforced in
-- the schema and not only in the caller.
-- ============================================================================

CREATE TABLE IF NOT EXISTS claimius.consent (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id         UUID NOT NULL REFERENCES claimius.samna_app(id),
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

COMMENT ON TABLE claimius.consent IS 'One live cookie consent decision per app and identity. A known user resolves across apps.';
COMMENT ON COLUMN claimius.consent.user_id IS 'Samna user_id when the visitor was authenticated at decision time.';
COMMENT ON COLUMN claimius.consent.anonymous_id IS 'Durable browser held id. The identity key while user_id is null.';
COMMENT ON COLUMN claimius.consent.categories IS 'Per category grant as JSON, keyed necessary, preferences, analytics, marketing.';
COMMENT ON COLUMN claimius.consent.policy_version IS 'Policy the visitor agreed to. A newer policy re-prompts.';
COMMENT ON COLUMN claimius.consent.ip IS 'Address the decision came from, recorded as evidence.';
COMMENT ON COLUMN claimius.consent.revoked_at IS 'Set when a newer decision supersedes this one.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_consent_live_user ON claimius.consent (app_id, user_id) WHERE user_id IS NOT NULL AND revoked_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_consent_live_anonymous ON claimius.consent (app_id, anonymous_id) WHERE user_id IS NULL AND revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_consent_user ON claimius.consent (user_id) WHERE user_id IS NOT NULL;

GRANT SELECT, INSERT, UPDATE ON claimius.consent TO claimius_writer;
GRANT SELECT ON claimius.consent TO claimius_reader;

CREATE TABLE IF NOT EXISTS claimius.analytics_session (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id        UUID NOT NULL REFERENCES claimius.samna_app(id),
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

COMMENT ON TABLE claimius.analytics_session IS 'Durable context of one visit. Events reference it so context is written once.';
COMMENT ON COLUMN claimius.analytics_session.session_key IS 'Browser generated session id. Unique per app.';
COMMENT ON COLUMN claimius.analytics_session.context IS 'Everything else the recorder could read, as JSON.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_analytics_session_key ON claimius.analytics_session (app_id, session_key);
CREATE INDEX IF NOT EXISTS idx_analytics_session_user ON claimius.analytics_session (app_id, user_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_analytics_session_anonymous ON claimius.analytics_session (app_id, anonymous_id, started_at DESC);

GRANT SELECT, INSERT, UPDATE ON claimius.analytics_session TO claimius_writer;
GRANT SELECT ON claimius.analytics_session TO claimius_reader;

CREATE TABLE IF NOT EXISTS claimius.analytics_event (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id      UUID NOT NULL REFERENCES claimius.samna_app(id),
    session_id  UUID NOT NULL REFERENCES claimius.analytics_session(id),
    user_id     UUID,
    type        TEXT NOT NULL,
    path        TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.analytics_event IS 'Append only capture. One row per signal, pointer samples included.';
COMMENT ON COLUMN claimius.analytics_event.type IS 'Signal kind such as click, pointer, scroll, input, submit, view.';
COMMENT ON COLUMN claimius.analytics_event.payload IS 'Signal body. Coordinates, viewport, selector and text for a click, sample arrays for pointer.';
COMMENT ON COLUMN claimius.analytics_event.occurred_at IS 'Browser clock at capture. received_at is the server clock at write.';

CREATE INDEX IF NOT EXISTS idx_analytics_event_session ON claimius.analytics_event (session_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_analytics_event_path ON claimius.analytics_event (app_id, path, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_analytics_event_type ON claimius.analytics_event (app_id, type, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_analytics_event_received ON claimius.analytics_event USING BRIN (received_at);

GRANT SELECT, INSERT ON claimius.analytics_event TO claimius_writer;
GRANT SELECT ON claimius.analytics_event TO claimius_reader;

-- ----------------------------------------------------------------------------
-- Consent resolution and write
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_consent(
    p_app_id       UUID,
    p_user_id      UUID DEFAULT NULL,
    p_anonymous_id TEXT DEFAULT NULL
) RETURNS SETOF claimius.consent AS $$
    SELECT *
    FROM claimius.consent
    WHERE revoked_at IS NULL
      AND (
          (p_user_id IS NOT NULL AND user_id = p_user_id)
          OR (p_anonymous_id IS NOT NULL AND user_id IS NULL AND anonymous_id = p_anonymous_id AND app_id = p_app_id)
      )
    ORDER BY (app_id = p_app_id) DESC, granted_at DESC
    LIMIT 1;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_consent(UUID, UUID, TEXT) IS 'Strongest live consent for an identity. A known user matches across apps, preferring the asking app. An anonymous visitor matches within the app only.';

CREATE OR REPLACE FUNCTION claimius.post_consent(
    p_app_id         UUID,
    p_anonymous_id   TEXT,
    p_categories     JSONB,
    p_policy_version TEXT,
    p_user_id        UUID DEFAULT NULL,
    p_ip             INET DEFAULT NULL,
    p_user_agent     TEXT DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row claimius.consent;
BEGIN
    UPDATE claimius.consent
    SET revoked_at = now(), sa_updated_at = now()
    WHERE app_id = p_app_id
      AND revoked_at IS NULL
      AND (
          (p_user_id IS NOT NULL AND user_id = p_user_id)
          OR (p_user_id IS NULL AND user_id IS NULL AND anonymous_id = p_anonymous_id)
      );

    INSERT INTO claimius.consent (
        app_id, user_id, anonymous_id, categories, policy_version, ip, user_agent
    ) VALUES (
        p_app_id, p_user_id, p_anonymous_id, COALESCE(p_categories, '{}'::jsonb), p_policy_version, p_ip, p_user_agent
    )
    RETURNING * INTO v_row;

    RETURN jsonb_build_object('consent', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.post_consent(UUID, TEXT, JSONB, TEXT, UUID, INET, TEXT) IS 'Records a consent decision, revoking the previous live one for the same identity.';

CREATE OR REPLACE FUNCTION claimius.consent_allows(
    p_app_id       UUID,
    p_category     TEXT,
    p_user_id      UUID DEFAULT NULL,
    p_anonymous_id TEXT DEFAULT NULL
) RETURNS BOOLEAN AS $$
    SELECT COALESCE(
        (SELECT (categories ->> p_category)::boolean
         FROM claimius.get_consent(p_app_id, p_user_id, p_anonymous_id)),
        FALSE
    );
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.consent_allows(UUID, TEXT, UUID, TEXT) IS 'True when the identity granted one category. Absent consent is a denial.';

-- ----------------------------------------------------------------------------
-- Capture
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.post_analytics_session(
    p_app_id       UUID,
    p_anonymous_id TEXT,
    p_session_key  TEXT,
    p_context      JSONB DEFAULT '{}'::jsonb,
    p_user_id      UUID DEFAULT NULL,
    p_ip           INET DEFAULT NULL,
    p_user_agent   TEXT DEFAULT NULL
) RETURNS JSONB AS $$
DECLARE
    v_row claimius.analytics_session;
BEGIN
    IF NOT claimius.consent_allows(p_app_id, 'analytics', p_user_id, p_anonymous_id) THEN
        RETURN jsonb_build_object('analytics_session', NULL);
    END IF;

    INSERT INTO claimius.analytics_session (
        app_id, user_id, anonymous_id, session_key, entry_path, referrer,
        user_agent, ip, language, timezone, platform,
        screen_width, screen_height, device_pixel_ratio, context
    ) VALUES (
        p_app_id, p_user_id, p_anonymous_id, p_session_key,
        p_context ->> 'entry_path',
        p_context ->> 'referrer',
        p_user_agent,
        p_ip,
        p_context ->> 'language',
        p_context ->> 'timezone',
        p_context ->> 'platform',
        (p_context ->> 'screen_width')::integer,
        (p_context ->> 'screen_height')::integer,
        (p_context ->> 'device_pixel_ratio')::real,
        p_context
    )
    ON CONFLICT (app_id, session_key) DO UPDATE
        SET last_seen_at = now(),
            user_id      = COALESCE(EXCLUDED.user_id, claimius.analytics_session.user_id)
    RETURNING * INTO v_row;

    RETURN jsonb_build_object('analytics_session', to_jsonb(v_row));
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.post_analytics_session(UUID, TEXT, TEXT, JSONB, UUID, INET, TEXT) IS 'Opens or touches the session row for a visit. Returns a null body when the analytics category is not granted.';

CREATE OR REPLACE FUNCTION claimius.post_analytics_events(
    p_app_id      UUID,
    p_session_key TEXT,
    p_events      JSONB
) RETURNS INTEGER AS $$
DECLARE
    v_session claimius.analytics_session;
    v_written INTEGER;
BEGIN
    SELECT * INTO v_session
    FROM claimius.analytics_session
    WHERE app_id = p_app_id AND session_key = p_session_key;

    IF NOT FOUND THEN
        RETURN 0;
    END IF;

    IF NOT claimius.consent_allows(p_app_id, 'analytics', v_session.user_id, v_session.anonymous_id) THEN
        RETURN 0;
    END IF;

    INSERT INTO claimius.analytics_event (app_id, session_id, user_id, type, path, payload, occurred_at)
    SELECT p_app_id,
           v_session.id,
           v_session.user_id,
           e ->> 'type',
           COALESCE(e ->> 'path', ''),
           COALESCE(e -> 'payload', '{}'::jsonb),
           to_timestamp((e ->> 'occurred_at')::double precision / 1000.0)
    FROM jsonb_array_elements(p_events) AS e
    WHERE e ->> 'type' IS NOT NULL;

    GET DIAGNOSTICS v_written = ROW_COUNT;

    UPDATE claimius.analytics_session
    SET last_seen_at = now()
    WHERE id = v_session.id;

    RETURN v_written;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.post_analytics_events(UUID, TEXT, JSONB) IS 'Writes a batch against an open session. Returns 0 and writes nothing when consent is absent or the session is unknown.';

CREATE OR REPLACE FUNCTION claimius.get_analytics_events(
    p_app_id  UUID,
    p_user_id UUID DEFAULT NULL,
    p_type    TEXT DEFAULT NULL,
    p_path    TEXT DEFAULT NULL,
    p_limit   INTEGER DEFAULT 100,
    p_offset  INTEGER DEFAULT 0
) RETURNS SETOF claimius.analytics_event AS $$
    SELECT *
    FROM claimius.analytics_event
    WHERE app_id = p_app_id
      AND (p_user_id IS NULL OR user_id = p_user_id)
      AND (p_type IS NULL OR type = p_type)
      AND (p_path IS NULL OR path = p_path)
    ORDER BY occurred_at DESC
    LIMIT p_limit OFFSET p_offset;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_events(UUID, UUID, TEXT, TEXT, INTEGER, INTEGER) IS 'Filtered capture read for the samna_auth list view, shaped like get_audit.';

GRANT EXECUTE ON FUNCTION claimius.get_consent(UUID, UUID, TEXT) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.get_consent(UUID, UUID, TEXT) TO claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.post_consent(UUID, TEXT, JSONB, TEXT, UUID, INET, TEXT) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.consent_allows(UUID, TEXT, UUID, TEXT) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.consent_allows(UUID, TEXT, UUID, TEXT) TO claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.post_analytics_session(UUID, TEXT, TEXT, JSONB, UUID, INET, TEXT) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.post_analytics_events(UUID, TEXT, JSONB) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.get_analytics_events(UUID, UUID, TEXT, TEXT, INTEGER, INTEGER) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.get_analytics_events(UUID, UUID, TEXT, TEXT, INTEGER, INTEGER) TO claimius_reader;
