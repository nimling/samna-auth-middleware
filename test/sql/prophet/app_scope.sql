BEGIN;
SELECT plan(15);

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_scope'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.system_org() RETURNS UUID AS $$
    SELECT sa_owner_id FROM claimius.samna_app WHERE slug = 'sqltest_scope'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.actor() RETURNS UUID AS $$
    SELECT sa_created_by FROM claimius.samna_app WHERE slug = 'sqltest_scope'
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.system_claim() RETURNS UUID AS $$
    SELECT claim_id FROM claimius.user_claim WHERE id = pg_temp.actor()
$$ LANGUAGE sql;

DO $$
BEGIN
    PERFORM claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_scope',
        p_system_app_name         => 'SQL scope test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-0000000005c1', 'Unlinked', 'company',
            '10000000-0000-4000-8000-0000000005c1', '10000000-0000-4000-8000-0000000005c1', 0, pg_temp.actor()),
           ('10000000-0000-4000-8000-0000000005c2', 'Unlinked later', 'company',
            '10000000-0000-4000-8000-0000000005c2', '10000000-0000-4000-8000-0000000005c2', 0, pg_temp.actor());
    PERFORM claimius.upsert_app_object(pg_temp.app(), 'claimius.organization', '10000000-0000-4000-8000-0000000005c2', pg_temp.actor());
END $$;

SELECT throws_ok(
    format($$SELECT claimius.insert_claim(p_app_id => %L, p_name => 'Orphan', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => '10000000-0000-4000-8000-0000000005c1', p_sa_root_id => '10000000-0000-4000-8000-0000000005c1',
        p_sa_created_by => %L)$$, pg_temp.app(), pg_temp.actor()),
    'P0403',
    NULL,
    'a claim owned by an organization not linked to its app is refused'
);

SELECT throws_ok(
    format($$SELECT claimius.insert_user_claim(p_app_id => %L, p_claim_id => %L, p_user_id => 'a0000000-0000-4000-8000-0000000005c1',
        p_sa_owner_id => '10000000-0000-4000-8000-0000000005c1', p_sa_created_by => %L)$$,
        pg_temp.app(), pg_temp.system_claim(), pg_temp.actor()),
    'P0403',
    NULL,
    'a user_claim owned by an organization not linked to its app is refused'
);

SELECT throws_ok(
    format($$SELECT claimius.insert_claim_object(p_app_id => %L, p_claim_id => %L,
        p_sa_owner_id => '10000000-0000-4000-8000-0000000005c1', p_sa_root_id => '10000000-0000-4000-8000-0000000005c1',
        p_sa_created_by => %L, p_object_id => %L, p_object_type => 'claimius.organization')$$,
        pg_temp.app(), pg_temp.system_claim(), pg_temp.actor(), pg_temp.system_org()),
    'P0403',
    NULL,
    'a claim_object owned by an organization not linked to its app is refused'
);

SELECT throws_ok(
    format($$SELECT claimius.insert_claim_object(p_app_id => %L, p_claim_id => %L,
        p_sa_owner_id => %L, p_sa_root_id => %L, p_sa_created_by => %L,
        p_object_id => '10000000-0000-4000-8000-0000000005c1', p_object_type => 'claimius.organization')$$,
        pg_temp.app(), pg_temp.system_claim(), pg_temp.system_org(), pg_temp.system_org(), pg_temp.actor()),
    'P0403',
    NULL,
    'a claim_object bound to an organization not linked to its app is refused'
);

SELECT throws_like(
    format($$SELECT claimius.insert_claim_object(p_app_id => %L, p_claim_id => %L,
        p_sa_owner_id => %L, p_sa_root_id => %L, p_sa_created_by => %L,
        p_object_id => '10000000-0000-4000-8000-0000000005c1', p_object_type => 'claimius.organization')$$,
        pg_temp.app(), pg_temp.system_claim(), pg_temp.system_org(), pg_temp.system_org(), pg_temp.actor()),
    format('%%claimius.organization 10000000-0000-4000-8000-0000000005c1 is not linked to app %s', pg_temp.app()),
    'the refusal names the organization and the app'
);

