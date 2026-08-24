#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Identity plaintext cleanup failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --material-directory </run/or/dev/shm/path>\n' "$0" >&2
}

material_directory=""
while (($# > 0)); do
  case "$1" in
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
for command_name in dd find grep realpath sh sort wc; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ -d "$material_directory/identity" && -d "$material_directory/management" && ! -L "$material_directory" ]] ||
  fail 'identity material directory is invalid'
canonical_directory=$(realpath -e -- "$material_directory")
case "$canonical_directory" in /run/*|/dev/shm/*) ;; *) fail 'identity plaintext must be stored on an ephemeral filesystem' ;; esac

entries=$(find "$canonical_directory/identity" "$canonical_directory/management" -type f -printf '%P\n' | sort)
expected=$(printf '%s\n' \
  admin-client-id admin-client-secret admin-initial-password admin-username \
  bootstrap-admin-password bootstrap-admin-username database-ca.crt database-ca.key \
  database-password organization-id owner-email owner-initial-password owner-username \
  grafana-admin/admin-password grafana-admin/admin-user \
  oauth2-control-center/client-id oauth2-control-center/client-secret oauth2-control-center/cookie-secret \
  oauth2-grafana/client-id oauth2-grafana/client-secret oauth2-grafana/cookie-secret \
  oauth2-headlamp/client-id oauth2-headlamp/client-secret oauth2-headlamp/cookie-secret \
  oauth2-vault/client-id oauth2-vault/client-secret oauth2-vault/cookie-secret \
  vault-oidc/client-id vault-oidc/client-secret | sort)
[[ "$entries" == "$expected" ]] || fail 'identity plaintext contents are invalid'
! find "$canonical_directory/identity" "$canonical_directory/management" -type l -print -quit | grep -q . ||
  fail 'identity plaintext contains a symbolic link'

find "$canonical_directory/identity" "$canonical_directory/management" -type f -exec sh -ec '
  for path do
    size=$(wc -c <"$path")
    if [ "$size" -gt 0 ]; then
      dd if=/dev/zero of="$path" bs="$size" count=1 conv=notrunc status=none
    fi
    : >"$path"
  done
' sh {} +
find "$canonical_directory/identity" "$canonical_directory/management" -depth -delete
[[ ! -e "$canonical_directory/identity" && ! -e "$canonical_directory/management" ]] ||
  fail 'identity plaintext remains after cleanup'
printf 'Identity plaintext removed from the ephemeral filesystem\n'
