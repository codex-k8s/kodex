#!/usr/bin/env bash
# Общая CLI-граница локальных image builders. Источник не меняет cwd/options и не читает env-файлы.

# Выводит прежнюю справку с именем вызываемого скрипта; не начинает сборку или импорт.
kodex_local_image_build_usage() {
  printf 'Usage: %s --source-root <path> --state-directory <path> --context <context>\n' "$0" >&2
}

# Разбирает аргументы в три явно названные переменные вызывающего scope через printf -v.
# Имена из кода проверяются до присваивания; значения передаются как литералы с форматом %s.
# --help и неизвестный аргумент сохраняют прежние usage/exit; fail принадлежит вызывающему builder.
kodex_local_image_build_parse() {
  local _kodex_local_image_invalid_outputs='build output variables are invalid'
  (($# >= 3)) || fail "$_kodex_local_image_invalid_outputs"
  local _kodex_local_image_source_output=$1 _kodex_local_image_state_output=$2
  local _kodex_local_image_context_output=$3 _kodex_local_image_output_name
  shift 3
  for _kodex_local_image_output_name in \
    "$_kodex_local_image_source_output" "$_kodex_local_image_state_output" "$_kodex_local_image_context_output"; do
    case "$_kodex_local_image_output_name" in
      ''|[0-9]*|*[!a-zA-Z0-9_]*|_kodex_local_image_*) fail "$_kodex_local_image_invalid_outputs" ;;
    esac
  done
  [[ "$_kodex_local_image_source_output" != "$_kodex_local_image_state_output" &&
    "$_kodex_local_image_source_output" != "$_kodex_local_image_context_output" &&
    "$_kodex_local_image_state_output" != "$_kodex_local_image_context_output" ]] ||
    fail "$_kodex_local_image_invalid_outputs"
  printf -v "$_kodex_local_image_source_output" '%s' ''
  printf -v "$_kodex_local_image_state_output" '%s' ''
  printf -v "$_kodex_local_image_context_output" '%s' ''
  while (($# > 0)); do
    case "$1" in
      --source-root) printf -v "$_kodex_local_image_source_output" '%s' "${2:-}"; shift 2 ;;
      --state-directory) printf -v "$_kodex_local_image_state_output" '%s' "${2:-}"; shift 2 ;;
      --context) printf -v "$_kodex_local_image_context_output" '%s' "${2:-}"; shift 2 ;;
      --help) kodex_local_image_build_usage; exit 0 ;;
      *) kodex_local_image_build_usage; fail "unsupported argument: $1" ;;
    esac
  done
}

# Проверяет прежние source/Dockerfile/state/context и tools в том же порядке до побочных действий.
# Не создаёт builder/cache, не меняет context и использует отдельную диагностику вызывающего скрипта.
kodex_local_image_build_validate() {
  local _kodex_local_image_source_root=$1 _kodex_local_image_state_directory=$2
  local _kodex_local_image_context=$3 _kodex_local_image_dockerfile=$4 _kodex_local_image_command_name
  [[ "$_kodex_local_image_source_root" == /* &&
    -f "$_kodex_local_image_source_root/$_kodex_local_image_dockerfile" ]] ||
    fail 'source root is invalid'
  [[ "$_kodex_local_image_state_directory" == /* && "$_kodex_local_image_state_directory" != / ]] ||
    fail 'state directory is invalid'
  [[ -n "$_kodex_local_image_context" ]] || fail 'exact context is required'
  for _kodex_local_image_command_name in docker jq sha256sum tar; do
    command -v "$_kodex_local_image_command_name" >/dev/null 2>&1 ||
      fail "$_kodex_local_image_command_name is required"
  done
  docker buildx version >/dev/null 2>&1 || fail 'docker buildx is required'
}

# Читает digest единственного manifest из OCI index.json, сохраняя stderr и status tar/jq.
# Не меняет архив и не импортирует образ; проверка sha256 и диагностика workload остаются у caller.
kodex_local_image_manifest_digest() {
  tar -xOf "$1" index.json | jq -er '
  if (.manifests | length) != 1 then error("one image manifest is required")
  else .manifests[0].digest end
'
}
