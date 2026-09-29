BEGIN;
SELECT plan(10);

CREATE FUNCTION pg_temp.app() RETURNS UUID AS $$
    SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_sync_feed'
$$ LANGUAGE sql;

DO $$
DECLARE
    v_app   UUID;
    v_sys   UUID := '00000000-0000-4000-8000-0000000000f0';
    v_root  UUID := '10000000-0000-4000-8000-0000000000f0';
    v_claim UUID;
    v_user  UUID;
BEGIN
    v_app := (claimius.init(
        p_system_app_slug         => 'sqltest_sync_feed',
        p_system_app_name         => 'SQL sync feed test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                                  'a0000000-0000-4000-8000-0000000000f3']::UUID[] LOOP
        PERFORM claimius.ensure_app_user(p_user_id => v_user, p_app_id => v_app, p_first_name => 'Holder');
    END LOOP;

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Feed root', 'company', v_root, v_root, 0, v_sys);
    PERFORM claimius.link_app_object(v_app, 'claimius.organization', v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-0000000000f1', 'Feed one', 'company', v_root, v_root, 1, v_sys),
           ('10000000-0000-4000-8000-0000000000f2', 'Feed two', 'company', v_root, v_root, 1, v_sys);

    CREATE TEMP TABLE before_grant AS SELECT coalesce(max(seq), 0) AS seq FROM claimius.sync_event;

    v_claim := (claimius.create_claim(
        p_app_id => v_app, p_name => 'Feed crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_app, p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                                  'a0000000-0000-4000-8000-0000000000f3']::UUID[] LOOP
        PERFORM claimius.assign_claim_user(p_app_id => v_app, p_claim_id => v_claim, p_user_id => v_user,
            p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    END LOOP;

    CREATE TEMP TABLE before_settle AS SELECT coalesce(max(seq), 0) AS seq FROM claimius.sync_event;

    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                                  'a0000000-0000-4000-8000-0000000000f3']::UUID[] LOOP
        PERFORM count(*) FROM claimius.get_objects(v_user, v_app, 'claimius.organization', 4);
    END LOOP;
END $$;

SELECT is_empty(
    $$SELECT 1 FROM pg_trigger
       WHERE tgname = 'tg_emit_sync_event'
         AND tgrelid IN ('claimius.user_object'::regclass, 'claimius.user_users'::regclass)$$,
    'user_object and user_users carry no sync publisher'
);

SELECT is(
    (SELECT array_agg(DISTINCT event_type ORDER BY event_type) FROM claimius.sync_event
      WHERE seq > (SELECT seq FROM before_grant) AND seq <= (SELECT seq FROM before_settle)),
    ARRAY['claimius.claim', 'claimius.claim_object', 'claimius.user_claim'],
    'a broad grant publishes its source rows: the claim, the binding and the assignments'
);

SELECT is(
    (SELECT count(*) FROM claimius.sync_event
      WHERE seq > (SELECT seq FROM before_grant) AND seq <= (SELECT seq FROM before_settle)
        AND event_type = 'claimius.user_claim'),
    3::BIGINT,
    'each assignment publishes one event'
);

SELECT ok(
    (SELECT count(*) FROM claimius.user_object
      WHERE app_id = pg_temp.app() AND object_type = 'claimius.organization'
        AND user_id IN ('a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                        'a0000000-0000-4000-8000-0000000000f3')) = 9
    AND (SELECT count(*) FROM claimius.user_users
          WHERE app_id = pg_temp.app()
            AND viewer_id IN ('a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                              'a0000000-0000-4000-8000-0000000000f3')
            AND target_user_id IN ('a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                                   'a0000000-0000-4000-8000-0000000000f3')) = 6,
    'the reads settled every holder''s pairs and counted every holder pair'
);

SELECT is_empty(
    $$SELECT 1 FROM claimius.sync_event
       WHERE seq > (SELECT seq FROM before_settle)
         AND event_type IN ('claimius.user_object', 'claimius.user_users')$$,
    'a settle writes no sync_event row for user_object or user_users'
);

INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
SELECT 'upsert', t, jsonb_build_object('id', gen_random_uuid(), 'app_id', pg_temp.app()), pg_temp.app()
FROM unnest(ARRAY['claimius.user_object', 'claimius.user_users', 'claimius.audit']) AS t;

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_sync_events(pg_temp.app(), 0)
       WHERE event_type IN ('claimius.user_object', 'claimius.user_users', 'claimius.audit')$$,
    'the delta feed serves no derived rows and no audit, even ones left in the outbox'
);

DO $$
BEGIN
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-0000000000f9', 'Feed late', 'company',
            '10000000-0000-4000-8000-0000000000f9', '10000000-0000-4000-8000-0000000000f9', 0,
            '00000000-0000-4000-8000-0000000000f0');
    CREATE TEMP TABLE before_link AS SELECT coalesce(max(seq), 0) AS seq FROM claimius.sync_event;
    PERFORM claimius.link_app_object(pg_temp.app(), 'claimius.organization', '10000000-0000-4000-8000-0000000000f9');
    CREATE TEMP TABLE before_unlink AS SELECT coalesce(max(seq), 0) AS seq FROM claimius.sync_event;
    PERFORM claimius.unlink_app_object(pg_temp.app(), 'claimius.organization', '10000000-0000-4000-8000-0000000000f9');
END $$;

SELECT is(
    (SELECT array_agg(operation::TEXT || ' ' || event_type || ' ' || (payload ->> 'id') || ' ' || (app_id = pg_temp.app())::TEXT)
       FROM claimius.sync_event
      WHERE seq > (SELECT seq FROM before_link) AND seq <= (SELECT seq FROM before_unlink)),
    ARRAY['upsert claimius.organization 10000000-0000-4000-8000-0000000000f9 true'],
    'linking an existing organization publishes the organization to the linked app'
);

SELECT ok(
    EXISTS (SELECT 1 FROM claimius.get_sync_events(pg_temp.app(), (SELECT seq FROM before_link)) e
             WHERE e.event_type = 'claimius.organization' AND e.operation = 'upsert'
               AND (e.payload ->> 'id')::UUID = '10000000-0000-4000-8000-0000000000f9'),
    'the linked organization reaches the app''s delta feed'
);

SELECT is(
    (SELECT array_agg(operation::TEXT || ' ' || event_type || ' ' || (payload ->> 'id') || ' ' || (app_id = pg_temp.app())::TEXT)
       FROM claimius.sync_event
      WHERE seq > (SELECT seq FROM before_unlink)),
    ARRAY['delete claimius.organization 10000000-0000-4000-8000-0000000000f9 true'],
    'unlinking publishes the organization''s delete to the unlinked app alone'
);

SELECT is(
    (SELECT count(*) FROM claimius.get_sync_events(pg_temp.app(), (SELECT seq FROM before_unlink)) e
      WHERE e.operation = 'delete' AND (e.payload ->> 'id')::UUID = '10000000-0000-4000-8000-0000000000f9'),
    1::BIGINT,
    'the unlink reaches the app''s delta feed'
);

SELECT * FROM finish();
ROLLBACK;
