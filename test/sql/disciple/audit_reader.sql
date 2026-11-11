BEGIN;
SELECT plan(6);

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-0000000007a0';
    v_actor  UUID := 'a0000000-0000-4000-8000-0000000007a1';
    v_root   UUID := '10000000-0000-4000-8000-0000000007a1';
    v_child  UUID := '10000000-0000-4000-8000-0000000007a2';
    v_claim  UUID;
    v_grant  UUID;
    v_user   UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();

    PERFORM claimius.upsert_app_user(p_user_id => v_actor, p_first_name => 'Actor');
    FOREACH v_user IN ARRAY ARRAY['a0000000-0000-4000-8000-0000000007b1', 'a0000000-0000-4000-8000-0000000007b2',
                                  'a0000000-0000-4000-8000-0000000007b3']::UUID[] LOOP
        PERFORM claimius.upsert_app_user(p_user_id => v_user, p_first_name => 'Reader');
    END LOOP;

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Audit root', 'customer', v_root, v_root, 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_child, 'Audit child', 'customer', v_root, v_root, 1, v_sys);

    v_claim := (claimius.insert_claim(
        p_name => 'Audit reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    v_grant := (claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_actor,
        p_sa_owner_id => v_root, p_sa_created_by => v_sys) -> 'user_claim' ->> 'id')::UUID;

    PERFORM claimius.insert_audit('PostUser', 'create', 'claimius.samna_user', v_actor, v_root, v_grant, 'created');
    PERFORM claimius.insert_audit('PostClaim', 'create', 'claimius.claim', v_claim, v_root, v_grant, 'created');
    PERFORM claimius.insert_audit('PostOrganization', 'create', 'claimius.organization', v_child, v_child, v_grant, 'created');
    PERFORM claimius.insert_audit('PutOrganization', 'update', 'claimius.organization', v_child, v_child, v_grant, 'updated');

    CREATE TEMP TABLE audit_claim AS SELECT v_claim AS id, v_root AS root, v_sys AS sys;
END $$;

SELECT is(
    (SELECT count(*) FROM claimius.get_audit('a0000000-0000-4000-8000-0000000007b1', 4)),
    0::BIGINT,
    'a reader whose grant has not arrived reads no audit'
);

SET LOCAL claimius.replay_mode = 'true';
INSERT INTO claimius.user_claim (claim_id, user_id, sa_owner_id, sa_created_by)
SELECT c.id, u.user_id, c.root, c.sys
FROM audit_claim c
    CROSS JOIN (VALUES ('a0000000-0000-4000-8000-0000000007b1'::UUID), ('a0000000-0000-4000-8000-0000000007b2'::UUID),
                       ('a0000000-0000-4000-8000-0000000007b3'::UUID)) u(user_id);
SET LOCAL claimius.replay_mode = 'false';

SELECT is(
    (SELECT count(*) FROM claimius.get_audit('a0000000-0000-4000-8000-0000000007b1', 4)),
    4::BIGINT,
    'the read after the replayed grant settles the reader and returns every audit of the organizations it reaches'
);

SELECT is(
    (SELECT count(*) FROM claimius.get_audit('a0000000-0000-4000-8000-0000000007b2', 4)),
    4::BIGINT,
    'a reader''s first read after a replayed grant returns every audit'
);

SELECT is(
    (SELECT count(*) FROM claimius.audit a
      WHERE a.id IN (SELECT id FROM claimius.get_audit('a0000000-0000-4000-8000-0000000007b3', 4))),
    4::BIGINT,
    'a first read through the AuditWhere subquery shape returns every audit'
);

SELECT is(
    (SELECT count(*) FROM claimius.get_audit('a0000000-0000-4000-8000-0000000007b3', 4)),
    4::BIGINT,
    'the reader''s second read agrees with its first'
);

INSERT INTO claimius.claim (id, name, sa_access, inherits, sa_owner_id, sa_root_id, sa_created_by)
SELECT 'c0000000-0000-4000-8000-0000000007c1', 'Routed child reader', 4, TRUE, root, root, sys FROM audit_claim;
INSERT INTO claimius.claim_object (claim_id, object_id, object_type, inherits, sa_owner_id, sa_root_id, sa_created_by)
SELECT 'c0000000-0000-4000-8000-0000000007c1', '10000000-0000-4000-8000-0000000007a2', 'claimius.organization', TRUE, root, root, sys FROM audit_claim;
DO $$ BEGIN PERFORM claimius.upsert_app_user(p_user_id => 'a0000000-0000-4000-8000-0000000007b4', p_first_name => 'Reader'); END $$;

SET LOCAL claimius.replay_mode = 'true';
INSERT INTO claimius.user_claim (claim_id, user_id, sa_owner_id, sa_created_by)
SELECT 'c0000000-0000-4000-8000-0000000007c1', 'a0000000-0000-4000-8000-0000000007b4', root, sys FROM audit_claim;
SET LOCAL claimius.replay_mode = 'false';

SELECT is(
    (SELECT count(*) FROM claimius.audit a
      WHERE a.id IN (SELECT id FROM claimius.get_audit('a0000000-0000-4000-8000-0000000007b4', 4))),
    2::BIGINT,
    'a claim and binding upserted from a routed answer and a grant replayed after them read on the first try'
);

SELECT * FROM finish();
ROLLBACK;
