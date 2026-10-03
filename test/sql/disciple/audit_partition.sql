BEGIN;
SELECT plan(6);

DO $$
BEGIN
    PERFORM claimius.upsert_disciple_state();

    CREATE TEMP TABLE written AS
    SELECT (claimius.insert_audit('update', 'test', 'claimius.organization',
                                 '10000000-0000-4000-8000-0000000000a7', '10000000-0000-4000-8000-0000000000a7',
                                 NULL, 'audit partition')).id AS id;

    INSERT INTO claimius.audit (id, operation, type, object_type, object_id, sa_owner_id, sa_created_at)
    VALUES ('90000000-0000-4000-8000-0000000000a7', 'update', 'test', 'claimius.organization',
            '10000000-0000-4000-8000-0000000000a7', '10000000-0000-4000-8000-0000000000a7', '2019-03-14T12:00:00Z');
END $$;

SELECT is(
    (SELECT c.relkind::TEXT FROM pg_class c WHERE c.oid = 'claimius.audit'::REGCLASS),
    'p',
    'audit is a partitioned table'
);

SELECT is(
    (SELECT a.amname::TEXT FROM pg_class i JOIN pg_am a ON a.oid = i.relam WHERE i.oid = 'claimius.idx_audit_sa_created_at'::REGCLASS),
    'brin',
    'sa_created_at carries a BRIN index'
);

SELECT is(
    (SELECT a.tableoid::REGCLASS::TEXT FROM claimius.audit a WHERE a.id = (SELECT id FROM written)),
    'claimius.audit_' || to_char(now() AT TIME ZONE 'UTC', 'YYYYMM'),
    'insert_audit opens the month''s partition and writes the row into it'
);

SELECT is(
    (SELECT a.tableoid::REGCLASS::TEXT FROM claimius.audit a WHERE a.id = '90000000-0000-4000-8000-0000000000a7'),
    'claimius.audit_default',
    'a row of a month with no partition lands in audit_default'
);

SELECT claimius.upsert_audit_partition('2019-03-01T00:00:00Z');

SELECT is(
    (SELECT a.tableoid::REGCLASS::TEXT FROM claimius.audit a WHERE a.id = '90000000-0000-4000-8000-0000000000a7'),
    'claimius.audit_201903',
    'opening a month moves that month''s rows out of audit_default'
);

SELECT is(
    (SELECT count(*) FROM claimius.audit a WHERE a.object_id = '10000000-0000-4000-8000-0000000000a7'),
    2::BIGINT,
    'every row stays readable through the parent'
);

SELECT * FROM finish();
ROLLBACK;
