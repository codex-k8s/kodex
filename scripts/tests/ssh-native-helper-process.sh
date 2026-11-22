#!/usr/bin/env bash
set -euo pipefail
# Только test process: реальный descriptor/receipt runtime, не installed-host qualification.
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
fixture_directory=$(mktemp -d "${TMPDIR:-/tmp}/kodex-ssh-helper-process.XXXXXXXX")
trap 'rm -f -- "$fixture_directory/helper.test"; rmdir -- "$fixture_directory"' EXIT
chmod 0700 "$fixture_directory"
(
  cd -- "$repository_root/tools/managed-ssh-helper"
  timeout 120s go test -c -race -p=1 -o "$fixture_directory/helper.test" ./internal/helper
)
chmod 0500 "$fixture_directory/helper.test"
export KODEX_SSH_HELPER_PROCESS_BINARY="$fixture_directory/helper.test"
KODEX_SSH_HELPER_PROCESS_SHA256=$(sha256sum "$fixture_directory/helper.test")
export KODEX_SSH_HELPER_PROCESS_SHA256=${KODEX_SSH_HELPER_PROCESS_SHA256%% *}
export KODEX_SSH_HELPER_PROCESS_REQUIRED=1
cd -- "$repository_root/services/internal/secret-broker"
timeout 120s go test -race -count=1 -p=1 -timeout=90s -run '^TestSSHNativeHelperProcessLifecycle$' ./internal/integration/sshtarget
