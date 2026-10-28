DROP FUNCTION IF EXISTS claimius.get_expired(TEXT, TEXT, INTEGER);
DROP FUNCTION IF EXISTS claimius.delete_exported(TEXT, UUID, TEXT, TEXT, TEXT[], BOOLEAN);
DROP FUNCTION IF EXISTS claimius.delete_exported(TEXT, TEXT, TEXT, TEXT[], BOOLEAN);
DROP FUNCTION IF EXISTS claimius.get_expired_audit(INTERVAL, TEXT, INTEGER);
DROP FUNCTION IF EXISTS claimius.delete_exported_audit(TEXT[]);
DROP FUNCTION IF EXISTS claimius.upsert_retention(TEXT, claimius.retention_class, INTEGER, TEXT, TEXT);
DROP TABLE IF EXISTS claimius.retention_export;

ALTER TABLE claimius.retention DROP CONSTRAINT IF EXISTS retention_export_class;
ALTER TABLE claimius.retention DROP COLUMN IF EXISTS export;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid JOIN pg_namespace n ON n.oid = t.typnamespace
               WHERE n.nspname = 'claimius' AND t.typname = 'retention_class' AND e.enumlabel = 'record')
       AND NOT EXISTS (SELECT 1 FROM claimius.retention WHERE class::TEXT = 'record') THEN
        ALTER TYPE claimius.retention_class RENAME TO retention_class_record;
        CREATE TYPE claimius.retention_class AS ENUM ('log', 'run', 'token', 'cache', 'queue', 'audit');
        ALTER TABLE claimius.retention ALTER COLUMN class TYPE claimius.retention_class USING class::TEXT::claimius.retention_class;
        DROP TYPE claimius.retention_class_record;
    END IF;
END $$;

COMMENT ON TYPE claimius.retention_class IS 'What a retention kind holds: log, run, token, cache, queue or audit. Every class is deleted by age through delete_expired. Application data has no class and is never age pruned.';
COMMENT ON TABLE claimius.retention IS 'How long each kind of runtime row is kept. One row per kind with its class, its days editable through update_retention; the leader of the retention job calls delete_expired for every row once a day.';
COMMENT ON COLUMN claimius.retention.class IS 'The retention_class of the kind.';
COMMENT ON COLUMN claimius.retention.prune IS 'The function delete_expired calls with make_interval(days => days), taking one INTERVAL and returning the rows removed. A host registers its own kinds with upsert_retention.';

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

CREATE OR REPLACE FUNCTION claimius.upsert_retention(p_kind TEXT, p_class claimius.retention_class, p_days INTEGER, p_prune TEXT)
    RETURNS VOID AS $$
BEGIN
    PERFORM (p_prune || '(INTERVAL)')::REGPROCEDURE;
    INSERT INTO claimius.retention (kind, class, days, prune)
    VALUES (p_kind, p_class, p_days, p_prune)
    ON CONFLICT (kind) DO UPDATE SET class = EXCLUDED.class, prune = EXCLUDED.prune;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_retention(TEXT, claimius.retention_class, INTEGER, TEXT) IS 'Registers a kind with its class, its default days and its prune function, which must exist taking one INTERVAL. A kind already registered keeps the days it was given and takes the class and prune. Run from a migration; the one way a host registers its own runtime tables. Application data is never registered.';

UPDATE claimius.retention SET prune = 'claimius.delete_expired_audit' WHERE kind = 'audit';

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

COMMENT ON FUNCTION claimius.get_retention() IS 'Every retention row with its class. The caller checks read on the owner organization of the app before it answers.';
