#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck disable=SC1091
. "$REPO_ROOT/scripts/lib/env.sh"

ENV_FILE="$REPO_ROOT/.env"
DRY_RUN_MODE="server"
WAIT=false
RENDER_DIR=""
TEMP_FILES=()
TEMP_DIRS=()

cleanup() {
  local path
  for path in "${TEMP_FILES[@]}"; do
    rm -f "$path"
  done
  for path in "${TEMP_DIRS[@]}"; do
    rm -rf "$path"
  done
}

mattercodex_temp_file() {
  local path
  path="$(mktemp)"
  TEMP_FILES+=("$path")
  printf '%s\n' "$path"
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
MATTERCODEX_IMAGE_TAG_WAS_SET=false
MATTERCODEX_BOT_SERVICE_IMAGE_WAS_SET=false
MATTERCODEX_AGENT_RUNNER_IMAGE_WAS_SET=false
[ -n "${MATTERCODEX_IMAGE_TAG+x}" ] && MATTERCODEX_IMAGE_TAG_WAS_SET=true
[ -n "${MATTERCODEX_BOT_SERVICE_IMAGE+x}" ] && MATTERCODEX_BOT_SERVICE_IMAGE_WAS_SET=true
[ -n "${MATTERCODEX_AGENT_RUNNER_IMAGE+x}" ] && MATTERCODEX_AGENT_RUNNER_IMAGE_WAS_SET=true
mattercodex_validate_base_env
mattercodex_require_commands ssh envsubst base64 tar git

configure_image_defaults_for_remote_build() {
  if [ "$MATTERCODEX_IMAGE_BUILD_STRATEGY" != "kaniko" ]; then
    return
  fi
  if [ "$DRY_RUN_MODE" = "none" ] && ! mattercodex_bool "$MATTERCODEX_IMAGE_TAG_WAS_SET"; then
    MATTERCODEX_IMAGE_TAG="$(git -C "$REPO_ROOT" rev-parse --short=12 HEAD)-$(date -u +%Y%m%d%H%M%S)"
    export MATTERCODEX_IMAGE_TAG
  fi
  if ! mattercodex_bool "$MATTERCODEX_BOT_SERVICE_IMAGE_WAS_SET"; then
    MATTERCODEX_BOT_SERVICE_IMAGE="${MATTERCODEX_IMAGE_REGISTRY_PULL_HOST}/${MATTERCODEX_IMAGE_REPOSITORY_PREFIX}/bot-service:${MATTERCODEX_IMAGE_TAG}"
    export MATTERCODEX_BOT_SERVICE_IMAGE
  fi
  if ! mattercodex_bool "$MATTERCODEX_AGENT_RUNNER_IMAGE_WAS_SET"; then
    MATTERCODEX_AGENT_RUNNER_IMAGE="${MATTERCODEX_IMAGE_REGISTRY_PULL_HOST}/${MATTERCODEX_IMAGE_REPOSITORY_PREFIX}/agent-runner:${MATTERCODEX_IMAGE_TAG}"
    export MATTERCODEX_AGENT_RUNNER_IMAGE
  fi
}

configure_image_defaults_for_remote_build

if [ -z "$RENDER_DIR" ]; then
  RENDER_DIR="$(mktemp -d)"
  TEMP_DIRS+=("$RENDER_DIR")
fi

"$REPO_ROOT/scripts/k8s/render-bot-service.sh" --env-file "$ENV_FILE" --render-dir "$RENDER_DIR" >/dev/null

APPLY_DRY_RUN_MODE="$DRY_RUN_MODE"
NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_NAMESPACE")"
RUNTIME_NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_RUNTIME_NAMESPACE")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"

apply_rendered_manifest_remote() {
  local template="$1"
  local output="$2"
  mattercodex_render_template "$template" "$output"
  mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE" < "$output"
}

if [ "$DRY_RUN_MODE" = "server" ] && {
  ! mattercodex_ssh "$REMOTE_KUBECTL get namespace $NAMESPACE_Q >/dev/null 2>&1" ||
  ! mattercodex_ssh "$REMOTE_KUBECTL get namespace $RUNTIME_NAMESPACE_Q >/dev/null 2>&1"
}; then
  mattercodex_log "namespace еще не создан; bot-service manifests проверяются через remote client dry-run"
  APPLY_DRY_RUN_MODE="client"
fi

remote_container_builder() {
  mattercodex_ssh 'set -eu
    if command -v docker >/dev/null 2>&1; then
      printf "docker\n"
    elif command -v nerdctl >/dev/null 2>&1; then
      printf "nerdctl\n"
    else
      printf "none\n"
    fi' </dev/null
}

remote_container_image_exists() {
  local image="$1"
  local image_q
  image_q="$(mattercodex_shell_quote "$image")"
  mattercodex_ssh "set -eu
    image=$image_q
    refs=\$image
    first=\${image%%/*}
    if [ \"\$first\" = \"\$image\" ]; then
      refs=\"\$refs docker.io/library/\$image\"
    elif [ \"\${first#*.}\" = \"\$first\" ] && [ \"\${first#*:}\" = \"\$first\" ] && [ \"\$first\" != \"localhost\" ]; then
      refs=\"\$refs docker.io/\$image\"
    fi
    if command -v sudo >/dev/null 2>&1 && sudo -n k3s ctr images ls -q >/tmp/matter-codex-images 2>/dev/null; then
      :
    elif command -v sudo >/dev/null 2>&1 && sudo -n ctr -n k8s.io images ls -q >/tmp/matter-codex-images 2>/dev/null; then
      :
    else
      exit 1
    fi
    for ref in \$refs; do
      if grep -Fx -- \"\$ref\" /tmp/matter-codex-images >/dev/null; then
        exit 0
      fi
    done
    exit 1" </dev/null
}

image_push_destination_for_pull_image() {
  local image="$1"
  local image_path
  if [ "$image" = "${image#*/}" ]; then
    image_path="${MATTERCODEX_IMAGE_REPOSITORY_PREFIX}/${image}"
  else
    image_path="${image#*/}"
  fi
  printf '%s/%s\n' "$MATTERCODEX_IMAGE_REGISTRY_PUSH_HOST" "$image_path"
}

remote_registry_image_exists() {
  local image="$1"
  local image_path
  local repository
  local tag
  local registry_url_q

  if [ "$image" = "${image#*/}" ]; then
    return 1
  fi
  image_path="${image#*/}"
  repository="${image_path%:*}"
  tag="${image_path##*:}"
  if [ "$repository" = "$image_path" ] || [ -z "$repository" ] || [ -z "$tag" ]; then
    return 1
  fi
  registry_url_q="$(mattercodex_shell_quote "http://${MATTERCODEX_IMAGE_REGISTRY_PULL_HOST}/v2/${repository}/manifests/${tag}")"
  mattercodex_ssh "curl -fsS -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' $registry_url_q >/dev/null" </dev/null
}

kaniko_arg_yaml_line() {
  local arg="$1"
  local escaped
  escaped="$(printf '%s' "$arg" | sed 's/\\/\\\\/g; s/"/\\"/g')"
  printf '            - "%s"\n' "$escaped"
}

apply_kaniko_build_infra_remote() {
  if [ "$DRY_RUN_MODE" != "none" ] || [ "$MATTERCODEX_IMAGE_BUILD_STRATEGY" != "kaniko" ]; then
    return
  fi
  if [ -f "$RENDER_DIR/15-runtime-limits.yaml" ]; then
    mattercodex_log "обновляется runtime quota перед созданием build PVC"
    mattercodex_remote_kubectl_apply_stdin none < "$RENDER_DIR/15-runtime-limits.yaml"
  fi
  if mattercodex_bool "$MATTERCODEX_IMAGE_REGISTRY_MANAGED"; then
    mattercodex_log "применяется MatterCodex registry на целевом сервере"
    mattercodex_remote_kubectl_apply_stdin none < "$RENDER_DIR/02-image-registry.yaml"
    mattercodex_log "ожидание MatterCodex registry"
    mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q rollout status deployment/$(mattercodex_shell_quote "$MATTERCODEX_IMAGE_REGISTRY_NAME") --timeout=180s >/dev/null" </dev/null
  fi
  mattercodex_log "применяется PVC для Kaniko build context"
  mattercodex_remote_kubectl_apply_stdin none < "$RENDER_DIR/03-kaniko-context-pvc.yaml"
}

upload_kaniko_context_remote() {
  local archive="$1"
  local context_subdir="$2"
  local uploader_pod="matter-codex-kaniko-context-upload"
  local uploader_pod_q
  local context_subdir_q
  local manifest

  uploader_pod_q="$(mattercodex_shell_quote "$uploader_pod")"
  context_subdir_q="$(mattercodex_shell_quote "$context_subdir")"
  manifest="$(mattercodex_temp_file)"
  MATTERCODEX_KANIKO_UPLOADER_POD="$uploader_pod"
  export MATTERCODEX_KANIKO_UPLOADER_POD

  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q delete pod $uploader_pod_q --ignore-not-found --wait=true >/dev/null" </dev/null
  mattercodex_render_template "$REPO_ROOT/deploy/k8s/bot-service/kaniko-uploader-pod.yaml.tpl" "$manifest"
  mattercodex_remote_kubectl_apply_stdin none < "$manifest"
  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q wait --for=condition=Ready pod/$uploader_pod_q --timeout=120s >/dev/null" </dev/null

  mattercodex_log "загрузка build context в Kaniko PVC: $context_subdir"
  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q exec -i $uploader_pod_q -- sh -c 'set -eu; subdir=\$1; rm -rf \"/workspace/\$subdir\"; mkdir -p \"/workspace/\$subdir\"; tar -xzf - -C \"/workspace/\$subdir\"' sh $context_subdir_q" < "$archive"
  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q delete pod $uploader_pod_q --ignore-not-found --wait=false >/dev/null" </dev/null
}

wait_kaniko_job_remote() {
  local job_name="$1"
  local job_name_q
  local job_ref_q
  local deadline
  local complete
  local failed

  job_name_q="$(mattercodex_shell_quote "$job_name")"
  job_ref_q="$(mattercodex_shell_quote "job/$job_name")"
  deadline=$(( $(date +%s) + MATTERCODEX_KANIKO_ACTIVE_DEADLINE_SECONDS ))

  while :; do
    complete="$(mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q get job $job_name_q -o jsonpath='{.status.conditions[?(@.type==\"Complete\")].status}' 2>/dev/null || true" </dev/null)"
    if [ "$complete" = "True" ]; then
      return
    fi
    failed="$(mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q get job $job_name_q -o jsonpath='{.status.conditions[?(@.type==\"Failed\")].status}' 2>/dev/null || true" </dev/null)"
    if [ "$failed" = "True" ]; then
      mattercodex_log "Kaniko job завершился ошибкой: $job_name"
      mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q logs $job_ref_q --all-containers=true --tail=200 || true" </dev/null >&2
      mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q describe $job_ref_q || true" </dev/null >&2
      mattercodex_die "Kaniko job завершился ошибкой: $job_name"
    fi
    if [ "$(date +%s)" -ge "$deadline" ]; then
      mattercodex_log "Kaniko job не завершился за timeout: $job_name"
      mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q logs $job_ref_q --all-containers=true --tail=200 || true" </dev/null >&2
      mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q describe $job_ref_q || true" </dev/null >&2
      mattercodex_die "Kaniko job timeout: $job_name"
    fi
    sleep 10
  done
}

