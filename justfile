deploy:
    SBUMP_VERSION_VAR=APP_VERSION ../sbump/sbump.sh patch --push-version

build:
    go build ./...

tidy:
    go mod tidy
