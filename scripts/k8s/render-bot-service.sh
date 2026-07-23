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

MATTERCODEX_BOOTSTRAP_HANDOFF=false
if [[ "${1:-}" == --mattercodex-bootstrap-handoff ]]; then
  MATTERCODEX_BOOTSTRAP_HANDOFF=true
  shift
fi

mattercodex_initial_git() {
  /usr/bin/env -i \
    PATH=/usr/bin:/bin \
    HOME=/nonexistent \
    LC_ALL=C \
    GIT_CONFIG_NOSYSTEM=1 \
    GIT_CONFIG_GLOBAL=/dev/null \
    GIT_NO_REPLACE_OBJECTS=1 \
    /usr/bin/git \
      -c core.useReplaceRefs=false \
      -c "safe.directory=$MATTERCODEX_PHYSICAL_REPO_ROOT" \
      -c core.hooksPath=/dev/null \
      -C "$MATTERCODEX_PHYSICAL_REPO_ROOT" \
      "$@"
}

mattercodex_initial_require_committed_fd() {
  local relative_path="$1"
  local fd="$2"
  local label="$3"
  local expected_object actual_object object_type
  local fd_path="/proc/$$/fd/$fd"

  if [[ -z "${MATTERCODEX_TRUSTED_HEAD:-}" ]]; then
    builtin printf 'FAIL: exact trusted Git HEAD не закреплён для initial commitment\n' >&2
    exit 1
  fi
  expected_object="$(mattercodex_initial_git rev-parse --verify "$MATTERCODEX_TRUSTED_HEAD:$relative_path")" || {
    builtin printf 'FAIL: %s отсутствует в HEAD trusted checkout\n' "$label" >&2
    exit 1
  }
  object_type="$(mattercodex_initial_git cat-file -t "$expected_object")" || {
    builtin printf 'FAIL: не удалось определить Git object type для %s\n' "$label" >&2
    exit 1
  }
  if [[ "$object_type" != blob ]]; then
    builtin printf 'FAIL: %s в HEAD trusted checkout не является blob\n' "$label" >&2
    exit 1
  fi
  actual_object="$(mattercodex_initial_git hash-object --no-filters "$fd_path")" || {
    builtin printf 'FAIL: не удалось вычислить content commitment для %s\n' "$label" >&2
    exit 1
  }
  if [[ "$actual_object" != "$expected_object" ]]; then
    builtin printf 'FAIL: %s не совпадает с content commitment HEAD trusted checkout\n' "$label" >&2
    exit 1
  fi
}

