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
           CASE WHEN NOT p_peer AND e.event_type = 'claimius.organization' THEN e.payload - 'private_seed' ELSE e.payload END,
           e.written_at
    FROM claimius.sync_event e
    WHERE e.app_id = p_app_id
      AND (e.txid, e.seq) > (p_txid::TEXT::XID8, p_seq)
      AND (e.txid < pg_snapshot_xmin(pg_current_snapshot()) OR e.txid = pg_current_xact_id_if_assigned())
      AND e.event_type NOT IN ('claimius.user_object', 'claimius.user_users', 'claimius.audit')
      AND (p_peer OR e.event_type <> 'claimius.app_handover')
      AND (e.operation <> 'upsert'
           OR e.event_type NOT IN ('claimius.claim', 'claimius.user_claim', 'claimius.claim_object')
           OR claimius.app_has_scope(p_app_id, (e.payload ->> 'sa_owner_id')::UUID,
                  e.payload ->> 'object_type', (e.payload ->> 'object_id')::UUID))
    ORDER BY e.txid ASC, e.seq ASC
    LIMIT p_limit;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_sync_events(UUID, BIGINT, BIGINT, INTEGER, BOOLEAN) IS 'Delta feed of source rows for one app after a (txid, seq) cursor, in (txid, seq) order. Serves only events whose transaction is below the xmin of the reading snapshot, or the reader''s own. Every event written later carries a txid at or above that xmin, so the cursor never passes an event still to be served, whatever order sequences commit in. Organization and location events are written once per linked app, so other apps'' shared hierarchy rows never leave the prophet. A claim, user_claim or claim_object whose owning organization, or bound organization or location, is not linked to the app is left out, since no consumer could apply it; linking the organization publishes it again. Handover events travel to peer prophets only, and an organization''s private_seed, the key of its apps'' app_secret, travels to peer prophets only.';

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
        FROM claimius.claim c WHERE c.app_id = p_app_id AND claimius.app_has_scope(p_app_id, c.sa_owner_id)
        UNION ALL
        SELECT 7, 0, 'claimius.user_claim', to_jsonb(uc) - 'txid', uc.sa_created_at
        FROM claimius.user_claim uc WHERE uc.app_id = p_app_id AND claimius.app_has_scope(p_app_id, uc.sa_owner_id)
        UNION ALL
        SELECT 8, 0, 'claimius.claim_object', to_jsonb(co) - 'txid', co.sa_created_at
        FROM claimius.claim_object co
        WHERE co.app_id = p_app_id AND claimius.app_has_scope(p_app_id, co.sa_owner_id, co.object_type, co.object_id)
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
    ) s
    ORDER BY s.step, s.level, s.created;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_peer_snapshot(UUID) IS 'Every source row of one app in the shape its feed events carry, for a peer prophet mirroring the app: the app row and its handovers first, parents before children, so each row applies against rows already applied. A claim, user_claim or claim_object whose owning organization, or bound organization or location, is not linked to the app is left out.';
