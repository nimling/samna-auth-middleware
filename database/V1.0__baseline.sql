CREATE SCHEMA IF NOT EXISTS samna_auth;

DO $$ BEGIN
    CREATE TYPE samna_auth.claim_type AS ENUM ('system', 'user', 'service', 'virtual');
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

CREATE TABLE IF NOT EXISTS samna_auth.samna_user (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    first_name VARCHAR(255),
    last_name VARCHAR(255),
    phone VARCHAR(50),
    email VARCHAR(255) UNIQUE NOT NULL,
    user_id UUID UNIQUE NOT NULL,
    status VARCHAR(50),
    type VARCHAR(50),
    config JSONB DEFAULT NULL,
    sa_deleted_at TIMESTAMP DEFAULT NULL,
    sa_created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sa_updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

DROP INDEX IF EXISTS samna_auth.idx_samna_user_user_id;
CREATE INDEX idx_samna_user_user_id ON samna_auth.samna_user(user_id);
DROP INDEX IF EXISTS samna_auth.idx_samna_user_email;
CREATE INDEX idx_samna_user_email ON samna_auth.samna_user(email);
DROP INDEX IF EXISTS samna_auth.idx_samna_user_sa_deleted_at;
CREATE INDEX idx_samna_user_sa_deleted_at ON samna_auth.samna_user(sa_deleted_at);

CREATE TABLE IF NOT EXISTS samna_auth.claim (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT,
    sa_access_level INTEGER NOT NULL,
    inherits BOOLEAN NOT NULL DEFAULT TRUE,
    type samna_auth.claim_type NOT NULL DEFAULT 'user',
    sa_created_by UUID,
    sa_owner_id UUID NOT NULL,
    sa_root_id UUID NOT NULL,
    sa_deleted_at TIMESTAMP DEFAULT NULL,
    sa_created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sa_updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

DROP INDEX IF EXISTS samna_auth.idx_claim_sa_owner_id;
CREATE INDEX idx_claim_sa_owner_id ON samna_auth.claim(sa_owner_id);
DROP INDEX IF EXISTS samna_auth.idx_claim_sa_root_id;
CREATE INDEX idx_claim_sa_root_id ON samna_auth.claim(sa_root_id);
DROP INDEX IF EXISTS samna_auth.idx_claim_sa_deleted_at;
CREATE INDEX idx_claim_sa_deleted_at ON samna_auth.claim(sa_deleted_at);

CREATE TABLE IF NOT EXISTS samna_auth.claim_object (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reason TEXT,
    function TEXT,
    claim_id UUID NOT NULL REFERENCES samna_auth.claim(id),
    object_id UUID NOT NULL,
    object_type TEXT NOT NULL,
    sa_level INTEGER NOT NULL DEFAULT 0,
    inherits BOOLEAN NOT NULL DEFAULT TRUE,
    ref_id UUID,
    sa_root_id UUID NOT NULL,
    sa_created_by UUID,
    sa_owner_id UUID NOT NULL,
    sa_deleted_at TIMESTAMP DEFAULT NULL,
    sa_created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sa_updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

DROP INDEX IF EXISTS samna_auth.idx_claim_object_claim_id;
CREATE INDEX idx_claim_object_claim_id ON samna_auth.claim_object(claim_id);
DROP INDEX IF EXISTS samna_auth.idx_claim_object_object_id;
CREATE INDEX idx_claim_object_object_id ON samna_auth.claim_object(object_id);
DROP INDEX IF EXISTS samna_auth.idx_claim_object_object_type;
CREATE INDEX idx_claim_object_object_type ON samna_auth.claim_object(object_type);
DROP INDEX IF EXISTS samna_auth.idx_claim_object_sa_deleted_at;
CREATE INDEX idx_claim_object_sa_deleted_at ON samna_auth.claim_object(sa_deleted_at);

CREATE TABLE IF NOT EXISTS samna_auth.user_claim (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    claim_id UUID NOT NULL REFERENCES samna_auth.claim(id),
    user_id UUID NOT NULL,
    reason VARCHAR,
    starts_at TIMESTAMP,
    ends_at TIMESTAMP,
    sa_created_by UUID,
    sa_owner_id UUID NOT NULL,
    sa_deleted_at TIMESTAMP DEFAULT NULL,
    sa_created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sa_updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

DROP INDEX IF EXISTS samna_auth.idx_user_claim_claim_id;
CREATE INDEX idx_user_claim_claim_id ON samna_auth.user_claim(claim_id);
DROP INDEX IF EXISTS samna_auth.idx_user_claim_user_id;
CREATE INDEX idx_user_claim_user_id ON samna_auth.user_claim(user_id);
DROP INDEX IF EXISTS samna_auth.idx_user_claim_sa_owner_id;
CREATE INDEX idx_user_claim_sa_owner_id ON samna_auth.user_claim(sa_owner_id);
DROP INDEX IF EXISTS samna_auth.idx_user_claim_sa_deleted_at;
CREATE INDEX idx_user_claim_sa_deleted_at ON samna_auth.user_claim(sa_deleted_at);

CREATE TABLE IF NOT EXISTS samna_auth.organization (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    sa_root_id UUID REFERENCES samna_auth.organization(id),
    sa_owner_id UUID REFERENCES samna_auth.organization(id),
    type VARCHAR(50),
    sa_level INTEGER NOT NULL DEFAULT 0,
    external_id VARCHAR(255),
    provider_id UUID,
    sa_deleted_at TIMESTAMP DEFAULT NULL,
    sa_created_by UUID NOT NULL,
    sa_created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sa_updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

DROP INDEX IF EXISTS samna_auth.idx_organization_sa_root_id;
CREATE INDEX idx_organization_sa_root_id ON samna_auth.organization(sa_root_id);
DROP INDEX IF EXISTS samna_auth.idx_organization_sa_owner_id;
CREATE INDEX idx_organization_sa_owner_id ON samna_auth.organization(sa_owner_id);
DROP INDEX IF EXISTS samna_auth.idx_organization_external_id;
CREATE INDEX idx_organization_external_id ON samna_auth.organization(external_id);
DROP INDEX IF EXISTS samna_auth.idx_organization_provider_id;
CREATE INDEX idx_organization_provider_id ON samna_auth.organization(provider_id);

CREATE TABLE IF NOT EXISTS samna_auth.location (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sa_owner_id UUID NOT NULL REFERENCES samna_auth.organization(id),
    sa_root_id UUID NOT NULL REFERENCES samna_auth.organization(id),
    name VARCHAR(255),
    description TEXT,
    sa_parent_id UUID REFERENCES samna_auth.location(id),
    type VARCHAR(50) NOT NULL DEFAULT 'room',
    longitude DOUBLE PRECISION,
    latitude DOUBLE PRECISION,
    sa_level INTEGER NOT NULL DEFAULT 0,
    code_type VARCHAR(50),
    code VARCHAR(255),
    external_id VARCHAR(255),
    provider_id UUID,
    sa_created_by UUID NOT NULL,
    sa_deleted_at TIMESTAMP DEFAULT NULL,
    sa_created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sa_updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

DROP INDEX IF EXISTS samna_auth.idx_location_sa_owner_id;
CREATE INDEX idx_location_sa_owner_id ON samna_auth.location(sa_owner_id);
DROP INDEX IF EXISTS samna_auth.idx_location_sa_root_id;
CREATE INDEX idx_location_sa_root_id ON samna_auth.location(sa_root_id);
DROP INDEX IF EXISTS samna_auth.idx_location_sa_parent_id;
CREATE INDEX idx_location_sa_parent_id ON samna_auth.location(sa_parent_id);
DROP INDEX IF EXISTS samna_auth.idx_location_external_id;
CREATE INDEX idx_location_external_id ON samna_auth.location(external_id);
DROP INDEX IF EXISTS samna_auth.idx_location_provider_id;
CREATE INDEX idx_location_provider_id ON samna_auth.location(provider_id);

CREATE TABLE IF NOT EXISTS samna_auth.audit
(
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    operation     varchar     NOT NULL,
    message       text                 DEFAULT '',
    type          varchar     NOT NULL,
    object_type   varchar     NOT NULL,
    object_id     uuid        NOT NULL,
    sa_created_by uuid        NOT NULL,
    sa_owner_id   uuid        NOT NULL,
    sa_created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sa_updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sa_audit_sa_owner_id ON samna_auth.audit (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_sa_audit_sa_created_at ON samna_auth.audit (sa_created_at);
CREATE INDEX IF NOT EXISTS idx_sa_audit_object_id ON samna_auth.audit (object_id);
CREATE INDEX IF NOT EXISTS idx_sa_audit_object_type ON samna_auth.audit (object_type);

CREATE TABLE IF NOT EXISTS samna_auth.user_relation
(
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid        NOT NULL REFERENCES samna_auth.samna_user (user_id),
    client_id     uuid,
    description   text,
    object_type   varchar     NOT NULL,
    object_id     uuid        NOT NULL,
    pinned        integer,
    priority      integer,
    sa_parent_id  uuid,
    sibling_id    uuid,
    used_at       timestamptz          DEFAULT CURRENT_TIMESTAMP,
    sa_deleted_at timestamptz,
    sa_created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sa_updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sa_user_relation_user_id ON samna_auth.user_relation (user_id);
CREATE INDEX IF NOT EXISTS idx_sa_user_relation_object_id ON samna_auth.user_relation (object_id);
CREATE INDEX IF NOT EXISTS idx_sa_user_relation_object_type ON samna_auth.user_relation (object_type);
CREATE INDEX IF NOT EXISTS idx_sa_user_relation_client_id ON samna_auth.user_relation (client_id);
CREATE INDEX IF NOT EXISTS idx_sa_user_relation_sa_deleted_at ON samna_auth.user_relation (sa_deleted_at);

CREATE OR REPLACE FUNCTION samna_auth.composite_id(VARIADIC parts text[])
RETURNS text AS $$
    SELECT array_to_string(parts, ':');
$$ LANGUAGE sql IMMUTABLE;

CREATE OR REPLACE FUNCTION samna_auth.decompose_id(p_id text)
RETURNS text[] AS $$
    SELECT string_to_array(p_id, ':');
$$ LANGUAGE sql IMMUTABLE;

CREATE TABLE IF NOT EXISTS samna_auth.user_object (
    id               text        NOT NULL PRIMARY KEY,
    user_id          uuid        NOT NULL,
    object_id        uuid        NOT NULL,
    object_type      text        NOT NULL,
    claim_id         uuid        NOT NULL,
    sa_access_level  integer     NOT NULL,
    sa_owner_id      uuid,
    sa_location_id   uuid,
    sa_name          text,
    sa_description   text,
    sa_link          text,
    sa_created_at    timestamptz NOT NULL DEFAULT now(),
    sa_updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_user_object_user_type ON samna_auth.user_object (user_id, object_type, sa_access_level);
CREATE INDEX IF NOT EXISTS idx_user_object_owner ON samna_auth.user_object (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_user_object_location ON samna_auth.user_object (sa_location_id);
CREATE INDEX IF NOT EXISTS idx_user_object_object ON samna_auth.user_object (object_id, object_type);
CREATE INDEX IF NOT EXISTS idx_user_object_claim ON samna_auth.user_object (claim_id);

CREATE TABLE IF NOT EXISTS samna_auth.user_users (
    id                   text        NOT NULL PRIMARY KEY,
    viewer_id            uuid        NOT NULL,
    target_user_id       uuid        NOT NULL,
    first_shared_at      timestamptz NOT NULL DEFAULT now(),
    last_shared_at       timestamptz NOT NULL DEFAULT now(),
    sharing_object_count integer     NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_user_users_viewer ON samna_auth.user_users (viewer_id);
CREATE INDEX IF NOT EXISTS idx_user_users_target ON samna_auth.user_users (target_user_id);

CREATE TABLE IF NOT EXISTS samna_auth.object_users (
    id               text        NOT NULL PRIMARY KEY,
    object_id        uuid        NOT NULL,
    object_type      text        NOT NULL,
    user_id          uuid        NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_object_users_object ON samna_auth.object_users (object_id, object_type);
CREATE INDEX IF NOT EXISTS idx_object_users_user ON samna_auth.object_users (user_id);

CREATE TABLE IF NOT EXISTS samna_auth.user_object_event (
    seq              bigserial   PRIMARY KEY,
    object_type      text        NOT NULL,
    obj              jsonb       NOT NULL,
    written_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_user_object_event_written ON samna_auth.user_object_event (written_at);

CREATE TABLE IF NOT EXISTS samna_auth.reconcile_queue (
    id               text        NOT NULL PRIMARY KEY,
    user_id          uuid        NOT NULL,
    object_id        uuid        NOT NULL,
    object_type      text        NOT NULL,
    sa_created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_reconcile_queue_user ON samna_auth.reconcile_queue (user_id);

CREATE TABLE IF NOT EXISTS samna_auth.client_state (
    id               int PRIMARY KEY DEFAULT 1,
    last_applied_seq bigint      NOT NULL DEFAULT 0,
    sa_updated_at    timestamptz NOT NULL DEFAULT now()
);
