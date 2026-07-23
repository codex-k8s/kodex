#!/bin/bash -p

set -euo pipefail

mattercodex_require_protected_shell_startup() {
  local environment_entry environment_key

  if [[ "$-" != *p* ]]; then
    printf 'FAIL: install-bot-service.sh нужно запускать напрямую, чтобы Bash privileged mode действовал до startup hooks\n' >&2
    exit 1
  fi
  if [[ -n "${BASH_ENV+x}" || -n "${ENV+x}" ]]; then
    printf 'FAIL: BASH_ENV и ENV запрещены для install-bot-service.sh\n' >&2
    exit 1
  fi
  while IFS= read -r -d '' environment_entry; do
    environment_key="${environment_entry%%=*}"
    case "$environment_key" in
      BASH_FUNC_*%%)
        printf 'FAIL: экспортированные shell functions запрещены для install-bot-service.sh\n' >&2
        exit 1
        ;;
    esac
  done < <(/usr/bin/env -0)
}

mattercodex_require_protected_shell_startup
builtin unset BASH_ENV ENV

mattercodex_initial_git() {
  /usr/bin/env -i \
    PATH=/usr/bin:/bin \
    HOME=/nonexistent \
    LC_ALL=C \
    GIT_CONFIG_NOSYSTEM=1 \
    GIT_CONFIG_GLOBAL=/dev/null \
    /usr/bin/git \
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

  expected_object="$(mattercodex_initial_git rev-parse --verify "HEAD:$relative_path")" || {
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
  local topology committed_root actual_root

  case "$script_path" in
    /*) ;;
    */*) script_path="$(builtin pwd -P)/$script_path" ;;
    *)
      builtin printf 'FAIL: невозможно определить абсолютный путь install-bot-service.sh без PATH lookup\n' >&2
      exit 1
      ;;
  esac
  if [[ ! -f "$script_path" || -L "$script_path" ]]; then
    builtin printf 'FAIL: защищённая точка входа install-bot-service.sh должна быть обычным файлом, а не symlink\n' >&2
    exit 1
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
    builtin printf 'FAIL: невозможно удержать descriptor install-bot-service.sh\n' >&2
    exit 1
  }
  topology="$(LC_ALL=C /usr/bin/stat -Lc '%h:%F' "/proc/$$/fd/$MATTERCODEX_ENTRYPOINT_FD")" || {
    builtin printf 'FAIL: невозможно проверить topology install-bot-service.sh\n' >&2
    exit 1
  }
  if [[ "$topology" != "1:regular file" ]]; then
    builtin printf 'FAIL: protected entrypoint install-bot-service.sh должен быть regular file с link count 1\n' >&2
    exit 1
  fi
  script_dir="${script_path%/*}"
  SCRIPT_DIR="$(builtin cd -P -- "$script_dir" && builtin pwd -P)" || {
    builtin printf 'FAIL: невозможно определить trusted script directory install-bot-service.sh\n' >&2
    exit 1
  }
  canonical_script_path="$SCRIPT_DIR/install-bot-service.sh"
  if [[ ! -f "$canonical_script_path" || -L "$canonical_script_path" || ! "$script_path" -ef "$canonical_script_path" ]]; then
    builtin printf 'FAIL: install-bot-service.sh не совпадает с canonical protected entrypoint текущего checkout\n' >&2
    exit 1
  fi
  REPO_ROOT="$(builtin cd -P -- "$SCRIPT_DIR/../.." && builtin pwd -P)" || {
    builtin printf 'FAIL: невозможно определить trusted repository root install-bot-service.sh\n' >&2
    exit 1
  }
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
    builtin printf 'FAIL: install-bot-service.sh не принадлежит trusted Git checkout root\n' >&2
    exit 1
  fi
  mattercodex_initial_git rev-parse --verify 'HEAD^{commit}' >/dev/null || {
    builtin printf 'FAIL: trusted Git checkout не содержит HEAD commit\n' >&2
    exit 1
  }
  mattercodex_initial_require_committed_fd \
    scripts/k8s/install-bot-service.sh \
    "$MATTERCODEX_ENTRYPOINT_FD" \
    "protected entrypoint install-bot-service.sh"

  bootstrap_path="$REPO_ROOT/scripts/lib/bootstrap.sh"
  if [[ ! -f "$bootstrap_path" || -L "$bootstrap_path" ]]; then
    builtin printf 'FAIL: trusted bootstrap helper должен быть обычным файлом без symlink\n' >&2
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
        ! "$bootstrap_path" -ef "/proc/$$/fd/$MATTERCODEX_BOOTSTRAP_HELPER_FD" ]]; then
    builtin printf 'FAIL: trusted bootstrap pathname изменён до descriptor-bound source\n' >&2
    exit 1
  fi
}

