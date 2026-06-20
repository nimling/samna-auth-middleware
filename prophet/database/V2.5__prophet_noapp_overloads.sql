-- ============================================================================
-- Claimius V2.5 Prophet No-app_id Overloads
-- ----------------------------------------------------------------------------
-- The shared Go access layer calls one signature on both roles: no app_id
-- parameter. On the disciple those are the native functions. On the prophet
-- these thin overloads resolve the app via claimius.get_app_id() and delegate
-- to the proven app_id functions, so the prophet stays multi app capable
-- (samna_auth SQL seeds still call the app_id forms) while the shared Go runs
-- unchanged on both roles.
--
-- The object form get_access(UUID, UUID, TEXT, INTEGER) would collide with the
-- app_id scope form, so the scope form is exposed as get_scope_access.
-- ============================================================================

-- ----------------------------------------------------------------------------
-- Read overloads (no app_id; resolve via get_app_id, delegate)
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.get_access(
    p_user_id          UUID,
    p_object_id        UUID,
    p_object_type      TEXT,
    p_required_access  INTEGER DEFAULT 0
) RETURNS TABLE (
                    user_object_id      UUID,
                    user_claim_id       UUID,
                    claim_id            UUID,
                    sa_access           INTEGER,
                    scope               JSONB,
                    sa_owner_id         UUID,
                    sa_root_id          UUID,
                    sa_location_id      UUID,
                    direct_grant        BOOLEAN
                ) AS $$
    SELECT * FROM claimius.get_access(p_user_id, claimius.get_app_id(), p_object_id, p_object_type, p_required_access);
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION claimius.get_direct_access(
    p_user_id       UUID,
    p_object_id     UUID,
    p_object_type   TEXT
) RETURNS TABLE (
                    user_object_id      UUID,
                    user_claim_id       UUID,
                    claim_id            UUID,
                    sa_access           INTEGER,
                    scope               JSONB,
                    sa_owner_id         UUID,
                    sa_root_id          UUID,
                    sa_location_id      UUID,
                    direct_grant        BOOLEAN
                ) AS $$
    SELECT * FROM claimius.get_direct_access(p_user_id, claimius.get_app_id(), p_object_id, p_object_type);
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION claimius.get_objects(
    p_user_id          UUID,
    p_object_type      TEXT,
    p_required_access  INTEGER DEFAULT 0
) RETURNS TABLE(
                   object_id           UUID,
                   user_claim_id       UUID,
                   sa_access           INTEGER,
                   scope               JSONB,
                   sa_owner_id         UUID,
                   sa_location_id      UUID,
                   sa_root_id          UUID,
                   sa_name             TEXT,
                   sa_description      TEXT,
                   sa_link             TEXT,
                   direct_grant        BOOLEAN
               ) AS $$
    SELECT * FROM claimius.get_objects(p_user_id, claimius.get_app_id(), p_object_type, p_required_access);
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION claimius.get_organizations(
    p_user_id UUID, p_required_access INTEGER DEFAULT 0
) RETURNS SETOF claimius.organization AS $$
    SELECT * FROM claimius.get_organizations(p_user_id, claimius.get_app_id(), p_required_access);
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION claimius.get_locations(
    p_user_id UUID, p_required_access INTEGER DEFAULT 0
) RETURNS SETOF claimius.location AS $$
    SELECT * FROM claimius.get_locations(p_user_id, claimius.get_app_id(), p_required_access);
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION claimius.get_users(
    p_user_id UUID, p_required_access INTEGER DEFAULT 0
) RETURNS SETOF claimius.samna_user AS $$
    SELECT * FROM claimius.get_users(p_user_id, claimius.get_app_id(), p_required_access);
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION claimius.get_claims(
    p_user_id UUID, p_required_access INTEGER DEFAULT 0
) RETURNS SETOF claimius.claim AS $$
    SELECT * FROM claimius.get_claims(p_user_id, claimius.get_app_id(), p_required_access);
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION claimius.get_claims(
    p_user_id UUID, p_object_id UUID, p_object_type TEXT
) RETURNS SETOF claimius.claim AS $$
    SELECT * FROM claimius.get_claims(p_user_id, claimius.get_app_id(), p_object_id, p_object_type);
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION claimius.get_audit(
    p_user_id UUID, p_required_access INTEGER DEFAULT 0, p_limit INTEGER DEFAULT 100, p_offset INTEGER DEFAULT 0
) RETURNS SETOF claimius.audit AS $$
    SELECT * FROM claimius.get_audit(p_user_id, claimius.get_app_id(), p_required_access, p_limit, p_offset);
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION claimius.get_secrets(
    p_user_id UUID, p_required_access INTEGER DEFAULT 0
) RETURNS SETOF claimius.samna_secret AS $$
    SELECT * FROM claimius.get_secrets(p_user_id, claimius.get_app_id(), p_required_access);
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION claimius.get_owner_graph(
    p_user_id UUID, p_start_id UUID DEFAULT NULL, p_depth INTEGER DEFAULT 0, p_required_access INTEGER DEFAULT 0
) RETURNS TABLE(object_id UUID, object_type TEXT, label TEXT, parent_id UUID, data JSONB) AS $$
    SELECT * FROM claimius.get_owner_graph(p_user_id, claimius.get_app_id(), p_start_id, p_depth, p_required_access);
