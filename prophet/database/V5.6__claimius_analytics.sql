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

DROP FUNCTION IF EXISTS claimius.post_consent(uuid,text,jsonb,text,uuid,inet,text);
CREATE OR REPLACE FUNCTION claimius.insert_consent(
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

COMMENT ON FUNCTION claimius.insert_consent(UUID, TEXT, JSONB, TEXT, UUID, INET, TEXT) IS 'Records a consent decision, revoking the previous live one for the same identity.';

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

DROP FUNCTION IF EXISTS claimius.post_analytics_session(uuid,text,text,jsonb,uuid,inet,text);
CREATE OR REPLACE FUNCTION claimius.upsert_analytics_session(
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

COMMENT ON FUNCTION claimius.upsert_analytics_session(UUID, TEXT, TEXT, JSONB, UUID, INET, TEXT) IS 'Opens or touches the session row for a visit. Returns a null body when the analytics category is not granted.';

DROP FUNCTION IF EXISTS claimius.post_analytics_events(uuid,text,jsonb);
CREATE OR REPLACE FUNCTION claimius.insert_analytics_events(
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
           least(greatest(coalesce(to_timestamp((e ->> 'occurred_at')::double precision / 1000.0), now()), now() - INTERVAL '1 day'), now())
    FROM jsonb_array_elements(p_events) AS e
    WHERE e ->> 'type' IS NOT NULL;

    GET DIAGNOSTICS v_written = ROW_COUNT;

    UPDATE claimius.analytics_session
    SET last_seen_at = now()
    WHERE id = v_session.id;

    RETURN v_written;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.insert_analytics_events(UUID, TEXT, JSONB) IS 'Writes a batch against an open session, each occurred_at clamped into the day before now so a browser clock cannot place a signal outside the days the fold and the retention cover. Returns 0 and writes nothing when consent is absent or the session is unknown.';

DROP FUNCTION IF EXISTS claimius.capture_app_access(UUID, UUID);
DROP FUNCTION IF EXISTS claimius.capture_app_ids(UUID);

CREATE OR REPLACE FUNCTION claimius.capture_user_apps(p_user_id UUID)
    RETURNS SETOF UUID AS $$
    WITH granted AS (
        SELECT c.app_id, co.scope
        FROM claimius.user_claim uc
            JOIN claimius.claim c ON c.id = uc.claim_id
            JOIN claimius.claim_object co ON co.claim_id = c.id
        WHERE uc.user_id = p_user_id
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
          AND co.sa_deleted_at IS NULL
          AND (uc.starts_at IS NULL OR uc.starts_at <= now())
          AND (uc.ends_at IS NULL OR uc.ends_at > now())
          AND co.object_id IS NULL
          AND jsonb_path_exists(co.scope, '$.analytics'::jsonpath)
          AND (claimius._effective_access(c.sa_access, co.sa_access) & 16) = 0
          AND (claimius._effective_access(c.sa_access, co.sa_access) & 4) = 4
    )
    SELECT app_id FROM granted
    UNION
    SELECT named::uuid
    FROM granted, jsonb_array_elements_text(
        CASE WHEN jsonb_typeof(scope -> 'analytics' -> 'app') = 'array'
             THEN scope -> 'analytics' -> 'app'
             ELSE '[]'::jsonb END) AS named
    WHERE named ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$';
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.capture_user_apps(UUID) IS 'Producer apps whose capture one person may read: the app each live analytics grant sits in, plus every app id named in that grant''s scope.';

CREATE OR REPLACE FUNCTION claimius.analytics_enabled(p_app_id UUID)
    RETURNS BOOLEAN AS $$
    SELECT EXISTS (
        SELECT 1
        FROM claimius.user_claim uc
            JOIN claimius.claim c ON c.id = uc.claim_id
            JOIN claimius.claim_object co ON co.claim_id = uc.claim_id
        WHERE c.app_id = p_app_id
          AND uc.sa_deleted_at IS NULL
          AND c.sa_deleted_at IS NULL
          AND co.sa_deleted_at IS NULL
          AND (uc.starts_at IS NULL OR uc.starts_at <= now())
          AND (uc.ends_at IS NULL OR uc.ends_at > now())
          AND co.object_id IS NULL
          AND jsonb_path_exists(co.scope, '$.analytics'::jsonpath)
          AND (claimius._effective_access(c.sa_access, co.sa_access) & 16) = 0
    );
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.analytics_enabled(UUID) IS 'True when the app holds a live object-less grant carrying the analytics scope. A NULL claim_object access inherits the claim mask.';

DROP FUNCTION IF EXISTS claimius.get_analytics_events(UUID, UUID, TEXT, TEXT, INTEGER, INTEGER);
DROP FUNCTION IF EXISTS claimius.get_analytics_events(UUID, UUID, UUID, UUID, TEXT, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER);
DROP FUNCTION IF EXISTS claimius.get_analytics_sessions(UUID, UUID, UUID, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER);

CREATE OR REPLACE FUNCTION claimius.get_analytics_events(
    p_user_id      UUID,
    p_producer_app UUID DEFAULT NULL,
    p_session_id   UUID DEFAULT NULL,
    p_visitor      UUID DEFAULT NULL,
    p_type         TEXT DEFAULT NULL,
    p_path         TEXT DEFAULT NULL,
    p_from         TIMESTAMPTZ DEFAULT NULL,
    p_to           TIMESTAMPTZ DEFAULT NULL,
    p_limit        INTEGER DEFAULT 100,
    p_offset       INTEGER DEFAULT 0
) RETURNS SETOF claimius.analytics_event AS $$
    SELECT e.*
    FROM claimius.analytics_event e
    WHERE e.app_id IN (SELECT claimius.capture_user_apps(p_user_id))
      AND (p_producer_app IS NULL OR e.app_id = p_producer_app)
      AND (p_session_id IS NULL OR e.session_id = p_session_id)
      AND (p_visitor IS NULL OR e.user_id = p_visitor)
      AND (p_type IS NULL OR e.type = p_type)
      AND (p_path IS NULL OR e.path = p_path)
      AND (p_from IS NULL OR e.occurred_at >= p_from)
      AND (p_to IS NULL OR e.occurred_at < p_to)
    ORDER BY e.occurred_at DESC
    LIMIT p_limit OFFSET p_offset;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_events(UUID, UUID, UUID, UUID, TEXT, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER) IS 'Capture read for one person, across every producer app their analytics grant covers. app_id names the producer and never decides access.';

CREATE OR REPLACE FUNCTION claimius.get_analytics_sessions(
    p_user_id      UUID,
    p_producer_app UUID DEFAULT NULL,
    p_visitor      UUID DEFAULT NULL,
    p_anonymous_id TEXT DEFAULT NULL,
    p_from         TIMESTAMPTZ DEFAULT NULL,
    p_to           TIMESTAMPTZ DEFAULT NULL,
    p_limit        INTEGER DEFAULT 100,
    p_offset       INTEGER DEFAULT 0
) RETURNS SETOF claimius.analytics_session AS $$
    SELECT s.*
    FROM claimius.analytics_session s
    WHERE s.app_id IN (SELECT claimius.capture_user_apps(p_user_id))
      AND (p_producer_app IS NULL OR s.app_id = p_producer_app)
      AND (p_visitor IS NULL OR s.user_id = p_visitor)
      AND (p_anonymous_id IS NULL OR s.anonymous_id = p_anonymous_id)
      AND (p_from IS NULL OR s.started_at >= p_from)
      AND (p_to IS NULL OR s.started_at < p_to)
    ORDER BY s.started_at DESC
    LIMIT p_limit OFFSET p_offset;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_sessions(UUID, UUID, UUID, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER) IS 'Visit read for one person, scoped exactly like get_analytics_events.';

DROP FUNCTION IF EXISTS claimius.get_analytics_apps(UUID);

CREATE OR REPLACE FUNCTION claimius.get_analytics_apps(p_user_id UUID)
    RETURNS TABLE (
        id        UUID,
        slug      TEXT,
        name      TEXT,
        app_image TEXT,
        origin    TEXT,
        sessions  BIGINT,
        events    BIGINT,
        visitors  BIGINT,
        last_seen TIMESTAMPTZ
    ) AS $$
    SELECT a.id,
           a.slug,
           a.name,
           a.app_image,
           substring(a.redirect_uris[1] FROM '^[a-zA-Z][a-zA-Z0-9+.-]*://[^/]+'),
           COUNT(DISTINCT s.id),
           COALESCE(SUM((SELECT COUNT(*) FROM claimius.analytics_event e WHERE e.session_id = s.id)), 0),
           COUNT(DISTINCT COALESCE(s.user_id::text, s.anonymous_id)),
           MAX(s.last_seen_at)
    FROM claimius.samna_app a
        LEFT JOIN claimius.analytics_session s ON s.app_id = a.id
    WHERE a.id IN (SELECT claimius.capture_user_apps(p_user_id))
      AND a.sa_deleted_at IS NULL
    GROUP BY a.id, a.slug, a.name, a.app_image, a.redirect_uris
    ORDER BY a.name;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_apps(UUID) IS 'The producer apps one person may read, named, with the volume captured against each and the origin its pages are served from. A disciple carries no samna_app replica, so this is where a capture reader learns what an app_id is and where to load the page a heat map overlays.';

GRANT EXECUTE ON FUNCTION claimius.get_consent(UUID, UUID, TEXT) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.get_consent(UUID, UUID, TEXT) TO claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.insert_consent(UUID, TEXT, JSONB, TEXT, UUID, INET, TEXT) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.consent_allows(UUID, TEXT, UUID, TEXT) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.consent_allows(UUID, TEXT, UUID, TEXT) TO claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.upsert_analytics_session(UUID, TEXT, TEXT, JSONB, UUID, INET, TEXT) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.insert_analytics_events(UUID, TEXT, JSONB) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.capture_user_apps(UUID) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.capture_user_apps(UUID) TO claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.analytics_enabled(UUID) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.analytics_enabled(UUID) TO claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_analytics_events(UUID, UUID, UUID, UUID, TEXT, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.get_analytics_events(UUID, UUID, UUID, UUID, TEXT, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER) TO claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_analytics_sessions(UUID, UUID, UUID, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.get_analytics_sessions(UUID, UUID, UUID, TEXT, TIMESTAMPTZ, TIMESTAMPTZ, INTEGER, INTEGER) TO claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_analytics_apps(UUID) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.get_analytics_apps(UUID) TO claimius_reader;
