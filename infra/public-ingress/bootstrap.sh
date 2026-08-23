#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Public ingress bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode preflight|apply|readback" \
    '  --namespace <namespace> --deployment <name> --cluster-role <name>' >&2
}

expected_context=""
mode=""
namespace=""
deployment=""
cluster_role=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --namespace) namespace="${2:-}"; shift 2 ;;
    --deployment) deployment="${2:-}"; shift 2 ;;
    --cluster-role) cluster_role="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" && -n "$namespace" && -n "$deployment" && -n "$cluster_role" ]] ||
  fail 'exact context and resource names are required'
[[ "$mode" == preflight || "$mode" == apply || "$mode" == readback ]] || fail 'mode is invalid'
for value in "$namespace" "$deployment" "$cluster_role"; do
  [[ "$value" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail 'resource name is invalid'
done
for command_name in curl jq kubectl sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail 'current Kubernetes context mismatch'

kubectl -n "$namespace" get deployment "$deployment" >/dev/null 2>&1 || fail 'ingress deployment is absent'
kubectl get clusterrole "$cluster_role" >/dev/null 2>&1 || fail 'ingress ClusterRole is absent'

crd_url=https://raw.githubusercontent.com/traefik/traefik/v3.7.1/docs/content/reference/dynamic-configuration/kubernetes-crd-definition-v1.yml
crd_sha256=cd2a9f0ac0575dab99cc0f1743e5177684e001122e2e12fa4afd700102f99def

if [[ "$mode" == apply ]]; then
  temporary_directory=$(mktemp -d)
  trap 'rm -rf -- "$temporary_directory"' EXIT
  restart_required=false
  curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location \
    "$crd_url" --output "$temporary_directory/traefik-crds.yaml"
  printf '%s  %s\n' "$crd_sha256" "$temporary_directory/traefik-crds.yaml" |
    sha256sum --check --status || fail 'Traefik CRD digest mismatch'
  kubectl apply -f "$temporary_directory/traefik-crds.yaml" >/dev/null

  if ! kubectl get clusterrole "$cluster_role" -o json |
    jq -e 'any(.rules[]?; (.apiGroups | index("traefik.io")) != null and (.resources | index("serverstransports")) != null)' >/dev/null; then
    kubectl patch clusterrole "$cluster_role" --type=json -p='[
      {"op":"add","path":"/rules/-","value":{
        "apiGroups":["traefik.io"],
        "resources":["middlewares","middlewaretcps","ingressroutes","ingressroutetcps","ingressrouteudps","traefikservices","serverstransports","serverstransporttcps","tlsoptions","tlsstores"],
        "verbs":["get","list","watch"]
      }}
    ]' >/dev/null
    restart_required=true
  fi

  if ! kubectl get clusterrole "$cluster_role" -o json |
    jq -e 'any(.rules[]?; (.apiGroups | index("")) != null and (.resources | index("configmaps")) != null and
      (["get","list","watch"] - .verbs | length == 0))' >/dev/null; then
    kubectl patch clusterrole "$cluster_role" --type=json -p='[
      {"op":"add","path":"/rules/-","value":{
        "apiGroups":[""],
        "resources":["configmaps"],
        "verbs":["get","list","watch"]
      }}
    ]' >/dev/null
    restart_required=true
  fi

  if ! kubectl -n "$namespace" get deployment "$deployment" -o jsonpath='{.spec.template.spec.containers[0].args}' |
    grep -Fq -- '--providers.kubernetescrd=true'; then
    kubectl -n "$namespace" patch deployment "$deployment" --type=json -p='[
      {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--providers.kubernetescrd=true"},
      {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--providers.kubernetescrd.allowcrossnamespace=false"}
    ]' >/dev/null
  fi
  if [[ "$restart_required" == true ]]; then
    kubectl -n "$namespace" rollout restart deployment "$deployment" >/dev/null
  fi
  kubectl -n "$namespace" rollout status deployment "$deployment" --timeout=300s >/dev/null
fi

if [[ "$mode" == readback ]]; then
  kubectl get customresourcedefinition serverstransports.traefik.io >/dev/null
  kubectl auth can-i list serverstransports.traefik.io \
    --as="system:serviceaccount:$namespace:$deployment" --all-namespaces | grep -Fxq yes ||
    fail 'ingress controller cannot read ServersTransport resources'
  kubectl auth can-i list configmaps \
    --as="system:serviceaccount:$namespace:$deployment" --all-namespaces | grep -Fxq yes ||
    fail 'ingress controller cannot read ConfigMap resources'
  kubectl -n "$namespace" get deployment "$deployment" -o jsonpath='{.spec.template.spec.containers[0].args}' |
    grep -Fq -- '--providers.kubernetescrd=true' || fail 'Kubernetes CRD provider is disabled'
fi

printf 'Public ingress bootstrap completed: %s\n' "$mode"