run_kaniko_build_remote() {
  local component="$1"
  local archive="$2"
  local dockerfile="$3"
  local pull_image="$4"
  local extra_args_yaml="$5"
  local job_name
  local destination
  local job_manifest
  local job_name_q

  job_name="mc-kaniko-${component}-$(date -u +%Y%m%d%H%M%S)-$$"
  job_name_q="$(mattercodex_shell_quote "$job_name")"
  destination="$(image_push_destination_for_pull_image "$pull_image")"
  job_manifest="$(mattercodex_temp_file)"

  upload_kaniko_context_remote "$archive" "$job_name"

  MATTERCODEX_KANIKO_JOB_NAME="$job_name"
  MATTERCODEX_KANIKO_COMPONENT="$component"
  MATTERCODEX_KANIKO_CONTEXT_SUBDIR="$job_name"
  MATTERCODEX_KANIKO_DOCKERFILE="$dockerfile"
  MATTERCODEX_KANIKO_DESTINATION="$destination"
  MATTERCODEX_KANIKO_CACHE_REPO="${MATTERCODEX_IMAGE_REGISTRY_PUSH_HOST}/${MATTERCODEX_IMAGE_REPOSITORY_PREFIX}/kaniko-cache/${component}"
  MATTERCODEX_KANIKO_EXTRA_ARGS_YAML="$extra_args_yaml"
  export MATTERCODEX_KANIKO_JOB_NAME MATTERCODEX_KANIKO_COMPONENT MATTERCODEX_KANIKO_CONTEXT_SUBDIR MATTERCODEX_KANIKO_DOCKERFILE MATTERCODEX_KANIKO_DESTINATION MATTERCODEX_KANIKO_CACHE_REPO MATTERCODEX_KANIKO_EXTRA_ARGS_YAML

  mattercodex_render_template "$REPO_ROOT/deploy/k8s/bot-service/kaniko-job.yaml.tpl" "$job_manifest"
  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q delete job $job_name_q --ignore-not-found --wait=true >/dev/null" </dev/null
  mattercodex_log "Kaniko сборка $component image"
  mattercodex_remote_kubectl_apply_stdin none < "$job_manifest"
  wait_kaniko_job_remote "$job_name"
  mattercodex_log "Kaniko сборка $component завершена: $pull_image"
}

