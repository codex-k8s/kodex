#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local ingress reconciliation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --context <exact-context> --mode apply|readback\n' "$0" >&2
}

context=""
mode=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "${KUBECONFIG:-}" && -f "$KUBECONFIG" && -n "$context" ]] ||
  fail 'explicit kubeconfig and context are required'
case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
[[ "$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')" == 'https://127.0.0.1:6443' ]] ||
  fail 'Kubernetes API is not the local loopback cluster'
kubectl --context "$context" get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'

nodes=$(kubectl --context "$context" get nodes -o json)
[[ "$(jq -r '.items | length' <<<"$nodes")" == 1 ]] || fail 'exactly one local node is required'
node_ip=$(jq -er '.items[0].status.addresses[] | select(.type == "InternalIP" and (.address | test("^[0-9.]+$"))) | .address' <<<"$nodes") ||
  fail 'local node IPv4 is unavailable'

service=$(kubectl --context "$context" -n kube-system get service/traefik -o json)
jq -e '.metadata.labels["app.kubernetes.io/instance"] == "kodex-local-traefik-kube-system" and .spec.type == "LoadBalancer"' <<<"$service" >/dev/null ||
  fail 'Traefik Service is not owned by the local Kodex release'
deployment=$(kubectl --context "$context" -n kube-system get deployment/traefik -o json)
jq -e '.metadata.annotations["meta.helm.sh/release-name"] == "kodex-local-traefik"' <<<"$deployment" >/dev/null ||
  fail 'Traefik Deployment is not owned by the local Kodex release'

daemonsets=$(kubectl --context "$context" -n kube-system get daemonsets -o json)
daemonset=$(jq -er '[.items[] | select(.metadata.labels["svccontroller.k3s.cattle.io/svcname"] == "traefik" and .metadata.labels["svccontroller.k3s.cattle.io/svcnamespace"] == "kube-system") | .metadata.name] | if length == 1 then .[0] else empty end' <<<"$daemonsets") ||
  fail 'exactly one Traefik ServiceLB DaemonSet is required'
pods=$(kubectl --context "$context" -n kube-system get pods -l "app=$daemonset" -o json)
pod=$(jq -er --arg daemonset "$daemonset" '[.items[] | select(.status.phase == "Running" and (.status.containerStatuses | length) > 0 and all(.status.containerStatuses[]; .ready == true)) | select(.metadata.ownerReferences[]? | .kind == "DaemonSet" and .name == $daemonset) | .metadata.name] | if length == 1 then .[0] else empty end' <<<"$pods") ||
  fail 'exactly one owned Traefik ServiceLB Pod is required'
target_ips=$(kubectl --context "$context" -n kube-system exec "$pod" -c lb-tcp-443 -- printenv DEST_IPS) ||
  fail 'ServiceLB target IP readback failed'

if [[ ",$target_ips," == *",$node_ip,"* ]]; then
  printf 'Kodex local ingress target is current: %s\n' "$node_ip"
  exit 0
fi
[[ "$mode" == apply ]] || fail 'ServiceLB target IP is stale; apply mode is required'

# Pod environment variables are fixed at creation. A node address change
# requires recreating this exact DaemonSet-owned Pod; no Service or PVC is removed.
kubectl --context "$context" -n kube-system delete pod "$pod" --wait=false >/dev/null ||
  fail 'stale ServiceLB Pod recreation failed'
kubectl --context "$context" -n kube-system rollout status "daemonset/$daemonset" --timeout=2m >/dev/null ||
  fail 'Traefik ServiceLB rollout did not complete'
target_ips=""
attempt=0
while ((attempt < 30)); do
  ((attempt += 1))
  pods=$(kubectl --context "$context" -n kube-system get pods -l "app=$daemonset" -o json)
  if pod=$(jq -er --arg daemonset "$daemonset" '[.items[] | select(.status.phase == "Running" and (.status.containerStatuses | length) > 0 and all(.status.containerStatuses[]; .ready == true)) | select(.metadata.ownerReferences[]? | .kind == "DaemonSet" and .name == $daemonset) | .metadata.name] | if length == 1 then .[0] else empty end' <<<"$pods"); then
    if target_ips=$(kubectl --context "$context" -n kube-system exec "$pod" -c lb-tcp-443 -- printenv DEST_IPS 2>/dev/null); then
      break
    fi
  fi
  sleep 2
done
[[ -n "$target_ips" ]] || fail 'recreated ServiceLB target IP readback failed'
[[ ",$target_ips," == *",$node_ip,"* ]] || fail 'recreated ServiceLB still has a stale target IP'
printf 'Kodex local ingress target reconciled: %s\n' "$node_ip"
