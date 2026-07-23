#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

builder=""
context=""
dockerfile=""
tag=""
network=""
frontend_attrs_json='{}'
frontend_attrs_seen=false
build_args=()

while (($# > 0)); do
  case "$1" in
    --builder)
      (($# >= 2)) || fail "для --builder нужно значение"
      builder="$2"
      shift 2
      ;;
    --context)
      (($# >= 2)) || fail "для --context нужен путь"
      context="$2"
      shift 2
      ;;
    --dockerfile)
      (($# >= 2)) || fail "для --dockerfile нужен путь"
      dockerfile="$2"
      shift 2
      ;;
    --tag)
      (($# >= 2)) || fail "для --tag нужно значение"
      tag="$2"
      shift 2
      ;;
    --network)
      (($# >= 2)) || fail "для --network нужно значение"
      network="$2"
      shift 2
      ;;
    --build-arg)
      (($# >= 2)) || fail "для --build-arg нужно значение"
      build_args+=("$2")
      shift 2
      ;;
    --frontend-attrs-json)
      (($# >= 2)) || fail "для --frontend-attrs-json нужен JSON object"
      [[ "$frontend_attrs_seen" == false ]] || fail "--frontend-attrs-json нельзя передавать повторно"
      frontend_attrs_json="$2"
      frontend_attrs_seen=true
      shift 2
      ;;
    --build-context | --build-context=* | --opt | --opt=* | --frontend | --frontend=*)
      fail "named contexts и произвольные BuildKit frontend attrs запрещены для защищённого agent-runner Dockerfile"
      ;;
    *)
      fail "неизвестный аргумент $1"
      ;;
  esac
done

[[ "$builder" == docker || "$builder" == nerdctl ]] || fail "builder должен быть docker или nerdctl"
[[ -n "$context" ]] || fail "build context не задан"
[[ "$context" != -* ]] || fail "build context не должен интерпретироваться как builder option"
[[ -n "$dockerfile" ]] || fail "Dockerfile не задан"
[[ -n "$tag" ]] || fail "image tag не задан"
[[ -z "$network" || "$network" == host ]] || fail "поддерживается только network=host"
[[ "$frontend_attrs_json" == '{}' ]] || \
  fail "allowlist BuildKit frontend attrs для защищённого agent-runner Dockerfile должен быть пустым JSON object"

case "$dockerfile" in
  services/jobs/agent-runner/Dockerfile | "$context/services/jobs/agent-runner/Dockerfile")
    allowed_build_arg=MATTERCODEX_CODEX_PACKAGE
    ;;
  deploy/images/agent-runner/Dockerfile | "$context/deploy/images/agent-runner/Dockerfile")
    allowed_build_arg=CODEX_PACKAGE
    ;;
  *)
    fail "неподдерживаемый защищённый agent-runner Dockerfile: $dockerfile"
    ;;
esac

build_arg_count=0
for build_arg in "${build_args[@]}"; do
  [[ "$build_arg" == *=* ]] || fail "build arg должен иметь форму key=value"
  build_arg_key="${build_arg%%=*}"
  [[ "$build_arg_key" == "$allowed_build_arg" ]] || \
    fail "для защищённого agent-runner Dockerfile разрешён только build arg $allowed_build_arg"
  build_arg_count=$((build_arg_count + 1))
done
((build_arg_count <= 1)) || fail "разрешённый agent-runner build arg нельзя передавать повторно"

[[ -f "$dockerfile" || -f "$context/$dockerfile" ]] || fail "Dockerfile не найден: $dockerfile"
builder_path="$(type -P -- "$builder" || true)"
[[ -n "$builder_path" && -x "$builder_path" ]] || fail "builder $builder не найден как исполняемый файл в PATH"

command=("$builder_path")
if [[ "$builder" == nerdctl ]]; then
  command+=(-n k8s.io)
fi
command+=(build)
if [[ -n "$network" ]]; then
  command+=("--network=$network")
fi
for build_arg in "${build_args[@]}"; do
  command+=(--build-arg "$build_arg")
done
command+=(-f "$dockerfile" -t "$tag" -- "$context")

unset BUILDKIT_SYNTAX
exec "${command[@]}"
