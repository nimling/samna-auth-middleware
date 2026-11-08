BEGIN;
SELECT plan(5);

CREATE TABLE public.sqltest_tr_alpha (
    id            UUID PRIMARY KEY,
    name          TEXT NOT NULL,
    sa_owner_id   UUID NOT NULL,
    sa_created_by UUID,
    sa_deleted_at TIMESTAMPTZ,
    sa_created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE public.sqltest_tr_beta (
    id            UUID PRIMARY KEY,
    name          TEXT NOT NULL,
    sa_owner_id   UUID NOT NULL,
    sa_created_by UUID,
    sa_deleted_at TIMESTAMPTZ,
    sa_created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

SELECT is(
    (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
     WHERE n.nspname = 'claimius'
       AND p.proname IN ('get_owner_graph', 'get_claim_graph', 'get_graph_links_owner', 'get_graph_links_claim')
       AND 'plan_cache_mode=force_custom_plan' = ANY(p.proconfig)),
    (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
     WHERE n.nspname = 'claimius'
       AND p.proname IN ('get_owner_graph', 'get_claim_graph', 'get_graph_links_owner', 'get_graph_links_claim')),
    'every graph function plans each call for its own parameters'
);

SELECT claimius.upsert_table_info('public.sqltest_tr_alpha');
SELECT claimius.upsert_table_info('public.sqltest_tr_beta');

SELECT is(
    (SELECT count(*) FROM claimius.table_info WHERE object_type IN ('public.sqltest_tr_alpha', 'public.sqltest_tr_beta')),
    2::BIGINT,
    'registering one table leaves every other registration in place'
);

SELECT lives_ok(
    $$SELECT claimius.delete_table_info('public.sqltest_tr_alpha')$$,
    'delete_table_info deregisters a registered table'
);

SELECT is(
    (SELECT array_agg(object_type ORDER BY object_type) FROM claimius.table_info WHERE object_type IN ('public.sqltest_tr_alpha', 'public.sqltest_tr_beta')),
    ARRAY['public.sqltest_tr_beta']::TEXT[],
    'only the named table leaves claimius'
);

SELECT throws_ok(
    $$SELECT claimius.delete_table_info('public.sqltest_tr_alpha')$$,
    'P0001',
    'object type public.sqltest_tr_alpha is not a registered external table',
    'deregistering a table that is not registered raises'
);

SELECT * FROM finish();
ROLLBACK;
