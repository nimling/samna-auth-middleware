-- ============================================================================
-- Claimius V1.0 Baseline Schema
-- ----------------------------------------------------------------------------
-- All table definitions, indexes, and comments. No functions, no triggers.
-- Tables are organized into logical groups:
--
--   1. Enums                    Type definitions for status / kind columns
--   2. Identity                 samna_user, samna_client, samna_secret
--   3. Hierarchy roots          organization, location
--   4. Claims and bindings      claim, user_claim, claim_object, user_token
--   5. Registry                 table_info, inheritance_info
--   6. Materialized access      user_object, object_info, user_users
--   7. Sync                     sync_event, prophet_state, disciple_state,
--                               reconcile_queue, sync_parked
--   8. Audit                    audit
--   9. Extensibility            object_field, user_field, user_relation
--
-- Naming conventions:
--   sa_owner_id        Owning organization. Required on every registered table.
--   sa_location_id     Optional location pin. Nullable.
--   sa_parent_id       Self reference for hierarchical tables. When present,
--                      must be paired with sa_root_id. Root rows self
--                      reference (sa_parent_id = id).
--   sa_root_id         Root of the self referencing tree. A row is a root iff
--                      sa_owner_id == sa_root_id == id (organization) or
--                      sa_parent_id == sa_root_id == id (location, parenthood).
--   sa_created_by      The user_claim.id under which this row was created.
--   sa_deleted_at      Soft delete timestamp. NULL means active.
--   sa_created_at      Insertion time.
--   sa_updated_at      Last update time. Maintained by trigger.
--
-- Object identity:
--   object_type is always <schema>.<table>, never bare table name.
--   object_id is a uuid.
--   The deterministic UUID hash of (object_type, object_id) and similar
--   tuples is produced by claimius.composite_id and used as row primary
--   keys on materialization tables.
-- ============================================================================

-- ----------------------------------------------------------------------------
-- 1. Enums
-- ----------------------------------------------------------------------------

DO $$
    BEGIN
        IF NOT EXISTS (SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace WHERE t.typname = 'claim_type' AND n.nspname = 'claimius') THEN
            CREATE TYPE claimius.claim_type AS ENUM ('system', 'user', 'service', 'virtual');
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace WHERE t.typname = 'user_type' AND n.nspname = 'claimius') THEN
            CREATE TYPE claimius.user_type AS ENUM ('user', 'client', 'system', 'guest');
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace WHERE t.typname = 'user_status' AND n.nspname = 'claimius') THEN
            CREATE TYPE claimius.user_status AS ENUM ('active', 'pending', 'suspended', 'inactive');
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace WHERE t.typname = 'tree_type' AND n.nspname = 'claimius') THEN
            CREATE TYPE claimius.tree_type AS ENUM ('ownership', 'location', 'parenthood');
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace WHERE t.typname = 'sync_operation' AND n.nspname = 'claimius') THEN
            CREATE TYPE claimius.sync_operation AS ENUM ('upsert', 'delete');
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace WHERE t.typname = 'app_status' AND n.nspname = 'claimius') THEN
            CREATE TYPE claimius.app_status AS ENUM ('draft', 'active', 'inactive', 'erroneous');
        END IF;
    END $$;

-- ----------------------------------------------------------------------------
-- 2. Identity
-- ----------------------------------------------------------------------------

