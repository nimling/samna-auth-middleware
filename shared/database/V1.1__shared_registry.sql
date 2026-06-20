-- ============================================================================
-- Claimius V1.1 Registry (shared)
-- ----------------------------------------------------------------------------
-- table_info and inheritance_info. Neither carries app_id. Both roles build
-- and read these independently, so they are identical and live in the shared
-- set. Populated by init_claimius_internal and init_claimius_tables.
-- ============================================================================

-- table_info
-- Registry of every table managed by claimius. Populated by
-- init_claimius_internal (for claimius.* tables) and init_claimius_tables
-- (for external app tables). Holds metadata used by triggers and calc
-- functions to avoid information_schema reflection at runtime.
CREATE TABLE IF NOT EXISTS claimius.table_info (
                                                   id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                   schema_name         TEXT NOT NULL,
                                                   table_name          TEXT NOT NULL,
                                                   object_type         TEXT GENERATED ALWAYS AS (schema_name || '.' || table_name) STORED,
                                                   is_internal         BOOLEAN NOT NULL,
                                                   has_sa_owner_id     BOOLEAN NOT NULL,
                                                   has_sa_location_id  BOOLEAN NOT NULL,
                                                   has_sa_parent_id    BOOLEAN NOT NULL,
                                                   has_sa_root_id      BOOLEAN NOT NULL,
                                                   has_sa_created_by   BOOLEAN NOT NULL,
                                                   has_sa_deleted_at   BOOLEAN NOT NULL,
                                                   has_sa_created_at   BOOLEAN NOT NULL,
                                                   has_sa_updated_at   BOOLEAN NOT NULL,
                                                   has_app_id          BOOLEAN NOT NULL,
                                                   has_name            BOOLEAN NOT NULL,
                                                   has_description     BOOLEAN NOT NULL,
                                                   sa_created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   sa_updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.table_info IS 'Registry of all tables managed by claimius. Cached column metadata.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_table_info_object_type ON claimius.table_info (object_type);
CREATE INDEX IF NOT EXISTS idx_table_info_is_internal ON claimius.table_info (is_internal);
CREATE INDEX IF NOT EXISTS idx_table_info_has_parent ON claimius.table_info (has_sa_parent_id, has_sa_root_id);

-- inheritance_info
-- Closure table of every ancestor-descendant relationship across the three
-- hierarchy types (ownership, location, parenthood). One row per edge.
-- Self-edges (depth = 0) included so a node is its own ancestor: that lets
-- "is X reachable from Y in tree T" be a single indexed lookup.
-- Trees are heterogeneous: descendant rows can carry a different object_type
-- from the root (an ownership tree rooted at an organization can contain
-- devices, locations, etc.).
CREATE TABLE IF NOT EXISTS claimius.inheritance_info (
                                                         tree_type       claimius.tree_type NOT NULL,
                                                         root_id         UUID NOT NULL,
                                                         ancestor_type   TEXT NOT NULL,
                                                         ancestor_id     UUID NOT NULL,
                                                         descendant_type TEXT NOT NULL,
                                                         descendant_id   UUID NOT NULL,
                                                         depth           INTEGER NOT NULL,
                                                         sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                         sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                         PRIMARY KEY (tree_type, root_id, ancestor_type, ancestor_id, descendant_type, descendant_id)
);

COMMENT ON TABLE claimius.inheritance_info IS 'Closure table of ancestor-descendant edges per tree. Self-edges at depth 0.';
COMMENT ON COLUMN claimius.inheritance_info.depth IS '0 for self-edge; 1 for direct child; N for N-hop descendant.';

CREATE INDEX IF NOT EXISTS idx_inheritance_info_descendant
    ON claimius.inheritance_info (descendant_type, descendant_id, tree_type)
    INCLUDE (ancestor_type, ancestor_id, root_id, depth);
CREATE INDEX IF NOT EXISTS idx_inheritance_info_ancestor
    ON claimius.inheritance_info (ancestor_type, ancestor_id, tree_type)
    INCLUDE (descendant_type, descendant_id, root_id, depth);
