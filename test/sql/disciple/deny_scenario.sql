BEGIN;
SELECT plan(16);

CREATE TABLE public.sqltest_doc (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    description   TEXT,
    sa_owner_id   UUID NOT NULL,
    sa_created_by UUID NOT NULL,
    sa_created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_deleted_at TIMESTAMPTZ
);

DO $$
DECLARE
    v_sys       UUID := '00000000-0000-4000-8000-0000000000e0';
    v_ada       UUID := 'a0000000-0000-4000-8000-0000000000e1';
    v_vic       UUID := 'a0000000-0000-4000-8000-0000000000e2';
    v_acme      UUID := '10000000-0000-4000-8000-000000000e01';
    v_sales     UUID := '10000000-0000-4000-8000-000000000e02';
    v_research  UUID := '10000000-0000-4000-8000-000000000e03';
    v_lab       UUID := '10000000-0000-4000-8000-000000000e04';
    v_archive   UUID := '10000000-0000-4000-8000-000000000e05';
    v_admin     UUID;
    v_nowrite   UUID;
    v_share     UUID;
    v_vendor    UUID;
    v_noown     UUID;
    v_ada_admin UUID;
BEGIN
    PERFORM claimius.upsert_disciple_state();
    PERFORM claimius.upsert_table_info('public.sqltest_doc');

    PERFORM claimius.upsert_app_user(p_user_id => v_ada, p_first_name => 'Ada');
    PERFORM claimius.upsert_app_user(p_user_id => v_vic, p_first_name => 'Vic');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_acme, 'Acme', 'company', v_acme, v_acme, 0, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_sales, 'Sales', 'company', v_acme, v_acme, 1, v_sys),
           (v_research, 'Research', 'company', v_acme, v_acme, 1, v_sys);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_lab, 'Lab', 'company', v_research, v_acme, 2, v_sys),
           (v_archive, 'Archive', 'company', v_research, v_acme, 2, v_sys);

    INSERT INTO public.sqltest_doc (id, name, sa_owner_id, sa_created_by)
    SELECT CASE WHEN o.name = 'Lab' AND n = 1 THEN '20000000-0000-4000-8000-000000000e41'::UUID ELSE gen_random_uuid() END,
           o.name || ' doc ' || n, o.id, v_sys
    FROM claimius.organization o
        CROSS JOIN generate_series(1, 2) n
    WHERE o.sa_root_id = v_acme;

    v_admin := (claimius.insert_claim(
        p_name => 'Admin', p_description => NULL, p_sa_access => 1,
        p_sa_owner_id => v_acme, p_sa_root_id => v_acme, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    v_nowrite := (claimius.insert_claim(
        p_name => 'No write on Research', p_description => NULL, p_sa_access => 18,
        p_sa_owner_id => v_acme, p_sa_root_id => v_acme, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    v_share := (claimius.insert_claim(
        p_name => 'Share', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_acme, p_sa_root_id => v_acme, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    v_vendor := (claimius.insert_claim(
        p_name => 'Vendor', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_acme, p_sa_root_id => v_acme, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    v_noown := (claimius.insert_claim(
        p_name => 'No owner on Ada sales doc', p_description => NULL, p_sa_access => 17,
        p_sa_owner_id => v_acme, p_sa_root_id => v_acme, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;

    PERFORM claimius.insert_claim_object(
        p_claim_id => v_admin, p_sa_owner_id => v_acme, p_sa_root_id => v_acme, p_sa_created_by => v_sys,
        p_object_id => v_acme, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_nowrite, p_sa_owner_id => v_acme, p_sa_root_id => v_acme, p_sa_created_by => v_sys,
        p_object_id => v_research, p_object_type => 'claimius.organization', p_inherits => TRUE);
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_share, p_sa_owner_id => v_acme, p_sa_root_id => v_acme, p_sa_created_by => v_sys,
        p_object_id => '20000000-0000-4000-8000-000000000e41', p_object_type => 'public.sqltest_doc', p_inherits => TRUE);

    v_ada_admin := (claimius.insert_user_claim(
        p_claim_id => v_admin, p_user_id => v_ada, p_sa_owner_id => v_acme, p_sa_created_by => v_sys
    ) -> 'user_claim' ->> 'id')::UUID;
    PERFORM claimius.insert_user_claim(p_claim_id => v_nowrite, p_user_id => v_ada, p_sa_owner_id => v_acme, p_sa_created_by => v_sys);
    PERFORM claimius.insert_user_claim(p_claim_id => v_share, p_user_id => v_ada, p_sa_owner_id => v_acme, p_sa_created_by => v_sys);
    PERFORM claimius.insert_user_claim(p_claim_id => v_noown, p_user_id => v_ada, p_sa_owner_id => v_acme, p_sa_created_by => v_sys);

    PERFORM claimius.insert_claim_object(
        p_claim_id => v_vendor, p_sa_owner_id => v_acme, p_sa_root_id => v_acme, p_sa_created_by => v_ada_admin,
        p_object_id => v_admin, p_object_type => 'claimius.claim', p_sa_access => 4, p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_claim_id => v_vendor, p_user_id => v_vic, p_sa_owner_id => v_acme, p_sa_created_by => v_sys);
    PERFORM claimius.insert_user_claim(p_claim_id => v_nowrite, p_user_id => v_vic, p_sa_owner_id => v_acme, p_sa_created_by => v_sys);

    INSERT INTO public.sqltest_doc (id, name, sa_owner_id, sa_created_by)
    VALUES ('20000000-0000-4000-8000-000000000e49', 'Ada lab doc', v_lab, v_ada_admin),
           ('20000000-0000-4000-8000-000000000e29', 'Ada sales doc', v_sales, v_ada_admin);

    PERFORM claimius.insert_claim_object(
        p_claim_id => v_noown, p_sa_owner_id => v_acme, p_sa_root_id => v_acme, p_sa_created_by => v_sys,
        p_object_id => '20000000-0000-4000-8000-000000000e29', p_object_type => 'public.sqltest_doc', p_inherits => TRUE);
END $$;

SELECT set_eq(
    $$SELECT o.name, g.sa_access
        FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000e1', 'claimius.organization') g
        JOIN claimius.organization o ON o.id = g.object_id
       WHERE o.sa_root_id = '10000000-0000-4000-8000-000000000e01'$$,
    $$VALUES ('Acme', 15), ('Sales', 15), ('Research', 12), ('Lab', 12), ('Archive', 12)$$,
    'admin over Acme with write denied on Research settles 15 outside the branch and 12 inside it'
);

SELECT set_eq(
    $$SELECT d.name, g.sa_access
        FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000e1', 'public.sqltest_doc') g
        JOIN public.sqltest_doc d ON d.id = g.object_id$$,
    $$VALUES ('Acme doc 1', 15), ('Acme doc 2', 15), ('Sales doc 1', 15), ('Sales doc 2', 15),
             ('Research doc 1', 12), ('Research doc 2', 12), ('Lab doc 1', 12), ('Lab doc 2', 12),
             ('Archive doc 1', 12), ('Archive doc 2', 12), ('Ada lab doc', 15), ('Ada sales doc', 14)$$,
    'documents follow their organization, the creator keeps 15 under an inherited deny, and a direct owner deny takes the creator grant away'
);

SELECT set_eq(
    $$SELECT o.name, g.sa_access
        FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000e2', 'claimius.organization') g
        JOIN claimius.organization o ON o.id = g.object_id
       WHERE o.sa_root_id = '10000000-0000-4000-8000-000000000e01'$$,
    $$VALUES ('Acme', 4), ('Sales', 4), ('Research', 4), ('Lab', 4), ('Archive', 4)$$,
    'a vendor claim linked to admin with link mask 4 reads every organization admin reaches'
);

SELECT is(
    (SELECT g.sa_access FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000e2', 'claimius.claim') g
      WHERE g.object_id = (SELECT id FROM claimius.claim WHERE name = 'Admin')),
    4,
    'holders of the linking claim read the linked claim at 4'
);

SELECT throws_ok(
    $$SELECT * FROM claimius.mutation_access('a0000000-0000-4000-8000-0000000000e1', '10000000-0000-4000-8000-000000000e03', 'claimius.organization', 2)$$,
    'P0403', NULL,
    'mutation_access for write on the denied branch answers P0403'
);

SELECT is(
    (SELECT sa_access FROM claimius.mutation_access('a0000000-0000-4000-8000-0000000000e1', '10000000-0000-4000-8000-000000000e03', 'claimius.organization', 4)),
    12,
    'mutation_access for read on the denied branch returns admin minus write, 12'
);

SELECT is(
    (SELECT sa_access FROM claimius.get_direct_access('a0000000-0000-4000-8000-0000000000e1',
        '10000000-0000-4000-8000-000000000e03', 'claimius.organization', 0, FALSE)),
    12,
    'get_direct_access applies the deny at required mask 0'
);

SELECT is(
    (SELECT claim_name FROM claimius.get_direct_access('a0000000-0000-4000-8000-0000000000e2',
        '10000000-0000-4000-8000-000000000e02', 'claimius.organization', 4, FALSE)),
    'Vendor',
    'get_direct_access follows a composition link to the linked claim bindings'
);

DO $$ BEGIN PERFORM claimius.update_claim(p_claim_id => (SELECT id FROM claimius.claim WHERE name = 'No write on Research'), p_sa_access => 20); END $$;

SELECT set_eq(
    $$SELECT o.name, g.sa_access
        FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000e1', 'claimius.organization') g
        JOIN claimius.organization o ON o.id = g.object_id
       WHERE o.sa_root_id = '10000000-0000-4000-8000-000000000e01'$$,
    $$VALUES ('Acme', 15), ('Sales', 15)$$,
    'a read deny removes the Research branch from the listing'
);

SELECT set_eq(
    $$SELECT d.name, g.sa_access
        FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000e1', 'public.sqltest_doc') g
        JOIN public.sqltest_doc d ON d.id = g.object_id$$,
    $$VALUES ('Acme doc 1', 15), ('Acme doc 2', 15), ('Sales doc 1', 15), ('Sales doc 2', 15),
             ('Lab doc 1', 4), ('Ada lab doc', 15), ('Ada sales doc', 14)$$,
    'under a read deny the directly shared document keeps 4 and the creator keeps 15'
);

SELECT throws_ok(
    $$SELECT * FROM claimius.mutation_access('a0000000-0000-4000-8000-0000000000e1', '10000000-0000-4000-8000-000000000e04', 'claimius.organization', 4)$$,
    'P0403', NULL,
    'mutation_access answers P0403 inside the read denied branch'
);

SELECT is(
    (SELECT claim_name FROM claimius.get_direct_access('a0000000-0000-4000-8000-0000000000e1',
        '20000000-0000-4000-8000-000000000e41', 'public.sqltest_doc', 4, FALSE)),
    'Share',
    'get_direct_access lets a direct share stand over an ancestor deny'
);

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_direct_access('a0000000-0000-4000-8000-0000000000e1',
        '10000000-0000-4000-8000-000000000e03', 'claimius.organization', 0, FALSE)$$,
    'get_direct_access finds nothing on an object whose every grant is denied'
);

SELECT set_eq(
    $$SELECT o.name, g.sa_access
        FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000e2', 'claimius.organization') g
        JOIN claimius.organization o ON o.id = g.object_id
       WHERE o.sa_root_id = '10000000-0000-4000-8000-000000000e01'$$,
    $$VALUES ('Acme', 4), ('Sales', 4)$$,
    'a composed grant is inherited on the branch, so the read deny removes it'
);

DO $$
DECLARE

    v_acme UUID := '10000000-0000-4000-8000-000000000e01';
    v_sys  UUID := '00000000-0000-4000-8000-0000000000e0';
    v_deny UUID;
BEGIN
    v_deny := (claimius.insert_claim(
        p_name => 'No read on Ada lab doc', p_description => NULL, p_sa_access => 20,
        p_sa_owner_id => v_acme, p_sa_root_id => v_acme, p_sa_created_by => v_sys
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_claim_id => v_deny, p_sa_owner_id => v_acme, p_sa_root_id => v_acme, p_sa_created_by => v_sys,
        p_object_id => '20000000-0000-4000-8000-000000000e49', p_object_type => 'public.sqltest_doc', p_inherits => TRUE);
    PERFORM claimius.insert_user_claim(p_claim_id => v_deny, p_user_id => 'a0000000-0000-4000-8000-0000000000e1',
        p_sa_owner_id => v_acme, p_sa_created_by => v_sys);
END $$;

SELECT is_empty(
    $$SELECT 1 FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000e1', 'public.sqltest_doc') g
       WHERE g.object_id = '20000000-0000-4000-8000-000000000e49'$$,
    'a direct read deny reaches the creator grant and removes the document'
);

SELECT throws_ok(
    $$SELECT * FROM claimius.mutation_access('a0000000-0000-4000-8000-0000000000e1', '20000000-0000-4000-8000-000000000e49', 'public.sqltest_doc', 4)$$,
    'P0403', NULL,
    'mutation_access answers P0403 to the creator under a direct read deny'
);

SELECT * FROM finish();
ROLLBACK;
