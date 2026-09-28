BEGIN;
SELECT plan(4);

CREATE TABLE public.sqltest_page (
    id            UUID PRIMARY KEY,
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
    v_sys   UUID := '00000000-0000-4000-8000-0000000000d0';
    v_rae   UUID := 'a0000000-0000-4000-8000-0000000000d7';
    v_root  UUID := '10000000-0000-4000-8000-000000000d70';
    v_claim UUID;
    v_bind  RECORD;
BEGIN
    PERFORM claimius.init();
    PERFORM claimius.init_claimius_tables('public.sqltest_page');
    PERFORM claimius.ensure_app_user(p_user_id => v_rae, p_first_name => 'Rae');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Redaction root', 'company', v_root, v_root, 0, v_sys);

    INSERT INTO public.sqltest_page (id, name, sa_owner_id, sa_created_by)
    SELECT ('20000000-0000-4000-8000-000000000d7' || n)::UUID, 'page ' || n, v_root, v_sys
    FROM generate_series(1, 5) n;

    FOR v_bind IN
        SELECT * FROM (VALUES
            ('Read name', 4, 1, '{"name": true}'::JSONB),
            ('Read description', 4, 1, '{"description": true}'::JSONB),
            ('Read name', 4, 2, '{"name": true}'::JSONB),
            ('Read all', 4, 2, NULL),
            ('Write name', 2, 3, '{"name": true}'::JSONB),
            ('Read all', 4, 3, NULL),
            ('Read all', 4, 4, NULL),
            ('Deny read description', 20, 4, '{"description": true}'::JSONB),
            ('Read missing field', 4, 5, '{"nosuch": true}'::JSONB)
        ) b(claim_name, access, page, scope)
    LOOP
        SELECT id INTO v_claim FROM claimius.claim WHERE name = v_bind.claim_name;
        IF v_claim IS NULL THEN
            v_claim := (claimius.create_claim(
                p_name => v_bind.claim_name, p_description => NULL, p_sa_access => v_bind.access,
                p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys
            ) -> 'claim' ->> 'id')::UUID;
            PERFORM claimius.assign_claim_user(p_claim_id => v_claim, p_user_id => v_rae, p_sa_owner_id => v_root, p_sa_created_by => v_sys);
        END IF;
        PERFORM claimius.assign_claim_object(
            p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_sys,
            p_object_id => ('20000000-0000-4000-8000-000000000d7' || v_bind.page)::UUID, p_object_type => 'public.sqltest_page',
            p_scope => v_bind.scope);
    END LOOP;
END $$;

SELECT set_eq(
    $$SELECT p.name, g.sa_access, g.scope
        FROM claimius.get_objects('a0000000-0000-4000-8000-0000000000d7', 'public.sqltest_page') g
        JOIN public.sqltest_page p ON p.id = g.object_id$$,
    $$VALUES ('page 1', 4, '{"name": 4, "description": 4, "*": 0}'::JSONB),
             ('page 2', 4, NULL::JSONB),
             ('page 3', 14, '{"name": 14, "*": 4}'::JSONB),
             ('page 4', 4, '{"description": 0, "*": 4}'::JSONB)$$,
    'user_object.scope maps each named field to its mask with * for the rest, and a field the type lacks grants nothing'
);

SELECT is(
    (SELECT scope FROM claimius.get_access('a0000000-0000-4000-8000-0000000000d7',
        '20000000-0000-4000-8000-000000000d73', 'public.sqltest_page', 4)),
    '{"name": 14, "*": 4}'::JSONB,
    'get_access returns the settled scope of the row'
);

SELECT is(
    (SELECT scope FROM claimius.mutation_access('a0000000-0000-4000-8000-0000000000d7', '20000000-0000-4000-8000-000000000d74', 'public.sqltest_page', 4)),
    '{"description": 0, "*": 4}'::JSONB,
    'mutation_access returns the settled scope, a scoped deny withholding its field'
);

SELECT throws_ok(
    $$SELECT * FROM claimius.mutation_access('a0000000-0000-4000-8000-0000000000d7', '20000000-0000-4000-8000-000000000d75', 'public.sqltest_page', 4)$$,
    'P0403', NULL,
    'a grant scoped only to fields the type lacks leaves the object unreachable'
);

SELECT * FROM finish();
ROLLBACK;
