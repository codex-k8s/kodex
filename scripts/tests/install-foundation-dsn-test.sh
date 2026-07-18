#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck disable=SC1091
. "$REPO_ROOT/scripts/lib/env.sh"

SYNTHETIC_PASSWORD='synthetic/with+reserved=value'
DSN="$(mattercodex_postgres_dsn \
  'runtime-user' \
  "$SYNTHETIC_PASSWORD" \
  'mattermost-postgres.mattermost.svc.cluster.local' \
  'mattercodex')"
mattercodex_validate_postgres_dsn "$DSN"
if [[ "$DSN" == *"$SYNTHETIC_PASSWORD"* ]] || [[ "$DSN" != *'%2F'* ]]; then
  printf 'PostgreSQL DSN не кодирует reserved password\n' >&2
  exit 1
fi

for installer in scripts/k8s/install-foundation.sh scripts/remote/install-foundation.sh; do
  if [ "$(grep -c 'mattercodex_postgres_dsn' "$REPO_ROOT/$installer")" -ne 2 ]; then
    printf 'installer не использует общий безопасный DSN builder: %s\n' "$installer" >&2
    exit 1
  fi
  if grep -Eq 'POSTGRES(_RUNTIME)?_DSN="postgres://' "$REPO_ROOT/$installer"; then
    printf 'installer содержит raw PostgreSQL DSN interpolation: %s\n' "$installer" >&2
    exit 1
  fi
done

printf 'матрица foundation DSN: PASS\n'
