BEGIN;
SELECT plan(2);

DO $$
DECLARE
    v_sys    UUID := '00000000-0000-4000-8000-0000000007c0';
    v_synced UUID := 'a0000000-0000-4000-8000-0000000007c1';
    v_local  UUID := 'a0000000-0000-4000-8000-0000000007c2';
    v_root   UUID := '10000000-0000-4000-8000-0000000007c0';
    v_crew   UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();
    PERFORM claimius.upsert_app_user(p_user_id => v_synced, p_first_name => 'Synced');
    PERFORM claimius.upsert_app_user(p_user_id => v_local, p_first_name => 'Local');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Replay cascade root', 'company', v_root, v_root, 0, v_sys);
    v_crew := (claimius.insert_claim(
        p_name => 'Replay cascade crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_user_claim(p_claim_id => v_crew, p_user_id => v_synced, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM claimius.insert_user_claim(p_claim_id => v_crew, p_user_id => v_local, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
END $$;

SET LOCAL claimius.replay_mode = 'true';
UPDATE claimius.samna_user SET sa_deleted_at = now() WHERE user_id = 'a0000000-0000-4000-8000-0000000007c1';
SET LOCAL claimius.replay_mode = 'false';
UPDATE claimius.samna_user SET sa_deleted_at = now() WHERE user_id = 'a0000000-0000-4000-8000-0000000007c2';

SELECT is(
    (SELECT count(*) FROM claimius.user_claim WHERE user_id = 'a0000000-0000-4000-8000-0000000007c1' AND sa_deleted_at IS NULL),
    1::BIGINT,
    'a synced soft delete leaves the user''s grants to the feed'
);

SELECT is(
    (SELECT count(*) FROM claimius.user_claim WHERE user_id = 'a0000000-0000-4000-8000-0000000007c2' AND sa_deleted_at IS NULL),
    0::BIGINT,
    'a local soft delete still cascades to the user''s grants'
);

SELECT * FROM finish();
ROLLBACK;
