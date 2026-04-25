deploy:
    ../sbump/sbump.sh patch --env APP_VERSION --push-version

build:
    go build ./...

tidy:
    go mod tidy
