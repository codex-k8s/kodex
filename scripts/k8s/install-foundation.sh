#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck disable=SC1091
. "$REPO_ROOT/scripts/lib/env.sh"

ENV_FILE="$REPO_ROOT/.env"
DRY_RUN_MODE="server"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file)
      ENV_FILE="$2"
      shift 2
      ;;
    --apply)
      DRY_RUN_MODE="none"
      shift
      ;;
    --dry-run=server)
      DRY_RUN_MODE="server"
      shift
      ;;
    --dry-run=client)
      DRY_RUN_MODE="client"
      shift
      ;;
    *)
      mattercodex_die "неизвестный аргумент: $1"
      ;;
  esac
done

mattercodex_load_env_file "$ENV_FILE"
mattercodex_validate_base_env
mattercodex_require_commands kubectl envsubst base64 jq

DRY_RUN_ARG="$(mattercodex_kubectl_dry_run_arg "$DRY_RUN_MODE")"
TEMPLATE_DIR="$REPO_ROOT/deploy/k8s/mattermost"

mattercodex_log "применяется манифест namespace"
mattercodex_render_template "$TEMPLATE_DIR/namespace.yaml.tpl" - | kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f - >/dev/null

if mattercodex_bool "${MATTERCODEX_CREATE_CLUSTER_ISSUER:-false}"; then
  mattercodex_log "применяется манифест ClusterIssuer"
  mattercodex_render_template "$TEMPLATE_DIR/cluster-issuer.yaml.tpl" - | kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f - >/dev/null
fi

EXISTING_POSTGRES_SECRET=""
if [ "$DRY_RUN_MODE" = "none" ]; then
  EXISTING_POSTGRES_SECRET="$(kubectl -n "$MATTERCODEX_NAMESPACE" get secret "$MATTERCODEX_POSTGRES_SECRET" -o json 2>/dev/null || true)"
fi

if [ -n "$EXISTING_POSTGRES_SECRET" ]; then
  POSTGRES_DB_B64="$(jq -r '.data["postgres-db"] // empty' <<<"$EXISTING_POSTGRES_SECRET")"
  POSTGRES_USER_B64="$(jq -r '.data["postgres-user"] // empty' <<<"$EXISTING_POSTGRES_SECRET")"
  POSTGRES_PASSWORD_B64="$(jq -r '.data["postgres-password"] // empty' <<<"$EXISTING_POSTGRES_SECRET")"
  POSTGRES_DSN_B64="$(jq -r '.data["mattermost-datasource"] // empty' <<<"$EXISTING_POSTGRES_SECRET")"
  EXISTING_RUNTIME_KEYS="$(jq '[.data["bot-service-runtime-user"], .data["bot-service-runtime-password"], .data["bot-service-runtime-datasource"]] | map(select(. != null and . != "")) | length' <<<"$EXISTING_POSTGRES_SECRET")"
  if [ -z "$POSTGRES_DB_B64" ] || [ -z "$POSTGRES_USER_B64" ] || [ -z "$POSTGRES_PASSWORD_B64" ] || [ -z "$POSTGRES_DSN_B64" ]; then
    mattercodex_die "существующий PostgreSQL secret не содержит полный migration-owner контракт"
  fi
  if [ "$EXISTING_RUNTIME_KEYS" = "3" ]; then
    mattercodex_log "PostgreSQL secret: runtime credentials уже подготовлены, ротация не выполняется"
    exit 0
  fi
  if [ "$EXISTING_RUNTIME_KEYS" != "0" ]; then
    mattercodex_die "PostgreSQL secret содержит неполный набор bot-service runtime credentials"
  fi
  POSTGRES_PASSWORD=""
else
  POSTGRES_PASSWORD="${MATTERCODEX_POSTGRES_PASSWORD:-$(mattercodex_generate_password)}"
  POSTGRES_DB_B64="$(printf '%s' "$MATTERCODEX_POSTGRES_DB" | base64 | tr -d '\n')"
  POSTGRES_USER_B64="$(printf '%s' "$MATTERCODEX_POSTGRES_USER" | base64 | tr -d '\n')"
  POSTGRES_PASSWORD_B64="$(printf '%s' "$POSTGRES_PASSWORD" | base64 | tr -d '\n')"
  POSTGRES_DSN="postgres://${MATTERCODEX_POSTGRES_USER}:${POSTGRES_PASSWORD}@mattermost-postgres.${MATTERCODEX_NAMESPACE}.svc.cluster.local:5432/${MATTERCODEX_POSTGRES_DB}?sslmode=disable&connect_timeout=10"
  POSTGRES_DSN_B64="$(printf '%s' "$POSTGRES_DSN" | base64 | tr -d '\n')"
fi

POSTGRES_RUNTIME_PASSWORD="${MATTERCODEX_POSTGRES_RUNTIME_PASSWORD:-$(mattercodex_generate_password)}"
POSTGRES_RUNTIME_DSN="postgres://${MATTERCODEX_POSTGRES_RUNTIME_USER}:${POSTGRES_RUNTIME_PASSWORD}@mattermost-postgres.${MATTERCODEX_NAMESPACE}.svc.cluster.local:5432/${MATTERCODEX_POSTGRES_DB}?sslmode=disable&connect_timeout=10"
POSTGRES_RUNTIME_USER_B64="$(printf '%s' "$MATTERCODEX_POSTGRES_RUNTIME_USER" | base64 | tr -d '\n')"
POSTGRES_RUNTIME_PASSWORD_B64="$(printf '%s' "$POSTGRES_RUNTIME_PASSWORD" | base64 | tr -d '\n')"
POSTGRES_RUNTIME_DSN_B64="$(printf '%s' "$POSTGRES_RUNTIME_DSN" | base64 | tr -d '\n')"
export POSTGRES_DB_B64
export POSTGRES_USER_B64
export POSTGRES_PASSWORD_B64
export POSTGRES_DSN_B64
export POSTGRES_RUNTIME_USER_B64
export POSTGRES_RUNTIME_PASSWORD_B64
export POSTGRES_RUNTIME_DSN_B64

mattercodex_log "применяется PostgreSQL secret"
mattercodex_render_template "$TEMPLATE_DIR/postgres-secret.yaml.tpl" - | kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f - >/dev/null

mattercodex_log "foundation шаг завершен"
