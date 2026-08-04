#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'agent-runner render failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --environment staging|production --handoff-key-id sha256-<16hex> --handoff-public-key-base64 <32-byte-ed25519-public-key> --kubernetes-api-cidrs <ip/32[,ipv6/128]> --kubernetes-api-ports <443[,endpoint-port]> --provider-egress-cidrs <ip/32[,ipv6/128]>\n' "$0" >&2
}

environment_name=""
handoff_key_id=""
handoff_public_key=""
api_cidrs_raw=""
api_ports_raw=""
provider_cidrs_raw=""
while (($# > 0)); do
  case "$1" in
    --environment) environment_name="${2:-}"; shift 2 ;;
    --handoff-key-id) handoff_key_id="${2:-}"; shift 2 ;;
    --handoff-public-key-base64) handoff_public_key="${2:-}"; shift 2 ;;
    --kubernetes-api-cidrs) api_cidrs_raw="${2:-}"; shift 2 ;;
    --kubernetes-api-ports) api_ports_raw="${2:-}"; shift 2 ;;
    --provider-egress-cidrs) provider_cidrs_raw="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) fail "unsupported argument: $1" ;;
  esac
done

case "$environment_name" in staging|production) ;; *) usage; fail "environment must be staging or production" ;; esac
[[ "$handoff_key_id" =~ ^sha256-[0-9a-f]{16}$ && "$handoff_key_id" != sha256-0000000000000000 ]] ||
  fail "handoff key id must be a non-zero sha256 prefix"
[[ "$handoff_public_key" =~ ^[A-Za-z0-9+/]{43}=$ ]] || fail "handoff public key must be canonical base64"
command -v base64 >/dev/null 2>&1 || fail "base64 is required"
public_key_file="$(mktemp)"
trap 'rm -f -- "$public_key_file"' EXIT
printf '%s' "$handoff_public_key" | base64 --decode >"$public_key_file" 2>/dev/null || fail "handoff public key is not valid base64"
[[ "$(wc -c <"$public_key_file")" == 32 ]] || fail "handoff public key must contain exactly 32 bytes"
[[ "$(od -An -tx1 "$public_key_file" | tr -d ' \n')" != "$(printf '00%.0s' {1..32})" ]] ||
  fail "handoff public key must not be zero"
[[ "$(base64 -w0 <"$public_key_file")" == "$handoff_public_key" ]] || fail "handoff public key base64 is not canonical"

IFS=',' read -r -a api_cidrs <<<"$api_cidrs_raw"
IFS=',' read -r -a api_ports <<<"$api_ports_raw"
IFS=',' read -r -a provider_cidrs <<<"$provider_cidrs_raw"
((${#api_cidrs[@]} >= 1 && ${#api_cidrs[@]} <= 32)) || fail "Kubernetes API CIDRs must contain one to 32 exact endpoints"
((${#api_ports[@]} >= 1 && ${#api_ports[@]} <= 8)) || fail "Kubernetes API ports must contain one to eight exact ports"
((${#provider_cidrs[@]} >= 1 && ${#provider_cidrs[@]} <= 64)) || fail "provider egress CIDRs must contain one to 64 exact endpoints"
has_service_port=false
for cidr in "${api_cidrs[@]}"; do
  [[ "$cidr" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}/32$ || "$cidr" =~ ^[0-9a-fA-F:]+/128$ ]] ||
    fail "Kubernetes API endpoint must be IPv4 /32 or IPv6 /128"
done
for cidr in "${provider_cidrs[@]}"; do
  [[ "$cidr" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}/32$ || "$cidr" =~ ^[0-9a-fA-F:]+/128$ ]] ||
    fail "provider egress endpoint must be IPv4 /32 or IPv6 /128"
done
for port in "${api_ports[@]}"; do
  [[ "$port" =~ ^[0-9]+$ ]] && ((10#$port >= 1 && 10#$port <= 65535)) || fail "Kubernetes API port is invalid"
  [[ "$port" == 443 ]] && has_service_port=true
done
[[ "$has_service_port" == true ]] || fail "Kubernetes API Service port 443 is required"
command -v kubectl >/dev/null 2>&1 || fail "kubectl is required"

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd -- "$script_dir/.." && pwd -P)"
rendered="$(kubectl kustomize "$repo_root/deploy/k8s/overlays/$environment_name/agent-runner")" || fail "kustomize render failed"
placeholder_key_id="sha256-0000000000000000"
placeholder_public_key="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
[[ "$(grep -Fc "$placeholder_key_id" <<<"$rendered" || true)" == 1 ]] || fail "render has an unexpected handoff key id placeholder count"
[[ "$(grep -Fc "$placeholder_public_key" <<<"$rendered" || true)" == 1 ]] || fail "render has an unexpected handoff public key placeholder count"
sed -e "s|$placeholder_key_id|$handoff_key_id|g" -e "s|$placeholder_public_key|$handoff_public_key|g" <<<"$rendered"

emit_policy() {
  local name="$1" destination="$2" ports="$3"
  printf '%s\n' '---' 'apiVersion: networking.k8s.io/v1' 'kind: NetworkPolicy' 'metadata:' "  name: $name" \
    '  namespace: mattercodex-system' 'spec:' '  podSelector:' '    matchLabels:' \
    '      app.kubernetes.io/component: role-runtime' '      runtime.mattercodex.dev/managed: "true"' \
    '  policyTypes: [Egress]' '  egress:' '    - to:'
  for cidr in $destination; do printf '        - ipBlock: {cidr: %s}\n' "$cidr"; done
  printf '%s\n' '      ports:'
  for port in $ports; do printf '        - {protocol: TCP, port: %s}\n' "$port"; done
}

emit_policy agent-runner-kubernetes-api-exact-endpoints "${api_cidrs[*]}" "${api_ports[*]}"
emit_policy agent-runner-provider-exact-endpoints "${provider_cidrs[*]}" "443"
