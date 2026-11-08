-- ============================================================================
-- Claimius V2.3 Access Functions
-- ----------------------------------------------------------------------------
-- The trigger functions that maintain the materialized access state. Three
-- families:
--   calc_object_access        Fires on regular registered tables
--   calc_hierarchical_access  Fires on hierarchical tables (org, location,
--                             parenthood enabled tables)
--   calc_claim_access         Fires on claim, claim_object, user_claim
--   emit_sync_event           Fires on the source tables
--                             to publish into sync_event
--   audit_trigger             Fires on core auditable tables to write audit
--   tg_update_timestamp       Generic sa_updated_at maintenance
-- ============================================================================

-- ----------------------------------------------------------------------------
-- calc_object_access
-- Trigger function for regular registered tables (no self referencing
-- hierarchy). Splices the row into ownership and (optionally) location trees,
-- materializes user_object for the acting user so a writer reads its own work
-- back, and records one access_cause row. A synced row under replay keeps
-- the closure and raises its area but settles no actor. Every other user
-- resolves its own slice on its next read through reconcile_if_pending.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.calc_object_access()
    RETURNS TRIGGER AS $$
DECLARE
    v_object_type   TEXT;
    v_obj_id        UUID;
    v_owner_id      UUID;
    v_location_id   UUID;
    v_actor         UUID;
    v_org_root      UUID;
    v_loc_root      UUID;
    v_owner_level   INTEGER;
    v_loc_level     INTEGER;
    v_op            TEXT;
    v_structural    BOOLEAN := TRUE;
    v_replay        BOOLEAN := claimius.in_replay_mode();
BEGIN
    v_object_type := TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME;
    v_op := TG_OP;

    IF v_op = 'DELETE' THEN
        v_obj_id := OLD.id;
    ELSE
        v_obj_id := NEW.id;
    END IF;

    -- Pull useful fields from the row using JSON conversion (works for any
    -- registered shape).
    IF v_op <> 'DELETE' THEN
        v_owner_id  := (to_jsonb(NEW) ->> 'sa_owner_id')::UUID;
        v_location_id := (to_jsonb(NEW) ->> 'sa_location_id')::UUID;
    END IF;

    -- Tree maintenance: detach on delete or hard delete; attach on insert
    -- or undelete; reattach on owner/location change. The object's own fields
    -- go to object_info, the one row every reader's listing reads them from,
    -- so a write that moves nothing access can follow, a rename included,
    -- refreshes what the acting user sees and leaves every other user
    -- untouched.
    v_structural := claimius._retree_object(v_object_type, v_obj_id, to_jsonb(OLD), to_jsonb(NEW));
    IF v_op <> 'UPDATE' OR EXISTS (
        SELECT 1 FROM unnest(ARRAY['name', 'description', 'sa_owner_id', 'sa_location_id', 'sa_root_id', 'sa_deleted_at']) AS k
        WHERE (to_jsonb(OLD) ->> k) IS DISTINCT FROM (to_jsonb(NEW) ->> k)
    ) THEN
        PERFORM claimius._write_object_info(v_object_type, ARRAY[v_obj_id]);
    END IF;

    -- The acting user materializes immediately so a writer reads its own work
    -- back within the same request.
    IF NOT v_replay THEN
        v_actor := claimius._actor_user_for_row(v_object_type, coalesce(NEW, OLD));
        IF v_actor IS NOT NULL THEN
            PERFORM claimius.recompute_user_object(v_actor, v_object_type, v_obj_id);
        END IF;
    END IF;

    IF v_structural THEN
        PERFORM claimius._raise_object_diff(v_object_type, v_obj_id);
    END IF;

    RETURN coalesce(NEW, OLD);
EXCEPTION WHEN OTHERS THEN
    PERFORM claimius.write_audit(
            v_op, 'error', v_object_type, v_obj_id,
            coalesce(v_owner_id, '00000000-0000-0000-0000-000000000000'::UUID),
            coalesce((to_jsonb(coalesce(NEW, OLD)) ->> 'sa_created_by')::UUID, '00000000-0000-0000-0000-000000000000'::UUID),
            SQLERRM
            );
    RAISE;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.calc_object_access() IS 'Trigger on regular registered tables. Maintains tree memberships and user_object.';

-- _attach_object_to_trees
-- Splices a non hierarchical row into its ownership and (optionally)
-- location trees by inserting closure edges: a self-edge for the row plus
-- one edge per ancestor of the parent at depth + 1. A parent outside the
-- closure leaves the row out, as a rebuild would. An organization is a node
-- of its own ownership tree, so only its location pin attaches here.
DROP FUNCTION IF EXISTS claimius._attach_object_to_trees(TEXT, UUID, RECORD);
CREATE OR REPLACE FUNCTION claimius._attach_object_to_trees(
    p_object_type TEXT,
    p_object_id   UUID,
    p_row         JSONB
) RETURNS VOID AS $$
DECLARE
    v_owner_id  UUID;
    v_loc_id    UUID;
    v_owner_root UUID;
    v_loc_root  UUID;
BEGIN
    v_owner_id := (p_row ->> 'sa_owner_id')::UUID;
    v_loc_id   := (p_row ->> 'sa_location_id')::UUID;

    IF v_owner_id IS NOT NULL AND p_object_type <> 'claimius.organization' THEN
        SELECT e.root_id INTO v_owner_root
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'ownership'
          AND e.descendant_type = 'claimius.organization'
          AND e.descendant_id = v_owner_id
          AND e.depth = 0;
        IF v_owner_root IS NOT NULL THEN
            INSERT INTO claimius.inheritance_info (
                tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
            ) VALUES (
                'ownership', v_owner_root, p_object_type, p_object_id, p_object_type, p_object_id, 0
            ) ON CONFLICT DO NOTHING;
            INSERT INTO claimius.inheritance_info (
                tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
            )
            SELECT 'ownership', v_owner_root, e.ancestor_type, e.ancestor_id, p_object_type, p_object_id, e.depth + 1
            FROM claimius.inheritance_info e
            WHERE e.tree_type = 'ownership'
              AND e.descendant_type = 'claimius.organization'
              AND e.descendant_id   = v_owner_id
            ON CONFLICT DO NOTHING;
        END IF;
    END IF;

    IF v_loc_id IS NOT NULL THEN
        SELECT e.root_id INTO v_loc_root
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'location'
          AND e.descendant_type = 'claimius.location'
          AND e.descendant_id = v_loc_id
          AND e.depth = 0;
        IF v_loc_root IS NOT NULL THEN
            INSERT INTO claimius.inheritance_info (
                tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
            ) VALUES (
                'location', v_loc_root, p_object_type, p_object_id, p_object_type, p_object_id, 0
            ) ON CONFLICT DO NOTHING;
            INSERT INTO claimius.inheritance_info (
                tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
            )
            SELECT 'location', v_loc_root, e.ancestor_type, e.ancestor_id, p_object_type, p_object_id, e.depth + 1
            FROM claimius.inheritance_info e
            WHERE e.tree_type = 'location'
              AND e.descendant_type = 'claimius.location'
              AND e.descendant_id   = v_loc_id
            ON CONFLICT DO NOTHING;
        END IF;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- _detach_object_from_trees