apply_kaniko_build_infra_remote

SHOULD_BUILD_AGENT_RUNNER=false
if [ "$DRY_RUN_MODE" = "none" ]; then
  if mattercodex_bool "${MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE:-true}"; then
    SHOULD_BUILD_AGENT_RUNNER=true
  elif [ "$MATTERCODEX_IMAGE_BUILD_STRATEGY" = "kaniko" ] && remote_registry_image_exists "$MATTERCODEX_AGENT_RUNNER_IMAGE"; then
    mattercodex_log "agent-runner image уже есть в MatterCodex registry"
  elif remote_container_image_exists "$MATTERCODEX_AGENT_RUNNER_IMAGE"; then
    mattercodex_log "agent-runner image уже есть в Kubernetes runtime"
  else
    mattercodex_log "agent-runner image отсутствует; включается rebuild несмотря на MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE=false"
    SHOULD_BUILD_AGENT_RUNNER=true
  fi
fi

if [ "$DRY_RUN_MODE" = "none" ] && mattercodex_bool "$SHOULD_BUILD_AGENT_RUNNER"; then
  mattercodex_log "сборка agent-runner image на целевом сервере"
  AGENT_RUNNER_ARCHIVE="$(mattercodex_temp_file)"
  tar -C "$REPO_ROOT" -czf "$AGENT_RUNNER_ARCHIVE" \
    go.mod \
    go.sum \
    services/jobs/agent-runner
  if [ "$MATTERCODEX_IMAGE_BUILD_STRATEGY" = "kaniko" ]; then
    run_kaniko_build_remote \
      "agent-runner" \
      "$AGENT_RUNNER_ARCHIVE" \
      "services/jobs/agent-runner/Dockerfile" \
      "$MATTERCODEX_AGENT_RUNNER_IMAGE" \
      "$(kaniko_arg_yaml_line "--build-arg=MATTERCODEX_CODEX_PACKAGE=$MATTERCODEX_CODEX_PACKAGE")"
  elif [ "$MATTERCODEX_IMAGE_BUILD_STRATEGY" = "docker" ]; then
    REMOTE_AGENT_RUNNER_DIR="/tmp/matter-codex-agent-runner-build"
    REMOTE_AGENT_RUNNER_DIR_Q="$(mattercodex_shell_quote "$REMOTE_AGENT_RUNNER_DIR")"
    AGENT_RUNNER_IMAGE_Q="$(mattercodex_shell_quote "$MATTERCODEX_AGENT_RUNNER_IMAGE")"
    CODEX_PACKAGE_Q="$(mattercodex_shell_quote "$MATTERCODEX_CODEX_PACKAGE")"
    REMOTE_AGENT_RUNNER_BUILDER="$(remote_container_builder)"
    if [ "$REMOTE_AGENT_RUNNER_BUILDER" != "docker" ] && [ "$REMOTE_AGENT_RUNNER_BUILDER" != "nerdctl" ]; then
      mattercodex_die "MATTERCODEX_IMAGE_BUILD_STRATEGY=docker требует docker или nerdctl на целевом сервере"
    fi
    mattercodex_ssh "rm -rf $REMOTE_AGENT_RUNNER_DIR_Q && mkdir -p $REMOTE_AGENT_RUNNER_DIR_Q && tar -xzf - -C $REMOTE_AGENT_RUNNER_DIR_Q" < "$AGENT_RUNNER_ARCHIVE"
    mattercodex_ssh "set -eu
      cd $REMOTE_AGENT_RUNNER_DIR_Q
      image=$AGENT_RUNNER_IMAGE_Q
      codex_package=$CODEX_PACKAGE_Q
      if [ '$REMOTE_AGENT_RUNNER_BUILDER' = 'docker' ]; then
        docker build --build-arg MATTERCODEX_CODEX_PACKAGE=\"\$codex_package\" -f services/jobs/agent-runner/Dockerfile -t \"\$image\" .
      else
        nerdctl -n k8s.io build --build-arg MATTERCODEX_CODEX_PACKAGE=\"\$codex_package\" -f services/jobs/agent-runner/Dockerfile -t \"\$image\" .
      fi" </dev/null
  else
    mattercodex_die "неподдерживаемая MATTERCODEX_IMAGE_BUILD_STRATEGY: $MATTERCODEX_IMAGE_BUILD_STRATEGY"
  fi
  if [ "$MATTERCODEX_IMAGE_BUILD_STRATEGY" = "docker" ] && ! remote_container_image_exists "$MATTERCODEX_AGENT_RUNNER_IMAGE"; then
    mattercodex_die "agent-runner image не найден в Kubernetes runtime после build: $MATTERCODEX_AGENT_RUNNER_IMAGE"
  fi
  mattercodex_log "agent-runner image подготовлен"
