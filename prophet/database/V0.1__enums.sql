-- ============================================================================
-- Claimius V1.0 Enums (shared)
-- ----------------------------------------------------------------------------
-- Type definitions for status and kind columns. Identical on prophet and
-- disciple, so they live in the shared set and are loaded before either
-- role's tables.
-- ============================================================================

DO $$
    BEGIN
        IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'claim_type') THEN
            CREATE TYPE claimius.claim_type AS ENUM ('system', 'user', 'service', 'virtual');
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'user_type') THEN
            CREATE TYPE claimius.user_type AS ENUM ('user', 'client', 'system', 'guest');
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'user_status') THEN
            CREATE TYPE claimius.user_status AS ENUM ('active', 'pending', 'suspended', 'inactive');
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'tree_type') THEN
            CREATE TYPE claimius.tree_type AS ENUM ('ownership', 'location', 'parenthood');
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'sync_operation') THEN
            CREATE TYPE claimius.sync_operation AS ENUM ('upsert', 'delete');
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'app_status') THEN
            CREATE TYPE claimius.app_status AS ENUM ('draft', 'active', 'inactive', 'erroneous');
        END IF;
    END $$;
