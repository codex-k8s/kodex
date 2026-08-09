#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'direct-production secret bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --context <exact-context> --mode preflight|apply|readback\n' "$0" >&2
}

expected_context=""
mode=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail "exact Kubernetes context is required"
case "$mode" in preflight|apply|readback) ;; *) fail "mode must be preflight, apply or readback" ;; esac
for command_name in kubectl openssl jq base64; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
actual_context=$(kubectl config current-context)
[[ "$actual_context" == "$expected_context" ]] || fail "Kubernetes context mismatch"

namespace=mattercodex-system
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
umask 077

secret_exists() {
  kubectl --context "$expected_context" -n "$namespace" get secret "$1" >/dev/null 2>&1
}

verify_secret_keys() {
  local name=$1
  shift
  local expected actual
  expected=$(printf '%s\n' "$@" | LC_ALL=C sort | jq -Rsc 'split("\n") | map(select(length > 0))')
  actual=$(kubectl --context "$expected_context" -n "$namespace" get secret "$name" -o json |
    jq -c '[.data | keys[]] | sort')
  [[ "$actual" == "$expected" ]] || fail "Secret $name has an unexpected key set"
}

random_hex() {
  openssl rand -hex 32
}

prepare_secret_files() {
  local name=$1
  shift
  local key file
  mkdir -p "$temporary_directory/$name"
  for key in "$@"; do
    file="$temporary_directory/$name/$key"
    case "$name:$key" in
      mattercodex-redis-credentials:username) printf '%s' default >"$file" ;;
      mattercodex-nats-credentials:username) printf '%s' mattercodex >"$file" ;;
      mattercodex-object-store-credentials:username) printf '%s' mattercodex >"$file" ;;
      *) random_hex >"$file" ;;
    esac
  done
}

materialize_secret() {
  local name=$1
  shift
  local keys=("$@")
  if secret_exists "$name"; then
    verify_secret_keys "$name" "${keys[@]}"
    printf 'Secret/%s: present\n' "$name"
    return
  fi
  [[ "$mode" != readback ]] || fail "required Secret $name is absent"
  prepare_secret_files "$name" "${keys[@]}"
  local arguments=()
  local key
  for key in "${keys[@]}"; do
    arguments+=("--from-file=$key=$temporary_directory/$name/$key")
  done
  if [[ "$mode" == apply ]]; then
    kubectl --context "$expected_context" -n "$namespace" create secret generic "$name" "${arguments[@]}" >/dev/null
    verify_secret_keys "$name" "${keys[@]}"
    printf 'Secret/%s: created\n' "$name"
  else
    kubectl --context "$expected_context" -n "$namespace" create secret generic "$name" "${arguments[@]}" --dry-run=client -o yaml >/dev/null
    printf 'Secret/%s: preflight-ready\n' "$name"
  fi
}

if [[ "$mode" != preflight ]]; then
  kubectl --context "$expected_context" get namespace "$namespace" >/dev/null 2>&1 ||
    fail "Namespace $namespace is absent"
fi

materialize_secret mattercodex-postgresql-bootstrap password
materialize_secret mattercodex-postgresql-app-passwords \
  CONTROL_PLANE_POSTGRES_PASSWORD \
  INTERNAL_RPC_AUTHORITY_POSTGRES_PASSWORD \
  RUNTIME_CONTROLLER_POSTGRES_PASSWORD \
  INTERACTION_GATEWAY_POSTGRES_PASSWORD \
  INTEGRATION_GATEWAY_POSTGRES_PASSWORD
materialize_secret mattercodex-redis-credentials username password
materialize_secret mattercodex-nats-credentials username password
materialize_secret mattercodex-object-store-credentials username password

printf 'direct-production secret bootstrap %s completed\n' "$mode"