-- samna_user
-- The canonical user record. One row per (user_id) pair, so the
-- same human appears as multiple rows across apps. user_id is the stable
-- application identifier referenced everywhere; id is the row PK.
CREATE TABLE IF NOT EXISTS claimius.samna_user (
                                                   id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                   user_id         UUID NOT NULL DEFAULT gen_random_uuid(),
                                                   first_name      VARCHAR(255),
                                                   last_name       VARCHAR(255),
                                                   user_name       TEXT,
                                                   user_image      TEXT,
                                                   phone           VARCHAR(50),
                                                   email           VARCHAR(255),
                                                   external_id     TEXT DEFAULT NULL,
                                                   status          claimius.user_status NOT NULL DEFAULT 'active',
                                                   type            claimius.user_type NOT NULL DEFAULT 'user',
                                                   config          JSONB,
                                                   sa_deleted_at   TIMESTAMPTZ,
                                                   sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.samna_user IS 'User record. One row per (user_id) pair.';
COMMENT ON COLUMN claimius.samna_user.user_id IS 'Stable application identifier across apps. Used in all references.';
COMMENT ON COLUMN claimius.samna_user.user_name IS 'Optional display username, distinct from first_name/last_name.';
ALTER TABLE claimius.samna_user ADD COLUMN IF NOT EXISTS user_image TEXT;

COMMENT ON COLUMN claimius.samna_user.user_image IS 'Optional user image. URL, string, or id that the implementing system recognizes.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_samna_user_user_app
    ON claimius.samna_user (user_id) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_samna_user_user_id ON claimius.samna_user (user_id);
CREATE INDEX IF NOT EXISTS idx_samna_user_email ON claimius.samna_user (email);
CREATE INDEX IF NOT EXISTS idx_samna_user_sa_deleted_at ON claimius.samna_user (sa_deleted_at);
CREATE INDEX IF NOT EXISTS idx_samna_user_status ON claimius.samna_user (status);

-- samna_client
-- Service account. Authenticates with secret_hash. Claims attach via
-- user_claim using client_id as user_id.
CREATE TABLE IF NOT EXISTS claimius.samna_client (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     name            TEXT NOT NULL,
                                                     description     TEXT,
                                                     client_id       UUID UNIQUE NOT NULL DEFAULT gen_random_uuid(),
                                                     secret_hash     TEXT NOT NULL,
                                                     status          VARCHAR(50) NOT NULL DEFAULT 'active',
                                                     expires_at      TIMESTAMPTZ,
                                                     sa_owner_id     UUID NOT NULL,
                                                     sa_created_by   UUID,
                                                     sa_deleted_at   TIMESTAMPTZ,
                                                     sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.samna_client IS 'Service account record. client_id used as user_id in user_claim.';

CREATE INDEX IF NOT EXISTS idx_samna_client_client_id ON claimius.samna_client (client_id);
CREATE INDEX IF NOT EXISTS idx_samna_client_sa_owner_id ON claimius.samna_client (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_samna_client_sa_deleted_at ON claimius.samna_client (sa_deleted_at);

-- samna_secret
-- Per app secret store. Keyed by (key) when not soft deleted.
CREATE TABLE IF NOT EXISTS claimius.samna_secret (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     key             TEXT NOT NULL,
                                                     value           TEXT NOT NULL,
                                                     expires_at      TIMESTAMPTZ,
                                                     sa_owner_id     UUID NOT NULL,
                                                     sa_created_by   UUID,
                                                     sa_deleted_at   TIMESTAMPTZ,
                                                     sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.samna_secret IS 'Per app secret store.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_samna_secret_app_key
    ON claimius.samna_secret (key) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_samna_secret_sa_owner_id ON claimius.samna_secret (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_samna_secret_sa_deleted_at ON claimius.samna_secret (sa_deleted_at);

-- ----------------------------------------------------------------------------
-- 3. Hierarchy roots
-- ----------------------------------------------------------------------------

-- organization
-- The org tree. Self references through sa_owner_id. Root iff
-- sa_owner_id == sa_root_id == id. NEVER null sa_owner_id; root self
-- references.
CREATE TABLE IF NOT EXISTS claimius.organization (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     name            VARCHAR(255) NOT NULL,
                                                     description     TEXT,
                                                     type            VARCHAR(50),
                                                     private_seed    TEXT NOT NULL DEFAULT '',
                                                     sa_owner_id     UUID NOT NULL,
                                                     sa_root_id      UUID NOT NULL,
                                                     sa_location_id  UUID,
                                                     sa_level        INTEGER NOT NULL DEFAULT 0,
                                                     external_id     TEXT DEFAULT NULL,
                                                     provider_id     UUID,
                                                     sa_created_by   UUID NOT NULL,
                                                     sa_deleted_at   TIMESTAMPTZ,
                                                     sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.organization IS 'Organization tree. Root iff sa_owner_id == sa_root_id == id.';
COMMENT ON COLUMN claimius.organization.private_seed IS 'Hex encoded HMAC seed. Synced from the prophet. Keys app_secret hashing for apps owned in this tree.';
COMMENT ON COLUMN claimius.organization.sa_owner_id IS 'Parent org. Equals id at root.';
COMMENT ON COLUMN claimius.organization.sa_root_id IS 'Top of tree. Equals id at root.';
COMMENT ON COLUMN claimius.organization.sa_location_id IS 'Optional location pin for the org itself.';

CREATE INDEX IF NOT EXISTS idx_organization_sa_owner_id ON claimius.organization (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_organization_sa_root_id ON claimius.organization (sa_root_id);
CREATE INDEX IF NOT EXISTS idx_organization_sa_location_id ON claimius.organization (sa_location_id) WHERE sa_location_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_organization_external_id ON claimius.organization (external_id);
CREATE INDEX IF NOT EXISTS idx_organization_provider_id ON claimius.organization (provider_id);
CREATE INDEX IF NOT EXISTS idx_organization_sa_deleted_at ON claimius.organization (sa_deleted_at);

-- location
-- The location tree. Locations belong to an organization. Self references
-- through sa_parent_id. Root iff sa_parent_id == sa_root_id == id.
CREATE TABLE IF NOT EXISTS claimius.location (
                                                 id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                 sa_owner_id     UUID NOT NULL REFERENCES claimius.organization(id),
                                                 sa_parent_id    UUID NOT NULL,
                                                 sa_root_id      UUID NOT NULL,
                                                 name            VARCHAR(255),
                                                 description     TEXT,
                                                 type            VARCHAR(50) NOT NULL DEFAULT 'room',
                                                 longitude       DOUBLE PRECISION,
                                                 latitude        DOUBLE PRECISION,
                                                 sa_level        INTEGER NOT NULL DEFAULT 0,
                                                 code_type       VARCHAR(50),
                                                 code            VARCHAR(255),
                                                 external_id     TEXT DEFAULT NULL,
                                                 provider_id     UUID,
                                                 sa_created_by   UUID NOT NULL,
                                                 sa_deleted_at   TIMESTAMPTZ,
                                                 sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                 sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.location IS 'Location tree. Root iff sa_parent_id == sa_root_id == id.';
COMMENT ON COLUMN claimius.location.sa_owner_id IS 'Owning organization. Required.';
COMMENT ON COLUMN claimius.location.sa_parent_id IS 'Parent location. Equals id at root.';
COMMENT ON COLUMN claimius.location.sa_root_id IS 'Top of tree. Equals id at root.';

CREATE INDEX IF NOT EXISTS idx_location_sa_owner_id ON claimius.location (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_location_sa_parent_id ON claimius.location (sa_parent_id);
CREATE INDEX IF NOT EXISTS idx_location_sa_root_id ON claimius.location (sa_root_id);
CREATE INDEX IF NOT EXISTS idx_location_external_id ON claimius.location (external_id);
CREATE INDEX IF NOT EXISTS idx_location_provider_id ON claimius.location (provider_id);
CREATE INDEX IF NOT EXISTS idx_location_sa_deleted_at ON claimius.location (sa_deleted_at);

-- ----------------------------------------------------------------------------
-- 4. Claims and bindings
-- ----------------------------------------------------------------------------

-- claim
-- A named permission template. Carries a bitwise access mask and is owned
-- by an organization. When the deny bit (0x10) of sa_access is set, the
-- other bits enumerate which permissions to subtract on the bound path.
CREATE TABLE IF NOT EXISTS claimius.claim (
                                              id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                              name            TEXT NOT NULL,
                                              description     TEXT,
                                              sa_access       INTEGER NOT NULL,
                                              inherits        BOOLEAN NOT NULL DEFAULT TRUE,
                                              type            claimius.claim_type NOT NULL DEFAULT 'user',
                                              sa_owner_id     UUID NOT NULL REFERENCES claimius.organization(id),
                                              sa_root_id      UUID NOT NULL REFERENCES claimius.organization(id),
                                              sa_created_by   UUID,
                                              sa_deleted_at   TIMESTAMPTZ,
                                              sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                              sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                              txid            XID8 NOT NULL DEFAULT pg_current_xact_id()
);

COMMENT ON TABLE claimius.claim IS 'Permission template. Access is a bitwise mask.';
COMMENT ON COLUMN claimius.claim.sa_access IS 'Bitwise access mask. Bits: 0x01 owner, 0x02 write, 0x04 read, 0x08 execute, 0x10 deny, 0x20 member. When the deny bit is set the other bits enumerate which permissions to subtract.';
COMMENT ON COLUMN claimius.claim.inherits IS 'When true, claim_object bindings cascade through hierarchy.';

CREATE INDEX IF NOT EXISTS idx_claim_sa_owner_id ON claimius.claim (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_claim_sa_root_id ON claimius.claim (sa_root_id);
CREATE INDEX IF NOT EXISTS idx_claim_deny_bit ON claimius.claim ((sa_access & 16)) WHERE (sa_access & 16) <> 0;

ALTER TABLE claimius.claim ADD COLUMN IF NOT EXISTS level INTEGER NOT NULL DEFAULT 0;
DO $$ BEGIN
    ALTER TABLE claimius.claim ADD CONSTRAINT claim_level_not_negative CHECK (level >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
COMMENT ON COLUMN claimius.claim.level IS 'Precedence of the claim. A deny subtracts from every grant below its level whatever their classes; at the same level a direct deny subtracts from every grant and a tree deny from inherited grants. A link takes the higher level of the two claims, and the claim of that level decides its mask. 0 for every claim resolves as before levels existed.';
DROP INDEX IF EXISTS claimius.idx_claim_sa_deleted_at;
ALTER TABLE claimius.claim ADD COLUMN IF NOT EXISTS txid XID8 NOT NULL DEFAULT pg_current_xact_id();
COMMENT ON COLUMN claimius.claim.txid IS 'Local transaction that last wrote the row, stamped by tg_update_txid on every insert and update, a replayed one included. get_claim_graph compares it to the snapshot a cached graph was built under.';

-- user_claim
-- Grants a claim to a user (or client). Optional time bounds. Reads filter
-- by current time against starts_at and ends_at.
CREATE TABLE IF NOT EXISTS claimius.user_claim (
                                                   id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                   claim_id        UUID NOT NULL REFERENCES claimius.claim(id),
                                                   user_id         UUID NOT NULL,
                                                   reason          TEXT,
                                                   starts_at       TIMESTAMPTZ,
                                                   ends_at         TIMESTAMPTZ,
                                                   sa_owner_id     UUID NOT NULL REFERENCES claimius.organization(id),
                                                   sa_created_by   UUID NOT NULL,
                                                   sa_deleted_at   TIMESTAMPTZ,
                                                   sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   txid            XID8 NOT NULL DEFAULT pg_current_xact_id()
);

COMMENT ON TABLE claimius.user_claim IS 'Grants a claim to a user in an app. id is referenced as sa_created_by everywhere.';
COMMENT ON COLUMN claimius.user_claim.user_id IS 'References samna_user.user_id, not samna_user.id.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_claim_unique
    ON claimius.user_claim (claim_id, user_id) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_claim_user_app ON claimius.user_claim (user_id);
CREATE INDEX IF NOT EXISTS idx_user_claim_claim_id ON claimius.user_claim (claim_id);
CREATE INDEX IF NOT EXISTS idx_user_claim_sa_owner_id ON claimius.user_claim (sa_owner_id);
DROP INDEX IF EXISTS claimius.idx_user_claim_sa_deleted_at;
ALTER TABLE claimius.user_claim ADD COLUMN IF NOT EXISTS txid XID8 NOT NULL DEFAULT pg_current_xact_id();
COMMENT ON COLUMN claimius.user_claim.txid IS 'Local transaction that last wrote the row, stamped by tg_update_txid on every insert and update, a replayed one included. get_claim_graph compares it to the snapshot a cached graph was built under.';

-- claim_object
-- Binds a claim. With (object_type, object_id) the binding targets that
-- object; scope keys are field names. With both NULL the binding has no
-- object; scope keys are custom.
CREATE TABLE IF NOT EXISTS claimius.claim_object (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     claim_id        UUID NOT NULL REFERENCES claimius.claim(id),
                                                     object_id       UUID,
                                                     object_type     TEXT,
                                                     reason          TEXT,
                                                     scope           JSONB,
                                                     sa_access       INTEGER,
                                                     inherits        BOOLEAN NOT NULL DEFAULT TRUE,
                                                     ref_id          UUID,
                                                     sa_owner_id     UUID NOT NULL REFERENCES claimius.organization(id),
                                                     sa_root_id      UUID NOT NULL REFERENCES claimius.organization(id),
                                                     sa_created_by   UUID,
                                                     sa_deleted_at   TIMESTAMPTZ,
                                                     sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     txid            XID8 NOT NULL DEFAULT pg_current_xact_id(),
                                                     CONSTRAINT claim_object_object_pair CHECK (
                                                         (object_id IS NULL AND object_type IS NULL)
                                                      OR (object_id IS NOT NULL AND object_type IS NOT NULL)
                                                     )
);

COMMENT ON TABLE claimius.claim_object IS 'Binds a claim. With (object_type, object_id) scope keys are field names; without them scope keys are custom.';
COMMENT ON COLUMN claimius.claim_object.sa_access IS 'Bitwise access mask with bits 0x01 owner, 0x02 write, 0x04 read, 0x08 execute, 0x10 deny, 0x20 member; NULL means inherit the claim sa_access at read time.';
COMMENT ON COLUMN claimius.claim_object.scope IS 'Object scoped: keys are field names, NULL means whole object. Object-less: keys are custom, NULL means no grant.';

CREATE INDEX IF NOT EXISTS idx_claim_object_claim_id ON claimius.claim_object (claim_id);
CREATE INDEX IF NOT EXISTS idx_claim_object_object ON claimius.claim_object (object_type, object_id) WHERE object_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_claim_object_no_object ON claimius.claim_object (claim_id) WHERE object_id IS NULL AND sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_claim_object_sa_owner_id ON claimius.claim_object (sa_owner_id);
DROP INDEX IF EXISTS claimius.idx_claim_object_sa_deleted_at;
ALTER TABLE claimius.claim_object ADD COLUMN IF NOT EXISTS txid XID8 NOT NULL DEFAULT pg_current_xact_id();
COMMENT ON COLUMN claimius.claim_object.txid IS 'Local transaction that last wrote the row, stamped by tg_update_txid on every insert and update, a replayed one included. get_claim_graph compares it to the snapshot a cached graph was built under.';
CREATE INDEX IF NOT EXISTS idx_claim_object_lookup
    ON claimius.claim_object (claim_id, object_type, object_id)
    WHERE sa_deleted_at IS NULL AND object_id IS NOT NULL;

-- user_token
-- Debug only bridge from JWT identity (external_id under id_claim) to
-- claimius.samna_user.user_id. Not synced. Populated by debug
-- POST /user/token, read by SAuth-User-Token validate.
CREATE TABLE IF NOT EXISTS claimius.user_token (
                                                   id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                   user_id         UUID NOT NULL,
                                                   id_claim        TEXT NOT NULL DEFAULT 'sub',
                                                   external_id     TEXT NOT NULL,
                                                   type            TEXT NOT NULL DEFAULT 'token',
                                                   claim_id        UUID REFERENCES claimius.claim(id),
                                                   data            JSONB,
                                                   sa_owner_id     UUID NOT NULL,
                                                   sa_created_by   UUID,
                                                   sa_deleted_at   TIMESTAMPTZ,
                                                   sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.user_token IS 'Debug only bridge from JWT identity (external_id under id_claim) to claimius.samna_user.user_id. Not synced.';

ALTER TABLE claimius.user_token ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'token';
COMMENT ON COLUMN claimius.user_token.type IS 'Binding kind: token for JWT derived, external for direct external_id.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_token_app_owner_external
    ON claimius.user_token (sa_owner_id, external_id) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_token_user_id ON claimius.user_token (user_id);
CREATE INDEX IF NOT EXISTS idx_user_token_type ON claimius.user_token (type) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_token_sa_owner_id ON claimius.user_token (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_user_token_sa_deleted_at ON claimius.user_token (sa_deleted_at);

-- ----------------------------------------------------------------------------
-- 5. Registry
-- ----------------------------------------------------------------------------

-- table_info
-- Registry of every table managed by claimius. Populated by
-- upsert_internal_table_info (for claimius.* tables) and upsert_table_info
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

-- ----------------------------------------------------------------------------
-- 6. Materialized access
-- ----------------------------------------------------------------------------

-- user_object
-- The reverse access index. One row per (user_id, object_type, object_id)
-- when the user has any access (claim based or direct), keyed on exactly that
-- tuple. The primary key carries sa_access so a listing of one reader's
-- objects of one type is an index only range. A pair holds only what is per
-- user: the mask, the grants, the scope and the direct flag. What is per
-- object, the owner, location, root, name, description, link and search
-- vector, lives once in object_info. The same pairs keyed from the object
-- side are the index idx_user_object_object_user, so the users of one object
-- are read here and the pairs are stored once.
-- used_count and last_used_at record how often and how lately the user checked
-- the object. touch_object_usage adds to them, a settle never writes them and
-- they leave with the pair. They are local to this database and never sync.
CREATE TABLE IF NOT EXISTS claimius.user_object (
                                                    user_id             UUID NOT NULL,
                                                    object_id           UUID NOT NULL,
                                                    object_type         TEXT NOT NULL,
                                                    sa_access           INTEGER NOT NULL,
                                                    grants              JSONB NOT NULL DEFAULT '[]'::jsonb,
                                                    scope               JSONB,
                                                    direct_grant        BOOLEAN NOT NULL DEFAULT FALSE,
                                                    used_count          BIGINT NOT NULL DEFAULT 0,
                                                    last_used_at        TIMESTAMPTZ,
                                                    sa_created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                    sa_updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                    CONSTRAINT user_object_pkey PRIMARY KEY (user_id, object_type, object_id) INCLUDE (sa_access)
);

COMMENT ON TABLE claimius.user_object IS 'Reverse access index. Read path. One row per (user, object) with access, keyed on that tuple. Per object fields live in object_info.';
COMMENT ON COLUMN claimius.user_object.grants IS 'JSON array of {claim_id, access, scope, cascaded_from, tree_type}. Empty when only direct.';
COMMENT ON COLUMN claimius.user_object.direct_grant IS 'True when user is creator (via sa_created_by lookup).';
COMMENT ON COLUMN claimius.user_object.sa_access IS 'Effective bitwise access mask: OR of grants[].access with deny bits subtracted. Bits: 0x01 owner, 0x02 write, 0x04 read, 0x08 execute. Direct creator gets 0x01|0x02|0x04|0x08.';
COMMENT ON COLUMN claimius.user_object.scope IS 'Effective merged scope: union of grants[].scope keys when any grant has a scope, else NULL meaning whole object access.';

ALTER TABLE claimius.user_object ADD COLUMN IF NOT EXISTS used_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE claimius.user_object ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ;

COMMENT ON COLUMN claimius.user_object.used_count IS 'Successful single object checks of the pair, added by touch_object_usage. Kept by every settle, gone with the pair.';
COMMENT ON COLUMN claimius.user_object.last_used_at IS 'Time of the latest flush that counted a check of the pair. Usage ranks by used_count * 2^(-age / 14 days).';

-- object_info
-- The per object fields every pair used to copy: owner, location, root, name,
-- description, link and the search vector built from name and description.
-- One row per live object of a registered table, written by the calc
-- triggers when the object is written and by the bulk paths, so a rename
-- touches one row and no pair. The owner and location indexes carry the name
-- so the children of one node come in name order.
CREATE TABLE IF NOT EXISTS claimius.object_info (
                                                    object_type         TEXT NOT NULL,
                                                    object_id           UUID NOT NULL,
                                                    sa_owner_id         UUID,
                                                    sa_location_id      UUID,
                                                    sa_root_id          UUID,
                                                    sa_name             TEXT,
                                                    sa_description      TEXT,
                                                    sa_link             TEXT,
                                                    search_vector       TSVECTOR GENERATED ALWAYS AS (
                                                        setweight(to_tsvector('simple'::REGCONFIG, coalesce(sa_name, '')), 'A') ||
                                                        setweight(to_tsvector('simple'::REGCONFIG, coalesce(sa_description, '')), 'B')
                                                    ) STORED,
                                                    sa_updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                    PRIMARY KEY (object_type, object_id)
);

COMMENT ON TABLE claimius.object_info IS 'Per object rendering and search fields of every live registered object, stored once. Written by calc_object_access, calc_hierarchical_access and the bulk seed; read by listings, search and the graphs.';
COMMENT ON COLUMN claimius.object_info.search_vector IS 'Generated from sa_name (weight A) and sa_description (weight B).';

CREATE INDEX IF NOT EXISTS idx_object_info_search ON claimius.object_info USING GIN (search_vector);
CREATE INDEX IF NOT EXISTS idx_object_info_owner_name ON claimius.object_info (sa_owner_id, sa_name, object_id);
CREATE INDEX IF NOT EXISTS idx_object_info_location_name ON claimius.object_info (sa_location_id, sa_name, object_id);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = 'claimius' AND table_name = 'user_object' AND column_name = 'sa_name') THEN
        INSERT INTO claimius.object_info (object_type, object_id, sa_owner_id, sa_location_id, sa_root_id, sa_name, sa_description, sa_link)
        SELECT DISTINCT ON (uo.object_type, uo.object_id)
               uo.object_type, uo.object_id, uo.sa_owner_id, uo.sa_location_id, uo.sa_root_id, uo.sa_name, uo.sa_description, uo.sa_link
        FROM claimius.user_object uo
        ORDER BY uo.object_type, uo.object_id, uo.sa_updated_at DESC
        ON CONFLICT (object_type, object_id) DO NOTHING;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = 'claimius' AND table_name = 'user_object' AND column_name = 'id') THEN
        DELETE FROM claimius.user_object a
        USING claimius.user_object b
        WHERE a.user_id = b.user_id
          AND a.object_type = b.object_type
          AND a.object_id = b.object_id
          AND (a.sa_updated_at, a.id) < (b.sa_updated_at, b.id);
        ALTER TABLE claimius.user_object DROP CONSTRAINT IF EXISTS user_object_pkey;
        ALTER TABLE claimius.user_object DROP COLUMN id;
        ALTER TABLE claimius.user_object
            ADD CONSTRAINT user_object_pkey PRIMARY KEY (user_id, object_type, object_id) INCLUDE (sa_access);
    END IF;
END $$;

ALTER TABLE claimius.user_object
    DROP COLUMN IF EXISTS sa_owner_id,
    DROP COLUMN IF EXISTS sa_location_id,
    DROP COLUMN IF EXISTS sa_root_id,
    DROP COLUMN IF EXISTS sa_name,
    DROP COLUMN IF EXISTS sa_description,
    DROP COLUMN IF EXISTS sa_link,
    DROP COLUMN IF EXISTS search_vector;

ALTER TABLE claimius.user_object SET (
    fillfactor = 90,
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_analyze_scale_factor = 0.02
);

DROP INDEX IF EXISTS claimius.idx_user_object_user_type;
DROP INDEX IF EXISTS claimius.idx_user_object_user_object;
DROP INDEX IF EXISTS claimius.idx_user_object_object;
DROP INDEX IF EXISTS claimius.idx_user_object_owner;
DROP INDEX IF EXISTS claimius.idx_user_object_location;
DROP INDEX IF EXISTS claimius.idx_user_object_root;
DROP INDEX IF EXISTS claimius.idx_user_object_grants;
DROP INDEX IF EXISTS claimius.idx_user_object_search;
CREATE INDEX IF NOT EXISTS idx_user_object_object_user ON claimius.user_object (object_type, object_id, user_id);

-- object_users
-- Held the pairs of user_object a second time, keyed from the object side.
-- The reverse lookup is idx_user_object_object_user and the rows are derived
-- from user_object, so the table goes.
DROP TABLE IF EXISTS claimius.object_users;

-- object_usage
-- Held the usage of each pair apart from the pair, where user_object now
-- carries it. Its counts are added into the pairs that exist, each pair
-- takes the later of the two last_used_at, and the table goes.
DO $$
BEGIN
    IF to_regclass('claimius.object_usage') IS NULL THEN
        RETURN;
    END IF;
    UPDATE claimius.user_object uo
    SET used_count = uo.used_count + ou.used_count,
        last_used_at = greatest(uo.last_used_at, ou.last_used_at)
    FROM claimius.object_usage ou
    WHERE ou.user_id = uo.user_id
      AND ou.object_type = uo.object_type
      AND ou.object_id = uo.object_id;
    DROP TABLE claimius.object_usage;
END $$;

-- user_users
-- Records that two users in the same app share at least one accessible
-- object. Used by get_users to list visible users. Kept by counting: a pair
-- of user_object entering or leaving moves sharing_object_count by one
-- against every other user of the object, in both directions, and a row
-- whose count reaches zero is removed.
-- The id is a deterministic UUID hash of (viewer_id, target_user_id)
-- produced by claimius.composite_id.
CREATE TABLE IF NOT EXISTS claimius.user_users (
                                                   id                      UUID PRIMARY KEY,
                                                   viewer_id               UUID NOT NULL,
                                                   target_user_id          UUID NOT NULL,
                                                   first_shared_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   last_shared_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   sharing_object_count    INTEGER NOT NULL DEFAULT 1
);

COMMENT ON TABLE claimius.user_users IS 'Pairs of users sharing at least one accessible object. Counted from user_object inserts and deletes.';
COMMENT ON COLUMN claimius.user_users.sharing_object_count IS 'Objects both users hold a user_object pair on. Moved by one per pair entering or leaving; the row is removed at zero.';

CREATE INDEX IF NOT EXISTS idx_user_users_viewer ON claimius.user_users (viewer_id);
CREATE INDEX IF NOT EXISTS idx_user_users_target ON claimius.user_users (target_user_id);

-- ----------------------------------------------------------------------------
-- 7. Sync
-- ----------------------------------------------------------------------------

-- sync_event
-- Append only event stream. Prophet writes here, disciple reads here. Each
-- event carries the full row payload as jsonb (or just {id} for deletes).
-- Disciples upsert by composite id, idempotently.
CREATE TABLE IF NOT EXISTS claimius.sync_event (
                                                   seq             BIGSERIAL PRIMARY KEY,
                                                   operation       claimius.sync_operation NOT NULL,
                                                   event_type      TEXT NOT NULL,
                                                   payload         JSONB NOT NULL,
                                                   written_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   txid            XID8 NOT NULL DEFAULT pg_current_xact_id()
);

ALTER TABLE claimius.sync_event ADD COLUMN IF NOT EXISTS txid XID8 NOT NULL DEFAULT pg_current_xact_id();

COMMENT ON TABLE claimius.sync_event IS 'Append only event stream for prophet to disciple sync.';
COMMENT ON COLUMN claimius.sync_event.event_type IS 'Source table: claimius.claim, claimius.user_claim, claimius.organization, etc. Derived tables never publish.';
COMMENT ON COLUMN claimius.sync_event.txid IS 'Transaction that wrote the event. The feed serves an event only once every transaction below it has ended, so a sequence that commits late is never passed over.';

DELETE FROM claimius.sync_event
WHERE event_type IN ('claimius.object_users', 'claimius.user_object', 'claimius.user_users', 'claimius.audit');

DROP INDEX IF EXISTS claimius.idx_sync_event_written_at;
CREATE INDEX IF NOT EXISTS idx_sync_event_written_brin ON claimius.sync_event USING BRIN (written_at);
DROP INDEX IF EXISTS claimius.idx_sync_event_type_seq;

-- prophet_state
-- Per app cursor on the prophet side. Tracks the highest sync_event.seq
-- already published to disciples for this app. Additionally carries the
-- system_app_slug marker on the system app's row (NULL on every other row).
CREATE TABLE IF NOT EXISTS claimius.prophet_state (
                                                      id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                      last_applied_seq    BIGINT NOT NULL DEFAULT 0,
                                                      system_app_slug     TEXT,
                                                      sa_updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_prophet_state_system_singleton
    ON claimius.prophet_state (system_app_slug)
    WHERE system_app_slug IS NOT NULL;

COMMENT ON TABLE claimius.prophet_state IS 'Per app publishing cursor. system_app_slug marks the deployment system app row.';

-- disciple_state
-- Singleton cursor on the disciple side. Tracks the (txid, seq) position
-- on the prophet feed already applied locally. The disciple identifies its app from env only,
-- so no app slug is stored here.
CREATE TABLE IF NOT EXISTS claimius.disciple_state (
                                                       id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                       last_applied_txid   BIGINT NOT NULL DEFAULT 0,
                                                       last_applied_seq    BIGINT NOT NULL DEFAULT 0,
                                                       sa_updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE claimius.disciple_state ADD COLUMN IF NOT EXISTS last_applied_txid BIGINT NOT NULL DEFAULT 0;

COMMENT ON TABLE claimius.disciple_state IS 'Singleton consumption cursor for the disciple: the prophet feed position (last_applied_txid, last_applied_seq) applied locally.';

ALTER TABLE claimius.disciple_state ADD COLUMN IF NOT EXISTS prophet TEXT;
ALTER TABLE claimius.disciple_state ADD COLUMN IF NOT EXISTS endpoint TEXT;

COMMENT ON COLUMN claimius.disciple_state.prophet IS 'Slug of the one prophet the disciple is bound to, as that prophet names itself on its feed. The cursor is a position on its feed alone. NULL until the first answer.';
COMMENT ON COLUMN claimius.disciple_state.endpoint IS 'Where the bound prophet answers. The disciple keeps pulling and routing writes there until another prophet serves it under an agreed handover.';

-- reconcile_queue
-- Disciple side retry queue. When applying a sync_event fails, the
-- (user, object, type) triple is queued here. reconcile_if_pending drains
-- entries inline at the top of every read for that user.
-- The id is a deterministic UUID hash of (user_id, object_type,
-- object_id) produced by claimius.composite_id.
CREATE TABLE IF NOT EXISTS claimius.reconcile_queue (
                                                        id              UUID PRIMARY KEY,
                                                        user_id         UUID NOT NULL,
                                                        object_id       UUID NOT NULL,
                                                        object_type     TEXT NOT NULL,
                                                        sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.reconcile_queue IS 'Disciple side retry queue for failed event applications.';

CREATE INDEX IF NOT EXISTS idx_reconcile_queue_user ON claimius.reconcile_queue (user_id);

CREATE TABLE IF NOT EXISTS claimius.sync_parked (
                                                    event_type      TEXT NOT NULL,
                                                    object_id       UUID NOT NULL,
                                                    operation       claimius.sync_operation NOT NULL,
                                                    payload         JSONB NOT NULL,
                                                    txid            BIGINT NOT NULL,
                                                    seq             BIGINT NOT NULL,
                                                    error           TEXT NOT NULL,
                                                    sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                    sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                    PRIMARY KEY (event_type, object_id)
);

COMMENT ON TABLE claimius.sync_parked IS 'Feed records the disciple could not apply for a missing parent row, one per row, the latest record of the row kept. The cursor moves past them; each applied page and snapshot retries them in feed order and drops those that apply. A later record of the same row that applies drops the parked one.';

-- access_diff
-- The outstanding areas of access that no longer reflect their inputs. A write
-- records one row here, keyed on what changed and never on the users it
-- affects, so write cost is constant in the number of users.
-- Each user settles its own share of an area when it reads: a check settles
-- the one pair it asks about, a listing settles the reader's share of every
-- area it meets and records it in access_diff_user. An area nobody reads is
-- never computed at all, and the row clears once every holder has settled it.
-- kind 'object' carries object_type and object_id: a row entered or left a
-- tree, changed owner or location, or was soft deleted. kind 'claim' carries
-- claim_id: a claim or a binding changed, and with user_id an assignment of
-- the claim to that one user changed.
-- The id is deterministic on the area, so repeated changes to the same area
-- collapse into one outstanding row instead of a queue of duplicates. Each
-- repeat adds one to raise_count, which supersedes every share settled before
-- it, and which with sa_created_at estimates how often the area is raised:
-- drain_access_diff settles a holder's share ahead of its read only when the
-- holder reads more often than that.
CREATE TABLE IF NOT EXISTS claimius.access_diff (
                                                    id              UUID PRIMARY KEY,
                                                    kind            TEXT NOT NULL,
                                                    object_type     TEXT,
                                                    object_id       UUID,
                                                    claim_id        UUID,
                                                    cascade         BOOLEAN NOT NULL DEFAULT FALSE,
                                                    sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                    sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE claimius.access_diff ADD COLUMN IF NOT EXISTS user_id UUID;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = 'claimius' AND table_name = 'access_diff' AND column_name = 'generation') THEN
        ALTER TABLE claimius.access_diff RENAME COLUMN generation TO raise_count;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = 'claimius' AND table_name = 'access_diff_user' AND column_name = 'generation') THEN
        ALTER TABLE claimius.access_diff_user RENAME COLUMN generation TO raise_count;
    END IF;
END $$;

ALTER TABLE claimius.access_diff ADD COLUMN IF NOT EXISTS raise_count BIGINT NOT NULL DEFAULT 1;

ALTER TABLE claimius.access_diff SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_analyze_scale_factor = 0.02
);

COMMENT ON TABLE claimius.access_diff IS 'Outstanding access areas awaiting recalculation. Written by calc triggers, settled per user by the reads that meet the area and by drain_access_diff, cleared once every holder has settled the current raise.';
COMMENT ON COLUMN claimius.access_diff.user_id IS 'Set on a claim area raised by an assignment: the one user whose access the assignment changed. NULL means every holder of the claim.';
COMMENT ON COLUMN claimius.access_diff.raise_count IS 'Raises of the area since it opened, one per write that raised it. A share settled at a lower count is superseded; raise_count / (now - sa_created_at) is the raise rate the drain compares a holder''s read rate against.';

CREATE INDEX IF NOT EXISTS idx_access_diff_object ON claimius.access_diff (object_type, object_id) WHERE kind = 'object';
CREATE INDEX IF NOT EXISTS idx_access_diff_claim ON claimius.access_diff (claim_id) WHERE kind = 'claim';
CREATE INDEX IF NOT EXISTS idx_access_diff_user ON claimius.access_diff (user_id) WHERE kind = 'claim' AND user_id IS NOT NULL;

-- access_diff_user
-- One row per area and user that settled its share of the area, at the
-- raise_count it settled. A user never settles the same raise twice, and the
-- row goes with its area.
CREATE TABLE IF NOT EXISTS claimius.access_diff_user (
                                                         diff_id         UUID NOT NULL REFERENCES claimius.access_diff (id) ON DELETE CASCADE,
                                                         user_id         UUID NOT NULL,
                                                         raise_count     BIGINT NOT NULL,
                                                         sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                         PRIMARY KEY (diff_id, user_id)
);

COMMENT ON TABLE claimius.access_diff_user IS 'Per user done record of an access_diff area. Written by the read or the drain that settles the user share, removed with the area.';

-- user_activity
-- When a user last used the app: one row per user, moved forward by
-- touch_object_usage whenever it counts a check of theirs. 1 / (now -
-- last_used_at) estimates how often the user reads, and drain_access_diff
-- settles a user's share of an area ahead of the read only when that rate is
-- at least the area's raise rate. Local, never synced.
CREATE TABLE IF NOT EXISTS claimius.user_activity (
                                                      user_id         UUID PRIMARY KEY,
                                                      last_used_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.user_activity IS 'Latest use of the app per user, written by touch_object_usage, read by drain_access_diff to order and gate the settle ahead of a read.';

-- claim_graph_cache
-- Per user materialized result of claimius.get_claim_graph for the default
-- argument shape: p_start_id IS NULL, p_depth = 0, p_required_access = 0. The
-- graph column holds the full row set as a jsonb array, one element per
-- (object_id, object_type, label, parent_id, data) tuple. Read path expands
-- back to rows. Dropped by triggers on user_object, object_info, user_users
-- and samna_user of the user, and validated on read against the snapshot the
-- graph was built under: a user_claim row of the user, its claim or one of
-- that claim's bindings written by a transaction the snapshot does not see
-- makes the row stale, a write that began before the build and committed
-- after it included.
CREATE TABLE IF NOT EXISTS claimius.claim_graph_cache (
                                                          user_id         UUID NOT NULL,
                                                          graph           JSONB NOT NULL,
                                                          snapshot        PG_SNAPSHOT NOT NULL,
                                                          PRIMARY KEY (user_id)
);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = 'claimius' AND table_name = 'claim_graph_cache' AND column_name = 'sa_updated_at') THEN
        DELETE FROM claimius.claim_graph_cache;
        ALTER TABLE claimius.claim_graph_cache DROP COLUMN sa_updated_at;
        ALTER TABLE claimius.claim_graph_cache ADD COLUMN snapshot PG_SNAPSHOT NOT NULL;
    END IF;
END $$;

COMMENT ON TABLE claimius.claim_graph_cache IS 'Per user cache of get_claim_graph default shape. Dropped by triggers on the user''s own rows, validated on read against the snapshot it was built under and the txid of the user''s grants, claims and bindings.';
COMMENT ON COLUMN claimius.claim_graph_cache.graph IS 'JSON array of {object_id, object_type, label, parent_id, data} tuples.';
COMMENT ON COLUMN claimius.claim_graph_cache.snapshot IS 'pg_current_snapshot() taken before the graph was read. A source row whose txid is not visible in it was written after the build saw the sources.';

-- ----------------------------------------------------------------------------
-- 8. Audit
-- ----------------------------------------------------------------------------

-- audit
-- Append only audit log, partitioned by month on sa_created_at. Written by
-- insert_audit, which creates the month's partition on the first row of the
-- month. A row no month partition takes lands in audit_default and moves to
-- its month when that partition is created. An unpartitioned audit table
-- from before becomes audit_default and its rows move to their months.
-- sa_created_by is nullable on this table only. Audit captures every event,
-- including ones with no clear actor: denied access attempts, system trigger
-- side effects, disciple replay, scheduled cleanups. Successful operations
-- still write the acting user_claim.id; everything else writes NULL.
DO $$
DECLARE
    v_fn REGPROCEDURE;
BEGIN
    IF (SELECT c.relkind FROM pg_class c WHERE c.oid = to_regclass('claimius.audit')) = 'r' THEN
        FOR v_fn IN SELECT p.oid::REGPROCEDURE FROM pg_proc p WHERE p.prorettype = 'claimius.audit'::REGTYPE LOOP
            EXECUTE format('DROP FUNCTION %s', v_fn);
        END LOOP;
        DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.audit;
        ALTER TABLE claimius.audit DROP CONSTRAINT IF EXISTS audit_pkey;
        DROP INDEX IF EXISTS claimius.idx_audit_sa_owner_id,
            claimius.idx_audit_sa_created_at, claimius.idx_audit_object;
        ALTER TABLE claimius.audit RENAME TO audit_default;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS claimius.audit (
                                              id              UUID NOT NULL DEFAULT gen_random_uuid(),
                                              operation       VARCHAR NOT NULL,
                                              message         TEXT DEFAULT '',
                                              type            VARCHAR NOT NULL,
                                              object_type     VARCHAR NOT NULL,
                                              object_id       UUID NOT NULL,
                                              sa_owner_id     UUID NOT NULL,
                                              sa_created_by   UUID,
                                              sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                              sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                              PRIMARY KEY (id, sa_created_at)
) PARTITION BY RANGE (sa_created_at);

-- _match_audit_partition_grants
-- Makes one partition of audit hold exactly the grants the partitioned table
-- holds, the owner and PUBLIC aside: every other grant on the partition is
-- revoked, then each grant on the partitioned table is given. So the
-- unpartitioned audit an older set left behind, now audit_default, keeps
-- none of its own.
CREATE OR REPLACE FUNCTION claimius._match_audit_partition_grants(p_partition REGCLASS)
    RETURNS VOID AS $$
DECLARE
    v_acl RECORD;
BEGIN
    FOR v_acl IN
        SELECT DISTINCT pg_get_userbyid(a.grantee) AS grantee
        FROM pg_class c, aclexplode(c.relacl) a
        WHERE c.oid = p_partition AND a.grantee <> 0 AND a.grantee <> c.relowner
    LOOP
        EXECUTE format('REVOKE ALL ON %s FROM %I', p_partition, v_acl.grantee);
    END LOOP;
    FOR v_acl IN
        SELECT a.privilege_type, pg_get_userbyid(a.grantee) AS grantee
        FROM pg_class c, aclexplode(c.relacl) a
        WHERE c.oid = 'claimius.audit'::REGCLASS AND a.grantee <> 0 AND a.grantee <> c.relowner
    LOOP
        EXECUTE format('GRANT %s ON %s TO %I', v_acl.privilege_type, p_partition, v_acl.grantee);
    END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius._match_audit_partition_grants(REGCLASS) IS 'Makes one audit partition hold exactly the grants of the partitioned table, owner and PUBLIC aside.';

DO $$
DECLARE
    v_acl RECORD;
BEGIN
    IF coalesce((SELECT c.relispartition FROM pg_class c WHERE c.oid = to_regclass('claimius.audit_default')), TRUE) THEN
        RETURN;
    END IF;
    ALTER TABLE claimius.audit ATTACH PARTITION claimius.audit_default DEFAULT;
    FOR v_acl IN
        SELECT a.privilege_type, pg_get_userbyid(a.grantee) AS grantee
        FROM pg_class c, aclexplode(c.relacl) a
        WHERE c.oid = 'claimius.audit_default'::REGCLASS AND a.grantee <> 0 AND a.grantee <> c.relowner
    LOOP
        EXECUTE format('GRANT %s ON claimius.audit TO %I', v_acl.privilege_type, v_acl.grantee);
    END LOOP;
    PERFORM claimius._match_audit_partition_grants('claimius.audit_default');
    IF to_regproc('claimius.update_timestamp') IS NOT NULL THEN
        CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.audit
            FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS claimius.audit_default PARTITION OF claimius.audit DEFAULT;

COMMENT ON TABLE claimius.audit IS 'Append only audit log, partitioned by month on sa_created_at. sa_created_by may be NULL when no actor applies (system triggers, replay, denied attempts).';
COMMENT ON COLUMN claimius.audit.sa_created_by IS 'Acting user_claim.id, or NULL for system events with no clear actor.';

CREATE INDEX IF NOT EXISTS idx_audit_sa_owner_id ON claimius.audit (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_audit_sa_created_at ON claimius.audit USING brin (sa_created_at);
CREATE INDEX IF NOT EXISTS idx_audit_object ON claimius.audit (object_type, object_id);

DROP FUNCTION IF EXISTS claimius.ensure_audit_partition(timestamp with time zone);
CREATE OR REPLACE FUNCTION claimius.upsert_audit_partition(p_at TIMESTAMPTZ)
    RETURNS VOID AS $$
DECLARE
    v_from  TIMESTAMPTZ := date_trunc('month', p_at, 'UTC');
    v_to    TIMESTAMPTZ := date_trunc('month', p_at, 'UTC') + INTERVAL '1 month';
    v_name  TEXT := 'audit_' || to_char(p_at AT TIME ZONE 'UTC', 'YYYYMM');
BEGIN
    IF to_regclass('claimius.' || v_name) IS NOT NULL THEN
        RETURN;
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended('claimius.audit_partition', 0));
    IF to_regclass('claimius.' || v_name) IS NOT NULL THEN
        RETURN;
    END IF;
    EXECUTE format('CREATE TABLE claimius.%I (LIKE claimius.audit INCLUDING DEFAULTS)', v_name);
    EXECUTE format('WITH moved AS (DELETE FROM claimius.audit_default WHERE sa_created_at >= $1 AND sa_created_at < $2 RETURNING *)
                    INSERT INTO claimius.%I SELECT * FROM moved', v_name) USING v_from, v_to;
    EXECUTE format('ALTER TABLE claimius.audit ATTACH PARTITION claimius.%I FOR VALUES FROM (%L) TO (%L)', v_name, v_from, v_to);
    PERFORM claimius._match_audit_partition_grants(('claimius.' || quote_ident(v_name))::REGCLASS);
EXCEPTION WHEN duplicate_table THEN
    RETURN;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.upsert_audit_partition(TIMESTAMPTZ) IS 'Creates the audit partition of the UTC month holding p_at when it is missing, moving that month''s rows out of audit_default. Owner rights, so a writer that only inserts audit rows can open a month.';

DO $$
DECLARE
    v_month TIMESTAMPTZ;
BEGIN
    FOR v_month IN SELECT DISTINCT date_trunc('month', a.sa_created_at, 'UTC') FROM claimius.audit_default a LOOP
        PERFORM claimius.upsert_audit_partition(v_month);
    END LOOP;
END $$;

-- ----------------------------------------------------------------------------
-- 9. Extensibility
-- ----------------------------------------------------------------------------

-- object_field
-- Per app schema definition for custom fields attached to a given
-- object_type.
CREATE TABLE IF NOT EXISTS claimius.object_field (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     object_type     TEXT NOT NULL,
                                                     name            TEXT NOT NULL,
                                                     description     TEXT,
                                                     json_schema     JSONB,
                                                     required        BOOLEAN NOT NULL DEFAULT FALSE,
                                                     value           TEXT,
                                                     sa_owner_id     UUID NOT NULL REFERENCES claimius.organization(id),
                                                     sa_created_by   UUID,
                                                     sa_deleted_at   TIMESTAMPTZ,
                                                     sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.object_field IS 'Per app custom field definitions for an object_type.';

CREATE INDEX IF NOT EXISTS idx_object_field_object_type ON claimius.object_field (object_type);
CREATE INDEX IF NOT EXISTS idx_object_field_sa_deleted_at ON claimius.object_field (sa_deleted_at);

-- user_field
-- User scoped value for an object_field. Lets each user supply their own
-- data against an app's custom fields.
CREATE TABLE IF NOT EXISTS claimius.user_field (
                                                   id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                   field_id        UUID NOT NULL REFERENCES claimius.object_field(id),
                                                   user_id         UUID NOT NULL,
                                                   value           TEXT,
                                                   sa_owner_id     UUID NOT NULL REFERENCES claimius.organization(id),
                                                   sa_created_by   UUID,
                                                   sa_deleted_at   TIMESTAMPTZ,
                                                   sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.user_field IS 'Per user value for an object_field.';

CREATE INDEX IF NOT EXISTS idx_user_field_field_id ON claimius.user_field (field_id);
CREATE INDEX IF NOT EXISTS idx_user_field_user_id ON claimius.user_field (user_id);
CREATE INDEX IF NOT EXISTS idx_user_field_sa_deleted_at ON claimius.user_field (sa_deleted_at);

-- user_relation
-- Lightweight per user pinning, priority, and tagging against arbitrary
-- objects. Used for recents, favourites, per app object tagging.
CREATE TABLE IF NOT EXISTS claimius.user_relation (
                                                      id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                      user_id         UUID NOT NULL,
                                                      description     TEXT,
                                                      object_type     VARCHAR NOT NULL,
                                                      object_id       UUID NOT NULL,
                                                      pinned          INTEGER,
                                                      priority        INTEGER,
                                                      used_at         TIMESTAMPTZ DEFAULT now(),
                                                      parent_id       UUID REFERENCES claimius.user_relation(id),
                                                      sibling_id      UUID REFERENCES claimius.user_relation(id),
                                                      sa_deleted_at   TIMESTAMPTZ,
                                                      sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                      sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.user_relation IS 'Per user pin, priority, and tag against any object.';

CREATE INDEX IF NOT EXISTS idx_user_relation_user_id ON claimius.user_relation (user_id);
CREATE INDEX IF NOT EXISTS idx_user_relation_object ON claimius.user_relation (object_type, object_id);
CREATE INDEX IF NOT EXISTS idx_user_relation_sa_deleted_at ON claimius.user_relation (sa_deleted_at);
CREATE INDEX IF NOT EXISTS idx_user_relation_parent_id ON claimius.user_relation (parent_id);
CREATE INDEX IF NOT EXISTS idx_user_relation_sibling_id ON claimius.user_relation (sibling_id);
