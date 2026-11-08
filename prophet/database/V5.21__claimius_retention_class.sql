DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace WHERE t.typname = 'retention_class' AND n.nspname = 'claimius') THEN
        CREATE TYPE claimius.retention_class AS ENUM ('log', 'run', 'token', 'cache', 'queue', 'audit', 'record');
    END IF;
END $$;

COMMENT ON TYPE claimius.retention_class IS 'What a retention kind holds. log, run, token, cache and queue are deleted by age through delete_expired; audit and record are exported to cold storage through get_expired and only then deleted through delete_exported. Application data has no class and is never age pruned.';

ALTER TABLE claimius.retention ADD COLUMN IF NOT EXISTS class claimius.retention_class;
ALTER TABLE claimius.retention ADD COLUMN IF NOT EXISTS export TEXT;

COMMENT ON TABLE claimius.retention IS 'How long each kind of runtime row is kept. One row per kind with its class, its days editable through update_retention; the leader of the retention job runs every row once a day, delete_expired for a log, run, token, cache or queue kind, get_expired then delete_exported for an audit or record kind.';
COMMENT ON COLUMN claimius.retention.class IS 'The retention_class of the kind. An audit or record kind is never deleted unless its rows were exported first.';
COMMENT ON COLUMN claimius.retention.prune IS 'For a log, run, token, cache or queue kind the function delete_expired calls with make_interval(days => days), taking one INTERVAL and returning the rows removed. For an audit or record kind the function delete_exported calls with the keys of one exported batch, taking TEXT[] and returning the rows removed.';
COMMENT ON COLUMN claimius.retention.export IS 'For an audit or record kind the function get_expired calls with (make_interval(days => days), after, limit), returning TABLE (key TEXT, data JSONB): the expired rows with a key greater than after, at most limit, in key order. NULL for every other class.';

