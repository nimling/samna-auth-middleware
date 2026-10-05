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
--   notify_sync_event         Fires on sync_event to wake held pulls
--   audit_trigger             Fires on core auditable tables to write audit
--   tg_update_timestamp       Generic sa_updated_at maintenance
-- ============================================================================

-- ----------------------------------------------------------------------------
-- calc_object_access
-- Trigger function for regular registered tables (no self referencing
-- hierarchy). On any change, recomputes user_object for affected users and
-- splices the row into ownership and (optionally) location trees.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.calc_object_access()
    RETURNS TRIGGER AS $$
DECLARE
    v_object_type   TEXT;
    v_obj_id        UUID;
    v_app_id        UUID;
    v_app_ids       UUID[];
    v_owner_id      UUID;
    v_location_id   UUID;
    v_user          RECORD;
    v_org_root      UUID;
    v_loc_root      UUID;
    v_owner_level   INTEGER;
    v_loc_level     INTEGER;
    v_op            TEXT;
    v_actor         UUID;
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
    -- registered shape). App scope comes from the row's app_id column when
    -- the table has it, from app_object links when it does not, otherwise
    -- from claimius.get_app_id().
    IF v_op <> 'DELETE' THEN
        v_app_ids   := ARRAY(SELECT claimius._app_ids_for_row(v_object_type, to_jsonb(NEW)));
        v_owner_id  := (to_jsonb(NEW) ->> 'sa_owner_id')::UUID;
        v_location_id := (to_jsonb(NEW) ->> 'sa_location_id')::UUID;
    END IF;
    IF v_op <> 'INSERT' THEN
        IF v_op = 'DELETE' THEN
            v_app_ids := ARRAY(SELECT claimius._app_ids_for_row(v_object_type, to_jsonb(OLD)));
        END IF;
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
    -- back within the same request. Every other user settles when it reads.
    -- A row a peer's feed carries under replay has no local actor, so only
    -- its area is raised.
    FOREACH v_app_id IN ARRAY v_app_ids LOOP
        v_actor := CASE WHEN v_replay THEN NULL ELSE claimius._actor_user_for_row(v_app_id, v_object_type, coalesce(NEW, OLD)) END;
        IF v_actor IS NOT NULL THEN
            PERFORM claimius.recompute_user_object(v_app_id, v_actor, v_object_type, v_obj_id);
        END IF;

        IF v_structural THEN
            PERFORM claimius._raise_object_diff(v_app_id, v_object_type, v_obj_id);
        END IF;
    END LOOP;

    RETURN coalesce(NEW, OLD);
