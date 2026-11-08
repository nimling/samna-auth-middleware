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
    ('audit', 365, 'claimius.delete_expired_audit'),
    ('notification', 90, 'claimius.delete_expired_notification')
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
