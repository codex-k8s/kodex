#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_schema="$repository_root/libs/go/eventing/postgresinbox/schema.sql"
migration="$repository_root/services/internal/runtime-controller/cmd/cli/migrations/20260803018800_runtime_controller.sql"
temporary_directory="$(mktemp -d)"
trap 'rm -rf "$temporary_directory"' EXIT

awk '/^INSERT INTO runtime_event_schema_versions \($/ {exit} {print}' \
  "$source_schema" > "$temporary_directory/source.sql"
awk '
  BEGIN { copy = 0 }
  /^-- Нормативный schema contract postgresinbox версии 1\.$/ { copy = 1 }
  /^CREATE TABLE runtime_configuration_projection \(/ { exit }
  copy { print }
' "$migration" > "$temporary_directory/migration.sql"

diff -u "$temporary_directory/source.sql" "$temporary_directory/migration.sql"
