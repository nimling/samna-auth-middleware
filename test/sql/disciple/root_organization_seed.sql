BEGIN;
SELECT plan(2);

DO $$ BEGIN PERFORM claimius.init(); END $$;

INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
VALUES ('e0000000-0000-4000-8000-00000000000e', 'Seeded root', 'company',
        'e0000000-0000-4000-8000-00000000000e', 'e0000000-0000-4000-8000-00000000000e', 0,
        'd0000000-0000-4000-8000-00000000000d');

SELECT is(
    (SELECT id FROM claimius.organization WHERE name = 'Seeded root'),
    'e0000000-0000-4000-8000-00000000000e'::UUID,
    'a root organization inserted on a disciple keeps its id'
);

SELECT isnt(
    (SELECT private_seed FROM claimius.organization WHERE id = 'e0000000-0000-4000-8000-00000000000e'),
    NULL,
    'a root organization inserted on a disciple gets a non null private_seed'
);

SELECT * FROM finish();
ROLLBACK;
