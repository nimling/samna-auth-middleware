-- ============================================================================
-- Analytics heat: folded cells arrive from the browser
-- ============================================================================
-- The capture script folds pointer samples into cells before it posts, so a
-- session sends one heat event per page and viewport width per flush, holding
-- [bucket_x, bucket_y, count] triples on the grid this schema already uses.
-- The fold reads those beside the positioned signals it always read, so a
-- click still lands as a click and a folded pointer field lands as pointer.
-- ============================================================================

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
    WITH signals AS (
        SELECT
            e.app_id,
            e.path,
            (e.payload ->> 'viewport_width')::int AS viewport_width,
            e.type,
            floor((e.payload ->> 'page_x')::numeric / claimius.analytics_bucket_size())::int AS bucket_x,
            floor((e.payload ->> 'page_y')::numeric / claimius.analytics_bucket_size())::int AS bucket_y,
            (e.occurred_at AT TIME ZONE 'UTC')::date AS day,
            claimius.analytics_event_weight(e.type) AS weight,
            e.session_id
        FROM claimius.analytics_event e
        WHERE (p_from IS NULL OR (e.occurred_at AT TIME ZONE 'UTC')::date >= p_from)
          AND (p_to IS NULL OR (e.occurred_at AT TIME ZONE 'UTC')::date <= p_to)
          AND (p_app_id IS NULL OR e.app_id = p_app_id)
          AND e.type <> 'heat'
          AND e.payload ? 'page_x'
          AND e.payload ? 'page_y'
          AND e.payload ? 'viewport_width'
        UNION ALL
        SELECT
            e.app_id,
            e.path,
            (e.payload ->> 'viewport_width')::int,
            COALESCE(e.payload ->> 'source', 'pointer'),
            (c ->> 0)::int,
            (c ->> 1)::int,
            (e.occurred_at AT TIME ZONE 'UTC')::date,
            (c ->> 2)::int * claimius.analytics_event_weight(COALESCE(e.payload ->> 'source', 'pointer')),
            e.session_id
        FROM claimius.analytics_event e
            CROSS JOIN LATERAL jsonb_array_elements(e.payload -> 'cells') AS c
        WHERE (p_from IS NULL OR (e.occurred_at AT TIME ZONE 'UTC')::date >= p_from)
          AND (p_to IS NULL OR (e.occurred_at AT TIME ZONE 'UTC')::date <= p_to)
          AND (p_app_id IS NULL OR e.app_id = p_app_id)
          AND e.type = 'heat'
          AND jsonb_typeof(e.payload -> 'cells') = 'array'
          AND e.payload ? 'viewport_width'
    )
    SELECT
        s.app_id,
        s.path,
        s.viewport_width,
        s.type,
        s.bucket_x,
        s.bucket_y,
        s.day,
        sum(s.weight),
        count(DISTINCT s.session_id)
    FROM signals s
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

COMMENT ON FUNCTION claimius.upsert_analytics_heat(DATE, DATE, UUID) IS 'Folds every positioned signal and every browser folded heat event on a range of whole days into its cell and returns the cells written. A day is the unit because a cell is replaced rather than added to.';
