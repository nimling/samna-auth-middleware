ALTER TABLE claimius.sync_event ADD COLUMN IF NOT EXISTS app_id UUID;

UPDATE claimius.sync_event
SET app_id = (payload ->> 'app_id')::uuid
WHERE app_id IS NULL AND payload ? 'app_id';

DROP INDEX IF EXISTS claimius.idx_sync_event_app_seq;
CREATE INDEX IF NOT EXISTS idx_sync_event_app_txid_seq ON claimius.sync_event (app_id, txid, seq);

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

DROP FUNCTION IF EXISTS claimius.get_sync_events(UUID, BIGINT, INTEGER);
DROP FUNCTION IF EXISTS claimius.get_sync_events(UUID, BIGINT, BIGINT, INTEGER);

DROP FUNCTION IF EXISTS claimius.get_sync_seq(UUID);

CREATE OR REPLACE FUNCTION claimius.get_sync_seq(p_app_id UUID)
    RETURNS TABLE (txid BIGINT, seq BIGINT) AS $$
    SELECT coalesce(max(h.txid), 0), coalesce(max(h.seq), 0)
    FROM (
        SELECT e.txid::TEXT::BIGINT AS txid, e.seq
        FROM claimius.sync_event e
        WHERE e.app_id = p_app_id
        ORDER BY e.txid DESC, e.seq DESC
        LIMIT 1
    ) h;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_sync_seq(UUID) IS 'Highest (txid, seq) position written for one app. A routed write answers with it, and the disciple that routed the write reads its own write once its cursor reaches it.';

DROP FUNCTION IF EXISTS claimius.check_sync_cursor(UUID, BIGINT);

CREATE OR REPLACE FUNCTION claimius.check_sync_cursor(p_app_id UUID, p_txid BIGINT, p_seq BIGINT)
    RETURNS TEXT AS $$
DECLARE
    v_issued    BIGINT;
    v_trimmed   RECORD;
    v_events    BIGINT;
    v_rows      BIGINT;
    v_txid      XID8 := p_txid::TEXT::XID8;
BEGIN
    v_issued := coalesce(pg_sequence_last_value(pg_get_serial_sequence('claimius.sync_event', 'seq')::REGCLASS), 0);
    IF p_seq > v_issued OR v_txid > pg_snapshot_xmax(pg_current_snapshot()) THEN
        RETURN 'ahead';
    END IF;

    SELECT ps.trimmed_txid, ps.trimmed_seq INTO v_trimmed FROM claimius.prophet_state ps WHERE ps.app_id = p_app_id;
    IF FOUND AND (v_txid, p_seq) < (v_trimmed.trimmed_txid, v_trimmed.trimmed_seq) THEN
        RETURN 'trimmed';
    END IF;

    SELECT count(*) INTO v_events
    FROM (SELECT 1 FROM claimius.sync_event e WHERE e.app_id = p_app_id AND (e.txid, e.seq) > (v_txid, p_seq) LIMIT 1001) b;
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
    FROM (SELECT 1 FROM claimius.sync_event e WHERE e.app_id = p_app_id AND (e.txid, e.seq) > (v_txid, p_seq) LIMIT v_rows + 1) b;
    IF v_events > v_rows THEN
        RETURN 'backlog';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.check_sync_cursor(UUID, BIGINT, BIGINT) IS 'Whether a consumer at a (txid, seq) cursor must take the snapshot instead of the feed. NULL when the feed serves it; ahead when the cursor is above every sequence or transaction the prophet issued; trimmed when events after the cursor were trimmed; backlog when the app''s events after the cursor outnumber its source rows.';

CREATE OR REPLACE FUNCTION claimius.check_sync_home(p_app_id UUID, p_prophet TEXT)
    RETURNS TEXT AS $$
    SELECT CASE
        WHEN a.home_prophet IS NOT NULL THEN a.home_prophet
        WHEN coalesce(p_prophet, '') IN ('', l.slug) THEN NULL
        WHEN EXISTS (
            SELECT 1 FROM claimius.app_handover h
            WHERE h.app_id = a.id
              AND h.from_prophet = p_prophet
              AND h.to_prophet = l.slug
              AND h.status = 'accepted'
        ) THEN NULL
        ELSE p_prophet
    END
    FROM claimius.samna_app a
        CROSS JOIN (SELECT slug FROM claimius.get_app()) l
    WHERE a.id = p_app_id;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.check_sync_home(UUID, TEXT) IS 'Whether this prophet serves the feed of an app to a consumer bound to p_prophet. NULL when it does: it is the app''s home and the consumer is unbound, bound here, or bound to a prophet that handed the app over here and saw it accepted. Otherwise the slug of the prophet the consumer stays with: the app''s home, or the one it is bound to.';

