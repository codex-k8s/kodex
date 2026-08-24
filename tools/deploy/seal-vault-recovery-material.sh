#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Vault recovery sealing failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --material-directory <owner-material-directory> --age-recipient-file <path> --output-file <new-age-file>\n' "$0" >&2
}

material_directory=""
age_recipient_file=""
output_file=""
while (($# > 0)); do
  case "$1" in
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --age-recipient-file) age_recipient_file="${2:-}"; shift 2 ;;
    --output-file) output_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
vault_directory="$material_directory/vault"
[[ -d "$vault_directory" && ! -L "$material_directory" ]] || fail 'Vault material directory is invalid'
[[ -f "$age_recipient_file" && -s "$age_recipient_file" && ! -L "$age_recipient_file" ]] || fail 'age recipient file is invalid'
[[ -n "$output_file" && ! -e "$output_file" ]] || fail 'new recovery output file is required'
for command_name in age sha256sum stat tar; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
recipient=$(<"$age_recipient_file")
[[ "$recipient" =~ ^age1[0-9a-z]{58}$ ]] || fail 'age recipient is invalid'

required=(root-token init.json unseal-key-1 unseal-key-2 unseal-key-3 unseal-key-4 unseal-key-5)
for name in "${required[@]}"; do
  path="$vault_directory/$name"
  [[ -f "$path" && -s "$path" && ! -L "$path" ]] || fail "Vault recovery material is absent: $name"
  mode=$(stat -c '%a' "$path")
  (((8#$mode & 0077) == 0)) || fail "Vault recovery material mode is unsafe: $name"
done

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
archive="$temporary_directory/vault-recovery.tar"
tar --create --format=posix --owner=0 --group=0 --numeric-owner \
  --directory "$vault_directory" --file "$archive" "${required[@]}"
umask 077
age --recipient "$recipient" --output "$output_file" "$archive"
chmod 0600 "$output_file"
[[ -s "$output_file" ]] || fail 'encrypted Vault recovery bundle is empty'
sha256sum "$output_file" >"$output_file.sha256"
chmod 0600 "$output_file.sha256"
for name in "${required[@]}"; do rm -f -- "$vault_directory/$name"; done
for name in "${required[@]}"; do [[ ! -e "$vault_directory/$name" ]] || fail 'plaintext Vault recovery material remains'; done
printf 'Vault recovery material sealed\n'
