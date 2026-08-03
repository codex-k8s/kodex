#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'runtime-controller render failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --environment staging|production --controller-image-ref <repository@sha256:digest> --authority-image-ref <repository@sha256:digest> --kubernetes-api-cidrs <ip/32[,ipv6/128]> --kubernetes-api-ports <443[,endpoint-port]>\n' "$0" >&2
}

environment_name=""
controller_image=""
authority_image=""
api_cidrs_raw=""
api_ports_raw=""
while (($# > 0)); do
  case "$1" in
    --environment) environment_name="${2:-}"; shift 2 ;;
    --controller-image-ref) controller_image="${2:-}"; shift 2 ;;
    --authority-image-ref) authority_image="${2:-}"; shift 2 ;;
    --kubernetes-api-cidrs) api_cidrs_raw="${2:-}"; shift 2 ;;
    --kubernetes-api-ports) api_ports_raw="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) fail "unsupported argument: $1" ;;
  esac
done

case "$environment_name" in staging|production) ;; *) usage; fail "environment must be staging or production" ;; esac
validate_image() {
  local value="$1" repository="$2"
  [[ "$value" == "$repository@sha256:"???????????????????????????????????????????????????????????????? ]] || fail "image reference has an unexpected repository or digest"
  local digest="${value##*@sha256:}"
  [[ "$digest" =~ ^[0-9a-f]{64}$ && "$digest" != "$(printf '0%.0s' {1..64})" ]] || fail "image digest must be non-zero lowercase sha256"
}
validate_image "$controller_image" "mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/runtime-controller"
validate_image "$authority_image" "ghcr.io/codex-k8s/matter-codex/internal-rpc-authority"

IFS=',' read -r -a api_cidrs <<<"$api_cidrs_raw"
IFS=',' read -r -a api_ports <<<"$api_ports_raw"
((${#api_cidrs[@]} >= 1 && ${#api_cidrs[@]} <= 32)) || fail "Kubernetes API CIDRs must contain one to 32 exact endpoints"
((${#api_ports[@]} >= 1 && ${#api_ports[@]} <= 8)) || fail "Kubernetes API ports must contain one to eight exact ports"
has_service_port=false
for cidr in "${api_cidrs[@]}"; do
  [[ "$cidr" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}/32$ || "$cidr" =~ ^[0-9a-fA-F:]+/128$ ]] || fail "Kubernetes API endpoint must be IPv4 /32 or IPv6 /128"
done
for port in "${api_ports[@]}"; do
  [[ "$port" =~ ^[0-9]+$ ]] && ((10#$port >= 1 && 10#$port <= 65535)) || fail "Kubernetes API port is invalid"
  [[ "$port" == 443 ]] && has_service_port=true
done
[[ "$has_service_port" == true ]] || fail "Kubernetes API Service port 443 is required"
command -v kubectl >/dev/null 2>&1 || fail "kubectl is required"

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd -- "$script_dir/.." && pwd -P)"
rendered="$(kubectl kustomize "$repo_root/deploy/k8s/overlays/$environment_name/runtime-controller")" || fail "kustomize render failed"
controller_placeholder="mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/runtime-controller@sha256:$(printf '0%.0s' {1..64})"
authority_placeholder="ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:$(printf '0%.0s' {1..64})"
[[ "$(grep -Fc "$controller_placeholder" <<<"$rendered" || true)" == 3 ]] || fail "render has an unexpected controller placeholder count"
[[ "$(grep -Fc "$authority_placeholder" <<<"$rendered" || true)" == 3 ]] || fail "render has an unexpected authority placeholder count"
sed -e "s|$controller_placeholder|$controller_image|g" -e "s|$authority_placeholder|$authority_image|g" <<<"$rendered"

emit_policy() {
  local name="$1" selector="$2"
  printf '%s\n' '---' 'apiVersion: networking.k8s.io/v1' 'kind: NetworkPolicy' 'metadata:' "  name: $name" '  namespace: mattercodex-system' 'spec:' '  podSelector:'
  printf '%s\n' "$selector"
  printf '%s\n' '  policyTypes: [Egress]' '  egress:' '    - to:'
  for cidr in "${api_cidrs[@]}"; do printf '        - ipBlock: {cidr: %s}\n' "$cidr"; done
  printf '%s\n' '      ports:'
  for port in "${api_ports[@]}"; do printf '        - {protocol: TCP, port: %s}\n' "$port"; done
}

emit_policy runtime-controller-kubernetes-api-exact-endpoints '    matchLabels:
      app.kubernetes.io/name: runtime-controller
      app.kubernetes.io/component: internal-controller'
emit_policy runtime-controller-workers-kubernetes-api-exact-endpoints '    matchLabels:
      app.kubernetes.io/name: runtime-controller
      runtime.mattercodex.dev/managed: "true"
    matchExpressions:
      - key: app.kubernetes.io/component
        operator: In
        values: [runtime-archive, runtime-restore-verifier, runtime-cleanup-authorizer]'
emit_policy runtime-role-kubernetes-api-exact-endpoints '    matchLabels:
      app.kubernetes.io/name: runtime-controller
      app.kubernetes.io/component: role-runtime
    matchExpressions:
      - key: runtime.mattercodex.dev/access-profile
        operator: In
        values: [project_read_only, cluster_admin]'
