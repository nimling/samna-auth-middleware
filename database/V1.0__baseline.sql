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

CREATE TABLE IF NOT EXISTS samna_auth.samna_client (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    object_type TEXT NOT NULL UNIQUE,
    updated_at TIMESTAMP NOT NULL DEFAULT '1970-01-01',
    updated_id UUID,
    deleted_at TIMESTAMP NOT NULL DEFAULT '1970-01-01',
    deleted_id UUID,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    removed_at TIMESTAMP DEFAULT NULL
);
