#!/usr/bin/env sh
set -eu

export PGPASSWORD=$(cat "$PGPASSWORD_FILE")
psql --set ON_ERROR_STOP=1 --command "
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kodex_backup_reader') THEN
    EXECUTE 'CREATE ROLE kodex_backup_reader '
      'LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT '
      'NOREPLICATION BYPASSRLS';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'artifact_retention_runtime') THEN
    EXECUTE 'CREATE ROLE artifact_retention_runtime '
      'NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT '
      'NOREPLICATION NOBYPASSRLS';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'artifact_retention_runtime_g1') THEN
    EXECUTE 'CREATE ROLE artifact_retention_runtime_g1 '
      'LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT '
      'NOREPLICATION NOBYPASSRLS';
  END IF;
END
\$\$;
GRANT artifact_retention_runtime TO artifact_retention_runtime_g1
  WITH INHERIT TRUE, SET TRUE, ADMIN FALSE;
" >/dev/null

roles='kodex_backup_reader artifact_retention_runtime_g1 ira_restore_controller_g1 ira_publisher_g4 ira_readback_attestor_g4 ira_role_image_builder_issuer_g1 ira_image_admission_issuer_g1 ira_image_promotion_issuer_g1 ira_automation_scheduler_issuer_g1 ira_session_archive_issuer_g1 ira_secret_broker_issuer_g1 ira_control_api_gateway_issuer_g1 ira_control_plane_verifier_g1 ira_control_plane_resolver_g1 ira_integration_gateway_issuer_g1 ira_interaction_gateway_issuer_g1 ira_runtime_controller_issuer_g1'

until [ "$(psql --tuples-only --no-align --set ON_ERROR_STOP=1 --command "SELECT count(*) FROM pg_roles WHERE rolname IN ('$(printf '%s' "$roles" | sed "s/ /','/g")')")" -eq 17 ]; do
  sleep 3
done

for role in $roles; do
  password=$(cat "/var/run/runtime-credentials/$role")
  case "$password" in (*[!a-f0-9]*|'') echo 'PostgreSQL runtime password format is invalid' >&2; exit 1;; esac
  printf 'ALTER ROLE %s PASSWORD '\''%s'\'';\n' "$role" "$password" | psql --set ON_ERROR_STOP=1 >/dev/null
done

verified=$(psql --tuples-only --no-align --set ON_ERROR_STOP=1 --command "SELECT count(*) FROM pg_authid WHERE rolname IN ('$(printf '%s' "$roles" | sed "s/ /','/g")') AND rolpassword LIKE 'SCRAM-SHA-256%'")
[ "$verified" -eq 17 ] || { echo 'PostgreSQL runtime credential readback failed' >&2; exit 1; }

psql --dbname control_plane --set ON_ERROR_STOP=1 <<'SQL' >/dev/null
GRANT CONNECT ON DATABASE control_plane TO kodex_backup_reader;
GRANT USAGE ON SCHEMA public, control_plane TO kodex_backup_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA public, control_plane TO kodex_backup_reader;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public, control_plane TO kodex_backup_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane
  GRANT SELECT ON TABLES TO kodex_backup_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane
  GRANT USAGE, SELECT ON SEQUENCES TO kodex_backup_reader;
SQL

psql --dbname internal_rpc_authority --set ON_ERROR_STOP=1 <<'SQL' >/dev/null
GRANT CONNECT ON DATABASE internal_rpc_authority TO kodex_backup_reader;
GRANT USAGE ON SCHEMA public, internal_rpc_authority TO kodex_backup_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA public, internal_rpc_authority TO kodex_backup_reader;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public, internal_rpc_authority TO kodex_backup_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE internal_rpc_authority_owner IN SCHEMA public
  GRANT SELECT ON TABLES TO kodex_backup_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE internal_rpc_authority_owner IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO kodex_backup_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE internal_rpc_authority_readback_owner IN SCHEMA internal_rpc_authority
  GRANT SELECT ON TABLES TO kodex_backup_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE internal_rpc_authority_readback_owner IN SCHEMA internal_rpc_authority
  GRANT USAGE, SELECT ON SEQUENCES TO kodex_backup_reader;
SQL

backup_verified=$(psql --tuples-only --no-align --set ON_ERROR_STOP=1 --command "
SELECT count(*)
FROM pg_roles
WHERE rolname = 'kodex_backup_reader'
  AND rolcanlogin
  AND rolbypassrls
  AND NOT rolsuper
  AND NOT rolcreatedb
  AND NOT rolcreaterole;")
[ "$backup_verified" -eq 1 ] || {
  echo 'PostgreSQL backup reader boundary readback failed' >&2
  exit 1
}

authority_verified=$(psql --dbname internal_rpc_authority --tuples-only --no-align \
  --set ON_ERROR_STOP=1 --command "
SELECT count(*)
FROM internal_rpc_authority.authority_runtime_database_identities AS identity
JOIN pg_roles AS runtime_role ON runtime_role.rolname = identity.principal
WHERE (identity.capability, identity.principal, identity.generation) IN (
    ('PUBLISHER', 'ira_publisher_g4', 4),
    ('READBACK_ATTESTOR', 'ira_readback_attestor_g4', 4)
  )
  AND identity.lifecycle_status = 'CURRENT'
  AND identity.registered_set_digest_sha256 =
      'ed499a5c2dfdd8365c567ccdaeddaf78fd878e0c73c78db30748506625b70986'
  AND runtime_role.rolcanlogin;")
[ "$authority_verified" -eq 2 ] || {
  echo 'PostgreSQL authority identity readback failed' >&2
  exit 1
}