CREATE OR REPLACE FUNCTION claimius.get_peer_snapshot(p_app_id UUID)
    RETURNS TABLE (event_type TEXT, payload JSONB) AS $$
    SELECT s.event_type, s.payload
    FROM (
        SELECT 1 AS step, 0 AS level, 'claimius.samna_app' AS event_type, to_jsonb(a) - 'txid' AS payload, a.sa_created_at AS created
        FROM claimius.samna_app a WHERE a.id = p_app_id
        UNION ALL
        SELECT 2, 0, 'claimius.app_handover', to_jsonb(h) - 'txid', h.sa_created_at
        FROM claimius.app_handover h WHERE h.app_id = p_app_id
        UNION ALL
        SELECT 3, o.sa_level, 'claimius.organization', to_jsonb(o) - 'txid', o.sa_created_at
        FROM claimius.organization o WHERE o.id IN (SELECT claimius.app_object_ids(p_app_id, 'claimius.organization'))
        UNION ALL
        SELECT 4, l.sa_level, 'claimius.location', to_jsonb(l) - 'txid', l.sa_created_at
        FROM claimius.location l WHERE l.id IN (SELECT claimius.app_object_ids(p_app_id, 'claimius.location'))
        UNION ALL
        SELECT 5, 0, 'claimius.samna_user', to_jsonb(u) - 'txid', u.sa_created_at
        FROM claimius.samna_user u WHERE u.app_id = p_app_id
        UNION ALL
        SELECT 6, 0, 'claimius.claim', to_jsonb(c) - 'txid', c.sa_created_at
        FROM claimius.claim c WHERE c.app_id = p_app_id
        UNION ALL
        SELECT 7, 0, 'claimius.user_claim', to_jsonb(uc) - 'txid', uc.sa_created_at
        FROM claimius.user_claim uc WHERE uc.app_id = p_app_id
        UNION ALL
        SELECT 8, 0, 'claimius.claim_object', to_jsonb(co) - 'txid', co.sa_created_at
        FROM claimius.claim_object co WHERE co.app_id = p_app_id
        UNION ALL
        SELECT 9, 0, 'claimius.samna_client', to_jsonb(sc) - 'txid', sc.sa_created_at
        FROM claimius.samna_client sc WHERE sc.app_id = p_app_id
        UNION ALL
        SELECT 10, 0, 'claimius.samna_secret', to_jsonb(ss) - 'txid', ss.sa_created_at
        FROM claimius.samna_secret ss WHERE ss.app_id = p_app_id
        UNION ALL
        SELECT 11, 0, 'claimius.object_field', to_jsonb(f) - 'txid', f.sa_created_at
        FROM claimius.object_field f WHERE f.app_id = p_app_id
        UNION ALL
        SELECT 12, 0, 'claimius.user_field', to_jsonb(uf) - 'txid', uf.sa_created_at
        FROM claimius.user_field uf WHERE uf.app_id = p_app_id
        UNION ALL
        SELECT 13, 0, 'claimius.user_relation', to_jsonb(r) - 'txid', r.sa_created_at
        FROM claimius.user_relation r WHERE r.app_id = p_app_id
        UNION ALL
        SELECT 14, 0, 'claimius.object_asset', to_jsonb(oa) - 'txid', oa.sa_created_at
        FROM claimius.object_asset oa
        WHERE p_app_id IN (SELECT claimius.apps_for_object_asset(oa.object_type, oa.object_id, oa.app_id))
    ) s
    ORDER BY s.step, s.level, s.created;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_peer_snapshot(UUID) IS 'Every source row of one app in the shape its feed events carry, for a peer prophet mirroring the app: the app row and its handovers first, parents before children, so each row applies against rows already applied. The object_asset links come last, every link apps_for_object_asset publishes to the app.';
