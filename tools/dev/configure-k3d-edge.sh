#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex k3d edge configuration failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <k3d-context> --cluster <name> --mode apply|readback" \
    '  [--host-ip <127.x.x.x>]' >&2
}

context=""
cluster=""
mode=""
host_ip=127.0.0.2
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --cluster) cluster=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    --host-ip) host_ip=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$context" =~ ^k3d-[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || fail 'context is invalid'
[[ "$cluster" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || fail 'cluster is invalid'
[[ "$context" == "k3d-$cluster" ]] || fail 'context and cluster do not match'
case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ "$host_ip" =~ ^127\.([0-9]{1,3}\.){2}[0-9]{1,3}$ ]] ||
  fail 'host IP must be an exact IPv4 loopback address'
for octet in ${host_ip//./ }; do
  ((10#$octet <= 255)) || fail 'host IP contains an invalid octet'
done
for command_name in docker jq k3d kubectl; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
k3d cluster list -o json | jq -e --arg cluster "$cluster" '
  [.[] | select(.name == $cluster and .serversRunning == .serversCount and
    .agentsRunning == .agentsCount)] | length == 1
' >/dev/null || fail 'k3d cluster is absent or not ready'

load_balancer="k3d-$cluster-serverlb"
docker inspect "$load_balancer" >/dev/null 2>&1 || fail 'k3d load balancer container is absent'
has_binding() {
  local container_port=$1 host_port=$2
  docker inspect "$load_balancer" --format '{{json .HostConfig.PortBindings}}' | jq -e \
    --arg key "$container_port/tcp" --arg host_ip "$host_ip" --arg host_port "$host_port" '
      any(.[$key][]?; .HostIp == $host_ip and .HostPort == $host_port)
    ' >/dev/null
}

if [[ "$mode" == apply ]]; then
  has_binding 80 80 || k3d cluster edit "$cluster" \
    --port-add "$host_ip:80:80@loadbalancer" >/dev/null
  has_binding 443 443 || k3d cluster edit "$cluster" \
    --port-add "$host_ip:443:443@loadbalancer" >/dev/null
fi

has_binding 80 80 || fail 'HTTP edge binding is absent'
has_binding 443 443 || fail 'HTTPS edge binding is absent'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable after edge configuration'

printf 'Kodex k3d edge configured: cluster=%s host=%s\n' "$cluster" "$host_ip"
