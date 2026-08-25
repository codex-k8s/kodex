#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Identity material generation failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf '%s\n' \
    "Usage: $0 --material-directory <owner-material-directory>" \
    '  --admin-username-file <path> --admin-initial-password-file <path>' \
    '  --owner-username-file <path> --owner-email-file <path>' \
    '  --owner-initial-password-file <path>' >&2
}

material_directory=""
admin_username_file=""
admin_initial_password_file=""
owner_username_file=""
owner_email_file=""
owner_initial_password_file=""
while (($# > 0)); do
  case "$1" in
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --admin-username-file) admin_username_file="${2:-}"; shift 2 ;;
    --admin-initial-password-file) admin_initial_password_file="${2:-}"; shift 2 ;;
    --owner-username-file) owner_username_file="${2:-}"; shift 2 ;;
    --owner-email-file) owner_email_file="${2:-}"; shift 2 ;;
    --owner-initial-password-file) owner_initial_password_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -d "$material_directory" && ! -L "$material_directory" ]] || fail 'owner material directory is invalid'
identity_directory="$material_directory/identity"
management_directory="$material_directory/management"
[[ ! -e "$identity_directory" && ! -e "$management_directory" ]] || fail 'identity material already exists'
for input_file in "$admin_username_file" "$admin_initial_password_file" \
  "$owner_username_file" "$owner_email_file" "$owner_initial_password_file"; do
  [[ -f "$input_file" && -s "$input_file" && ! -L "$input_file" ]] || fail 'identity input file is invalid'
done
for command_name in base64 jq openssl; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

read_single_line() {
  local path=$1 label=$2 value
  value=$(<"$path")
  [[ -n "$value" && "$value" != *$'\n'* && "$value" != *$'\r'* ]] || fail "$label must be one nonempty line"
  printf '%s' "$value"
}

admin_username=$(read_single_line "$admin_username_file" 'administrator username')
admin_password=$(read_single_line "$admin_initial_password_file" 'administrator initial password')
owner_username=$(read_single_line "$owner_username_file" 'owner username')
owner_email=$(read_single_line "$owner_email_file" 'owner email')
owner_password=$(read_single_line "$owner_initial_password_file" 'owner initial password')
[[ "$admin_username" =~ ^[a-zA-Z0-9._@-]{3,128}$ ]] || fail 'administrator username is invalid'
[[ ${#admin_password} -ge 20 ]] || fail 'administrator initial password is too short'
[[ "$owner_username" =~ ^[a-zA-Z0-9._@-]{3,128}$ ]] || fail 'owner username is invalid'
[[ "$owner_username" != "$admin_username" ]] || fail 'owner and bootstrap admin usernames must differ'
[[ "$owner_email" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]] || fail 'owner email is invalid'
[[ ${#owner_password} -ge 20 ]] || fail 'owner initial password is too short'
[[ "$owner_password" != "$admin_password" ]] || fail 'owner and bootstrap admin passwords must differ'

umask 077
mkdir -p "$identity_directory" "$management_directory"
printf '%s' "$admin_username" >"$identity_directory/admin-username"
printf '%s' "$admin_password" >"$identity_directory/admin-initial-password"
printf 'kodex-bootstrap-%s' "$(openssl rand -hex 8)" >"$identity_directory/bootstrap-admin-username"
openssl rand -base64 48 | tr -d '\n' >"$identity_directory/bootstrap-admin-password"
printf '%s' "$owner_username" >"$identity_directory/owner-username"
printf '%s' "$owner_email" >"$identity_directory/owner-email"
printf '%s' "$owner_password" >"$identity_directory/owner-initial-password"
openssl rand -base64 48 | tr -d '\n' >"$identity_directory/database-password"
openssl ecparam -name prime256v1 -genkey -noout -out "$identity_directory/database-ca.key"
openssl req -x509 -new -sha256 -key "$identity_directory/database-ca.key" \
  -subj /CN=Kodex-Identity-Database-CA -days 3650 \
  -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -addext 'subjectKeyIdentifier=hash' \
  -out "$identity_directory/database-ca.crt" >/dev/null 2>&1
if [[ -r /proc/sys/kernel/random/uuid ]]; then
  tr -d '\n' </proc/sys/kernel/random/uuid >"$identity_directory/organization-id"
else
  uuid_hex=$(openssl rand -hex 16)
  variant=$(((16#${uuid_hex:16:1} & 3) | 8))
  printf '%s-%s-4%s-%x%s-%s' \
    "${uuid_hex:0:8}" "${uuid_hex:8:4}" "${uuid_hex:13:3}" \
    "$variant" "${uuid_hex:17:3}" "${uuid_hex:20:12}" \
    >"$identity_directory/organization-id"
fi
printf '%s' kodex-sso-bootstrap >"$identity_directory/admin-client-id"
openssl rand -base64 48 | tr -d '\n' >"$identity_directory/admin-client-secret"

for surface in control-center grafana vault headlamp; do
  surface_directory="$management_directory/oauth2-$surface"
  mkdir -p "$surface_directory"
  printf 'kodex-%s-proxy' "$surface" >"$surface_directory/client-id"
  openssl rand -base64 48 | tr -d '\n' >"$surface_directory/client-secret"
  openssl rand -base64 32 | tr -d '\n' >"$surface_directory/cookie-secret"
done
mkdir -p "$management_directory/vault-oidc" "$management_directory/grafana-admin"
printf '%s' kodex-vault-ui >"$management_directory/vault-oidc/client-id"
openssl rand -base64 48 | tr -d '\n' >"$management_directory/vault-oidc/client-secret"
printf '%s' kodex-owner >"$management_directory/grafana-admin/admin-user"
openssl rand -base64 48 | tr -d '\n' >"$management_directory/grafana-admin/admin-password"

find "$identity_directory" "$management_directory" -type f -exec chmod 0600 {} +
find "$identity_directory" "$management_directory" -type f -exec test -s {} \; || fail 'generated material is incomplete'
unset admin_password owner_password
printf 'Identity material generated\n'
