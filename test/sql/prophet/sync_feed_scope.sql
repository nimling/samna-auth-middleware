BEGIN;
SELECT plan(4);

DO $$
DECLARE
    v_system UUID;
    v_owner  UUID := 'a0000000-0000-4000-8000-0000000000fb';
    v_mine   UUID := '10000000-0000-4000-8000-0000000000e1';
    v_theirs UUID := '10000000-0000-4000-8000-0000000000e2';
    v_app_a  UUID := '50000000-0000-4000-8000-0000000000a1';
    v_app_b  UUID := '50000000-0000-4000-8000-0000000000b1';
BEGIN
    v_system := (claimius.init(
        p_system_app_slug         => 'sqltest_feed',
        p_system_app_name         => 'SQL feed test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    INSERT INTO claimius.samna_app (id, slug, name, private_key, private_seed, sa_owner_id, sa_created_by)
    SELECT v_app_a, 'sqltest_feed_a', 'Feed A', 'key', 'seed', sa_owner_id, sa_created_by FROM claimius.samna_app WHERE id = v_system;
    INSERT INTO claimius.samna_app (id, slug, name, private_key, private_seed, sa_owner_id, sa_created_by)
    SELECT v_app_b, 'sqltest_feed_b', 'Feed B', 'key', 'seed', sa_owner_id, sa_created_by FROM claimius.samna_app WHERE id = v_system;

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by, private_seed)
    VALUES (v_mine, 'Mine', 'company', v_mine, v_mine, 0, v_owner, 'seed-mine'),
           (v_theirs, 'Theirs', 'company', v_theirs, v_theirs, 0, v_owner, 'seed-theirs');
    PERFORM claimius.link_app_object(v_app_a, 'claimius.organization', v_mine);
    PERFORM claimius.link_app_object(v_app_b, 'claimius.organization', v_theirs);

    PERFORM claimius.ensure_app_user(p_user_id => v_owner, p_app_id => v_app_a, p_first_name => 'Own');
    PERFORM claimius.ensure_app_user(p_user_id => v_owner, p_app_id => v_app_b, p_first_name => 'Own');
END $$;

SELECT ok(
    EXISTS (SELECT 1 FROM claimius.get_sync_events('50000000-0000-4000-8000-0000000000a1', 0) e
             WHERE e.event_type = 'claimius.organization' AND (e.payload ->> 'id')::UUID = '10000000-0000-4000-8000-0000000000e1'),
    'an organization linked to the app reaches its delta feed'
);

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_sync_events('50000000-0000-4000-8000-0000000000a1', 0) e
       WHERE e.event_type = 'claimius.organization' AND (e.payload ->> 'id')::UUID = '10000000-0000-4000-8000-0000000000e2'$$,
    'an organization linked only to another app never reaches this app''s delta feed'
);

SELECT ok(
    EXISTS (SELECT 1 FROM claimius.get_sync_events('50000000-0000-4000-8000-0000000000a1', 0) e
             JOIN claimius.sync_event s ON s.seq = e.seq
             WHERE s.app_id = '50000000-0000-4000-8000-0000000000a1'),
    'the delta feed carries the app''s own app scoped events'
);

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_sync_events('50000000-0000-4000-8000-0000000000a1', 0) e
       JOIN claimius.sync_event s ON s.seq = e.seq
       WHERE s.app_id = '50000000-0000-4000-8000-0000000000b1'$$,
    'the delta feed carries no event stamped for another app'
);

SELECT * FROM finish();
ROLLBACK;
