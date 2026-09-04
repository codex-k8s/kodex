#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex Teleport route bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode preflight|apply|readback" \
    '  --host <exact-DNS> --backend-address <private-IPv4>' \
    '  --backend-ca-file <path> --ingress-class <name> --cluster-issuer <name>' \
    '  --allowed-ipv4-addresses <comma-list> [--allowed-ipv6-addresses <comma-list>]' \
    '  [--kubernetes-group <name>]' >&2
}

context=""
mode=""
host=""
backend_address=""
backend_ca_file=""
ingress_class=""
cluster_issuer=""
allowed_ipv4_addresses=""
allowed_ipv6_addresses=""
kubernetes_group=kodex-teleport-dev-observers
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    --host) host=${2:-}; shift 2 ;;
    --backend-address) backend_address=${2:-}; shift 2 ;;
    --backend-ca-file) backend_ca_file=${2:-}; shift 2 ;;
    --ingress-class) ingress_class=${2:-}; shift 2 ;;
    --cluster-issuer) cluster_issuer=${2:-}; shift 2 ;;
    --allowed-ipv4-addresses) allowed_ipv4_addresses=${2:-}; shift 2 ;;
    --allowed-ipv6-addresses) allowed_ipv6_addresses=${2:-}; shift 2 ;;
    --kubernetes-group) kubernetes_group=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$mode" in preflight|apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ -n "$context" && "$(kubectl config current-context)" == "$context" ]] ||
  fail 'Kubernetes context mismatch'
