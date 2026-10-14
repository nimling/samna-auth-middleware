BEGIN;
SELECT plan(27);

CREATE TEMP TABLE crew (key TEXT PRIMARY KEY, id UUID);

CREATE FUNCTION pg_temp.crew(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM crew WHERE key = p_key
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.link(p_object_type TEXT, p_object_id UUID, p_home UUID) RETURNS UUID AS $$
    INSERT INTO claimius.object_asset (app_id, object_type, object_id, asset_id, kind, mime_type, sa_owner_id, sa_created_by)
    VALUES (p_home, p_object_type, p_object_id, gen_random_uuid(), 'file', 'image/png', p_object_id, gen_random_uuid())
    RETURNING id
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.published(p_link UUID, p_operation TEXT DEFAULT 'upsert') RETURNS UUID[] AS $$
    SELECT coalesce(array_agg(e.app_id ORDER BY e.app_id), '{}')
    FROM claimius.sync_event e
    WHERE e.event_type = 'claimius.object_asset'
      AND e.operation::TEXT = p_operation
      AND (e.payload ->> 'id')::UUID = p_link
$$ LANGUAGE sql;

DO $$
DECLARE
    v_system UUID;
    v_owner  UUID := 'a0000000-0000-4000-8000-0000000a55e0';
    v_app_a  UUID := '50000000-0000-4000-8000-0000000a55a1';
    v_app_b  UUID := '50000000-0000-4000-8000-0000000a55b1';
    v_org_a  UUID := '10000000-0000-4000-8000-0000000a55a1';
    v_org_b  UUID := '10000000-0000-4000-8000-0000000a55b1';
BEGIN
    v_system := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_asset',
        p_system_app_name         => 'SQL asset test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    INSERT INTO claimius.samna_app (id, slug, name, private_key, private_seed, sa_owner_id, sa_created_by)
    SELECT v_app_a, 'sqltest_asset_a', 'Asset A', 'key', 'seed', sa_owner_id, sa_created_by FROM claimius.samna_app WHERE id = v_system;
    INSERT INTO claimius.samna_app (id, slug, name, private_key, private_seed, sa_owner_id, sa_created_by)
    SELECT v_app_b, 'sqltest_asset_b', 'Asset B', 'key', 'seed', sa_owner_id, sa_created_by FROM claimius.samna_app WHERE id = v_system;

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_org_a, 'Asset org A', 'company', v_org_a, v_org_a, 0, v_owner),
           (v_org_b, 'Asset org B', 'company', v_org_b, v_org_b, 0, v_owner);
    PERFORM claimius.upsert_app_object(v_app_a, 'claimius.organization', v_org_a);

    PERFORM claimius.upsert_app_user(p_user_id => v_owner, p_app_id => v_app_a, p_first_name => 'Both');
    PERFORM claimius.upsert_app_user(p_user_id => v_owner, p_app_id => v_app_b, p_first_name => 'Both');
    PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-0000000a55e1', p_app_id => v_app_a, p_first_name => 'Late');

    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
    INSERT INTO claimius.claim (id, app_id, name, sa_access, sa_owner_id, sa_root_id, sa_created_by)
    VALUES ('20000000-0000-4000-8000-0000000a55a1', v_app_a, 'Claim of A', 4, v_org_a, v_org_a, v_owner);
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);

    INSERT INTO crew VALUES
        ('system', v_system), ('app_a', v_app_a), ('app_b', v_app_b), ('org_a', v_org_a), ('org_b', v_org_b),
        ('system_user', (SELECT uc.user_id FROM claimius.user_claim uc JOIN claimius.samna_app a ON a.sa_created_by = uc.id WHERE a.id = v_system)),
        ('system_org', (SELECT sa_owner_id FROM claimius.samna_app WHERE id = v_system));
END $$;

SELECT has_table('claimius', 'object_asset', 'object_asset exists on the prophet');

SELECT col_not_null('claimius', 'object_asset', 'app_id', 'a prophet link names its home app');

SELECT col_not_null('claimius', 'object_asset', 'sa_created_by', 'a link always names the grant it was written under');

SELECT ok(EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'claimius' AND indexname = 'idx_object_asset_app_asset'
                   AND indexdef LIKE '%UNIQUE%(app_id, asset_id)%sa_deleted_at IS NULL%'),
    'an asset of one home app has one live link');

SELECT throws_ok(
    $$INSERT INTO claimius.object_asset (app_id, object_type, object_id, asset_id, kind, mime_type, sa_owner_id, sa_created_by)
      VALUES (gen_random_uuid(), 'public.booking', gen_random_uuid(), gen_random_uuid(), 'file', 'text/plain', gen_random_uuid(), gen_random_uuid())$$,
    '23514', NULL, 'a link to an object outside claimius is refused');

