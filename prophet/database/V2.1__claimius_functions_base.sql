CREATE OR REPLACE FUNCTION claimius.composite_id(VARIADIC parts TEXT[])
    RETURNS UUID AS $$
SELECT md5(array_to_string(parts, ':'))::UUID;
$$ LANGUAGE sql IMMUTABLE;

COMMENT ON FUNCTION claimius.composite_id(TEXT[]) IS 'Deterministic UUID hash of the joined parts. Same inputs always produce same id.';

GRANT EXECUTE ON FUNCTION claimius.composite_id(TEXT[])
    TO claimius_writer, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.split_object_type(p_object_type TEXT)
    RETURNS TEXT[] AS $$
SELECT CASE
           WHEN position('.' IN p_object_type) = 0 THEN ARRAY['public', p_object_type]
           ELSE ARRAY[split_part(p_object_type, '.', 1), split_part(p_object_type, '.', 2)]
           END;
$$ LANGUAGE sql IMMUTABLE;

COMMENT ON FUNCTION claimius.split_object_type(TEXT) IS 'Splits a schema.table string into [schema, table]. Defaults to public schema.';

GRANT EXECUTE ON FUNCTION claimius.split_object_type(TEXT)
    TO claimius_writer, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.update_timestamp()
    RETURNS TRIGGER AS $$
BEGIN
    IF claimius.in_replay_mode() AND NEW.sa_updated_at IS DISTINCT FROM OLD.sa_updated_at THEN
        RETURN NEW;
    END IF;
    NEW.sa_updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_timestamp() IS 'Trigger function. Sets sa_updated_at = now() on UPDATE. Under replay_mode a row applied from the prophet keeps the sa_updated_at it carries, so the replica matches its source and an unchanged row compares equal.';

GRANT EXECUTE ON FUNCTION claimius.update_timestamp()
    TO claimius_writer, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.update_txid()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.txid = pg_current_xact_id();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.update_txid() IS 'Trigger function. Sets txid = pg_current_xact_id() on INSERT and UPDATE, under replay_mode too, so txid always names the local transaction that last wrote the row.';

GRANT EXECUTE ON FUNCTION claimius.update_txid()
    TO claimius_writer, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.get_table_columns(p_object_type TEXT)
    RETURNS TABLE(column_name TEXT) AS $$
DECLARE
    v_schema TEXT;
    v_table TEXT;
    v_parts TEXT[];
BEGIN
    v_parts := claimius.split_object_type(p_object_type);
    v_schema := v_parts[1];
    v_table := v_parts[2];
    RETURN QUERY
        SELECT c.column_name::TEXT
        FROM information_schema.columns c
        WHERE c.table_schema = v_schema AND c.table_name = v_table;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_table_columns(TEXT) IS 'Returns column names for a schema qualified table.';

GRANT EXECUTE ON FUNCTION claimius.get_table_columns(TEXT)
    TO claimius_writer, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.table_has_column(p_object_type TEXT, p_column TEXT)
    RETURNS BOOLEAN AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM claimius.get_table_columns(p_object_type) WHERE column_name = p_column
    );
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.table_has_column(TEXT, TEXT) IS 'True if the given table has the given column.';

GRANT EXECUTE ON FUNCTION claimius.table_has_column(TEXT, TEXT)
    TO claimius_writer, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.ensure_uuid_null(p UUID) RETURNS UUID
    LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
SELECT NULLIF(p, '00000000-0000-0000-0000-000000000000'::uuid);
$$;

COMMENT ON FUNCTION claimius.ensure_uuid_null(p uuid) IS 'Returns NULL when input is NULL or the zero UUID; otherwise the UUID unchanged.';

GRANT EXECUTE ON FUNCTION claimius.ensure_uuid_null(p uuid)
    TO claimius_writer, claimius_reader;

DROP FUNCTION IF EXISTS claimius.write_audit(UUID, TEXT, TEXT, TEXT, UUID, UUID, UUID, TEXT);

DROP FUNCTION IF EXISTS claimius.write_audit(uuid,text,text,text,uuid,uuid,uuid,text);
CREATE OR REPLACE FUNCTION claimius.insert_audit(
    p_app_id        UUID,
    p_operation     TEXT,
    p_type          TEXT,
    p_object_type   TEXT,
    p_object_id     UUID,
    p_sa_owner_id   UUID,
    p_sa_created_by UUID DEFAULT NULL,
    p_message       TEXT DEFAULT ''
) RETURNS claimius.audit AS $$
DECLARE
    v_row claimius.audit;
BEGIN
    PERFORM claimius.upsert_audit_partition(now());
    INSERT INTO claimius.audit (
        app_id, operation, type, object_type, object_id,
        sa_owner_id, sa_created_by, message
    ) VALUES (
                 p_app_id, p_operation, p_type, p_object_type, p_object_id,
                 p_sa_owner_id, p_sa_created_by, p_message
             )
    RETURNING * INTO v_row;
    RETURN v_row;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.insert_audit(p_app_id uuid, p_operation text, p_type text, p_object_type text, p_object_id uuid, p_sa_owner_id uuid, p_sa_created_by uuid, p_message text) IS 'Inserts a row into the audit log and returns it.';

