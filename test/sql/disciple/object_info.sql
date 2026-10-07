BEGIN;
SELECT plan(18);

CREATE FUNCTION pg_temp.writes(p_table TEXT) RETURNS BIGINT AS $$
    SELECT coalesce((SELECT n_tup_ins + n_tup_upd + n_tup_del FROM pg_stat_xact_user_tables
                     WHERE schemaname = 'claimius' AND relname = p_table), 0)
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.listing(p_user UUID) RETURNS TABLE (object_id UUID, sa_access INTEGER, sa_name TEXT, sa_description TEXT, sa_owner_id UUID) AS $$
    SELECT g.object_id, g.sa_access, g.sa_name, g.sa_description, g.sa_owner_id
    FROM claimius.get_objects(p_user, 'claimius.organization') g
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.sourced(p_user UUID) RETURNS TABLE (object_id UUID, sa_access INTEGER, sa_name TEXT, sa_description TEXT, sa_owner_id UUID) AS $$
    SELECT uo.object_id, uo.sa_access, o.name::TEXT, o.description::TEXT, o.sa_owner_id
    FROM claimius.user_object uo
        JOIN claimius.organization o ON o.id = uo.object_id AND o.sa_deleted_at IS NULL
    WHERE uo.user_id = p_user AND uo.object_type = 'claimius.organization'
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys   UUID := '00000000-0000-4000-8000-0000000000b0';
    v_root  UUID := '10000000-0000-4000-8000-0000000000b0';
    v_crew  UUID;
    v_user  UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();

    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000b1', 'a0000000-0000-4000-8000-0000000000b2']::UUID[] LOOP
        PERFORM claimius.upsert_app_user(p_user_id => v_user, p_first_name => 'Reader');
    END LOOP;

    INSERT INTO claimius.organization (id, name, description, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Info root', 'The info root', 'company', v_root, v_root, 0, v_sys);
    INSERT INTO claimius.organization (id, name, description, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-0000000000b1', 'Info harbour', 'Docks and cranes', 'company', v_root, v_root, 1, v_sys),
           ('10000000-0000-4000-8000-0000000000b2', 'Info quarry', 'Stone and gravel', 'company', v_root, v_root, 1, v_sys),
           ('10000000-0000-4000-8000-0000000000b3', 'Info meadow', 'Grass and sheep', 'company', v_root, v_root, 1, v_sys);

    v_crew := (claimius.insert_claim(
        p_name => 'Info crew', p_description => 'Reads the info tree', p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000b1', 'a0000000-0000-4000-8000-0000000000b2']::UUID[] LOOP
        PERFORM claimius.insert_user_claim(p_claim_id => v_crew, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    END LOOP;
END $$;

SELECT set_eq(
    $$SELECT * FROM pg_temp.listing('a0000000-0000-4000-8000-0000000000b1')$$,
    $$SELECT * FROM pg_temp.sourced('a0000000-0000-4000-8000-0000000000b1')$$,
    'a listing returns every pair of the reader with the name, description and owner of the source row'
);
SELECT set_eq(
    $$SELECT * FROM pg_temp.listing('a0000000-0000-4000-8000-0000000000b2')$$,
    $$SELECT * FROM pg_temp.sourced('a0000000-0000-4000-8000-0000000000b2')$$,
    'a second reader lists the same four organizations from the same source fields'
);

SELECT is(
    (SELECT count(*) FROM claimius.user_object
      WHERE object_type = 'claimius.organization'
        AND object_id = '10000000-0000-4000-8000-0000000000b1'),
    2::BIGINT,
    'two readers hold a pair each on the harbour'
);
SELECT results_eq(
    $$SELECT sa_name, sa_description, sa_owner_id FROM claimius.object_info
       WHERE object_type = 'claimius.organization' AND object_id = '10000000-0000-4000-8000-0000000000b1'$$,
    $$VALUES ('Info harbour'::TEXT, 'Docks and cranes'::TEXT, '10000000-0000-4000-8000-0000000000b0'::UUID)$$,
    'the harbour''s fields are stored once, in one object_info row'
);
SELECT columns_are('claimius', 'user_object',
    ARRAY['user_id', 'object_id', 'object_type', 'sa_access', 'grants', 'scope', 'direct_grant', 'used_count', 'last_used_at', 'settled_txid', 'sa_created_at', 'sa_updated_at'],
    'a pair carries only the per user columns');
SELECT col_is_pk('claimius', 'user_object', ARRAY['user_id', 'object_type', 'object_id'],
    'the pair is keyed on its natural key');

SELECT set_eq(
    $$SELECT s.object_id FROM claimius.search('a0000000-0000-4000-8000-0000000000b1'::UUID, 'quarry'::TEXT, ARRAY['claimius.organization'], 0, 20, 0) s$$,
    $$VALUES ('10000000-0000-4000-8000-0000000000b2'::UUID)$$,
    'search finds the organization whose name matches, through object_info'
);
SELECT set_eq(
    $$SELECT s.object_id FROM claimius.search('a0000000-0000-4000-8000-0000000000b1'::UUID, 'gravel'::TEXT, ARRAY['claimius.organization'], 0, 20, 0) s$$,
    $$VALUES ('10000000-0000-4000-8000-0000000000b2'::UUID)$$,
    'search finds the organization whose description matches'
);

CREATE TEMP TABLE before_rename AS
SELECT pg_temp.writes('user_object') AS pairs, pg_temp.writes('object_info') AS infos,
       (SELECT count(*) FROM claimius.access_diff) AS areas;

UPDATE claimius.organization SET name = 'Info port', description = 'Ships and cargo'
WHERE id = '10000000-0000-4000-8000-0000000000b1';

SELECT is(pg_temp.writes('object_info') - (SELECT infos FROM before_rename), 1::BIGINT,
    'a rename writes the one object_info row');
SELECT is((SELECT count(*) FROM claimius.access_diff), (SELECT areas FROM before_rename),
    'a rename raises no area');

SELECT set_eq(
    $$SELECT sa_name FROM pg_temp.listing('a0000000-0000-4000-8000-0000000000b1') WHERE object_id = '10000000-0000-4000-8000-0000000000b1'
      UNION ALL
      SELECT sa_name FROM pg_temp.listing('a0000000-0000-4000-8000-0000000000b2') WHERE object_id = '10000000-0000-4000-8000-0000000000b1'$$,
    $$VALUES ('Info port'::TEXT), ('Info port'::TEXT)$$,
    'every reader''s listing shows the new name'
);
SELECT is(pg_temp.writes('user_object') - (SELECT pairs FROM before_rename), 0::BIGINT,
    'neither the rename nor the listings after it wrote a pair: nothing settled');
SELECT set_eq(
    $$SELECT s.object_id FROM claimius.search('a0000000-0000-4000-8000-0000000000b2'::UUID, 'cargo'::TEXT, ARRAY['claimius.organization'], 0, 20, 0) s$$,
    $$VALUES ('10000000-0000-4000-8000-0000000000b1'::UUID)$$,
    'search finds the renamed organization by its new description for another reader'
);
SELECT is(
    (SELECT count(*) FROM claimius.search('a0000000-0000-4000-8000-0000000000b2'::UUID, 'cranes'::TEXT, ARRAY['claimius.organization'], 0, 20, 0)),
    0::BIGINT,
    'the old description no longer matches'
);

UPDATE claimius.organization SET sa_deleted_at = now() WHERE id = '10000000-0000-4000-8000-0000000000b3';
SELECT ok(NOT EXISTS (SELECT 1 FROM claimius.object_info WHERE object_id = '10000000-0000-4000-8000-0000000000b3'),
    'a soft deleted organization leaves object_info');
SELECT set_eq(
    $$SELECT * FROM pg_temp.listing('a0000000-0000-4000-8000-0000000000b2')$$,
    $$SELECT * FROM pg_temp.sourced('a0000000-0000-4000-8000-0000000000b2')$$,
    'after the delete settles the listing still returns every pair with its source fields'
);

SELECT is(
    (SELECT array_agg(c.relname::TEXT ORDER BY c.relname) FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
      WHERE i.indrelid = 'claimius.user_object'::regclass),
    ARRAY['idx_user_object_object_user', 'user_object_pkey'],
    'a settled pair writes two index entries: the primary key and the reverse lookup'
);
SELECT ok(
    (SELECT count(*) FROM pg_indexes WHERE schemaname = 'claimius' AND indexname IN (
        'idx_object_info_owner_name', 'idx_object_info_location_name', 'idx_object_info_search', 'idx_sync_event_written_brin')) = 4
    AND NOT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'claimius' AND indexname IN (
        'idx_sync_event_type_seq', 'idx_claim_sa_deleted_at', 'idx_user_claim_sa_deleted_at', 'idx_claim_object_sa_deleted_at')),
    'object_info carries its name ordered owner and location windows and its search index, sync_event its BRIN, and the unread indexes are gone'
);

SELECT * FROM finish();
ROLLBACK;
