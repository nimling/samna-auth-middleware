BEGIN;
SELECT plan(5);

DO $$
DECLARE
    v_sys   UUID := '00000000-0000-4000-8000-0000000007c0';
    v_user  UUID := 'a0000000-0000-4000-8000-0000000007c1';
    v_root  UUID := '10000000-0000-4000-8000-0000000007c0';
    v_crew  UUID;
    v_deny  UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();
    PERFORM claimius.upsert_app_user(p_user_id => v_user, p_first_name => 'Reader');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Graph root', 'company', v_root, v_root, 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    SELECT md5('graph kid ' || k)::UUID, 'Kid ' || k, 'company', v_root, v_root, 1, v_sys
    FROM generate_series(1, 60) k;
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    SELECT md5('graph grand ' || k || ' ' || g)::UUID, 'Grand ' || k || ' ' || g, 'company', md5('graph kid ' || k)::UUID, v_root, 2, v_sys
    FROM generate_series(1, 60) k, generate_series(1, 60) g;

    v_crew := (claimius.insert_claim(
        p_name => 'Graph crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_deny := (claimius.insert_claim(
        p_name => 'Graph hide kid', p_description => NULL, p_sa_access => 20,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys, p_inherits => FALSE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_deny, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => md5('graph kid 1')::UUID, p_object_type => 'claimius.organization', p_inherits => FALSE);
    PERFORM claimius.insert_user_claim(p_claim_id => v_crew, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM claimius.insert_user_claim(p_claim_id => v_deny, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM count(*) FROM claimius.get_organizations(v_user, 0);
END $$;

CREATE TEMP TABLE graph_run AS
SELECT sum(pg_stat_get_xact_tuples_returned(i.indexrelid))::BIGINT AS entries, clock_timestamp() AS at, NULL::JSONB AS links
FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'claimius';

UPDATE graph_run SET links = claimius.get_graph_links_owner(
    'a0000000-0000-4000-8000-0000000007c1', 4) -> 'links';

CREATE TEMP TABLE graph_cost AS
SELECT (SELECT sum(pg_stat_get_xact_tuples_returned(i.indexrelid))::BIGINT
        FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'claimius') - r.entries AS entries,
       clock_timestamp() - r.at AS took
FROM graph_run r;

SELECT is(
    (SELECT count(*) FROM jsonb_array_elements((SELECT links FROM graph_run)) l WHERE l ->> 'target_type' = 'organization'),
    3659::BIGINT,
    'every organization the reader reaches but the root has one link to its closest reached ancestor');

SELECT is(
    (SELECT count(*) FROM jsonb_array_elements((SELECT links FROM graph_run)) l
     WHERE l ->> 'source_id' = '10000000-0000-4000-8000-0000000007c0'
       AND l ->> 'target_id' IN (SELECT md5('graph grand 1 ' || g)::UUID::TEXT FROM generate_series(1, 60) g)),
    60::BIGINT,
    'the children of the hidden organization link past it to the root, the closest ancestor the reader reaches'
);

SELECT is(
    (SELECT count(*) FROM jsonb_array_elements((SELECT links FROM graph_run)) l
     WHERE l ->> 'target_id' = md5('graph grand 2 7')::UUID::TEXT
       AND l ->> 'source_id' = md5('graph kid 2')::UUID::TEXT),
    1::BIGINT,
    'an organization under a reached parent links to that parent and to nothing deeper'
);

SELECT diag('graph links over 3,660 organizations read ' || (SELECT entries FROM graph_cost) || ' index entries in ' || (SELECT took FROM graph_cost));

SELECT cmp_ok((SELECT entries FROM graph_cost), '<', 20 * 3660::BIGINT,
    'the links read index entries linear in the organizations, fewer than 20 each');

SELECT cmp_ok((SELECT took FROM graph_cost), '<', INTERVAL '3 seconds',
    'the links of 3,660 organizations take under three seconds');

SELECT * FROM finish();
ROLLBACK;
