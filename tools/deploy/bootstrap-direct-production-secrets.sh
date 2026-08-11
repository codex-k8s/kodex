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
for command_name in kubectl openssl jq base64 cmp head tail tr wc; do
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
  openssl rand -hex 32 | tr -d '\n'
}

expected_username() {
  case "$1" in
    mattercodex-redis-credentials) printf '%s' default ;;
    mattercodex-object-store-credentials) printf '%s' mattercodex ;;
    *) fail "Secret $1 has no declared username contract" ;;
  esac
}

load_existing_secret_files() {
  local name=$1
  shift
  local key file expected_file size last_byte normalized_file
  existing_secret_changed=0
  mkdir -p "$temporary_directory/$name"
  for key in "$@"; do
    file="$temporary_directory/$name/$key"
    kubectl --context "$expected_context" -n "$namespace" get secret "$name" -o json |
      jq -er --arg key "$key" '.data[$key]' | base64 -d >"$file" ||
      fail "Secret $name key $key cannot be decoded"
    if [[ "$key" == username ]]; then
      expected_file="$temporary_directory/$name/$key.expected"
      expected_username "$name" >"$expected_file"
      cmp -s "$file" "$expected_file" || fail "Secret $name key $key has an unexpected value"
      rm -f -- "$expected_file"
      continue
    fi
    size=$(wc -c <"$file")
    if [[ "$size" == 65 ]]; then
      last_byte=$(tail -c 1 "$file" | base64 | tr -d '\n')
      [[ "$last_byte" == Cg== ]] || fail "Secret $name key $key has an invalid 65-byte value"
      normalized_file="$file.normalized"
      head -c 64 "$file" >"$normalized_file"
      LC_ALL=C grep -Eq '^[0-9a-f]{64}$' "$normalized_file" ||
        fail "Secret $name key $key has invalid legacy material"
      mv -- "$normalized_file" "$file"
      existing_secret_changed=1
      continue
    fi
    [[ "$size" == 64 ]] && LC_ALL=C grep -Eq '^[0-9a-f]{64}$' "$file" ||
      fail "Secret $name key $key must contain exactly 64 lowercase hexadecimal bytes"
  done
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
    load_existing_secret_files "$name" "${keys[@]}"
    if [[ "$existing_secret_changed" == 1 ]]; then
      [[ "$mode" == apply ]] ||
        fail "Secret $name contains legacy newline-terminated material; run apply to normalize it"
      local update_arguments=()
      local update_key
      for update_key in "${keys[@]}"; do
        update_arguments+=("--from-file=$update_key=$temporary_directory/$name/$update_key")
      done
      kubectl --context "$expected_context" -n "$namespace" create secret generic "$name" \
        "${update_arguments[@]}" --dry-run=client -o yaml |
        kubectl --context "$expected_context" -n "$namespace" apply -f - >/dev/null
      verify_secret_keys "$name" "${keys[@]}"
      load_existing_secret_files "$name" "${keys[@]}"
      [[ "$existing_secret_changed" == 0 ]] || fail "Secret $name normalization did not persist"
      printf 'Secret/%s: normalized legacy material\n' "$name"
      return
    fi
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
materialize_secret mattercodex-redis-credentials username password
materialize_secret mattercodex-object-store-credentials username password

printf 'direct-production secret bootstrap %s completed\n' "$mode"
