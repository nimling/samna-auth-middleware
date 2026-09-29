BEGIN;
SELECT plan(1);

CREATE TEMP TABLE superseded (signature TEXT PRIMARY KEY);
INSERT INTO superseded VALUES
        ('claimius._apply_denies(jsonb,jsonb,text,uuid)'),
        ('claimius._attach_object_to_trees(text,uuid,record)'),
        ('claimius._build_grants_for_object(uuid,uuid,text,uuid)'),
        ('claimius._denormalize_object(text,uuid)'),
        ('claimius._detach_object_from_trees(text,uuid,record)'),
        ('claimius._direct_grant_for_object(uuid,uuid,text,uuid)'),
        ('claimius._grant_hybrid_roles()'),
        ('claimius._prune_graph_rows(jsonb,uuid,uuid,uuid,integer,text,uuid)'),
        ('claimius._raise_claim_diff(uuid,uuid)'),
        ('claimius._settle_claim_diff_for_user(uuid,uuid,uuid)'),
        ('claimius._settle_claim_diff(uuid,uuid)'),
        ('claimius._settle_object_diff(uuid,text,uuid,boolean)'),
        ('claimius.analytics_capture_due(uuid,text,integer,interval)'),
        ('claimius.build_location_tree(uuid)'),
        ('claimius.build_ownership_tree(uuid)'),
        ('claimius.build_parenthood_tree(text,uuid)'),
        ('claimius.ensure_app_user(uuid,uuid,text,text,text,text,text,text)'),
        ('claimius.get_analytics_capture(uuid,text,integer)'),
        ('claimius.get_claim_graph(uuid,uuid,uuid,integer,integer)'),
        ('claimius.get_direct_access(uuid,uuid,uuid,text)'),
        ('claimius.get_graph_links_claim(uuid,uuid,integer,text[],text[],uuid,integer)'),
        ('claimius.get_graph_links_owner(uuid,uuid,integer,text[],text[],uuid,integer)'),
        ('claimius.get_graph_objects(uuid,uuid,integer,text[],text[],uuid[],boolean,boolean)'),
        ('claimius.get_locations(uuid,uuid,integer)'),
        ('claimius.get_objects(uuid,uuid,text,integer)'),
        ('claimius.get_organizations(uuid,uuid,integer)'),
        ('claimius.get_owner_graph(uuid,uuid,uuid,integer,integer)'),
        ('claimius.get_secrets(uuid,uuid,integer)'),
        ('claimius.get_sync_events(uuid,bigint,integer)'),
        ('claimius.get_user_access(uuid,uuid,uuid,integer)'),
        ('claimius.graph_object_types()'),
        ('claimius.init_hybrid(text,text,text,text,text,uuid,text,text,text,text,uuid[],jsonb,text,text,text,uuid,jsonb)'),
        ('claimius.mutation_access(uuid,uuid,text,integer)'),
        ('claimius.post_analytics_capture(uuid,text,integer,integer,text,bytea)'),
        ('claimius.recompute_user_object(uuid,uuid,text,uuid)'),
        ('claimius.resolve_access(uuid,uuid,text,integer)'),
        ('claimius.resolve_access(uuid,uuid,uuid,text,integer)'),
        ('claimius.search(uuid,uuid,text,text[],integer,uuid[],uuid[],uuid[],uuid,text,uuid[],integer,integer)'),
        ('claimius.touch_object_usage(uuid,uuid,text,uuid)');

SELECT is_empty($$
    SELECT s.signature FROM superseded s
    WHERE EXISTS (
        SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE n.nspname = 'claimius' AND p.oid::REGPROCEDURE::TEXT = s.signature)
$$, 'no function v1.1.0-alpha0078, v1.1.9 or the link graphs before their window carried that a fresh build lacks survives the replay');

SELECT * FROM finish();
ROLLBACK;
