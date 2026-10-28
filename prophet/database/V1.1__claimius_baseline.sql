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
        IF NOT EXISTS (SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace WHERE t.typname = 'app_connection' AND n.nspname = 'claimius') THEN
            CREATE TYPE claimius.app_connection AS ENUM ('direct', 'code', 'bff', 'static');
        END IF;
    END $$;

CREATE TABLE IF NOT EXISTS claimius.samna_app (
                                                  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                  slug            TEXT UNIQUE NOT NULL,
                                                  name            TEXT NOT NULL,
                                                  description     TEXT DEFAULT '',
                                                  contact_email   TEXT,
                                                  app_image       TEXT,
                                                  redirect_uris   TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
                                                  sync_uri        TEXT NOT NULL DEFAULT '',
                                                  provider_ids    UUID[] NOT NULL DEFAULT ARRAY[]::UUID[],
                                                  status          claimius.app_status NOT NULL DEFAULT 'active',
                                                  connection      claimius.app_connection NOT NULL DEFAULT 'code',
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
                                                  sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                  sa_parent_id    UUID NOT NULL REFERENCES claimius.samna_app(id),
                                                  sa_root_id      UUID NOT NULL REFERENCES claimius.samna_app(id),
                                                  cookie_domain   TEXT,
                                                  cookie_same_site TEXT CHECK (cookie_same_site IN ('lax', 'strict', 'none'))
);

CREATE INDEX IF NOT EXISTS idx_samna_app_sa_parent_id ON claimius.samna_app (sa_parent_id);

CREATE INDEX IF NOT EXISTS idx_samna_app_sa_root_id ON claimius.samna_app (sa_root_id);

COMMENT ON COLUMN claimius.samna_app.sa_parent_id IS 'Parent app. Equals id at root. An Initiate app is another frontend of its root: own slug, redirect URIs, pages and style, while users, claims, organizations and the sync resolve on the root.';

COMMENT ON COLUMN claimius.samna_app.sa_root_id IS 'Root app. Equals id at root. Tokens for an Initiate app''s login are issued for the root.';

COMMENT ON COLUMN claimius.samna_app.cookie_domain IS 'Domain an Initiate app''s session cookies are set on by its disciple. Empty for host only cookies on the host that served the request, a parent domain to share them with the disciple''s host.';

COMMENT ON COLUMN claimius.samna_app.cookie_same_site IS 'SameSite of an Initiate app''s session cookies: lax or strict when the frontend shares a site with the disciple or routes its api through its own host, none when it calls the disciple from another site.';


COMMENT ON TABLE claimius.samna_app IS 'App registry. Set of rows determines deployment role.';
COMMENT ON COLUMN claimius.samna_app.slug IS 'URL slug for app, unique.';
COMMENT ON COLUMN claimius.samna_app.connection IS 'Session delivery mode. direct, code, bff, or static.';
COMMENT ON COLUMN claimius.samna_app.contact_email IS 'Contact email for the app.';
COMMENT ON COLUMN claimius.samna_app.app_image IS 'App image URL or asset reference.';
COMMENT ON COLUMN claimius.samna_app.redirect_uris IS 'Registered OAuth redirect URIs. First entry is the primary origin.';
COMMENT ON COLUMN claimius.samna_app.sync_uri IS 'Sync receiver URL for this app (used by disciples).';
COMMENT ON COLUMN claimius.samna_app.provider_ids IS 'Array of allowed provider UUIDs from external provider table.';
COMMENT ON COLUMN claimius.samna_app.private_key IS 'App private key for signing. Generated externally in Go.';
COMMENT ON COLUMN claimius.samna_app.private_seed IS 'Encryption seed material. Generated externally in Go.';
COMMENT ON COLUMN claimius.samna_app.app_secret IS 'HMAC SHA256 hash of (seed || raw secret). Validation rehashes and compares.';
COMMENT ON COLUMN claimius.samna_app.secret_version IS 'Version counter for app_secret rotation.';
COMMENT ON COLUMN claimius.samna_app.claim_id IS 'Default claim granted to users who log in to this app.';
COMMENT ON COLUMN claimius.samna_app.style IS 'UI styling configuration as JSON.';
COMMENT ON COLUMN claimius.samna_app.sa_owner_id IS 'Organization that owns this app.';
COMMENT ON COLUMN claimius.samna_app.sa_created_by IS 'user_claim.id of the actor who created this app.';

ALTER TABLE claimius.samna_app ADD COLUMN IF NOT EXISTS home_prophet TEXT;

COMMENT ON COLUMN claimius.samna_app.home_prophet IS 'The prophet that decides the app''s writes, by its prophet_peer.prophet slug. NULL when this prophet is the app''s home.';

CREATE INDEX IF NOT EXISTS idx_samna_app_sa_deleted_at ON claimius.samna_app (sa_deleted_at);
CREATE INDEX IF NOT EXISTS idx_samna_app_status ON claimius.samna_app (status);
CREATE INDEX IF NOT EXISTS idx_samna_app_sa_owner_id ON claimius.samna_app (sa_owner_id);

CREATE TABLE IF NOT EXISTS claimius.samna_user (
                                                   id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                   user_id         UUID NOT NULL DEFAULT gen_random_uuid(),
                                                   app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
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
                                                   sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   profile         JSONB NOT NULL DEFAULT '{}'::jsonb
);

COMMENT ON COLUMN claimius.samna_user.profile IS 'The user''s public profile, free form. The user writes its own keys on Samna Auth; an app writes under its own slug through PUT /sam/profile, {"<app_slug>": {"<field>": {"label", "value", "link", "index"}}}; the sync carries the whole object to every app that sees the user. The keys a product shows are its own.';


COMMENT ON TABLE claimius.samna_user IS 'User record. One row per (user_id, app_id) pair.';
COMMENT ON COLUMN claimius.samna_user.user_id IS 'Stable application identifier across apps. Used in all references.';
COMMENT ON COLUMN claimius.samna_user.app_id IS 'Required app scope. Disciples only hold rows for their own app.';
COMMENT ON COLUMN claimius.samna_user.user_name IS 'Optional display username, distinct from first_name/last_name.';
ALTER TABLE claimius.samna_user ADD COLUMN IF NOT EXISTS user_image TEXT;

COMMENT ON COLUMN claimius.samna_user.user_image IS 'Optional user image. URL, string, or id that the implementing system recognizes.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_samna_user_user_app
    ON claimius.samna_user (user_id, app_id) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_samna_user_user_id ON claimius.samna_user (user_id);