mattercodex_resolve_bootstrap_paths
# shellcheck disable=SC1091
. "/proc/$$/fd/$MATTERCODEX_BOOTSTRAP_HELPER_FD"
mattercodex_establish_bootstrap scripts/k8s/install-bot-service.sh true true

ENV_FILE="$REPO_ROOT/.env"
DRY_RUN_MODE="server"
WAIT=false
RENDER_DIR=""
RENDER_DIR_CREATED=false

cleanup() {
  if mattercodex_bool "$RENDER_DIR_CREATED"; then
    rm -rf "$RENDER_DIR"
  fi
}

trap cleanup EXIT

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
    --wait)
      WAIT=true
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
    --render-dir)
      RENDER_DIR="$2"
      shift 2
      ;;
    *)
      mattercodex_die "неизвестный аргумент: $1"
      ;;
  esac
done

mattercodex_load_env_file "$ENV_FILE"
mattercodex_require_protected_shell_startup
mattercodex_validate_base_env
mattercodex_require_commands kubectl envsubst base64

if [ -z "$RENDER_DIR" ]; then
  RENDER_DIR="$(mktemp -d)"
  RENDER_DIR_CREATED=true
fi

mattercodex_run_render_helper --env-file "$ENV_FILE" --render-dir "$RENDER_DIR" >/dev/null

DRY_RUN_ARG="$(mattercodex_kubectl_dry_run_arg "$DRY_RUN_MODE")"

apply_rendered_manifest() {
  local template="$1"
  local output="$2"
  mattercodex_render_template "$template" "$output"
  kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$output" >/dev/null
}

kubernetes_object_revision() {
  local kind="$1"
  local name="$2"
  local identity
  identity="$(kubectl -n "$MATTERCODEX_NAMESPACE" get "$kind" "$name" -o jsonpath='{.metadata.uid}:{.metadata.resourceVersion}' 2>/dev/null || true)"
  if [ -z "$identity" ]; then
    identity="missing"
  fi
  printf '%s/%s:%s\n' "$kind" "$name" "$identity"
}

render_deployment_with_live_pod_inputs() {
  MATTERCODEX_BOT_SERVICE_POD_INPUT_REVISION="$(mattercodex_pod_input_revision \
    "$(kubernetes_object_revision configmap "$MATTERCODEX_BOT_SERVICE_CONFIG_CONFIGMAP")" \
    "$(kubernetes_object_revision secret "$MATTERCODEX_BOT_SERVICE_SECRET")" \
    "$(kubernetes_object_revision secret "$MATTERCODEX_POSTGRES_SECRET")" \
    "$(kubernetes_object_revision secret "$MATTERCODEX_GITHUB_SECRET")")"
  export MATTERCODEX_BOT_SERVICE_POD_INPUT_REVISION
  mattercodex_render_template \
    "$REPO_ROOT/deploy/k8s/bot-service/deployment.yaml.tpl" \
    "$RENDER_DIR/30-deployment.yaml"
}

if [ "$DRY_RUN_MODE" = "none" ] && mattercodex_bool "${MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE:-true}"; then
  mattercodex_require_commands docker
  mattercodex_log "сборка agent-runner image"
  mattercodex_run_build_wrapper \
    --builder docker \
    --context "$REPO_ROOT" \
    --dockerfile "$REPO_ROOT/services/jobs/agent-runner/Dockerfile" \
    --tag "$MATTERCODEX_AGENT_RUNNER_IMAGE" \
    --network host \
    --build-arg "MATTERCODEX_CODEX_PACKAGE=$MATTERCODEX_CODEX_PACKAGE" \
    --frontend-attrs-json '{}'
fi

if [ "$DRY_RUN_MODE" = "none" ] && mattercodex_bool "${MATTERCODEX_BOT_SERVICE_BUILD_IMAGE:-true}"; then
  mattercodex_require_commands docker
  mattercodex_log "сборка bot-service image"
  docker build \
    --network=host \
    --target prod \
    -f "$REPO_ROOT/services/external/bot-service/Dockerfile" \
    -t "$MATTERCODEX_BOT_SERVICE_IMAGE" \
    "$REPO_ROOT"
fi

