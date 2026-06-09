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
mattercodex_require_commands ssh envsubst base64

TEMPLATE_DIR="$REPO_ROOT/deploy/k8s/mattermost"

mattercodex_log "применяется namespace на целевом сервере"
mattercodex_render_template "$TEMPLATE_DIR/namespace.yaml.tpl" - | mattercodex_remote_kubectl_apply_stdin "$DRY_RUN_MODE"

if mattercodex_bool "${MATTERCODEX_CREATE_CLUSTER_ISSUER:-false}"; then
  mattercodex_log "применяется ClusterIssuer на целевом сервере"
  mattercodex_render_template "$TEMPLATE_DIR/cluster-issuer.yaml.tpl" - | mattercodex_remote_kubectl_apply_stdin "$DRY_RUN_MODE"
fi

NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_NAMESPACE")"
SECRET_Q="$(mattercodex_shell_quote "$MATTERCODEX_POSTGRES_SECRET")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"
SECRET_DRY_RUN_MODE="$DRY_RUN_MODE"

if [ "$DRY_RUN_MODE" = "none" ] && mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q get secret $SECRET_Q >/dev/null 2>&1"; then
  mattercodex_log "PostgreSQL secret: уже существует на сервере, ротация не выполняется"
  exit 0
fi

if [ "$DRY_RUN_MODE" = "server" ] && ! mattercodex_ssh "$REMOTE_KUBECTL get namespace $NAMESPACE_Q >/dev/null 2>&1"; then
  mattercodex_log "namespace еще не создан; PostgreSQL secret проверяется через remote client dry-run"
  SECRET_DRY_RUN_MODE="client"
fi

POSTGRES_PASSWORD="${MATTERCODEX_POSTGRES_PASSWORD:-$(mattercodex_generate_password)}"
POSTGRES_DSN="postgres://${MATTERCODEX_POSTGRES_USER}:${POSTGRES_PASSWORD}@mattermost-postgres.${MATTERCODEX_NAMESPACE}.svc.cluster.local:5432/${MATTERCODEX_POSTGRES_DB}?sslmode=disable&connect_timeout=10"

POSTGRES_DB_B64="$(printf '%s' "$MATTERCODEX_POSTGRES_DB" | base64 | tr -d '\n')"
POSTGRES_USER_B64="$(printf '%s' "$MATTERCODEX_POSTGRES_USER" | base64 | tr -d '\n')"
POSTGRES_PASSWORD_B64="$(printf '%s' "$POSTGRES_PASSWORD" | base64 | tr -d '\n')"
POSTGRES_DSN_B64="$(printf '%s' "$POSTGRES_DSN" | base64 | tr -d '\n')"

mattercodex_log "применяется PostgreSQL secret на целевом сервере"
cat <<EOF | mattercodex_remote_kubectl_apply_stdin "$SECRET_DRY_RUN_MODE"
apiVersion: v1
kind: Secret
metadata:
  name: ${MATTERCODEX_POSTGRES_SECRET}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-postgres
    app.kubernetes.io/component: database
type: Opaque
data:
  postgres-db: ${POSTGRES_DB_B64}
  postgres-user: ${POSTGRES_USER_B64}
  postgres-password: ${POSTGRES_PASSWORD_B64}
  mattermost-datasource: ${POSTGRES_DSN_B64}
EOF

mattercodex_log "remote foundation шаг завершен"
