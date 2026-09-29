ALTER TABLE claimius.sync_event ADD COLUMN IF NOT EXISTS app_id UUID;

UPDATE claimius.sync_event
SET app_id = (payload ->> 'app_id')::uuid
WHERE app_id IS NULL AND payload ? 'app_id';

CREATE INDEX IF NOT EXISTS idx_sync_event_app_seq ON claimius.sync_event (app_id, seq);

WITH legacy AS (
    DELETE FROM claimius.sync_event
    WHERE app_id IS NULL
      AND event_type IN ('claimius.organization', 'claimius.location')
    RETURNING event_type, (payload ->> 'id')::UUID AS object_id
),
touched AS (
    SELECT DISTINCT event_type, object_id FROM legacy
)
INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
SELECT CASE WHEN r.payload IS NULL THEN 'delete' ELSE 'upsert' END::claimius.sync_operation,
       t.event_type,
       coalesce(r.payload, jsonb_build_object('id', t.object_id)),
       ao.app_id
FROM touched t
    JOIN claimius.app_object ao
        ON ao.object_type = t.event_type
       AND ao.object_id = t.object_id
       AND ao.sa_deleted_at IS NULL
    LEFT JOIN LATERAL (
        SELECT to_jsonb(o) AS payload FROM claimius.organization o
        WHERE t.event_type = 'claimius.organization' AND o.id = t.object_id
        UNION ALL
        SELECT to_jsonb(l) FROM claimius.location l
        WHERE t.event_type = 'claimius.location' AND l.id = t.object_id
    ) r ON TRUE
ORDER BY t.event_type, t.object_id, ao.app_id;

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
    WHERE e.app_id = p_app_id
      AND e.seq > p_seq
      AND (e.txid < pg_snapshot_xmin(pg_current_snapshot()) OR e.txid = pg_current_xact_id_if_assigned())
      AND e.event_type NOT IN ('claimius.user_object', 'claimius.user_users', 'claimius.audit')
    ORDER BY e.seq ASC
    LIMIT p_limit;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_sync_events(UUID, BIGINT, INTEGER) IS 'Delta feed of source rows for one app after a sequence, in sequence order. Serves only events whose transaction is below the xmin of the reading snapshot, or the reader''s own, so an event whose lower sequence commits after a higher one is never passed over. Organization and location events are written once per linked app, so other apps'' shared hierarchy rows never leave the prophet.';

CREATE OR REPLACE FUNCTION claimius.get_sync_seq(p_app_id UUID)
    RETURNS BIGINT AS $$
    SELECT coalesce(max(e.seq), 0)
    FROM claimius.sync_event e
    WHERE e.app_id = p_app_id;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_sync_seq(UUID) IS 'Highest sequence written for one app. A routed write answers with it, and the disciple that routed the write reads its own write once its cursor reaches it.';

CREATE OR REPLACE FUNCTION claimius.check_sync_cursor(p_app_id UUID, p_seq BIGINT)
    RETURNS TEXT AS $$
DECLARE
    v_issued    BIGINT;
    v_trimmed   BIGINT;
    v_events    BIGINT;
    v_rows      BIGINT;
BEGIN
    v_issued := coalesce(pg_sequence_last_value(pg_get_serial_sequence('claimius.sync_event', 'seq')::REGCLASS), 0);
    IF p_seq > v_issued THEN
        RETURN 'ahead';
    END IF;

    SELECT ps.trimmed_seq INTO v_trimmed FROM claimius.prophet_state ps WHERE ps.app_id = p_app_id;
    IF p_seq < coalesce(v_trimmed, 0) THEN
        RETURN 'trimmed';
    END IF;

    SELECT count(*) INTO v_events
    FROM (SELECT 1 FROM claimius.sync_event e WHERE e.app_id = p_app_id AND e.seq > p_seq LIMIT 1001) b;
    IF v_events = 0 THEN
        RETURN NULL;
    END IF;
    IF claimius.count_sync_source_rows(p_app_id, v_events) < v_events THEN
        RETURN 'backlog';
    END IF;
    IF v_events <= 1000 THEN
        RETURN NULL;
    END IF;

    v_rows := claimius.count_sync_source_rows(p_app_id);
    SELECT count(*) INTO v_events
    FROM (SELECT 1 FROM claimius.sync_event e WHERE e.app_id = p_app_id AND e.seq > p_seq LIMIT v_rows + 1) b;
    IF v_events > v_rows THEN
        RETURN 'backlog';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.check_sync_cursor(UUID, BIGINT) IS 'Whether a consumer at a cursor must take the snapshot instead of the feed. NULL when the feed serves it; ahead when the cursor is above every sequence the prophet issued; trimmed when events after the cursor were trimmed; backlog when the app''s events after the cursor outnumber its source rows.';
