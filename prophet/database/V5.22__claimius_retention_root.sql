ALTER TABLE claimius.retention_export ADD COLUMN IF NOT EXISTS root_id UUID;

CREATE INDEX IF NOT EXISTS idx_retention_export_root ON claimius.retention_export (root_id, kind, exported_at DESC);

COMMENT ON COLUMN claimius.retention_export.root_id IS 'The root organization whose rows the object holds, from the sa_root_id of each exported row; NULL for rows with no root, written under none.';
COMMENT ON COLUMN claimius.retention_export.object_path IS 'The object in the sink chosen for the root: [export/]<app_slug>/<root_id or none>/<kind>/<yyyy>/<mm>/<first key>-<last key>.ndjson.gz, one JSON row per line, gzip. The export/ prefix marks a root that collects its own rows from the shared sink.';
COMMENT ON COLUMN claimius.retention.export IS 'For an audit or record kind the function get_expired calls with (make_interval(days => days), after, limit), returning TABLE (key TEXT, data JSONB): the expired rows with a key greater than after, at most limit, in key order. The sa_root_id of data names the root organization the row is exported under; a row without one goes to the shared sink under none. NULL for every other class.';

CREATE OR REPLACE FUNCTION claimius.get_expired_audit(p_keep INTERVAL, p_after TEXT, p_limit INTEGER)
    RETURNS TABLE (key TEXT, data JSONB) AS $$
    SELECT lpad((extract(EPOCH FROM a.sa_created_at) * 1000000)::BIGINT::TEXT, 16, '0') || replace(a.id::TEXT, '-', ''),
           to_jsonb(a) || jsonb_build_object('sa_root_id', o.sa_root_id)
    FROM claimius.audit a
        LEFT JOIN claimius.organization o ON o.id = a.sa_owner_id
    WHERE (a.sa_created_at < date_trunc('month', now() - p_keep, 'UTC')
           OR (a.tableoid = 'claimius.audit_default'::REGCLASS AND a.sa_created_at < now() - p_keep))
      AND ((a.sa_created_at, a.id) > (TIMESTAMPTZ 'epoch' + left(NULLIF(p_after, ''), 16)::BIGINT * INTERVAL '1 microsecond',
                                      substr(NULLIF(p_after, ''), 17)::UUID)) IS NOT FALSE
    ORDER BY a.sa_created_at, a.id
    LIMIT p_limit;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_expired_audit(INTERVAL, TEXT, INTEGER) IS 'The audit rows of every UTC month that ended before now less p_keep, and of audit_default older than that, after p_after, at most p_limit, ordered by (sa_created_at, id), each with the sa_root_id of its owner organization, NULL when the owner is no organization. The key is the creation time in epoch microseconds, sixteen digits, then the id without hyphens. The audit export.';

DROP FUNCTION IF EXISTS claimius.delete_exported(TEXT, TEXT, TEXT, TEXT[], BOOLEAN);

CREATE OR REPLACE FUNCTION claimius.delete_exported(p_kind TEXT, p_root_id UUID, p_object_path TEXT, p_sha256 TEXT, p_keys TEXT[], p_first BOOLEAN)
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
    INSERT INTO claimius.retention_export (kind, root_id, object_path, first_key, last_key, row_count, sha256)
    VALUES (p_kind, p_root_id, p_object_path, p_keys[1], p_keys[cardinality(p_keys)], cardinality(p_keys), p_sha256);
    EXECUTE format('SELECT %s($1)', v_retention.prune::REGPROC) INTO v_deleted USING p_keys;
    UPDATE claimius.retention
    SET pruned = CASE WHEN p_first THEN v_deleted ELSE coalesce(pruned, 0) + v_deleted END, pruned_at = now()
    WHERE kind = p_kind;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.delete_exported(TEXT, UUID, TEXT, TEXT, TEXT[], BOOLEAN) IS 'Records one batch of one root organization written to cold storage in retention_export, p_root_id NULL for rows with no root, then runs the prune of the kind for exactly p_keys, in their order, in the same transaction, and returns the rows removed. p_first starts the count of a run in pruned. Raises P0404 for a kind with no retention row and 22023 for a kind deleted by age or an empty batch. Called by the retention job only after the object is written.';
