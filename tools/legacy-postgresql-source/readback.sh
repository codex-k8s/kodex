#!/usr/bin/env bash

set -euo pipefail

readonly credential_dir=/var/run/secrets/mattercodex/legacy-postgresql-source
readonly config_dir=/var/run/config/mattercodex/legacy-postgresql-source
readonly trust_dir="${config_dir}/trust"
readonly scratch_dir=/var/run/mattercodex/legacy-postgresql-source
readonly expected_principal=matter_codex_migration_g1

die() {
  printf 'legacy PostgreSQL source readback failed: %s\n' "$*" >&2
  exit 1
}

read_secret_file() {
  local path="$1"
  [ -s "$path" ] || die "required credential file is missing"
  tr -d '\r\n' < "$path"
}

export PGUSER="$(read_secret_file "${credential_dir}/username")"
export PGPASSWORD="$(read_secret_file "${credential_dir}/password")"
export PGDATABASE="$(read_secret_file "${credential_dir}/database")"
export PGOPTIONS='-c statement_timeout=10000 -c lock_timeout=2000 -c default_transaction_read_only=on'

[ "$PGUSER" = "$expected_principal" ] || die "unexpected PostgreSQL principal"
[[ "$PGPASSWORD" =~ ^[a-f0-9]{64}$ ]] || die "invalid PostgreSQL credential contract"
[[ "$PGDATABASE" =~ ^[A-Za-z_][A-Za-z0-9_-]{0,62}$ ]] || die "invalid PostgreSQL database name"
[ -s "${trust_dir}/ca.pem" ] || die "trusted CA is missing"
[ -s "${trust_dir}/server.pem" ] || die "expected server certificate is missing"
[ -s "${config_dir}/principal__readback.sql" ] || die "principal readback query is missing"

source_dsn="$(read_secret_file "${credential_dir}/source-dsn")"
expected_dsn="postgres://${expected_principal}:${PGPASSWORD}@${PGHOST}:5432/${PGDATABASE}?sslmode=verify-full&connect_timeout=10"
[ "$source_dsn" = "$expected_dsn" ] || die "source DSN contract is invalid"
unset source_dsn
unset expected_dsn

handshake_file="${scratch_dir}/handshake.pem"
served_file="${scratch_dir}/served.pem"
served_der="${scratch_dir}/served.der"
expected_der="${scratch_dir}/expected.der"

openssl s_client \
  -starttls postgres \
  -connect "${PGHOST}:${PGPORT}" \
  -servername "$PGHOST" \
  -verify_hostname "$PGHOST" \
  -CAfile "${trust_dir}/ca.pem" \
  -verify_return_error \
  -tls1_3 \
  -showcerts \
  < /dev/null > "$handshake_file" 2>/dev/null || die "TLS handshake failed"

awk '
  /-----BEGIN CERTIFICATE-----/ { capture = 1 }
  capture { print }
  /-----END CERTIFICATE-----/ { exit }
' "$handshake_file" > "$served_file"
[ -s "$served_file" ] || die "served certificate is missing"

openssl verify -CAfile "${trust_dir}/ca.pem" "$served_file" >/dev/null 2>&1 ||
  die "served certificate is not trusted"
openssl x509 -in "$served_file" -outform DER > "$served_der"
openssl x509 -in "${trust_dir}/server.pem" -outform DER > "$expected_der"
cmp -s "$served_der" "$expected_der" || die "served certificate differs from accepted immutable snapshot"

actual_san="$(openssl x509 -in "$served_file" -noout -ext subjectAltName | tail -n +2 | tr -d '[:space:]')"
[ "$actual_san" = "DNS:${PGHOST}" ] || die "served certificate SAN set is not exact"

principal_result="$({
  sed "s/@required_role/'matter_codex_migration'/g" "${config_dir}/principal__readback.sql"
} | psql -X -v ON_ERROR_STOP=1 -At 2>/dev/null)"
[ "$principal_result" = 't|t|t' ] || die "canonical principal readback rejected effective privileges"

metadata_result="$(psql -X -v ON_ERROR_STOP=1 -At 2>/dev/null <<'SQL'
SELECT EXISTS (
           SELECT 1
           FROM pg_catalog.pg_stat_ssl
           WHERE pid = pg_catalog.pg_backend_pid()
             AND ssl
             AND version = 'TLSv1.3'
       )
       AND current_setting('ssl') = 'on'
       AND current_setting('ssl_min_protocol_version') = 'TLSv1.3'
       AND current_setting('ssl_max_protocol_version') = 'TLSv1.3'
       AND current_user = session_user
       AND session_user = 'matter_codex_migration_g1'
       AND has_table_privilege(session_user, 'public.matter_codex_legacy_data_cutovers', 'SELECT')
       AND has_table_privilege(session_user, 'public.matter_codex_legacy_data_cutovers', 'INSERT')
       AND has_table_privilege(session_user, 'public.matter_codex_legacy_data_cutovers', 'UPDATE')
       AND NOT has_table_privilege(session_user, 'public.matter_codex_legacy_data_cutovers', 'DELETE')
       AND NOT has_table_privilege(session_user, 'public.matter_codex_legacy_data_cutovers', 'TRUNCATE')
       AND NOT has_table_privilege(session_user, 'public.matter_codex_legacy_data_cutovers', 'REFERENCES')
       AND NOT has_table_privilege(session_user, 'public.matter_codex_legacy_data_cutovers', 'TRIGGER')
       AND has_schema_privilege(session_user, 'public', 'USAGE')
       AND NOT has_schema_privilege(session_user, 'public', 'CREATE')
       AND NOT EXISTS (
           SELECT 1 FROM pg_catalog.pg_class
           WHERE relowner = (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = session_user)
       )
       AND NOT EXISTS (
           SELECT 1 FROM pg_catalog.pg_namespace
           WHERE nspowner = (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = session_user)
       )
       AND NOT EXISTS (
           SELECT 1 FROM pg_catalog.pg_database
           WHERE datdba = (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = session_user)
       );
SQL
)"
[ "$metadata_result" = 't' ] || die "TLS or privilege metadata readback failed"

printf 'legacy PostgreSQL source readback: ok\n'
