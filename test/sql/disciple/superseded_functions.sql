BEGIN;
SELECT plan(3);

CREATE TEMP TABLE superseded (signature TEXT PRIMARY KEY);
INSERT INTO superseded VALUES
        ('claimius.insert_claim(text,text,integer,uuid,uuid,uuid,boolean,claimius.claim_type)'),
        ('claimius.update_claim(uuid,text,text,integer,boolean)'),
        ('claimius.ensure_audit_partition(timestamp with time zone)'),
        ('claimius.write_audit(text,text,text,uuid,uuid,uuid,text)'),
        ('claimius.create_claim(text,text,integer,uuid,uuid,uuid,boolean,claimius.claim_type)'),
        ('claimius.remove_claim(uuid,uuid)'),
        ('claimius.assign_claim_user(uuid,uuid,uuid,uuid,text,timestamp with time zone,timestamp with time zone)'),
        ('claimius.update_claim_user(uuid,text,timestamp with time zone,timestamp with time zone)'),
        ('claimius.remove_user_claim(uuid,uuid)'),
        ('claimius.assign_claim_object(uuid,uuid,uuid,uuid,uuid,text,integer,boolean,text,uuid,jsonb)'),
        ('claimius.remove_claim_object(uuid,uuid)'),
        ('claimius.init_claimius_tables(text[])'),
        ('claimius.init_claimius_internal()'),
        ('claimius.merge_user(uuid,uuid,uuid)'),
        ('claimius.create_root_organization(text,uuid,text,text,uuid,text)'),
        ('claimius.init(text)'),
        ('claimius.ensure_app_user(uuid,text,text,text,text,text,text,text)'),
        ('claimius.create_organization_user(uuid,uuid,jsonb,boolean,jsonb,text)'),
        ('claimius._apply_denies(jsonb,jsonb,text,uuid)'),
        ('claimius._attach_object_to_trees(text,uuid,record)'),
        ('claimius._build_grants_for_object(uuid,text,uuid)'),
        ('claimius._denormalize_object(text,uuid)'),
        ('claimius._detach_object_from_trees(text,uuid,record)'),
        ('claimius._direct_grant_for_object(uuid,text,uuid)'),
        ('claimius._prune_graph_rows(jsonb,uuid,uuid,integer,text,uuid)'),
        ('claimius._raise_claim_diff(uuid)'),
        ('claimius._settle_claim_diff_for_user(uuid,uuid)'),
        ('claimius._settle_claim_diff(uuid)'),
        ('claimius._settle_object_diff(text,uuid,boolean)'),
        ('claimius._settle_object_diff(uuid,text,uuid,boolean)'),
        ('claimius._settle_diff_share(uuid,uuid)'),
        ('claimius._clear_diff(uuid)'),
        ('claimius._lock_all_objects()'),
        ('claimius._lock_objects(text[])'),
        ('claimius._object_lock_budget()'),
        ('claimius._count_user_users()'),
        ('claimius._refresh_user_users(uuid)'),
        ('claimius.create_organization_user(uuid,uuid,uuid,jsonb,boolean)'),
        ('claimius._diff_holders(uuid)'),
        ('claimius.build_location_tree(uuid)'),
        ('claimius.build_ownership_tree(uuid)'),
        ('claimius.build_parenthood_tree(text,uuid)'),
        ('claimius.ensure_app_user(uuid,text,text,text,text,text,text)'),
        ('claimius.get_claim_graph(uuid,uuid,integer,integer)'),
        ('claimius.get_direct_access(uuid,uuid,text)'),
        ('claimius.get_graph_links_claim(uuid,integer,text[],text[],uuid,integer)'),
        ('claimius.get_graph_links_owner(uuid,integer,text[],text[],uuid,integer)'),
        ('claimius._window_links(jsonb,uuid,integer,text,uuid)'),
        ('claimius.get_graph_objects(uuid,integer,text[],text[],uuid[],boolean,boolean)'),
        ('claimius.get_locations(uuid,integer)'),
        ('claimius.get_objects(uuid,text,integer)'),
        ('claimius.get_organizations(uuid,integer)'),
        ('claimius.get_owner_graph(uuid,uuid,integer,integer)'),
        ('claimius.get_secrets(uuid,integer)'),
        ('claimius.get_user_access(uuid,uuid,integer)'),
        ('claimius.graph_object_types()'),
        ('claimius.mutation_access(uuid,uuid,text,integer)'),
        ('claimius.resolve_access(uuid,uuid,text,integer)'),
        ('claimius.search(uuid,text,text[],integer,uuid[],uuid[],uuid[],uuid,text,uuid[],integer,integer)'),
        ('claimius.touch_object_usage(uuid,text,uuid)'),
        ('claimius._settle_object_diff(uuid,text[],uuid[],boolean[])'),
        ('claimius._settle_claim_diff(uuid,uuid,timestamp with time zone)'),
        ('claimius._diff_holders(uuid[])'),
        ('claimius._clear_diff(uuid[])'),
        ('claimius._settle_diff_share(uuid,uuid[])'),
        ('claimius._cascade_recompute(uuid,text,uuid)'),
        ('claimius.recompute_user_object(uuid,text,uuid[])'),
        ('claimius._build_grants_for_objects(uuid,text,uuid[],uuid[])'),
        ('claimius._best_grant(jsonb,uuid,integer)'),
        ('claimius.upsert_retention(text,integer,text)'),
        ('claimius.delete_exported(text,text,text,text[],boolean)'),
        ('claimius.delete_exported(text,uuid,text,text,text[],boolean)'),
        ('claimius.get_expired(text,text,integer)'),
        ('claimius.get_expired_audit(interval,text,integer)'),
        ('claimius.delete_exported_audit(text[])'),
        ('claimius.upsert_retention(text,claimius.retention_class,integer,text,text)');

SELECT is_empty($$
    SELECT s.signature FROM superseded s
    WHERE EXISTS (
        SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE n.nspname = 'claimius' AND p.oid::REGPROCEDURE::TEXT = s.signature)
$$, 'no function v1.1.0-alpha0078, v1.1.9 or the link graphs before their window carried that a fresh build lacks survives the replay');

SELECT hasnt_table('claimius', 'access_diff_user', 'the per user settle record is gone: the cursor and the mark replace it');
SELECT hasnt_table('claimius', 'reconcile_queue', 'the reconcile queue is gone: nothing parks a pair');

SELECT * FROM finish();
ROLLBACK;
