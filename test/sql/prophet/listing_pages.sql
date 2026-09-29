BEGIN;
SELECT plan(13);

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_pages'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.pairs(p_user UUID) RETURNS BIGINT AS $$
    SELECT count(*) FROM claimius.user_object
     WHERE user_id = p_user
       AND object_type = 'claimius.organization'
$$ LANGUAGE sql;

DO $$
DECLARE
    v_app   UUID;
    v_sys   UUID := '00000000-0000-4000-8000-0000000005a0';
    v_root  UUID := md5('pages root')::UUID;
    v_crew  UUID;
    v_user  UUID;
    v_n     INTEGER;
BEGIN
    v_app := (claimius.init(
        p_system_app_slug         => 'sqltest_pages',
        p_system_app_name         => 'SQL pages test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000005a1', 'a0000000-0000-4000-8000-0000000005a2']::UUID[] LOOP
        PERFORM claimius.ensure_app_user(p_user_id => v_user, p_app_id => v_app, p_first_name => 'Reader');
    END LOOP;

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Pages root', 'company', v_root, v_root, 0, v_sys);
    PERFORM claimius.link_app_object(v_app, 'claimius.organization', v_root);
    FOR v_n IN 1..11 LOOP
        INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
        VALUES (md5('pages ' || v_n)::UUID, 'Pages ' || v_n, 'company', v_root, v_root, 1, v_sys);
        PERFORM claimius.link_app_object(v_app, 'claimius.organization', md5('pages ' || v_n)::UUID);
    END LOOP;

    v_crew := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Pages crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000005a1', 'a0000000-0000-4000-8000-0000000005a2']::UUID[] LOOP
        PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_crew, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    END LOOP;
END $$;

SELECT has_index('claimius', 'user_object', 'idx_user_object_user_object', ARRAY['user_id', 'app_id', 'object_type', 'object_id', 'sa_access'],
    'the reader''s range is keyed on user, app, type and object id and carries the mask');

SELECT hasnt_index('claimius', 'user_object', 'idx_user_object_user_type', 'the range keyed on the mask is gone');

SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000005a1') + pg_temp.pairs('a0000000-0000-4000-8000-0000000005a2'), 0::BIGINT,
    'granting the claim settles neither reader in the write');

SELECT is(
    (SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000005a1', pg_temp.app(), 'claimius.organization', 0, NULL, 3)),
    3::BIGINT,
    'a first page of three returns three rows'
);

SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000005a1'), 12::BIGINT,
    'reading one page settles the reader''s whole share before the page is read');

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_objects('a0000000-0000-4000-8000-0000000005a2', pg_temp.app(), 'claimius.organization', 0,
        'ffffffff-ffff-ffff-ffff-ffffffffffff', 1)$$,
    'a page past the last key is empty'
);

SELECT is(pg_temp.pairs('a0000000-0000-4000-8000-0000000005a2'), 12::BIGINT,
    'an empty page settles the share all the same, so the settle does not depend on the page');

CREATE TEMP TABLE paged (page INTEGER, ord BIGINT, object_id UUID);

DO $$
DECLARE
    v_after UUID;
    v_page  INTEGER := 0;
    v_rows  INTEGER;
BEGIN
    LOOP
        v_page := v_page + 1;
        INSERT INTO paged (page, ord, object_id)
        SELECT v_page, g.ordinality, g.object_id
        FROM claimius.get_objects('a0000000-0000-4000-8000-0000000005a1', pg_temp.app(), 'claimius.organization', 0, v_after, 5)
            WITH ORDINALITY AS g;
        GET DIAGNOSTICS v_rows = ROW_COUNT;
        EXIT WHEN v_rows = 0 OR v_page > 20;
        SELECT p.object_id INTO v_after FROM paged p WHERE p.page = v_page ORDER BY p.ord DESC LIMIT 1;
    END LOOP;
END $$;

SELECT results_eq(
    $$SELECT page, count(*) FROM paged GROUP BY page ORDER BY page$$,
    $$VALUES (1, 5::BIGINT), (2, 5::BIGINT), (3, 2::BIGINT)$$,
    'paging by five walks twelve objects in pages of five, five and two, then an empty page'
);

SELECT is((SELECT count(DISTINCT object_id) FROM paged), 12::BIGINT, 'every object comes exactly once across the pages');

SELECT is_empty(
    $$SELECT 1 FROM (
        SELECT row_number() OVER (ORDER BY page, ord) AS walked, row_number() OVER (ORDER BY object_id) AS keyed
        FROM paged) r
      WHERE r.walked <> r.keyed$$,
    'the pages come in key order'
);

SELECT results_eq(
    $$SELECT object_id FROM paged ORDER BY page, ord$$,
    $$SELECT object_id FROM claimius.get_objects('a0000000-0000-4000-8000-0000000005a1', pg_temp.app(), 'claimius.organization')$$,
    'the concatenated pages equal the unpaged listing'
);

SELECT results_eq(
    $$SELECT id FROM claimius.get_organizations('a0000000-0000-4000-8000-0000000005a1', pg_temp.app(), 0, NULL, 5)
      UNION ALL
      SELECT id FROM claimius.get_organizations('a0000000-0000-4000-8000-0000000005a1', pg_temp.app(), 0,
          (SELECT o.id FROM claimius.get_organizations('a0000000-0000-4000-8000-0000000005a1', pg_temp.app(), 0, NULL, 5) o ORDER BY o.id DESC LIMIT 1), 7)$$,
    $$SELECT id FROM claimius.get_organizations('a0000000-0000-4000-8000-0000000005a1', pg_temp.app())$$,
    'get_organizations pages the same range, in id order'
);

SELECT is(
    (SELECT count(*) FROM claimius.get_objects('a0000000-0000-4000-8000-0000000005a1', pg_temp.app(), 'claimius.organization', 8, NULL, 5)),
    0::BIGINT,
    'the mask test applies inside the page'
);

SELECT * FROM finish();
ROLLBACK;