fi

if [ "$DRY_RUN_MODE" = "none" ] && mattercodex_bool "${MATTERCODEX_BOT_SERVICE_BUILD_IMAGE:-true}"; then
  mattercodex_log "сборка bot-service image на целевом сервере"
  BOT_SERVICE_ARCHIVE="$(mattercodex_temp_file)"
  tar -C "$REPO_ROOT" -czf "$BOT_SERVICE_ARCHIVE" \
    go.mod \
    go.sum \
    libs/go/i18n \
    services/external/bot-service
  if [ "$MATTERCODEX_IMAGE_BUILD_STRATEGY" = "kaniko" ]; then
    run_kaniko_build_remote \
      "bot-service" \
      "$BOT_SERVICE_ARCHIVE" \
      "services/external/bot-service/Dockerfile" \
      "$MATTERCODEX_BOT_SERVICE_IMAGE" \
      "$(kaniko_arg_yaml_line "--target=prod")"
  elif [ "$MATTERCODEX_IMAGE_BUILD_STRATEGY" = "docker" ]; then
    REMOTE_BOT_SERVICE_DIR="/tmp/matter-codex-bot-service-build"
    REMOTE_BOT_SERVICE_DIR_Q="$(mattercodex_shell_quote "$REMOTE_BOT_SERVICE_DIR")"
    BOT_SERVICE_IMAGE_Q="$(mattercodex_shell_quote "$MATTERCODEX_BOT_SERVICE_IMAGE")"
    REMOTE_BOT_SERVICE_BUILDER="$(remote_container_builder)"
    if [ "$REMOTE_BOT_SERVICE_BUILDER" != "docker" ] && [ "$REMOTE_BOT_SERVICE_BUILDER" != "nerdctl" ]; then
      mattercodex_die "MATTERCODEX_IMAGE_BUILD_STRATEGY=docker требует docker или nerdctl на целевом сервере"
    fi
    mattercodex_ssh "rm -rf $REMOTE_BOT_SERVICE_DIR_Q && mkdir -p $REMOTE_BOT_SERVICE_DIR_Q && tar -xzf - -C $REMOTE_BOT_SERVICE_DIR_Q" < "$BOT_SERVICE_ARCHIVE"
    mattercodex_ssh "set -eu
      cd $REMOTE_BOT_SERVICE_DIR_Q
      image=$BOT_SERVICE_IMAGE_Q
      if [ '$REMOTE_BOT_SERVICE_BUILDER' = 'docker' ]; then
        docker build --target prod -f services/external/bot-service/Dockerfile -t \"\$image\" .
      else
        nerdctl -n k8s.io build --target prod -f services/external/bot-service/Dockerfile -t \"\$image\" .
      fi" </dev/null
  else
    mattercodex_die "неподдерживаемая MATTERCODEX_IMAGE_BUILD_STRATEGY: $MATTERCODEX_IMAGE_BUILD_STRATEGY"
  fi
  if [ "$MATTERCODEX_IMAGE_BUILD_STRATEGY" = "docker" ] && ! remote_container_image_exists "$MATTERCODEX_BOT_SERVICE_IMAGE"; then
    mattercodex_die "bot-service image не найден в Kubernetes runtime после build: $MATTERCODEX_BOT_SERVICE_IMAGE"
  fi
  mattercodex_log "bot-service image подготовлен"
