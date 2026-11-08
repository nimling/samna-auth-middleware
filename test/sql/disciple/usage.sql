BEGIN;
SELECT plan(23);

CREATE FUNCTION pg_temp.pair(p_user UUID, p_object UUID) RETURNS TABLE (used_count BIGINT, last_used_at TIMESTAMPTZ, sa_access INTEGER) AS $$
    SELECT uo.used_count, uo.last_used_at, uo.sa_access FROM claimius.user_object uo
    WHERE uo.user_id = p_user AND uo.object_type = 'claimius.organization' AND uo.object_id = p_object
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.window(p_width INTEGER, p_sort TEXT, p_next UUID) RETURNS TABLE (label TEXT) AS $$
    SELECT g.label FROM claimius.get_owner_graph(
        'a0000000-0000-4000-8000-0000000000e1', '10000000-0000-4000-8000-0000000000e0', 0, 4, p_width, p_sort, p_next) g
    WHERE g.object_type = 'claimius.organization'
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys   UUID := '00000000-0000-4000-8000-0000000000e0';
    v_root  UUID := '10000000-0000-4000-8000-0000000000e0';
    v_crew  UUID;
    v_user  UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();

    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000e1', 'a0000000-0000-4000-8000-0000000000e2']::UUID[] LOOP
        PERFORM claimius.upsert_app_user(p_user_id => v_user, p_first_name => 'Usage');
    END LOOP;

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Usage root', 'company', v_root, v_root, 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-0000000000e9', 'Usage elsewhere', 'company', '10000000-0000-4000-8000-0000000000e9', '10000000-0000-4000-8000-0000000000e9', 0, v_sys);
    INSERT INTO claimius.organization (id, name, description, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-0000000000e1', 'Bravo harbour', 'Harbour', 'company', v_root, v_root, 1, v_sys),
           ('10000000-0000-4000-8000-0000000000e2', 'Alpha harbour', 'Harbour', 'company', v_root, v_root, 1, v_sys),
           ('10000000-0000-4000-8000-0000000000e3', 'Charlie harbour', 'Harbour', 'company', v_root, v_root, 1, v_sys);

    v_crew := (claimius.insert_claim(
        p_name => 'Usage crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_claim_id => v_crew, p_user_id => 'a0000000-0000-4000-8000-0000000000e1', p_sa_owner_id => v_root, p_sa_created_by => v_sys);
END $$;

SELECT is((SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000e1', 'claimius.organization')), 4::BIGINT,
    'the reader lists the root and its three children, settling four pairs');

SELECT is(
    claimius.touch_object_usage(
        ARRAY['a0000000-0000-4000-8000-0000000000e1', 'a0000000-0000-4000-8000-0000000000e1',
              'a0000000-0000-4000-8000-0000000000e1', 'a0000000-0000-4000-8000-0000000000e2']::UUID[],
        ARRAY['claimius.organization', 'claimius.organization', 'claimius.organization', 'claimius.organization'],
        ARRAY['10000000-0000-4000-8000-0000000000e3', '10000000-0000-4000-8000-0000000000e3',
              '10000000-0000-4000-8000-0000000000e1', '10000000-0000-4000-8000-0000000000e3']::UUID[],
        ARRAY[3, 4, 1, 9]::BIGINT[]),
    2,
    'a flush touches the two settled pairs and skips the pair of a user who holds none'
);

SELECT is((SELECT used_count FROM pg_temp.pair('a0000000-0000-4000-8000-0000000000e1', '10000000-0000-4000-8000-0000000000e3')), 7::BIGINT,
    'repeats of a pair in one flush add up');
SELECT is((SELECT last_used_at FROM pg_temp.pair('a0000000-0000-4000-8000-0000000000e1', '10000000-0000-4000-8000-0000000000e3')), now(),
    'the flush stamps last_used_at');
SELECT is_empty($$SELECT 1 FROM claimius.user_object WHERE user_id = 'a0000000-0000-4000-8000-0000000000e2'$$,
    'a pair that is not settled is not made by usage');
SELECT set_eq($$SELECT user_id FROM claimius.user_activity$$,
    $$VALUES ('a0000000-0000-4000-8000-0000000000e1'::UUID), ('a0000000-0000-4000-8000-0000000000e2'::UUID)$$,
    'every counted user moves its user_activity row');

SELECT claimius.get_claim_graph('a0000000-0000-4000-8000-0000000000e1');
SELECT claimius.touch_object_usage(ARRAY['a0000000-0000-4000-8000-0000000000e1']::UUID[], ARRAY['claimius.organization'],
    ARRAY['10000000-0000-4000-8000-0000000000e2']::UUID[], ARRAY[1]::BIGINT[]);
SELECT ok(EXISTS (SELECT 1 FROM claimius.claim_graph_cache WHERE user_id = 'a0000000-0000-4000-8000-0000000000e1'),
    'a usage flush leaves the reader''s cached claim graph in place');

UPDATE claimius.claim_object SET sa_access = 6 WHERE claim_id = (SELECT id FROM claimius.claim WHERE name = 'Usage crew');
UPDATE claimius.claim SET sa_access = 6 WHERE name = 'Usage crew';
SELECT is((SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000e1', 'claimius.organization', 6)), 4::BIGINT,
    'a wider grant settles the reader''s pairs again');
SELECT results_eq(
    $$SELECT used_count, sa_access FROM pg_temp.pair('a0000000-0000-4000-8000-0000000000e1', '10000000-0000-4000-8000-0000000000e3')$$,
    $$VALUES (7::BIGINT, 6)$$,
    'the settle rewrote the mask and kept the usage');

SELECT set_eq($$SELECT * FROM pg_temp.window(2, 'name', NULL)$$,
    $$VALUES ('Usage root'::TEXT), ('Alpha harbour'::TEXT), ('Bravo harbour'::TEXT)$$,
    'a name window of two shows the first two children of the root in name order');
SELECT set_eq($$SELECT * FROM pg_temp.window(2, 'usage', NULL)$$,
    $$VALUES ('Usage root'::TEXT), ('Charlie harbour'::TEXT), ('Alpha harbour'::TEXT)$$,
    'a usage window of two shows the heaviest child, then the lighter ones in name order');
SELECT set_eq($$SELECT * FROM pg_temp.window(2, 'name', '10000000-0000-4000-8000-0000000000e1')$$,
    $$VALUES ('Charlie harbour'::TEXT)$$,
    'the continuation after Bravo shows the next sibling');
SELECT is((SELECT count(*) FROM pg_temp.window(0, 'name', NULL)), 4::BIGINT,
    'a width of 0 shows every node');
SELECT throws_ok($$SELECT * FROM pg_temp.window(2, 'size', NULL)$$, '22023', NULL,
    'a sort other than name or usage is refused');

SELECT is(
    (SELECT count(*) FROM claimius.get_claim_graph('a0000000-0000-4000-8000-0000000000e1', NULL, 0, 0, 2, 'name') g
      WHERE g.object_type = 'claimius.organization'),
    2::BIGINT,
    'the claim graph window shows two of the four objects under the binding'
);

SELECT results_eq(
    $$SELECT name FROM claimius.search('a0000000-0000-4000-8000-0000000000e1'::UUID, 'harbour'::TEXT, ARRAY['claimius.organization'], 0, 20, 0)$$,
    $$VALUES ('Charlie harbour'::TEXT), ('Bravo harbour'::TEXT), ('Alpha harbour'::TEXT)$$,
    'search ranks equal matches by the reader''s decayed usage'
);

ALTER TABLE claimius.user_claim DISABLE TRIGGER USER;
ALTER TABLE claimius.claim DISABLE TRIGGER USER;
ALTER TABLE claimius.claim_object DISABLE TRIGGER USER;
UPDATE claimius.user_claim SET txid = '3';
UPDATE claimius.claim SET txid = '3';
UPDATE claimius.claim_object SET txid = '3';
ALTER TABLE claimius.user_claim ENABLE TRIGGER USER;
ALTER TABLE claimius.claim ENABLE TRIGGER USER;
ALTER TABLE claimius.claim_object ENABLE TRIGGER USER;

DELETE FROM claimius.claim_graph_cache;
SELECT claimius.get_claim_graph('a0000000-0000-4000-8000-0000000000e1');
UPDATE claimius.claim_graph_cache
SET graph = graph || '[{"object_id": "90000000-0000-4000-8000-0000000000e9", "object_type": "marker", "label": "cached", "parent_id": null, "data": {}}]'::JSONB;

SELECT claimius.insert_claim(
    p_name => 'Usage other', p_description => NULL, p_sa_access => 4,
    p_sa_owner_id => '10000000-0000-4000-8000-0000000000e9', p_sa_root_id => '10000000-0000-4000-8000-0000000000e9',
    p_sa_created_by => '00000000-0000-4000-8000-0000000000e0');
SELECT ok(EXISTS (SELECT 1 FROM claimius.get_claim_graph('a0000000-0000-4000-8000-0000000000e1') g WHERE g.object_type = 'marker'),
    'a claim the reader does not hold leaves the cached claim graph valid');

UPDATE claimius.claim_object SET inherits = TRUE, sa_access = 4 WHERE claim_id = (SELECT id FROM claimius.claim WHERE name = 'Usage crew');
SELECT ok(NOT EXISTS (SELECT 1 FROM claimius.get_claim_graph('a0000000-0000-4000-8000-0000000000e1') g WHERE g.object_type = 'marker'),
    'a binding of a claim the reader holds changed since the cache was built, so the graph is built again');

SELECT is(claimius._decayed_usage(8, now() - INTERVAL '14 days'), 4::DOUBLE PRECISION, 'usage halves every 14 days');
SELECT is(claimius._decayed_usage(0, now()), 0::DOUBLE PRECISION, 'a pair never used weighs nothing');

UPDATE claimius.user_claim SET sa_deleted_at = now() WHERE user_id = 'a0000000-0000-4000-8000-0000000000e1';
SELECT is((SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000e1', 'claimius.organization')), 0::BIGINT,
    'a revoked grant empties the reader''s listing');
SELECT is_empty($$SELECT 1 FROM claimius.user_object WHERE user_id = 'a0000000-0000-4000-8000-0000000000e1' AND used_count > 0$$,
    'the usage leaves with the pairs');

SELECT is(to_regclass('claimius.object_usage'), NULL,
    'object_usage is retired, its usage kept on the pairs');

SELECT * FROM finish();
ROLLBACK;
