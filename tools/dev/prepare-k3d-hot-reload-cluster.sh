#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex k3d hot-reload cluster preparation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --cluster <name> --context <k3d-context> --mode apply|readback" \
    '  --source-root <absolute-repository-path> --state-directory <absolute-path>' \
    '  [--host-ip <127.x.x.x>] [--replace-empty|--replace-owned]' >&2
}

cluster=""
context=""
mode=""
source_root=""
state_directory=""
host_ip=127.0.0.2
replace_empty=false
replace_owned=false
while (($# > 0)); do
  case "$1" in
    --cluster) cluster=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    --source-root) source_root=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --host-ip) host_ip=${2:-}; shift 2 ;;
    --replace-empty) replace_empty=true; shift ;;
    --replace-owned) replace_owned=true; shift ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
[[ "$replace_empty" != true || "$replace_owned" != true ]] ||
  fail 'cluster replacement modes are mutually exclusive'

[[ "$cluster" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || fail 'cluster is invalid'
[[ "$context" == "k3d-$cluster" ]] || fail 'context and cluster do not match'
case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ "$host_ip" =~ ^127\.([0-9]{1,3}\.){2}[0-9]{1,3}$ ]] ||
  fail 'host IP must be an exact IPv4 loopback address'
for octet in ${host_ip//./ }; do
  ((10#$octet <= 255)) || fail 'host IP contains an invalid octet'
done
[[ "$source_root" == /* && ( -d "$source_root/.git" || -f "$source_root/.git" ) &&
  ! -L "$source_root" ]] || fail 'source root is invalid'
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" &&
  ! -L "$state_directory" ]] || fail 'state directory is invalid'
case "$state_directory" in "$source_root"|"$source_root"/*) fail 'state directory must stay outside source' ;; esac
for command_name in docker install jq k3d kubectl; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
install -d -m 0700 "$state_directory"

cluster_json=$(k3d cluster list -o json)
cluster_count=$(jq -r --arg cluster "$cluster" '[.[] | select(.name == $cluster)] | length' <<<"$cluster_json")
[[ "$cluster_count" == 0 || "$cluster_count" == 1 ]] || fail 'k3d cluster identity is ambiguous'

node_mounts_match() {
  local node=$1
  docker inspect "$node" --format '{{json .Mounts}}' | jq -e \
    --arg source "$source_root" --arg state "$state_directory" '
      any(.[]; .Type == "bind" and .Source == $source and .Destination == $source and .RW == false) and
      any(.[]; .Type == "bind" and .Source == $state and .Destination == $state and .RW == true)
    ' >/dev/null
}

cluster_source_matches=false
if [[ "$cluster_count" == 1 ]] &&
  node_mounts_match "k3d-$cluster-server-0" &&
  node_mounts_match "k3d-$cluster-agent-0"; then
  cluster_source_matches=true
fi

api_binding_matches() {
  docker inspect "k3d-$cluster-serverlb" --format '{{json .HostConfig.PortBindings}}' | jq -e \
    --arg host_ip "$host_ip" '
      any(.["6443/tcp"][]?; .HostIp == $host_ip and .HostPort == "6443")
    ' >/dev/null
}

cluster_matches=false
if [[ "$cluster_source_matches" == true ]] &&
  api_binding_matches; then
  cluster_matches=true
fi

if [[ "$mode" == apply && "$cluster_matches" == false ]]; then
  [[ "$replace_empty" == true || "$replace_owned" == true ]] ||
    fail 'cluster replacement requires an explicit replacement mode'
  if [[ "$cluster_count" == 1 ]]; then
    [[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
    kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'
    if [[ "$replace_owned" == true ]]; then
      [[ "$cluster_source_matches" == true ]] || fail 'owned cluster source mounts mismatch'
      kubectl get namespace -o json | jq -e '
        all(.items[].metadata.name;
          . == "default" or . == "kube-node-lease" or . == "kube-public" or
          . == "kube-system" or . == "cert-manager" or . == "identity" or
          . == "kodex-runtime" or . == "kodex-secret-drafts" or
          . == "kodex-system" or . == "kodex-trust" or . == "observability")
      ' >/dev/null || fail 'owned cluster contains an unexpected namespace'
      kubectl -n default get deployment,statefulset,daemonset,job,cronjob -o json | jq -e \
        '.items | length == 0' >/dev/null || fail 'default namespace contains workloads'
    else
      kubectl get namespace -o json | jq -e '
        [.items[].metadata.name |
          select(. != "default" and . != "kube-node-lease" and
            . != "kube-public" and . != "kube-system")] | length == 0
      ' >/dev/null || fail 'cluster contains non-system namespaces'
      kubectl get persistentvolume -o json | jq -e '.items | length == 0' >/dev/null ||
        fail 'cluster contains persistent volumes'
    fi
    k3s_image=$(docker inspect "k3d-$cluster-server-0" --format '{{.Config.Image}}')
    [[ "$k3s_image" =~ ^(docker\.io/)?rancher/k3s:v[0-9]+\.[0-9]+\.[0-9]+-k3s[0-9]+$ ]] ||
      fail 'existing k3s image is not exact'
    k3d cluster delete "$cluster" >/dev/null
  else
    k3s_image=docker.io/rancher/k3s:v1.35.5-k3s1
  fi
  k3d cluster create "$cluster" \
    --servers 1 --agents 1 --image "$k3s_image" \
    --api-port "$host_ip:6443" \
    --port "$host_ip:80:80@loadbalancer" \
    --port "$host_ip:443:443@loadbalancer" \
    --volume "$source_root:$source_root:ro@all" \
    --volume "$state_directory:$state_directory@all" \
    --kubeconfig-update-default --kubeconfig-switch-context \
    --wait --timeout 5m >/dev/null
fi

[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch after preparation'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable after preparation'
node_mounts_match "k3d-$cluster-server-0" || fail 'server mount contract mismatch'
node_mounts_match "k3d-$cluster-agent-0" || fail 'agent mount contract mismatch'
api_binding_matches || fail 'Kubernetes API loopback binding contract mismatch'
"$(dirname -- "$0")/configure-k3d-edge.sh" --context "$context" --cluster "$cluster" \
  --mode "$mode" --host-ip "$host_ip" >/dev/null

printf 'Kodex k3d hot-reload cluster ready: cluster=%s host=%s\n' "$cluster" "$host_ip"