CREATE TABLE IF NOT EXISTS claimius.retention_export (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind        TEXT NOT NULL,
    object_path TEXT NOT NULL UNIQUE,
    first_key   TEXT NOT NULL,
    last_key    TEXT NOT NULL,
    row_count   BIGINT NOT NULL CHECK (row_count > 0),
    sha256      TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    exported_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_retention_export_kind ON claimius.retention_export (kind, exported_at DESC);

COMMENT ON TABLE claimius.retention_export IS 'The manifest of cold storage. One row per exported batch of an audit or record kind, written by delete_exported in the transaction that deletes exactly those rows. Never pruned.';
COMMENT ON COLUMN claimius.retention_export.object_path IS 'The object in the sink: <app_slug>/<kind>/<yyyy>/<mm>/<first key>-<last key>.ndjson.gz, one JSON row per line, gzip.';
COMMENT ON COLUMN claimius.retention_export.sha256 IS 'Lower case hex sha256 of the object as written.';

CREATE OR REPLACE FUNCTION claimius.get_expired_audit(p_keep INTERVAL, p_after TEXT, p_limit INTEGER)
    RETURNS TABLE (key TEXT, data JSONB) AS $$
    SELECT lpad((extract(EPOCH FROM a.sa_created_at) * 1000000)::BIGINT::TEXT, 16, '0') || replace(a.id::TEXT, '-', ''),
           to_jsonb(a)
    FROM claimius.audit a
    WHERE (a.sa_created_at < date_trunc('month', now() - p_keep, 'UTC')
           OR (a.tableoid = 'claimius.audit_default'::REGCLASS AND a.sa_created_at < now() - p_keep))
      AND ((a.sa_created_at, a.id) > (TIMESTAMPTZ 'epoch' + left(NULLIF(p_after, ''), 16)::BIGINT * INTERVAL '1 microsecond',
                                      substr(NULLIF(p_after, ''), 17)::UUID)) IS NOT FALSE
    ORDER BY a.sa_created_at, a.id
    LIMIT p_limit;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_expired_audit(INTERVAL, TEXT, INTEGER) IS 'The audit rows of every UTC month that ended before now less p_keep, and of audit_default older than that, after p_after, at most p_limit, ordered by (sa_created_at, id). The key is the creation time in epoch microseconds, sixteen digits, then the id without hyphens. The audit export.';

CREATE OR REPLACE FUNCTION claimius.delete_exported_audit(p_keys TEXT[]) RETURNS BIGINT AS $$
DECLARE
    v_cut       TIMESTAMPTZ;
    v_partition TEXT;
    v_empty     BOOLEAN;
    v_deleted   BIGINT;
BEGIN
    DELETE FROM claimius.audit a
    USING (
        SELECT TIMESTAMPTZ 'epoch' + left(k, 16)::BIGINT * INTERVAL '1 microsecond' AS at, substr(k, 17)::UUID AS id
        FROM unnest(p_keys) AS k
    ) e
    WHERE a.sa_created_at = e.at AND a.id = e.id;
    GET DIAGNOSTICS v_deleted = ROW_COUNT;
    SELECT now() - make_interval(days => r.days) INTO v_cut FROM claimius.retention r WHERE r.kind = 'audit';
    FOR v_partition IN
        SELECT c.relname
        FROM pg_inherits i
            JOIN pg_class c ON c.oid = i.inhrelid
        WHERE i.inhparent = 'claimius.audit'::REGCLASS
          AND c.relname ~ '^audit_[0-9]{6}$'
          AND (to_date(substr(c.relname, 7), 'YYYYMM')::TIMESTAMP AT TIME ZONE 'UTC') + INTERVAL '1 month' <= v_cut
        ORDER BY c.relname
    LOOP
        EXECUTE format('SELECT NOT EXISTS (SELECT 1 FROM claimius.%I)', v_partition) INTO v_empty;
        IF v_empty THEN
            EXECUTE format('DROP TABLE claimius.%I', v_partition);
        END IF;
    END LOOP;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_exported_audit(TEXT[]) IS 'Deletes exactly the audit rows of p_keys, keys as get_expired_audit writes them, then drops every monthly partition whose month ended before the audit retention and that holds no row any more, so a partition is dropped only after each of its rows was exported. Returns the rows removed. The audit retention prune.';

CREATE OR REPLACE FUNCTION claimius.get_expired_consent(p_keep INTERVAL, p_after TEXT, p_limit INTEGER)
    RETURNS TABLE (key TEXT, data JSONB) AS $$
    SELECT replace(c.id::TEXT, '-', ''), to_jsonb(c)
    FROM claimius.consent c
    WHERE (c.revoked_at < now() - p_keep
           OR (c.user_id IS NULL AND c.revoked_at IS NULL AND c.sa_updated_at < now() - p_keep))
      AND (c.id > NULLIF(p_after, '')::UUID) IS NOT FALSE
    ORDER BY c.id
    LIMIT p_limit;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_expired_consent(INTERVAL, TEXT, INTEGER) IS 'The decisions superseded before now less p_keep, and the live decisions of anonymous visitors not renewed since, after p_after, at most p_limit, ordered by id. The key is the id without hyphens. The consent export.';

CREATE OR REPLACE FUNCTION claimius.delete_exported_consent(p_keys TEXT[]) RETURNS BIGINT AS $$
DECLARE
    v_n BIGINT;
BEGIN
    DELETE FROM claimius.consent WHERE id = ANY (p_keys::UUID[]);
    GET DIAGNOSTICS v_n = ROW_COUNT;
    RETURN v_n;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_exported_consent(TEXT[]) IS 'Deletes exactly the decisions of p_keys, keys as get_expired_consent writes them. Returns the rows removed. The consent retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_notification(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_cut     TIMESTAMPTZ := now() - p_keep;
    v_n       BIGINT;
    v_deleted BIGINT := 0;
BEGIN
    LOOP
        DELETE FROM claimius.notification
        WHERE id IN (
            SELECT n.id FROM claimius.notification n
            WHERE n.sa_deleted_at < v_cut OR n.closed_at < v_cut
            ORDER BY n.id
            LIMIT 10000
        );
        GET DIAGNOSTICS v_n = ROW_COUNT;
        v_deleted := v_deleted + v_n;
        EXIT WHEN v_n < 10000;
    END LOOP;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_notification(INTERVAL) IS 'Deletes, in batches of 10,000 by id, the notifications soft deleted or closed before now less p_keep; an open notification is never age pruned. The notification retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_peer_collision(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_cut     TIMESTAMPTZ := now() - p_keep;
    v_n       BIGINT;
    v_deleted BIGINT := 0;
BEGIN
    LOOP
        DELETE FROM claimius.peer_collision
        WHERE id IN (
            SELECT p.id FROM claimius.peer_collision p
            WHERE p.sa_created_at < v_cut
            ORDER BY p.id
            LIMIT 10000
        );
        GET DIAGNOSTICS v_n = ROW_COUNT;
        v_deleted := v_deleted + v_n;
        EXIT WHEN v_n < 10000;
    END LOOP;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_peer_collision(INTERVAL) IS 'Deletes, in batches of 10,000 by id, the refused mirror rows recorded before now less p_keep. The peer_collision retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_analytics_session(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_cut     TIMESTAMPTZ := now() - p_keep;
    v_n       BIGINT;
    v_deleted BIGINT := 0;
BEGIN
    LOOP
        DELETE FROM claimius.analytics_session
        WHERE id IN (
            SELECT s.id FROM claimius.analytics_session s
            WHERE s.last_seen_at < v_cut
              AND NOT EXISTS (SELECT 1 FROM claimius.analytics_event e WHERE e.session_id = s.id)
            ORDER BY s.id
            LIMIT 10000
        );
        GET DIAGNOSTICS v_n = ROW_COUNT;
        v_deleted := v_deleted + v_n;
        EXIT WHEN v_n < 10000;
    END LOOP;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_analytics_session(INTERVAL) IS 'Deletes, in batches of 10,000 by id, the sessions last seen before now less p_keep that no raw event refers to any more. The analytics_session retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_analytics_heat(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_day     DATE := ((now() - p_keep) AT TIME ZONE 'UTC')::date;
    v_n       BIGINT;
    v_deleted BIGINT := 0;
BEGIN
    LOOP
        DELETE FROM claimius.analytics_heat
        WHERE (app_id, path, viewport_width, type, day, bucket_x, bucket_y) IN (
            SELECT h.app_id, h.path, h.viewport_width, h.type, h.day, h.bucket_x, h.bucket_y
            FROM claimius.analytics_heat h
            WHERE h.day < v_day
            ORDER BY h.app_id, h.path, h.viewport_width, h.type, h.day, h.bucket_x, h.bucket_y
            LIMIT 10000
        );
        GET DIAGNOSTICS v_n = ROW_COUNT;
        v_deleted := v_deleted + v_n;
        EXIT WHEN v_n < 10000;
    END LOOP;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_analytics_heat(INTERVAL) IS 'Deletes, in batches of 10,000 by primary key, the heat cells of every UTC day before the day p_keep ago. The analytics_heat retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_analytics_capture(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_cut     TIMESTAMPTZ := now() - p_keep;
    v_n       BIGINT;
    v_deleted BIGINT := 0;
BEGIN
    LOOP
        DELETE FROM claimius.analytics_capture
        WHERE id IN (
            SELECT c.id FROM claimius.analytics_capture c
            WHERE c.captured_at < v_cut
            ORDER BY c.id
            LIMIT 10000
        );
        GET DIAGNOSTICS v_n = ROW_COUNT;
        v_deleted := v_deleted + v_n;
        EXIT WHEN v_n < 10000;
    END LOOP;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_analytics_capture(INTERVAL) IS 'Deletes, in batches of 10,000 by id, the page snapshots taken before now less p_keep, of pages no browser has captured since. The analytics_capture retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_rate_limit(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_cut     TIMESTAMPTZ := now() - p_keep;
    v_n       BIGINT;
    v_deleted BIGINT := 0;
BEGIN
    LOOP
        DELETE FROM claimius.rate_limit
        WHERE key IN (
            SELECT l.key FROM claimius.rate_limit l
            WHERE l.tat < v_cut
            ORDER BY l.key
            LIMIT 10000
        );
        GET DIAGNOSTICS v_n = ROW_COUNT;
        v_deleted := v_deleted + v_n;
        EXIT WHEN v_n < 10000;
    END LOOP;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_rate_limit(INTERVAL) IS 'Deletes, in batches of 10,000 by key, the keys whose theoretical arrival time passed before now less p_keep; such a key limits exactly as an absent one. The rate_limit retention prune.';

CREATE OR REPLACE FUNCTION claimius.delete_expired_par_request(p_keep INTERVAL) RETURNS BIGINT AS $$
DECLARE
    v_cut     TIMESTAMPTZ := now() - p_keep;
    v_n       BIGINT;
    v_deleted BIGINT := 0;
BEGIN
    LOOP
        DELETE FROM claimius.par_request
        WHERE request_uri IN (
            SELECT p.request_uri FROM claimius.par_request p
            WHERE p.expires_at < v_cut
            ORDER BY p.request_uri
            LIMIT 10000
        );
        GET DIAGNOSTICS v_n = ROW_COUNT;
        v_deleted := v_deleted + v_n;
        EXIT WHEN v_n < 10000;
    END LOOP;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.delete_expired_par_request(INTERVAL) IS 'Deletes, in batches of 10,000 by request_uri, the pushed authorization requests that expired unused before now less p_keep. The par_request retention prune.';

DROP FUNCTION IF EXISTS claimius.upsert_retention(TEXT, INTEGER, TEXT);
CREATE OR REPLACE FUNCTION claimius.upsert_retention(p_kind TEXT, p_class claimius.retention_class, p_days INTEGER, p_prune TEXT, p_export TEXT DEFAULT NULL)
    RETURNS VOID AS $$
BEGIN
    IF p_class IN ('audit', 'record') THEN
        IF p_export IS NULL THEN
            RAISE EXCEPTION 'upsert_retention: % is class %, which needs an export function', p_kind, p_class USING ERRCODE = '22023';
        END IF;
        PERFORM (p_export || '(INTERVAL, TEXT, INTEGER)')::REGPROCEDURE;
        PERFORM (p_prune || '(TEXT[])')::REGPROCEDURE;
    ELSE
        IF p_export IS NOT NULL THEN
            RAISE EXCEPTION 'upsert_retention: % is class %, which is deleted by age and takes no export function', p_kind, p_class USING ERRCODE = '22023';
        END IF;
        PERFORM (p_prune || '(INTERVAL)')::REGPROCEDURE;
    END IF;
    INSERT INTO claimius.retention (kind, class, days, prune, export)
    VALUES (p_kind, p_class, p_days, p_prune, p_export)
    ON CONFLICT (kind) DO UPDATE SET class = EXCLUDED.class, prune = EXCLUDED.prune, export = EXCLUDED.export;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_retention(TEXT, claimius.retention_class, INTEGER, TEXT, TEXT) IS 'Registers a kind with its class, its default days, its prune function and, for an audit or record kind, its export function, each of which must exist with the arguments its class calls it with. A kind already registered keeps the days it was given and takes the class, prune and export. Run from a migration; the one way a host registers its own runtime tables. Application data is never registered.';

SELECT claimius.upsert_retention(r.kind, r.class::claimius.retention_class, r.days, r.prune, r.export)
FROM (VALUES
    ('access_diff', 'log', 7, 'claimius.trim_access_diff', NULL),
    ('sync_event', 'log', 7, 'claimius.trim_sync_event', NULL),
    ('audit', 'audit', 365, 'claimius.delete_exported_audit', 'claimius.get_expired_audit'),
    ('notification', 'log', 90, 'claimius.delete_expired_notification', NULL),
    ('peer_collision', 'log', 30, 'claimius.delete_expired_peer_collision', NULL),
    ('analytics_event', 'log', 30, 'claimius.delete_analytics_event', NULL),
    ('analytics_session', 'log', 30, 'claimius.delete_expired_analytics_session', NULL),
    ('analytics_heat', 'log', 365, 'claimius.delete_expired_analytics_heat', NULL),
    ('analytics_capture', 'cache', 90, 'claimius.delete_expired_analytics_capture', NULL),
    ('consent', 'record', 365, 'claimius.delete_exported_consent', 'claimius.get_expired_consent'),
    ('rate_limit', 'token', 1, 'claimius.delete_expired_rate_limit', NULL),
    ('par_request', 'token', 1, 'claimius.delete_expired_par_request', NULL)
) AS r (kind, class, days, prune, export);

DELETE FROM claimius.retention WHERE class IS NULL;

ALTER TABLE claimius.retention ALTER COLUMN class SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'retention_export_class' AND conrelid = 'claimius.retention'::REGCLASS) THEN
        ALTER TABLE claimius.retention ADD CONSTRAINT retention_export_class CHECK ((class IN ('audit', 'record')) = (export IS NOT NULL));
    END IF;
