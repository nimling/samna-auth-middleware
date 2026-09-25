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
    if [ -z "{{target}}" ]; then
        go test ./...
        bash test/sql/run.sh
    else
        go test ./{{target}}/...
    fi

test-sql:
    bash test/sql/run.sh
