BEGIN;
SELECT plan(5);

SELECT claimius.upsert_disciple_state();
SET LOCAL claimius.replay_mode = 'true';

INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
VALUES ('10000000-0000-4000-8000-000000000ab1', 'Linked child', 'customer',
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

SELECT results_eq(
    $$SELECT object_id, hop FROM claimius.get_owner_ancestors('claimius.organization', '10000000-0000-4000-8000-000000000ab1')$$,
    $$VALUES ('10000000-0000-4000-8000-000000000ab1'::UUID, 0)$$,
    'an organization whose owner is outside the app is the top of its own closure'
);

INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
VALUES ('10000000-0000-4000-8000-000000000ab2', 'Linked grandchild', 'customer',
        '10000000-0000-4000-8000-000000000ab1', '10000000-0000-4000-8000-000000000ab0', 2,
        '00000000-0000-4000-8000-000000000ab0');

SELECT results_eq(
    $$SELECT object_id, hop FROM claimius.get_owner_ancestors('claimius.organization', '10000000-0000-4000-8000-000000000ab2')$$,
    $$VALUES ('10000000-0000-4000-8000-000000000ab2'::UUID, 0), ('10000000-0000-4000-8000-000000000ab1'::UUID, 1)$$,
    'a child of that organization hangs under it'
);

SELECT * FROM finish();
ROLLBACK;
