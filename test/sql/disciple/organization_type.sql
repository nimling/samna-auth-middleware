BEGIN;
SELECT plan(2);

SELECT is(
    enum_range(NULL::claimius.organization_type)::text[],
    ARRAY['system', 'customer', 'entity'],
    'the organization type is system, customer or entity'
);

SELECT is(
    (SELECT count(*) FROM claimius.organization WHERE type::text NOT IN ('system', 'customer', 'entity')),
    0::bigint,
    'no organization carries a label outside the type'
);

SELECT * FROM finish();
ROLLBACK;
