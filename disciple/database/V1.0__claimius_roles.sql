DO $$
    BEGIN
        IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'claimius_admin') THEN
            CREATE ROLE claimius_admin;
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'claimius_writer') THEN
            CREATE ROLE claimius_writer;
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'claimius_reader') THEN
            CREATE ROLE claimius_reader;
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'claimius_disciple_client') THEN
            CREATE ROLE claimius_disciple_client;
        END IF;
    END $$;

CREATE SCHEMA IF NOT EXISTS claimius AUTHORIZATION claimius_admin;

GRANT USAGE ON SCHEMA claimius TO claimius_writer;
GRANT USAGE ON SCHEMA claimius TO claimius_reader;
GRANT USAGE ON SCHEMA claimius TO claimius_disciple_client;
