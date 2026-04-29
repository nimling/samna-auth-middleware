deploy:
    ../sbump/sbump.sh patch --env APP_VERSION --push-version

build:
    go build ./...

tidy:
    go mod tidy

test target='':
    #!/usr/bin/env bash
    set -e
    if [ -z "{{target}}" ]; then
        go test ./test/...
    else
        go test ./test/{{target}}/...
    fi