SELECT ok(has_table_privilege('claimius_writer', 'claimius.object_asset', 'INSERT')
      AND has_table_privilege('claimius_writer', 'claimius.object_asset', 'UPDATE')
      AND has_table_privilege('claimius_writer', 'claimius.object_asset', 'SELECT'),
    'claimius_writer writes links directly, like user_relation');

SELECT ok(has_function_privilege('claimius_reader', 'claimius.get_object_assets(uuid, uuid, text, uuid, text)', 'EXECUTE'),
    'claimius_reader executes get_object_assets');

SELECT ok(EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = 'claimius.object_asset'::REGCLASS AND tgname = 'tg_enforce_app_home')
      AND EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = 'claimius.object_asset'::REGCLASS AND tgname = 'tg_update_txid')
      AND EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = 'claimius.object_asset'::REGCLASS AND tgname = 'tg_update_timestamp')
      AND NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = 'claimius.object_asset'::REGCLASS AND tgname = 'tg_calc_access'),
    'a link carries the timestamp, txid and home triggers and no access calc');

INSERT INTO crew VALUES
    ('on_org', pg_temp.link('claimius.organization', pg_temp.crew('org_a'), pg_temp.crew('app_b'))),
    ('on_user', pg_temp.link('claimius.samna_user', 'a0000000-0000-4000-8000-0000000a55e0', pg_temp.crew('app_b'))),
    ('on_app', pg_temp.link('claimius.samna_app', pg_temp.crew('app_a'), pg_temp.crew('app_b'))),
    ('on_claim', pg_temp.link('claimius.claim', '20000000-0000-4000-8000-0000000a55a1', pg_temp.crew('app_b'))),
    ('on_other', pg_temp.link('claimius.samna_secret', gen_random_uuid(), pg_temp.crew('app_b'))),
    ('on_late_org', pg_temp.link('claimius.organization', pg_temp.crew('org_b'), pg_temp.crew('app_a'))),
    ('on_late_user', pg_temp.link('claimius.samna_user', 'a0000000-0000-4000-8000-0000000a55e1', pg_temp.crew('app_a')));

SELECT is(pg_temp.published(pg_temp.crew('on_org')), ARRAY[pg_temp.crew('app_a')],
    'a link on an organization goes to the apps linked to it, not to its home');

SELECT is(pg_temp.published(pg_temp.crew('on_user')), ARRAY[pg_temp.crew('app_a'), pg_temp.crew('app_b')],
    'a link on a user goes to every app holding a live row of that user');

SELECT is(pg_temp.published(pg_temp.crew('on_app')), ARRAY[pg_temp.crew('app_a')],
    'a link on an app goes to that app');

SELECT is(pg_temp.published(pg_temp.crew('on_claim')), ARRAY[pg_temp.crew('app_a')],
    'a link on a claim goes to the claim''s app');

SELECT is(pg_temp.published(pg_temp.crew('on_other')), ARRAY[pg_temp.crew('app_b')],
    'a link on any other claimius type goes to its home app');

SELECT is(pg_temp.published(pg_temp.crew('on_late_org')), '{}'::UUID[],
    'a link on an organization no app is linked to goes nowhere');

DO $$ BEGIN PERFORM claimius.upsert_app_object(pg_temp.crew('app_b'), 'claimius.organization', pg_temp.crew('org_b')); END $$;

SELECT is(pg_temp.published(pg_temp.crew('on_late_org')), ARRAY[pg_temp.crew('app_b')],
    'linking the organization to an app later publishes its live links to that app');

DO $$ BEGIN PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-0000000a55e1', p_app_id => pg_temp.crew('app_b'), p_first_name => 'Late'); END $$;

SELECT is(pg_temp.published(pg_temp.crew('on_late_user')), ARRAY[pg_temp.crew('app_a'), pg_temp.crew('app_b')],
    'a samna_user row inserted for an app publishes the user''s live links to that app');

DO $$ BEGIN PERFORM claimius.delete_app_object(pg_temp.crew('app_b'), 'claimius.organization', pg_temp.crew('org_b')); END $$;

SELECT is(pg_temp.published(pg_temp.crew('on_late_org'), 'delete'), ARRAY[pg_temp.crew('app_b')],
    'unlinking the organization deletes its links from that app');