EXCEPTION WHEN OTHERS THEN
    PERFORM claimius.insert_audit(
            v_app_id, v_op, 'error', v_object_type, v_obj_id,
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
-- access targets of their own: samna_app, user_claim, claim_object. Keeps
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
    v_app_id        UUID;
    v_app_ids       UUID[];
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

    -- New organizations and locations inherit the app links of their owning
    -- organization so apps linked to a tree see rows added under it.
    IF v_op = 'INSERT' AND NOT v_replay AND v_object_type IN ('claimius.organization', 'claimius.location') THEN
        PERFORM claimius.upsert_app_object(a, v_object_type, v_obj_id, (to_jsonb(NEW) ->> 'sa_created_by')::UUID)
        FROM claimius.apps_for_object('claimius.organization', (to_jsonb(NEW) ->> 'sa_owner_id')::UUID) a
        WHERE (to_jsonb(NEW) ->> 'sa_owner_id')::UUID <> v_obj_id;
    END IF;

    IF v_op <> 'DELETE' THEN
        v_app_ids   := ARRAY(SELECT claimius._app_ids_for_row(v_object_type, to_jsonb(NEW)));
        v_root_id   := (to_jsonb(NEW) ->> 'sa_root_id')::UUID;
        v_new_parent := (to_jsonb(NEW) ->> v_parent_col)::UUID;
        v_is_active := (to_jsonb(NEW) ->> 'sa_deleted_at') IS NULL;
    END IF;
    IF v_op <> 'INSERT' THEN
        v_old_root_id := (to_jsonb(OLD) ->> 'sa_root_id')::UUID;
        v_old_parent := (to_jsonb(OLD) ->> v_parent_col)::UUID;
        v_was_active := (to_jsonb(OLD) ->> 'sa_deleted_at') IS NULL;
        IF v_op = 'DELETE' THEN
            v_app_ids := ARRAY(SELECT claimius._app_ids_for_row(v_object_type, to_jsonb(OLD)));
        END IF;
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
    -- back within the same request. One diff carries the row and its subtree,
    -- and the closure walk runs once, inside the read that claims the area. A
    -- row a peer's feed carries under replay has no local actor, so only its
    -- area is raised.
    FOREACH v_app_id IN ARRAY v_app_ids LOOP
        v_actor := CASE WHEN v_replay THEN NULL ELSE claimius._actor_user_for_row(v_app_id, v_object_type, coalesce(NEW, OLD)) END;
        IF v_actor IS NOT NULL THEN
            PERFORM claimius.recompute_user_object(v_app_id, v_actor, v_object_type, v_obj_id);
        END IF;

        IF NOT v_renamed THEN
            PERFORM claimius._raise_object_diff(v_app_id, v_object_type, v_obj_id, TRUE);
        END IF;
    END LOOP;

    RETURN coalesce(NEW, OLD);
EXCEPTION WHEN OTHERS THEN
    IF coalesce(v_app_id, v_app_ids[1]) IS NOT NULL THEN
        PERFORM claimius.insert_audit(
                coalesce(v_app_id, v_app_ids[1]), v_op, 'error', v_object_type, v_obj_id,
                coalesce((to_jsonb(coalesce(NEW, OLD)) ->> 'sa_owner_id')::UUID, '00000000-0000-0000-0000-000000000000'::UUID),
                coalesce((to_jsonb(coalesce(NEW, OLD)) ->> 'sa_created_by')::UUID, '00000000-0000-0000-0000-000000000000'::UUID),
                SQLERRM
                );
    END IF;
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
    v_app_id      UUID;
    v_claim_id    UUID;
    v_actor       UUID;
BEGIN
    IF claimius.in_replay_mode() THEN
        RETURN coalesce(NEW, OLD);
    END IF;

    v_table  := TG_TABLE_NAME;
    v_app_id := coalesce(NEW.app_id, OLD.app_id);

    -- Every shape resolves to the claim whose area moved. claim_object carries
    -- it directly, user_claim carries the assignment and moves the area of
    -- its one user only, and a composition link moves the linked claim as
    -- well as its own.
    IF v_table = 'claim' THEN
        v_claim_id := coalesce(NEW.id, OLD.id);
        PERFORM claimius._raise_claim_diff(v_app_id, v_claim_id);
    ELSIF v_table = 'user_claim' THEN
        v_claim_id := coalesce(NEW.claim_id, OLD.claim_id);
        PERFORM claimius._raise_claim_diff(v_app_id, v_claim_id, coalesce(NEW.user_id, OLD.user_id));
    ELSE
        v_claim_id := coalesce(NEW.claim_id, OLD.claim_id);
        PERFORM claimius._raise_claim_diff(v_app_id, v_claim_id);
        IF coalesce(NEW.object_type, OLD.object_type) = 'claimius.claim' THEN
            PERFORM claimius._raise_claim_diff(v_app_id, coalesce(NEW.object_id, OLD.object_id));
        END IF;
    END IF;

    -- The acting user settles its pairs on the objects the row names, the
    -- claim and a binding's object, so the request reads them back. Every
    -- other pair, the assignee's included, settles when its user reads.
    v_actor := claimius._actor_user_for_row(v_app_id, 'claimius.' || v_table, coalesce(NEW, OLD));
    IF v_actor IS NOT NULL THEN
        PERFORM claimius.recompute_user_object(v_app_id, v_actor, 'claimius.claim', v_claim_id);
        IF v_table = 'claim_object' THEN
            PERFORM claimius.recompute_user_object(
                    v_app_id, v_actor, coalesce(NEW.object_type, OLD.object_type), coalesce(NEW.object_id, OLD.object_id));
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
-- The first binding of an app that carries no live binding yet may
-- come from an actor of another app who holds owner on the organization
-- owning the samna_app row, judged in the actor's own app, the app of its
-- user_claim. That is how a new app on a prophet receives its first claim.
-- The first binding on a samna_user that a claim of the binding's owning
-- organization is assigned to may come from an actor holding owner on that
-- organization. That is how insert_organization_user binds a new user's
-- virtual claim.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius._check_claim_composition()
    RETURNS TRIGGER AS $$
DECLARE
    v_user_id      UUID;
    v_actor_app_id UUID;
    v_app_owner_id UUID;
    v_has_access   BOOLEAN;
    v_actor_access INTEGER;
    v_effective    INTEGER;
    v_level        INTEGER;
    v_actor_level  INTEGER;
BEGIN
    IF claimius.in_replay_mode() THEN
        RETURN NEW;
    END IF;

    -- A binding may never hand out more than the actor already holds on the
    -- object. What it hands out is its effective mask, the claim mask narrowed
    -- by the binding mask, so a NULL binding mask is judged as the full claim
    -- mask. Denies are exempt from the mask, and so is a removal, but a binding
    -- of a claim above level 0, deny or grant, needs the actor to hold that
    -- level on the object.
    -- No computed access counts as none held, so an actor who cannot reach an
    -- object cannot grant it away. Creating an object records the creator,
    -- which computes their access before any binding is written.
    SELECT claimius._effective_access(c.sa_access, NEW.sa_access), c.level INTO v_effective, v_level
    FROM claimius.claim c
    WHERE c.id = NEW.claim_id;

    IF NEW.object_id IS NOT NULL
        AND NEW.object_type IS DISTINCT FROM 'claimius.claim'
        AND NEW.sa_deleted_at IS NULL
        AND ((coalesce(v_effective, 0) & 16) = 0 OR coalesce(v_level, 0) > 0)
        AND (TG_OP = 'INSERT'
             OR NEW.sa_access IS DISTINCT FROM OLD.sa_access
             OR NEW.claim_id IS DISTINCT FROM OLD.claim_id
             OR NEW.object_id IS DISTINCT FROM OLD.object_id)
    THEN
        SELECT uc.user_id, uc.app_id INTO v_user_id, v_actor_app_id
        FROM claimius.user_claim uc
        WHERE uc.id = NEW.sa_created_by
          AND uc.sa_deleted_at IS NULL;

        IF v_user_id IS NOT NULL THEN
            PERFORM claimius.reconcile_if_pending(NEW.app_id, v_user_id, NEW.object_type, NEW.object_id);

            SELECT max(sa_access) INTO v_actor_access
            FROM claimius.user_object
            WHERE user_id     = v_user_id
              AND app_id      = NEW.app_id
              AND object_type = NEW.object_type
              AND object_id   = NEW.object_id;

            SELECT coalesce(max(coalesce((g ->> 'level')::INTEGER, 0)), 0) INTO v_actor_level
            FROM claimius.user_object uo
                CROSS JOIN LATERAL jsonb_array_elements(uo.grants) g
            WHERE uo.user_id = v_user_id
              AND uo.app_id = NEW.app_id
              AND uo.object_type = NEW.object_type
              AND uo.object_id = NEW.object_id;

            IF (coalesce(v_effective, 0) & ~coalesce(v_actor_access, 0) & 15) <> 0
                AND v_actor_app_id IS DISTINCT FROM NEW.app_id
                AND NOT EXISTS (
                    SELECT 1 FROM claimius.claim_object co
                    WHERE co.app_id = NEW.app_id
                      AND co.sa_deleted_at IS NULL)
            THEN
                SELECT a.sa_owner_id INTO v_app_owner_id
                FROM claimius.samna_app a
                WHERE a.id = NEW.app_id
                  AND a.sa_deleted_at IS NULL;

                IF v_app_owner_id IS NOT NULL THEN
                    PERFORM claimius.reconcile_if_pending(v_actor_app_id, v_user_id, 'claimius.organization', v_app_owner_id);

                    IF EXISTS (
                        SELECT 1 FROM claimius.user_object
                        WHERE user_id     = v_user_id
                          AND app_id      = v_actor_app_id
                          AND object_type = 'claimius.organization'
                          AND object_id   = v_app_owner_id
                          AND (sa_access & 1) <> 0)
                    THEN
                        v_actor_access := 15;
                    END IF;
                END IF;
            END IF;

            IF (coalesce(v_effective, 0) & ~coalesce(v_actor_access, 0) & 15) <> 0
                AND NEW.object_type = 'claimius.samna_user'
                AND NOT EXISTS (
                    SELECT 1 FROM claimius.claim_object co
                    WHERE co.app_id = NEW.app_id
                      AND co.object_type = 'claimius.samna_user'
                      AND co.object_id = NEW.object_id
                      AND co.sa_deleted_at IS NULL)
                AND EXISTS (
                    SELECT 1 FROM claimius.user_claim m
                    WHERE m.app_id = NEW.app_id
                      AND m.user_id = NEW.object_id
                      AND m.sa_owner_id = NEW.sa_owner_id
                      AND m.sa_deleted_at IS NULL)
            THEN
                PERFORM claimius.reconcile_if_pending(NEW.app_id, v_user_id, 'claimius.organization', NEW.sa_owner_id);

                IF EXISTS (
                    SELECT 1 FROM claimius.user_object
                    WHERE user_id     = v_user_id
                      AND app_id      = NEW.app_id
                      AND object_type = 'claimius.organization'
                      AND object_id   = NEW.sa_owner_id
                      AND (sa_access & 1) <> 0)
                THEN
                    v_actor_access := 15;
                END IF;
            END IF;

            IF (coalesce(v_effective, 0) & 16) = 0 AND (coalesce(v_effective, 0) & ~coalesce(v_actor_access, 0) & 15) <> 0 THEN
                RAISE EXCEPTION 'cannot grant more access than held: user % holds % on % %, requested %',
                    v_user_id, coalesce(v_actor_access, 0), NEW.object_type, NEW.object_id, v_effective USING ERRCODE = 'P0403';
            END IF;

            IF coalesce(v_level, 0) > coalesce(v_actor_level, 0) THEN
                RAISE EXCEPTION 'cannot bind a claim of level % above the level % user % holds on % %',
                    v_level, coalesce(v_actor_level, 0), v_user_id, NEW.object_type, NEW.object_id USING ERRCODE = 'P0403';
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
          AND app_id        = NEW.app_id
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

    PERFORM claimius.reconcile_if_pending(NEW.app_id, v_user_id, 'claimius.claim', NEW.object_id);

    SELECT EXISTS (
        SELECT 1 FROM claimius.user_object
        WHERE user_id     = v_user_id
          AND app_id      = NEW.app_id
          AND object_type = 'claimius.claim'
          AND object_id   = NEW.object_id
          AND (sa_access & 4) <> 0
    ) INTO v_has_access;

    IF NOT v_has_access THEN
        RAISE EXCEPTION 'cannot compose: user % has no read access to claim %', v_user_id, NEW.object_id USING ERRCODE = 'P0403';
    END IF;

    SELECT greatest(a.level, t.level) INTO v_level
    FROM claimius.claim a, claimius.claim t
    WHERE a.id = NEW.claim_id
      AND t.id = NEW.object_id;

    IF coalesce(v_level, 0) > 0 THEN
        SELECT coalesce(max(coalesce((g ->> 'level')::INTEGER, 0)), 0) INTO v_actor_level
        FROM claimius.user_object uo
            CROSS JOIN LATERAL jsonb_array_elements(uo.grants) g
        WHERE uo.user_id = v_user_id
              AND uo.app_id = NEW.app_id
          AND uo.object_type = 'claimius.claim'
          AND uo.object_id = NEW.object_id;

        IF v_level > coalesce(v_actor_level, 0) THEN
            RAISE EXCEPTION 'cannot compose: user % holds level % on claim %, the link needs level %',
                v_user_id, coalesce(v_actor_level, 0), NEW.object_id, v_level USING ERRCODE = 'P0403';
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._check_claim_composition() IS 'BEFORE INSERT/UPDATE trigger on claim_object. Rejects a grant whose effective mask hands out more access than the actor holds on the object, denies and removals exempt. The first binding of an app may come from an actor of another app holding owner on the organization that owns the app, judged in the actor''s own app. The first binding on a samna_user holding a claim assigned under the binding''s owning organization may come from an actor holding owner on that organization. Enforces composition link invariants: no self loop, no reverse link cycle, caller has read on target claim. Bypassed during disciple replay.';

DROP TRIGGER IF EXISTS tg_check_claim_composition ON claimius.claim_object;
CREATE TRIGGER tg_check_claim_composition
    BEFORE INSERT OR UPDATE ON claimius.claim_object
    FOR EACH ROW EXECUTE FUNCTION claimius._check_claim_composition();

CREATE OR REPLACE FUNCTION claimius._check_claim_level(
    p_app_id    UUID,
    p_actor_uc  UUID,
    p_claim_id  UUID,
    p_level     INTEGER
) RETURNS VOID AS $$
DECLARE
    v_user_id UUID;
    v_binding RECORD;
    v_deny    INTEGER;
BEGIN
    IF coalesce(p_level, 0) = 0 THEN
        RETURN;
    END IF;

    SELECT uc.user_id INTO v_user_id
    FROM claimius.user_claim uc
    WHERE uc.id = p_actor_uc
      AND uc.app_id = p_app_id
      AND uc.sa_deleted_at IS NULL;
    IF v_user_id IS NULL THEN
        RETURN;
    END IF;

    PERFORM claimius.reconcile_if_pending(p_app_id, v_user_id, 'claimius.claim', p_claim_id);
    IF NOT EXISTS (
        SELECT 1 FROM claimius.user_object
        WHERE user_id = v_user_id
          AND app_id = p_app_id
          AND object_type = 'claimius.claim'
          AND object_id = p_claim_id
          AND (sa_access & 2) <> 0
    ) THEN
        RAISE EXCEPTION 'cannot hand out claim % at level %: user % has no write on it', p_claim_id, p_level, v_user_id USING ERRCODE = 'P0403';
    END IF;

    FOR v_binding IN
        SELECT co.object_type, co.object_id, claimius._effective_access(c.sa_access, co.sa_access) & 15 AS bits
        FROM claimius.claim_object co
            JOIN claimius.claim c ON c.id = co.claim_id
        WHERE co.claim_id = p_claim_id
          AND co.app_id = p_app_id
          AND co.sa_deleted_at IS NULL
          AND co.object_id IS NOT NULL
          AND co.object_type <> 'claimius.claim'
          AND (claimius._effective_access(c.sa_access, co.sa_access) & 16) = 0
    LOOP
        SELECT max(d.level) INTO v_deny
        FROM claimius.user_claim duc
            JOIN claimius.claim d ON d.id = duc.claim_id AND d.sa_deleted_at IS NULL
            JOIN claimius.claim_object dco ON dco.claim_id = d.id AND dco.app_id = p_app_id AND dco.sa_deleted_at IS NULL
        WHERE duc.user_id = v_user_id
          AND duc.app_id = p_app_id
          AND duc.sa_deleted_at IS NULL
          AND (claimius._effective_access(d.sa_access, dco.sa_access) & 16) <> 0
          AND (claimius._effective_access(d.sa_access, dco.sa_access) & v_binding.bits) <> 0
          AND (
              (dco.object_type = v_binding.object_type AND dco.object_id = v_binding.object_id)
              OR (d.inherits AND dco.inherits AND EXISTS (
                  SELECT 1 FROM claimius.inheritance_info a
                  WHERE a.descendant_type = v_binding.object_type
                    AND a.descendant_id = v_binding.object_id
                    AND a.ancestor_type = dco.object_type
                    AND a.ancestor_id = dco.object_id
                    AND a.depth > 0))
          );
        IF v_deny IS NOT NULL AND p_level > v_deny THEN
            RAISE EXCEPTION 'cannot hand out claim % at level %: user % is denied on % % at level %',
                p_claim_id, p_level, v_user_id, v_binding.object_type, v_binding.object_id, v_deny USING ERRCODE = 'P0403';
        END IF;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._check_claim_level(UUID, UUID, UUID, INTEGER) IS 'Refuses to hand out a claim above level 0 that the actor could not: the actor, the user of p_actor_uc in the app, must hold write on the claim, and on no object the claim is bound to may the claim lift a deny of a lower level the actor faces on any of the bits it grants there. Level 0 and an actor that is not a live user_claim of the app pass as before.';

CREATE OR REPLACE FUNCTION claimius._check_claim_grant()
    RETURNS TRIGGER AS $$
BEGIN
    IF claimius.in_replay_mode() OR NEW.sa_deleted_at IS NOT NULL THEN
        RETURN NEW;
    END IF;
    IF TG_TABLE_NAME = 'claim' THEN
        PERFORM claimius._check_claim_level(NEW.app_id, NEW.sa_created_by, NEW.id, NEW.level);
    ELSE
        PERFORM claimius._check_claim_level(NEW.app_id, NEW.sa_created_by, NEW.claim_id,
            (SELECT c.level FROM claimius.claim c WHERE c.id = NEW.claim_id));
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS tg_check_claim_grant ON claimius.user_claim;
CREATE TRIGGER tg_check_claim_grant
    BEFORE INSERT OR UPDATE OF claim_id, user_id ON claimius.user_claim
    FOR EACH ROW EXECUTE FUNCTION claimius._check_claim_grant();

DROP TRIGGER IF EXISTS tg_check_claim_level ON claimius.claim;
CREATE TRIGGER tg_check_claim_level
    BEFORE UPDATE OF level ON claimius.claim
    FOR EACH ROW WHEN (NEW.level > OLD.level) EXECUTE FUNCTION claimius._check_claim_grant();

CREATE OR REPLACE FUNCTION claimius.app_has_scope(
    p_app_id      UUID,
    p_owner_id    UUID,
    p_object_type TEXT DEFAULT NULL,
    p_object_id   UUID DEFAULT NULL
) RETURNS BOOLEAN AS $$
    SELECT claimius.app_has_object(p_app_id, 'claimius.organization', p_owner_id)
       AND (p_object_type IS NULL
            OR p_object_type NOT IN ('claimius.organization', 'claimius.location')
            OR claimius.app_has_object(p_app_id, p_object_type, p_object_id));
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.app_has_scope(UUID, UUID, TEXT, UUID) IS 'True when the owning organization, and the object when it is an organization or a location, are live in the app through app_object, so a row of the app scoped to them reaches the app''s disciples.';

CREATE OR REPLACE FUNCTION claimius._check_app_scope()
    RETURNS TRIGGER AS $$
DECLARE
    v_object_type TEXT;
    v_object_id   UUID;
BEGIN
    IF claimius.in_replay_mode() OR NEW.sa_deleted_at IS NOT NULL THEN
        RETURN NEW;
    END IF;
    IF TG_TABLE_NAME = 'claim_object' THEN
        v_object_type := NEW.object_type;
        v_object_id := NEW.object_id;
    END IF;
    IF TG_OP = 'UPDATE'
        AND NEW.app_id = OLD.app_id
        AND NEW.sa_owner_id = OLD.sa_owner_id
        AND OLD.sa_deleted_at IS NULL
        AND (TG_TABLE_NAME <> 'claim_object'
             OR (v_object_type IS NOT DISTINCT FROM (to_jsonb(OLD) ->> 'object_type')
                 AND v_object_id IS NOT DISTINCT FROM (to_jsonb(OLD) ->> 'object_id')::UUID))
    THEN
        RETURN NEW;
    END IF;
    IF NOT claimius.app_has_object(NEW.app_id, 'claimius.organization', NEW.sa_owner_id) THEN
        RAISE EXCEPTION 'cannot write claimius.% %: owning organization % is not linked to app %',
            TG_TABLE_NAME, NEW.id, NEW.sa_owner_id, NEW.app_id USING ERRCODE = 'P0403';
    END IF;
    IF NOT claimius.app_has_scope(NEW.app_id, NEW.sa_owner_id, v_object_type, v_object_id) THEN
        RAISE EXCEPTION 'cannot write claimius.% %: % % is not linked to app %',
            TG_TABLE_NAME, NEW.id, v_object_type, v_object_id, NEW.app_id USING ERRCODE = 'P0403';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._check_app_scope() IS 'BEFORE INSERT/UPDATE trigger on claim, user_claim and claim_object. Refuses with P0403 a live row whose owning organization, or whose bound organization or location, is not linked to the row''s app, since the row could never reach the app''s disciples. An update that keeps the app, owner and object of a live row passes. Bypassed during replay.';

DROP TRIGGER IF EXISTS tg_check_app_scope ON claimius.claim;
CREATE TRIGGER tg_check_app_scope
    BEFORE INSERT OR UPDATE ON claimius.claim
    FOR EACH ROW EXECUTE FUNCTION claimius._check_app_scope();

DROP TRIGGER IF EXISTS tg_check_app_scope ON claimius.user_claim;
CREATE TRIGGER tg_check_app_scope
    BEFORE INSERT OR UPDATE ON claimius.user_claim
    FOR EACH ROW EXECUTE FUNCTION claimius._check_app_scope();

DROP TRIGGER IF EXISTS tg_check_app_scope ON claimius.claim_object;
CREATE TRIGGER tg_check_app_scope
    BEFORE INSERT OR UPDATE ON claimius.claim_object
    FOR EACH ROW EXECUTE FUNCTION claimius._check_app_scope();

-- _cascade_recompute
-- For an inheritable binding to (object_type, object_id), recomputes
-- user_object for every descendant of that node in every tree it appears
-- in, for the given user.
CREATE OR REPLACE FUNCTION claimius._cascade_recompute(
    p_app_id        UUID,
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
            PERFORM claimius.recompute_user_object(p_app_id, p_user_id, v_descendant.object_type, v_descendant.object_id);
        END LOOP;
END;
$$ LANGUAGE plpgsql;

-- ----------------------------------------------------------------------------
-- emit_sync_event
-- Trigger function that publishes changes to the source tables into
-- sync_event. Disciples consume from sync_event by seq and derive user_object
-- and user_users themselves, so the derived tables never publish.
-- An organization or location carries no app_id, so its change is written
-- once for every app holding a live link to it, stamped with that app.
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
        v_payload := jsonb_build_object('id', (to_jsonb(OLD) ->> 'id'), 'app_id', (to_jsonb(OLD) ->> 'app_id'));
    ELSE
        v_op := 'upsert';
        v_payload := to_jsonb(NEW) - 'txid';
    END IF;

    IF v_type IN ('claimius.organization', 'claimius.location') THEN
        INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
        SELECT v_op, v_type, v_payload, a.app_id
        FROM claimius.apps_for_object(v_type, (v_payload ->> 'id')::UUID) AS a(app_id);
    ELSE
        INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
        VALUES (v_op, v_type, v_payload, (v_payload ->> 'app_id')::uuid);
    END IF;

    RETURN coalesce(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.emit_sync_event() IS 'Trigger on the source tables. Publishes into sync_event. An organization or location change is written once per app linked to it.';

-- ----------------------------------------------------------------------------
-- emit_app_object_sync_event
-- Trigger function on app_object. An organization or location reaches an
-- app's disciple through its link, so a link that goes live publishes the
-- linked row stamped with the app, and a link that ends publishes its delete
-- to that app alone. The disciple carries no app_object; the linked row is
-- the source it derives access from.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.emit_app_object_sync_event()
    RETURNS TRIGGER AS $$
DECLARE
    v_link      claimius.app_object;
    v_was_live  BOOLEAN := TG_OP <> 'INSERT' AND (to_jsonb(OLD) ->> 'sa_deleted_at') IS NULL;
    v_is_live   BOOLEAN := TG_OP <> 'DELETE' AND (to_jsonb(NEW) ->> 'sa_deleted_at') IS NULL;
    v_payload   JSONB;
BEGIN
    IF claimius.in_replay_mode() OR v_was_live = v_is_live THEN
        RETURN coalesce(NEW, OLD);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM claimius.prophet_state) THEN
        RETURN coalesce(NEW, OLD);
    END IF;

    v_link := coalesce(NEW, OLD);

    IF NOT v_is_live THEN
        INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
        VALUES ('delete', v_link.object_type, jsonb_build_object('id', v_link.object_id), v_link.app_id);
        RETURN coalesce(NEW, OLD);
    END IF;

    IF v_link.object_type = 'claimius.organization' THEN
        SELECT to_jsonb(o) INTO v_payload FROM claimius.organization o WHERE o.id = v_link.object_id;
    ELSIF v_link.object_type = 'claimius.location' THEN
        SELECT to_jsonb(l) INTO v_payload FROM claimius.location l WHERE l.id = v_link.object_id;
    END IF;

    IF v_payload IS NOT NULL THEN
        INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
        VALUES ('upsert', v_link.object_type, v_payload, v_link.app_id);
    END IF;

    INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
    SELECT 'upsert', s.event_type, s.payload, v_link.app_id
    FROM (
        SELECT 1 AS step, 'claimius.claim' AS event_type, to_jsonb(c) - 'txid' AS payload, c.sa_created_at AS created
        FROM claimius.claim c
        WHERE c.app_id = v_link.app_id
          AND v_link.object_type = 'claimius.organization'
          AND c.sa_owner_id = v_link.object_id
        UNION ALL
        SELECT 2, 'claimius.user_claim', to_jsonb(uc) - 'txid', uc.sa_created_at
        FROM claimius.user_claim uc
        WHERE uc.app_id = v_link.app_id
          AND v_link.object_type = 'claimius.organization'
          AND uc.sa_owner_id = v_link.object_id
        UNION ALL
        SELECT 3, 'claimius.claim_object', to_jsonb(co) - 'txid', co.sa_created_at
        FROM claimius.claim_object co
        WHERE co.app_id = v_link.app_id
          AND ((v_link.object_type = 'claimius.organization' AND co.sa_owner_id = v_link.object_id)
               OR (co.object_type = v_link.object_type AND co.object_id = v_link.object_id))
    ) s
    ORDER BY s.step, s.created;

    RETURN coalesce(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.emit_app_object_sync_event() IS 'Trigger on app_object. Publishes the linked row to the app on link, followed by the app''s claims, grants and bindings scoped to it, and its delete on unlink.';

-- ----------------------------------------------------------------------------
-- get_app_home
-- The prophet that decides an app's writes when it is not this one: the
-- app's home_prophet, or the peer this prophet offered the app to while the
-- offer stands. NULL when this prophet decides them.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_app_home(p_app_id UUID)
    RETURNS TEXT AS $$
    SELECT coalesce(a.home_prophet, h.to_prophet)
    FROM claimius.samna_app a
        LEFT JOIN claimius.app_handover h
            ON h.app_id = a.id
           AND h.status = 'offered'
           AND a.home_prophet IS NULL
    WHERE a.id = p_app_id;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_app_home(UUID) IS 'Slug of the prophet deciding the app''s writes when it is not this one: its home_prophet, or the target of this prophet''s standing handover offer. NULL when this prophet is the app''s home.';

-- ----------------------------------------------------------------------------
-- enforce_app_home
-- Trigger on the source tables. A row of an app another prophet decides is
-- written here only by the replay of that prophet's feed, so the app keeps a
-- single writer. An organization or location belongs to the apps it is
-- linked to, and is refused when any of them is decided elsewhere.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.enforce_app_home()
    RETURNS TRIGGER AS $$
DECLARE
    v_row   JSONB := to_jsonb(coalesce(NEW, OLD));
    v_type  TEXT := TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME;
    v_home  TEXT;
BEGIN
    IF claimius.in_replay_mode() THEN
        RETURN coalesce(NEW, OLD);
    END IF;

    IF v_row ? 'app_id' THEN
        v_home := claimius.get_app_home((v_row ->> 'app_id')::UUID);
    ELSE
        SELECT claimius.get_app_home(ao.app_id) INTO v_home
        FROM claimius.app_object ao
        WHERE ao.object_type = v_type
          AND ao.object_id = (v_row ->> 'id')::UUID
          AND ao.sa_deleted_at IS NULL
          AND claimius.get_app_home(ao.app_id) IS NOT NULL
        LIMIT 1;
    END IF;

    IF v_home IS NOT NULL THEN
        RAISE EXCEPTION '% % belongs to an app decided by prophet %, its writes go there', v_type, v_row ->> 'id', v_home
            USING ERRCODE = 'P0409', HINT = v_home;
    END IF;

    RETURN coalesce(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.enforce_app_home() IS 'Trigger on the source tables. Refuses, with SQLSTATE P0409 and the home in the hint, a write outside replay to a row of an app another prophet decides.';

-- ----------------------------------------------------------------------------
-- notify_sync_event
-- Statement trigger on sync_event. Notifies claimius_sync once per app the
-- statement wrote events for, with the app id as payload. The notification is
-- delivered at commit, which is what a held pull on the prophet waits for.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.notify_sync_event()
    RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('claimius_sync', w.app_id::TEXT)
    FROM (SELECT DISTINCT app_id FROM written WHERE app_id IS NOT NULL) w;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.notify_sync_event() IS 'Statement trigger on sync_event. Notifies claimius_sync once per app written for, delivered at commit.';

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
-- the areas it needs: the object areas of the app of the type it lists, with
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
    p_app_id        UUID,
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
    IF p_app_id IS NULL OR p_user_id IS NULL THEN
        RETURN;
    END IF;
    PERFORM set_config('lock_timeout', claimius._settle_lock_timeout(), TRUE);

    IF p_object_type IS NOT NULL AND p_object_id IS NOT NULL THEN
        BEGIN
            DELETE FROM claimius.reconcile_queue
            WHERE id IN (
                SELECT q.id FROM claimius.reconcile_queue q
                WHERE q.app_id = p_app_id
                  AND q.user_id = p_user_id
                  AND q.object_type = p_object_type
                  AND q.object_id = p_object_id
                FOR UPDATE SKIP LOCKED
            );
            v_open := FOUND;

            IF NOT v_open AND (
                EXISTS (SELECT 1 FROM claimius.access_diff d WHERE d.app_id = p_app_id AND d.kind = 'object')
                OR EXISTS (SELECT 1 FROM claimius.access_diff d WHERE d.app_id = p_app_id AND d.kind = 'claim')
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
                      AND uc.app_id = p_app_id
                      AND uc.sa_deleted_at IS NULL
                    UNION
                    SELECT link.object_id
                    FROM claimius.user_claim uc
                        JOIN claimius.claim_object link
                            ON link.claim_id = uc.claim_id
                           AND link.app_id = p_app_id
                           AND link.object_type = 'claimius.claim'
                           AND link.sa_deleted_at IS NULL
                    WHERE uc.user_id = p_user_id
                      AND uc.app_id = p_app_id
                      AND uc.sa_deleted_at IS NULL
                ),
                claim_areas AS (
                    SELECT d.* FROM held h
                        JOIN claimius.access_diff d
                            ON d.app_id = p_app_id
                           AND d.kind = 'claim'
                           AND d.claim_id = h.claim_id
                           AND d.user_id IS NULL
                    UNION ALL
                    SELECT d.* FROM claimius.access_diff d
                    WHERE d.app_id = p_app_id
                      AND d.kind = 'claim'
                      AND d.user_id = p_user_id
                )
                SELECT EXISTS (
                        SELECT 1
                        FROM nodes n
                            JOIN claimius.access_diff d
                                ON d.app_id = p_app_id
                               AND d.kind = 'object'
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
                                         AND co.app_id = p_app_id
                                  WHERE co.claim_id = d.claim_id
                                     OR co.claim_id IN (
                                         SELECT link.object_id FROM claimius.claim_object link
                                         WHERE link.claim_id = d.claim_id
                                           AND link.app_id = p_app_id
                                           AND link.object_type = 'claimius.claim'))
                              OR EXISTS (
                                  SELECT 1 FROM claimius.user_object uo
                                  WHERE uo.user_id = p_user_id AND uo.app_id = p_app_id AND uo.object_type = p_object_type AND uo.object_id = p_object_id
                                    AND (uo.grants @> jsonb_build_array(jsonb_build_object('claim_id', d.claim_id))
                                         OR uo.grants @> jsonb_build_array(jsonb_build_object('composed_from', d.claim_id))))
                              OR (p_object_type = 'claimius.claim' AND p_object_id = d.claim_id))
                    )
                INTO v_open;
            END IF;

            IF v_open THEN
                PERFORM claimius.recompute_user_object(p_app_id, p_user_id, p_object_type, p_object_id);
            END IF;
        EXCEPTION WHEN OTHERS THEN
            -- leave the pair outstanding for the next reader
            NULL;
        END;
    ELSIF pg_try_advisory_xact_lock(hashtextextended('claimius.share:' || p_app_id::TEXT || ':' || p_user_id::TEXT, 0)) THEN
        BEGIN
            v_areas := ARRAY(
                SELECT d.id FROM claimius.access_diff d
                WHERE d.app_id = p_app_id
                  AND (
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
                                AND uc.app_id = p_app_id
                                AND uc.sa_deleted_at IS NULL
                                AND (
                                    uc.claim_id = d.claim_id
                                    OR EXISTS (
                                        SELECT 1 FROM claimius.claim_object link
                                        WHERE link.claim_id = uc.claim_id
                                          AND link.app_id = p_app_id
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
            PERFORM claimius._settle_diff_share(p_app_id, p_user_id, v_areas[v_at : v_at + v_batch - 1]);
        END LOOP;

        FOR v_row IN
            SELECT id, object_id, object_type FROM claimius.reconcile_queue
            WHERE app_id = p_app_id AND user_id = p_user_id
                FOR UPDATE SKIP LOCKED
            LOOP
                BEGIN
                    PERFORM claimius.recompute_user_object(p_app_id, p_user_id, v_row.object_type, v_row.object_id);
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

COMMENT ON FUNCTION claimius.reconcile_if_pending(UUID, UUID, TEXT, UUID) IS 'Settles what a read meets and drains reconcile_queue. With a target it recomputes the one pair when an open area covers it; without one it settles the reader''s share of every open area it needs, of p_object_type when given, set based, under one share lock and a bounded number of object locks, and records it in access_diff_user. Called at read entry points.';

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
-- leaving the rest to the next drain. The prophet drains the areas of its
-- own app and of every app no disciple has been in contact with for 7 days;
-- the areas of an app a disciple serves are drained there, so each settle is
-- paid once. Run by a scheduled job. Returns the shares settled.
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
            SELECT s.app_id, s.user_id, s.diff_id
            FROM (
                SELECT DISTINCT d.app_id, h.user_id, d.id AS diff_id, a.last_used_at
                FROM claimius.access_diff d
                    CROSS JOIN LATERAL claimius._diff_holders(d.app_id, ARRAY[d.id]) h
                    JOIN claimius.user_activity a ON a.user_id = h.user_id AND a.app_id = d.app_id
                WHERE (
                        EXISTS (
                            SELECT 1 FROM claimius.prophet_state ps
                            WHERE ps.app_id = d.app_id AND ps.system_app_slug IS NOT NULL)
                        OR NOT EXISTS (
                            SELECT 1 FROM claimius.sync_consumer c
                            WHERE c.app_id = d.app_id AND c.last_contact_at > now() - INTERVAL '7 days'))
                  AND extract(epoch FROM now() - d.sa_created_at) >= d.raise_count * extract(epoch FROM now() - a.last_used_at)
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
            IF claimius._settle_diff_share(v_share.app_id, v_share.user_id, ARRAY[v_share.diff_id]) > 0 THEN
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

COMMENT ON FUNCTION claimius.drain_access_diff(INTEGER) IS 'Settles up to p_limit_pairs holder shares of open access_diff areas off the request path, only where 1 / (now - user_activity.last_used_at) >= raise_count / (now - sa_created_at), most recent user first. On the prophet only the areas of its own app and of apps with no disciple in contact. Returns the shares settled.';

-- cascade_user_soft_delete
-- When read functions detect a soft deleted user, propagate the soft delete
-- to their user_claim, user_relation, and user_field rows.
CREATE OR REPLACE FUNCTION claimius.cascade_user_soft_delete(p_app_id UUID, p_user_id UUID)
    RETURNS VOID AS $$
BEGIN
    UPDATE claimius.user_claim
    SET sa_deleted_at = now()
    WHERE user_id = p_user_id AND app_id = p_app_id AND sa_deleted_at IS NULL;

    UPDATE claimius.user_relation
    SET sa_deleted_at = now()
    WHERE user_id = p_user_id AND app_id = p_app_id AND sa_deleted_at IS NULL;

    UPDATE claimius.user_field
    SET sa_deleted_at = now()
    WHERE user_id = p_user_id AND app_id = p_app_id AND sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.cascade_user_soft_delete(p_app_id uuid, p_user_id uuid) IS 'Propagates soft delete from samna_user to dependent rows.';

-- check_user_active
-- Pure predicate. Returns true when the user row exists, has status=active,
-- and is not soft deleted. Called at the top of get_* functions; must not
-- perform writes because every read would trigger them under load. Cascade of
-- soft delete to user_claim / user_relation / user_field is owned by the
-- trigger tg_cascade_user_soft_delete attached in V3.0__triggers.sql.
CREATE OR REPLACE FUNCTION claimius.check_user_active(p_app_id UUID, p_user_id UUID)
    RETURNS BOOLEAN AS $$
DECLARE
    v_status        claimius.user_status;
    v_deleted       TIMESTAMPTZ;
BEGIN
    SELECT status, sa_deleted_at INTO v_status, v_deleted
    FROM claimius.samna_user
    WHERE user_id = p_user_id AND app_id = p_app_id;

    IF v_status IS NULL THEN
        RETURN FALSE;
    END IF;

    IF v_status <> 'active' OR v_deleted IS NOT NULL THEN
        RETURN FALSE;
    END IF;

    RETURN TRUE;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.check_user_active(p_app_id uuid, p_user_id uuid) IS 'Predicate: returns true when the user row exists, has status=active, and is not soft deleted.';

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
        PERFORM claimius.cascade_user_soft_delete(NEW.app_id, NEW.user_id);
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

    IF NEW.private_seed IS NULL OR NEW.private_seed = '' THEN
        IF NEW.sa_owner_id IS NULL OR NEW.sa_owner_id = NEW.id THEN
            NEW.private_seed := encode(gen_random_bytes(32), 'hex');
        ELSE
            SELECT o.private_seed INTO NEW.private_seed
            FROM claimius.organization o
            WHERE o.id = NEW.sa_owner_id;
            IF NEW.private_seed IS NULL OR NEW.private_seed = '' THEN
                NEW.private_seed := encode(gen_random_bytes(32), 'hex');
            END IF;
        END IF;
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