fi

if [ -n "${MATTERCODEX_MATTERMOST_BOT_TOKEN:-}" ] || [ -n "${MATTERCODEX_MATTERMOST_SLASH_TOKEN:-}" ]; then
  export BOT_TOKEN_B64
  export SLASH_TOKEN_B64
  BOT_TOKEN_B64="$(printf '%s' "${MATTERCODEX_MATTERMOST_BOT_TOKEN:-}" | base64 | tr -d '\n')"
  SLASH_TOKEN_B64="$(printf '%s' "${MATTERCODEX_MATTERMOST_SLASH_TOKEN:-}" | base64 | tr -d '\n')"
  mattercodex_log "применяется bot-service secret на целевом сервере"
  apply_rendered_manifest_remote "$REPO_ROOT/deploy/k8s/bot-service/bot-service-secret.yaml.tpl" "$RENDER_DIR/05-bot-service-secret.yaml"
else
  mattercodex_log "Mattermost bot/slash token не заданы; bot-service secret не создается"
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
  mattercodex_log "применяется GitHub secret на целевом сервере"
  apply_rendered_manifest_remote "$REPO_ROOT/deploy/k8s/bot-service/github-secret.yaml.tpl" "$RENDER_DIR/06-github-secret.yaml"
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
  mattercodex_log "применяется agent GitHub secret на целевом сервере"
  apply_rendered_manifest_remote "$REPO_ROOT/deploy/k8s/bot-service/agent-github-secret.yaml.tpl" "$RENDER_DIR/07-agent-github-secret.yaml"
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
  mattercodex_log "применяется Codex auth secret на целевом сервере"
  apply_rendered_manifest_remote "$REPO_ROOT/deploy/k8s/bot-service/codex-auth-secret.yaml.tpl" "$RENDER_DIR/08-codex-auth-secret.yaml"