-- Removes a non hierarchical row from its ownership and location trees by
-- deleting every closure edge where this row appears as descendant. Since
-- the row is non hierarchical it has no descendants of its own. For an
-- organization only its location pin detaches.
DROP FUNCTION IF EXISTS claimius._detach_object_from_trees(TEXT, UUID, RECORD);
CREATE OR REPLACE FUNCTION claimius._detach_object_from_trees(
    p_object_type TEXT,
    p_object_id   UUID
) RETURNS VOID AS $$
BEGIN
    DELETE FROM claimius.inheritance_info
    WHERE tree_type IN ('ownership', 'location')
      AND (tree_type = 'location' OR p_object_type <> 'claimius.organization')
      AND descendant_type = p_object_type
      AND descendant_id   = p_object_id;
END;
$$ LANGUAGE plpgsql;

-- _retree_object
-- Keeps the ownership and location closure rows of one non hierarchical row,
-- or of the pins of a hierarchical one, in step with a write: detach on
-- delete, attach on insert or restore, both on an owner or location change.
-- Returns whether the write moved anything access can follow.
CREATE OR REPLACE FUNCTION claimius._retree_object(
    p_object_type TEXT,
    p_object_id   UUID,
    p_old         JSONB,
    p_new         JSONB
) RETURNS BOOLEAN AS $$
DECLARE
    v_was_active BOOLEAN := p_old IS NOT NULL AND (p_old ->> 'sa_deleted_at') IS NULL;
    v_is_active  BOOLEAN := p_new IS NOT NULL AND (p_new ->> 'sa_deleted_at') IS NULL;
    v_moved      BOOLEAN := (p_old ->> 'sa_owner_id') IS DISTINCT FROM (p_new ->> 'sa_owner_id')
                            OR (p_old ->> 'sa_location_id') IS DISTINCT FROM (p_new ->> 'sa_location_id');
BEGIN
    IF v_was_active AND (NOT v_is_active OR v_moved) THEN
        PERFORM claimius._detach_object_from_trees(p_object_type, p_object_id);
    END IF;
    IF v_is_active AND (NOT v_was_active OR v_moved) THEN
        PERFORM claimius._attach_object_to_trees(p_object_type, p_object_id, p_new);
    END IF;
    RETURN NOT (v_was_active AND v_is_active AND NOT v_moved);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._retree_object(TEXT, UUID, JSONB, JSONB) IS 'Moves one row''s ownership and location closure rows with a write. True when the write is structural.';

-- ----------------------------------------------------------------------------
-- calc_object_trees
-- Trigger for tables whose rows sit in the ownership closure but are not
-- access targets of their own: user_claim and claim_object. Keeps
-- their closure rows and nothing else.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.calc_object_trees()
    RETURNS TRIGGER AS $$