CREATE INDEX IF NOT EXISTS idx_samna_user_app_id ON claimius.samna_user (app_id);
CREATE INDEX IF NOT EXISTS idx_samna_user_email ON claimius.samna_user (email);
CREATE INDEX IF NOT EXISTS idx_samna_user_sa_deleted_at ON claimius.samna_user (sa_deleted_at);
CREATE INDEX IF NOT EXISTS idx_samna_user_status ON claimius.samna_user (status);
CREATE INDEX IF NOT EXISTS idx_samna_user_app_external ON claimius.samna_user (app_id, external_id) WHERE sa_deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS claimius.samna_client (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     name            TEXT NOT NULL,
                                                     description     TEXT,
                                                     app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
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
CREATE INDEX IF NOT EXISTS idx_samna_client_app_id ON claimius.samna_client (app_id);
CREATE INDEX IF NOT EXISTS idx_samna_client_sa_owner_id ON claimius.samna_client (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_samna_client_sa_deleted_at ON claimius.samna_client (sa_deleted_at);

CREATE TABLE IF NOT EXISTS claimius.samna_secret (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
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
    ON claimius.samna_secret (app_id, key) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_samna_secret_app_id ON claimius.samna_secret (app_id);
CREATE INDEX IF NOT EXISTS idx_samna_secret_sa_owner_id ON claimius.samna_secret (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_samna_secret_sa_deleted_at ON claimius.samna_secret (sa_deleted_at);

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
COMMENT ON COLUMN claimius.organization.private_seed IS 'Hex encoded HMAC seed. Generated at root creation, inherited by children. Keys app_secret hashing for apps owned in this tree.';
COMMENT ON COLUMN claimius.organization.sa_owner_id IS 'Parent org. Equals id at root.';
COMMENT ON COLUMN claimius.organization.sa_root_id IS 'Top of tree. Equals id at root.';
COMMENT ON COLUMN claimius.organization.sa_location_id IS 'Optional location pin for the org itself.';

CREATE INDEX IF NOT EXISTS idx_organization_sa_owner_id ON claimius.organization (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_organization_sa_root_id ON claimius.organization (sa_root_id);
CREATE INDEX IF NOT EXISTS idx_organization_sa_location_id ON claimius.organization (sa_location_id) WHERE sa_location_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_organization_external_id ON claimius.organization (external_id);
CREATE INDEX IF NOT EXISTS idx_organization_provider_id ON claimius.organization (provider_id);
CREATE INDEX IF NOT EXISTS idx_organization_sa_deleted_at ON claimius.organization (sa_deleted_at);

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

CREATE TABLE IF NOT EXISTS claimius.app_object (
                                                   id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                   app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
                                                   object_type     TEXT NOT NULL,
                                                   object_id       UUID NOT NULL,
                                                   sa_created_by   UUID,
                                                   sa_deleted_at   TIMESTAMPTZ,
                                                   sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.app_object IS 'App to object links for shared hierarchy objects (organizations and locations).';

CREATE UNIQUE INDEX IF NOT EXISTS idx_app_object_unique
    ON claimius.app_object (app_id, object_type, object_id) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_app_object_object ON claimius.app_object (object_type, object_id);
CREATE INDEX IF NOT EXISTS idx_app_object_app_id ON claimius.app_object (app_id);

CREATE TABLE IF NOT EXISTS claimius.claim (
                                              id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                              app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
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
                                              txid            XID8 NOT NULL DEFAULT pg_current_xact_id(),
                                              CONSTRAINT claim_app_id_id_key UNIQUE (app_id, id)
);

COMMENT ON TABLE claimius.claim IS 'Permission template. Access is a bitwise mask.';
COMMENT ON COLUMN claimius.claim.sa_access IS 'Bitwise access mask. Bits: 0x01 owner, 0x02 write, 0x04 read, 0x08 execute, 0x10 deny, 0x20 member. When the deny bit is set the other bits enumerate which permissions to subtract.';
COMMENT ON COLUMN claimius.claim.inherits IS 'When true, claim_object bindings cascade through hierarchy.';

CREATE INDEX IF NOT EXISTS idx_claim_app_id ON claimius.claim (app_id);
CREATE INDEX IF NOT EXISTS idx_claim_sa_owner_id ON claimius.claim (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_claim_sa_root_id ON claimius.claim (sa_root_id);
CREATE INDEX IF NOT EXISTS idx_claim_deny_bit ON claimius.claim ((sa_access & 16)) WHERE (sa_access & 16) <> 0;

ALTER TABLE claimius.claim ADD COLUMN IF NOT EXISTS level INTEGER NOT NULL DEFAULT 0;
DO $$ BEGIN
    ALTER TABLE claimius.claim ADD CONSTRAINT claim_level_not_negative CHECK (level >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
COMMENT ON COLUMN claimius.claim.level IS 'The level of the claim, 0 unless set. When a grant and a deny meet at different levels the higher level wins whatever their classes; at the same level a direct deny subtracts from every grant and a tree deny from inherited grants. A link takes the higher level of the two claims, and the claim of that level decides its mask. 0 for every claim resolves as before levels existed.';
DROP INDEX IF EXISTS claimius.idx_claim_sa_deleted_at;
ALTER TABLE claimius.claim ADD COLUMN IF NOT EXISTS txid XID8 NOT NULL DEFAULT pg_current_xact_id();
COMMENT ON COLUMN claimius.claim.txid IS 'Local transaction that last wrote the row, stamped by tg_update_txid on every insert and update, a replayed one included. get_claim_graph compares it to the snapshot a cached graph was built under.';
ALTER TABLE claimius.claim ADD COLUMN IF NOT EXISTS guest_mode TEXT NOT NULL DEFAULT 'off';
DO $$ BEGIN
    ALTER TABLE claimius.claim ADD CONSTRAINT claim_guest_mode_check CHECK (guest_mode IN ('open', 'code', 'off'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
COMMENT ON COLUMN claimius.claim.guest_mode IS 'How the guest of the claim is reached: open, by anyone without a credential, through a disciple''s get_guest_access; code, only by the holder of a live guest code of the claim; off, not at all. Set only through update_claim_guest, which also ensures the guest and its grant. A guest reads at most: get_access caps a guest user at READ.';

CREATE TABLE IF NOT EXISTS claimius.user_claim (
                                                   id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                   app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
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
                                                   txid            XID8 NOT NULL DEFAULT pg_current_xact_id(),
                                                   CONSTRAINT user_claim_app_claim_fkey FOREIGN KEY (app_id, claim_id) REFERENCES claimius.claim (app_id, id)
);

COMMENT ON CONSTRAINT claim_object_app_claim_fkey ON claimius.claim_object IS 'A binding lives in the app of its claim, so the feed and the snapshot of an app never carry a binding whose claim they do not.';

COMMENT ON CONSTRAINT user_claim_app_claim_fkey ON claimius.user_claim IS 'A grant lives in the app of its claim, so the feed and the snapshot of an app never carry a grant whose claim they do not.';


COMMENT ON TABLE claimius.user_claim IS 'Grants a claim to a user in an app. id is referenced as sa_created_by everywhere.';
COMMENT ON COLUMN claimius.user_claim.user_id IS 'References samna_user.user_id, not samna_user.id.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_claim_unique
    ON claimius.user_claim (claim_id, user_id, app_id) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_claim_user_app ON claimius.user_claim (user_id, app_id);
CREATE INDEX IF NOT EXISTS idx_user_claim_claim_id ON claimius.user_claim (claim_id);
CREATE INDEX IF NOT EXISTS idx_user_claim_sa_owner_id ON claimius.user_claim (sa_owner_id);
DROP INDEX IF EXISTS claimius.idx_user_claim_sa_deleted_at;
ALTER TABLE claimius.user_claim ADD COLUMN IF NOT EXISTS txid XID8 NOT NULL DEFAULT pg_current_xact_id();
COMMENT ON COLUMN claimius.user_claim.txid IS 'Local transaction that last wrote the row, stamped by tg_update_txid on every insert and update, a replayed one included. get_claim_graph compares it to the snapshot a cached graph was built under.';

CREATE TABLE IF NOT EXISTS claimius.claim_object (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
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
                                                     ),
                                                     CONSTRAINT claim_object_app_claim_fkey FOREIGN KEY (app_id, claim_id) REFERENCES claimius.claim (app_id, id)
);

COMMENT ON TABLE claimius.claim_object IS 'Binds a claim. With (object_type, object_id) scope keys are field names; without them scope keys are custom.';
COMMENT ON COLUMN claimius.claim_object.sa_access IS 'Bitwise access mask with bits 0x01 owner, 0x02 write, 0x04 read, 0x08 execute, 0x10 deny, 0x20 member; NULL means inherit the claim sa_access at read time.';
COMMENT ON COLUMN claimius.claim_object.scope IS 'Object scoped: keys are field names, NULL means whole object. Object-less: keys are custom, NULL means no grant.';

CREATE INDEX IF NOT EXISTS idx_claim_object_app_id ON claimius.claim_object (app_id);
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

ALTER TABLE IF EXISTS claimius.user_token RENAME TO user_identity;
DROP INDEX IF EXISTS claimius.idx_user_token_user_id, claimius.idx_user_token_app_id, claimius.idx_user_token_type, claimius.idx_user_token_sa_owner_id, claimius.idx_user_token_sa_deleted_at, claimius.idx_user_token_owner_external;

CREATE TABLE IF NOT EXISTS claimius.user_identity (
                                                   id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                   app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
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

COMMENT ON TABLE claimius.user_identity IS 'Bridge from an external identity, a token''s claim named by id_claim or a direct external_id, to claimius.samna_user.user_id. Written on the prophet, it syncs to its app without data.';

ALTER TABLE claimius.user_identity ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'token';
COMMENT ON COLUMN claimius.user_identity.type IS 'Binding kind: token for JWT derived, external for direct external_id.';

DROP INDEX IF EXISTS claimius.idx_user_identity_app_owner_external;
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_identity_owner_external
    ON claimius.user_identity (sa_owner_id, external_id) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_identity_user_id ON claimius.user_identity (user_id);
CREATE INDEX IF NOT EXISTS idx_user_identity_app_id ON claimius.user_identity (app_id);
CREATE INDEX IF NOT EXISTS idx_user_identity_type ON claimius.user_identity (type) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_identity_sa_owner_id ON claimius.user_identity (sa_owner_id);
CREATE INDEX IF NOT EXISTS idx_user_identity_sa_deleted_at ON claimius.user_identity (sa_deleted_at);

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

CREATE TABLE IF NOT EXISTS claimius.user_object (
                                                    app_id              UUID NOT NULL,
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
                                                    CONSTRAINT user_object_pkey PRIMARY KEY (user_id, app_id, object_type, object_id) INCLUDE (sa_access)
);

COMMENT ON TABLE claimius.user_object IS 'Reverse access index. Read path. One row per (user, app, object) with access, keyed on that tuple. Per object fields live in object_info.';
COMMENT ON COLUMN claimius.user_object.grants IS 'JSON array of {claim_id, access, scope, cascaded_from, tree_type}. Empty when only direct.';
COMMENT ON COLUMN claimius.user_object.direct_grant IS 'True when user is creator (via sa_created_by lookup).';
COMMENT ON COLUMN claimius.user_object.sa_access IS 'Effective bitwise access mask: OR of grants[].access with deny bits subtracted. Bits: 0x01 owner, 0x02 write, 0x04 read, 0x08 execute. Direct creator gets 0x01|0x02|0x04|0x08.';
COMMENT ON COLUMN claimius.user_object.scope IS 'Effective merged scope: union of grants[].scope keys when any grant has a scope, else NULL meaning whole object access.';

ALTER TABLE claimius.user_object ADD COLUMN IF NOT EXISTS used_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE claimius.user_object ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ;
ALTER TABLE claimius.user_object ADD COLUMN IF NOT EXISTS settled_txid XID8 NOT NULL DEFAULT '0';

COMMENT ON COLUMN claimius.user_object.used_count IS 'Successful single object checks of the pair, added by touch_object_usage. Kept by every settle, gone with the pair.';
COMMENT ON COLUMN claimius.user_object.last_used_at IS 'Time of the latest flush that counted a check of the pair. Usage ranks by used_count * 2^(-age / 14 days).';
COMMENT ON COLUMN claimius.user_object.settled_txid IS 'The pair''s mark: the snapshot xmin of the settle that last computed or confirmed it. A check tests only the areas of the object''s root and the user''s assignments raised at or after it, and moves it as a heap only update. Not indexed.';

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
    IF NOT EXISTS (SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace WHERE n.nspname = 'claimius' AND t.typname = 'search_hit') THEN
        CREATE TYPE claimius.search_hit AS (object_type TEXT, object_id UUID, score REAL);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS claimius.search_source (
    object_type   TEXT PRIMARY KEY,
    fn            REGPROCEDURE NOT NULL,
    sa_created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.search_source IS 'One search function per registered object type, supplied by the host for its own rows. Written by upsert_search_source, read by search.';
COMMENT ON TYPE claimius.search_hit IS 'One row a search source returns: the object and its score in [0, 1].';

DO $$
DECLARE
    v_new   BOOLEAN;
    v_table RECORD;
BEGIN
    v_new := NOT EXISTS (SELECT 1 FROM information_schema.columns
                         WHERE table_schema = 'claimius' AND table_name = 'object_info' AND column_name = 'sa_owner_root_id');
    ALTER TABLE claimius.object_info
        ADD COLUMN IF NOT EXISTS sa_created_by UUID,
        ADD COLUMN IF NOT EXISTS sa_owner_root_id UUID;
    IF NOT v_new OR to_regclass('claimius.table_info') IS NULL THEN
        RETURN;
    END IF;
    FOR v_table IN SELECT * FROM claimius.table_info LOOP
        EXECUTE format(
            'UPDATE claimius.object_info oi
             SET sa_created_by = %s, sa_owner_root_id = %s
             FROM %I.%I r
                 LEFT JOIN claimius.organization o ON o.id = %s
                 LEFT JOIN claimius.location l ON l.id = %s
                 LEFT JOIN claimius.organization lo ON lo.id = l.sa_owner_id
             WHERE oi.object_type = %L AND oi.object_id = r.id',
            CASE WHEN v_table.has_sa_created_by THEN 'r.sa_created_by' ELSE 'NULL::UUID' END,
            CASE WHEN v_table.object_type = 'claimius.organization' THEN 'r.sa_root_id' ELSE 'coalesce(o.sa_root_id, lo.sa_root_id)' END,
            v_table.schema_name, v_table.table_name,
            CASE WHEN v_table.has_sa_owner_id THEN 'r.sa_owner_id' ELSE 'NULL::UUID' END,
            CASE WHEN v_table.has_sa_location_id THEN 'r.sa_location_id' ELSE 'NULL::UUID' END,
            v_table.object_type);
    END LOOP;
    INSERT INTO claimius.object_info (object_type, object_id, sa_owner_id, sa_root_id, sa_name, sa_description, sa_created_by, sa_owner_root_id)
    SELECT 'claimius.samna_app', a.id, a.sa_owner_id, o.sa_root_id, a.name, a.description, a.sa_created_by, o.sa_root_id
    FROM claimius.samna_app a
        LEFT JOIN claimius.organization o ON o.id = a.sa_owner_id
    WHERE a.sa_deleted_at IS NULL
    ON CONFLICT (object_type, object_id) DO UPDATE SET
        sa_created_by    = EXCLUDED.sa_created_by,
        sa_owner_root_id = EXCLUDED.sa_owner_root_id;
END $$;

COMMENT ON COLUMN claimius.object_info.sa_created_by IS 'The grant the row was created under, copied from the row, so a settle reads the creator here and nothing from the implementing side''s table.';
COMMENT ON COLUMN claimius.object_info.sa_owner_root_id IS 'The root of the owning organization: the organization''s own root, or the root of the owner of the row or of its location. The root an area on the object is keyed by.';

CREATE INDEX IF NOT EXISTS idx_object_info_created_by ON claimius.object_info (sa_created_by, sa_owner_root_id);

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
          AND a.app_id = b.app_id
          AND a.object_type = b.object_type
          AND a.object_id = b.object_id
          AND (a.sa_updated_at, a.id) < (b.sa_updated_at, b.id);
        ALTER TABLE claimius.user_object DROP CONSTRAINT IF EXISTS user_object_pkey;
        ALTER TABLE claimius.user_object DROP COLUMN id;
        ALTER TABLE claimius.user_object
            ADD CONSTRAINT user_object_pkey PRIMARY KEY (user_id, app_id, object_type, object_id) INCLUDE (sa_access);
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
CREATE INDEX IF NOT EXISTS idx_user_object_object_user ON claimius.user_object (app_id, object_type, object_id, user_id);

DROP TABLE IF EXISTS claimius.object_users;

DO $$
BEGIN
    IF to_regclass('claimius.object_usage') IS NULL THEN
        RETURN;
    END IF;
    UPDATE claimius.user_object uo
    SET used_count = uo.used_count + ou.used_count,
        last_used_at = greatest(uo.last_used_at, ou.last_used_at)
    FROM claimius.object_usage ou
    WHERE ou.app_id = uo.app_id
      AND ou.user_id = uo.user_id
      AND ou.object_type = uo.object_type
      AND ou.object_id = uo.object_id;
    DROP TABLE claimius.object_usage;
END $$;

CREATE TABLE IF NOT EXISTS claimius.user_users (
                                                   id                      UUID PRIMARY KEY,
                                                   app_id                  UUID NOT NULL,
                                                   viewer_id               UUID NOT NULL,
                                                   target_user_id          UUID NOT NULL,
                                                   first_shared_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   last_shared_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   sharing_object_count    INTEGER NOT NULL DEFAULT 1
);

COMMENT ON TABLE claimius.user_users IS 'Pairs of users sharing at least one accessible object. Folded from the user_users_diff rows user_object inserts and deletes raise; read exact through _user_users_for_viewer.';
COMMENT ON COLUMN claimius.user_users.sharing_object_count IS 'Objects both users hold a user_object pair on. Moved by the folds of user_users_diff; the row is removed at zero.';

CREATE INDEX IF NOT EXISTS idx_user_users_viewer ON claimius.user_users (app_id, viewer_id);
CREATE INDEX IF NOT EXISTS idx_user_users_target ON claimius.user_users (app_id, target_user_id);

CREATE TABLE IF NOT EXISTS claimius.user_users_diff (
                                                        id              BIGINT GENERATED ALWAYS AS IDENTITY,
                                                        app_id          UUID NOT NULL,
                                                        user_id         UUID NOT NULL,
                                                        object_type     TEXT NOT NULL,
                                                        object_id       UUID NOT NULL,
                                                        delta           SMALLINT NOT NULL CHECK (delta IN (-1, 1)),
                                                        sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                        PRIMARY KEY (app_id, object_type, object_id, id)
);

COMMENT ON TABLE claimius.user_users_diff IS 'One row per user_object pair that entered (+1) or left (-1) on an object that is not a claim, not yet folded into user_users. Raised by a statement trigger on user_object with no lock; folded per object by _settle_user_users_diff. Derived, never published.';

CREATE INDEX IF NOT EXISTS idx_user_users_diff_user ON claimius.user_users_diff (app_id, user_id);

ALTER TABLE claimius.user_users_diff SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_analyze_scale_factor = 0.02
);

CREATE TABLE IF NOT EXISTS claimius.sync_event (
                                                   seq             BIGSERIAL PRIMARY KEY,
                                                   operation       claimius.sync_operation NOT NULL,
                                                   event_type      TEXT NOT NULL,
                                                   payload         JSONB NOT NULL,
                                                   written_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   txid            XID8 NOT NULL DEFAULT pg_current_xact_id(),
                                                   app_id          UUID
);

CREATE INDEX IF NOT EXISTS idx_sync_event_app_txid_seq ON claimius.sync_event (app_id, txid, seq);


ALTER TABLE claimius.sync_event ADD COLUMN IF NOT EXISTS txid XID8 NOT NULL DEFAULT pg_current_xact_id();

COMMENT ON TABLE claimius.sync_event IS 'Append only event stream for prophet to disciple sync.';
COMMENT ON COLUMN claimius.sync_event.event_type IS 'Source table: claimius.claim, claimius.user_claim, claimius.organization, etc. Derived tables never publish.';
COMMENT ON COLUMN claimius.sync_event.txid IS 'Transaction that wrote the event. The feed serves an event only once every transaction below it has ended, so a sequence that commits late is never passed over.';

DELETE FROM claimius.sync_event
WHERE event_type IN ('claimius.object_users', 'claimius.user_object', 'claimius.user_users', 'claimius.user_users_diff', 'claimius.audit');

DROP INDEX IF EXISTS claimius.idx_sync_event_written_at;
CREATE INDEX IF NOT EXISTS idx_sync_event_written_brin ON claimius.sync_event USING BRIN (written_at);
DROP INDEX IF EXISTS claimius.idx_sync_event_type_seq;

CREATE TABLE IF NOT EXISTS claimius.prophet_state (
                                                      id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                      app_id              UUID NOT NULL UNIQUE REFERENCES claimius.samna_app(id),
                                                      last_applied_txid   XID8 NOT NULL DEFAULT '0',
                                                      last_applied_seq    BIGINT NOT NULL DEFAULT 0,
                                                      system_app_slug     TEXT,
                                                      sync_mode           TEXT NOT NULL DEFAULT 'idle',
                                                      sync_failures       INTEGER NOT NULL DEFAULT 0,
                                                      next_attempt_at     TIMESTAMPTZ,
                                                      last_contact_at     TIMESTAMPTZ,
                                                      trimmed_txid        XID8 NOT NULL DEFAULT '0',
                                                      trimmed_seq         BIGINT NOT NULL DEFAULT 0,
                                                      sa_updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE claimius.prophet_state ADD COLUMN IF NOT EXISTS trimmed_seq BIGINT NOT NULL DEFAULT 0;
ALTER TABLE claimius.prophet_state ADD COLUMN IF NOT EXISTS last_applied_txid XID8 NOT NULL DEFAULT '0';
ALTER TABLE claimius.prophet_state ADD COLUMN IF NOT EXISTS trimmed_txid XID8 NOT NULL DEFAULT '0';
ALTER TABLE claimius.prophet_state ADD COLUMN IF NOT EXISTS access_trimmed_txid XID8 NOT NULL DEFAULT '0';
UPDATE claimius.prophet_state SET access_trimmed_txid = pg_current_xact_id() WHERE access_trimmed_txid = '0'::XID8;

CREATE UNIQUE INDEX IF NOT EXISTS idx_prophet_state_system_singleton
    ON claimius.prophet_state (system_app_slug)
    WHERE system_app_slug IS NOT NULL;

COMMENT ON TABLE claimius.prophet_state IS 'Per app publishing state. (last_applied_txid, last_applied_seq) is the lowest cursor among the app''s consumers in contact, (trimmed_txid, trimmed_seq) the highest position of the app''s events trim_sync_event removed. system_app_slug marks the deployment system app row.';
COMMENT ON COLUMN claimius.prophet_state.access_trimmed_txid IS 'The lowest cursor among the app''s users used within the access_diff retention at its last trim, or the oldest running transaction when none was: every area below it is gone, and a reader whose cursor is below it resettles its whole reach.';

CREATE TABLE IF NOT EXISTS claimius.sync_consumer (
                                                      app_id              UUID NOT NULL REFERENCES claimius.samna_app(id),
                                                      consumer_id         UUID NOT NULL,
                                                      last_applied_txid   XID8 NOT NULL DEFAULT '0',
                                                      last_applied_seq    BIGINT NOT NULL DEFAULT 0,
                                                      last_contact_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                      PRIMARY KEY (app_id, consumer_id)
);

ALTER TABLE claimius.sync_consumer ADD COLUMN IF NOT EXISTS last_applied_txid XID8 NOT NULL DEFAULT '0';

COMMENT ON TABLE claimius.sync_consumer IS 'Per consumer sync cursor: the (txid, seq) position a disciple database last proved it applied by asking for what comes after it.';

INSERT INTO claimius.sync_consumer (app_id, consumer_id, last_applied_seq, last_contact_at)
SELECT ps.app_id, '00000000-0000-0000-0000-000000000000', ps.last_applied_seq, ps.last_contact_at
FROM claimius.prophet_state ps
WHERE ps.last_contact_at IS NOT NULL
ON CONFLICT (app_id, consumer_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS claimius.disciple_state (
                                                       id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                       last_applied_txid   BIGINT NOT NULL DEFAULT 0,
                                                       last_applied_seq    BIGINT NOT NULL DEFAULT 0,
                                                       disciple_app_slug   TEXT NOT NULL,
                                                       sa_updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE claimius.disciple_state ADD COLUMN IF NOT EXISTS last_applied_txid BIGINT NOT NULL DEFAULT 0;

COMMENT ON TABLE claimius.disciple_state IS 'Singleton consumption cursor for the disciple: the prophet feed position (last_applied_txid, last_applied_seq) applied locally.';

ALTER TABLE claimius.disciple_state ADD COLUMN IF NOT EXISTS prophet TEXT;
ALTER TABLE claimius.disciple_state ADD COLUMN IF NOT EXISTS endpoint TEXT;

COMMENT ON COLUMN claimius.disciple_state.prophet IS 'Slug of the one prophet the disciple is bound to, as that prophet names itself on its feed. The cursor is a position on its feed alone. NULL until the first answer.';
COMMENT ON COLUMN claimius.disciple_state.endpoint IS 'Where the bound prophet answers. The disciple keeps pulling and routing writes there until another prophet serves it under an agreed handover.';

CREATE TABLE IF NOT EXISTS claimius.prophet_peer (
                                                     id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     prophet             TEXT NOT NULL,
                                                     endpoint            TEXT NOT NULL,
                                                     app_slug            TEXT NOT NULL,
                                                     app_secret          TEXT NOT NULL,
                                                     status              TEXT NOT NULL DEFAULT 'active',
                                                     last_applied_txid   BIGINT NOT NULL DEFAULT 0,
                                                     last_applied_seq    BIGINT NOT NULL DEFAULT 0,
                                                     sa_created_by       UUID NOT NULL,
                                                     sa_created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     sa_updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     CONSTRAINT prophet_peer_status CHECK (status IN ('active', 'suspended'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_prophet_peer_app_slug ON claimius.prophet_peer (app_slug);
CREATE INDEX IF NOT EXISTS idx_prophet_peer_prophet ON claimius.prophet_peer (prophet);

COMMENT ON TABLE claimius.prophet_peer IS 'Apps this prophet mirrors from peer prophets, one peer per app, with the credential and the cursor of the pull. Admin managed.';
COMMENT ON COLUMN claimius.prophet_peer.prophet IS 'The peer''s slug, the name it answers its feed under and samna_app.home_prophet refers to.';
COMMENT ON COLUMN claimius.prophet_peer.endpoint IS 'Base url of the peer. Its /sam serves the feed.';
COMMENT ON COLUMN claimius.prophet_peer.app_slug IS 'Slug of the mirrored app, the one the pull authenticates as.';
COMMENT ON COLUMN claimius.prophet_peer.app_secret IS 'Raw app secret the peer''s /sam app auth validates.';

ALTER TABLE claimius.prophet_peer ADD COLUMN IF NOT EXISTS peer_secret TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN claimius.prophet_peer.peer_secret IS 'Raw peer secret the home of the app granted this prophet in its peer_grant; the pull presents it to be served as a peer.';

CREATE TABLE IF NOT EXISTS claimius.peer_grant (
                                                   id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                   app_id              UUID NOT NULL REFERENCES claimius.samna_app(id),
                                                   prophet             TEXT NOT NULL,
                                                   secret_hash         TEXT NOT NULL,
                                                   status              TEXT NOT NULL DEFAULT 'active',
                                                   sa_created_by       UUID NOT NULL,
                                                   sa_created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   sa_updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   sa_deleted_at       TIMESTAMPTZ,
                                                   CONSTRAINT peer_grant_status CHECK (status IN ('active', 'suspended'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_peer_grant_app_prophet ON claimius.peer_grant (app_id, prophet) WHERE sa_deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_peer_grant_app_secret ON claimius.peer_grant (app_id, secret_hash) WHERE sa_deleted_at IS NULL;

COMMENT ON TABLE claimius.peer_grant IS 'Peer prophets this prophet, the home of an app, serves that app as a peer: the feed with its handovers and the snapshot with the app keys and organization seeds a home needs. Written through upsert_peer_grant by the prophet''s admin.';
COMMENT ON COLUMN claimius.peer_grant.prophet IS 'Slug of the peer prophet the grant is for.';
COMMENT ON COLUMN claimius.peer_grant.secret_hash IS 'SHA-256 hex of the random peer secret, which the peer keeps raw in its prophet_peer.peer_secret.';

CREATE TABLE IF NOT EXISTS claimius.app_handover (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
                                                     from_prophet    TEXT NOT NULL,
                                                     to_prophet      TEXT NOT NULL,
                                                     status          TEXT NOT NULL DEFAULT 'offered',
                                                     sa_created_by   UUID NOT NULL,
                                                     sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     CONSTRAINT app_handover_status CHECK (status IN ('offered', 'accepted'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_app_handover_offered ON claimius.app_handover (app_id) WHERE status = 'offered';
CREATE INDEX IF NOT EXISTS idx_app_handover_app ON claimius.app_handover (app_id, from_prophet, to_prophet);

COMMENT ON TABLE claimius.app_handover IS 'Offered and accepted moves of an app''s home between prophets. Travels on the app''s feed to peers only.';

CREATE TABLE IF NOT EXISTS claimius.peer_collision (
                                                       id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                       prophet         TEXT NOT NULL,
                                                       app_slug        TEXT NOT NULL,
                                                       object_type     TEXT NOT NULL,
                                                       object_id       UUID,
                                                       reason          TEXT NOT NULL,
                                                       payload         JSONB NOT NULL,
                                                       sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_peer_collision_object ON claimius.peer_collision (object_type, object_id);

COMMENT ON TABLE claimius.peer_collision IS 'Mirrored rows refused on apply because their id is held locally by another app or another home.';

DROP TABLE IF EXISTS claimius.reconcile_queue;

CREATE TABLE IF NOT EXISTS claimius.access_diff (
                                                    id              UUID PRIMARY KEY,
                                                    app_id          UUID NOT NULL,
                                                    kind            TEXT NOT NULL,
                                                    object_type     TEXT,
                                                    object_id       UUID,
                                                    claim_id        UUID,
                                                    cascade         BOOLEAN NOT NULL DEFAULT FALSE,
                                                    sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                    sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE claimius.access_diff ADD COLUMN IF NOT EXISTS user_id UUID;
ALTER TABLE claimius.access_diff ADD COLUMN IF NOT EXISTS txid XID8 NOT NULL DEFAULT pg_current_xact_id();
ALTER TABLE claimius.access_diff ADD COLUMN IF NOT EXISTS root_id UUID;
ALTER TABLE claimius.access_diff DROP COLUMN IF EXISTS generation, DROP COLUMN IF EXISTS raise_count;
DROP TABLE IF EXISTS claimius.access_diff_user;
DROP INDEX IF EXISTS claimius.idx_access_diff_claim;
DROP INDEX IF EXISTS claimius.idx_access_diff_user;

DO $$
BEGIN
    IF to_regprocedure('claimius.composite_id(text[])') IS NULL
        OR NOT EXISTS (SELECT 1 FROM claimius.access_diff WHERE kind = 'claim') THEN
        RETURN;
    END IF;
    INSERT INTO claimius.access_diff (id, app_id, kind, object_type, object_id, cascade)
    SELECT claimius.composite_id('object', d.app_id::TEXT, co.object_type, co.object_id::TEXT),
           d.app_id, 'object', co.object_type, co.object_id, bool_or(co.inherits AND c.inherits)
    FROM claimius.access_diff d
        JOIN claimius.claim c ON c.id = d.claim_id
        JOIN claimius.claim_object co
            ON co.claim_id = d.claim_id
           AND co.app_id = d.app_id
           AND co.sa_deleted_at IS NULL
           AND co.object_id IS NOT NULL
           AND co.object_type <> 'claimius.claim'
    WHERE d.kind = 'claim' AND d.user_id IS NULL
    GROUP BY d.app_id, co.object_type, co.object_id
    ON CONFLICT (id) DO UPDATE SET
        cascade = claimius.access_diff.cascade OR EXCLUDED.cascade,
        txid = pg_current_xact_id();
    INSERT INTO claimius.access_diff (id, app_id, kind, object_type, object_id, cascade)
    SELECT claimius.composite_id('object', d.app_id::TEXT, 'claimius.claim', d.claim_id::TEXT),
           d.app_id, 'object', 'claimius.claim', d.claim_id, FALSE
    FROM claimius.access_diff d
    WHERE d.kind = 'claim' AND d.user_id IS NULL
    ON CONFLICT (id) DO UPDATE SET txid = pg_current_xact_id();
    UPDATE claimius.access_diff
    SET id = claimius.composite_id('assignment', app_id::TEXT, claim_id::TEXT, user_id::TEXT),
        kind = 'assignment'
    WHERE kind = 'claim' AND user_id IS NOT NULL;
    DELETE FROM claimius.access_diff WHERE kind = 'claim';
END $$;

ALTER TABLE claimius.access_diff SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_analyze_scale_factor = 0.02
);

COMMENT ON TABLE claimius.access_diff IS 'The open areas of the app since its oldest active cursor. Written by the calc triggers and the sync apply, keyed on what changed and never on the users it reaches; read by a listing from the reader''s cursor and by a check from the pair''s mark; trimmed by drain_access_diff below the oldest cursor among the users used within 7 days.';
COMMENT ON COLUMN claimius.access_diff.kind IS 'object: a change on the object named, with cascade when its descendants are in the area. assignment: a change to one user''s grant of claim_id.';
COMMENT ON COLUMN claimius.access_diff.user_id IS 'The assignee of an assignment area. NULL on an object area.';
COMMENT ON COLUMN claimius.access_diff.txid IS 'The transaction of the latest raise, set again on every raise. An area is settled by a cursor or a mark taken from a snapshot whose xmin is above it.';
COMMENT ON COLUMN claimius.access_diff.root_id IS 'The owner root of the object of an object area, from the row or its object_info row, so a reader ranges the areas of its own roots and a check those of the object''s root. NULL on an assignment area and on an object with no root.';

CREATE INDEX IF NOT EXISTS idx_access_diff_object ON claimius.access_diff (app_id, object_type, object_id) WHERE kind = 'object';
CREATE INDEX IF NOT EXISTS idx_access_diff_root_txid ON claimius.access_diff (app_id, root_id, txid);
CREATE INDEX IF NOT EXISTS idx_access_diff_app_txid ON claimius.access_diff (app_id, txid);
CREATE INDEX IF NOT EXISTS idx_access_diff_user ON claimius.access_diff (app_id, user_id, txid) WHERE kind = 'assignment';

CREATE TABLE IF NOT EXISTS claimius.user_activity (
                                                      user_id         UUID NOT NULL,
                                                      app_id          UUID NOT NULL,
                                                      last_used_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                      PRIMARY KEY (user_id, app_id)
);

ALTER TABLE claimius.user_activity ADD COLUMN IF NOT EXISTS settled_txid XID8 NOT NULL DEFAULT '0';
ALTER TABLE claimius.user_activity ADD COLUMN IF NOT EXISTS settled_at TIMESTAMPTZ;
ALTER TABLE claimius.user_activity ADD COLUMN IF NOT EXISTS directory_at TIMESTAMPTZ;

COMMENT ON TABLE claimius.user_activity IS 'The reader''s cursor per app: the snapshot xmin of its last share settle, when it ran, when it last viewed the directory, and when it last used the app. The cursor and settled_at are written by the share settle, directory_at by the directory reads, last_used_at by touch_object_usage.';
COMMENT ON COLUMN claimius.user_activity.settled_txid IS 'The cursor: every area of the app with txid below it was settled for this user, or found not to reach its pairs.';
COMMENT ON COLUMN claimius.user_activity.settled_at IS 'When the cursor last moved; with the areas raised since, the raise rate the drain compares the read rate against.';
COMMENT ON COLUMN claimius.user_activity.directory_at IS 'When the user last read who sees whom. Set, the user is a viewer whose user_users rows the fold keeps; older than 30 days, the rows are rebuilt on the next directory read.';

CREATE INDEX IF NOT EXISTS idx_user_activity_directory ON claimius.user_activity (app_id, directory_at) WHERE directory_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS claimius.usage_diff (
                                                   id              BIGINT GENERATED ALWAYS AS IDENTITY,
                                                   app_id          UUID NOT NULL,
                                                   user_id         UUID NOT NULL,
                                                   object_type     TEXT NOT NULL,
                                                   object_id       UUID NOT NULL,
                                                   used_count      BIGINT NOT NULL,
                                                   used_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                   PRIMARY KEY (app_id, user_id, id)
);

COMMENT ON TABLE claimius.usage_diff IS 'Counted checks a usage flush recorded on a settled pair, not yet folded into user_object.used_count and last_used_at. Written by touch_object_usage with no lock on the pair; folded by _settle_usage_diff on the user''s next listing and by drain_access_diff. Local, never published.';

ALTER TABLE claimius.usage_diff SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_analyze_scale_factor = 0.02
);

CREATE TABLE IF NOT EXISTS claimius.claim_graph_cache (
                                                          app_id          UUID NOT NULL,
                                                          user_id         UUID NOT NULL,
                                                          graph           JSONB NOT NULL,
                                                          snapshot        PG_SNAPSHOT NOT NULL,
                                                          PRIMARY KEY (app_id, user_id)
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

CREATE OR REPLACE FUNCTION claimius.get_app()
    RETURNS SETOF claimius.samna_app AS $$
DECLARE
    v_slug TEXT;
BEGIN
    SELECT system_app_slug INTO v_slug
    FROM claimius.prophet_state
    WHERE system_app_slug IS NOT NULL
    LIMIT 1;

    IF v_slug IS NULL THEN
        RAISE EXCEPTION 'get_app: no system app registered. This deployment is a pure disciple, or init has not run.';
    END IF;

    RETURN QUERY
        SELECT * FROM claimius.samna_app
        WHERE slug = v_slug AND sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_app() IS 'Returns the system app row for this deployment (prophet or hybrid). Raises on pure disciple.';

CREATE OR REPLACE FUNCTION claimius.get_disciple_app()
    RETURNS SETOF claimius.samna_app AS $$
DECLARE
    v_slug TEXT;
BEGIN
    SELECT disciple_app_slug INTO v_slug
    FROM claimius.disciple_state
    LIMIT 1;

    IF v_slug IS NULL THEN
        RAISE EXCEPTION 'get_disciple_app: no disciple app registered. This deployment is a pure prophet, or upsert_disciple_state has not run.';
    END IF;

    RETURN QUERY
        SELECT * FROM claimius.samna_app
        WHERE slug = v_slug AND sa_deleted_at IS NULL;
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_disciple_app() IS 'Returns the upstream prophet app this disciple mirrors. Raises on a prophet and on a hybrid, which carry no disciple_state row.';

CREATE OR REPLACE FUNCTION claimius.get_prophet_app_id()
    RETURNS UUID AS $$
SELECT id FROM claimius.get_app();
$$ LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_prophet_app_id() IS 'Scalar wrapper over get_app. Returns the system app id; raises on pure disciple.';

CREATE OR REPLACE FUNCTION claimius.get_disciple_app_id()
    RETURNS UUID AS $$
SELECT id FROM claimius.get_disciple_app();
$$ LANGUAGE sql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_disciple_app_id() IS 'Scalar wrapper over get_disciple_app. Returns the upstream prophet app id; raises on a prophet and on a hybrid.';

CREATE OR REPLACE FUNCTION claimius.get_app_id()
    RETURNS UUID AS $$
DECLARE
    v_id UUID;
BEGIN
    SELECT a.id INTO v_id
    FROM claimius.prophet_state ps
             JOIN claimius.samna_app a ON a.slug = ps.system_app_slug
    WHERE ps.system_app_slug IS NOT NULL
      AND a.sa_deleted_at IS NULL
    LIMIT 1;

    IF v_id IS NOT NULL THEN
        RETURN v_id;
    END IF;

    SELECT a.id INTO v_id
    FROM claimius.disciple_state ds
             JOIN claimius.samna_app a ON a.slug = ds.disciple_app_slug
    WHERE a.sa_deleted_at IS NULL
    LIMIT 1;

    IF v_id IS NULL THEN
        RAISE EXCEPTION 'get_app_id: deployment has neither a prophet system app nor a disciple app registered.';
    END IF;

    RETURN v_id;
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_app_id() IS 'Returns the deployment own app id. Prophet/hybrid: system app. Pure disciple: upstream app.';

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
        DROP INDEX IF EXISTS claimius.idx_audit_app_id, claimius.idx_audit_sa_owner_id,
            claimius.idx_audit_sa_created_at, claimius.idx_audit_object;
        ALTER TABLE claimius.audit RENAME TO audit_default;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS claimius.audit (
                                              id              UUID NOT NULL DEFAULT gen_random_uuid(),
                                              app_id          UUID NOT NULL DEFAULT claimius.get_app_id(),
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

CREATE INDEX IF NOT EXISTS idx_audit_app_id ON claimius.audit (app_id);
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

CREATE TABLE IF NOT EXISTS claimius.object_field (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
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

CREATE INDEX IF NOT EXISTS idx_object_field_app_id ON claimius.object_field (app_id);
CREATE INDEX IF NOT EXISTS idx_object_field_object_type ON claimius.object_field (object_type);
CREATE INDEX IF NOT EXISTS idx_object_field_sa_deleted_at ON claimius.object_field (sa_deleted_at);

CREATE TABLE IF NOT EXISTS claimius.user_field (
                                                   id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                   app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
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

CREATE INDEX IF NOT EXISTS idx_user_field_app_id ON claimius.user_field (app_id);
CREATE INDEX IF NOT EXISTS idx_user_field_field_id ON claimius.user_field (field_id);
CREATE INDEX IF NOT EXISTS idx_user_field_user_id ON claimius.user_field (user_id);
CREATE INDEX IF NOT EXISTS idx_user_field_sa_deleted_at ON claimius.user_field (sa_deleted_at);

CREATE TABLE IF NOT EXISTS claimius.user_relation (
                                                      id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                      app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
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
CREATE INDEX IF NOT EXISTS idx_user_relation_app_id ON claimius.user_relation (app_id);
CREATE INDEX IF NOT EXISTS idx_user_relation_sa_deleted_at ON claimius.user_relation (sa_deleted_at);
CREATE INDEX IF NOT EXISTS idx_user_relation_parent_id ON claimius.user_relation (parent_id);
CREATE INDEX IF NOT EXISTS idx_user_relation_sibling_id ON claimius.user_relation (sibling_id);

CREATE TABLE IF NOT EXISTS claimius.object_asset (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     app_id          UUID NOT NULL,
                                                     object_type     TEXT NOT NULL CHECK (object_type LIKE 'claimius.%'),
                                                     object_id       UUID NOT NULL,
                                                     asset_id        UUID NOT NULL,
                                                     kind            TEXT NOT NULL,
                                                     name            TEXT,
                                                     description     TEXT,
                                                     mime_type       TEXT NOT NULL,
                                                     size            BIGINT,
                                                     "index"         INTEGER NOT NULL DEFAULT 0,
                                                     sa_owner_id     UUID NOT NULL,
                                                     sa_created_by   UUID NOT NULL,
                                                     sa_deleted_at   TIMESTAMPTZ,
                                                     sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     txid            XID8 NOT NULL DEFAULT pg_current_xact_id()
);

COMMENT ON TABLE claimius.object_asset IS 'Shared link from an asset held by its home app to a claimius object. The asset row and its bytes stay in the home service; the link is published to every app that sees the object. Not a registered access table: access to a link follows its object.';
COMMENT ON COLUMN claimius.object_asset.id IS 'Link id, the id a reader fetches the file by.';
COMMENT ON COLUMN claimius.object_asset.app_id IS 'Home app holding the asset row and its bytes. No foreign key: a peer prophet mirroring one app holds links of homes it does not mirror.';
COMMENT ON COLUMN claimius.object_asset.object_type IS 'Schema qualified type of the linked object, always a claimius type.';
COMMENT ON COLUMN claimius.object_asset.object_id IS 'Id of the linked object; samna_user.user_id for a user.';
COMMENT ON COLUMN claimius.object_asset.asset_id IS 'Id of the asset in the home app''s own storage.';
COMMENT ON COLUMN claimius.object_asset.kind IS 'Role of the asset on the object: profile, banner, logo, file and so on.';
COMMENT ON COLUMN claimius.object_asset.name IS 'Display name of the asset.';
COMMENT ON COLUMN claimius.object_asset.description IS 'Free text description of the asset.';
COMMENT ON COLUMN claimius.object_asset.mime_type IS 'Media type of the bytes the home serves.';
COMMENT ON COLUMN claimius.object_asset.size IS 'Size of the bytes in octets, when the home knows it.';
COMMENT ON COLUMN claimius.object_asset."index" IS 'Order among the links of one object; get_object_assets sorts by it, then by sa_created_at.';
COMMENT ON COLUMN claimius.object_asset.sa_owner_id IS 'Owner the access gate reported for the linked object when the link was written; the user itself for a link on the writer''s own samna_user.';
COMMENT ON COLUMN claimius.object_asset.sa_created_by IS 'user_claim.id of the grant the writer held WRITE on the object through; for a link on the writer''s own samna_user, the writer''s default claim grant in the home app.';
COMMENT ON COLUMN claimius.object_asset.sa_deleted_at IS 'Soft delete timestamp. A deleted link is published like any other change and never read.';
COMMENT ON COLUMN claimius.object_asset.sa_created_at IS 'Creation timestamp.';
COMMENT ON COLUMN claimius.object_asset.sa_updated_at IS 'Last update timestamp, set by tg_update_timestamp.';
COMMENT ON COLUMN claimius.object_asset.txid IS 'Local transaction that last wrote the row, stamped by tg_update_txid on every insert and update, a replayed one included.';

DROP INDEX IF EXISTS claimius.idx_object_asset_app_asset;
CREATE UNIQUE INDEX IF NOT EXISTS idx_object_asset_app_asset_object ON claimius.object_asset (app_id, asset_id, object_type, object_id) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_object_asset_object ON claimius.object_asset (object_type, object_id);

UPDATE claimius.table_info SET table_name = 'user_identity' WHERE schema_name = 'claimius' AND table_name = 'user_token';
UPDATE claimius.object_info SET object_type = 'claimius.user_identity' WHERE object_type = 'claimius.user_token';
UPDATE claimius.inheritance_info SET ancestor_type = 'claimius.user_identity' WHERE ancestor_type = 'claimius.user_token';
UPDATE claimius.inheritance_info SET descendant_type = 'claimius.user_identity' WHERE descendant_type = 'claimius.user_token';
UPDATE claimius.user_object SET object_type = 'claimius.user_identity' WHERE object_type = 'claimius.user_token';
UPDATE claimius.access_diff SET object_type = 'claimius.user_identity' WHERE object_type = 'claimius.user_token';

CREATE TABLE IF NOT EXISTS claimius.code (
                                             id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                             app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
                                             hash            TEXT NOT NULL,
                                             kind            TEXT NOT NULL,
                                             claim_id        UUID NOT NULL REFERENCES claimius.claim(id),
                                             object_id       UUID,
                                             object_type     TEXT,
                                             data            JSONB NOT NULL DEFAULT '{}'::JSONB,
                                             starts_at       TIMESTAMPTZ,
                                             ends_at         TIMESTAMPTZ,
                                             count           INTEGER NOT NULL DEFAULT 0,
                                             used_count      INTEGER NOT NULL DEFAULT 0,
                                             revoked_at      TIMESTAMPTZ,
                                             sa_owner_id     UUID NOT NULL,
                                             sa_created_by   UUID NOT NULL,
                                             sa_deleted_at   TIMESTAMPTZ,
                                             sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                             sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                             CONSTRAINT code_kind_check CHECK (kind IN ('invite', 'guest')),
                                             CONSTRAINT code_count_check CHECK (count >= 0 AND used_count >= 0)
);

COMMENT ON TABLE claimius.code IS 'Access code of a claim, written on the prophet and replicated to the app''s disciples. The value is shown once at creation and never stored: hash is its SHA-256. An invite code grants the claim to the signed in user who redeems it; a guest code lets its holder act as the guest of the claim. Not a registered access table: a code is managed by the owners of its claim.';
COMMENT ON COLUMN claimius.code.hash IS 'Hex SHA-256 of the code value. The value itself, 32 random bytes base64url, is never stored.';
COMMENT ON COLUMN claimius.code.kind IS 'invite: redeemed once per signed in user into a grant of the claim. guest: the holder acts as the guest of the claim.';
COMMENT ON COLUMN claimius.code.object_id IS 'Optional object the code was handed out for, for the service to open.';
COMMENT ON COLUMN claimius.code.object_type IS 'Schema qualified type of object_id.';
COMMENT ON COLUMN claimius.code.data IS 'Free form JSON the creating service keeps with the code.';
COMMENT ON COLUMN claimius.code.starts_at IS 'The code resolves from this time on; NULL from creation.';
COMMENT ON COLUMN claimius.code.ends_at IS 'The code resolves until this time; NULL with no end.';
COMMENT ON COLUMN claimius.code.count IS 'Uses the code allows; 0 is unlimited.';
COMMENT ON COLUMN claimius.code.used_count IS 'Uses so far, raised atomically by update_code_used and redeem_code.';
COMMENT ON COLUMN claimius.code.revoked_at IS 'Set by delete_code; a revoked code never resolves again.';
COMMENT ON COLUMN claimius.code.sa_owner_id IS 'Owning organization of the claim.';
COMMENT ON COLUMN claimius.code.sa_created_by IS 'user_claim.id of the grant by which the creator owns the claim.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_code_app_hash ON claimius.code (app_id, hash) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_code_claim_id ON claimius.code (claim_id);
CREATE INDEX IF NOT EXISTS idx_code_app_id ON claimius.code (app_id);

CREATE UNLOGGED TABLE IF NOT EXISTS claimius.session (
    id         text PRIMARY KEY,
    data       jsonb NOT NULL,
    expires_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_session_expires_at ON claimius.session (expires_at);

CREATE UNLOGGED TABLE IF NOT EXISTS claimius.dpop_jti (
    jti        text PRIMARY KEY,
    expires_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_dpop_jti_expires_at ON claimius.dpop_jti (expires_at);

CREATE UNLOGGED TABLE IF NOT EXISTS claimius.rate_limit (
    key text PRIMARY KEY,
    tat timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_rate_limit_tat ON claimius.rate_limit (tat);

CREATE UNLOGGED TABLE IF NOT EXISTS claimius.par_request (
    request_uri text PRIMARY KEY,
    payload     jsonb NOT NULL,
    expires_at  timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_par_request_expires_at ON claimius.par_request (expires_at);

CREATE UNLOGGED TABLE IF NOT EXISTS claimius.jwks_cache (
    cache_key  text PRIMARY KEY,
    body       jsonb NOT NULL,
    expires_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_jwks_cache_expires_at ON claimius.jwks_cache (expires_at);

CREATE TABLE IF NOT EXISTS claimius.notification (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     app_id          UUID NOT NULL REFERENCES claimius.samna_app(id),
                                                     user_id         UUID NOT NULL,
                                                     title           TEXT NOT NULL,
                                                     description     TEXT,
                                                     style           JSONB NOT NULL DEFAULT '{}'::jsonb,
                                                     type            TEXT NOT NULL,
                                                     link            TEXT,
                                                     object_id       UUID,
                                                     object_type     TEXT,
                                                     sa_created_by   UUID,
                                                     sa_owner_id     UUID,
                                                     read_at         TIMESTAMPTZ,
                                                     closed_at       TIMESTAMPTZ,
                                                     repeats_at      INTERVAL,
                                                     sa_deleted_at   TIMESTAMPTZ,
                                                     sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.notification IS 'Per user notification. One row per recipient.';

COMMENT ON COLUMN claimius.notification.user_id IS 'Recipient samna user_id. The fan out key.';

COMMENT ON COLUMN claimius.notification.style IS 'Per notification presentation customization as JSON.';

COMMENT ON COLUMN claimius.notification.object_id IS 'Target object the notification points at, when any.';

COMMENT ON COLUMN claimius.notification.object_type IS 'Qualified object type of object_id, when any.';

COMMENT ON COLUMN claimius.notification.sa_created_by IS 'User that triggered the notification, when a user did.';

COMMENT ON COLUMN claimius.notification.sa_owner_id IS 'Owning organization in a managed environment, when any.';

COMMENT ON COLUMN claimius.notification.repeats_at IS 'Recurrence interval for repeating notifications.';

CREATE INDEX IF NOT EXISTS idx_notification_user ON claimius.notification (app_id, user_id, sa_created_at DESC);

CREATE INDEX IF NOT EXISTS idx_notification_object ON claimius.notification (object_id, object_type);

CREATE INDEX IF NOT EXISTS idx_notification_sa_deleted_at ON claimius.notification (sa_deleted_at);

CREATE TABLE IF NOT EXISTS claimius.consent (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id         UUID NOT NULL REFERENCES claimius.samna_app(id),
    user_id        UUID,
    anonymous_id   TEXT NOT NULL,
    categories     JSONB NOT NULL DEFAULT '{}'::jsonb,
    policy_version TEXT NOT NULL,
    ip             INET,
    user_agent     TEXT,
    granted_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at     TIMESTAMPTZ,
    sa_created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.consent IS 'One live cookie consent decision per app and identity. A known user resolves across apps.';

COMMENT ON COLUMN claimius.consent.user_id IS 'Samna user_id when the visitor was authenticated at decision time.';

COMMENT ON COLUMN claimius.consent.anonymous_id IS 'Durable browser held id. The identity key while user_id is null.';

COMMENT ON COLUMN claimius.consent.categories IS 'Per category grant as JSON, keyed necessary, preferences, analytics, marketing.';

COMMENT ON COLUMN claimius.consent.policy_version IS 'Policy the visitor agreed to. A newer policy re-prompts.';

COMMENT ON COLUMN claimius.consent.ip IS 'Address the decision came from, recorded as evidence.';

COMMENT ON COLUMN claimius.consent.revoked_at IS 'Set when a newer decision supersedes this one.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_consent_live_user ON claimius.consent (app_id, user_id) WHERE user_id IS NOT NULL AND revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_consent_live_anonymous ON claimius.consent (app_id, anonymous_id) WHERE user_id IS NULL AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_consent_user ON claimius.consent (user_id) WHERE user_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS claimius.analytics_session (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id        UUID NOT NULL REFERENCES claimius.samna_app(id),
    user_id       UUID,
    anonymous_id  TEXT NOT NULL,
    session_key   TEXT NOT NULL,
    entry_path    TEXT,
    referrer      TEXT,
    user_agent    TEXT,
    ip            INET,
    language      TEXT,
    timezone      TEXT,
    platform      TEXT,
    screen_width  INTEGER,
    screen_height INTEGER,
    device_pixel_ratio REAL,
    context       JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.analytics_session IS 'Durable context of one visit. Events reference it so context is written once.';

COMMENT ON COLUMN claimius.analytics_session.session_key IS 'Browser generated session id. Unique per app.';

COMMENT ON COLUMN claimius.analytics_session.context IS 'Everything else the recorder could read, as JSON.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_analytics_session_key ON claimius.analytics_session (app_id, session_key);

CREATE INDEX IF NOT EXISTS idx_analytics_session_user ON claimius.analytics_session (app_id, user_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_analytics_session_anonymous ON claimius.analytics_session (app_id, anonymous_id, started_at DESC);

CREATE TABLE IF NOT EXISTS claimius.analytics_event (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id      UUID NOT NULL REFERENCES claimius.samna_app(id),
    session_id  UUID NOT NULL REFERENCES claimius.analytics_session(id),
    user_id     UUID,
    type        TEXT NOT NULL,
    path        TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.analytics_event IS 'Append only capture. One row per signal, pointer samples included.';

COMMENT ON COLUMN claimius.analytics_event.type IS 'Signal kind such as click, pointer, scroll, input, submit, view.';

COMMENT ON COLUMN claimius.analytics_event.payload IS 'Signal body. Coordinates, viewport, selector and text for a click, sample arrays for pointer.';

COMMENT ON COLUMN claimius.analytics_event.occurred_at IS 'Browser clock at capture. received_at is the server clock at write.';

CREATE INDEX IF NOT EXISTS idx_analytics_event_session ON claimius.analytics_event (session_id, occurred_at);

CREATE INDEX IF NOT EXISTS idx_analytics_event_path ON claimius.analytics_event (app_id, path, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_analytics_event_type ON claimius.analytics_event (app_id, type, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_analytics_event_received ON claimius.analytics_event USING BRIN (received_at);

CREATE TABLE IF NOT EXISTS claimius.analytics_capture (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id         UUID NOT NULL REFERENCES claimius.samna_app(id),
    path           TEXT NOT NULL,
    content_type   TEXT NOT NULL,
    body           BYTEA NOT NULL,
    captured_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT analytics_capture_app_id_path_key UNIQUE (app_id, path)
);

COMMENT ON TABLE claimius.analytics_capture IS 'A picture of a page as the browser rendered it, one per app, path and viewport width.';

CREATE INDEX IF NOT EXISTS idx_analytics_capture_app ON claimius.analytics_capture (app_id, captured_at DESC);

COMMENT ON TABLE claimius.analytics_capture IS 'A serialised copy of a page as the browser held it, one per app and path.';

COMMENT ON COLUMN claimius.analytics_capture.body IS 'The document tree as posted, masked. content_type says what it is.';

CREATE TABLE IF NOT EXISTS claimius.analytics_heat (
    app_id         UUID NOT NULL REFERENCES claimius.samna_app(id),
    path           TEXT NOT NULL,
    viewport_width INTEGER NOT NULL,
    type           TEXT NOT NULL,
    bucket_x       INTEGER NOT NULL,
    bucket_y       INTEGER NOT NULL,
    day            DATE NOT NULL,
    weight         BIGINT NOT NULL DEFAULT 0,
    sessions       BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (app_id, path, viewport_width, type, day, bucket_x, bucket_y)
);

COMMENT ON TABLE claimius.analytics_heat IS 'The heat field as a grid. One row per cell per kind per day, folded out of analytics_event.';

COMMENT ON COLUMN claimius.analytics_heat.bucket_x IS 'Page x at capture scale divided by claimius.analytics_bucket_size, floored.';

COMMENT ON COLUMN claimius.analytics_heat.bucket_y IS 'Page y at capture scale divided by claimius.analytics_bucket_size, floored.';

COMMENT ON COLUMN claimius.analytics_heat.weight IS 'Summed weight in the cell. A click counts 4 and every other kind counts 1, the same weighting the overlay draws with.';

COMMENT ON COLUMN claimius.analytics_heat.sessions IS 'Distinct sessions that put weight in the cell, so a single visitor cannot read as a hot area.';

CREATE INDEX IF NOT EXISTS idx_analytics_heat_read ON claimius.analytics_heat (app_id, path, day DESC);

