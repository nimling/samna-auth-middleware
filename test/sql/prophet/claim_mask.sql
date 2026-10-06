BEGIN;
SELECT plan(13);

DO $$
DECLARE
    v_app    UUID;
    v_sys    UUID := '00000000-0000-4000-8000-0000000000c0';
    v_wes    UUID := 'a0000000-0000-4000-8000-0000000000c1';
    v_uma    UUID := 'a0000000-0000-4000-8000-0000000000c2';
    v_root   UUID := '10000000-0000-4000-8000-000000000c01';
    v_child  UUID := '10000000-0000-4000-8000-000000000c02';
    v_narrow UUID;
    v_reader UUID;
    v_mia    UUID := 'a0000000-0000-4000-8000-0000000000c3';
    v_member UUID;
BEGIN
    v_app := (claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_mask',
        p_system_app_name         => 'SQL mask test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    ) -> 'samna_app' ->> 'id')::UUID;

    PERFORM claimius.upsert_app_user(p_user_id => v_wes, p_app_id => v_app, p_first_name => 'Wes');
    PERFORM claimius.upsert_app_user(p_user_id => v_uma, p_app_id => v_app, p_first_name => 'Uma');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Mask root', 'company', v_root, v_root, 0, v_sys);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_child, 'Mask child', 'company', v_root, v_root, 1, v_sys);

    v_narrow := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Narrowed', p_description => NULL, p_sa_access => 2,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_narrow, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_sa_access => 4, p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_narrow, p_user_id => v_uma, p_sa_owner_id => v_root, p_sa_created_by => v_sys);

    v_reader := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_reader, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_reader, p_user_id => v_wes, p_sa_owner_id => v_root, p_sa_created_by => v_sys);

    PERFORM claimius.insert_claim(
        p_app_id => v_app, p_name => 'Editor', p_description => NULL, p_sa_access => 2,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys);

    PERFORM claimius.upsert_app_user(p_user_id => v_mia, p_app_id => v_app, p_first_name => 'Mia');
    v_member := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Member owner', p_description => NULL, p_sa_access => 33,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_member, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
        p_object_id => v_child, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_member, p_user_id => v_mia, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
END $$;

SELECT throws_ok(
    $$SELECT claimius.insert_claim(
        p_app_id => (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_mask'), p_name => 'Deny only', p_description => NULL,
        p_sa_access => 16, p_sa_owner_id => '10000000-0000-4000-8000-000000000c01', p_sa_root_id => '10000000-0000-4000-8000-000000000c01',
        p_sa_created_by => '00000000-0000-4000-8000-0000000000c0')$$,
    '22023', NULL,
    'insert_claim refuses a mask carrying only the deny bit'
);

SELECT throws_ok(
    $$SELECT claimius.insert_claim(
        p_app_id => (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_mask'), p_name => 'Nothing', p_description => NULL,
        p_sa_access => 0, p_sa_owner_id => '10000000-0000-4000-8000-000000000c01', p_sa_root_id => '10000000-0000-4000-8000-000000000c01',
        p_sa_created_by => '00000000-0000-4000-8000-0000000000c0')$$,
    '22023', NULL,
    'insert_claim refuses an empty mask'
);

SELECT throws_ok(
    $$SELECT claimius.update_claim(p_claim_id => (SELECT id FROM claimius.claim WHERE name = 'Narrowed'), p_sa_access => 16)$$,
    '22023', NULL,
    'update_claim refuses a mask carrying only the deny bit'
);

SELECT is(
    ARRAY(
        SELECT (claimius.insert_claim(
            p_app_id => (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_mask'), p_name => 'Expanded ' || m, p_description => NULL,
            p_sa_access => m, p_sa_owner_id => '10000000-0000-4000-8000-000000000c01', p_sa_root_id => '10000000-0000-4000-8000-000000000c01',
            p_sa_created_by => '00000000-0000-4000-8000-0000000000c0') -> 'claim' ->> 'sa_access')::INTEGER
        FROM unnest(ARRAY[1, 2, 8, 4, 32, 33, 17, 18, 20, 24]) WITH ORDINALITY u(m, i)
        ORDER BY i
    ),
    ARRAY[15, 14, 12, 4, 32, 47, 17, 18, 20, 24],
    'insert_claim expands grant masks and stores deny masks verbatim'
);

SELECT is(
    ARRAY[claimius._effective_access(14, 4), claimius._effective_access(4, 15), claimius._effective_access(15, NULL),
          claimius._effective_access(15, 2), claimius._effective_access(18, NULL), claimius._effective_access(18, 20)],
    ARRAY[4, 4, 15, 14, 18, 16],
    'the effective mask is claim & expand(coalesce(binding, claim)) and a binding never widens its claim'
);

SELECT is(
    ARRAY[claimius._close_access(13), claimius._close_access(11), claimius._close_access(43), claimius._close_access(7), claimius._close_access(14)],
    ARRAY[12, 0, 32, 4, 14],
    'closing a mask drops every bit whose implied bits are gone'
);

SELECT is(
    (SELECT g.sa_access FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000c2', (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_mask'), 'claimius.organization') g
      WHERE g.object_id = '10000000-0000-4000-8000-000000000c01'),
    4,
    'a claim of 14 bound with mask 4 settles 4, not 14'
);

SELECT is(
    (SELECT g.sa_access FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000c2', (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_mask'), 'claimius.organization') g
      WHERE g.object_id = '10000000-0000-4000-8000-000000000c02'),
    4,
    'the narrowed mask is what cascades'
);

