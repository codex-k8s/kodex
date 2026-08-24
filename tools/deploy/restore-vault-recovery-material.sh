#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Vault recovery restore failed: %s\n' "$*" >&2; exit 1; }
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
[[ -f "$age_identity_file" && -s "$age_identity_file" && ! -L "$age_identity_file" ]] || fail 'age identity file is invalid'
[[ -f "$bundle_file" && -s "$bundle_file" && ! -L "$bundle_file" ]] || fail 'encrypted recovery bundle is invalid'
[[ -f "$checksum_file" && -s "$checksum_file" && ! -L "$checksum_file" ]] || fail 'recovery checksum is invalid'
for command_name in age sha256sum stat tar; do command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"; done
identity_mode=$(stat -c '%a' "$age_identity_file")
(((8#$identity_mode & 0077) == 0)) || fail 'age identity permissions are too broad'
expected_sha=$(awk 'NR == 1 && NF >= 1 && $1 ~ /^[a-f0-9]{64}$/ {print $1}' "$checksum_file")
[[ -n "$expected_sha" && $(wc -l <"$checksum_file") -eq 1 ]] || fail 'recovery checksum format is invalid'
actual_sha=$(sha256sum "$bundle_file" | awk '{print $1}')
[[ "$actual_sha" == "$expected_sha" ]] || fail 'recovery bundle checksum mismatch'
vault_directory="$material_directory/vault"
mkdir -p "$vault_directory"
for name in root-token init.json unseal-key-1 unseal-key-2 unseal-key-3 unseal-key-4 unseal-key-5; do
  [[ ! -e "$vault_directory/$name" ]] || fail 'refusing to overwrite plaintext Vault recovery material'
done
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
archive="$temporary_directory/vault-recovery.tar"
age --decrypt --identity "$age_identity_file" --output "$archive" "$bundle_file"
entries=$(tar -tf "$archive" | sort)
expected=$(printf '%s\n' init.json root-token unseal-key-1 unseal-key-2 unseal-key-3 unseal-key-4 unseal-key-5 | sort)
[[ "$entries" == "$expected" ]] || fail 'Vault recovery archive contents are invalid'
tar -xf "$archive" -C "$vault_directory" --no-same-owner --no-same-permissions
chmod 0600 "$vault_directory"/root-token "$vault_directory"/init.json "$vault_directory"/unseal-key-*
printf 'Vault recovery material restored\n'
