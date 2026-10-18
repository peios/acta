#!/bin/bash
# Workspace commands intentionally run outside Bazel's build sandbox.
set -euo pipefail
root=${BUILD_WORKSPACE_DIRECTORY:?Run this command with ./bazel run}
mode=$1
shift
case "$mode" in
format-check)
    gofmt=$(realpath "$1")
    cd "$root"
    mapfile -d '' files < <(find cmd internal web learn \
        -type d \( -name node_modules -o -name .svelte-kit -o -path web/build \) -prune \
        -o -type f -name '*.go' -print0)
    result=$("$gofmt" -l "${files[@]}")
    if [[ -n $result ]]; then
        printf 'Go files need formatting:\n%s\n' "$result" >&2
        exit 1
    fi
    ;;
integration)
    : "${ACTA_TEST_DATABASE_URL:?Set ACTA_TEST_DATABASE_URL to an expendable PostgreSQL database}"
    cd "$root"
    exec ./bazel test --config=integration //... "$@"
    ;;
db)
    cd "$root"
    exec docker compose up -d --wait db
    ;;
run)
    binary=$(realpath "$1")
    shift
    cd "$root"
    export ACTA_DATABASE_URL=${ACTA_DATABASE_URL:-postgres://acta:acta-development-only@127.0.0.1:5433/acta?sslmode=disable}
    export ACTA_DEV_ORIGIN=${ACTA_DEV_ORIGIN:-http://localhost:5173}
    exec "$binary" -listen :8081 "$@"
    ;;
dev)
    node=$(realpath "$1")
    npm=$(realpath "$2")
    PATH="$(dirname "$node"):$PATH"
    export PATH
    cd "$root/web"
    # Hot reload writes to the checkout, unlike sandboxed builds and tests.
    "$node" "$npm" ci --no-audit --no-fund --ignore-scripts
    exec "$node" "$npm" run dev
    ;;
*)
    echo "Unknown developer command: $mode" >&2
    exit 1
    ;;
esac