CREATE TEMP TABLE before_delete AS SELECT coalesce(max(seq), 0) AS seq FROM claimius.sync_event;
UPDATE claimius.object_asset SET sa_deleted_at = now() WHERE id = pg_temp.crew('on_user');

SELECT is(
    ARRAY(SELECT e.app_id FROM claimius.sync_event e
          WHERE e.seq > (SELECT seq FROM before_delete) AND (e.payload ->> 'id')::UUID = pg_temp.crew('on_user')
            AND e.payload ->> 'sa_deleted_at' IS NOT NULL ORDER BY e.app_id),
    ARRAY[pg_temp.crew('app_a'), pg_temp.crew('app_b')],
    'a soft delete follows the same fan-out');

DELETE FROM claimius.object_asset WHERE id = pg_temp.crew('on_claim');

SELECT is(pg_temp.published(pg_temp.crew('on_claim'), 'delete'), ARRAY[pg_temp.crew('app_a')],
    'a hard delete follows the same fan-out');

UPDATE claimius.object_asset SET object_type = 'claimius.organization', object_id = pg_temp.crew('org_a')
WHERE id = pg_temp.crew('on_other');

SELECT is(pg_temp.published(pg_temp.crew('on_other'), 'delete'), ARRAY[pg_temp.crew('app_b')],
    'a link moved to another object is deleted from the apps that no longer see it');

SELECT ok(
    EXISTS (SELECT 1 FROM claimius.get_peer_snapshot(pg_temp.crew('app_a')) s
            WHERE s.event_type = 'claimius.object_asset' AND (s.payload ->> 'id')::UUID = pg_temp.crew('on_org'))
    AND NOT EXISTS (SELECT 1 FROM claimius.get_peer_snapshot(pg_temp.crew('app_b')) s
            WHERE s.event_type = 'claimius.object_asset' AND (s.payload ->> 'id')::UUID = pg_temp.crew('on_org')),
    'the snapshot of an app carries the links published to it and no other');

SELECT is(
    claimius.count_sync_source_rows(pg_temp.crew('app_a'))
        - (SELECT count(*) FROM claimius.get_peer_snapshot(pg_temp.crew('app_a')) s
           WHERE s.event_type NOT IN ('claimius.samna_app', 'claimius.app_handover', 'claimius.organization', 'claimius.location')),
    (SELECT count(*) FROM claimius.app_object ao
     WHERE ao.app_id = pg_temp.crew('app_a') AND ao.sa_deleted_at IS NULL
       AND ao.object_type IN ('claimius.organization', 'claimius.location')),
    'the snapshot source count includes the app''s links');

INSERT INTO crew VALUES
    ('system_link', pg_temp.link('claimius.organization', pg_temp.crew('system_org'), pg_temp.crew('system'))),
    ('self_link', pg_temp.link('claimius.samna_user', pg_temp.crew('system_user'), pg_temp.crew('system')));

DO $$ BEGIN PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-0000000a55e9', p_app_id => pg_temp.crew('system'), p_first_name => 'Outsider'); END $$;

SELECT is(
    ARRAY(SELECT id FROM claimius.get_object_assets(pg_temp.crew('system_user'), pg_temp.crew('system'), 'claimius.organization', pg_temp.crew('system_org'))),
    ARRAY[pg_temp.crew('system_link')],
    'a reader with READ on the organization reads its links');

SELECT is_empty(
    format($$SELECT 1 FROM claimius.get_object_assets('a0000000-0000-4000-8000-0000000a55e9', %L, 'claimius.organization', %L)$$,
           pg_temp.crew('system'), pg_temp.crew('system_org')),
    'a reader without READ on the organization reads none of its links');

SELECT is(
    ARRAY(SELECT id FROM claimius.get_object_assets(pg_temp.crew('system_user'), pg_temp.crew('system'), 'claimius.samna_user', pg_temp.crew('system_user'))),
    ARRAY[pg_temp.crew('self_link')],
    'a user reads the links on itself');

SELECT is_empty(
    format($$SELECT 1 FROM claimius.get_object_assets('a0000000-0000-4000-8000-0000000a55e9', %L, 'claimius.samna_user', %L)$$,
           pg_temp.crew('system'), pg_temp.crew('system_user')),
    'a user who does not see another user reads none of its links');

SELECT is_empty(
    format($$SELECT 1 FROM claimius.get_object_assets(%L, %L, 'claimius.organization', %L, 'profile')$$,
           pg_temp.crew('system_user'), pg_temp.crew('system'), pg_temp.crew('system_org')),
    'a kind filter returns only links of that kind');

SELECT * FROM finish();
ROLLBACK;
