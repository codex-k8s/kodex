#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dsn="${INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_DSN:-}"

if [[ -z "$dsn" ]]; then
  echo "INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_DSN is required" >&2
  exit 2
fi
for role_dsn_name in \
  INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_ATTESTOR_DSN \
  INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_PUBLISHER_DSN \
  INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_VERIFIER_G1_DSN \
  INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_VERIFIER_G2_DSN
do
  if [[ -z "${!role_dsn_name:-}" ]]; then
    echo "${role_dsn_name} is required" >&2
    exit 2
  fi
done

database_name="$(psql "$dsn" -X -Atq -v ON_ERROR_STOP=1 -c 'select current_database()')"
if [[ ! "$database_name" =~ ^mattercodex_contract_[a-z0-9_]+$ ]]; then
  echo "contract PostgreSQL database name must start with mattercodex_contract_" >&2
  exit 2
fi

psql "$dsn" -X -q -v ON_ERROR_STOP=1 \
  -f "$repo_root/contracts/authorization/v1/postgresql-readback-boundary.sql"
for role_check in \
  "INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_ATTESTOR_DSN:ira_readback_attestor_g1" \
  "INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_PUBLISHER_DSN:ira_publisher_g1" \
  "INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_VERIFIER_G1_DSN:ira_control_plane_verifier_g1" \
  "INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_VERIFIER_G2_DSN:ira_control_plane_verifier_g2"
do
  role_dsn_name="${role_check%%:*}"
  expected_role="${role_check#*:}"
  actual_role="$(
    psql "${!role_dsn_name}" -X -Atq -v ON_ERROR_STOP=1 \
      -c 'select session_user'
  )"
  if [[ "$actual_role" != "$expected_role" ]]; then
    echo "${role_dsn_name} does not use the required PostgreSQL role" >&2
    exit 2
  fi
done
psql "$dsn" -X -q -v ON_ERROR_STOP=1 \
  -f "$repo_root/contracts/authorization/v1/postgresql-readback-assertions.sql"
psql "$dsn" -X -q -v ON_ERROR_STOP=1 \
  -f "$repo_root/contracts/authorization/v1/postgresql-readback-behavior.sql"
