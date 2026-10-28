DROP FUNCTION IF EXISTS claimius.upsert_retention(TEXT, claimius.retention_class, INTEGER, TEXT);

ALTER TABLE claimius.retention DROP COLUMN IF EXISTS class;

DROP TYPE IF EXISTS claimius.retention_class;

CREATE OR REPLACE FUNCTION claimius.upsert_retention(p_kind TEXT, p_days INTEGER, p_prune TEXT)
    RETURNS VOID AS $$
BEGIN
    PERFORM (p_prune || '(INTERVAL)')::REGPROCEDURE;
    INSERT INTO claimius.retention (kind, days, prune)
    VALUES (p_kind, p_days, p_prune)
    ON CONFLICT (kind) DO UPDATE SET prune = EXCLUDED.prune;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_retention(TEXT, INTEGER, TEXT) IS 'Registers one of the claimius runtime kinds with its default days and its prune function, which must exist taking one INTERVAL and returning the rows removed. A kind already registered keeps its days. The kinds are the middleware''s own: a host prunes its own tables with the same shape in its own schema and registers nothing here.';

COMMENT ON TABLE claimius.retention IS 'How long each kind of claimius runtime row is kept. One row per kind with its days, editable through update_retention by the owner of the instance, its prune function, and what the last run removed and when. Application data has no kind and is never aged.';

COMMENT ON FUNCTION claimius.get_retention() IS 'Every retention row. The caller checks read on the owner organization of the app before it answers.';
