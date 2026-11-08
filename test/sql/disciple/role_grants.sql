BEGIN;
SELECT plan(6);

SELECT is_empty($$
    SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'claimius' AND c.relkind IN ('r', 'p')
      AND NOT has_table_privilege('claimius_disciple_client', c.oid, 'SELECT')
$$, 'claimius_disciple_client reads every claimius table');

SELECT is_empty($$
    SELECT t FROM unnest(ARRAY['user_object', 'object_info', 'user_users', 'disciple_state', 'samna_user', 'samna_client',
                               'samna_secret', 'organization', 'location', 'claim', 'user_claim', 'claim_object',
                               'object_field', 'user_field', 'user_relation', 'audit', 'inheritance_info', 'table_info',
                               'notification', 'claim_graph_cache']) AS t
    WHERE NOT has_table_privilege('claimius_disciple_client', 'claimius.' || t, 'INSERT, UPDATE, DELETE')
$$, 'claimius_disciple_client writes every table the sync layer applies');

SELECT ok(NOT has_table_privilege('claimius_reader', 'claimius.claim_graph_cache', 'INSERT, UPDATE, DELETE'),
    'claimius_reader holds no DML on the claim graph cache');

SELECT is_empty($$
    SELECT p.oid::REGPROCEDURE FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname = 'claimius' AND p.proname LIKE 'get\_%'
      AND NOT has_function_privilege('claimius_reader', p.oid, 'EXECUTE')
$$, 'claimius_reader executes every get_* function');

SELECT is_empty($$
    SELECT i.inhrelid::REGCLASS FROM pg_inherits i
    WHERE i.inhparent = 'claimius.audit'::REGCLASS
      AND ARRAY(SELECT a.privilege_type || ':' || a.grantee FROM pg_class c, aclexplode(c.relacl) a
                WHERE c.oid = i.inhrelid AND a.grantee <> 0 AND a.grantee <> c.relowner ORDER BY 1)
       IS DISTINCT FROM
          ARRAY(SELECT a.privilege_type || ':' || a.grantee FROM pg_class c, aclexplode(c.relacl) a
                WHERE c.oid = 'claimius.audit'::REGCLASS AND a.grantee <> 0 AND a.grantee <> c.relowner ORDER BY 1)
$$, 'every audit partition holds exactly the grants of the partitioned table');

GRANT INSERT, UPDATE, DELETE ON claimius.audit_default TO claimius_reader;
SELECT claimius._match_audit_partition_grants('claimius.audit_default');
SELECT ok(NOT has_table_privilege('claimius_reader', 'claimius.audit_default', 'INSERT')
      AND has_table_privilege('claimius_disciple_client', 'claimius.audit_default', 'INSERT'),
    'a grant audit_default kept from the unpartitioned audit is taken back to the partitioned table''s');

SELECT * FROM finish();
ROLLBACK;
