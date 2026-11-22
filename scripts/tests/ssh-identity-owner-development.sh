#!/usr/bin/env bash
set -euo pipefail
# Одноразовый owner-профиль проверяет встроенные миграции047 и048.
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
profile=--ssh-identity-owner-development
case "$#:${1:-}" in
  0:) ;;
  1:--revoke) profile=--ssh-identity-revoke-owner-development ;;
  1:--shared-health) profile=--shared-integration-health-development ;;
  *) printf '%s\n' 'SSH owner development profile is invalid' >&2; exit 1 ;;
esac
# Прежний профиль на frozen69b0 сохраняет собственные пины. Текущий seed
# применяет миграции через Goose и сверяет повторное применение048.
bash "$repository_root/scripts/tests/control-plane-postgres-test.sh" "$profile"
