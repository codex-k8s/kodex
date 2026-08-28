#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
deploy="$root/tools/dev/deploy-local.sh"

fail() {
  printf 'Local backup-controller credentials contract failed: %s\n' "$*" >&2
  exit 1
}

[[ $(grep -Fc 'user: "kodex_backup_reader"' "$deploy") -eq 2 ]] ||
  fail 'both backup databases must use the dedicated read-only login'
[[ $(grep -Fc 'password: secret($database; "kodex_backup_reader")' "$deploy") -eq 2 ]] ||
  fail 'both backup databases must use dedicated generated credentials'
grep -Fq 'kodex.dev/backup-credentials-sha256' "$deploy" ||
  fail 'backup-controller rollout is not bound to the credentials digest'
grep -Fq 'NOREPLICATION BYPASSRLS' \
  "$root/deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh" ||
  fail 'backup reader does not have the required bounded BYPASSRLS role'
grep -Fq 'GRANT SELECT ON ALL TABLES IN SCHEMA public, control_plane' \
  "$root/deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh" ||
  fail 'control-plane backup read grant reconciliation is absent'
grep -Fq 'GRANT SELECT ON ALL TABLES IN SCHEMA public, internal_rpc_authority' \
  "$root/deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh" ||
  fail 'authority backup read grant reconciliation is absent'

printf 'Local backup-controller credentials contract passed.\n'