SELECT is(
    (SELECT sa_access FROM claimius.get_direct_access('a0000000-0000-4000-8000-0000000000c2', (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_mask'),
        '10000000-0000-4000-8000-000000000c02', 'claimius.organization', 0, FALSE)),
    4,
    'get_direct_access reports the narrowed mask'
);

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_direct_access('a0000000-0000-4000-8000-0000000000c2', (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_mask'),
        '10000000-0000-4000-8000-000000000c02', 'claimius.organization', 2, FALSE)$$,
    'get_direct_access does not grant write through a binding narrowed to read'
);

SELECT throws_ok(
    $$SELECT claimius.insert_claim_object(
        p_app_id => (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_mask'),
        p_claim_id => (SELECT id FROM claimius.claim WHERE name = 'Editor'),
        p_sa_owner_id => '10000000-0000-4000-8000-000000000c01', p_sa_root_id => '10000000-0000-4000-8000-000000000c01',
        p_sa_created_by => (SELECT uc.id FROM claimius.user_claim uc JOIN claimius.claim c ON c.id = uc.claim_id
                             WHERE c.name = 'Reader' AND uc.user_id = 'a0000000-0000-4000-8000-0000000000c1'),
        p_object_id => '10000000-0000-4000-8000-000000000c02', p_object_type => 'claimius.organization')$$,
    'P0403', NULL,
    'a NULL binding mask is judged as the full claim mask against the writer access'
);

SELECT lives_ok(
    $$SELECT claimius.insert_claim_object(
        p_app_id => (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_mask'),
        p_claim_id => (SELECT id FROM claimius.claim WHERE name = 'Editor'),
        p_sa_owner_id => '10000000-0000-4000-8000-000000000c01', p_sa_root_id => '10000000-0000-4000-8000-000000000c01',
        p_sa_created_by => (SELECT uc.id FROM claimius.user_claim uc JOIN claimius.claim c ON c.id = uc.claim_id
                             WHERE c.name = 'Reader' AND uc.user_id = 'a0000000-0000-4000-8000-0000000000c1'),
        p_object_id => '10000000-0000-4000-8000-000000000c02', p_object_type => 'claimius.organization', p_sa_access => 4)$$,
    'a binding narrowed to what the writer holds is accepted'
);

SELECT is(
    (SELECT g.sa_access FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000c3', (SELECT id FROM claimius.samna_app WHERE slug = 'sqltest_mask'), 'claimius.organization') g
      WHERE g.object_id = '10000000-0000-4000-8000-000000000c02'),
    47,
    'a grant of owner and member settles both, the member bit standing beside the rest'
);

SELECT * FROM finish();
ROLLBACK;
