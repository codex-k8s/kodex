#!/usr/bin/env bash
set -euo pipefail
export GOTOOLCHAIN=go1.26.6 GOMAXPROCS=2 GOWORK=off

# Только synthetic owner PostgreSQL; agents/provider/STT не запускаются.
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
fixture_directory=$(mktemp -d "${TMPDIR:-/tmp}/kodex-assistant-warm-wire.XXXXXXXX")
cleanup() {
  [[ "$fixture_directory" == */kodex-assistant-warm-wire.* && -d "$fixture_directory" && ! -L "$fixture_directory" ]] || return
  rm -f -- "$fixture_directory/owner.json" "$fixture_directory/wire.json"
  rmdir -- "$fixture_directory"
}
trap cleanup EXIT
export KODEX_TEST_WARM_OWNER_SNAPSHOT_PATH="$fixture_directory/owner.json"
export KODEX_TEST_WARM_WIRE_PATH="$fixture_directory/wire.json"
unset KODEX_CONTROL_PLANE_TEST_DSN KODEX_CONTROL_PLANE_TEST_ADMIN_DSN KODEX_CONTROL_PLANE_TEST_TEMPLATE_DATABASE KODEX_CONTROL_PLANE_TEST_FILTER

timeout --foreground 180s bash "$repository_root/scripts/tests/control-plane-postgres-test.sh" '^TestAssistantWarmRuntimeWireComponent$'
(
  cd -- "$repository_root/services/internal/control-plane"
  env -u GOFLAGS GOENV=off GOWORK=off go test -p 2 -count=1 -timeout=30s ./internal/transport/grpc -run '^TestAssistantWarmRuntimeOwnerSnapshotTransport$'
)
(
  cd -- "$repository_root/services/internal/runtime-controller"
  env -u GOFLAGS GOENV=off GOWORK=off go test -p 2 -count=1 -timeout=30s ./internal/workload -run '^TestAssistantWarmRuntimeOwnerWireBuild$'
)
printf 'Assistant warm owner/caster/controller wire pipeline passed\n'
