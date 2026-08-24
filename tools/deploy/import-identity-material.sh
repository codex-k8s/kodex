#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Identity material import failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --material-directory <owner-material-directory> --age-identity-file <path> --bundle-file <age-file> --checksum-file <sha256-file>\n' "$0" >&2
}

material_directory=""
age_identity_file=""
bundle_file=""
checksum_file=""
while (($# > 0)); do
  case "$1" in
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --age-identity-file) age_identity_file="${2:-}"; shift 2 ;;
    --bundle-file) bundle_file="${2:-}"; shift 2 ;;
    --checksum-file) checksum_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
[[ -d "$material_directory" && ! -L "$material_directory" ]] || fail 'owner material directory is invalid'
[[ ! -e "$material_directory/identity" && ! -e "$material_directory/management" ]] || fail 'identity material already exists'
[[ -f "$age_identity_file" && -s "$age_identity_file" && ! -L "$age_identity_file" ]] || fail 'age identity file is invalid'
[[ -f "$bundle_file" && -s "$bundle_file" && ! -L "$bundle_file" ]] || fail 'identity bundle is invalid'
[[ -f "$checksum_file" && -s "$checksum_file" && ! -L "$checksum_file" ]] || fail 'identity checksum is invalid'
for command_name in age sha256sum stat tar; do command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"; done
identity_mode=$(stat -c '%a' "$age_identity_file")
(((8#$identity_mode & 0077) == 0)) || fail 'age identity permissions are too broad'
expected_sha=$(awk 'NR == 1 && NF >= 1 && $1 ~ /^[a-f0-9]{64}$/ {print $1}' "$checksum_file")
[[ -n "$expected_sha" && $(wc -l <"$checksum_file") -eq 1 ]] || fail 'identity checksum format is invalid'
actual_sha=$(sha256sum "$bundle_file" | awk '{print $1}')
[[ "$actual_sha" == "$expected_sha" ]] || fail 'identity bundle checksum mismatch'
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
archive="$temporary_directory/identity.tar"
extracted="$temporary_directory/extracted"
age --decrypt --identity "$age_identity_file" --output "$archive" "$bundle_file"
entries=$(tar -tf "$archive" | sed '/\/$/d' | sort)
expected=$(printf '%s\n' \
  identity/admin-client-id \
  identity/admin-client-secret \
  identity/admin-initial-password \
  identity/admin-username \
  identity/bootstrap-admin-password \
  identity/bootstrap-admin-username \
  identity/database-ca.crt \
  identity/database-ca.key \
  identity/database-password \
  identity/organization-id \
  identity/owner-email \
  identity/owner-initial-password \
  identity/owner-username \
  management/grafana-admin/admin-password \
  management/grafana-admin/admin-user \
  management/oauth2-control-center/client-id \
  management/oauth2-control-center/client-secret \
  management/oauth2-control-center/cookie-secret \
  management/oauth2-grafana/client-id \
  management/oauth2-grafana/client-secret \
  management/oauth2-grafana/cookie-secret \
  management/oauth2-headlamp/client-id \
  management/oauth2-headlamp/client-secret \
  management/oauth2-headlamp/cookie-secret \
  management/oauth2-vault/client-id \
  management/oauth2-vault/client-secret \
  management/oauth2-vault/cookie-secret \
  management/vault-oidc/client-id \
  management/vault-oidc/client-secret | sort)
[[ "$entries" == "$expected" ]] || fail 'identity bundle contents are invalid'
mkdir -p "$extracted"
tar -xf "$archive" -C "$extracted" --no-same-owner --no-same-permissions
! find "$extracted" -type l -print -quit | grep -q . || fail 'identity bundle contains a symbolic link'
find "$extracted/identity" "$extracted/management" -type f -exec chmod 0600 {} +
mv "$extracted/identity" "$extracted/management" "$material_directory/"
find "$material_directory/identity" "$material_directory/management" -type f -exec chmod 0600 {} +
printf 'Identity material imported\n'
