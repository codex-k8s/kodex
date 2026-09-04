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
[[ $(grep -Fc 'bucket: "kodex-session-archives"' "$deploy") -eq 1 ]] ||
  fail 'session archive object store must be included exactly once'
[[ $(grep -Fc 'name: "session-archives"' "$deploy") -eq 1 ]] ||
  fail 'session archive object store identity must be stable'
[[ $(grep -Fc 'prefix: "session-archive/v1"' "$deploy") -eq 1 ]] ||
  fail 'session archive object store must use the canonical immutable prefix'
grep -Fq 'NOREPLICATION BYPASSRLS' \
  "$root/deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh" ||
  fail 'backup reader does not have the required bounded BYPASSRLS role'
grep -Fq 'GRANT SELECT ON ALL TABLES IN SCHEMA public, control_plane' \
  "$root/deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh" ||
  fail 'control-plane backup read grant reconciliation is absent'
grep -Fq 'GRANT SELECT ON ALL TABLES IN SCHEMA public, internal_rpc_authority' \
  "$root/deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh" ||
  fail 'authority backup read grant reconciliation is absent'
grep -Fq 'until pg_isready --timeout=2' \
  "$root/deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh" ||
  fail 'runtime credential reconciliation does not wait for the routable PostgreSQL endpoint'
grep -Fq 'if [ "$attempt" -ge 90 ]' \
  "$root/deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh" ||
  fail 'runtime credential PostgreSQL readiness wait is not bounded'

printf 'Local backup-controller credentials contract passed.\n'
