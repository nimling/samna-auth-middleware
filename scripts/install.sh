#!/bin/sh
set -e

BIN="$(go env GOPATH 2>/dev/null || echo "$HOME/go")/bin"
GO_VERSION=$(sed -n 's/^go \([0-9.]*\)$/\1/p' go.mod)

has() {
    command -v "$1" > /dev/null 2>&1
}

at_least() {
    [ -n "$1" ] && [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -1)" = "$2" ]
}

version() {
    case "$1" in
        go) go version | sed 's/.*go\([0-9.]*\).*/\1/' ;;
        node) node --version | sed 's/^v//' ;;
        pnpm) pnpm --version ;;
        hurl) hurl --version | head -1 | awk '{print $2}' ;;
        psql) psql --version | awk '{print $3}' ;;
        docker) docker --version | sed 's/.*version \([0-9.]*\).*/\1/' ;;
        smig) smig --version | awk '{print $NF}' ;;
        sbump) sbump --version | awk '{print $NF}' ;;
    esac
}

brew_install() {
    if ! has brew; then
        echo "$1 needs installing and Homebrew is not here to do it. Install $1, then run just install again."
        exit 1
    fi
    brew install "$@" || brew upgrade "$@"
}

ensure() {
    name=$1
    want=$2
    shift 2
    have=""
    if has "$name"; then
        have=$(version "$name" 2>/dev/null || true)
    fi
    if has "$name" && { [ -z "$want" ] || at_least "$have" "$want"; }; then
        echo "$name: found ${have:-}"
        return
    fi
    if has "$name"; then
        echo "$name: $have is below $want, installing $want"
    else
        echo "$name: installing ${want:-latest}"
    fi
    "$@"
    if ! has "$name"; then
        echo "$name: could not be installed"
        exit 1
    fi
}

ensure go "$GO_VERSION" brew_install go
ensure node "" brew_install node
ensure pnpm "" brew_install pnpm
ensure hurl "" brew_install hurl
ensure psql "" sh -c "brew install libpq && brew link --force libpq"
ensure docker "" brew_install --cask orbstack
ensure smig "" sh -c "v=\$(GOPRIVATE=github.com/nimling/* go list -m -f '{{.Version}}' github.com/nimling/samna-migrate@latest) && GOPRIVATE=github.com/nimling/* go install -ldflags \"-X github.com/nimling/samna-migrate/pkg/cli.Version=\$v\" github.com/nimling/samna-migrate/cmd@\$v && mv '$BIN/cmd' '$BIN/smig'"
ensure sbump "" sh -c "GOPRIVATE=github.com/nimling/* go install github.com/nimling/sbump/cmd@latest && mv '$BIN/cmd' '$BIN/sbump'"

case ":$PATH:" in
    *":$BIN:"*) ;;
    *) echo "Add $BIN to PATH so smig and sbump are found." ;;
esac

go mod download
cd ts && pnpm install
cd ..
echo "Installed."
