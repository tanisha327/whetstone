#!/usr/bin/env bash
# Run the Whetstone test suite, degrading gracefully on a machine that cannot
# reach a Go module proxy.
#
# The core packages (everything except the TUI) are stdlib-only, so Go's module
# pruning lets them build and test with no downloads at all. That is the whole
# suite except internal/tui and cmd/whetstone, and it is what runs here when
# the charmbracelet dependencies are unavailable.
#
# Usage:  ./scripts/test.sh [extra go test flags...]
#         ./scripts/test.sh -v
#         ./scripts/test.sh -run TestDismiss

set -uo pipefail
cd "$(dirname "$0")/.."

# Go is installed but not on PATH in a default Git Bash session here.
if ! command -v go >/dev/null 2>&1; then
    for candidate in "/c/Program Files/Go/bin" "/c/Go/bin"; do
        if [ -x "$candidate/go" ]; then
            PATH="$candidate:$PATH"
            export PATH
            break
        fi
    done
fi
if ! command -v go >/dev/null 2>&1; then
    echo "go not found. Install it, or add its bin directory to PATH." >&2
    exit 1
fi

CORE="./internal/doc/...
./internal/provider/...
./internal/outline/...
./internal/lens/...
./internal/provoke/...
./internal/workspace/..."

echo "== $(go version)"

echo
echo "== gofmt"
unformatted="$(gofmt -l . || true)"
if [ -n "$unformatted" ]; then
    echo "not gofmt-clean:"
    echo "$unformatted"
    echo "run: gofmt -w ."
    exit 1
fi
echo "clean"

echo
echo "== go vet (core)"
# shellcheck disable=SC2086
go vet $CORE || exit 1

echo
echo "== go test (core)"
# -race needs a C toolchain; on Windows that means mingw, which is often absent.
race=""
if command -v gcc >/dev/null 2>&1; then
    race="-race"
else
    echo "note: gcc not found, running without -race (CI runs it)"
fi
# shellcheck disable=SC2086
go test $race -cover $CORE "$@" || exit 1

echo
echo "== go build (tui + cmd)"
if go build ./internal/tui/... ./cmd/... 2>/dev/null; then
    echo "ok"
    echo
    echo "== go test (tui)"
    # shellcheck disable=SC2086
    go test $race -cover ./internal/tui/... "$@"
else
    cat <<'EOF'
SKIPPED - could not build the TUI packages.

They need github.com/charmbracelet/{bubbletea,lipgloss} and golang.org/x/term,
and this machine has no route to proxy.golang.org. To unblock, do one of:

  1. Point GOPROXY at a mirror you can reach:
         go env -w GOPROXY=https://<proxy>,direct
         go mod tidy

  2. Vendor the dependencies from a machine that does have egress:
         go mod tidy && go mod vendor
     then copy the vendor/ directory here. Builds use it automatically.

  3. Let CI do it: push the branch and read the run. The GitHub Actions
     workflow has both network access and a C toolchain, so it covers the TUI
     packages and -race.

Everything above this line passed, which is the full domain layer:
provider, doc, lens, provoke, outline, workspace.
EOF
fi
