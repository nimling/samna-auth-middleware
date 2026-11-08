#!/usr/bin/env bash
set -euo pipefail

role="$1"
name="claimius-$role-$$-$RANDOM"
docker run -d --rm --name "$name" -e POSTGRES_PASSWORD=claimius -e POSTGRES_DB=claimius -p 127.0.0.1::5432 postgres:17 >/dev/null
port=$(docker port "$name" 5432/tcp | head -1 | cut -d: -f2)
for _ in $(seq 1 60); do
    PGPASSWORD=claimius psql -h 127.0.0.1 -p "$port" -U postgres -d claimius -qtAc 'SELECT 1' >/dev/null 2>&1 && break
    sleep 1
done
env -i PATH="$PATH" HOME="$HOME" PGHOST=127.0.0.1 PGPORT="$port" PGUSER=postgres PGPASSWORD=claimius PGDATABASE=claimius PGSSLMODE=disable \
    smig up --yes --schema="$role/database/migrate.yml" --db-dir="$role/database" >&2
echo "name=$name"
echo "export PGHOST=127.0.0.1 PGPORT=$port PGUSER=postgres PGPASSWORD=claimius PGDATABASE=claimius"
