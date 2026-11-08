BEGIN;
SELECT plan(7);

CREATE TEMP TABLE stamp (key TEXT PRIMARY KEY, id UUID);

CREATE FUNCTION pg_temp.stamp(p_key TEXT) RETURNS UUID AS $$
    SELECT id FROM stamp WHERE key = p_key
$$ LANGUAGE sql;

DO $$
DECLARE
    v_sys     UUID := '00000000-0000-4000-8000-000000000e00';
    v_root    UUID := '10000000-0000-4000-8000-000000000e01';
    v_ada     UUID := 'a0000000-0000-4000-8000-000000000e01';
    v_bo      UUID := 'a0000000-0000-4000-8000-000000000e02';
    v_cy      UUID := 'a0000000-0000-4000-8000-000000000e03';
    v_member  UUID;
    v_crew    UUID;
    v_planted UUID;
    v_stray   UUID;
BEGIN
    PERFORM claimius.init();
    PERFORM claimius.ensure_app_user(p_user_id => v_ada, p_first_name => 'Ada');
    PERFORM claimius.ensure_app_user(p_user_id => v_bo, p_first_name => 'Bo');
    PERFORM claimius.ensure_app_user(p_user_id => v_cy, p_first_name => 'Cy');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Stamp root', 'company', v_root, v_root, 0, v_sys);

    v_member := (claimius.create_claim(
        p_name => 'Stamp member', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    v_crew := (claimius.create_claim(
        p_name => 'Stamp crew', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    v_planted := (claimius.create_claim(
        p_name => 'Stamp planted', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    v_stray := (claimius.create_claim(
        p_name => 'Stamp stray', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_claim_id => v_crew, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);

    INSERT INTO stamp VALUES
        ('root', v_root), ('crew', v_crew), ('planted', v_planted), ('stray', v_stray),
        ('ada_uc', (claimius.assign_claim_user(p_claim_id => v_member, p_user_id => v_ada,
            p_sa_owner_id => v_root, p_sa_created_by => v_sys) -> 'user_claim' ->> 'id')::UUID);
    PERFORM claimius.assign_claim_user(p_claim_id => v_member, p_user_id => v_bo, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM claimius.assign_claim_user(p_claim_id => v_crew, p_user_id => v_bo, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    PERFORM claimius.assign_claim_user(p_claim_id => v_member, p_user_id => v_cy, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
    UPDATE claimius.user_claim SET sa_deleted_at = now() WHERE user_id = v_cy;

    UPDATE claimius.user_claim SET sa_created_by = v_ada WHERE id = pg_temp.stamp('ada_uc');
    UPDATE claimius.claim SET sa_created_by = v_ada WHERE id = v_planted;
    UPDATE claimius.organization SET sa_created_by = v_ada WHERE id = v_root;
    UPDATE claimius.claim_object SET sa_created_by = v_bo WHERE claim_id = v_crew;
    UPDATE claimius.claim SET sa_created_by = v_cy WHERE id = v_crew;
    UPDATE claimius.claim SET sa_created_by = '99999999-0000-4000-8000-000000000e09' WHERE id = v_stray;
END $$;

SELECT cmp_ok(claimius._restamp_created_by(), '>=', 3, 'the upgrade rewrites the stamps a v1.1.9 set wrote as user ids');

SELECT is((SELECT sa_created_by FROM claimius.user_claim WHERE id = pg_temp.stamp('ada_uc')), pg_temp.stamp('ada_uc'),
    'a user_claim stamped with its own user, who holds only it, takes its own id');
SELECT is((SELECT sa_created_by FROM claimius.claim WHERE id = pg_temp.stamp('planted')), pg_temp.stamp('ada_uc'),
    'a claim stamped with a user holding one live user_claim takes that user_claim.id');
SELECT is((SELECT sa_created_by FROM claimius.organization WHERE id = pg_temp.stamp('root')), pg_temp.stamp('ada_uc'),
    'an organization takes it too');
SELECT is((SELECT sa_created_by FROM claimius.claim_object WHERE claim_id = pg_temp.stamp('crew') LIMIT 1),
    'a0000000-0000-4000-8000-000000000e02'::UUID,
    'a stamp whose user holds two live user_claims is left untouched');
SELECT is((SELECT ARRAY[c.sa_created_by, s.sa_created_by] FROM claimius.claim c, claimius.claim s
           WHERE c.id = pg_temp.stamp('crew') AND s.id = pg_temp.stamp('stray')),
    ARRAY['a0000000-0000-4000-8000-000000000e03', '99999999-0000-4000-8000-000000000e09']::UUID[],
    'a stamp whose user holds no live user_claim, and one naming no user, are left untouched');

SELECT is(claimius._restamp_created_by(), 0, 'a second run rewrites nothing');

SELECT * FROM finish();
ROLLBACK;
