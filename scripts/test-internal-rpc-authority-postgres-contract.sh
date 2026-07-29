#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dsn="${INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_DSN:-}"

if [[ -z "$dsn" ]]; then
  echo "INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_DSN is required" >&2
  exit 2
fi

database_name="$(psql "$dsn" -X -Atq -v ON_ERROR_STOP=1 -c 'select current_database()')"
if [[ ! "$database_name" =~ ^mattercodex_contract_[a-z0-9_]+$ ]]; then
  echo "contract PostgreSQL database name must start with mattercodex_contract_" >&2
  exit 2
fi

psql "$dsn" -X -q -v ON_ERROR_STOP=1 \
  -f "$repo_root/contracts/authorization/v1/postgresql-readback-boundary.sql"
psql "$dsn" -X -q -v ON_ERROR_STOP=1 \
  -f "$repo_root/contracts/authorization/v1/postgresql-readback-assertions.sql"
