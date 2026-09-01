#!/usr/bin/env sh
set -eu

apply_password() {
  role=$1
  key=$2
  password=$(cat "/var/run/runtime-credentials/$key")
  case "$password" in (*[!a-f0-9]*|'') echo 'PostgreSQL bootstrap password format is invalid' >&2; exit 1;; esac
  printf 'ALTER ROLE %s PASSWORD '\''%s'\'';\n' "$role" "$password" | psql --username postgres --dbname postgres --set ON_ERROR_STOP=1 >/dev/null
}

apply_password control_plane_migrator control_plane_migrator
apply_password control_plane_runtime_g1 control_plane_runtime_g1
apply_password artifact_retention_runtime_g1 artifact_retention_runtime_g1
apply_password internal_rpc_authority_migrator internal_rpc_authority_migrator
apply_password kodex_backup_reader kodex_backup_reader
