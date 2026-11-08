DROP FUNCTION IF EXISTS claimius.post_analytics_capture(UUID, TEXT, TEXT, BYTEA);

CREATE OR REPLACE FUNCTION claimius.upsert_analytics_capture(
    p_app_id       UUID,
    p_path         TEXT,
    p_content_type TEXT,
    p_body         BYTEA
) RETURNS claimius.analytics_capture AS $$
    INSERT INTO claimius.analytics_capture (app_id, path, content_type, body)
    VALUES (p_app_id, p_path, p_content_type, p_body)
    ON CONFLICT (app_id, path) DO UPDATE SET
        content_type = EXCLUDED.content_type,
        body         = EXCLUDED.body,
        captured_at  = now()
    RETURNING *;
$$ LANGUAGE sql;

COMMENT ON FUNCTION claimius.upsert_analytics_capture(UUID, TEXT, TEXT, BYTEA) IS 'Stores the snapshot of a page, replacing the one held for the same app and path.';

GRANT EXECUTE ON FUNCTION claimius.upsert_analytics_capture(UUID, TEXT, TEXT, BYTEA) TO claimius_writer;
