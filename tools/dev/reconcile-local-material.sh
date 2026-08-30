#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local material reconciliation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --context <exact-context> --state-directory <path> --mode reconcile|commit\n' \
    "$0" >&2
}

context=""
state_directory=""
mode=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'exact Kubernetes context is required'
case "$mode" in reconcile|commit) ;; *) fail 'mode is invalid' ;; esac
[[ "$state_directory" == /* && -d "$state_directory" && ! -L "$state_directory" &&
  "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
  fail 'state directory is invalid'
[[ "$(stat -c '%u' "$state_directory")" == "$(id -u)" &&
  $((8#$(stat -c '%a' "$state_directory") & 8#077)) == 0 ]] ||
  fail 'state directory must be owned by the current user and private'
[[ "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'production context is forbidden'
for command_name in jq kubectl openssl sha256sum stat; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
marker="$state_directory/material-contract-revision.json"
material_directory="$state_directory/material"

digest_files() {
  local path relative
  for path in "$@"; do
    [[ -f "$path" && -s "$path" && ! -L "$path" ]] || fail "contract source is invalid: $path"
    relative=${path#"$repository_root/"}
    printf '%s\0%s\n' "$relative" "$(sha256sum "$path" | awk '{print $1}')"
  done | sha256sum | awk '{print $1}'
}

projection_digest=$(digest_files \
  "$repository_root/tools/install/secret-projections.json" \
  "$repository_root/tools/install/generate-material.sh" \
  "$repository_root/tools/install/materialize-secrets.sh")
nats_digest=$(digest_files \
  "$repository_root/tools/install/nats-runtime-users.tsv" \
  "$repository_root/tools/deploy/materialize-nats-operator-files.sh" \
  "$repository_root/tools/install/materialize-nats-runtime-users.sh" \
  "$repository_root/tools/install/reconcile-nats-runtime-users.sh")
expected_revision=$(jq -cn \
  --arg projection_digest "$projection_digest" \
  --arg nats_digest "$nats_digest" '
    {
      version:1,
      secretProjectionContractSHA256:$projection_digest,
      natsMaterialContractSHA256:$nats_digest
    }
  ')

if [[ "$mode" == commit ]]; then
  [[ -d "$material_directory" && ! -L "$material_directory" &&
    -s "$material_directory/projections.sha256" &&
    -s "$material_directory/nats/runtime-user-policy.version" ]] ||
    fail 'generated local material is incomplete'
  temporary_marker=$(mktemp "$state_directory/.material-contract-revision.XXXXXX")
  printf '%s\n' "$expected_revision" >"$temporary_marker"
  chmod 0600 "$temporary_marker"
  mv -- "$temporary_marker" "$marker"
  printf 'Kodex local material contract revision committed\n'
  exit 0
fi

revision_matches=false
if [[ -f "$marker" && ! -L "$marker" && -d "$material_directory" &&
  ! -L "$material_directory" ]]; then
  actual_revision=$(jq -cS . "$marker" 2>/dev/null || true)
  expected_canonical=$(jq -cS . <<<"$expected_revision")
  [[ "$actual_revision" == "$expected_canonical" ]] && revision_matches=true
fi
if [[ "$revision_matches" == true ]]; then
  certificate_files=(
    "$material_directory/node-pull/client.crt"
    "$material_directory/projections/kodex-buildkit-tls/server.crt"
    "$material_directory/projections/kodex-image-registry-pull/tls.crt"
    "$material_directory/projections/kodex-image-registry-promotion/tls.crt"
    "$material_directory/projections/role-image-builder-buildkit-client/tls.crt"
  )
  for certificate_file in "${certificate_files[@]}"; do
    if [[ ! -f "$certificate_file" || -L "$certificate_file" ]] ||
      ! openssl x509 -in "$certificate_file" -noout -checkend 900 >/dev/null 2>&1; then
      revision_matches=false
      break
    fi
  done
fi
if [[ "$revision_matches" == true ]]; then
  printf 'reuse\n'
  exit 0
fi

kodex_namespace=$(kubectl get namespace/kodex-system -o json 2>/dev/null || true)
identity_namespace=$(kubectl get namespace/identity -o json 2>/dev/null || true)
if [[ -n "$kodex_namespace" ]]; then
  jq -e '
    .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
    .metadata.labels["kodex.dev/environment"] == "staging" and
    .metadata.labels["kodex.dev/local-profile"] == "hot-reload"
  ' <<<"$kodex_namespace" >/dev/null || fail 'kodex-system is not an exact local profile'
fi
if [[ -n "$identity_namespace" ]]; then
  jq -e '
    .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
    .metadata.labels["kodex.dev/capability"] == "identity" and
    ((.metadata.labels["kodex.dev/local-profile"] // "hot-reload") == "hot-reload")
  ' <<<"$identity_namespace" >/dev/null || fail 'identity is not an exact local profile'
  [[ -n "$kodex_namespace" ||
    "$(jq -r '.metadata.labels["kodex.dev/local-profile"] // ""' <<<"$identity_namespace")" == hot-reload ]] ||
    fail 'legacy identity namespace cannot be attributed to the local Kodex profile'
fi

if [[ -n "$kodex_namespace" || -n "$identity_namespace" ]]; then
  namespaces=()
  [[ -z "$kodex_namespace" ]] || namespaces+=(kodex-system)
  [[ -z "$identity_namespace" ]] || namespaces+=(identity)
  kubectl delete namespace "${namespaces[@]}" --wait=false >/dev/null
  for namespace in "${namespaces[@]}"; do
    deadline=$((SECONDS + 600))
    while kubectl get "namespace/$namespace" >/dev/null 2>&1; do
      ((SECONDS < deadline)) || fail "namespace deletion timed out: $namespace"
      sleep 1
    done
  done
fi

rm -rf -- "$material_directory"
rm -f -- "$marker"
printf '%s\n' "$([[ -n "$kodex_namespace" || -n "$identity_namespace" ]] && printf recreate || printf create)"
