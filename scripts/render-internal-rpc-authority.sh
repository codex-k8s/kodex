#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'internal-rpc-authority render failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --environment staging|production --image-ref <repository@sha256:digest> --kubernetes-api-cidrs <ip/32[,ipv6/128]> --kubernetes-api-ports <443[,endpoint-port]>\n' "$0" >&2
}

environment_name=""
image_ref=""
kubernetes_api_cidrs=""
kubernetes_api_ports=""
while (($# > 0)); do
  case "$1" in
    --environment)
      (($# >= 2)) || fail "--environment requires a value"
      environment_name="$2"
      shift 2
      ;;
    --image-ref)
      (($# >= 2)) || fail "--image-ref requires a value"
      image_ref="$2"
      shift 2
      ;;
    --kubernetes-api-cidrs)
      (($# >= 2)) || fail "--kubernetes-api-cidrs requires a value"
      kubernetes_api_cidrs="$2"
      shift 2
      ;;
    --kubernetes-api-ports)
      (($# >= 2)) || fail "--kubernetes-api-ports requires a value"
      kubernetes_api_ports="$2"
      shift 2
      ;;
    --help)
      usage
      exit 0
      ;;
    *)
      fail "unsupported argument: $1"
      ;;
  esac
done

[[ -n "$kubernetes_api_ports" ]] ||
  fail "exact Kubernetes API Service and EndpointSlice ports are required"
IFS=',' read -r -a api_ports <<<"$kubernetes_api_ports"
((${#api_ports[@]} >= 1 && ${#api_ports[@]} <= 8)) ||
  fail "Kubernetes API ports must contain between one and eight values"
has_service_port=false
for port in "${api_ports[@]}"; do
  [[ "$port" =~ ^[0-9]+$ ]] &&
    ((10#$port >= 1 && 10#$port <= 65535)) ||
    fail "Kubernetes API ports must be exact TCP port numbers"
  [[ "$port" == "443" ]] && has_service_port=true
done
[[ "$has_service_port" == true ]] ||
  fail "Kubernetes API Service port 443 is required"

case "$environment_name" in
  staging | production) ;;
  *)
    usage
    fail "environment must be staging or production"
    ;;
esac

expected_repository="ghcr.io/codex-k8s/kodex/internal-rpc-authority"
case "$image_ref" in
  "${expected_repository}@sha256:"????????????????????????????????????????????????????????????????) ;;
  *) fail "image reference must use the registered repository and exact sha256 digest" ;;
esac
digest="${image_ref##*@sha256:}"
[[ "$digest" =~ ^[a-f0-9]{64}$ ]] || fail "image digest must be 64 lowercase hexadecimal characters"
[[ "$digest" != "0000000000000000000000000000000000000000000000000000000000000000" ]] ||
  fail "zero image digest is a fail-closed source placeholder"

[[ -n "$kubernetes_api_cidrs" ]] ||
  fail "exact Kubernetes API endpoint CIDRs are required"
IFS=',' read -r -a api_cidrs <<<"$kubernetes_api_cidrs"
((${#api_cidrs[@]} >= 1 && ${#api_cidrs[@]} <= 32)) ||
  fail "Kubernetes API endpoint CIDRs must contain between one and 32 addresses"
for cidr in "${api_cidrs[@]}"; do
  if [[ "$cidr" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}/32$ ]]; then
    IFS='.' read -r octet1 octet2 octet3 octet4 <<<"${cidr%/32}"
    for octet in "$octet1" "$octet2" "$octet3" "$octet4"; do
      ((10#$octet <= 255)) || fail "invalid Kubernetes API IPv4 endpoint: $cidr"
    done
  elif [[ "$cidr" =~ ^[0-9a-fA-F:]+/128$ && "$cidr" == *:* ]]; then
    :
  else
    fail "Kubernetes API endpoints must be exact IPv4 /32 or IPv6 /128 CIDRs"
  fi
done

command -v kubectl >/dev/null 2>&1 || fail "kubectl is required"

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd -- "$script_dir/.." && pwd -P)"
overlay="$repo_root/deploy/k8s/overlays/$environment_name/internal-rpc-authority"
placeholder="${expected_repository}@sha256:0000000000000000000000000000000000000000000000000000000000000000"

rendered="$(kubectl kustomize "$overlay")" || fail "kustomize render failed"
occurrences="$(grep -Fc "image: $placeholder" <<<"$rendered" || true)"
[[ "$occurrences" == "8" ]] ||
  fail "render must contain exactly eight registered deployable image placeholders"

awk -v placeholder="$placeholder" -v replacement="$image_ref" '
  {
    sub("image: " placeholder "$", "image: " replacement)
    print
  }
' <<<"$rendered"

cat <<'EOF'
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: internal-rpc-authority-kubernetes-api-exact-endpoints
  namespace: kodex-system
spec:
  podSelector:
    matchExpressions:
      - key: app.kubernetes.io/name
        operator: In
        values:
          - internal-rpc-authority-publisher
          - internal-rpc-authority-restore-controller
          - internal-rpc-authority-restore-operator
          - internal-rpc-authority-restore-pitr
          - internal-rpc-authority-restore-recovery
  policyTypes: [Egress]
  egress:
    - to:
EOF
for cidr in "${api_cidrs[@]}"; do
  printf '        - ipBlock: {cidr: %s}\n' "$cidr"
done
cat <<'EOF'
      ports:
EOF
for port in "${api_ports[@]}"; do
  printf '        - {protocol: TCP, port: %s}\n' "$port"
done
cat <<'EOF'
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: internal-rpc-authority-database-credential-kubernetes-api
  namespace: kodex-system
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: internal-rpc-authority
      app.kubernetes.io/component: database-credential-reconciler
  policyTypes: [Egress]
  egress:
    - to:
EOF
for cidr in "${api_cidrs[@]}"; do
  printf '        - ipBlock: {cidr: %s}\n' "$cidr"
done
cat <<'EOF'
      ports:
EOF
for port in "${api_ports[@]}"; do
  printf '        - {protocol: TCP, port: %s}\n' "$port"
done
