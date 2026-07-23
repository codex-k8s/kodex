mattercodex_bootstrap_fail() {
  builtin printf 'FAIL: %s\n' "$*" >&2
  return 1
}

mattercodex_bootstrap_git() {
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

mattercodex_bootstrap_fd_path() {
  builtin printf '/proc/%s/fd/%s\n' "$$" "$1"
}

mattercodex_bootstrap_require_committed_fd() {
  local relative_path="$1"
  local fd="$2"
  local label="$3"
  local expected_object actual_object object_type
  local fd_path

  fd_path="$(mattercodex_bootstrap_fd_path "$fd")"
  [[ -n "${MATTERCODEX_TRUSTED_HEAD:-}" ]] ||
    mattercodex_bootstrap_fail "exact trusted Git HEAD не закреплён для bootstrap commitment"
  expected_object="$(mattercodex_bootstrap_git rev-parse --verify "$MATTERCODEX_TRUSTED_HEAD:$relative_path")" ||
    mattercodex_bootstrap_fail "$label отсутствует в HEAD trusted checkout"
  object_type="$(mattercodex_bootstrap_git cat-file -t "$expected_object")" ||
    mattercodex_bootstrap_fail "не удалось определить Git object type для $label"
  [[ "$object_type" == blob ]] ||
    mattercodex_bootstrap_fail "$label в HEAD trusted checkout не является blob"
  actual_object="$(mattercodex_bootstrap_git hash-object --no-filters "$fd_path")" ||
    mattercodex_bootstrap_fail "не удалось вычислить content commitment для $label"
  [[ "$actual_object" == "$expected_object" ]] ||
    mattercodex_bootstrap_fail "$label не совпадает с content commitment HEAD trusted checkout"
}

mattercodex_bootstrap_open_directory() {
  local path="$1"
  local label="$2"
  local output_name="$3"
  local opened_fd fd_path file_type

  [[ -d "$path" && ! -L "$path" ]] ||
    mattercodex_bootstrap_fail "$label должен быть физическим каталогом без symlink"
  exec {opened_fd}<"$path" ||
    mattercodex_bootstrap_fail "не удалось удержать descriptor для $label"
  fd_path="$(mattercodex_bootstrap_fd_path "$opened_fd")"
  [[ -d "$fd_path" && "$path" -ef "$fd_path" ]] ||
    mattercodex_bootstrap_fail "$label изменён во время открытия descriptor"
  file_type="$(LC_ALL=C /usr/bin/stat -Lc '%F' "$fd_path")" ||
    mattercodex_bootstrap_fail "не удалось проверить descriptor для $label"
  [[ "$file_type" == directory ]] ||
    mattercodex_bootstrap_fail "$label descriptor не является каталогом"
  builtin printf -v "$output_name" '%s' "$opened_fd"
}

mattercodex_bootstrap_open_committed_file() {
  local path="$1"
  local relative_path="$2"
  local label="$3"
  local output_name="$4"
  local opened_fd fd_path topology

  [[ -f "$path" && ! -L "$path" ]] ||
    mattercodex_bootstrap_fail "$label должен быть обычным файлом без symlink"
  exec {opened_fd}<"$path" ||
    mattercodex_bootstrap_fail "не удалось удержать descriptor для $label"
  fd_path="$(mattercodex_bootstrap_fd_path "$opened_fd")"
  [[ -f "$fd_path" && "$path" -ef "$fd_path" ]] ||
    mattercodex_bootstrap_fail "$label изменён во время открытия descriptor"
  topology="$(LC_ALL=C /usr/bin/stat -Lc '%h:%F' "$fd_path")" ||
    mattercodex_bootstrap_fail "не удалось проверить topology descriptor для $label"
  [[ "$topology" == "1:regular file" ]] ||
    mattercodex_bootstrap_fail "$label должен быть обычным файлом с link count 1"
  mattercodex_bootstrap_require_committed_fd "$relative_path" "$opened_fd" "$label"
  builtin printf -v "$output_name" '%s' "$opened_fd"
}

mattercodex_bootstrap_require_stable_directory() {
  local path="$1"
  local fd="$2"
  local label="$3"
  local fd_path

  fd_path="$(mattercodex_bootstrap_fd_path "$fd")"
  [[ -d "$path" && ! -L "$path" && "$path" -ef "$fd_path" ]] ||
    mattercodex_bootstrap_fail "trusted bootstrap topology изменена после validation: $label"
}

mattercodex_bootstrap_require_stable_file() {
  local path="$1"
  local fd="$2"
  local label="$3"
  local fd_path

  fd_path="$(mattercodex_bootstrap_fd_path "$fd")"
  [[ -f "$path" && ! -L "$path" && "$path" -ef "$fd_path" ]] ||
    mattercodex_bootstrap_fail "trusted bootstrap topology изменена после validation: $label"
}

mattercodex_bootstrap_test_sync() {
  local ready_fd="${MATTERCODEX_BOOTSTRAP_TEST_READY_FD:-}"
  local continue_fd="${MATTERCODEX_BOOTSTRAP_TEST_CONTINUE_FD:-}"
  local acknowledgement

  if [[ -z "$ready_fd" && -z "$continue_fd" ]]; then
    return
  fi
  [[ "$ready_fd" =~ ^[0-9]+$ && "$continue_fd" =~ ^[0-9]+$ ]] ||
    mattercodex_bootstrap_fail "bootstrap race test descriptors должны задаваться парой числовых fd"
  builtin printf 'validated\n' >&"$ready_fd" ||
    mattercodex_bootstrap_fail "не удалось отправить bootstrap race validation signal"
  IFS= builtin read -r -u "$continue_fd" acknowledgement ||
    mattercodex_bootstrap_fail "не удалось получить bootstrap race continue signal"
  [[ "$acknowledgement" == continue ]] ||
    mattercodex_bootstrap_fail "bootstrap race continue signal имеет недопустимое значение"
}

mattercodex_establish_bootstrap() {
  local entrypoint_relative_path="$1"
  local require_render_helper="$2"
  local require_build_wrapper="$3"
  local committed_root current_head current_root entrypoint_dir_relative entrypoint_file
  local root_fd_path scripts_fd_path lib_fd_path k8s_fd_path remote_fd_path
  local entrypoint_dir_fd canonical_entrypoint_path canonical_bootstrap_path
  local env_helper_path render_helper_path build_wrapper_path

  [[ "$MATTERCODEX_PHYSICAL_REPO_ROOT" == /* ]] ||
    mattercodex_bootstrap_fail "trusted repository root должен быть абсолютным физическим путём"
  current_root="$(builtin cd -P -- "$MATTERCODEX_PHYSICAL_REPO_ROOT" && builtin pwd -P)" ||
    mattercodex_bootstrap_fail "не удалось повторно определить physical trusted repository root"
  [[ "$current_root" == "$MATTERCODEX_PHYSICAL_REPO_ROOT" ]] ||
    mattercodex_bootstrap_fail "trusted repository root изменён до topology binding"
  [[ -d "$MATTERCODEX_PHYSICAL_REPO_ROOT/.git" && ! -L "$MATTERCODEX_PHYSICAL_REPO_ROOT/.git" ]] ||
    mattercodex_bootstrap_fail "trusted repository root должен содержать физический .git directory"
  committed_root="$(mattercodex_bootstrap_git rev-parse --show-toplevel)" ||
    mattercodex_bootstrap_fail "trusted repository root не является поддерживаемым Git checkout"
  committed_root="$(builtin cd -P -- "$committed_root" && builtin pwd -P)" ||
    mattercodex_bootstrap_fail "не удалось определить physical Git checkout root"
  [[ "$committed_root" == "$MATTERCODEX_PHYSICAL_REPO_ROOT" ]] ||
    mattercodex_bootstrap_fail "entrypoint не принадлежит trusted Git checkout root"
  current_head="$(mattercodex_bootstrap_git rev-parse --verify 'HEAD^{commit}')" ||
    mattercodex_bootstrap_fail "trusted Git checkout не содержит HEAD commit"
  [[ -n "${MATTERCODEX_TRUSTED_HEAD:-}" && "$current_head" == "$MATTERCODEX_TRUSTED_HEAD" ]] ||
    mattercodex_bootstrap_fail "trusted Git HEAD изменён до bootstrap commitment"

  mattercodex_bootstrap_open_directory \
    "$MATTERCODEX_PHYSICAL_REPO_ROOT" \
    "trusted repository root" \
    MATTERCODEX_REPO_ROOT_FD
  root_fd_path="$(mattercodex_bootstrap_fd_path "$MATTERCODEX_REPO_ROOT_FD")"
  mattercodex_bootstrap_open_directory \
    "$root_fd_path/.git" \
    "trusted .git directory" \
    MATTERCODEX_GIT_DIR_FD
  mattercodex_bootstrap_open_directory \
    "$root_fd_path/scripts" \
    "trusted scripts directory" \
    MATTERCODEX_SCRIPTS_DIR_FD
  scripts_fd_path="$(mattercodex_bootstrap_fd_path "$MATTERCODEX_SCRIPTS_DIR_FD")"
  mattercodex_bootstrap_open_directory \
    "$scripts_fd_path/lib" \
    "trusted scripts/lib directory" \
    MATTERCODEX_LIB_DIR_FD
  mattercodex_bootstrap_open_directory \
    "$scripts_fd_path/k8s" \
    "trusted scripts/k8s directory" \
    MATTERCODEX_K8S_DIR_FD
  mattercodex_bootstrap_open_directory \
    "$scripts_fd_path/remote" \
    "trusted scripts/remote directory" \
    MATTERCODEX_REMOTE_DIR_FD

  entrypoint_dir_relative="${entrypoint_relative_path%/*}"
  entrypoint_file="${entrypoint_relative_path##*/}"
  case "$entrypoint_dir_relative" in
    scripts/k8s) entrypoint_dir_fd="$MATTERCODEX_K8S_DIR_FD" ;;
    scripts/remote) entrypoint_dir_fd="$MATTERCODEX_REMOTE_DIR_FD" ;;
    *) mattercodex_bootstrap_fail "неподдерживаемый protected entrypoint $entrypoint_relative_path" ;;
  esac
  canonical_entrypoint_path="$(mattercodex_bootstrap_fd_path "$entrypoint_dir_fd")/$entrypoint_file"
  mattercodex_bootstrap_require_stable_file \
    "$canonical_entrypoint_path" \
    "$MATTERCODEX_ENTRYPOINT_FD" \
    "protected entrypoint"
  mattercodex_bootstrap_require_committed_fd \
    "$entrypoint_relative_path" \
    "$MATTERCODEX_ENTRYPOINT_FD" \
    "protected entrypoint"

  lib_fd_path="$(mattercodex_bootstrap_fd_path "$MATTERCODEX_LIB_DIR_FD")"
  canonical_bootstrap_path="$lib_fd_path/bootstrap.sh"
  mattercodex_bootstrap_require_stable_file \
    "$canonical_bootstrap_path" \
    "$MATTERCODEX_BOOTSTRAP_HELPER_FD" \
    "trusted bootstrap helper"
  mattercodex_bootstrap_require_committed_fd \
    "scripts/lib/bootstrap.sh" \
    "$MATTERCODEX_BOOTSTRAP_HELPER_FD" \
    "trusted bootstrap helper"

  env_helper_path="$lib_fd_path/env.sh"
  mattercodex_bootstrap_open_committed_file \
    "$env_helper_path" \
    "scripts/lib/env.sh" \
    "trusted env helper" \
    ENV_HELPER_FD
  ENV_HELPER_PATH="$(mattercodex_bootstrap_fd_path "$ENV_HELPER_FD")"

  if [[ "$require_render_helper" == true ]]; then
    k8s_fd_path="$(mattercodex_bootstrap_fd_path "$MATTERCODEX_K8S_DIR_FD")"
    render_helper_path="$k8s_fd_path/render-bot-service.sh"
    mattercodex_bootstrap_open_committed_file \
      "$render_helper_path" \
      "scripts/k8s/render-bot-service.sh" \
      "trusted render helper" \
      RENDER_HELPER_FD
  fi

  if [[ "$require_build_wrapper" == true ]]; then
    build_wrapper_path="$scripts_fd_path/build-agent-runner-image.sh"
    mattercodex_bootstrap_open_committed_file \
      "$build_wrapper_path" \
      "scripts/build-agent-runner-image.sh" \
      "trusted agent-runner build wrapper" \
      BUILD_WRAPPER_FD
  fi

  mattercodex_bootstrap_test_sync

  mattercodex_bootstrap_require_stable_directory \
    "$MATTERCODEX_PHYSICAL_REPO_ROOT" \
    "$MATTERCODEX_REPO_ROOT_FD" \
    "repository root"
  mattercodex_bootstrap_require_stable_directory \
    "$MATTERCODEX_PHYSICAL_REPO_ROOT/.git" \
    "$MATTERCODEX_GIT_DIR_FD" \
    ".git"
  mattercodex_bootstrap_require_stable_directory \
    "$MATTERCODEX_PHYSICAL_REPO_ROOT/scripts" \
    "$MATTERCODEX_SCRIPTS_DIR_FD" \
    "scripts"
  mattercodex_bootstrap_require_stable_directory \
    "$MATTERCODEX_PHYSICAL_REPO_ROOT/scripts/lib" \
    "$MATTERCODEX_LIB_DIR_FD" \
    "scripts/lib"
  mattercodex_bootstrap_require_stable_directory \
    "$MATTERCODEX_PHYSICAL_REPO_ROOT/scripts/k8s" \
    "$MATTERCODEX_K8S_DIR_FD" \
    "scripts/k8s"
  mattercodex_bootstrap_require_stable_directory \
    "$MATTERCODEX_PHYSICAL_REPO_ROOT/scripts/remote" \
    "$MATTERCODEX_REMOTE_DIR_FD" \
    "scripts/remote"
  mattercodex_bootstrap_require_stable_file \
    "$MATTERCODEX_PHYSICAL_REPO_ROOT/$entrypoint_relative_path" \
    "$MATTERCODEX_ENTRYPOINT_FD" \
    "$entrypoint_relative_path"
  mattercodex_bootstrap_require_stable_file \
    "$MATTERCODEX_PHYSICAL_REPO_ROOT/scripts/lib/bootstrap.sh" \
    "$MATTERCODEX_BOOTSTRAP_HELPER_FD" \
    "scripts/lib/bootstrap.sh"
  mattercodex_bootstrap_require_stable_file \
    "$MATTERCODEX_PHYSICAL_REPO_ROOT/scripts/lib/env.sh" \
    "$ENV_HELPER_FD" \
    "scripts/lib/env.sh"
  if [[ "$require_render_helper" == true ]]; then
    mattercodex_bootstrap_require_stable_file \
      "$MATTERCODEX_PHYSICAL_REPO_ROOT/scripts/k8s/render-bot-service.sh" \
      "$RENDER_HELPER_FD" \
      "scripts/k8s/render-bot-service.sh"
  fi
  if [[ "$require_build_wrapper" == true ]]; then
    mattercodex_bootstrap_require_stable_file \
      "$MATTERCODEX_PHYSICAL_REPO_ROOT/scripts/build-agent-runner-image.sh" \
      "$BUILD_WRAPPER_FD" \
      "scripts/build-agent-runner-image.sh"
  fi

  REPO_ROOT="$MATTERCODEX_PHYSICAL_REPO_ROOT"
  case "$entrypoint_dir_relative" in
    scripts/k8s) SCRIPT_DIR="$REPO_ROOT/scripts/k8s" ;;
    scripts/remote) SCRIPT_DIR="$REPO_ROOT/scripts/remote" ;;
  esac
  # shellcheck disable=SC1090
  . "$ENV_HELPER_PATH"
}

mattercodex_run_render_helper() {
  [[ -n "${RENDER_HELPER_FD:-}" ]] ||
    mattercodex_bootstrap_fail "trusted render helper descriptor не установлен"
  MATTERCODEX_BOOTSTRAP_HANDOFF_REPO_ROOT="$MATTERCODEX_PHYSICAL_REPO_ROOT" \
  MATTERCODEX_BOOTSTRAP_HANDOFF_HELPER_PATH="$(mattercodex_bootstrap_fd_path "$MATTERCODEX_BOOTSTRAP_HELPER_FD")" \
  MATTERCODEX_BOOTSTRAP_HANDOFF_TRUSTED_HEAD="$MATTERCODEX_TRUSTED_HEAD" \
    /bin/bash -p \
      "$(mattercodex_bootstrap_fd_path "$RENDER_HELPER_FD")" \
      --mattercodex-bootstrap-handoff \
      "$@"
}

mattercodex_run_build_wrapper() {
  [[ -n "${BUILD_WRAPPER_FD:-}" ]] ||
    mattercodex_bootstrap_fail "trusted build wrapper descriptor не установлен"
  /bin/bash -p "$(mattercodex_bootstrap_fd_path "$BUILD_WRAPPER_FD")" "$@"
}
