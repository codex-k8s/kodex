#!/usr/bin/env bash
# Материализация владельца передаётся native consumer без копии renderer.
set -euo pipefail
umask 077
[[ -n "${KODEX_TEST_OPENCODE_BINARY:-}" ]] || {
  printf '%s\n' 'KODEX_TEST_OPENCODE_BINARY must select the pinned qualification executable' >&2
  exit 1
}
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
fixture_directory=$(mktemp -d)
trap 'rm -rf -- "$fixture_directory"' EXIT
export KODEX_TEST_TEMPLATE_WIRE_FIXTURE="$fixture_directory/materializations.json"
(
  cd "$repo_root/services/internal/control-plane"
  env -u GOFLAGS GOENV=off GOWORK=off timeout 120s go test -count=1 -timeout=90s -run '^TestNativeWireMaterializations$' ./internal/domain/service/prompt
)
(
  cd "$repo_root/services/jobs/agent-runner"
  env -u GOFLAGS GOENV=off GOWORK=off timeout 480s go test -count=1 -race -timeout=420s -v ./internal/opencode
)
