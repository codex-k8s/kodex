#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf '%s\n' 'usage: kubernetes-api-egress.sh discover|render|validate|readback|apply --context NAME --namespace NAME --policy NAME --pod-selector KEY=VALUE'
}

command_name="${1:-}"
if [[ -z "$command_name" ]]; then
  usage >&2
  exit 2
fi
shift

kube_context=""
target_namespace=""
policy_name=""
pod_selector=""
while (($#)); do
  case "$1" in
    --context) kube_context="${2:-}"; shift 2 ;;
    --namespace) target_namespace="${2:-}"; shift 2 ;;
    --policy) policy_name="${2:-}"; shift 2 ;;
    --pod-selector) pod_selector="${2:-}"; shift 2 ;;
    *) usage >&2; exit 2 ;;
  esac
done

if [[ -z "$kube_context" || -z "$target_namespace" || -z "$policy_name" ||
      "$pod_selector" != *=* ]]; then
  usage >&2
  exit 2
fi
selector_key="${pod_selector%%=*}"
selector_value="${pod_selector#*=}"
if [[ -z "$selector_key" || -z "$selector_value" || "$selector_key" == "$selector_value" ]]; then
  printf '%s\n' 'pod selector is invalid' >&2
  exit 2
fi

actual_context="$(kubectl config current-context)"
if [[ "$actual_context" != "$kube_context" ]]; then
  printf '%s\n' 'kube context does not match the requested exact context' >&2
  exit 1
fi

discover() {
  local service_json slices_json
  service_json="$(kubectl --context "$kube_context" get service kubernetes -n default -o json)"
  slices_json="$(kubectl --context "$kube_context" get endpointslice -n default \
    -l kubernetes.io/service-name=kubernetes -o json)"
  jq -n --argjson service "$service_json" --argjson slices "$slices_json" '
    ($service.spec.clusterIP // "") as $service_ip |
    ([ $service.spec.ports[] | select(.protocol == "TCP") | .port ] | unique) as $service_ports |
    ([ $slices.items[] | select(.addressType == "IPv4") |
       .endpoints[] | select(.conditions.ready == true) | .addresses[] ] | unique | sort) as $endpoint_ips |
    ([ $slices.items[] | select(.addressType == "IPv4") | .ports[] |
       select((.protocol // "TCP") == "TCP" and .port != null) | .port ] | unique | sort) as $endpoint_ports |
    if ($service_ip | test("^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$")) and
       ($service_ports | length) > 0 and ($endpoint_ips | length) > 0 and ($endpoint_ports | length) > 0
    then {service_ip:$service_ip, service_ports:$service_ports,
          endpoint_ips:$endpoint_ips, endpoint_ports:$endpoint_ports}
    else error("Kubernetes API discovery has no exact ready IPv4 path") end
  '
}

render() {
  local discovery
  discovery="$(discover)"
  jq -n \
    --arg namespace "$target_namespace" \
    --arg policy "$policy_name" \
    --arg selector_key "$selector_key" \
    --arg selector_value "$selector_value" \
    --argjson discovery "$discovery" '
    {
      apiVersion:"networking.k8s.io/v1",
      kind:"NetworkPolicy",
      metadata:{name:$policy, namespace:$namespace,
        labels:{"app.kubernetes.io/managed-by":"kodex-kubernetes-api-egress"}},
      spec:{
        podSelector:{matchLabels:{($selector_key):$selector_value}},
        policyTypes:["Egress"],
        egress:[
          {to:[{ipBlock:{cidr:($discovery.service_ip + "/32")}}],
           ports:[$discovery.service_ports[] | {protocol:"TCP",port:.}]},
          {to:[$discovery.endpoint_ips[] | {ipBlock:{cidr:(. + "/32")}}],
           ports:[$discovery.endpoint_ports[] | {protocol:"TCP",port:.}]}
        ]
      }
    }
  ' | yq -P
}

case "$command_name" in
  discover)
    discover
    ;;
  render)
    render
    ;;
  validate)
    rendered_dir="$(mktemp -d)"
    trap 'rm -rf -- "$rendered_dir"' EXIT
    render >"$rendered_dir/networkpolicy.yaml"
    kubectl --context "$kube_context" apply --server-side --dry-run=server -f "$rendered_dir/networkpolicy.yaml" >/dev/null
    printf '%s\n' 'Kubernetes API egress policy passed server-side validation'
    ;;
  readback)
    rendered_dir="$(mktemp -d)"
    trap 'rm -rf -- "$rendered_dir"' EXIT
    render >"$rendered_dir/networkpolicy.yaml"
    kubectl --context "$kube_context" diff -f "$rendered_dir/networkpolicy.yaml" >/dev/null
    kubectl --context "$kube_context" get networkpolicy "$policy_name" -n "$target_namespace" -o name
    ;;
  apply)
    if [[ "${KODEX_OWNER_APPROVED:-}" != "true" ]]; then
      printf '%s\n' 'apply requires explicit owner approval' >&2
      exit 1
    fi
    rendered_dir="$(mktemp -d)"
    trap 'rm -rf -- "$rendered_dir"' EXIT
    render >"$rendered_dir/networkpolicy.yaml"
    kubectl --context "$kube_context" apply --server-side --field-manager=kodex-kubernetes-api-egress -f "$rendered_dir/networkpolicy.yaml"
    kubectl --context "$kube_context" diff -f "$rendered_dir/networkpolicy.yaml" >/dev/null
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
