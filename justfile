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

test name="":
    #!/usr/bin/env bash
    set -e
    export PGSSLMODE=disable
    run=(); [ -z {{quote(name)}} ] || run=(-run {{quote(name)}})
    go test "${run[@]}" ./...
    tmp=$(mktemp -d); names=""
    trap 'docker rm -f $names >/dev/null 2>&1; rm -rf "$tmp"' EXIT
    db() { bash scripts/db.sh "$1" > "$tmp/$2"; names="$names $(sed -n 's/^name=//p' "$tmp/$2")"; sed -n 's/^export //p' "$tmp/$2" | tr ' ' '\n' | sed "s/^/export $3/" > "$tmp/$2.env"; }
    db disciple disciple ""
    (. "$tmp/disciple.env"; go test -tags dbtest -count=1 -p 1 "${run[@]}" ./shared/... ./disciple/... ./smw/...)
    db prophet prophet ""
    db prophet peer PEER_
    db disciple federated DISCIPLE_
    (. "$tmp/prophet.env"; . "$tmp/peer.env"; . "$tmp/federated.env"; go test -tags dbtest -count=1 -p 1 "${run[@]}" ./prophet/...)

verify:
    #!/usr/bin/env bash
    set -e
    export PGSSLMODE=disable
    tmp=$(mktemp -d); names=""
    trap 'docker rm -f $names >/dev/null 2>&1; rm -rf "$tmp"' EXIT
    db() { bash scripts/db.sh "$1" > "$tmp/$2"; names="$names $(sed -n 's/^name=//p' "$tmp/$2")"; sed -n 's/^export //p' "$tmp/$2" | tr ' ' '\n' | sed "s/^/export $3/" > "$tmp/$2.env"; }
    db prophet prophet ""
    db disciple disciple DISCIPLE_
    . "$tmp/prophet.env"; . "$tmp/disciple.env"
    go test -tags dbtest -count=1 -run Scenario -v ./prophet/echo/

test-hurl:
    #!/usr/bin/env bash
    set -e
    export PGSSLMODE=disable
    out=$(bash scripts/db.sh disciple)
    name=$(echo "$out" | sed -n 's/^name=//p')
    eval "$(echo "$out" | sed -n 's/^export /export /p')"
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

bench:
    #!/usr/bin/env bash
    set -e
    export PGSSLMODE=disable
    for role in prophet disciple; do
        out=$(bash scripts/db.sh $role)
        name=$(echo "$out" | sed -n 's/^name=//p')
        eval "$(echo "$out" | sed -n 's/^export /export /p')"
        psql -q -c "ALTER DATABASE \"$PGDATABASE\" SET claimius.bench = 'on'"
        psql -q -v ON_ERROR_STOP=0 -f bench/$role/bench.sql 2>&1 | grep '^ *# \|not ok' || true
        docker rm -f "$name" >/dev/null
    done
