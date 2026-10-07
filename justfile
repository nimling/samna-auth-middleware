set dotenv-load
set export

default:
    @just --list

install:
    go install github.com/nimling/sbump/cmd@latest
    mv "$(go env GOPATH)/bin/cmd" "$(go env GOPATH)/bin/sbump"

deploy:
    sbump patch --env APP_VERSION --json ts/package.json@.version --push-version --auto --workflow

build:
    go build ./...

tidy:
    go mod tidy

test target='':
    #!/usr/bin/env bash
    set -e
    export PGSSLMODE=disable
    if [ -z "{{target}}" ]; then
        go test ./...
        from=$(git describe --tags --abbrev=0 --match 'v[0-9]*')
        for role in prophet disciple; do
            smig test --schema=$role/database/migrate.yml --db-dir=$role/database --from=$from
            out=$(smig test --schema=$role/database/migrate.yml --db-dir=$role/database --keep -s 2>&1 | sed 's/\x1b\[[0-9;]*m//g')
            name=$(echo "$out" | grep -o 'smig-test-[0-9]*' | head -1)
            eval "$(echo "$out" | grep '^ *PG[A-Z]*=' | sed 's/^ *//; s/^/export /')"
            pkgs="./shared/... ./disciple/... ./smw/..."; peer=""
            if [ $role = prophet ]; then
                pkgs=./prophet/...
                out=$(smig test --schema=$role/database/migrate.yml --db-dir=$role/database --keep -s 2>&1 | sed 's/\x1b\[[0-9;]*m//g')
                peer=$(echo "$out" | grep -o 'smig-test-[0-9]*' | head -1)
                eval "$(echo "$out" | grep '^ *PG[A-Z]*=' | sed 's/^ *//; s/^/export PEER_/')"
                out=$(smig test --schema=disciple/database/migrate.yml --db-dir=disciple/database --keep -s 2>&1 | sed 's/\x1b\[[0-9;]*m//g')
                peer="$peer $(echo "$out" | grep -o 'smig-test-[0-9]*' | head -1)"
                eval "$(echo "$out" | grep '^ *PG[A-Z]*=' | sed 's/^ *//; s/^/export DISCIPLE_/')"
            fi
            go test -tags dbtest -count=1 -p 1 $pkgs || { docker rm -f "$name" $peer >/dev/null; exit 1; }
            docker rm -f "$name" $peer >/dev/null
        done
    else
        go test ./{{target}}/...
    fi

verify:
    #!/usr/bin/env bash
    set -e
    export PGSSLMODE=disable
    names=""
    for role in prophet disciple; do
        out=$(smig test --schema=$role/database/migrate.yml --db-dir=$role/database --keep -s 2>&1 | sed 's/\x1b\[[0-9;]*m//g')
        names="$names $(echo "$out" | grep -o 'smig-test-[0-9]*' | head -1)"
        prefix=""; [ $role = disciple ] && prefix=DISCIPLE_
        eval "$(echo "$out" | grep '^ *PG[A-Z]*=' | sed "s/^ *//; s/^/export $prefix/")"
    done
    status=0
    go test -tags dbtest -count=1 -run Scenario -v ./prophet/echo/ || status=$?
    docker rm -f $names >/dev/null
    exit $status

test-hurl:
    #!/usr/bin/env bash
    set -e
    export PGSSLMODE=disable
    out=$({ smig test --schema=disciple/database/migrate.yml --db-dir=disciple/database --keep 2>&1 || true; } | sed 's/\x1b\[[0-9;]*m//g')
    name=$(echo "$out" | grep -o 'smig-test-[0-9]*' | head -1)
    [ -n "$name" ] || { echo "$out"; exit 1; }
    eval "$(echo "$out" | grep '^ *PG[A-Z]*=' | sed 's/^ *//; s/^/export /')"
    dir=$(mktemp -d)
    trap 'kill "$pid" 2>/dev/null; docker rm -f "$name" >/dev/null' EXIT
    psql -v ON_ERROR_STOP=1 -q -o /dev/null -f test/hurl/seed.sql
    go build -o "$dir/example-disciple" ./example/disciple
    port=$((20000 + RANDOM % 20000)); while (echo >/dev/tcp/127.0.0.1/$port) 2>/dev/null; do port=$((port + 1)); done
    PORT="$port" SAUTH_DEBUG_USER_ID=a0000000-0000-4000-8000-00000000000a "$dir/example-disciple" >"$dir/server.log" 2>&1 &
    pid=$!; disown
    for _ in $(seq 1 60); do curl -s -o /dev/null "http://127.0.0.1:$port/api/root" && break; sleep 1; done
    curl -s -o /dev/null "http://127.0.0.1:$port/api/root" || { echo "example disciple did not start"; cat "$dir/server.log"; exit 1; }
    hurl --test \
        --variable base="http://127.0.0.1:$port" \
        --variable user_a=a0000000-0000-4000-8000-00000000000a \
        --variable root=10000000-0000-4000-8000-000000000001 \
        --variable alpha=10000000-0000-4000-8000-000000000011 \
        --variable bravo=10000000-0000-4000-8000-000000000012 \
        --variable charlie=10000000-0000-4000-8000-000000000013 \
        --variable grand=10000000-0000-4000-8000-000000000111 \
        --variable hall=40000000-0000-4000-8000-000000000001 \
        --variable wing=40000000-0000-4000-8000-000000000002 \
        --variable missing=99999999-0000-4000-8000-000000000999 \
        test/hurl/*.hurl
