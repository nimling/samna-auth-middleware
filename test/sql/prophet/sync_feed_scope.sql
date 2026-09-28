BEGIN;

DO $$
DECLARE
    v_init     JSONB;
    v_system   UUID;
    v_owner    UUID := 'a0000000-0000-4000-8000-0000000000fb';
    v_mine     UUID := '10000000-0000-4000-8000-0000000000e1';
    v_theirs   UUID := '10000000-0000-4000-8000-0000000000e2';
    v_app_a    UUID := '50000000-0000-4000-8000-0000000000a1';
    v_app_b    UUID := '50000000-0000-4000-8000-0000000000b1';
    v_seq      BIGINT;
    v_count    INTEGER;
BEGIN
    v_init := claimius.init(
        p_system_app_slug         => 'sqltest_feed',
        p_system_app_name         => 'SQL feed test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_system := (v_init -> 'samna_app' ->> 'id')::UUID;

    INSERT INTO claimius.samna_app (id, slug, name, private_key, private_seed, sa_owner_id, sa_created_by)
    SELECT v_app_a, 'sqltest_feed_a', 'Feed A', 'key', 'seed', sa_owner_id, sa_created_by FROM claimius.samna_app WHERE id = v_system;
    INSERT INTO claimius.samna_app (id, slug, name, private_key, private_seed, sa_owner_id, sa_created_by)
    SELECT v_app_b, 'sqltest_feed_b', 'Feed B', 'key', 'seed', sa_owner_id, sa_created_by FROM claimius.samna_app WHERE id = v_system;

    SELECT coalesce(max(seq), 0) INTO v_seq FROM claimius.sync_event;

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by, private_seed)
    VALUES (v_mine, 'Mine', 'company', v_mine, v_mine, 0, v_owner, 'seed-mine'),
           (v_theirs, 'Theirs', 'company', v_theirs, v_theirs, 0, v_owner, 'seed-theirs');
    PERFORM claimius.link_app_object(v_app_a, 'claimius.organization', v_mine);
    PERFORM claimius.link_app_object(v_app_b, 'claimius.organization', v_theirs);

    SELECT count(*) INTO v_count
      FROM claimius.get_sync_events(v_app_a, v_seq) e
     WHERE e.event_type = 'claimius.organization' AND (e.payload ->> 'id')::UUID = v_mine;
    IF v_count = 0 THEN
        RAISE EXCEPTION 'an organization linked to the app must reach its delta feed';
    END IF;

    SELECT count(*) INTO v_count
      FROM claimius.get_sync_events(v_app_a, v_seq) e
     WHERE e.event_type = 'claimius.organization' AND (e.payload ->> 'id')::UUID = v_theirs;
    IF v_count <> 0 THEN
        RAISE EXCEPTION 'an organization linked only to another app must never reach this app''s delta feed, got % events', v_count;
    END IF;
END $$;

ROLLBACK;
