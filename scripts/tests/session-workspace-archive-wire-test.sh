#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
wire_directory=$(mktemp -d "${TMPDIR:-/tmp}/kodex-workspace-wire.XXXXXX")
chmod 0700 "$wire_directory"

cleanup() {
  rm -f -- "$wire_directory/owner-tasks.json"
  rmdir -- "$wire_directory"
}
trap cleanup EXIT

# Оба модуля используют только локальный toolchain/cache и private fixture.
# Credentials, DSN, Go flags и Kubernetes identity из окружения не наследуются.
run_module() {
  local module_path=$1 test_name=$2
  (
    cd -- "$repository_root/$module_path"
    timeout --kill-after=5s 120s env -i \
      HOME="$HOME" USER="${USER:-s}" PATH="$PATH" \
      LANG=C.UTF-8 LC_ALL=C.UTF-8 CGO_ENABLED=0 GOENV=off GOWORK=off \
      GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOMAXPROCS=2 \
      KUBECONFIG=/dev/null KODEX_WORKSPACE_OWNER_PROTO_FIXTURE="$wire_directory/owner-tasks.json" \
      go test -mod=readonly -p 2 -count=1 -json -run "^${test_name}$" \
        ./internal/"$3"
  )
}

run_module services/internal/control-plane TestWorkspaceArchiveOwnerWire transport/grpc
run_module services/jobs/session-archive TestWorkspaceArchiveOwnerWireConsumer model
