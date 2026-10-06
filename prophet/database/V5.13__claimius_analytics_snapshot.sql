DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = 'claimius' AND table_name = 'analytics_capture' AND column_name = 'image') THEN
        DELETE FROM claimius.analytics_capture;
        ALTER TABLE claimius.analytics_capture DROP CONSTRAINT IF EXISTS analytics_capture_app_id_path_viewport_width_key;
        ALTER TABLE claimius.analytics_capture DROP COLUMN IF EXISTS viewport_width;
        ALTER TABLE claimius.analytics_capture DROP COLUMN IF EXISTS page_height;
        ALTER TABLE claimius.analytics_capture RENAME COLUMN image TO body;
        ALTER TABLE claimius.analytics_capture ADD CONSTRAINT analytics_capture_app_id_path_key UNIQUE (app_id, path);
    END IF;
END $$;

COMMENT ON TABLE claimius.analytics_capture IS 'A serialised copy of a page as the browser held it, one per app and path.';
COMMENT ON COLUMN claimius.analytics_capture.body IS 'The document tree as posted, masked. content_type says what it is.';

DROP FUNCTION IF EXISTS claimius.post_analytics_capture(UUID, TEXT, INTEGER, INTEGER, TEXT, BYTEA);
DROP FUNCTION IF EXISTS claimius.get_analytics_capture(UUID, TEXT, INTEGER);
DROP FUNCTION IF EXISTS claimius.analytics_capture_due(UUID, TEXT, INTEGER, INTERVAL);

CREATE OR REPLACE FUNCTION claimius.get_analytics_capture(
    p_app_id UUID,
    p_path   TEXT
) RETURNS SETOF claimius.analytics_capture AS $$
    SELECT *
    FROM claimius.analytics_capture
    WHERE app_id = p_app_id
      AND path = p_path;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_capture(UUID, TEXT) IS 'The snapshot held for a page.';

CREATE OR REPLACE FUNCTION claimius.get_analytics_captured(
    p_app_id  UUID,
    p_max_age INTERVAL DEFAULT '7 days'
) RETURNS SETOF TEXT AS $$
    SELECT path
    FROM claimius.analytics_capture
    WHERE app_id = p_app_id
      AND captured_at > now() - p_max_age
    ORDER BY path;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_captured(UUID, INTERVAL) IS 'The paths an app holds a fresh snapshot for. The consent answer carries the list so a browser knows which pages need no capture without asking per view.';

CREATE OR REPLACE FUNCTION claimius.get_analytics_summary(
    p_user_id      UUID,
    p_producer_app UUID,
    p_from         TIMESTAMPTZ DEFAULT NULL,
    p_to           TIMESTAMPTZ DEFAULT NULL
) RETURNS JSONB AS $$
    WITH visits AS (
        SELECT s.id,
               COALESCE(s.user_id::text, s.anonymous_id) AS actor,
               s.user_id,
               s.anonymous_id,
               s.ip,
               s.user_agent,
               s.started_at
        FROM claimius.analytics_session s
        WHERE s.app_id = p_producer_app
          AND p_producer_app IN (SELECT claimius.capture_user_apps(p_user_id))
          AND (p_from IS NULL OR s.started_at >= p_from)
          AND (p_to IS NULL OR s.started_at < p_to)
    ),
    signals AS (
        SELECT e.session_id,
               e.type,
               e.path,
               (e.payload ->> 'viewport_width')::int AS viewport_width
        FROM claimius.analytics_event e
            JOIN visits v ON v.id = e.session_id
        WHERE e.type <> 'heat'
    ),
    actor_paths AS (
        SELECT v.actor,
               g.path,
               row_number() OVER (PARTITION BY v.actor ORDER BY count(*) DESC, g.path) AS rank
        FROM signals g
            JOIN visits v ON v.id = g.session_id
        GROUP BY v.actor, g.path
    ),
    actors AS (
        SELECT v.actor AS id,
               max(v.user_id::text) AS user_id,
               max(v.anonymous_id) AS anonymous_id,
               max(v.ip::text) AS ip,
               max(v.user_agent) AS user_agent,
               count(DISTINCT v.id) AS sessions,
               count(g.session_id) AS events,
               max(v.started_at) AS last_seen
        FROM visits v
            LEFT JOIN signals g ON g.session_id = v.id
        GROUP BY v.actor
    ),
    paths AS (
        SELECT g.path,
               count(*) AS events,
               count(DISTINCT g.session_id) AS sessions,
               mode() WITHIN GROUP (ORDER BY g.viewport_width) AS viewport_width
        FROM signals g
        GROUP BY g.path
    )
    SELECT jsonb_build_object(
        'actors', COALESCE((
            SELECT jsonb_agg(jsonb_build_object(
                'id', a.id,
                'user_id', a.user_id,
                'anonymous_id', a.anonymous_id,
                'ip', a.ip,
                'user_agent', a.user_agent,
                'sessions', a.sessions,
                'events', a.events,
                'last_seen', a.last_seen,
                'path', (SELECT ap.path FROM actor_paths ap WHERE ap.actor = a.id AND ap.rank = 1)
            ) ORDER BY a.events DESC, a.last_seen DESC)
            FROM actors a
        ), '[]'::jsonb),
        'paths', COALESCE((
            SELECT jsonb_agg(jsonb_build_object(
                'path', p.path,
                'events', p.events,
                'sessions', p.sessions,
                'viewport_width', p.viewport_width
            ) ORDER BY p.events DESC, p.path)
            FROM paths p
        ), '[]'::jsonb),
        'types', COALESCE((
            SELECT jsonb_agg(DISTINCT g.type)
            FROM signals g
        ), '[]'::jsonb)
    );
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_summary(UUID, UUID, TIMESTAMPTZ, TIMESTAMPTZ) IS 'Who visited a producer app, which paths they touched and at what width, and which signal kinds exist, for one reader, scoped exactly like get_analytics_events.';

GRANT EXECUTE ON FUNCTION claimius.get_analytics_capture(UUID, TEXT) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.get_analytics_capture(UUID, TEXT) TO claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_analytics_captured(UUID, INTERVAL) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.get_analytics_captured(UUID, INTERVAL) TO claimius_reader;
GRANT EXECUTE ON FUNCTION claimius.get_analytics_summary(UUID, UUID, TIMESTAMPTZ, TIMESTAMPTZ) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.get_analytics_summary(UUID, UUID, TIMESTAMPTZ, TIMESTAMPTZ) TO claimius_reader;
