#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Bootstrap registry operation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode preflight|apply|readback" \
    '  --registry-host <exact-dns-name> --ingress-class <name> --cluster-issuer <name>' \
    '  [--username-file <path> --password-file <path> --docker-config-output <path>]' >&2
}

expected_context=""
mode=""
username_file=""
password_file=""
docker_config_output=""
registry_host=""
ingress_class=""
cluster_issuer=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --username-file) username_file="${2:-}"; shift 2 ;;
    --password-file) password_file="${2:-}"; shift 2 ;;
    --docker-config-output) docker_config_output="${2:-}"; shift 2 ;;
    --registry-host) registry_host="${2:-}"; shift 2 ;;
    --ingress-class) ingress_class="${2:-}"; shift 2 ;;
    --cluster-issuer) cluster_issuer="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail 'exact context is required'
[[ "$mode" == preflight || "$mode" == apply || "$mode" == readback ]] || fail 'mode is invalid'
[[ "$registry_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$registry_host" == *.* ]] ||
  fail 'registry host is invalid'
for resource_name in "$ingress_class" "$cluster_issuer"; do
  [[ "$resource_name" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail 'ingress resource name is invalid'
done
for command_name in base64 curl htpasswd jq kubectl yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail 'current Kubernetes context mismatch'

namespace=matter-kodex-prod
script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)

kubectl get namespace "$namespace" >/dev/null 2>&1 || fail 'registry namespace is absent'

if [[ "$mode" == apply ]]; then
  for input_file in "$username_file" "$password_file"; do
    [[ -f "$input_file" && -r "$input_file" && ! -L "$input_file" ]] || fail 'registry credential file is invalid'
  done
  username=$(tr -d '\r\n' <"$username_file")
  password=$(tr -d '\r\n' <"$password_file")
  [[ "$username" =~ ^[a-zA-Z0-9._-]{3,64}$ ]] || fail 'registry username is invalid'
  [[ ${#password} -ge 32 ]] || fail 'registry password is too short'

  temporary_directory=$(mktemp -d)
  trap 'rm -rf -- "$temporary_directory"' EXIT
  htpasswd -i -B -C 12 -c "$temporary_directory/users" "$username" \
    <"$password_file" >/dev/null
  kubectl -n "$namespace" create secret generic kodex-release-registry-auth \
    --from-file=users="$temporary_directory/users" --dry-run=client -o yaml |
    kubectl apply -f - >/dev/null
  rendered="$temporary_directory/registry.yaml"
  kubectl kustomize "$script_directory" >"$rendered"
  REGISTRY_HOST="$registry_host" INGRESS_CLASS="$ingress_class" CLUSTER_ISSUER="$cluster_issuer" yq -i '
    (.. | select(tag == "!!str")) |= (
      sub("__KODEX_REGISTRY_HOST__"; strenv(REGISTRY_HOST)) |
      sub("__KODEX_INGRESS_CLASS__"; strenv(INGRESS_CLASS)) |
      sub("__KODEX_CLUSTER_ISSUER__"; strenv(CLUSTER_ISSUER))
    )
  ' "$rendered"
  kubectl apply -f "$rendered" >/dev/null
  kubectl -n "$namespace" rollout status deployment/kodex-registry --timeout=300s >/dev/null

  if [[ -n "$docker_config_output" ]]; then
    umask 077
    auth=$(printf '%s:%s' "$username" "$password" | base64 | tr -d '\n')
    jq -n --arg host "$registry_host" --arg auth "$auth" \
      '{auths:{($host):{auth:$auth}}}' >"$docker_config_output"
  fi
fi

if [[ "$mode" == preflight || "$mode" == readback ]]; then
  kubectl -n "$namespace" get deployment kodex-registry >/dev/null
  kubectl -n "$namespace" get service kodex-registry >/dev/null
  kubectl -n "$namespace" get persistentvolumeclaim kodex-registry >/dev/null
fi

if [[ "$mode" == readback ]]; then
  [[ -f "$username_file" && -f "$password_file" ]] || fail 'credential files are required for readback'
  temporary_directory=$(mktemp -d)
  trap 'rm -rf -- "$temporary_directory"' EXIT
  username=$(tr -d '\r\n' <"$username_file")
  password=$(tr -d '\r\n' <"$password_file")
  printf 'user = "%s:%s"\nsilent\nshow-error\nfail\n' "$username" "$password" >"$temporary_directory/curl.conf"
  chmod 0600 "$temporary_directory/curl.conf"
  response=$(curl --config "$temporary_directory/curl.conf" --max-time 15 "https://$registry_host/v2/")
  [[ "$response" == '{}' ]] || fail 'registry HTTPS readback failed'
fi

printf 'Bootstrap registry operation completed: %s\n' "$mode"
