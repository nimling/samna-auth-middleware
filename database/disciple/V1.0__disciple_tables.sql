-- ============================================================================
-- Claimius V1.0 Disciple Tables
-- ----------------------------------------------------------------------------
-- The disciple table set. Single app: a disciple mirrors exactly one app, so
-- app_id is dropped from every table, every foreign key to samna_app, every
-- index, and every composite unique. The Go sync layer filters incoming rows
-- by the payload app_id and strips it before upsert, so app_id never reaches
-- the disciple schema. Enums and the registry tables live in the shared set.
--
-- Differences from the prophet set:
--   * No app_id column anywhere.
--   * samna_app is kept (one row, the mirrored app; it never had app_id).
--   * No sync_event, no prophet_state. The disciple consumes from the
--     prophet over HTTP, it does not store the event stream.
--   * disciple_state and reconcile_queue are added (reconcile_queue without
--     app_id).
--   * Identity helpers resolve the single app via disciple_state.
-- ============================================================================

-- ----------------------------------------------------------------------------
-- 1. Identity
-- ----------------------------------------------------------------------------

-- samna_app
-- The mirrored app. A disciple holds the single row for the app it mirrors,
-- received via sync. samna_app has no app_id column; it is the app.
CREATE TABLE IF NOT EXISTS claimius.samna_app (
                                                  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                  slug            TEXT UNIQUE NOT NULL,
                                                  name            TEXT NOT NULL,
                                                  description     TEXT DEFAULT '',
                                                  contact_email   TEXT,
                                                  app_image       TEXT,
                                                  redirect_uri    TEXT NOT NULL DEFAULT '',
                                                  sync_uri        TEXT NOT NULL DEFAULT '',
                                                  provider_ids    UUID[] NOT NULL DEFAULT ARRAY[]::UUID[],
                                                  status          claimius.app_status NOT NULL DEFAULT 'active',
                                                  private_key     TEXT NOT NULL,
                                                  private_seed    TEXT NOT NULL,
                                                  app_secret      TEXT,
                                                  secret_version  INTEGER NOT NULL DEFAULT 1,
                                                  claim_id        UUID,
                                                  style           JSONB,
                                                  sa_owner_id     UUID NOT NULL,
                                                  sa_created_by   UUID NOT NULL,
                                                  sa_deleted_at   TIMESTAMPTZ,
                                                  sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                  sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.samna_app IS 'The single mirrored app for this disciple.';

CREATE INDEX IF NOT EXISTS idx_samna_app_sa_deleted_at ON claimius.samna_app (sa_deleted_at);
CREATE INDEX IF NOT EXISTS idx_samna_app_status ON claimius.samna_app (status);
CREATE INDEX IF NOT EXISTS idx_samna_app_sa_owner_id ON claimius.samna_app (sa_owner_id);

-- samna_user
-- The canonical user record. One row per user_id on the disciple, since the
-- disciple is single app. user_id is the stable application identifier
-- referenced everywhere; id is the row PK.
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

COMMENT ON TABLE claimius.samna_user IS 'User record. One row per user_id on the single app disciple.';
COMMENT ON COLUMN claimius.samna_user.user_id IS 'Stable application identifier. Used in all references.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_samna_user_user
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
-- Secret store. Keyed by key when not soft deleted.
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

COMMENT ON TABLE claimius.samna_secret IS 'Secret store for the disciple app.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_samna_secret_key
    ON claimius.samna_secret (key) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_samna_secret_sa_owner_id ON claimius.samna_secret (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_samna_secret_sa_deleted_at ON claimius.samna_secret (sa_deleted_at);

-- ----------------------------------------------------------------------------
-- 2. Hierarchy roots
-- ----------------------------------------------------------------------------

-- organization
-- The org tree. Self references through sa_owner_id. Root iff
-- sa_owner_id == sa_root_id == id.
CREATE TABLE IF NOT EXISTS claimius.organization (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     name            VARCHAR(255) NOT NULL,
                                                     description     TEXT,
                                                     type            VARCHAR(50),
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
-- 3. Claims and bindings
-- ----------------------------------------------------------------------------

-- claim
-- A named permission template. Carries a bitwise access mask and is owned
-- by an organization.
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
                                              sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.claim IS 'Permission template. Access is a bitwise mask.';
COMMENT ON COLUMN claimius.claim.sa_access IS 'Bitwise access mask. Bits: 0x01 owner, 0x02 write, 0x04 read, 0x08 execute, 0x10 deny. When the deny bit is set the other bits enumerate which permissions to subtract.';
COMMENT ON COLUMN claimius.claim.inherits IS 'When true, claim_object bindings cascade through hierarchy.';

CREATE INDEX IF NOT EXISTS idx_claim_sa_owner_id ON claimius.claim (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_claim_sa_root_id ON claimius.claim (sa_root_id);
CREATE INDEX IF NOT EXISTS idx_claim_deny_bit ON claimius.claim ((sa_access & 16)) WHERE (sa_access & 16) <> 0;
CREATE INDEX IF NOT EXISTS idx_claim_sa_deleted_at ON claimius.claim (sa_deleted_at);

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
                                                   sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.user_claim IS 'Grants a claim to a user. id is referenced as sa_created_by everywhere.';
COMMENT ON COLUMN claimius.user_claim.user_id IS 'References samna_user.user_id, not samna_user.id.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_claim_unique
    ON claimius.user_claim (claim_id, user_id) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_claim_user ON claimius.user_claim (user_id);
CREATE INDEX IF NOT EXISTS idx_user_claim_claim_id ON claimius.user_claim (claim_id);
CREATE INDEX IF NOT EXISTS idx_user_claim_sa_owner_id ON claimius.user_claim (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_user_claim_sa_deleted_at ON claimius.user_claim (sa_deleted_at);

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
                                                     CONSTRAINT claim_object_object_pair CHECK (
                                                         (object_id IS NULL AND object_type IS NULL)
                                                      OR (object_id IS NOT NULL AND object_type IS NOT NULL)
                                                     )
);

COMMENT ON TABLE claimius.claim_object IS 'Binds a claim. With (object_type, object_id) scope keys are field names; without them scope keys are custom.';
COMMENT ON COLUMN claimius.claim_object.sa_access IS 'Bitwise access mask with bits 0x01 owner, 0x02 write, 0x04 read, 0x08 execute, 0x10 deny; NULL means inherit the claim sa_access at read time.';
COMMENT ON COLUMN claimius.claim_object.scope IS 'Object scoped: keys are field names, NULL means whole object. Object-less: keys are custom, NULL means no grant.';

CREATE INDEX IF NOT EXISTS idx_claim_object_claim_id ON claimius.claim_object (claim_id);
CREATE INDEX IF NOT EXISTS idx_claim_object_object ON claimius.claim_object (object_type, object_id) WHERE object_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_claim_object_no_object ON claimius.claim_object (claim_id) WHERE object_id IS NULL AND sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_claim_object_sa_owner_id ON claimius.claim_object (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_claim_object_sa_deleted_at ON claimius.claim_object (sa_deleted_at);
CREATE INDEX IF NOT EXISTS idx_claim_object_lookup
    ON claimius.claim_object (claim_id, object_type, object_id)
    WHERE sa_deleted_at IS NULL AND object_id IS NOT NULL;

-- user_token
-- Debug only bridge from JWT identity (external_id under id_claim) to
-- claimius.samna_user.user_id. Not synced.
CREATE TABLE IF NOT EXISTS claimius.user_token (
                                                   id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                   user_id         UUID NOT NULL,
                                                   id_claim        TEXT NOT NULL DEFAULT 'sub',
                                                   external_id     TEXT NOT NULL,
                                                   claim_id        UUID REFERENCES claimius.claim(id),
                                                   data            JSONB,
                                                   sa_owner_id     UUID NOT NULL,
                                                   sa_created_by   UUID,
                                                   sa_deleted_at   TIMESTAMPTZ,
                                                   sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.user_token IS 'Debug only bridge from JWT identity (external_id under id_claim) to claimius.samna_user.user_id. Not synced.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_token_claim_external
    ON claimius.user_token (sa_owner_id, id_claim, external_id) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_token_user_id ON claimius.user_token (user_id);
CREATE INDEX IF NOT EXISTS idx_user_token_sa_owner_id ON claimius.user_token (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_user_token_sa_deleted_at ON claimius.user_token (sa_deleted_at);

-- ----------------------------------------------------------------------------
-- 4. Materialized access
-- ----------------------------------------------------------------------------

-- user_object
-- The reverse access index. One row per (user_id, object_type, object_id)
-- when the user has any access. Carries denormalized rendering fields and a
-- tsvector for search. The id is a deterministic UUID hash produced by
-- claimius.composite_id so prophet and disciple converge on the same row
-- identity (the prophet hashes app_id in; the disciple receives the id).
CREATE TABLE IF NOT EXISTS claimius.user_object (
                                                    id                  UUID PRIMARY KEY,
                                                    user_id             UUID NOT NULL,
                                                    object_id           UUID NOT NULL,
                                                    object_type         TEXT NOT NULL,
                                                    sa_access           INTEGER NOT NULL,
                                                    grants              JSONB NOT NULL DEFAULT '[]'::jsonb,
                                                    scope               JSONB,
                                                    direct_grant        BOOLEAN NOT NULL DEFAULT FALSE,
                                                    sa_owner_id         UUID,
                                                    sa_location_id      UUID,
                                                    sa_root_id          UUID,
                                                    sa_name             TEXT,
                                                    sa_description      TEXT,
                                                    sa_link             TEXT,
                                                    search_vector       TSVECTOR,
                                                    sa_created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                    sa_updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.user_object IS 'Reverse access index. Read path. One row per (user, object) with access.';
COMMENT ON COLUMN claimius.user_object.grants IS 'JSON array of {claim_id, access, scope, cascaded_from, tree_type}. Empty when only direct.';
COMMENT ON COLUMN claimius.user_object.direct_grant IS 'True when user is creator (via sa_created_by lookup).';
COMMENT ON COLUMN claimius.user_object.sa_access IS 'Effective bitwise access mask: OR of grants[].access with deny bits subtracted. Bits: 0x01 owner, 0x02 write, 0x04 read, 0x08 execute. Direct creator gets 0x01|0x02|0x04|0x08.';
COMMENT ON COLUMN claimius.user_object.scope IS 'Effective merged scope: union of grants[].scope keys when any grant has a scope, else NULL meaning whole object access.';

CREATE INDEX IF NOT EXISTS idx_user_object_user_type ON claimius.user_object (user_id, object_type, sa_access);
CREATE INDEX IF NOT EXISTS idx_user_object_object ON claimius.user_object (object_id, object_type);
CREATE INDEX IF NOT EXISTS idx_user_object_owner ON claimius.user_object (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_user_object_location ON claimius.user_object (sa_location_id) WHERE sa_location_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_user_object_root ON claimius.user_object (sa_root_id);
CREATE INDEX IF NOT EXISTS idx_user_object_grants ON claimius.user_object USING GIN (grants jsonb_path_ops);
CREATE INDEX IF NOT EXISTS idx_user_object_search ON claimius.user_object USING GIN (search_vector);

-- object_users
-- Inverse of user_object. For each object, lists users with access. Used to
-- compute user_users.
CREATE TABLE IF NOT EXISTS claimius.object_users (
                                                     id              UUID PRIMARY KEY,
                                                     object_id       UUID NOT NULL,
                                                     object_type     TEXT NOT NULL,
                                                     user_id         UUID NOT NULL,
                                                     sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.object_users IS 'Inverse of user_object. (object, user) pairs with access.';

CREATE INDEX IF NOT EXISTS idx_object_users_object ON claimius.object_users (object_id, object_type);
CREATE INDEX IF NOT EXISTS idx_object_users_user ON claimius.object_users (user_id);

-- user_users
-- Records that two users share at least one accessible object. Used by
-- get_users to list visible users.
CREATE TABLE IF NOT EXISTS claimius.user_users (
                                                   id                      UUID PRIMARY KEY,
                                                   viewer_id               UUID NOT NULL,
                                                   target_user_id          UUID NOT NULL,
                                                   first_shared_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   last_shared_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   sharing_object_count    INTEGER NOT NULL DEFAULT 1
);

COMMENT ON TABLE claimius.user_users IS 'Pairs of users sharing at least one accessible object.';

CREATE INDEX IF NOT EXISTS idx_user_users_viewer ON claimius.user_users (viewer_id);
CREATE INDEX IF NOT EXISTS idx_user_users_target ON claimius.user_users (target_user_id);

-- ----------------------------------------------------------------------------
-- 5. Sync (disciple side)
-- ----------------------------------------------------------------------------

-- disciple_state
-- Singleton cursor on the disciple side. Tracks the highest sync_event.seq
-- already applied locally. disciple_app_slug records which app this disciple
-- mirrors from upstream prophet; set by init_disciple.
CREATE TABLE IF NOT EXISTS claimius.disciple_state (
                                                       id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                       last_applied_seq    BIGINT NOT NULL DEFAULT 0,
                                                       disciple_app_slug   TEXT NOT NULL,
                                                       sa_updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.disciple_state IS 'Singleton consumption cursor for the disciple.';

-- reconcile_queue
-- Disciple side retry queue. When applying a sync_event fails, the
-- (user, object, type) triple is queued here. reconcile_if_pending drains
-- entries inline at the top of every read for that user.
CREATE TABLE IF NOT EXISTS claimius.reconcile_queue (
                                                        id              UUID PRIMARY KEY,
                                                        user_id         UUID NOT NULL,
                                                        object_id       UUID NOT NULL,
                                                        object_type     TEXT NOT NULL,
                                                        sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.reconcile_queue IS 'Disciple side retry queue for failed event applications.';

CREATE INDEX IF NOT EXISTS idx_reconcile_queue_user ON claimius.reconcile_queue (user_id);

-- ----------------------------------------------------------------------------
-- 6. Cache
-- ----------------------------------------------------------------------------

-- claim_graph_cache
-- Per user materialized result of claimius.get_claim_graph for the default
-- argument shape. Invalidated by triggers on user_object, user_users,
-- samna_user, claim, and claim_object.
CREATE TABLE IF NOT EXISTS claimius.claim_graph_cache (
                                                          user_id         UUID NOT NULL,
                                                          graph           JSONB NOT NULL,
                                                          sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                          PRIMARY KEY (user_id)
);

COMMENT ON TABLE claimius.claim_graph_cache IS 'Per user cache of get_claim_graph default shape. Trigger invalidated.';
COMMENT ON COLUMN claimius.claim_graph_cache.graph IS 'JSON array of {object_id, object_type, label, parent_id, data} tuples.';

-- ----------------------------------------------------------------------------
-- 7. App identity helpers
-- Resolve the single mirrored app via disciple_state. No app_id is used in
-- any disciple query; these exist so the sync layer and client can name the
-- app this disciple mirrors.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_disciple_app()
    RETURNS SETOF claimius.samna_app AS $$
DECLARE
    v_slug TEXT;
BEGIN
    SELECT disciple_app_slug INTO v_slug
    FROM claimius.disciple_state
    LIMIT 1;

    IF v_slug IS NULL THEN
        RAISE EXCEPTION 'get_disciple_app: no disciple app registered. init_disciple has not run.';
    END IF;

    RETURN QUERY
        SELECT * FROM claimius.samna_app
        WHERE slug = v_slug AND sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION claimius.get_disciple_app IS 'Returns the upstream prophet app this disciple mirrors.';

CREATE OR REPLACE FUNCTION claimius.get_disciple_app_id()
    RETURNS UUID AS $$
SELECT id FROM claimius.get_disciple_app();
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_disciple_app_id IS 'Scalar wrapper over get_disciple_app. Returns the mirrored app id.';

-- ----------------------------------------------------------------------------
-- 8. Audit
-- ----------------------------------------------------------------------------

-- audit
-- Append only audit log. sa_created_by is nullable on this table only.
CREATE TABLE IF NOT EXISTS claimius.audit (
                                              id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                              operation       VARCHAR NOT NULL,
                                              message         TEXT DEFAULT '',
                                              type            VARCHAR NOT NULL,
                                              object_type     VARCHAR NOT NULL,
                                              object_id       UUID NOT NULL,
                                              sa_owner_id     UUID NOT NULL,
                                              sa_created_by   UUID,
                                              sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                              sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.audit IS 'Append only audit log. sa_created_by may be NULL when no actor applies (system triggers, replay, denied attempts).';
COMMENT ON COLUMN claimius.audit.sa_created_by IS 'Acting user_claim.id, or NULL for system events with no clear actor.';

CREATE INDEX IF NOT EXISTS idx_audit_sa_owner_id ON claimius.audit (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_audit_sa_created_at ON claimius.audit (sa_created_at);
CREATE INDEX IF NOT EXISTS idx_audit_object ON claimius.audit (object_type, object_id);

-- ----------------------------------------------------------------------------
-- 9. Extensibility
-- ----------------------------------------------------------------------------

-- object_field
-- Schema definition for custom fields attached to a given object_type.
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

COMMENT ON TABLE claimius.object_field IS 'Custom field definitions for an object_type.';

CREATE INDEX IF NOT EXISTS idx_object_field_object_type ON claimius.object_field (object_type);
CREATE INDEX IF NOT EXISTS idx_object_field_sa_deleted_at ON claimius.object_field (sa_deleted_at);

-- user_field
-- User scoped value for an object_field.
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
-- objects.
CREATE TABLE IF NOT EXISTS claimius.user_relation (
                                                      id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                      user_id         UUID NOT NULL,
                                                      description     TEXT,
                                                      object_type     VARCHAR NOT NULL,
                                                      object_id       UUID NOT NULL,
                                                      pinned          INTEGER,
                                                      priority        INTEGER,
                                                      used_at         TIMESTAMPTZ DEFAULT now(),
                                                      sa_deleted_at   TIMESTAMPTZ,
                                                      sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                      sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.user_relation IS 'Per user pin, priority, and tag against any object.';

CREATE INDEX IF NOT EXISTS idx_user_relation_user_id ON claimius.user_relation (user_id);
CREATE INDEX IF NOT EXISTS idx_user_relation_object ON claimius.user_relation (object_type, object_id);
CREATE INDEX IF NOT EXISTS idx_user_relation_sa_deleted_at ON claimius.user_relation (sa_deleted_at);