else
  mattercodex_log "Codex auth.json path не задан; Codex auth secret не создается"
fi

mattercodex_log "применяются манифесты bot-service на целевом сервере"
for manifest in \
  "$RENDER_DIR/15-runtime-limits.yaml" \
  "$RENDER_DIR/02-image-registry.yaml" \
  "$RENDER_DIR/03-kaniko-context-pvc.yaml" \
  "$RENDER_DIR/10-configmap.yaml" \
  "$RENDER_DIR/20-rbac.yaml" \
  "$RENDER_DIR/30-deployment.yaml" \
  "$RENDER_DIR/40-service.yaml" \
  "$RENDER_DIR/50-ingress.yaml"; do
  if [ -f "$manifest" ]; then
    manifest_dry_run_mode="$APPLY_DRY_RUN_MODE"
    if [ "$APPLY_DRY_RUN_MODE" = "server" ]; then
      case "$(basename "$manifest")" in
        02-image-registry.yaml|03-kaniko-context-pvc.yaml)
          manifest_dry_run_mode="client"
          ;;
      esac
    fi
    mattercodex_remote_kubectl_apply_stdin "$manifest_dry_run_mode" < "$manifest"
  fi
done

if [ "$DRY_RUN_MODE" = "none" ]; then
  LEGACY_CODE_CONFIGMAP="${MATTERCODEX_BOT_SERVICE_CODE_CONFIGMAP:-matter-codex-bot-service-code}"
  LEGACY_CODE_CONFIGMAP_Q="$(mattercodex_shell_quote "$LEGACY_CODE_CONFIGMAP")"
  mattercodex_log "удаляется legacy bot-service source ConfigMap на целевом сервере, если он остался"
  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q delete configmap $LEGACY_CODE_CONFIGMAP_Q --ignore-not-found >/dev/null"
  mattercodex_log "перезапуск bot-service для применения image/config на целевом сервере"
  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q rollout restart deployment/matter-codex-bot-service >/dev/null"
  if mattercodex_bool "$WAIT"; then
    mattercodex_log "ожидание rollout bot-service на целевом сервере"
    mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q rollout status deployment/matter-codex-bot-service --timeout=300s >/dev/null"
  fi
fi

mattercodex_log "remote bot-service install шаг завершен"