$$ LANGUAGE sql;

CREATE OR REPLACE FUNCTION claimius.get_claim_graph(
    p_user_id UUID, p_start_id UUID DEFAULT NULL, p_depth INTEGER DEFAULT 0, p_required_access INTEGER DEFAULT 0
) RETURNS TABLE(object_id UUID, object_type TEXT, label TEXT, parent_id UUID, data JSONB) AS $$
    SELECT * FROM claimius.get_claim_graph(p_user_id, claimius.get_app_id(), p_start_id, p_depth, p_required_access);
$$ LANGUAGE sql;

CREATE OR REPLACE FUNCTION claimius.search(
    p_user_id UUID, p_query TEXT, p_object_types TEXT[] DEFAULT NULL,
    p_required_access INTEGER DEFAULT 0, p_limit INTEGER DEFAULT 20, p_offset INTEGER DEFAULT 0
) RETURNS TABLE(
                   object_id UUID, object_type TEXT, user_claim_id UUID, name TEXT, description TEXT,
                   link TEXT, sa_access INTEGER, scope JSONB, owner_id UUID, location_id UUID,
                   is_direct_grant BOOLEAN, rank REAL
               ) AS $$
    SELECT * FROM claimius.search(p_user_id, claimius.get_app_id(), p_query, p_object_types, p_required_access, p_limit, p_offset);
$$ LANGUAGE sql STABLE;

-- ----------------------------------------------------------------------------
-- Write overloads (no app_id; resolve via get_app_id, delegate)
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION claimius.create_claim(
    p_name TEXT, p_description TEXT, p_sa_access INTEGER, p_sa_owner_id UUID, p_sa_root_id UUID,
    p_sa_created_by UUID, p_inherits BOOLEAN DEFAULT TRUE, p_type claimius.claim_type DEFAULT 'user'
) RETURNS JSONB AS $$
    SELECT claimius.create_claim(claimius.get_app_id(), p_name, p_description, p_sa_access, p_sa_owner_id, p_sa_root_id, p_sa_created_by, p_inherits, p_type);
$$ LANGUAGE sql;

CREATE OR REPLACE FUNCTION claimius.assign_claim_user(
    p_claim_id UUID, p_user_id UUID, p_sa_owner_id UUID, p_sa_created_by UUID,
    p_reason TEXT DEFAULT NULL, p_starts_at TIMESTAMPTZ DEFAULT NULL, p_ends_at TIMESTAMPTZ DEFAULT NULL
) RETURNS JSONB AS $$
    SELECT claimius.assign_claim_user(claimius.get_app_id(), p_claim_id, p_user_id, p_sa_owner_id, p_sa_created_by, p_reason, p_starts_at, p_ends_at);
$$ LANGUAGE sql;

CREATE OR REPLACE FUNCTION claimius.assign_claim_object(
    p_claim_id UUID, p_sa_owner_id UUID, p_sa_root_id UUID, p_sa_created_by UUID,
    p_object_id UUID DEFAULT NULL, p_object_type TEXT DEFAULT NULL, p_sa_access INTEGER DEFAULT NULL,
    p_inherits BOOLEAN DEFAULT TRUE, p_reason TEXT DEFAULT NULL, p_ref_id UUID DEFAULT NULL, p_scope JSONB DEFAULT NULL
) RETURNS JSONB AS $$
    SELECT claimius.assign_claim_object(claimius.get_app_id(), p_claim_id, p_sa_owner_id, p_sa_root_id, p_sa_created_by, p_object_id, p_object_type, p_sa_access, p_inherits, p_reason, p_ref_id, p_scope);
$$ LANGUAGE sql;

CREATE OR REPLACE FUNCTION claimius.ensure_app_user(
    p_user_id UUID, p_first_name TEXT DEFAULT NULL, p_last_name TEXT DEFAULT NULL,
    p_user_name TEXT DEFAULT NULL, p_user_image TEXT DEFAULT NULL, p_email TEXT DEFAULT NULL,
    p_phone TEXT DEFAULT NULL, p_external_id TEXT DEFAULT NULL
) RETURNS JSONB AS $$
    SELECT claimius.ensure_app_user(p_user_id, claimius.get_app_id(), p_first_name, p_last_name, p_user_name, p_user_image, p_email, p_phone, p_external_id);
$$ LANGUAGE sql;
