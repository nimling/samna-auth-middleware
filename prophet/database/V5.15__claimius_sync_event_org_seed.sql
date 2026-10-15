CREATE OR REPLACE FUNCTION claimius.get_sync_events(
    p_app_id    UUID,
    p_txid      BIGINT,
    p_seq       BIGINT,
    p_limit     INTEGER DEFAULT 1000,
    p_peer      BOOLEAN DEFAULT FALSE
) RETURNS TABLE (
    txid        BIGINT,
    seq         BIGINT,
    operation   claimius.sync_operation,
    event_type  TEXT,
    payload     JSONB,
    written_at  TIMESTAMPTZ
) AS $$
    SELECT e.txid::TEXT::BIGINT, e.seq, e.operation, e.event_type,
           CASE WHEN NOT p_peer AND e.event_type = 'claimius.organization' THEN e.payload - 'private_seed'
                WHEN NOT p_peer AND e.event_type = 'claimius.user_token' THEN e.payload - 'data'
                ELSE e.payload END,
           e.written_at
    FROM claimius.sync_event e
    WHERE e.app_id = p_app_id
      AND (e.txid, e.seq) > (p_txid::TEXT::XID8, p_seq)
      AND (e.txid < pg_snapshot_xmin(pg_current_snapshot()) OR e.txid = pg_current_xact_id_if_assigned())
      AND e.event_type NOT IN ('claimius.user_object', 'claimius.user_users', 'claimius.user_users_diff')
      AND (p_peer OR e.event_type <> 'claimius.app_handover')
    ORDER BY e.txid ASC, e.seq ASC
    LIMIT p_limit;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_sync_events(UUID, BIGINT, BIGINT, INTEGER, BOOLEAN) IS 'Delta feed of source rows for one app after a (txid, seq) cursor, in (txid, seq) order. Serves only events whose transaction is below the xmin of the reading snapshot, or the reader''s own. Every event written later carries a txid at or above that xmin, so the cursor never passes an event still to be served, whatever order sequences commit in. Organization and location events are written once per linked app, so other apps'' shared hierarchy rows never leave the prophet. Handover events travel to peer prophets only, and an organization''s private_seed, the key of its apps'' app_secret, and a user_token''s data, the claims of the token it was issued from, travel to peer prophets only.';
