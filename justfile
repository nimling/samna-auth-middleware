deploy:
    ./sbump.sh patch --push-version --env-file=.env --env-file=.env.example --version-var=APP_VERSION

build:
    go build ./...

tidy:
    go mod tidy