GRANT EXECUTE ON FUNCTION claimius.insert_audit(p_app_id uuid, p_operation text, p_type text, p_object_type text, p_object_id uuid, p_sa_owner_id uuid, p_sa_created_by uuid, p_message text)
    TO claimius_writer, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.in_replay_mode()
    RETURNS BOOLEAN AS $$
BEGIN
    RETURN coalesce(current_setting('claimius.replay_mode', TRUE), 'false') = 'true';
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.in_replay_mode() IS 'True when claimius.replay_mode session GUC is "true". Triggers short circuit.';

GRANT EXECUTE ON FUNCTION claimius.in_replay_mode()
    TO claimius_writer, claimius_reader;

CREATE OR REPLACE FUNCTION claimius.get_subtree(
    p_tree_type     claimius.tree_type,
    p_root_id       UUID,
    p_start_type    TEXT,
    p_start_id      UUID
) RETURNS TABLE(object_type TEXT, object_id UUID) AS $$
BEGIN
    RETURN QUERY
        SELECT i.descendant_type, i.descendant_id
        FROM claimius.inheritance_info i
        WHERE i.tree_type = p_tree_type
          AND i.root_id = p_root_id
          AND i.ancestor_type = p_start_type
          AND i.ancestor_id = p_start_id;
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_subtree(p_tree_type claimius.tree_type, p_root_id uuid, p_start_type text, p_start_id uuid) IS 'Returns (object_type, object_id) descendants under a start node in a tree, self-edge included.';

GRANT EXECUTE ON FUNCTION claimius.get_subtree(p_tree_type claimius.tree_type, p_root_id uuid, p_start_type text, p_start_id uuid)
    TO claimius_writer, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._popcount(p INTEGER)
    RETURNS INTEGER AS $$
DECLARE
    v INTEGER := coalesce(p, 0);
    n INTEGER := 0;
BEGIN
    WHILE v <> 0 LOOP
        v := v & (v - 1);
        n := n + 1;
    END LOOP;
    RETURN n;
END;
$$ LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE;

COMMENT ON FUNCTION claimius._popcount(INTEGER) IS 'Bit count of an integer access mask. Used for ranking grants.';

GRANT EXECUTE ON FUNCTION claimius._popcount(INTEGER)
    TO claimius_writer, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._expand_access(p INTEGER)
    RETURNS INTEGER LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT p
        | CASE WHEN p & 1 <> 0 THEN 14 ELSE 0 END
        | CASE WHEN p & 2 <> 0 THEN 12 ELSE 0 END
        | CASE WHEN p & 8 <> 0 THEN 4 ELSE 0 END
$$;

COMMENT ON FUNCTION claimius._expand_access(INTEGER) IS 'The mask with the bits it implies added: owner implies write, read and execute, write implies read and execute, execute implies read.';

GRANT EXECUTE ON FUNCTION claimius._expand_access(INTEGER)
    TO claimius_writer, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._effective_access(p_claim INTEGER, p_binding INTEGER)
    RETURNS INTEGER LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT p_claim & claimius._expand_access(coalesce(p_binding, p_claim))
$$;

COMMENT ON FUNCTION claimius._effective_access(INTEGER, INTEGER) IS 'Effective mask of a binding: claim & expand(coalesce(binding, claim)).';

GRANT EXECUTE ON FUNCTION claimius._effective_access(INTEGER, INTEGER)
    TO claimius_writer, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._close_access(p INTEGER)
    RETURNS INTEGER LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT (p & ~15)
        | CASE WHEN p & 4 <> 0 THEN 4 ELSE 0 END
        | CASE WHEN p & 12 = 12 THEN 8 ELSE 0 END
        | CASE WHEN p & 14 = 14 THEN 2 ELSE 0 END
        | CASE WHEN p & 15 = 15 THEN 1 ELSE 0 END
$$;

COMMENT ON FUNCTION claimius._close_access(INTEGER) IS 'The mask closed under implication: a bit whose implied bits are not all present is dropped.';

GRANT EXECUTE ON FUNCTION claimius._close_access(INTEGER)
    TO claimius_writer, claimius_reader;

CREATE OR REPLACE FUNCTION claimius._decayed_usage(p_used_count BIGINT, p_last_used_at TIMESTAMPTZ)
    RETURNS DOUBLE PRECISION LANGUAGE sql STABLE PARALLEL SAFE AS $$
    SELECT CASE
               WHEN coalesce(p_used_count, 0) = 0 OR p_last_used_at IS NULL THEN 0::DOUBLE PRECISION
               ELSE p_used_count * power(2::DOUBLE PRECISION,
                        -greatest(extract(epoch FROM now() - p_last_used_at), 0) / 1209600)
           END
$$;

COMMENT ON FUNCTION claimius._decayed_usage(BIGINT, TIMESTAMPTZ) IS 'used_count * 2^(-age / 14 days), age since last_used_at. 0 for a pair never used.';

GRANT EXECUTE ON FUNCTION claimius._decayed_usage(BIGINT, TIMESTAMPTZ)
    TO claimius_writer, claimius_reader;
