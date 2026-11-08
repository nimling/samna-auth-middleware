CREATE TABLE IF NOT EXISTS claimius.retention (
    kind        TEXT PRIMARY KEY,
    days        INTEGER NOT NULL CHECK (days > 0),
    prune       TEXT NOT NULL,
    pruned      BIGINT,
    pruned_at   TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.retention IS 'How long each kind of runtime row is kept. One row per kind, its days editable through update_retention; the leader of the retention job calls delete_expired for every row once a day.';
COMMENT ON COLUMN claimius.retention.prune IS 'The function delete_expired calls with make_interval(days => days), taking one INTERVAL and returning the rows removed. A host registers its own kinds with upsert_retention.';
COMMENT ON COLUMN claimius.retention.pruned IS 'Rows the last run removed.';
COMMENT ON COLUMN claimius.retention.pruned_at IS 'When the last run finished.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_audit(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_cut       TIMESTAMPTZ := now() - p_keep;
    v_partition TEXT;
    v_n         BIGINT;
    v_deleted   BIGINT := 0;
BEGIN
    FOR v_partition IN
        SELECT c.relname
        FROM pg_inherits i
            JOIN pg_class c ON c.oid = i.inhrelid
        WHERE i.inhparent = 'claimius.audit'::REGCLASS
          AND c.relname ~ '^audit_[0-9]{6}$'
          AND (to_date(substr(c.relname, 7), 'YYYYMM')::TIMESTAMP AT TIME ZONE 'UTC') + INTERVAL '1 month' <= v_cut
        ORDER BY c.relname
    LOOP
        EXECUTE format('SELECT count(*) FROM claimius.%I', v_partition) INTO v_n;
        EXECUTE format('DROP TABLE claimius.%I', v_partition);
        v_deleted := v_deleted + v_n;
    END LOOP;
    DELETE FROM claimius.audit_default WHERE sa_created_at < v_cut;
    GET DIAGNOSTICS v_n = ROW_COUNT;
    RETURN v_deleted + v_n;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_audit(INTERVAL) IS 'Drops every monthly audit partition whose month ended before now less p_keep, and deletes the rows of audit_default older than that. Returns the rows removed. The audit retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_notification(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_n BIGINT;
BEGIN
    DELETE FROM claimius.notification
    WHERE sa_deleted_at < now() - p_keep
       OR closed_at < now() - p_keep;
    GET DIAGNOSTICS v_n = ROW_COUNT;
    RETURN v_n;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_notification(INTERVAL) IS 'Deletes the notifications deleted or closed before now less p_keep. The notification retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_peer_collision(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_n BIGINT;
BEGIN
    DELETE FROM claimius.peer_collision WHERE sa_created_at < now() - p_keep;
    GET DIAGNOSTICS v_n = ROW_COUNT;
    RETURN v_n;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_peer_collision(INTERVAL) IS 'Deletes the refused mirror rows recorded before now less p_keep. The peer_collision retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_analytics_session(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_n BIGINT;
BEGIN
    DELETE FROM claimius.analytics_session s
    WHERE s.last_seen_at < now() - p_keep
      AND NOT EXISTS (SELECT 1 FROM claimius.analytics_event e WHERE e.session_id = s.id);
    GET DIAGNOSTICS v_n = ROW_COUNT;
    RETURN v_n;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_analytics_session(INTERVAL) IS 'Deletes the sessions last seen before now less p_keep that no raw event refers to any more. The analytics_session retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_analytics_heat(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_n BIGINT;
BEGIN
    DELETE FROM claimius.analytics_heat WHERE day < ((now() - p_keep) AT TIME ZONE 'UTC')::date;
    GET DIAGNOSTICS v_n = ROW_COUNT;
    RETURN v_n;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_analytics_heat(INTERVAL) IS 'Deletes the heat cells of every UTC day before the day p_keep ago. The analytics_heat retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_analytics_capture(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_n BIGINT;
BEGIN
    DELETE FROM claimius.analytics_capture WHERE captured_at < now() - p_keep;
    GET DIAGNOSTICS v_n = ROW_COUNT;
    RETURN v_n;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_analytics_capture(INTERVAL) IS 'Deletes the page snapshots taken before now less p_keep, of pages no browser has captured since. The analytics_capture retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_consent(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_n BIGINT;
BEGIN
    DELETE FROM claimius.consent
    WHERE revoked_at < now() - p_keep
       OR (user_id IS NULL AND revoked_at IS NULL AND sa_updated_at < now() - p_keep);
    GET DIAGNOSTICS v_n = ROW_COUNT;
    RETURN v_n;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_consent(INTERVAL) IS 'Deletes the decisions superseded before now less p_keep, and the live decisions of anonymous visitors not renewed since, who are asked again. The consent retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_rate_limit(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_n BIGINT;
BEGIN
    DELETE FROM claimius.rate_limit WHERE tat < now() - p_keep;
    GET DIAGNOSTICS v_n = ROW_COUNT;
    RETURN v_n;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_rate_limit(INTERVAL) IS 'Deletes the keys whose theoretical arrival time passed before now less p_keep; such a key limits exactly as an absent one. The rate_limit retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_par_request(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_n BIGINT;
BEGIN
    DELETE FROM claimius.par_request WHERE expires_at < now() - p_keep;
    GET DIAGNOSTICS v_n = ROW_COUNT;
    RETURN v_n;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_par_request(INTERVAL) IS 'Deletes the pushed authorization requests that expired unused before now less p_keep. The par_request retention prune.';

CREATE OR REPLACE FUNCTION claimius.upsert_retention(p_kind TEXT, p_days INTEGER, p_prune TEXT)
    RETURNS VOID AS $$
BEGIN
    PERFORM p_prune::REGPROC;
    INSERT INTO claimius.retention (kind, days, prune)
    VALUES (p_kind, p_days, p_prune)
    ON CONFLICT (kind) DO UPDATE SET prune = EXCLUDED.prune;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_retention(TEXT, INTEGER, TEXT) IS 'Registers a kind with its default days and its prune function, which must exist. A kind already registered keeps the days it was given and takes the prune. Run from a migration; a host registers its own runtime tables the same way.';

SELECT claimius.upsert_retention(r.kind, r.days, r.prune)
FROM (VALUES
    ('access_diff', 7, 'claimius.trim_access_diff'),
    ('sync_event', 7, 'claimius.trim_sync_event'),
    ('audit', 365, 'claimius.delete_expired_audit'),
    ('notification', 90, 'claimius.delete_expired_notification'),
    ('peer_collision', 30, 'claimius.delete_expired_peer_collision'),
    ('analytics_event', 30, 'claimius.delete_analytics_event'),
    ('analytics_session', 30, 'claimius.delete_expired_analytics_session'),
    ('analytics_heat', 365, 'claimius.delete_expired_analytics_heat'),
    ('analytics_capture', 90, 'claimius.delete_expired_analytics_capture'),
    ('consent', 365, 'claimius.delete_expired_consent'),
    ('rate_limit', 1, 'claimius.delete_expired_rate_limit'),
    ('par_request', 1, 'claimius.delete_expired_par_request')
) AS r (kind, days, prune);

CREATE OR REPLACE FUNCTION claimius.delete_expired(p_kind TEXT)
    RETURNS BIGINT AS $$
DECLARE
    v_retention claimius.retention;
    v_deleted   BIGINT;
BEGIN
    SELECT * INTO v_retention FROM claimius.retention WHERE kind = p_kind;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'delete_expired: no retention for %', p_kind USING ERRCODE = 'P0404';
    END IF;
    EXECUTE format('SELECT %s($1)', v_retention.prune::REGPROC) INTO v_deleted USING make_interval(days => v_retention.days);
    UPDATE claimius.retention SET pruned = v_deleted, pruned_at = now() WHERE kind = p_kind;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.delete_expired(TEXT) IS 'Runs the prune of one kind with its days as the interval to keep, records the rows removed and when, and returns them. Raises P0404 for a kind with no retention row. Called by the leader of the retention job once a day per row.';

CREATE OR REPLACE FUNCTION claimius.get_retention()
    RETURNS SETOF claimius.retention AS $$
    SELECT * FROM claimius.retention ORDER BY kind;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_retention() IS 'Every retention row. The caller checks read on the owner organization of the app before it answers.';

CREATE OR REPLACE FUNCTION claimius.update_retention(p_kind TEXT, p_days INTEGER)
    RETURNS JSONB AS $$
DECLARE
    v_retention claimius.retention;
BEGIN
    IF p_days IS NULL OR p_days < 1 THEN
        RAISE EXCEPTION 'retention: days must be at least 1, got %', p_days USING ERRCODE = '22023';
    END IF;
    UPDATE claimius.retention
    SET days = p_days, updated_at = now()
    WHERE kind = p_kind
    RETURNING * INTO v_retention;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'retention: no kind %', p_kind USING ERRCODE = 'P0404';
    END IF;
    RETURN jsonb_build_object('retention', to_jsonb(v_retention));
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.update_retention(TEXT, INTEGER) IS 'Sets the days of one kind. P0404 for an unknown kind, 22023 for fewer than one day. The caller checks write on the owner organization of the app first.';
