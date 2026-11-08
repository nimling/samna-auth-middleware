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

CREATE INDEX IF NOT EXISTS idx_analytics_capture_app ON claimius.analytics_capture (app_id, captured_at DESC);

GRANT SELECT, INSERT, UPDATE ON claimius.analytics_capture TO claimius_writer;
GRANT SELECT ON claimius.analytics_capture TO claimius_reader;