[[ "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'production context is forbidden'
for dns_name in "$host" "$ingress_class" "$cluster_issuer"; do
  [[ "$dns_name" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail 'DNS-like argument is invalid'
done
[[ "$host" == *.* ]] || fail 'Teleport host must be an exact DNS name'
[[ "$kubernetes_group" =~ ^[a-z0-9]([a-z0-9:-]*[a-z0-9])?$ ]] ||
  fail 'Teleport Kubernetes group is invalid'
python3 - "$backend_address" <<'PY' || fail 'Teleport backend address must be a private non-loopback IPv4 address'
import ipaddress
import sys

address = ipaddress.ip_address(sys.argv[1])
if address.version != 4 or not address.is_private or address.is_loopback or address.is_unspecified:
    raise SystemExit(1)
PY
[[ "$backend_ca_file" == /* && -f "$backend_ca_file" && ! -L "$backend_ca_file" ]] ||
  fail 'Teleport backend CA file is invalid'
openssl x509 -in "$backend_ca_file" -noout -checkend 2592000 >/dev/null ||
  fail 'Teleport backend CA certificate expires too soon'
for command_name in curl helm jq kubectl openssl python3 yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
if [[ "$mode" != readback ]]; then
  "$repository_root/tools/dev/preflight-public-hosts.sh" --hosts "$host" \
    --allowed-ipv4-addresses "$allowed_ipv4_addresses" \
    --allowed-ipv6-addresses "$allowed_ipv6_addresses" \
    --context "$context" --backend-address "$backend_address"
fi
if [[ "$mode" == preflight ]]; then
  printf 'Kodex Teleport route preflight completed\n'
  exit 0
fi

namespace=teleport
service_name=teleport-host
transport_name=teleport-host
certificate_name=teleport-tls
role_name=kodex-teleport-dev-observer
binding_name=kodex-teleport-dev-observers

render_kubernetes_role() {
  yq -n -o=yaml '
    .apiVersion = "rbac.authorization.k8s.io/v1" |
    .kind = "ClusterRole" |
    .metadata.name = "kodex-teleport-dev-observer" |
    .metadata.labels."app.kubernetes.io/part-of" = "kodex-dev" |
    .rules = [
      {"nonResourceURLs":["/api","/api/*","/apis","/apis/*","/healthz","/livez","/readyz","/version"],"verbs":["get"]},
      {"apiGroups":[""],"resources":["configmaps","endpoints","events","namespaces","nodes","persistentvolumeclaims","persistentvolumes","pods","pods/log","replicationcontrollers","resourcequotas","serviceaccounts","services"],"verbs":["get","list","watch"]},
      {"apiGroups":["apps"],"resources":["daemonsets","deployments","replicasets","statefulsets"],"verbs":["get","list","watch"]},
      {"apiGroups":["batch"],"resources":["cronjobs","jobs"],"verbs":["get","list","watch"]},
      {"apiGroups":["networking.k8s.io"],"resources":["ingresses","networkpolicies"],"verbs":["get","list","watch"]},
      {"apiGroups":["cert-manager.io"],"resources":["certificates","certificaterequests","clusterissuers","issuers"],"verbs":["get","list","watch"]},
      {"apiGroups":["apiextensions.k8s.io"],"resources":["customresourcedefinitions"],"verbs":["get","list","watch"]},
      {"apiGroups":["admissionregistration.k8s.io"],"resources":["mutatingwebhookconfigurations","validatingadmissionpolicies","validatingadmissionpolicybindings","validatingwebhookconfigurations"],"verbs":["get","list","watch"]}
    ]
  '
}

if [[ "$mode" == apply ]]; then
  if helm --namespace "$namespace" status teleport >/dev/null 2>&1; then
    helm --namespace "$namespace" uninstall teleport --wait --timeout 5m >/dev/null
  fi
  kubectl create namespace "$namespace" --dry-run=client -o yaml |
    kubectl apply --server-side --field-manager=kodex-teleport-route -f - >/dev/null
  kubectl -n "$namespace" delete deployment,statefulset,service,ingress \
    -l app.kubernetes.io/instance=teleport --ignore-not-found --wait=true --timeout=5m >/dev/null

  HOST="$host" CLUSTER_ISSUER="$cluster_issuer" yq -n -o=yaml '
    .apiVersion = "cert-manager.io/v1" |
    .kind = "Certificate" |
    .metadata.name = "teleport-tls" |
    .metadata.namespace = "teleport" |
    .metadata.labels."app.kubernetes.io/part-of" = "kodex-dev" |
    .spec.secretName = "teleport-tls" |
    .spec.dnsNames = [strenv(HOST)] |
    .spec.issuerRef.name = strenv(CLUSTER_ISSUER) |
    .spec.issuerRef.kind = "ClusterIssuer" |
    .spec.issuerRef.group = "cert-manager.io"
  ' | kubectl apply --server-side --field-manager=kodex-teleport-route -f - >/dev/null
  kubectl -n "$namespace" wait --for=condition=Ready "certificate/$certificate_name" \
    --timeout=10m >/dev/null || fail 'Teleport public certificate is not ready'

  kubectl -n "$namespace" create secret generic teleport-host-internal-ca \
    --from-file=ca.crt="$backend_ca_file" --dry-run=client -o yaml |
    kubectl apply --server-side --field-manager=kodex-teleport-route -f - >/dev/null

  yq -n -o=yaml '
      .apiVersion = "v1" |
      .kind = "Service" |
      .metadata.name = "teleport-host" |
      .metadata.namespace = "teleport" |
      .metadata.labels."app.kubernetes.io/name" = "teleport-host-route" |
      .metadata.labels."app.kubernetes.io/part-of" = "kodex-dev" |
      .metadata.annotations."traefik.ingress.kubernetes.io/service.serverstransport" =
        "teleport-teleport-host@kubernetescrd" |
      .spec.ports = [{"name":"https","port":443,"protocol":"TCP","targetPort":3080}]
    ' | kubectl apply --server-side --field-manager=kodex-teleport-route -f - >/dev/null
  BACKEND_ADDRESS="$backend_address" yq -n -o=yaml '
      .apiVersion = "discovery.k8s.io/v1" |
      .kind = "EndpointSlice" |
      .metadata.name = "teleport-host" |
      .metadata.namespace = "teleport" |
      .metadata.labels."kubernetes.io/service-name" = "teleport-host" |
      .metadata.labels."app.kubernetes.io/part-of" = "kodex-dev" |
      .addressType = "IPv4" |
      .ports = [{"name":"https","protocol":"TCP","port":3080}] |
      .endpoints = [{"addresses":[strenv(BACKEND_ADDRESS)],"conditions":{"ready":true}}]
    ' | kubectl apply --server-side --field-manager=kodex-teleport-route -f - >/dev/null
  HOST="$host" yq -n -o=yaml '
      .apiVersion = "traefik.io/v1alpha1" |
      .kind = "ServersTransport" |
      .metadata.name = "teleport-host" |
      .metadata.namespace = "teleport" |
      .spec.serverName = strenv(HOST) |
      .spec.rootCAsSecrets = ["teleport-host-internal-ca"] |
      .spec.insecureSkipVerify = false
    ' | kubectl apply --server-side --field-manager=kodex-teleport-route -f - >/dev/null
  HOST="$host" INGRESS_CLASS="$ingress_class" yq -n -o=yaml '
      .apiVersion = "networking.k8s.io/v1" |
      .kind = "Ingress" |
      .metadata.name = "teleport-host" |
      .metadata.namespace = "teleport" |
      .metadata.labels."app.kubernetes.io/part-of" = "kodex-dev" |
      .metadata.annotations."traefik.ingress.kubernetes.io/router.entrypoints" = "websecure" |
      .metadata.annotations."traefik.ingress.kubernetes.io/service.serversscheme" = "https" |
      .spec.ingressClassName = strenv(INGRESS_CLASS) |
      .spec.tls = [{"hosts":[strenv(HOST)],"secretName":"teleport-tls"}] |
      .spec.rules = [{
        "host":strenv(HOST),
        "http":{"paths":[{"path":"/","pathType":"Prefix","backend":{"service":{"name":"teleport-host","port":{"number":443}}}}]}
      }]
    ' | kubectl apply --server-side --field-manager=kodex-teleport-route -f - >/dev/null

  render_kubernetes_role |
    kubectl apply --server-side --field-manager=kodex-teleport-route -f - >/dev/null
  KUBERNETES_GROUP="$kubernetes_group" yq -n -o=yaml '
      .apiVersion = "rbac.authorization.k8s.io/v1" |
      .kind = "ClusterRoleBinding" |
      .metadata.name = "kodex-teleport-dev-observers" |
      .metadata.labels."app.kubernetes.io/part-of" = "kodex-dev" |
      .roleRef = {"apiGroup":"rbac.authorization.k8s.io","kind":"ClusterRole","name":"kodex-teleport-dev-observer"} |
      .subjects = [{"apiGroup":"rbac.authorization.k8s.io","kind":"Group","name":strenv(KUBERNETES_GROUP)}]
    ' | kubectl apply --server-side --field-manager=kodex-teleport-route -f - >/dev/null
fi

kubectl -n "$namespace" get certificate "$certificate_name" -o json | jq -e '
  any(.status.conditions[]?; .type == "Ready" and .status == "True")
' >/dev/null || fail 'Teleport certificate readback failed'
kubectl -n "$namespace" get service "$service_name" -o json | jq -e '
  .spec.clusterIP != null and
  ([.spec.ports[] | select(.name == "https" and .port == 443 and .targetPort == 3080)] | length) == 1 and
  .metadata.annotations["traefik.ingress.kubernetes.io/service.serverstransport"] ==
    "teleport-teleport-host@kubernetescrd"
' >/dev/null || fail 'Teleport host Service readback failed'
kubectl -n "$namespace" get endpointslice "$service_name" -o json | jq -e \
  --arg backend "$backend_address" '
    .addressType == "IPv4" and
    [.endpoints[] | select(.conditions.ready == true) | .addresses[]] == [$backend] and
    [.ports[] | select(.name == "https" and .protocol == "TCP") | .port] == [3080]
  ' >/dev/null || fail 'Teleport host EndpointSlice readback failed'
kubectl -n "$namespace" get serverstransport "$transport_name" -o json | jq -e \
  --arg host "$host" '
    .spec.serverName == $host and
    .spec.rootCAsSecrets == ["teleport-host-internal-ca"] and
    .spec.insecureSkipVerify == false
  ' >/dev/null || fail 'Teleport backend TLS identity readback failed'
kubectl -n "$namespace" get ingress "$service_name" -o json | jq -e \
  --arg host "$host" --arg ingress_class "$ingress_class" '
    .spec.ingressClassName == $ingress_class and
    .spec.rules[0].host == $host and
    .spec.tls[0].secretName == "teleport-tls" and
    .spec.rules[0].http.paths[0].backend.service.name == "teleport-host"
  ' >/dev/null || fail 'Teleport host Ingress readback failed'
expected_role=$(render_kubernetes_role | yq -o=json -I=0 '.')
actual_role=$(kubectl get clusterrole "$role_name" -o json)
jq -n -e --argjson expected "$expected_role" --argjson actual "$actual_role" '
  def normalize_rule:
    {
      apiGroups: ((.apiGroups // []) | sort),
      nonResourceURLs: ((.nonResourceURLs // []) | sort),
      resourceNames: ((.resourceNames // []) | sort),
      resources: ((.resources // []) | sort),
      verbs: ((.verbs // []) | sort)
    } | with_entries(select((.value | length) > 0));
  def normalize_rules: [.rules[] | normalize_rule] | sort_by(tojson);
  ($actual | normalize_rules) == ($expected | normalize_rules) and
  ($actual.metadata.labels["app.kubernetes.io/part-of"] == "kodex-dev")
' >/dev/null || fail 'Teleport Kubernetes role differs from the exact bounded read-only profile'
kubectl get clusterrolebinding "$binding_name" -o json | jq -e \
  --arg group "$kubernetes_group" '
    .roleRef == {"apiGroup":"rbac.authorization.k8s.io","kind":"ClusterRole","name":"kodex-teleport-dev-observer"} and
    .subjects == [{"apiGroup":"rbac.authorization.k8s.io","kind":"Group","name":$group}]
  ' >/dev/null || fail 'Teleport Kubernetes group binding readback failed'
curl --proto '=https' --tlsv1.2 --fail --silent --show-error \
  "https://$host/webapi/ping" | jq -e '.auth.type == "github"' >/dev/null ||
  fail 'Teleport public GitHub authentication endpoint readback failed'
printf 'Kodex Teleport route bootstrap completed: %s\n' "$mode"