END $$;

DROP FUNCTION IF EXISTS claimius.delete_expired_audit(INTERVAL);
DROP FUNCTION IF EXISTS claimius.delete_expired_consent(INTERVAL);

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
    IF v_retention.export IS NOT NULL THEN
        RAISE EXCEPTION 'delete_expired: % is class %, deleted only through delete_exported', p_kind, v_retention.class USING ERRCODE = '22023';
    END IF;
    EXECUTE format('SELECT %s($1)', v_retention.prune::REGPROC) INTO v_deleted USING make_interval(days => v_retention.days);
    UPDATE claimius.retention SET pruned = v_deleted, pruned_at = now() WHERE kind = p_kind;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.delete_expired(TEXT) IS 'Runs the prune of one log, run, token, cache or queue kind with its days as the interval to keep, records the rows removed and when, and returns them. Raises P0404 for a kind with no retention row and 22023 for an audit or record kind, which only delete_exported deletes. Called by the leader of the retention job once a day per row.';

CREATE OR REPLACE FUNCTION claimius.get_expired(p_kind TEXT, p_after TEXT, p_limit INTEGER)
    RETURNS TABLE (key TEXT, data JSONB) AS $$
DECLARE
    v_retention claimius.retention;
BEGIN
    SELECT * INTO v_retention FROM claimius.retention r WHERE r.kind = p_kind;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'get_expired: no retention for %', p_kind USING ERRCODE = 'P0404';
    END IF;
    IF v_retention.export IS NULL THEN
        RAISE EXCEPTION 'get_expired: % is class %, deleted by age through delete_expired', p_kind, v_retention.class USING ERRCODE = '22023';
    END IF;
    RETURN QUERY EXECUTE format('SELECT * FROM %s($1, $2, $3)', v_retention.export::REGPROC)
        USING make_interval(days => v_retention.days), coalesce(p_after, ''), p_limit;
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_expired(TEXT, TEXT, INTEGER) IS 'One batch of the expired rows of an audit or record kind: at most p_limit rows with a key greater than p_after, an empty p_after the first batch, in key order, each as (key, data). Raises P0404 for a kind with no retention row and 22023 for a kind deleted by age. Read by the retention job before it writes the batch to cold storage.';

