#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_root=${TMPDIR:-/tmp}
if [[ "$temporary_root" != /* || ! -d "$temporary_root" || ! -w "$temporary_root" || ! -x "$temporary_root" ]]; then
  printf 'Runtime MCP catalog test failed: temporary directory is invalid\n' >&2
  exit 1
fi
temporary_root=$(cd -- "$temporary_root" && pwd -P)
umask 077
fixture_directory=$(mktemp -d "$temporary_root/kodex-mcp-catalog.XXXXXX")
trap 'rm -rf -- "$fixture_directory"' EXIT
chmod 0700 "$fixture_directory"
export KODEX_RUNTIME_MCP_CATALOG_FIXTURE="$fixture_directory/catalog.json"
export GOMAXPROCS="${GOMAXPROCS:-4}" GOWORK=off TMPDIR="$temporary_root"

go -C "$repository_root/services/internal/runtime-controller" test -p 2 -count=1 -timeout=60s ./internal/callback -run '^TestRuntimeMCPCatalogWireProducer$'
go -C "$repository_root/services/jobs/agent-runner" test -p 2 -count=1 -timeout=60s ./internal/readiness -run '^TestRuntimeMCPCatalogWireConsumer$'
