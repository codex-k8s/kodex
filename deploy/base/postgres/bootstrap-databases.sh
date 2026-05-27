#!/bin/sh
set -eu

POSTGRES_HOST="${POSTGRES_HOST:-postgres}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
export PGPASSWORD="${POSTGRES_PASSWORD}"

wait_for_postgres() {
  until pg_isready -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" >/dev/null 2>&1; do
    sleep 2
  done
}

create_database() {
  database="$1"
  escaped_ident="$(printf '%s' "${database}" | sed 's/"/""/g')"
  escaped_literal="$(printf '%s' "${database}" | sed "s/'/''/g")"

  if psql -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" -tAc "SELECT 1 FROM pg_database WHERE datname = '${escaped_literal}'" | grep -q 1; then
    echo "database ${database} already exists"
    return
  fi

  psql -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"${escaped_ident}\""
}

wait_for_postgres
create_database "${KODEX_ACCESS_MANAGER_DATABASE_NAME:-kodex_access_manager}"
create_database "${KODEX_PROJECT_CATALOG_DATABASE_NAME:-kodex_project_catalog}"
create_database "${KODEX_PACKAGE_HUB_DATABASE_NAME:-kodex_package_hub}"
create_database "${KODEX_PROVIDER_HUB_DATABASE_NAME:-kodex_provider_hub}"
create_database "${KODEX_INTERACTION_HUB_DATABASE_NAME:-kodex_interaction_hub}"
create_database "${KODEX_FLEET_MANAGER_DATABASE_NAME:-kodex_fleet_manager}"
create_database "${KODEX_RUNTIME_MANAGER_DATABASE_NAME:-kodex_runtime_manager}"
create_database "${KODEX_PLATFORM_EVENT_LOG_DATABASE_NAME:-kodex_platform_event_log}"
