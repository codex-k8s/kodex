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
mattercodex_require_commands kubectl envsubst

DRY_RUN_ARG="$(mattercodex_kubectl_dry_run_arg "$DRY_RUN_MODE")"
TEMPLATE_DIR="$REPO_ROOT/deploy/k8s/mattermost"

mattercodex_log "применяется манифест namespace"
mattercodex_render_template "$TEMPLATE_DIR/namespace.yaml.tpl" - | kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f -

if mattercodex_bool "${MATTERCODEX_CREATE_CLUSTER_ISSUER:-false}"; then
  mattercodex_log "применяется манифест ClusterIssuer"
  mattercodex_render_template "$TEMPLATE_DIR/cluster-issuer.yaml.tpl" - | kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f -
fi

if [ "$DRY_RUN_MODE" = "none" ] && kubectl -n "$MATTERCODEX_NAMESPACE" get secret "$MATTERCODEX_POSTGRES_SECRET" >/dev/null 2>&1; then
  mattercodex_log "PostgreSQL secret: уже существует, ротация не выполняется"
  exit 0
fi

POSTGRES_PASSWORD="${MATTERCODEX_POSTGRES_PASSWORD:-$(mattercodex_generate_password)}"
POSTGRES_DSN="postgres://${MATTERCODEX_POSTGRES_USER}:${POSTGRES_PASSWORD}@mattermost-postgres.${MATTERCODEX_NAMESPACE}.svc.cluster.local:5432/${MATTERCODEX_POSTGRES_DB}?sslmode=disable&connect_timeout=10"

mattercodex_log "применяется PostgreSQL secret"
kubectl -n "$MATTERCODEX_NAMESPACE" create secret generic "$MATTERCODEX_POSTGRES_SECRET" \
  --from-literal=postgres-db="$MATTERCODEX_POSTGRES_DB" \
  --from-literal=postgres-user="$MATTERCODEX_POSTGRES_USER" \
  --from-literal=postgres-password="$POSTGRES_PASSWORD" \
  --from-literal=mattermost-datasource="$POSTGRES_DSN" \
  --dry-run=client \
  -o yaml | kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f -

mattercodex_log "foundation шаг завершен"
