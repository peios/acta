#!/bin/bash
# Export the five native binaries for local tools and the container build.
set -euo pipefail
output=${ACTA_OUTPUT_DIR:-${BUILD_WORKSPACE_DIRECTORY:?}/bin}
mkdir -p "$output"
while [[ $# -gt 0 ]]; do
    name=$1
    binary=$2
    shift 2
    install -m 755 "$binary" "$output/$name"
done
