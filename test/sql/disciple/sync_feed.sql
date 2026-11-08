BEGIN;
SELECT plan(5);

DO $$
DECLARE
    v_sys   UUID := '00000000-0000-4000-8000-0000000000f0';
    v_root  UUID := '10000000-0000-4000-8000-0000000000f0';
    v_claim UUID;
    v_user  UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();
    INSERT INTO claimius.prophet_state (last_applied_seq) VALUES (0);

    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                                  'a0000000-0000-4000-8000-0000000000f3']::UUID[] LOOP
        PERFORM claimius.upsert_app_user(p_user_id => v_user, p_first_name => 'Holder');
    END LOOP;

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Feed root', 'company', v_root, v_root, 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES ('10000000-0000-4000-8000-0000000000f1', 'Feed one', 'company', v_root, v_root, 1, v_sys),
           ('10000000-0000-4000-8000-0000000000f2', 'Feed two', 'company', v_root, v_root, 1, v_sys);

    CREATE TEMP TABLE before_grant AS SELECT coalesce(max(seq), 0) AS seq FROM claimius.sync_event;

    v_claim := (claimius.insert_claim(
        p_name => 'Feed crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                                  'a0000000-0000-4000-8000-0000000000f3']::UUID[] LOOP
        PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_user,
            p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    END LOOP;

    CREATE TEMP TABLE before_settle AS SELECT coalesce(max(seq), 0) AS seq FROM claimius.sync_event;

    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                                  'a0000000-0000-4000-8000-0000000000f3']::UUID[] LOOP
        PERFORM count(*) FROM claimius.get_objects(v_user, 'claimius.organization', 4);
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
      WHERE object_type = 'claimius.organization'
        AND user_id IN ('a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
                        'a0000000-0000-4000-8000-0000000000f3')) = 9
    AND (SELECT count(*) FROM claimius.user_users
          WHERE viewer_id IN ('a0000000-0000-4000-8000-0000000000f1', 'a0000000-0000-4000-8000-0000000000f2',
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

SELECT * FROM finish();
ROLLBACK;