if [ -n "${MATTERCODEX_MATTERMOST_BOT_TOKEN:-}" ] || [ -n "${MATTERCODEX_MATTERMOST_SLASH_TOKEN:-}" ] || [ -n "${MATTERCODEX_CONTROL_CENTER_READ_TOKEN:-}" ]; then
  export BOT_TOKEN_B64
  export SLASH_TOKEN_B64
  export ADMIN_TOKEN_B64
  export CONTROL_CENTER_READ_TOKEN_B64
  BOT_TOKEN_B64="$(printf '%s' "${MATTERCODEX_MATTERMOST_BOT_TOKEN:-}" | base64 | tr -d '\n')"
  SLASH_TOKEN_B64="$(printf '%s' "${MATTERCODEX_MATTERMOST_SLASH_TOKEN:-}" | base64 | tr -d '\n')"
  ADMIN_TOKEN_B64="$(printf '%s' "${MATTERCODEX_MATTERMOST_ADMIN_TOKEN:-}" | base64 | tr -d '\n')"
  CONTROL_CENTER_READ_TOKEN_B64="$(printf '%s' "${MATTERCODEX_CONTROL_CENTER_READ_TOKEN:-}" | base64 | tr -d '\n')"
  mattercodex_log "применяется bot-service secret"
  apply_rendered_manifest "$REPO_ROOT/deploy/k8s/bot-service/bot-service-secret.yaml.tpl" "$RENDER_DIR/05-bot-service-secret.yaml"
else
  mattercodex_log "Mattermost bot/slash и Control Center read token не заданы; bot-service secret не создается"
fi

GITHUB_TOKEN_VALUE="${MATTERCODEX_GITHUB_TOKEN:-${GITHUB_PAT:-${GIT_BOT_TOKEN:-}}}"
GITHUB_WEBHOOK_SECRET_VALUE="${MATTERCODEX_GITHUB_WEBHOOK_SECRET:-${GITHUB_WEBHOOK_SECRET:-}}"
GITHUB_USERNAME_VALUE="${MATTERCODEX_GITHUB_USERNAME:-${GITHUB_USERNAME:-${GITHUB_USER:-}}}"
GITHUB_EMAIL_VALUE="${MATTERCODEX_GITHUB_EMAIL:-${GITHUB_EMAIL:-}}"
if [ -z "$GITHUB_EMAIL_VALUE" ] && [ -n "$GITHUB_USERNAME_VALUE" ]; then
  GITHUB_EMAIL_VALUE="${GITHUB_USERNAME_VALUE}@users.noreply.github.com"
fi
if [ -n "$GITHUB_TOKEN_VALUE" ] || [ -n "$GITHUB_WEBHOOK_SECRET_VALUE" ]; then
  if [ -n "$GITHUB_TOKEN_VALUE" ]; then
    [ -n "$GITHUB_USERNAME_VALUE" ] || mattercodex_die "GitHub username не задан: укажи MATTERCODEX_GITHUB_USERNAME или GITHUB_USERNAME/GITHUB_USER"
    [ -n "$GITHUB_EMAIL_VALUE" ] || mattercodex_die "GitHub email не задан: укажи MATTERCODEX_GITHUB_EMAIL или GITHUB_EMAIL"
  fi
  export GITHUB_TOKEN_B64
  export GITHUB_WEBHOOK_SECRET_B64
  export GITHUB_USERNAME_B64
  export GITHUB_EMAIL_B64
  GITHUB_TOKEN_B64="$(printf '%s' "$GITHUB_TOKEN_VALUE" | base64 | tr -d '\n')"
  GITHUB_WEBHOOK_SECRET_B64="$(printf '%s' "$GITHUB_WEBHOOK_SECRET_VALUE" | base64 | tr -d '\n')"
  GITHUB_USERNAME_B64="$(printf '%s' "$GITHUB_USERNAME_VALUE" | base64 | tr -d '\n')"
  GITHUB_EMAIL_B64="$(printf '%s' "$GITHUB_EMAIL_VALUE" | base64 | tr -d '\n')"
  mattercodex_log "применяется GitHub secret"
  apply_rendered_manifest "$REPO_ROOT/deploy/k8s/bot-service/github-secret.yaml.tpl" "$RENDER_DIR/06-github-secret.yaml"
else
  mattercodex_log "GitHub token/webhook secret не заданы; GitHub secret не создается"
fi

AGENT_GITHUB_TOKEN_VALUE="${MATTERCODEX_AGENT_GITHUB_TOKEN:-${GIT_BOT_TOKEN:-}}"
AGENT_GITHUB_USERNAME_VALUE="${MATTERCODEX_AGENT_GITHUB_USERNAME:-${GIT_BOT_USERNAME:-}}"
AGENT_GITHUB_EMAIL_VALUE="${MATTERCODEX_AGENT_GITHUB_EMAIL:-${GIT_BOT_MAIL:-${GIT_BOT_EMAIL:-}}}"
if [ -z "$AGENT_GITHUB_EMAIL_VALUE" ] && [ -n "$AGENT_GITHUB_USERNAME_VALUE" ]; then
  AGENT_GITHUB_EMAIL_VALUE="${AGENT_GITHUB_USERNAME_VALUE}@users.noreply.github.com"
