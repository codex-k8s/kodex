#!/usr/bin/env sh
set -eu

export PGPASSWORD=$(cat "$PGPASSWORD_FILE")
roles='ira_restore_controller_g1 ira_publisher_g4 ira_readback_attestor_g4 ira_role_image_builder_issuer_g1 ira_image_admission_issuer_g1 ira_image_promotion_issuer_g1 ira_automation_scheduler_issuer_g1 ira_session_archive_issuer_g1 ira_control_api_gateway_issuer_g1 ira_control_plane_verifier_g1 ira_control_plane_resolver_g1 ira_integration_gateway_issuer_g1 ira_interaction_gateway_issuer_g1 ira_runtime_controller_issuer_g1'

until [ "$(psql --tuples-only --no-align --set ON_ERROR_STOP=1 --command "SELECT count(*) FROM pg_roles WHERE rolname IN ('$(printf '%s' "$roles" | sed "s/ /','/g")')")" -eq 14 ]; do
  sleep 3
done

for role in $roles; do
  password=$(cat "/var/run/runtime-credentials/$role")
  case "$password" in (*[!a-f0-9]*|'') echo 'PostgreSQL runtime password format is invalid' >&2; exit 1;; esac
  printf 'ALTER ROLE %s PASSWORD '\''%s'\'';\n' "$role" "$password" | psql --set ON_ERROR_STOP=1 >/dev/null
done

verified=$(psql --tuples-only --no-align --set ON_ERROR_STOP=1 --command "SELECT count(*) FROM pg_authid WHERE rolname IN ('$(printf '%s' "$roles" | sed "s/ /','/g")') AND rolpassword LIKE 'SCRAM-SHA-256%'")
[ "$verified" -eq 14 ] || { echo 'PostgreSQL runtime credential readback failed' >&2; exit 1; }

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
