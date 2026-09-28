#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
name="samna-mw-test-pg-$$"
port="${CLAIMIUS_TEST_PORT:-55439}"

docker run --rm -d --name "$name" -e POSTGRES_PASSWORD=pg -p "$port:5432" postgres:17 >/dev/null
trap 'docker stop "$name" >/dev/null' EXIT

export PGHOST=localhost PGPORT="$port" PGUSER=postgres PGPASSWORD=pg PGSSLMODE=disable
for _ in $(seq 1 60); do
    if pg_isready -q -d postgres; then break; fi
    sleep 1
done
pg_isready -q -d postgres || { echo "postgres on port $port did not start"; exit 1; }
sleep 2

run() {
    local db="$1" file="$2" out
    if ! out=$(psql -q -v ON_ERROR_STOP=1 -d "$db" -f "$file" 2>&1); then
        echo "FAIL $db $(basename "$file")"
        echo "$out" | grep -E "ERROR|CONTEXT|DETAIL" || echo "$out"
        exit 1
    fi
}

load() {
    local db="$1" dir="$2"
    shift 2
    psql -q -d postgres -c "CREATE DATABASE $db" >/dev/null
    psql -q -d "$db" -c "CREATE EXTENSION IF NOT EXISTS pgcrypto" >/dev/null
    for f in $(ls "$dir"/V*.sql | sort -V) "$@"; do
        run "$db" "$f"
    done
}

tag="$(git -C "$root" describe --tags --abbrev=0 --match 'v[0-9]*' HEAD)"

for role in disciple prophet; do
    current=$(ls "$root/$role/database/migrations"/V*.sql | sort -V)

    load "fresh_$role" "$root/$role/database" $current
    for t in "$root"/test/sql/"$role"/*.sql; do
        run "fresh_$role" "$t"
        echo "ok fresh $role $(basename "$t")"
    done

    previous="$(mktemp -d)"
    mkdir -p "$previous/migrations"
    git -C "$root" archive "$tag" "$role/database" | tar -x -C "$previous" --strip-components=2
    shipped=$(ls "$previous"/migrations/V*.sql 2>/dev/null | sort -V || true)
    load "upgrade_$role" "$previous" $shipped
    for f in $current; do
        if [ ! -f "$previous/migrations/$(basename "$f")" ]; then
            run "upgrade_$role" "$f"
            run "upgrade_$role" "$f"
            echo "ok upgrade $role from $tag $(basename "$f") twice"
        fi
    done
    for t in "$root"/test/sql/"$role"/*.sql; do
        run "upgrade_$role" "$t"
        echo "ok upgraded $role $(basename "$t")"
    done
done

load "go_disciple" "$root/disciple/database" $(ls "$root/disciple/database/migrations"/V*.sql | sort -V)
(cd "$root" && PGDATABASE=go_disciple SAUTH_DEBUG_USER_ID=a0000000-0000-4000-8000-00000000000a \
    go test -tags dbtest -count=1 ./shared/...)

load "go_prophet" "$root/prophet/database" $(ls "$root/prophet/database/migrations"/V*.sql | sort -V)
(cd "$root" && PGDATABASE=go_prophet go test -tags dbtest -count=1 ./prophet/...)

load "hurl_disciple" "$root/disciple/database" $(ls "$root/disciple/database/migrations"/V*.sql | sort -V) "$root/test/hurl/seed.sql"
http_port="${CLAIMIUS_TEST_HTTP_PORT:-18089}"
server_dir="$(mktemp -d)"
(cd "$root" && go build -o "$server_dir/example-disciple" ./example/disciple)
PGDATABASE=hurl_disciple PORT="$http_port" SAUTH_DEBUG_USER_ID=a0000000-0000-4000-8000-00000000000a \
    "$server_dir/example-disciple" >"$server_dir/server.log" 2>&1 &
server_pid=$!
trap 'kill "$server_pid" 2>/dev/null; docker stop "$name" >/dev/null' EXIT
for _ in $(seq 1 60); do
    if curl -s -o /dev/null "http://localhost:$http_port/api/root"; then break; fi
    sleep 1
done
curl -s -o /dev/null "http://localhost:$http_port/api/root" || { echo "example disciple did not start"; cat "$server_dir/server.log"; exit 1; }
hurl --test \
    --variable base="http://localhost:$http_port" \
    --variable user_a=a0000000-0000-4000-8000-00000000000a \
    --variable root=10000000-0000-4000-8000-000000000001 \
    --variable alpha=10000000-0000-4000-8000-000000000011 \
    --variable bravo=10000000-0000-4000-8000-000000000012 \
    --variable charlie=10000000-0000-4000-8000-000000000013 \
    --variable grand=10000000-0000-4000-8000-000000000111 \
    --variable missing=99999999-0000-4000-8000-000000000999 \
    "$root"/test/hurl/*.hurl
