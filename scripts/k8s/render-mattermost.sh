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

TEMPLATE_DIR="$REPO_ROOT/deploy/k8s/mattermost"

mattercodex_render_template "$TEMPLATE_DIR/namespace.yaml.tpl" "$RENDER_DIR/00-namespace.yaml"
mattercodex_render_template "$TEMPLATE_DIR/postgres.yaml.tpl" "$RENDER_DIR/10-postgres.yaml"
mattercodex_render_template "$TEMPLATE_DIR/mattermost.yaml.tpl" "$RENDER_DIR/20-mattermost.yaml"
mattercodex_render_template "$TEMPLATE_DIR/ingress.yaml.tpl" "$RENDER_DIR/30-ingress.yaml"

if mattercodex_bool "${MATTERCODEX_CREATE_CLUSTER_ISSUER:-false}"; then
  mattercodex_render_template "$TEMPLATE_DIR/cluster-issuer.yaml.tpl" "$RENDER_DIR/05-cluster-issuer.yaml"
fi

mattercodex_log "манифесты отрендерены: $RENDER_DIR"
find "$RENDER_DIR" -maxdepth 1 -type f -name '*.yaml' -print | sort
