#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck disable=SC1091
. "$REPO_ROOT/scripts/lib/env.sh"

ENV_FILE="$REPO_ROOT/.env"
RENDER_DIR=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file)
      ENV_FILE="$2"
      shift 2
      ;;
    --render-dir)
      RENDER_DIR="$2"
      shift 2
      ;;
    *)
      mattercodex_die "неизвестный аргумент: $1"
      ;;
  esac
done

if [ -z "$RENDER_DIR" ]; then
  RENDER_DIR="$(mktemp -d)"
else
  mkdir -p "$RENDER_DIR"
fi

mattercodex_load_env_file "$ENV_FILE"
mattercodex_validate_base_env
mattercodex_require_commands envsubst

TEMPLATE_DIR="$REPO_ROOT/deploy/k8s/bot-service"
APP_FILE="$REPO_ROOT/services/bot-service/app.py"

{
  cat <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${MATTERCODEX_BOT_SERVICE_CODE_CONFIGMAP}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-bot-service
    app.kubernetes.io/component: bot-service-code
data:
  app.py: |
EOF
  sed 's/^/    /' "$APP_FILE"
} > "$RENDER_DIR/10-code-configmap.yaml"

mattercodex_render_template "$TEMPLATE_DIR/configmap.yaml.tpl" "$RENDER_DIR/20-configmap.yaml"
mattercodex_render_template "$TEMPLATE_DIR/deployment.yaml.tpl" "$RENDER_DIR/30-deployment.yaml"
mattercodex_render_template "$TEMPLATE_DIR/service.yaml.tpl" "$RENDER_DIR/40-service.yaml"
mattercodex_render_template "$TEMPLATE_DIR/ingress.yaml.tpl" "$RENDER_DIR/50-ingress.yaml"

mattercodex_log "bot-service манифесты отрендерены: $RENDER_DIR"
find "$RENDER_DIR" -maxdepth 1 -type f -name '*.yaml' -print | sort
