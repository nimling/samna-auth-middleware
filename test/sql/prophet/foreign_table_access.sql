BEGIN;
SELECT plan(4);

DO $$
DECLARE
    v_app     UUID;
    v_a       UUID := 'a0000000-0000-4000-8000-0000000000fa';
    v_root    UUID := '10000000-0000-4000-8000-0000000000f1';
    v_foreign UUID := '20000000-0000-4000-8000-0000000000f2';
    v_claim   UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_foreign',
        p_system_app_name         => 'SQL foreign test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    PERFORM claimius.upsert_app_user(p_user_id => v_a, p_app_id => v_app, p_first_name => 'Ada');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Foreign root', 'customer', v_root, v_root, 0, v_a);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);

    v_claim := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Disciple writer', p_description => NULL, p_sa_access => 6,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_claim, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_a);

    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a,
        p_object_id => v_foreign, p_object_type => 'public.sqltest_disciple_only', p_sa_access => 6, p_inherits => TRUE
    );
END $$;

SELECT is(
    (SELECT count(*) FROM claimius.claim_object
      WHERE claim_id = (SELECT id FROM claimius.claim WHERE name = 'Disciple writer')
        AND object_type = 'public.sqltest_disciple_only'
        AND object_id = '20000000-0000-4000-8000-0000000000f2'),
    1::BIGINT,
    'the prophet stores a binding to a table only a disciple holds'
);

SELECT lives_ok(
    $$SELECT count(*) FROM claimius.search(
        'a0000000-0000-4000-8000-0000000000fa', (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_foreign'),
        'Foreign', NULL::TEXT[], 0, NULL::UUID[], NULL::UUID[], NULL::UUID[], NULL::UUID, NULL::TEXT, 20, 0)$$,
    'search settles a claim bound to a table outside table_info without error'
);

SELECT claimius._raise_claim_diff(
    (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_foreign'),
    (SELECT id FROM claimius.claim WHERE name = 'Disciple writer')
);

SELECT lives_ok(
    $$SELECT count(*) FROM claimius.get_organizations(
        'a0000000-0000-4000-8000-0000000000fa', (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_foreign'), 4)$$,
    'a read settles a raised claim diff over a table outside table_info without error'
);

SELECT is_empty(
    $$SELECT 1 FROM claimius.access_diff
       WHERE kind = 'claim' AND claim_id = (SELECT id FROM claimius.claim WHERE name = 'Disciple writer')$$,
    'the claim diff row clears after the read'
);

SELECT * FROM finish();
ROLLBACK;