BEGIN
    PERFORM claimius._retree_object(
            TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME, (coalesce(NEW, OLD)).id, to_jsonb(OLD), to_jsonb(NEW));
    RETURN coalesce(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.calc_object_trees() IS 'Trigger on owned tables outside the access targets. Maintains their closure rows.';

-- ----------------------------------------------------------------------------
-- calc_hierarchical_access
-- Trigger for hierarchical tables (organization, location, parenthood
-- enabled tables). Maintains the table's own tree incrementally plus its
-- membership in ownership and location trees. An insert writes the node's
-- d + 1 rows, a reparent splices its subtree, a delete drops its subtree, and
-- a restore rebuilds only the restored subtree. Disallows cross root
-- reparenting outside migrate_root.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.calc_hierarchical_access()
    RETURNS TRIGGER AS $$
DECLARE
    v_object_type   TEXT;
    v_obj_id        UUID;
    v_root_id       UUID;
    v_old_root_id   UUID;
    v_op            TEXT;
    v_was_active    BOOLEAN := FALSE;
    v_is_active     BOOLEAN := FALSE;
    v_tree          claimius.tree_type;
    v_parent_col    TEXT := 'sa_parent_id';
    v_old_parent    UUID;
    v_new_parent    UUID;
    v_actor         UUID;
    v_replay        BOOLEAN := claimius.in_replay_mode();
    v_renamed       BOOLEAN := FALSE;
BEGIN
    v_object_type := TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME;
    v_op := TG_OP;

    IF v_op = 'DELETE' THEN
        v_obj_id := OLD.id;
    ELSE
        v_obj_id := NEW.id;
    END IF;

    IF v_object_type = 'claimius.organization' THEN
        v_tree := 'ownership';
        v_parent_col := 'sa_owner_id';
    ELSIF v_object_type = 'claimius.location' THEN
        v_tree := 'location';
    ELSE
        v_tree := 'parenthood';
    END IF;

    IF v_op <> 'DELETE' THEN
        v_root_id   := (to_jsonb(NEW) ->> 'sa_root_id')::UUID;
        v_new_parent := (to_jsonb(NEW) ->> v_parent_col)::UUID;
        v_is_active := (to_jsonb(NEW) ->> 'sa_deleted_at') IS NULL;
    END IF;
    IF v_op <> 'INSERT' THEN
        v_old_root_id := (to_jsonb(OLD) ->> 'sa_root_id')::UUID;
        v_old_parent := (to_jsonb(OLD) ->> v_parent_col)::UUID;
        v_was_active := (to_jsonb(OLD) ->> 'sa_deleted_at') IS NULL;
    END IF;

    -- Cross root reparenting is not allowed
    IF v_op = 'UPDATE' AND v_old_root_id IS DISTINCT FROM v_root_id AND NOT v_replay
        AND current_setting('claimius.migrating_root', TRUE) IS DISTINCT FROM 'on' THEN
        RAISE EXCEPTION 'Cross root reparenting is not allowed. Use migrate_root() if absolutely required.';
    END IF;

    PERFORM pg_advisory_xact_lock(hashtext(coalesce(v_root_id, v_old_root_id)::TEXT));

    IF NOT v_is_active THEN
        PERFORM claimius._splice_subtree(v_tree, v_object_type, v_obj_id, v_old_root_id, NULL);
    ELSIF v_op = 'INSERT' THEN
        INSERT INTO claimius.inheritance_info (
            tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
        ) VALUES (
            v_tree, v_root_id, v_object_type, v_obj_id, v_object_type, v_obj_id, 0
        );
        IF v_new_parent <> v_obj_id THEN
            PERFORM claimius._splice_subtree(v_tree, v_object_type, v_obj_id, v_root_id, v_new_parent);
        END IF;
    ELSIF NOT v_was_active OR v_old_root_id IS DISTINCT FROM v_root_id OR NOT EXISTS (
        SELECT 1 FROM claimius.inheritance_info e
        WHERE e.tree_type = v_tree
          AND e.descendant_type = v_object_type
          AND e.descendant_id = v_obj_id
          AND e.depth = 0
    ) THEN
        IF v_tree = 'ownership' THEN
            PERFORM claimius.build_ownership_tree(v_root_id, v_obj_id);
        ELSIF v_tree = 'location' THEN
            PERFORM claimius.build_location_tree(v_root_id, v_obj_id);
        ELSE
            PERFORM claimius.build_parenthood_tree(v_object_type, v_root_id, v_obj_id);
        END IF;
    ELSIF v_old_parent IS DISTINCT FROM v_new_parent THEN
        PERFORM claimius._splice_subtree(v_tree, v_object_type, v_obj_id, v_root_id, v_new_parent);
    END IF;

    IF v_object_type = 'claimius.location' THEN
        -- a location's owner can also place it under an ownership tree
        PERFORM claimius._reattach_location_in_ownership(v_obj_id, NEW, OLD, v_op);
    ELSE
        -- Organizations pin into the location tree, and external parenthood
        -- tables that also carry sa_owner_id / sa_location_id enter the
        -- ownership and location closures on attach so cascade through
        -- location and owner trees reaches them.
        PERFORM claimius._retree_object(v_object_type, v_obj_id, to_jsonb(OLD), to_jsonb(NEW));
    END IF;

    -- The object's own fields go to object_info. A write that moves nothing
    -- but the name and description is a rename: every reader's listing reads
    -- it there, so it raises no area.
    IF v_op <> 'UPDATE' OR EXISTS (
        SELECT 1 FROM unnest(ARRAY['name', 'description', 'sa_owner_id', 'sa_location_id', 'sa_root_id', 'sa_deleted_at']) AS k
        WHERE (to_jsonb(OLD) ->> k) IS DISTINCT FROM (to_jsonb(NEW) ->> k)
    ) THEN
        PERFORM claimius._write_object_info(v_object_type, ARRAY[v_obj_id]);
    END IF;
    v_renamed := v_op = 'UPDATE'
                 AND to_jsonb(OLD) - ARRAY['name', 'description', 'sa_updated_at'] = to_jsonb(NEW) - ARRAY['name', 'description', 'sa_updated_at'];

    -- The acting user materializes immediately so a writer reads its own work
    -- back within the same request. A synced row under replay has no local
    -- actor, so only its area is raised, one diff for the row and its subtree.
    IF NOT v_replay THEN
        v_actor := claimius._actor_user_for_row(v_object_type, coalesce(NEW, OLD));
        IF v_actor IS NOT NULL THEN
            PERFORM claimius.recompute_user_object(v_actor, v_object_type, v_obj_id);
        END IF;
    END IF;

    IF NOT v_renamed THEN
        PERFORM claimius._raise_object_diff(v_object_type, v_obj_id, TRUE);
    END IF;

    RETURN coalesce(NEW, OLD);
EXCEPTION WHEN OTHERS THEN
    PERFORM claimius.write_audit(
            v_op, 'error', v_object_type, v_obj_id,
            coalesce((to_jsonb(coalesce(NEW, OLD)) ->> 'sa_owner_id')::UUID, '00000000-0000-0000-0000-000000000000'::UUID),
            coalesce((to_jsonb(coalesce(NEW, OLD)) ->> 'sa_created_by')::UUID, '00000000-0000-0000-0000-000000000000'::UUID),
            SQLERRM
            );
    RAISE;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.calc_hierarchical_access() IS 'Trigger on hierarchical tables. Maintains the closure incrementally and reconciles users.';

-- _reattach_location_in_ownership
-- A location is also a node in the ownership tree of its sa_owner_id
-- organization. When the owner changes, or the location is deleted or
-- restored, drop the location's edges from the ownership tree and insert
-- fresh edges under the live owner. Closure operations only.
CREATE OR REPLACE FUNCTION claimius._reattach_location_in_ownership(
    p_obj_id UUID, p_new RECORD, p_old RECORD, p_op TEXT
) RETURNS VOID AS $$
DECLARE
    v_old_owner UUID;
    v_new_owner UUID;
    v_was_in    BOOLEAN := FALSE;
    v_is_in     BOOLEAN := FALSE;
    v_new_root  UUID;
BEGIN
    IF p_op <> 'INSERT' THEN
        v_old_owner := (to_jsonb(p_old) ->> 'sa_owner_id')::UUID;
        v_was_in := (to_jsonb(p_old) ->> 'sa_deleted_at') IS NULL;
    END IF;
    IF p_op <> 'DELETE' THEN
        v_new_owner := (to_jsonb(p_new) ->> 'sa_owner_id')::UUID;
        v_is_in := (to_jsonb(p_new) ->> 'sa_deleted_at') IS NULL;
    END IF;

    IF v_was_in AND v_is_in AND v_old_owner IS NOT DISTINCT FROM v_new_owner THEN
        RETURN;
    END IF;

    DELETE FROM claimius.inheritance_info
    WHERE tree_type = 'ownership'
      AND descendant_type = 'claimius.location'
      AND descendant_id   = p_obj_id;

    IF v_is_in AND v_new_owner IS NOT NULL THEN
        SELECT e.root_id INTO v_new_root
        FROM claimius.inheritance_info e
        WHERE e.tree_type = 'ownership'
          AND e.descendant_type = 'claimius.organization'
          AND e.descendant_id = v_new_owner
          AND e.depth = 0;
        IF v_new_root IS NOT NULL THEN
            INSERT INTO claimius.inheritance_info (
                tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
            ) VALUES (
                'ownership', v_new_root, 'claimius.location', p_obj_id, 'claimius.location', p_obj_id, 0
            );
            INSERT INTO claimius.inheritance_info (
                tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id, depth
            )
            SELECT 'ownership', v_new_root, e.ancestor_type, e.ancestor_id, 'claimius.location', p_obj_id, e.depth + 1
            FROM claimius.inheritance_info e
            WHERE e.tree_type = 'ownership'
              AND e.descendant_type = 'claimius.organization'
              AND e.descendant_id   = v_new_owner;
        END IF;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- ----------------------------------------------------------------------------
-- calc_claim_access
-- Trigger for claim, claim_object, user_claim. When any of these change,
-- find the affected (user, object) pairs and recompute user_object for
-- each.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.calc_claim_access()
    RETURNS TRIGGER AS $$
DECLARE
    v_table       TEXT;
    v_claim_id    UUID;
    v_actor       UUID;
BEGIN
    IF claimius.in_replay_mode() THEN
        RETURN coalesce(NEW, OLD);
    END IF;

    v_table := TG_TABLE_NAME;

    -- Every shape resolves to the claim whose area moved. claim_object carries
    -- it directly, user_claim carries the assignment and moves the area of
    -- its one user only, and a composition link moves the linked claim as
    -- well as its own.
    IF v_table = 'claim' THEN
        v_claim_id := coalesce(NEW.id, OLD.id);
        PERFORM claimius._raise_claim_diff(v_claim_id);
    ELSIF v_table = 'user_claim' THEN
        v_claim_id := coalesce(NEW.claim_id, OLD.claim_id);
        PERFORM claimius._raise_claim_diff(v_claim_id, coalesce(NEW.user_id, OLD.user_id));
    ELSE
        v_claim_id := coalesce(NEW.claim_id, OLD.claim_id);
        PERFORM claimius._raise_claim_diff(v_claim_id);
        IF coalesce(NEW.object_type, OLD.object_type) = 'claimius.claim' THEN
            PERFORM claimius._raise_claim_diff(coalesce(NEW.object_id, OLD.object_id));
        END IF;
    END IF;

    -- The acting user settles its pairs on the objects the row names, the
    -- claim and a binding's object, so the request reads them back. Every
    -- other pair, the assignee's included, settles when its user reads.
    v_actor := claimius._actor_user_for_row('claimius.' || v_table, coalesce(NEW, OLD));
    IF v_actor IS NOT NULL THEN
        PERFORM claimius.recompute_user_object(v_actor, 'claimius.claim', v_claim_id);
        IF v_table = 'claim_object' THEN
            PERFORM claimius.recompute_user_object(
                    v_actor, coalesce(NEW.object_type, OLD.object_type), coalesce(NEW.object_id, OLD.object_id));
        END IF;
    END IF;

    RETURN coalesce(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.calc_claim_access() IS 'Trigger on claim, claim_object, user_claim. Reconciles affected users including composition link fan out.';

-- ----------------------------------------------------------------------------
-- _check_claim_composition
-- BEFORE INSERT/UPDATE trigger on claim_object. Enforces composition link
-- invariants: no self loop, no reverse link cycle, caller has read on
-- target claim. Bypassed during disciple replay.
-- The first binding on a samna_user that a claim of the binding's owning
-- organization is assigned to may come from an actor holding owner on that
-- organization. That is how create_organization_user binds a new user's
-- virtual claim.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius._check_claim_composition()
    RETURNS TRIGGER AS $$
DECLARE
    v_user_id      UUID;
    v_has_access   BOOLEAN;
    v_actor_access INTEGER;
    v_effective    INTEGER;
BEGIN
    IF claimius.in_replay_mode() THEN
        RETURN NEW;
    END IF;

    -- A binding may never hand out more than the actor already holds on the
    -- object. What it hands out is its effective mask, the claim mask narrowed
    -- by the binding mask, so a NULL binding mask is judged as the full claim
    -- mask. Denies are restrictive, so they are exempt, and so is a removal.
    -- No computed access counts as none held, so an actor who cannot reach an
    -- object cannot grant it away. Creating an object records the creator,
    -- which computes their access before any binding is written.
    SELECT claimius._effective_access(c.sa_access, NEW.sa_access) INTO v_effective
    FROM claimius.claim c
    WHERE c.id = NEW.claim_id;

    IF NEW.object_id IS NOT NULL
        AND NEW.object_type IS DISTINCT FROM 'claimius.claim'
        AND NEW.sa_deleted_at IS NULL
        AND (coalesce(v_effective, 0) & 16) = 0
        AND (TG_OP = 'INSERT'
             OR NEW.sa_access IS DISTINCT FROM OLD.sa_access
             OR NEW.claim_id IS DISTINCT FROM OLD.claim_id
             OR NEW.object_id IS DISTINCT FROM OLD.object_id)
    THEN
        SELECT uc.user_id INTO v_user_id
        FROM claimius.user_claim uc
        WHERE uc.id = NEW.sa_created_by
          AND uc.sa_deleted_at IS NULL;

        IF v_user_id IS NOT NULL THEN
            PERFORM claimius.reconcile_if_pending(v_user_id, NEW.object_type, NEW.object_id);

            SELECT max(sa_access) INTO v_actor_access
            FROM claimius.user_object
            WHERE user_id     = v_user_id
              AND object_type = NEW.object_type
              AND object_id   = NEW.object_id;

            IF (coalesce(v_effective, 0) & ~coalesce(v_actor_access, 0) & 15) <> 0
                AND NEW.object_type = 'claimius.samna_user'
                AND NOT EXISTS (
                    SELECT 1 FROM claimius.claim_object co
                    WHERE co.object_type = 'claimius.samna_user'
                      AND co.object_id = NEW.object_id
                      AND co.sa_deleted_at IS NULL)
                AND EXISTS (
                    SELECT 1 FROM claimius.user_claim m
                    WHERE m.user_id = NEW.object_id
                      AND m.sa_owner_id = NEW.sa_owner_id
                      AND m.sa_deleted_at IS NULL)
            THEN
                PERFORM claimius.reconcile_if_pending(v_user_id, 'claimius.organization', NEW.sa_owner_id);

                IF EXISTS (
                    SELECT 1 FROM claimius.user_object
                    WHERE user_id     = v_user_id
                      AND object_type = 'claimius.organization'
                      AND object_id   = NEW.sa_owner_id
                      AND (sa_access & 1) <> 0)
                THEN
                    v_actor_access := 15;
                END IF;
            END IF;

            IF (coalesce(v_effective, 0) & ~coalesce(v_actor_access, 0) & 15) <> 0 THEN
                RAISE EXCEPTION 'cannot grant more access than held: user % holds % on % %, requested %',
                    v_user_id, coalesce(v_actor_access, 0), NEW.object_type, NEW.object_id, v_effective;
            END IF;
        END IF;
    END IF;

    IF NEW.object_type IS DISTINCT FROM 'claimius.claim' OR NEW.object_id IS NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.object_id = NEW.claim_id THEN
        RAISE EXCEPTION 'claim composition cycle: claim % cannot point at itself', NEW.claim_id;
    END IF;

    IF EXISTS (
        SELECT 1 FROM claimius.claim_object
        WHERE claim_id      = NEW.object_id
          AND object_type   = 'claimius.claim'
          AND object_id     = NEW.claim_id
          AND TRUE
          AND sa_deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'claim composition cycle: % already points at %', NEW.object_id, NEW.claim_id;
    END IF;

    SELECT uc.user_id INTO v_user_id
    FROM claimius.user_claim uc
    WHERE uc.id = NEW.sa_created_by
      AND uc.sa_deleted_at IS NULL;

    IF v_user_id IS NULL THEN
        RAISE EXCEPTION 'sa_created_by % is not a valid user_claim', NEW.sa_created_by;
    END IF;

    PERFORM claimius.reconcile_if_pending(v_user_id, 'claimius.claim', NEW.object_id);

    SELECT EXISTS (
        SELECT 1 FROM claimius.user_object
        WHERE user_id     = v_user_id
          AND TRUE
          AND object_type = 'claimius.claim'
          AND object_id   = NEW.object_id
          AND (sa_access & 4) <> 0
    ) INTO v_has_access;

    IF NOT v_has_access THEN
        RAISE EXCEPTION 'cannot compose: user % has no read access to claim %', v_user_id, NEW.object_id;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._check_claim_composition() IS 'BEFORE INSERT/UPDATE trigger on claim_object. Rejects a grant whose effective mask hands out more access than the actor holds on the object, denies and removals exempt. The first binding on a samna_user holding a claim assigned under the binding''s owning organization may come from an actor holding owner on that organization. Enforces composition link invariants: no self loop, no reverse link cycle, caller has read on target claim. Bypassed during disciple replay.';

DROP TRIGGER IF EXISTS tg_check_claim_composition ON claimius.claim_object;
CREATE TRIGGER tg_check_claim_composition
    BEFORE INSERT OR UPDATE ON claimius.claim_object
    FOR EACH ROW EXECUTE FUNCTION claimius._check_claim_composition();

-- _cascade_recompute
-- For an inheritable binding to (object_type, object_id), recomputes
-- user_object for every descendant of that node in every tree it appears
-- in, for the given user.
CREATE OR REPLACE FUNCTION claimius._cascade_recompute(
    p_user_id       UUID,
    p_object_type   TEXT,
    p_object_id     UUID
) RETURNS VOID AS $$
DECLARE
    v_descendant RECORD;
BEGIN
    FOR v_descendant IN
        SELECT DISTINCT e.descendant_type AS object_type, e.descendant_id AS object_id
        FROM claimius.inheritance_info e
        WHERE e.ancestor_type = p_object_type
          AND e.ancestor_id   = p_object_id
        LOOP
            PERFORM claimius.recompute_user_object(p_user_id, v_descendant.object_type, v_descendant.object_id);
        END LOOP;
END;
$$ LANGUAGE plpgsql;

-- ----------------------------------------------------------------------------
-- emit_sync_event
-- Trigger function that publishes changes to the source tables into
-- sync_event. Disciples consume from sync_event by seq and derive user_object
-- and user_users themselves, so the derived tables never publish.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.emit_sync_event()
    RETURNS TRIGGER AS $$
DECLARE
    v_op    claimius.sync_operation;
    v_type  TEXT;
    v_payload JSONB;
BEGIN
    IF claimius.in_replay_mode() THEN
        RETURN coalesce(NEW, OLD);
    END IF;

    -- sync_event is an outbox. A deployment with no registered consumer in
    -- prophet_state publishes to nobody, so the row would never be read and
    -- never be trimmed. A pure disciple is exactly that case.
    IF NOT EXISTS (SELECT 1 FROM claimius.prophet_state) THEN
        RETURN coalesce(NEW, OLD);
    END IF;

    v_type := TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME;

    IF TG_OP = 'DELETE' THEN
        v_op := 'delete';
        v_payload := jsonb_build_object('id', (to_jsonb(OLD) ->> 'id'));
    ELSE
        v_op := 'upsert';
        v_payload := to_jsonb(NEW) - 'txid';
    END IF;

    INSERT INTO claimius.sync_event (operation, event_type, payload)
    VALUES (v_op, v_type, v_payload);

    RETURN coalesce(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.emit_sync_event() IS 'Trigger on the source tables. Publishes into sync_event.';

-- ----------------------------------------------------------------------------
-- Read time self heal
-- ----------------------------------------------------------------------------

-- reconcile_if_pending
-- Settles outstanding access areas inline, at the top of every get_* function.
-- Two independent sources feed it.
--
-- The diff. access_diff holds the areas a write marked instead of recomputing,
-- and each user settles its own part of them when it reads.
--
-- Called with a target, p_object_type and p_object_id, the read is a check
-- and settles the one pair it asks about: once, when an area still open for
-- the reader covers it. The areas that cover a pair are the object areas of
-- the object and, with cascade, of its ancestors, and the claim areas of the
-- claims the reader holds, links to or was assigned, that are bound on the
-- object or an ancestor or that the pair's grants name. No area is marked
-- settled, since the rest of the reader's share is still open.
--
-- Called without one, the read is a listing and settles the reader's share of
-- the areas it needs: the object areas of the type it lists, with
-- p_object_type, or that cascade over that type, every object area without
-- one, and the claim areas of the reader's claims. It takes every one of
-- them, oldest first, however many are open, so a listing never reads a pair
-- an open area still covers. Each share is set based, one recompute per
-- object type, and is recorded in access_diff_user at the area's
-- raise_count, so the reader never settles the same raise twice. The area row
-- is cleared once every holder has settled it. A reader locks the areas it
-- takes FOR KEY SHARE SKIP LOCKED, which writers raising the area again do
-- not wait on, and its whole share with one transaction advisory lock, so two
-- reads of one user never settle it at once; a read that finds it held
-- settles nothing. A batch big enough to spend the per object lock budget
-- takes every object at once instead, so however many areas are open the
-- read holds a bounded number of locks.
-- Every lock the settle waits for is bounded by _settle_lock_timeout: a
-- unit that waits longer is given up, stays open, and the read takes its
-- pairs as they stand, so a read never holds a writer into a deadlock.
--
-- The retry queue. reconcile_queue holds sync event applications that failed.
-- A check drains the pair it asks about, a listing every pair of the reader.
CREATE OR REPLACE FUNCTION claimius.reconcile_if_pending(
    p_user_id       UUID,
    p_object_type   TEXT DEFAULT NULL,
    p_object_id     UUID DEFAULT NULL
)
    RETURNS VOID AS $$
DECLARE
    v_lock_timeout TEXT := current_setting('lock_timeout');
    v_row       RECORD;
    v_open      BOOLEAN;
    v_areas     UUID[];
    v_at        INTEGER;
    v_batch     INTEGER := claimius._object_lock_budget() / 4;
BEGIN
    IF p_user_id IS NULL THEN
        RETURN;
    END IF;
    PERFORM set_config('lock_timeout', claimius._settle_lock_timeout(), TRUE);

    IF p_object_type IS NOT NULL AND p_object_id IS NOT NULL THEN
        BEGIN
            DELETE FROM claimius.reconcile_queue
            WHERE id IN (
                SELECT q.id FROM claimius.reconcile_queue q
                WHERE q.user_id = p_user_id
                  AND q.object_type = p_object_type
                  AND q.object_id = p_object_id
                FOR UPDATE SKIP LOCKED
            );
            v_open := FOUND;

            IF NOT v_open AND (
                EXISTS (SELECT 1 FROM claimius.access_diff d WHERE d.kind = 'object')
                OR EXISTS (SELECT 1 FROM claimius.access_diff d WHERE d.kind = 'claim')
            ) THEN
                WITH nodes AS MATERIALIZED (
                    SELECT p_object_type AS object_type, p_object_id AS object_id, TRUE AS self
                    UNION
                    SELECT e.ancestor_type, e.ancestor_id, FALSE
                    FROM claimius.inheritance_info e
                    WHERE e.descendant_type = p_object_type
                      AND e.descendant_id = p_object_id
                      AND e.depth > 0
                ),
                held AS MATERIALIZED (
                    SELECT uc.claim_id
                    FROM claimius.user_claim uc
                    WHERE uc.user_id = p_user_id
                      AND uc.sa_deleted_at IS NULL
                    UNION
                    SELECT link.object_id
                    FROM claimius.user_claim uc
                        JOIN claimius.claim_object link
                            ON link.claim_id = uc.claim_id
                           AND link.object_type = 'claimius.claim'
                           AND link.sa_deleted_at IS NULL
                    WHERE uc.user_id = p_user_id
                      AND uc.sa_deleted_at IS NULL
                ),
                claim_areas AS (
                    SELECT d.* FROM held h
                        JOIN claimius.access_diff d
                            ON d.kind = 'claim'
                           AND d.claim_id = h.claim_id
                           AND d.user_id IS NULL
                    UNION ALL
                    SELECT d.* FROM claimius.access_diff d
                    WHERE d.kind = 'claim'
                      AND d.user_id = p_user_id
                )
                SELECT EXISTS (
                        SELECT 1
                        FROM nodes n
                            JOIN claimius.access_diff d
                                ON d.kind = 'object'
                               AND d.object_type = n.object_type
                               AND d.object_id = n.object_id
                        WHERE (n.self OR d.cascade)
                          AND NOT EXISTS (
                              SELECT 1 FROM claimius.access_diff_user du
                              WHERE du.diff_id = d.id AND du.user_id = p_user_id AND du.raise_count = d.raise_count))
                    OR EXISTS (
                        SELECT 1
                        FROM claim_areas d
                        WHERE NOT EXISTS (
                                SELECT 1 FROM claimius.access_diff_user du
                                WHERE du.diff_id = d.id AND du.user_id = p_user_id AND du.raise_count = d.raise_count)
                          AND (
                              EXISTS (
                                  SELECT 1
                                  FROM nodes n
                                      JOIN claimius.claim_object co
                                          ON co.object_type = n.object_type
                                         AND co.object_id = n.object_id
                                  WHERE co.claim_id = d.claim_id
                                     OR co.claim_id IN (
                                         SELECT link.object_id FROM claimius.claim_object link
                                         WHERE link.claim_id = d.claim_id
                                           AND link.object_type = 'claimius.claim'))
                              OR EXISTS (
                                  SELECT 1 FROM claimius.user_object uo
                                  WHERE uo.user_id = p_user_id AND uo.object_type = p_object_type AND uo.object_id = p_object_id
                                    AND (uo.grants @> jsonb_build_array(jsonb_build_object('claim_id', d.claim_id))
                                         OR uo.grants @> jsonb_build_array(jsonb_build_object('composed_from', d.claim_id))))
                              OR (p_object_type = 'claimius.claim' AND p_object_id = d.claim_id))
                    )
                INTO v_open;
            END IF;

            IF v_open THEN
                PERFORM claimius.recompute_user_object(p_user_id, p_object_type, p_object_id);
            END IF;
        EXCEPTION WHEN OTHERS THEN
            -- leave the pair outstanding for the next reader
            NULL;
        END;
    ELSIF pg_try_advisory_xact_lock(hashtextextended('claimius.share:' || p_user_id::TEXT, 0)) THEN
        BEGIN
            v_areas := ARRAY(
                SELECT d.id FROM claimius.access_diff d
                WHERE (
                          (d.kind = 'object' AND (
                              p_object_type IS NULL
                              OR d.object_type = p_object_type
                              OR (d.cascade AND EXISTS (
                                  SELECT 1 FROM claimius.inheritance_info e
                                  WHERE e.ancestor_type = d.object_type
                                    AND e.ancestor_id = d.object_id
                                    AND e.descendant_type = p_object_type
                                    AND e.depth > 0))))
                          OR d.user_id = p_user_id
                          OR (d.user_id IS NULL AND EXISTS (
                              SELECT 1 FROM claimius.user_claim uc
                              WHERE uc.user_id = p_user_id
                                AND uc.sa_deleted_at IS NULL
                                AND (
                                    uc.claim_id = d.claim_id
                                    OR EXISTS (
                                        SELECT 1 FROM claimius.claim_object link
                                        WHERE link.claim_id = uc.claim_id
                                          AND link.object_type = 'claimius.claim'
                                          AND link.object_id = d.claim_id
                                          AND link.sa_deleted_at IS NULL
                                    )
                                )
                          ))
                      )
                  AND NOT EXISTS (
                      SELECT 1 FROM claimius.access_diff_user du
                      WHERE du.diff_id = d.id AND du.user_id = p_user_id AND du.raise_count = d.raise_count)
                ORDER BY d.sa_created_at, d.id
                FOR KEY SHARE OF d SKIP LOCKED);
        EXCEPTION WHEN lock_not_available THEN
            v_areas := '{}';
        END;

        IF cardinality(v_areas) * 4 > claimius._object_lock_budget() THEN
            BEGIN
                PERFORM claimius._lock_all_objects();
            EXCEPTION WHEN lock_not_available THEN
                NULL;
            END;
        END IF;
        FOR v_at IN 1 .. coalesce(cardinality(v_areas), 0) BY v_batch LOOP
            PERFORM claimius._settle_diff_share(p_user_id, v_areas[v_at : v_at + v_batch - 1]);
        END LOOP;

        FOR v_row IN
            SELECT id, object_id, object_type FROM claimius.reconcile_queue
            WHERE user_id = p_user_id
                FOR UPDATE SKIP LOCKED
            LOOP
                BEGIN
                    PERFORM claimius.recompute_user_object(p_user_id, v_row.object_type, v_row.object_id);
                    DELETE FROM claimius.reconcile_queue WHERE id = v_row.id;
                EXCEPTION WHEN OTHERS THEN
                    -- leave it queued for the next attempt
                    NULL;
                END;
            END LOOP;
    END IF;
    PERFORM set_config('lock_timeout', v_lock_timeout, TRUE);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.reconcile_if_pending(UUID, TEXT, UUID) IS 'Settles what a read meets and drains reconcile_queue. With a target it recomputes the one pair when an open area covers it; without one it settles the reader''s share of every open area it needs, of p_object_type when given, set based, under one share lock and a bounded number of object locks, and records it in access_diff_user. Called at read entry points.';

-- drain_access_diff
-- Settles, outside any request, the shares of open areas whose holders are
-- likelier to read before the area is raised again than after. A holder j
-- reads at the rate 1 / (now - last_used_at) of its user_activity row, an
-- area a is raised at raise_count / (now - sa_created_at), and the share is
-- worth settling ahead when the first is at least the second. A share that
-- fails the test, or whose holder has never used the app, waits for the
-- holder's own read. Shares are taken most recent user first, at most
-- p_limit_pairs of them, each through _settle_diff_share, so a share a read is
-- settling at the same moment is skipped. A drain holds the share locks of at
-- most 64 users and stops once its per object locks near _object_lock_budget,
-- leaving the rest to the next drain. The sync loop calls it between
-- pulls, on the one process holding the sync lock. Returns the shares settled.
CREATE OR REPLACE FUNCTION claimius.drain_access_diff(
    p_limit_pairs   INTEGER DEFAULT 1000
) RETURNS INTEGER AS $$
DECLARE
    v_lock_timeout TEXT := current_setting('lock_timeout');
    v_share     RECORD;
    v_settled   INTEGER := 0;
    v_users     UUID[] := '{}';
BEGIN
    PERFORM set_config('lock_timeout', claimius._settle_lock_timeout(), TRUE);
    BEGIN
        FOR v_share IN
            SELECT s.user_id, s.diff_id
            FROM (
                SELECT DISTINCT h.user_id, d.id AS diff_id, a.last_used_at
                FROM claimius.access_diff d
                    CROSS JOIN LATERAL claimius._diff_holders(ARRAY[d.id]) h
                    JOIN claimius.user_activity a ON a.user_id = h.user_id
                WHERE extract(epoch FROM now() - d.sa_created_at) >= d.raise_count * extract(epoch FROM now() - a.last_used_at)
                  AND NOT EXISTS (
                      SELECT 1 FROM claimius.access_diff_user du
                      WHERE du.diff_id = d.id AND du.user_id = h.user_id AND du.raise_count = d.raise_count)
            ) s
            ORDER BY s.last_used_at DESC, s.diff_id, s.user_id
            LIMIT greatest(p_limit_pairs, 0)
        LOOP
            EXIT WHEN coalesce(nullif(current_setting('claimius.object_locks', TRUE), ''), '0')::INTEGER * 2 >= claimius._object_lock_budget();
            IF NOT v_share.user_id = ANY(v_users) THEN
                CONTINUE WHEN cardinality(v_users) >= 64;
                v_users := v_users || v_share.user_id;
            END IF;
            IF claimius._settle_diff_share(v_share.user_id, ARRAY[v_share.diff_id]) > 0 THEN
                v_settled := v_settled + 1;
            END IF;
        END LOOP;
    EXCEPTION WHEN lock_not_available THEN
        NULL;
    END;

    PERFORM set_config('lock_timeout', v_lock_timeout, TRUE);
    RETURN v_settled;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.drain_access_diff(INTEGER) IS 'Settles up to p_limit_pairs holder shares of open access_diff areas off the request path, only where 1 / (now - user_activity.last_used_at) >= raise_count / (now - sa_created_at), most recent user first. Returns the shares settled.';

-- cascade_user_soft_delete
-- When read functions detect a soft deleted user, propagate the soft delete
-- to their user_claim, user_relation, and user_field rows.
CREATE OR REPLACE FUNCTION claimius.cascade_user_soft_delete(p_user_id UUID)
    RETURNS VOID AS $$
BEGIN
    UPDATE claimius.user_claim
    SET sa_deleted_at = now()
    WHERE user_id = p_user_id AND TRUE AND sa_deleted_at IS NULL;

    UPDATE claimius.user_relation
    SET sa_deleted_at = now()
    WHERE user_id = p_user_id AND TRUE AND sa_deleted_at IS NULL;

    UPDATE claimius.user_field
    SET sa_deleted_at = now()
    WHERE user_id = p_user_id AND TRUE AND sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.cascade_user_soft_delete(p_user_id uuid) IS 'Propagates soft delete from samna_user to dependent rows.';

-- check_user_active
-- Pure predicate. Returns true when the user row exists, has status=active,
-- and is not soft deleted. Called at the top of get_* functions; must not
-- perform writes because every read would trigger them under load. Cascade of
-- soft delete to user_claim / user_relation / user_field is owned by the
-- trigger tg_cascade_user_soft_delete attached in V3.0__triggers.sql.
CREATE OR REPLACE FUNCTION claimius.check_user_active(p_user_id UUID)
    RETURNS BOOLEAN AS $$
DECLARE
    v_status        claimius.user_status;
    v_deleted       TIMESTAMPTZ;
BEGIN
    SELECT status, sa_deleted_at INTO v_status, v_deleted
    FROM claimius.samna_user
    WHERE user_id = p_user_id AND TRUE;

    IF v_status IS NULL THEN
        RETURN FALSE;
    END IF;

    IF v_status <> 'active' OR v_deleted IS NOT NULL THEN
        RETURN FALSE;
    END IF;

    RETURN TRUE;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.check_user_active(p_user_id uuid) IS 'Predicate: returns true when the user row exists, has status=active, and is not soft deleted.';

-- _cascade_user_soft_delete_trg
-- Trigger wrapper that fires cascade_user_soft_delete exactly once on the
-- transition where samna_user.sa_deleted_at flips from NULL to NOT NULL.
-- Attached as tg_cascade_user_soft_delete on claimius.samna_user in
-- V3.0__triggers.sql. Cascade out of the write path keeps read functions free
-- of side effects.
CREATE OR REPLACE FUNCTION claimius._cascade_user_soft_delete_trg()
    RETURNS TRIGGER AS $$
BEGIN
    IF OLD.sa_deleted_at IS NULL AND NEW.sa_deleted_at IS NOT NULL THEN
        PERFORM claimius.cascade_user_soft_delete(NEW.user_id);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._cascade_user_soft_delete_trg() IS 'Trigger wrapper: fires cascade_user_soft_delete once on samna_user soft delete transition.';

-- ----------------------------------------------------------------------------
-- Self root triggers
-- BEFORE INSERT triggers that fill self referencing parent and root columns.
--   Root row (parent column NULL or = NEW.id): set parent column and
--   sa_root_id to NEW.id.
--   Child row (parent column set, sa_root_id NULL): copy sa_root_id from
--   the parent row.
-- Three variants by hierarchy: organization (sa_owner_id), location
-- (sa_parent_id, lookup in same table), parenthood (sa_parent_id, lookup in
-- the firing table via TG_TABLE_*).
-- ----------------------------------------------------------------------------

-- For organizations. Parent column is sa_owner_id; lookup target is
-- claimius.organization.
CREATE OR REPLACE FUNCTION claimius._set_self_owner_on_insert()
    RETURNS TRIGGER AS $$
DECLARE
    v_root UUID;
    v_zero CONSTANT UUID := '00000000-0000-0000-0000-000000000000'::uuid;
BEGIN
    IF NEW.sa_root_id  = v_zero THEN NEW.sa_root_id  := NULL; END IF;
    IF NEW.sa_owner_id = v_zero THEN NEW.sa_owner_id := NULL; END IF;

    IF (NEW.private_seed IS NULL OR NEW.private_seed = '')
       AND NEW.sa_owner_id IS NOT NULL AND NEW.sa_owner_id <> NEW.id THEN
        NEW.private_seed := coalesce((
            SELECT o.private_seed
            FROM claimius.organization o
            WHERE o.id = NEW.sa_owner_id
        ), '');
    END IF;

    -- Caller already provided sa_root_id: trust it.
    IF NEW.sa_root_id IS NOT NULL THEN
        RETURN NEW;
    END IF;

    -- New root: self reference both columns.
    IF NEW.sa_owner_id IS NULL OR NEW.sa_owner_id = NEW.id THEN
        NEW.sa_owner_id := NEW.id;
        NEW.sa_root_id  := NEW.id;
        RETURN NEW;
    END IF;

    -- Child: copy parent org's sa_root_id.
    SELECT sa_root_id INTO v_root
    FROM claimius.organization
    WHERE id = NEW.sa_owner_id;

    NEW.sa_root_id := coalesce(v_root, NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._set_self_owner_on_insert() IS 'BEFORE INSERT for organizations. Self references on root creation; copies parent org sa_root_id otherwise.';

-- For locations.
-- Parent column is sa_parent_id (locations belong to a location tree).
-- Lookup target is claimius.location (parent is in the same table).
-- sa_owner_id is required and not auto filled.
CREATE OR REPLACE FUNCTION claimius._set_self_root_on_insert()
    RETURNS TRIGGER AS $$
DECLARE
    v_root UUID;
    v_zero CONSTANT UUID := '00000000-0000-0000-0000-000000000000'::uuid;
BEGIN
    IF NEW.sa_root_id   = v_zero THEN NEW.sa_root_id   := NULL; END IF;
    IF NEW.sa_parent_id = v_zero THEN NEW.sa_parent_id := NULL; END IF;

    IF NEW.sa_root_id IS NOT NULL THEN
        RETURN NEW;
    END IF;

    -- New root: parent self references, sa_root_id self references.
    IF NEW.sa_parent_id IS NULL OR NEW.sa_parent_id = NEW.id THEN
        NEW.sa_parent_id := NEW.id;
        NEW.sa_root_id   := NEW.id;
        RETURN NEW;
    END IF;

    -- Child: copy parent location's sa_root_id.
    SELECT sa_root_id INTO v_root
    FROM claimius.location
    WHERE id = NEW.sa_parent_id;

    NEW.sa_root_id := coalesce(v_root, NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._set_self_root_on_insert() IS 'BEFORE INSERT for locations. Self references on root creation; copies parent location sa_root_id otherwise.';

-- For parenthood tables. Parent column is sa_parent_id; lookup target is
-- the same table the trigger fires on (TG_TABLE_*).
CREATE OR REPLACE FUNCTION claimius._set_self_parent_on_insert()
    RETURNS TRIGGER AS $$
DECLARE
    v_root  UUID;
    v_sql   TEXT;
    v_zero  CONSTANT UUID := '00000000-0000-0000-0000-000000000000'::uuid;
BEGIN
    IF NEW.sa_root_id   = v_zero THEN NEW.sa_root_id   := NULL; END IF;
    IF NEW.sa_parent_id = v_zero THEN NEW.sa_parent_id := NULL; END IF;

    IF NEW.sa_root_id IS NOT NULL THEN
        RETURN NEW;
    END IF;

    -- New root.
    IF NEW.sa_parent_id IS NULL OR NEW.sa_parent_id = NEW.id THEN
        NEW.sa_parent_id := NEW.id;
        NEW.sa_root_id   := NEW.id;
        RETURN NEW;
    END IF;

    -- Child: dynamic lookup against the same table.
    v_sql := format('SELECT sa_root_id FROM %I.%I WHERE id = $1', TG_TABLE_SCHEMA, TG_TABLE_NAME);
    EXECUTE v_sql INTO v_root USING NEW.sa_parent_id;

    NEW.sa_root_id := coalesce(v_root, NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._set_self_parent_on_insert() IS 'BEFORE INSERT for parenthood tables. Self references on root creation; copies parent sa_root_id from the same table otherwise.';
-- recompute_external_access
-- Materializes user_object for one user across every registered external
-- table. The prophet cannot compute these rows because the tables exist only
-- on the disciple, so sync calls this after applying a claim, claim_object or
-- user_claim payload. One set based recompute per table over its live rows
-- and the pairs the user already holds on it, so a pair whose row went is
-- purged in the same call; user_users follows through _count_user_users.
CREATE OR REPLACE FUNCTION claimius.recompute_external_access(
    p_user_id UUID
) RETURNS INTEGER AS $$
DECLARE
    v_table RECORD;
    v_ids   UUID[];
    v_count INTEGER := 0;
BEGIN
    FOR v_table IN
        SELECT object_type, schema_name, table_name, has_sa_deleted_at
        FROM claimius.table_info
        WHERE is_internal = FALSE
        LOOP
            EXECUTE format(
                'SELECT ARRAY(SELECT id FROM %I.%I%s UNION SELECT object_id FROM claimius.user_object WHERE user_id = $1 AND object_type = $2)',
                v_table.schema_name,
                v_table.table_name,
                CASE WHEN v_table.has_sa_deleted_at THEN ' WHERE sa_deleted_at IS NULL' ELSE '' END
            ) INTO v_ids USING p_user_id, v_table.object_type;

            IF cardinality(v_ids) > 0 THEN
                PERFORM claimius.recompute_user_object(p_user_id, v_table.object_type, VARIADIC v_ids);
                v_count := v_count + cardinality(v_ids);
            END IF;
        END LOOP;

    RETURN v_count;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.recompute_external_access(p_user_id uuid) IS 'Rebuilds user_object for one user over every external registered table, one set based recompute per table. Called by disciple sync after a claim binding arrives, since the prophet holds no external tables.';
