#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
fixture_directory=$(mktemp -d /tmp/kodex-mcp-catalog.XXXXXX)
chmod 0700 "$fixture_directory"
trap 'rm -rf -- "$fixture_directory"' EXIT
export KODEX_RUNTIME_MCP_CATALOG_FIXTURE="$fixture_directory/catalog.json"
export GOMAXPROCS="${GOMAXPROCS:-4}" GOWORK=off TMPDIR=/tmp

go -C "$repository_root/services/internal/runtime-controller" test -p 2 -count=1 -timeout=60s ./internal/callback -run '^TestRuntimeMCPCatalogWireProducer$'
go -C "$repository_root/services/jobs/agent-runner" test -p 2 -count=1 -timeout=60s ./internal/readiness -run '^TestRuntimeMCPCatalogWireConsumer$'