SELECT lives_ok(
    format($$SELECT claimius.insert_claim(p_app_id => %L, p_name => 'Linked', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => '10000000-0000-4000-8000-0000000005c2', p_sa_root_id => '10000000-0000-4000-8000-0000000005c2',
        p_sa_created_by => %L)$$, pg_temp.app(), pg_temp.actor()),
    'a claim owned by an organization linked to its app is written'
);

SELECT lives_ok(
    $q$DO $$
    BEGIN
        PERFORM set_config('claimius.replay_mode', 'true', TRUE);
        INSERT INTO claimius.claim (id, app_id, name, sa_access, sa_owner_id, sa_root_id, sa_created_by)
        VALUES ('20000000-0000-4000-8000-0000000005c9', pg_temp.app(), 'Replayed', 4,
                '10000000-0000-4000-8000-0000000005c1', '10000000-0000-4000-8000-0000000005c1', pg_temp.actor());
        PERFORM set_config('claimius.replay_mode', 'false', TRUE);
    END $$$q$,
    'replay writes a row whatever its organization'
);

SELECT lives_ok(
    $$UPDATE claimius.claim SET sa_deleted_at = now() WHERE id = '20000000-0000-4000-8000-0000000005c9'$$,
    'an out of scope row can still be soft deleted'
);

DO $$
BEGIN
    PERFORM claimius.delete_app_object(pg_temp.app(), 'claimius.organization', '10000000-0000-4000-8000-0000000005c2');
    PERFORM claimius.insert_claim(p_app_id => pg_temp.app(), p_name => 'In scope', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => pg_temp.system_org(), p_sa_root_id => pg_temp.system_org(), p_sa_created_by => pg_temp.actor());
END $$;

SELECT ok(
    EXISTS (SELECT 1 FROM claimius.sync_event
             WHERE app_id = pg_temp.app() AND event_type = 'claimius.claim' AND payload ->> 'name' = 'Linked'),
    'the claim written while its organization was linked is in the app''s sync_event'
);

SELECT is_empty(
    format($$SELECT 1 FROM claimius.get_sync_events(%L, 0, 0) e
              WHERE e.event_type = 'claimius.claim' AND e.payload ->> 'name' = 'Linked'$$, pg_temp.app()),
    'the delta feed leaves out a claim whose organization is no longer linked to the app'
);

SELECT is_empty(
    format($$SELECT 1 FROM claimius.get_peer_snapshot(%L) s
              WHERE s.event_type = 'claimius.claim' AND s.payload ->> 'name' = 'Linked'$$, pg_temp.app()),
    'the peer snapshot leaves out a claim whose organization is not linked to the app'
);

SELECT ok(
    EXISTS (SELECT 1 FROM claimius.get_sync_events(pg_temp.app(), 0, 0) e
             WHERE e.event_type = 'claimius.claim' AND e.payload ->> 'name' = 'In scope'),
    'the delta feed still carries the claims in scope'
);

SELECT throws_ok(
    format($$UPDATE claimius.claim SET sa_owner_id = '10000000-0000-4000-8000-0000000005c1' WHERE id = %L$$, pg_temp.system_claim()),
    'P0403',
    NULL,
    'moving a claim to an organization not linked to its app is refused'
);

DO $$
BEGIN
    CREATE TEMP TABLE before_link AS SELECT coalesce(max(seq), 0) AS seq FROM claimius.sync_event;
    PERFORM claimius.upsert_app_object(pg_temp.app(), 'claimius.organization', '10000000-0000-4000-8000-0000000005c2', pg_temp.actor());
END $$;

SELECT is(
    ARRAY(SELECT e.event_type FROM claimius.get_sync_events(pg_temp.app(), 0, 0) e
           WHERE e.seq > (SELECT seq FROM before_link)
             AND (e.payload ->> 'id' = '10000000-0000-4000-8000-0000000005c2' OR e.payload ->> 'name' = 'Linked')
           ORDER BY e.seq),
    ARRAY['claimius.organization', 'claimius.claim'],
    'linking the organization again publishes it, then the app''s claim it owns'
);

SELECT ok(
    EXISTS (SELECT 1 FROM claimius.get_peer_snapshot(pg_temp.app()) s
             WHERE s.event_type = 'claimius.claim' AND s.payload ->> 'name' = 'Linked'),
    'once linked the snapshot carries the claim'
);

SELECT * FROM finish();
ROLLBACK;
