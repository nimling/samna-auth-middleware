BEGIN;
SELECT plan(3);

SELECT claimius.upsert_disciple_state();
SET LOCAL claimius.replay_mode = 'true';

INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
VALUES ('10000000-0000-4000-8000-000000000ab1', 'Linked child', 'company',
        '10000000-0000-4000-8000-000000000ab0', '10000000-0000-4000-8000-000000000ab0', 1,
        '00000000-0000-4000-8000-000000000ab0');

SELECT lives_ok(
    $$INSERT INTO claimius.claim (id, name, sa_access, sa_owner_id, sa_root_id, sa_created_by)
      VALUES ('30000000-0000-4000-8000-000000000ab1', 'Child crew', 4,
              '10000000-0000-4000-8000-000000000ab1', '10000000-0000-4000-8000-000000000ab0',
              '00000000-0000-4000-8000-000000000ab0')$$,
    'a claim owned by a linked organization applies with its root outside the app'
);

SELECT lives_ok(
    $$INSERT INTO claimius.claim_object (claim_id, object_id, object_type, sa_owner_id, sa_root_id, sa_created_by)
      VALUES ('30000000-0000-4000-8000-000000000ab1', '10000000-0000-4000-8000-000000000ab1', 'claimius.organization',
              '10000000-0000-4000-8000-000000000ab1', '10000000-0000-4000-8000-000000000ab0',
              '00000000-0000-4000-8000-000000000ab0')$$,
    'its binding applies with the same root'
);

SELECT is_empty(
    $$SELECT 1 FROM claimius.organization WHERE id = '10000000-0000-4000-8000-000000000ab0'$$,
    'the root outside the app is never held on the disciple'
);

SELECT * FROM finish();
ROLLBACK;