fi
if [ -n "$AGENT_GITHUB_TOKEN_VALUE" ]; then
  [ -n "$AGENT_GITHUB_USERNAME_VALUE" ] || mattercodex_die "agent GitHub username не задан: укажи MATTERCODEX_AGENT_GITHUB_USERNAME или GIT_BOT_USERNAME"
  [ -n "$AGENT_GITHUB_EMAIL_VALUE" ] || mattercodex_die "agent GitHub email не задан: укажи MATTERCODEX_AGENT_GITHUB_EMAIL или GIT_BOT_MAIL/GIT_BOT_EMAIL"
  export AGENT_GITHUB_TOKEN_B64
  export AGENT_GITHUB_USERNAME_B64
  export AGENT_GITHUB_EMAIL_B64
  AGENT_GITHUB_TOKEN_B64="$(printf '%s' "$AGENT_GITHUB_TOKEN_VALUE" | base64 | tr -d '\n')"
  AGENT_GITHUB_USERNAME_B64="$(printf '%s' "$AGENT_GITHUB_USERNAME_VALUE" | base64 | tr -d '\n')"
  AGENT_GITHUB_EMAIL_B64="$(printf '%s' "$AGENT_GITHUB_EMAIL_VALUE" | base64 | tr -d '\n')"
  mattercodex_log "применяется agent GitHub secret"
  apply_rendered_manifest "$REPO_ROOT/deploy/k8s/bot-service/agent-github-secret.yaml.tpl" "$RENDER_DIR/07-agent-github-secret.yaml"
else
  mattercodex_log "agent GitHub token не задан; agent GitHub secret не создается"
fi

CODEX_AUTH_JSON_PATH="${MATTERCODEX_CODEX_AUTH_JSON_PATH:-${CODEX_AUTH_JSON_PATH:-}}"
if [ -n "$CODEX_AUTH_JSON_PATH" ]; then
  [ -f "$CODEX_AUTH_JSON_PATH" ] || mattercodex_die "Codex auth.json не найден: $CODEX_AUTH_JSON_PATH"
  CODEX_AUTH_ACCOUNT="${MATTERCODEX_CODEX_AUTH_ACCOUNT:-primary}"
  CODEX_AUTH_SECRET_NAME="${MATTERCODEX_CODEX_AUTH_SECRET}-${CODEX_AUTH_ACCOUNT}"
  export CODEX_AUTH_ACCOUNT
  export CODEX_AUTH_SECRET_NAME
  export CODEX_AUTH_JSON_B64
  CODEX_AUTH_JSON_B64="$(base64 "$CODEX_AUTH_JSON_PATH" | tr -d '\n')"
  mattercodex_log "применяется Codex auth secret"
  apply_rendered_manifest "$REPO_ROOT/deploy/k8s/bot-service/codex-auth-secret.yaml.tpl" "$RENDER_DIR/08-codex-auth-secret.yaml"
else
  mattercodex_log "Codex auth.json path не задан; Codex auth secret не создается"
fi

mattercodex_log "применяются манифесты bot-service"
for manifest in \
  "$RENDER_DIR/10-configmap.yaml" \
  "$RENDER_DIR/15-runtime-limits.yaml" \
  "$RENDER_DIR/20-rbac.yaml"; do
  if [ -f "$manifest" ]; then
    kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$manifest" >/dev/null
  fi
done

if [ "$DRY_RUN_MODE" = "none" ]; then
  render_deployment_with_live_pod_inputs
fi

for manifest in \
  "$RENDER_DIR/30-deployment.yaml" \
  "$RENDER_DIR/40-service.yaml" \
  "$RENDER_DIR/50-ingress.yaml"; do
  if [ -f "$manifest" ]; then
    kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$manifest" >/dev/null
  fi
done

if [ "$DRY_RUN_MODE" = "none" ]; then
  LEGACY_CODE_CONFIGMAP="${MATTERCODEX_BOT_SERVICE_CODE_CONFIGMAP:-matter-codex-bot-service-code}"
  mattercodex_log "удаляется legacy bot-service source ConfigMap, если он остался"
  kubectl -n "$MATTERCODEX_NAMESPACE" delete configmap "$LEGACY_CODE_CONFIGMAP" --ignore-not-found >/dev/null
  if mattercodex_bool "$WAIT"; then
    mattercodex_log "ожидание rollout bot-service"
    kubectl -n "$MATTERCODEX_NAMESPACE" rollout status deployment/matter-codex-bot-service --timeout=300s >/dev/null
  fi
fi

mattercodex_log "bot-service install шаг завершен"
