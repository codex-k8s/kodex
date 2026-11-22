#!/usr/bin/env bash
# Проверяет закрытый протокол, installed helper и настоящий sshd без host config/credentials.
set -euo pipefail

if [[ $# -ne 0 ]]; then
  echo 'managed SSH test accepts no arguments' >&2
  exit 2
fi
task_repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
export GOTOOLCHAIN=local GOWORK=off GOMAXPROCS=2
export GOMEMLIMIT=${GOMEMLIMIT:-2GiB}
make -C "$task_repo_root" check-go-toolchain
command -v bwrap >/dev/null
command -v docker >/dev/null
test -x /usr/sbin/sshd
/usr/sbin/sshd -V
bwrap --version
cd "$task_repo_root/libs/go/managedssh"
timeout 60s go test -p=1 -race -count=1 -timeout=45s -v ./...
cd "$task_repo_root/tools/managed-ssh-helper"
timeout 120s env KODEX_MANAGED_SSH_NATIVE_REQUIRED=1 go test -p=1 -race -count=1 -timeout=90s -v ./...
cd "$task_repo_root/services/internal/secret-broker"
timeout 180s env KODEX_MANAGED_SSH_NATIVE_REQUIRED=1 go test -p=1 -race -count=1 -timeout=120s -v ./internal/integration/sshtarget ./internal/domain/service/sshidentity ./internal/kubernetes ./internal/controlplane
