ALTER TABLE claimius.sync_event ADD COLUMN IF NOT EXISTS app_id UUID;

UPDATE claimius.sync_event
SET app_id = (payload ->> 'app_id')::uuid
WHERE app_id IS NULL AND payload ? 'app_id';

CREATE INDEX IF NOT EXISTS idx_sync_event_app_seq ON claimius.sync_event (app_id, seq);

CREATE OR REPLACE FUNCTION claimius.get_sync_events(
    p_app_id    UUID,
    p_seq       BIGINT,
    p_limit     INTEGER DEFAULT 1000
) RETURNS TABLE (
    seq         BIGINT,
    operation   claimius.sync_operation,
    event_type  TEXT,
    payload     JSONB,
    written_at  TIMESTAMPTZ
) AS $$
    SELECT e.seq, e.operation, e.event_type, e.payload, e.written_at
    FROM claimius.sync_event e
    WHERE e.seq > p_seq
      AND (
          e.app_id = p_app_id
          OR (
              e.app_id IS NULL
              AND e.event_type IN ('claimius.organization', 'claimius.location')
              AND (e.payload ->> 'id')::UUID IN (SELECT claimius.app_object_ids(p_app_id, e.event_type))
          )
      )
    ORDER BY e.seq ASC
    LIMIT p_limit;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_sync_events(UUID, BIGINT, INTEGER) IS 'Delta feed for one app after a sequence. Rows carrying the app id, plus organization and location rows the app reaches through app_object. Other apps'' shared hierarchy rows never leave the prophet.';
