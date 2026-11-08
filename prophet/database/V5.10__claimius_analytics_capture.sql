-- ============================================================================
-- Claimius V5.10 Analytics capture
-- ----------------------------------------------------------------------------
-- The heat layer is drawn over a picture of the page, not over the live site,
-- because a site that answers X-Frame-Options refuses to be framed at all.
-- The browser rasterises the page it is on and posts it with the view event.
--
-- One row per app, path and viewport width. A later capture replaces the one
-- it matches, so the store holds the current look of every page rather than a
-- row per visit.
-- ============================================================================

CREATE TABLE IF NOT EXISTS claimius.analytics_capture (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id         UUID NOT NULL REFERENCES claimius.samna_app(id),
    path           TEXT NOT NULL,
    viewport_width INTEGER NOT NULL,
    page_height    INTEGER NOT NULL,
    content_type   TEXT NOT NULL,
    image          BYTEA NOT NULL,
    captured_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (app_id, path, viewport_width)
);

COMMENT ON TABLE claimius.analytics_capture IS 'A picture of a page as the browser rendered it, one per app, path and viewport width.';
COMMENT ON COLUMN claimius.analytics_capture.viewport_width IS 'Width the page laid out at. Interaction coordinates scale against this same number.';
COMMENT ON COLUMN claimius.analytics_capture.page_height IS 'Full scroll height of the document at capture, so the reader can size the frame before the image loads.';
COMMENT ON COLUMN claimius.analytics_capture.image IS 'Raster bytes as posted. content_type says what they are.';

CREATE INDEX IF NOT EXISTS idx_analytics_capture_app ON claimius.analytics_capture (app_id, captured_at DESC);

GRANT SELECT, INSERT, UPDATE ON claimius.analytics_capture TO claimius_writer;
GRANT SELECT ON claimius.analytics_capture TO claimius_reader;

-- ----------------------------------------------------------------------------
-- Capture write and read
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.post_analytics_capture(
    p_app_id         UUID,
    p_path           TEXT,
    p_viewport_width INTEGER,
    p_page_height    INTEGER,
    p_content_type   TEXT,
    p_image          BYTEA
) RETURNS claimius.analytics_capture AS $$
    INSERT INTO claimius.analytics_capture (
        app_id, path, viewport_width, page_height, content_type, image
    ) VALUES (
        p_app_id, p_path, p_viewport_width, p_page_height, p_content_type, p_image
    )
    ON CONFLICT (app_id, path, viewport_width) DO UPDATE SET
        page_height  = EXCLUDED.page_height,
        content_type = EXCLUDED.content_type,
        image        = EXCLUDED.image,
        captured_at  = now()
    RETURNING *;
$$ LANGUAGE sql;

COMMENT ON FUNCTION claimius.post_analytics_capture(UUID, TEXT, INTEGER, INTEGER, TEXT, BYTEA) IS 'Stores the picture of a page, replacing the one held for the same app, path and viewport width.';

CREATE OR REPLACE FUNCTION claimius.get_analytics_capture(
    p_app_id         UUID,
    p_path           TEXT,
    p_viewport_width INTEGER DEFAULT NULL
) RETURNS SETOF claimius.analytics_capture AS $$
    SELECT *
    FROM claimius.analytics_capture
    WHERE app_id = p_app_id
      AND path = p_path
    ORDER BY (p_viewport_width IS NOT NULL AND viewport_width = p_viewport_width) DESC,
             captured_at DESC
    LIMIT 1;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_capture(UUID, TEXT, INTEGER) IS 'The picture held for a page, preferring the asked viewport width and falling back to the most recent capture of that path.';

-- ----------------------------------------------------------------------------
-- Capture staleness
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.analytics_capture_due(
    p_app_id         UUID,
    p_path           TEXT,
    p_viewport_width INTEGER,
    p_max_age        INTERVAL DEFAULT '7 days'
) RETURNS BOOLEAN AS $$
    SELECT NOT EXISTS (
        SELECT 1
        FROM claimius.analytics_capture
        WHERE app_id = p_app_id
          AND path = p_path
          AND viewport_width = p_viewport_width
          AND captured_at > now() - p_max_age
    );
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.analytics_capture_due(UUID, TEXT, INTEGER, INTERVAL) IS 'True when no fresh picture is held, which is what tells one visiting browser in a while to pay the cost of rasterising the page.';
