CREATE TABLE IF NOT EXISTS claimius.analytics_heat (
    app_id         UUID NOT NULL REFERENCES claimius.samna_app(id),
    path           TEXT NOT NULL,
    viewport_width INTEGER NOT NULL,
    type           TEXT NOT NULL,
    bucket_x       INTEGER NOT NULL,
    bucket_y       INTEGER NOT NULL,
    day            DATE NOT NULL,
    weight         BIGINT NOT NULL DEFAULT 0,
    sessions       BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (app_id, path, viewport_width, type, day, bucket_x, bucket_y)
);

COMMENT ON TABLE claimius.analytics_heat IS 'The heat field as a grid. One row per cell per kind per day, folded out of analytics_event.';
COMMENT ON COLUMN claimius.analytics_heat.bucket_x IS 'Page x at capture scale divided by claimius.analytics_bucket_size, floored.';
COMMENT ON COLUMN claimius.analytics_heat.bucket_y IS 'Page y at capture scale divided by claimius.analytics_bucket_size, floored.';
COMMENT ON COLUMN claimius.analytics_heat.weight IS 'Summed weight in the cell. A click counts 4 and every other kind counts 1, the same weighting the overlay draws with.';
COMMENT ON COLUMN claimius.analytics_heat.sessions IS 'Distinct sessions that put weight in the cell, so a single visitor cannot read as a hot area.';

CREATE INDEX IF NOT EXISTS idx_analytics_heat_read ON claimius.analytics_heat (app_id, path, day DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.analytics_heat TO claimius_writer;
GRANT SELECT ON claimius.analytics_heat TO claimius_reader;

CREATE OR REPLACE FUNCTION claimius.analytics_bucket_size() RETURNS INTEGER AS $$
    SELECT 16;
$$ LANGUAGE sql IMMUTABLE;

COMMENT ON FUNCTION claimius.analytics_bucket_size() IS 'Cell edge in pixels at capture scale. Well under the blur radius the overlay draws with, so the rendered field is unchanged by the fold.';

CREATE OR REPLACE FUNCTION claimius.analytics_event_weight(p_type TEXT) RETURNS INTEGER AS $$
    SELECT CASE WHEN p_type = 'click' THEN 4 ELSE 1 END;
$$ LANGUAGE sql IMMUTABLE;

COMMENT ON FUNCTION claimius.analytics_event_weight(TEXT) IS 'What one signal contributes to a cell. Matches the weight the capture script and the overlay already agree on.';

DROP FUNCTION IF EXISTS claimius.post_analytics_heat(date,date,uuid);
CREATE OR REPLACE FUNCTION claimius.upsert_analytics_heat(
    p_from   DATE,
    p_to     DATE,
    p_app_id UUID DEFAULT NULL
) RETURNS BIGINT AS $$
DECLARE
    v_cells BIGINT;
BEGIN
    INSERT INTO claimius.analytics_heat AS h (
        app_id, path, viewport_width, type, bucket_x, bucket_y, day, weight, sessions
    )
    SELECT
        e.app_id,
        e.path,
        (e.payload ->> 'viewport_width')::int,
        e.type,
        floor((e.payload ->> 'page_x')::numeric / claimius.analytics_bucket_size())::int,
        floor((e.payload ->> 'page_y')::numeric / claimius.analytics_bucket_size())::int,
        (e.occurred_at AT TIME ZONE 'UTC')::date,
        sum(claimius.analytics_event_weight(e.type)),
        count(DISTINCT e.session_id)
    FROM claimius.analytics_event e
    WHERE (p_from IS NULL OR (e.occurred_at AT TIME ZONE 'UTC')::date >= p_from)
      AND (p_to IS NULL OR (e.occurred_at AT TIME ZONE 'UTC')::date <= p_to)
      AND (p_app_id IS NULL OR e.app_id = p_app_id)
      AND e.payload ? 'page_x'
      AND e.payload ? 'page_y'
      AND e.payload ? 'viewport_width'
    GROUP BY 1, 2, 3, 4, 5, 6, 7
    ON CONFLICT (app_id, path, viewport_width, type, day, bucket_x, bucket_y) DO UPDATE SET
        weight   = EXCLUDED.weight,
        sessions = EXCLUDED.sessions
    WHERE h.weight IS DISTINCT FROM EXCLUDED.weight
       OR h.sessions IS DISTINCT FROM EXCLUDED.sessions;

    GET DIAGNOSTICS v_cells = ROW_COUNT;
    RETURN v_cells;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_analytics_heat(DATE, DATE, UUID) IS 'Folds every positioned signal on a range of whole days into its cell and returns the cells written. A day is the unit because a cell is replaced rather than added to: re folding a whole day is always correct, and folding half a day would throw the other half away.';

CREATE OR REPLACE FUNCTION claimius.get_analytics_heat(
    p_app_id         UUID,
    p_path           TEXT,
    p_viewport_width INTEGER DEFAULT NULL,
    p_type           TEXT DEFAULT NULL,
    p_from           DATE DEFAULT NULL,
    p_to             DATE DEFAULT NULL
) RETURNS TABLE (x INTEGER, y INTEGER, weight BIGINT, sessions BIGINT) AS $$
    SELECT
        h.bucket_x * claimius.analytics_bucket_size(),
        h.bucket_y * claimius.analytics_bucket_size(),
        sum(h.weight)::bigint,
        max(h.sessions)
    FROM claimius.analytics_heat h
    WHERE h.app_id = p_app_id
      AND h.path = p_path
      AND (p_viewport_width IS NULL OR h.viewport_width = p_viewport_width)
      AND (p_type IS NULL OR h.type = p_type)
      AND (p_from IS NULL OR h.day >= p_from)
      AND (p_to IS NULL OR h.day <= p_to)
    GROUP BY 1, 2;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_analytics_heat(UUID, TEXT, INTEGER, TEXT, DATE, DATE) IS 'The heat field for a page, already in page coordinates at capture scale, so the caller passes the rows straight to the overlay.';

CREATE OR REPLACE FUNCTION claimius.delete_analytics_event(p_before DATE) RETURNS BIGINT AS $$
DECLARE
    v_rows BIGINT;
BEGIN
    IF p_before IS NULL THEN
        RAISE EXCEPTION 'delete_analytics_event needs a cutoff';
    END IF;

    IF p_before > (now() AT TIME ZONE 'UTC')::date THEN
        RAISE EXCEPTION 'delete_analytics_event cannot drop a day that has not finished';
    END IF;

    PERFORM claimius.upsert_analytics_heat(NULL, p_before - 1);

    DELETE FROM claimius.analytics_event
    WHERE (occurred_at AT TIME ZONE 'UTC')::date < p_before;
    GET DIAGNOSTICS v_rows = ROW_COUNT;
    RETURN v_rows;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_analytics_event(DATE) IS 'Drops raw signal from every day before the cutoff, folding those days first so the heat field keeps everything it had. The cutoff is a day boundary and never today, because folding an unfinished day would discard the rest of it.';
