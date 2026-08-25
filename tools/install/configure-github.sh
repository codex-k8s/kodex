#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex GitHub configuration failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode apply|readback" \
    '  --owner-pat-file <path> --workflow-sha-file <path>' \
    '  --registry-docker-config-file <path> --owner-actor-id-file <path>' >&2
}

context=""
mode=""
owner_pat_file=""
workflow_sha_file=""
registry_docker_config_file=""
owner_actor_id_file=""
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --owner-pat-file) owner_pat_file="${2:-}"; shift 2 ;;
    --workflow-sha-file) workflow_sha_file="${2:-}"; shift 2 ;;
    --registry-docker-config-file) registry_docker_config_file="${2:-}"; shift 2 ;;
    --owner-actor-id-file) owner_actor_id_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$mode" == apply || "$mode" == readback ]] || fail 'mode is invalid'
[[ -n "$context" ]] || fail 'exact Kubernetes context is required'
[[ -n "$owner_actor_id_file" && "$owner_actor_id_file" == /* ]] ||
  fail 'absolute owner actor ID file path is required'
for input_file in "$owner_pat_file" "$workflow_sha_file" "$registry_docker_config_file"; do
  [[ -f "$input_file" && -s "$input_file" && ! -L "$input_file" ]] ||
    fail 'required input file is absent or invalid'
  file_mode=$(stat -c '%a' "$input_file")
  (((8#$file_mode & 0077) == 0)) || fail 'required input file permissions are too broad'
done
grep -Eq '^[a-f0-9]{40}$' "$workflow_sha_file" || fail 'workflow SHA is invalid'
jq -e '.auths | type == "object" and length == 1' "$registry_docker_config_file" >/dev/null ||
  fail 'release registry Docker configuration is invalid'
for command_name in gh git jq kubectl stat; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'current Kubernetes context mismatch'

repository=codex-k8s/kodex
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
[[ "$(git -C "$repository_root" rev-parse HEAD)" == "$(<"$workflow_sha_file")" ]] ||
  fail 'repository HEAD differs from the workflow SHA'
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
umask 077

export GH_TOKEN
GH_TOKEN=$(<"$owner_pat_file")
owner_actor_id=$(gh api user --jq '.id')
[[ "$owner_actor_id" =~ ^[1-9][0-9]*$ ]] || fail 'GitHub owner actor ID is invalid'
if [[ "$mode" == apply ]]; then
  install -d -m 0700 "$(dirname -- "$owner_actor_id_file")"
  printf '%s' "$owner_actor_id" >"$owner_actor_id_file"
  chmod 0600 "$owner_actor_id_file"
fi
[[ -f "$owner_actor_id_file" && "$(<"$owner_actor_id_file")" == "$owner_actor_id" ]] ||
  fail 'owner actor ID file readback mismatch'

api_service_ip=$(kubectl --context "$context" -n default get service kubernetes \
  -o json | jq -er '.spec.clusterIP | select(type == "string" and test("^[0-9]+(\\.[0-9]+){3}$"))')
api_endpoint_cidrs=$(kubectl --context "$context" -n default get endpoints kubernetes -o json | jq -er '
  [.subsets[]?.addresses[]?.ip | select(test("^[0-9]+(\\.[0-9]+){3}$")) | . + "/32"] |
  unique | sort | select(length > 0) | join(",")
')
api_endpoint_ports=$(kubectl --context "$context" -n default get endpoints kubernetes -o json | jq -er '
  [.subsets[]?.ports[]?.port | tostring] | unique | sort | select(length > 0) | join(",")
')

get_variable_value() {
  local name=$1
  gh api "repos/$repository/actions/variables/$name" --jq '.value'
}

set_variable() {
  local name=$1 value=$2
  if [[ "$mode" == apply ]]; then
    gh variable set "$name" --repo "$repository" --body "$value"
  fi
  [[ "$(get_variable_value "$name")" == "$value" ]] ||
    fail "GitHub variable readback mismatch: $name"
}

set_variable KODEX_REGISTRY_PUSH "$KODEX_REGISTRY_HOST"
set_variable KODEX_NODE_PULL "$KODEX_REGISTRY_HOST"
set_variable KODEX_REPOSITORY_PREFIX kodex
set_variable KODEX_BUILD_PROXY http://kodex-ci-egress-proxy.kodex-ci.svc.cluster.local:8080
set_variable KODEX_BUILD_NO_PROXY kodex-registry.kodex-infra.svc.cluster.local,localhost,127.0.0.1
set_variable KODEX_PUBLIC_HOST "$KODEX_CONTROL_HOST"
set_variable KODEX_PUBLIC_ORIGIN "https://$KODEX_CONTROL_HOST"
set_variable KODEX_OIDC_ISSUER "https://$KODEX_OIDC_HOST/realms/kodex"
set_variable KODEX_OIDC_JWKS_URL "https://$KODEX_OIDC_HOST/realms/kodex/protocol/openid-connect/certs"
set_variable KODEX_OIDC_CONNECT_ADDRESS "$KODEX_OIDC_CONNECT_ADDRESS"
set_variable KODEX_OIDC_TLS_SERVER_NAME "$KODEX_OIDC_TLS_SERVER_NAME"
set_variable KODEX_PROMOTED_PULL_HOST "$KODEX_PROMOTED_PULL_HOST"
set_variable KODEX_KUBERNETES_API_SERVICE_CIDR "$api_service_ip/32"
set_variable KODEX_KUBERNETES_API_ENDPOINT_CIDRS "$api_endpoint_cidrs"
set_variable KODEX_KUBERNETES_API_ENDPOINT_PORTS "$api_endpoint_ports"
set_variable KODEX_INGRESS_CLASS "$KODEX_INGRESS_CLASS"
set_variable KODEX_CLUSTER_ISSUER "$KODEX_CLUSTER_ISSUER"
set_variable KODEX_INGRESS_NAMESPACE "$KODEX_INGRESS_NAMESPACE"
set_variable KODEX_INGRESS_POD_NAME "$KODEX_INGRESS_POD_NAME"
set_variable KODEX_OIDC_NAMESPACE "$KODEX_OIDC_NAMESPACE"
set_variable KODEX_OIDC_POD_NAME "$KODEX_OIDC_POD_NAME"
set_variable KODEX_OIDC_POD_COMPONENT "$KODEX_OIDC_POD_COMPONENT"
set_variable KODEX_OIDC_TARGET_PORT "$KODEX_OIDC_TARGET_PORT"
set_variable KODEX_DISABLE_OBSERVABILITY "${KODEX_DISABLE_OBSERVABILITY:-false}"
if [[ -n "${KODEX_MATTERMOST_HOST:-}" ]]; then
  set_variable KODEX_MATTERMOST_HOST "$KODEX_MATTERMOST_HOST"
elif get_variable_value KODEX_MATTERMOST_HOST >/dev/null 2>&1; then
  if [[ "$mode" == apply ]]; then
    gh variable delete KODEX_MATTERMOST_HOST --repo "$repository"
  else
    fail 'retired Mattermost variable remains configured'
  fi
fi
if [[ -z "${KODEX_MATTERMOST_HOST:-}" ]] &&
  get_variable_value KODEX_MATTERMOST_HOST >/dev/null 2>&1; then
  fail 'retired Mattermost variable deletion readback failed'
fi

if [[ "$mode" == apply ]]; then
  gh secret set KODEX_RELEASE_REGISTRY_DOCKER_CONFIG_JSON --repo "$repository" \
    <"$registry_docker_config_file"
fi
gh secret list --repo "$repository" --json name --jq \
  'any(.[]; .name == "KODEX_RELEASE_REGISTRY_DOCKER_CONFIG_JSON")' | grep -Fxq true ||
  fail 'release registry GitHub secret is absent'

"$repository_root/infra/github/bootstrap-actions-policy.sh" --mode "$mode" \
  --workflow-sha-file "$workflow_sha_file" \
  --build-owner-actor-id-file "$owner_actor_id_file" \
  --deploy-owner-actor-id-file "$owner_actor_id_file"
printf 'Kodex GitHub configuration completed: %s\n' "$mode"
