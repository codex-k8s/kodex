#!/bin/bash -p

set -euo pipefail

mattercodex_require_protected_shell_startup() {
  local environment_entry environment_key

  if [[ "$-" != *p* ]]; then
    builtin printf 'FAIL: render-bot-service.sh нужно запускать напрямую, чтобы Bash privileged mode действовал до startup hooks\n' >&2
    exit 1
  fi
  if [[ -n "${BASH_ENV+x}" || -n "${ENV+x}" ]]; then
    builtin printf 'FAIL: BASH_ENV и ENV запрещены для render-bot-service.sh\n' >&2
    exit 1
  fi
  while IFS= read -r -d '' environment_entry; do
    environment_key="${environment_entry%%=*}"
    case "$environment_key" in
      BASH_FUNC_*%%)
        builtin printf 'FAIL: экспортированные shell functions запрещены для render-bot-service.sh\n' >&2
        exit 1
        ;;
    esac
  done < <(/usr/bin/env -0)
}

mattercodex_require_protected_shell_startup
builtin unset BASH_ENV ENV

mattercodex_resolve_bootstrap_paths() {
  local script_path="${BASH_SOURCE[0]}"
  local script_dir

  case "$script_path" in
    /*) ;;
    */*) script_path="$(builtin pwd -P)/$script_path" ;;
    *)
      builtin printf 'FAIL: невозможно определить абсолютный путь render-bot-service.sh без PATH lookup\n' >&2
      exit 1
      ;;
  esac
  script_dir="${script_path%/*}"
  SCRIPT_DIR="$(builtin cd -P -- "$script_dir" && builtin pwd -P)" || {
    builtin printf 'FAIL: невозможно определить trusted script directory render-bot-service.sh\n' >&2
    exit 1
  }
  REPO_ROOT="$(builtin cd -P -- "$SCRIPT_DIR/../.." && builtin pwd -P)" || {
    builtin printf 'FAIL: невозможно определить trusted repository root render-bot-service.sh\n' >&2
    exit 1
  }
}

mattercodex_resolve_bootstrap_paths
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
mattercodex_require_protected_shell_startup
mattercodex_validate_base_env
mattercodex_require_commands envsubst sha256sum

TEMPLATE_DIR="$REPO_ROOT/deploy/k8s/bot-service"

rm -f "$RENDER_DIR/02-image-registry.yaml" "$RENDER_DIR/03-kaniko-context-pvc.yaml" "$RENDER_DIR/15-runtime-limits.yaml"

if mattercodex_bool "$MATTERCODEX_IMAGE_REGISTRY_MANAGED"; then
  mattercodex_render_template "$TEMPLATE_DIR/image-registry.yaml.tpl" "$RENDER_DIR/02-image-registry.yaml"
fi
if [ "$MATTERCODEX_IMAGE_BUILD_STRATEGY" = "kaniko" ]; then
  mattercodex_render_template "$TEMPLATE_DIR/kaniko-context-pvc.yaml.tpl" "$RENDER_DIR/03-kaniko-context-pvc.yaml"
fi
mattercodex_render_template "$TEMPLATE_DIR/configmap.yaml.tpl" "$RENDER_DIR/10-configmap.yaml"
CONFIG_REVISION_OUTPUT="$(sha256sum "$RENDER_DIR/10-configmap.yaml")"
MATTERCODEX_BOT_SERVICE_POD_INPUT_REVISION="${CONFIG_REVISION_OUTPUT%% *}"
export MATTERCODEX_BOT_SERVICE_POD_INPUT_REVISION
if mattercodex_bool "$MATTERCODEX_RUNTIME_ENABLED" && mattercodex_bool "$MATTERCODEX_RUNTIME_LIMITS_ENABLED"; then
  mattercodex_render_template "$TEMPLATE_DIR/runtime-limits.yaml.tpl" "$RENDER_DIR/15-runtime-limits.yaml"
fi
mattercodex_render_template "$TEMPLATE_DIR/rbac.yaml.tpl" "$RENDER_DIR/20-rbac.yaml"
mattercodex_render_template "$TEMPLATE_DIR/deployment.yaml.tpl" "$RENDER_DIR/30-deployment.yaml"
mattercodex_render_template "$TEMPLATE_DIR/service.yaml.tpl" "$RENDER_DIR/40-service.yaml"
mattercodex_render_template "$TEMPLATE_DIR/ingress.yaml.tpl" "$RENDER_DIR/50-ingress.yaml"

mattercodex_log "bot-service манифесты отрендерены: $RENDER_DIR"
find "$RENDER_DIR" -maxdepth 1 -type f -name '*.yaml' -print | sort