mattercodex_resolve_bootstrap_paths() {
  local script_path="${BASH_SOURCE[0]}"
  local script_dir canonical_script_path bootstrap_path
  local topology committed_root actual_root current_head

  if [[ "$MATTERCODEX_BOOTSTRAP_HANDOFF" == true ]]; then
    REPO_ROOT="${MATTERCODEX_BOOTSTRAP_HANDOFF_REPO_ROOT:-}"
    bootstrap_path="${MATTERCODEX_BOOTSTRAP_HANDOFF_HELPER_PATH:-}"
    case "$script_path:$bootstrap_path" in
      /proc/[0-9]*/fd/[0-9]*:/proc/[0-9]*/fd/[0-9]*) ;;
      *)
        builtin printf 'FAIL: internal render handoff требует удержанные /proc descriptor paths\n' >&2
        exit 1
        ;;
    esac
    case "$REPO_ROOT" in
      /*) ;;
      *)
        builtin printf 'FAIL: internal render handoff требует absolute trusted repository root\n' >&2
        exit 1
        ;;
    esac
    SCRIPT_DIR="$REPO_ROOT/scripts/k8s"
  else
    case "$script_path" in
      /*) ;;
      */*) script_path="$(builtin pwd -P)/$script_path" ;;
      *)
        builtin printf 'FAIL: невозможно определить абсолютный путь render-bot-service.sh без PATH lookup\n' >&2
        exit 1
        ;;
    esac
    if [[ ! -f "$script_path" || -L "$script_path" ]]; then
      builtin printf 'FAIL: защищённая точка входа render-bot-service.sh должна быть обычным файлом, а не symlink\n' >&2
      exit 1
    fi
    script_dir="${script_path%/*}"
    SCRIPT_DIR="$(builtin cd -P -- "$script_dir" && builtin pwd -P)" || {
      builtin printf 'FAIL: невозможно определить trusted script directory render-bot-service.sh\n' >&2
      exit 1
    }
    REPO_ROOT="$(builtin cd -P -- "$SCRIPT_DIR/../.." && builtin pwd -P)" || {
      builtin printf 'FAIL: невозможно определить trusted repository root render-bot-service.sh\n' >&2
      exit 1
    }
    bootstrap_path="$REPO_ROOT/scripts/lib/bootstrap.sh"
  fi
  for trusted_primitive in /usr/bin/env /usr/bin/git /usr/bin/stat /bin/bash; do
    if [[ ! -x "$trusted_primitive" || -L "$trusted_primitive" ]]; then
      builtin printf 'FAIL: обязательный absolute system primitive недоступен как regular executable: %s\n' "$trusted_primitive" >&2
      exit 1
    fi
  done
  if [[ ! -d "/proc/$$/fd" ]]; then
    builtin printf 'FAIL: /proc descriptor boundary недоступна для protected bootstrap\n' >&2
    exit 1
  fi
  exec {MATTERCODEX_ENTRYPOINT_FD}<"$script_path" || {
    builtin printf 'FAIL: невозможно удержать descriptor render-bot-service.sh\n' >&2
    exit 1
  }
  topology="$(LC_ALL=C /usr/bin/stat -Lc '%h:%F' "/proc/$$/fd/$MATTERCODEX_ENTRYPOINT_FD")" || {
    builtin printf 'FAIL: невозможно проверить topology render-bot-service.sh\n' >&2
    exit 1
  }
  if [[ "$topology" != "1:regular file" ]]; then
    builtin printf 'FAIL: protected entrypoint render-bot-service.sh должен быть regular file с link count 1\n' >&2
    exit 1
  fi
  canonical_script_path="$SCRIPT_DIR/render-bot-service.sh"
  if [[ ! -f "$canonical_script_path" || -L "$canonical_script_path" || ! "$script_path" -ef "$canonical_script_path" ]]; then
    builtin printf 'FAIL: render-bot-service.sh не совпадает с canonical protected entrypoint текущего checkout\n' >&2
    exit 1
  fi
  MATTERCODEX_PHYSICAL_REPO_ROOT="$REPO_ROOT"
  if [[ ! -d "$REPO_ROOT/.git" || -L "$REPO_ROOT/.git" ||
        ! -d "$REPO_ROOT/scripts" || -L "$REPO_ROOT/scripts" ||
        ! -d "$REPO_ROOT/scripts/lib" || -L "$REPO_ROOT/scripts/lib" ]]; then
    builtin printf 'FAIL: trusted repository root должен иметь physical .git/scripts/lib topology без symlink\n' >&2
    exit 1
  fi
  committed_root="$(mattercodex_initial_git rev-parse --show-toplevel)" || {
    builtin printf 'FAIL: trusted repository root не является поддерживаемым Git checkout\n' >&2
    exit 1
  }
  actual_root="$(builtin cd -P -- "$committed_root" && builtin pwd -P)" || {
    builtin printf 'FAIL: невозможно определить physical Git checkout root\n' >&2
    exit 1
  }
  if [[ "$actual_root" != "$REPO_ROOT" ]]; then
    builtin printf 'FAIL: render-bot-service.sh не принадлежит trusted Git checkout root\n' >&2
    exit 1
  fi
  current_head="$(mattercodex_initial_git rev-parse --verify 'HEAD^{commit}')" || {
    builtin printf 'FAIL: trusted Git checkout не содержит HEAD commit\n' >&2
    exit 1
  }
  if [[ "$MATTERCODEX_BOOTSTRAP_HANDOFF" == true ]]; then
    MATTERCODEX_TRUSTED_HEAD="${MATTERCODEX_BOOTSTRAP_HANDOFF_TRUSTED_HEAD:-}"
    if [[ -z "$MATTERCODEX_TRUSTED_HEAD" || "$current_head" != "$MATTERCODEX_TRUSTED_HEAD" ]]; then
      builtin printf 'FAIL: trusted Git HEAD изменён до internal render handoff\n' >&2
      exit 1
    fi
  else
    MATTERCODEX_TRUSTED_HEAD="$current_head"
  fi
  mattercodex_initial_require_committed_fd \
    scripts/k8s/render-bot-service.sh \
    "$MATTERCODEX_ENTRYPOINT_FD" \
    "protected entrypoint render-bot-service.sh"

  if [[ ! -f "$bootstrap_path" ||
        ( "$MATTERCODEX_BOOTSTRAP_HANDOFF" != true && -L "$bootstrap_path" ) ]]; then
    builtin printf 'FAIL: trusted bootstrap helper должен быть обычным файлом или удержанным handoff descriptor\n' >&2
    exit 1
  fi
  exec {MATTERCODEX_BOOTSTRAP_HELPER_FD}<"$bootstrap_path" || {
    builtin printf 'FAIL: невозможно удержать descriptor trusted bootstrap helper\n' >&2
    exit 1
  }
  topology="$(LC_ALL=C /usr/bin/stat -Lc '%h:%F' "/proc/$$/fd/$MATTERCODEX_BOOTSTRAP_HELPER_FD")" || {
    builtin printf 'FAIL: невозможно проверить topology trusted bootstrap helper\n' >&2
    exit 1
  }
  if [[ "$topology" != "1:regular file" || ! "$bootstrap_path" -ef "/proc/$$/fd/$MATTERCODEX_BOOTSTRAP_HELPER_FD" ]]; then
    builtin printf 'FAIL: trusted bootstrap helper должен быть stable regular file с link count 1\n' >&2
    exit 1
  fi
  mattercodex_initial_require_committed_fd \
    scripts/lib/bootstrap.sh \
    "$MATTERCODEX_BOOTSTRAP_HELPER_FD" \
    "trusted bootstrap helper"
  if [[ ! "$canonical_script_path" -ef "/proc/$$/fd/$MATTERCODEX_ENTRYPOINT_FD" ||
        ! "$REPO_ROOT/scripts/lib/bootstrap.sh" -ef "/proc/$$/fd/$MATTERCODEX_BOOTSTRAP_HELPER_FD" ]]; then
    builtin printf 'FAIL: trusted bootstrap pathname изменён до descriptor-bound source\n' >&2
    exit 1
  fi
}

mattercodex_resolve_bootstrap_paths
# shellcheck disable=SC1091
. "/proc/$$/fd/$MATTERCODEX_BOOTSTRAP_HELPER_FD"
mattercodex_establish_bootstrap scripts/k8s/render-bot-service.sh false false

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