CREATE OR REPLACE FUNCTION claimius.delete_exported(p_kind TEXT, p_object_path TEXT, p_sha256 TEXT, p_keys TEXT[], p_first BOOLEAN)
    RETURNS BIGINT AS $$
DECLARE
    v_retention claimius.retention;
    v_deleted   BIGINT;
BEGIN
    SELECT * INTO v_retention FROM claimius.retention WHERE kind = p_kind;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'delete_exported: no retention for %', p_kind USING ERRCODE = 'P0404';
    END IF;
    IF v_retention.export IS NULL THEN
        RAISE EXCEPTION 'delete_exported: % is class %, deleted by age through delete_expired', p_kind, v_retention.class USING ERRCODE = '22023';
    END IF;
    IF coalesce(cardinality(p_keys), 0) = 0 THEN
        RAISE EXCEPTION 'delete_exported: no keys for %', p_kind USING ERRCODE = '22023';
    END IF;
    INSERT INTO claimius.retention_export (kind, object_path, first_key, last_key, row_count, sha256)
    VALUES (p_kind, p_object_path, p_keys[1], p_keys[cardinality(p_keys)], cardinality(p_keys), p_sha256);
    EXECUTE format('SELECT %s($1)', v_retention.prune::REGPROC) INTO v_deleted USING p_keys;
    UPDATE claimius.retention
    SET pruned = CASE WHEN p_first THEN v_deleted ELSE coalesce(pruned, 0) + v_deleted END, pruned_at = now()
    WHERE kind = p_kind;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.delete_exported(TEXT, TEXT, TEXT, TEXT[], BOOLEAN) IS 'Records one batch written to cold storage in retention_export, then runs the prune of the kind for exactly p_keys, in their order, in the same transaction, and returns the rows removed. p_first starts the count of a run in pruned. Raises P0404 for a kind with no retention row and 22023 for a kind deleted by age or an empty batch. Called by the retention job only after the object is written.';

CREATE OR REPLACE FUNCTION claimius.get_retention()
    RETURNS SETOF claimius.retention AS $$
    SELECT * FROM claimius.retention ORDER BY kind;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_retention() IS 'Every retention row with its class. The caller checks read on the owner organization of the app before it answers.';
