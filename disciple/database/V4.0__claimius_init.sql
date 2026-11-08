DO $$
    BEGIN
        PERFORM claimius.upsert_internal_table_info();
        PERFORM claimius._restamp_created_by();
        RAISE NOTICE 'Claimius internal tables registered. Schema ready.';
        RAISE NOTICE 'Implementer must now call: upsert_disciple_state';
        RAISE NOTICE 'Then: upsert_table_info(VARIADIC <schema.table list>)';
    END $$;

DO $$
    DECLARE
        r RECORD;
    BEGIN
        FOR r IN
            SELECT o.id, o.sa_root_id
            FROM claimius.organization o
            WHERE o.sa_deleted_at IS NULL
              AND NOT EXISTS (SELECT 1 FROM claimius.organization p WHERE p.id = o.sa_owner_id AND p.id <> o.id)
              AND NOT EXISTS (
                SELECT 1 FROM claimius.inheritance_info e
                WHERE e.tree_type = 'ownership'
                  AND e.descendant_type = 'claimius.organization'
                  AND e.descendant_id = o.id
                  AND e.depth = 0
              )
        LOOP
            PERFORM claimius.build_ownership_tree(r.sa_root_id, r.id);
        END LOOP;

        FOR r IN
            SELECT l.id, l.sa_root_id
            FROM claimius.location l
            WHERE l.sa_deleted_at IS NULL
              AND NOT EXISTS (SELECT 1 FROM claimius.location p WHERE p.id = l.sa_parent_id AND p.id <> l.id)
              AND NOT EXISTS (
                SELECT 1 FROM claimius.inheritance_info e
                WHERE e.tree_type = 'location'
                  AND e.descendant_type = 'claimius.location'
                  AND e.descendant_id = l.id
                  AND e.depth = 0
              )
        LOOP
            PERFORM claimius.build_location_tree(r.sa_root_id, r.id);
        END LOOP;
    END $$;
