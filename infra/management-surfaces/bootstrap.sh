#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Management surfaces bootstrap failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode preflight|apply-monitoring|apply-surfaces|readback|reconcile" \
    '  --oidc-issuer <https-url> --oidc-connect-address <service.namespace.svc.cluster.local:port>' \
    '  --oidc-target-port <port> --control-center-host <dns> --grafana-host <dns>' \
    '  --headlamp-host <dns>' \
    '  --ingress-class <name> --cluster-issuer <name> --ingress-namespace <name>' \
    '  --ingress-pod-name <label> --kubernetes-api-service-cidr <host-cidr>' \
    '  --kubernetes-api-endpoint-cidrs <host-cidr[,host-cidr...]>' \
    '  --kubernetes-api-endpoint-ports <port[,port...]>' >&2
}

context=""
mode=""
oidc_issuer=""
oidc_connect_address=""
oidc_target_port=""
control_center_host=""
grafana_host=""
headlamp_host=""
ingress_class=""
cluster_issuer=""
ingress_namespace=""
ingress_pod_name=""
kubernetes_api_service_cidr=""
kubernetes_api_endpoint_cidrs=""
kubernetes_api_endpoint_ports=""
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --oidc-issuer) oidc_issuer="${2:-}"; shift 2 ;;
    --oidc-connect-address) oidc_connect_address="${2:-}"; shift 2 ;;
    --oidc-target-port) oidc_target_port="${2:-}"; shift 2 ;;
    --control-center-host) control_center_host="${2:-}"; shift 2 ;;
    --grafana-host) grafana_host="${2:-}"; shift 2 ;;
    --headlamp-host) headlamp_host="${2:-}"; shift 2 ;;
    --ingress-class) ingress_class="${2:-}"; shift 2 ;;
    --cluster-issuer) cluster_issuer="${2:-}"; shift 2 ;;
    --ingress-namespace) ingress_namespace="${2:-}"; shift 2 ;;
    --ingress-pod-name) ingress_pod_name="${2:-}"; shift 2 ;;
    --kubernetes-api-service-cidr) kubernetes_api_service_cidr="${2:-}"; shift 2 ;;
    --kubernetes-api-endpoint-cidrs) kubernetes_api_endpoint_cidrs="${2:-}"; shift 2 ;;
    --kubernetes-api-endpoint-ports) kubernetes_api_endpoint_ports="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'exact context is required'
case "$mode" in preflight|apply-monitoring|apply-surfaces|readback|reconcile) ;; *) fail 'mode is invalid' ;; esac
[[ "$oidc_issuer" =~ ^https://[a-zA-Z0-9._:-]+/realms/[a-zA-Z0-9._-]+$ ]] || fail 'OIDC issuer is invalid'
[[ "$oidc_connect_address" =~ ^([a-z0-9]([a-z0-9-]*[a-z0-9])?)\.([a-z0-9]([a-z0-9-]*[a-z0-9])?)\.svc\.cluster\.local:([1-9][0-9]{0,4})$ ]] ||
  fail 'OIDC connect address must identify an exact in-cluster Service'
oidc_service_name=${BASH_REMATCH[1]}
oidc_service_namespace=${BASH_REMATCH[3]}
oidc_service_port=${BASH_REMATCH[5]}
((10#$oidc_service_port <= 65535)) || fail 'OIDC connect Service port is invalid'
if [[ ! "$oidc_target_port" =~ ^[1-9][0-9]{0,4}$ ]] || ((10#$oidc_target_port > 65535)); then
  fail 'OIDC target port is invalid'
fi
for host in "$control_center_host" "$grafana_host" "$headlamp_host"; do
  [[ "$host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$host" == *.* ]] || fail 'surface host is invalid'
done
oidc_origin=${oidc_issuer%/realms/*}
oidc_host=${oidc_origin#https://}
host_count=$(printf '%s\n' "$oidc_host" "$control_center_host" "$grafana_host" "$headlamp_host" |
  sort -u | wc -l)
[[ "$host_count" -eq 4 ]] || fail 'OIDC and management hosts must be unique'
for value in "$ingress_class" "$cluster_issuer" "$ingress_namespace" "$ingress_pod_name"; do
  [[ "$value" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail 'deployment selector is invalid'
done
for command_name in go helm jq kubectl sha256sum yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'current Kubernetes context mismatch'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
validator="$repository_root/tools/release/validate-host-cidr.go"
go run "$validator" "$kubernetes_api_service_cidr" >/dev/null || fail 'Kubernetes API Service CIDR is invalid'
oidc_connect_ip=$(kubectl -n "$oidc_service_namespace" get service "$oidc_service_name" -o json | jq -er \
  --argjson service_port "$oidc_service_port" '
    select(.spec.clusterIP != null and .spec.clusterIP != "None") |
    select(any(.spec.ports[]; .port == $service_port)) |
    .spec.clusterIP
  ') || fail 'OIDC connect Service or port is unavailable'
go run "$validator" "$oidc_connect_ip/32" >/dev/null || fail 'OIDC connect Service does not have an IPv4 ClusterIP'
IFS=',' read -r -a api_endpoint_cidrs <<<"$kubernetes_api_endpoint_cidrs"
IFS=',' read -r -a api_endpoint_ports <<<"$kubernetes_api_endpoint_ports"
((${#api_endpoint_cidrs[@]} >= 1 && ${#api_endpoint_cidrs[@]} <= 16)) || fail 'Kubernetes API endpoint CIDR count is invalid'
((${#api_endpoint_ports[@]} >= 1 && ${#api_endpoint_ports[@]} <= 8)) || fail 'Kubernetes API endpoint port count is invalid'
for cidr in "${api_endpoint_cidrs[@]}"; do go run "$validator" "$cidr" >/dev/null || fail 'Kubernetes API endpoint CIDR is invalid'; done
for port in "${api_endpoint_ports[@]}"; do
  if [[ ! "$port" =~ ^[1-9][0-9]{0,4}$ ]] || ((10#$port > 65535)); then
    fail 'Kubernetes API endpoint port is invalid'
  fi
done

lock_file="$script_directory/charts.lock.json"
jq -e '
  .schemaVersion == 1 and (.charts | length) == 3 and
  ([.charts[].name] | unique | length) == 3 and
  all(.charts[];
    (.name | test("^[a-z0-9-]+$")) and (.chart | test("^[a-z0-9-]+$")) and
    (.version | test("^[0-9]+\\.[0-9]+\\.[0-9]+$")) and (.sha256 | test("^[a-f0-9]{64}$")))
' "$lock_file" >/dev/null || fail 'management chart lock is invalid'

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
download_chart() {
  local name=$1 chart repository version expected_sha directory archive
  chart=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .chart' "$lock_file")
  repository=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .repository' "$lock_file")
  version=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .version' "$lock_file")
  expected_sha=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .sha256' "$lock_file")
  directory="$temporary_directory/charts/$name"
  mkdir -p "$directory"
  helm pull "$chart" --repo "$repository" --version "$version" --destination "$directory" >/dev/null
  archive=$(find "$directory" -maxdepth 1 -type f -name '*.tgz' -print -quit)
  [[ -n "$archive" ]] || fail "chart archive is absent: $name"
  printf '%s  %s\n' "$expected_sha" "$archive" | sha256sum --check --status || fail "chart digest mismatch: $name"
  printf '%s\n' "$archive"
}

recover_interrupted_helm_release() {
  local release=$1 namespace=$2 status_json status rollback_revision
  if ! status_json=$(helm status "$release" --namespace "$namespace" -o json 2>/dev/null); then
    return 0
  fi
  status=$(jq -er '.info.status' <<<"$status_json") ||
    fail "Helm release status is unreadable: $release"
  case "$status" in
    pending-upgrade|pending-rollback)
      rollback_revision=$(helm history "$release" --namespace "$namespace" --max 20 -o json |
        jq -er '
          [.[] | select(.status == "deployed" or .status == "superseded")] |
          sort_by(.revision | tonumber) | last | .revision | tostring
        ') || fail "safe Helm rollback revision is absent: $release"
      helm rollback "$release" "$rollback_revision" --namespace "$namespace" \
        --wait --timeout 10m >/dev/null || fail "Helm rollback failed: $release"
      ;;
    pending-install|pending-uninstall)
      fail "Helm release requires explicit recovery: $release ($status)"
      ;;
  esac
}

monitoring_chart=$(download_chart kube-prometheus-stack)
oauth2_chart=$(download_chart oauth2-proxy)
headlamp_chart=$(download_chart headlamp)

routes="$temporary_directory/routes.yaml"
OIDC_TARGET_PORT="$oidc_target_port" INGRESS_CLASS="$ingress_class" CLUSTER_ISSUER="$cluster_issuer" \
INGRESS_NAMESPACE="$ingress_namespace" INGRESS_POD_NAME="$ingress_pod_name" \
GRAFANA_HOST="$grafana_host" HEADLAMP_HOST="$headlamp_host" \
KUBERNETES_API_SERVICE_CIDR="$kubernetes_api_service_cidr" yq '
  (.. | select(tag == "!!str")) |= (
    sub("__KODEX_OIDC_TARGET_PORT__"; strenv(OIDC_TARGET_PORT)) |
    sub("__KODEX_INGRESS_CLASS__"; strenv(INGRESS_CLASS)) |
    sub("__KODEX_CLUSTER_ISSUER__"; strenv(CLUSTER_ISSUER)) |
    sub("__KODEX_INGRESS_NAMESPACE__"; strenv(INGRESS_NAMESPACE)) |
    sub("__KODEX_INGRESS_POD_NAME__"; strenv(INGRESS_POD_NAME)) |
    sub("__KODEX_GRAFANA_HOST__"; strenv(GRAFANA_HOST)) |
    sub("__KODEX_HEADLAMP_HOST__"; strenv(HEADLAMP_HOST)) |
    sub("__KODEX_KUBERNETES_API_SERVICE_CIDR__"; strenv(KUBERNETES_API_SERVICE_CIDR))
  )
' "$script_directory/routes.yaml" >"$routes"
OIDC_TARGET_PORT="$oidc_target_port" yq -i '
  with(select(.kind == "NetworkPolicy" and
    ((.metadata.name | test("^oauth2-.+-exact-paths$")) or
      .metadata.name == "sso-oauth2-proxy-ingress"));
    (.spec.egress[]?.ports[]? | select(.port == strenv(OIDC_TARGET_PORT)).port) =
      (strenv(OIDC_TARGET_PORT) | tonumber) |
    (.spec.ingress[]?.ports[]? | select(.port == strenv(OIDC_TARGET_PORT)).port) =
      (strenv(OIDC_TARGET_PORT) | tonumber))
' "$routes"
endpoint_destinations=$(printf '%s\n' "${api_endpoint_cidrs[@]}" | jq -Rsc 'split("\n") | map(select(length > 0) | {ipBlock:{cidr:.}})')
endpoint_ports=$(printf '%s\n' "${api_endpoint_ports[@]}" | jq -Rsc 'split("\n") | map(select(length > 0) | {protocol:"TCP",port:tonumber})')
endpoint_rule=$(jq -cn --argjson to "$endpoint_destinations" --argjson ports "$endpoint_ports" '{to:$to,ports:$ports}')
KUBERNETES_API_ENDPOINT_RULE="$endpoint_rule" yq -i '
  with(select(.kind == "NetworkPolicy" and
    .metadata.namespace == "platform-admin" and .metadata.name == "headlamp-exact-paths");
    .spec.egress += [(strenv(KUBERNETES_API_ENDPOINT_RULE) | from_json)])
' "$routes"
! grep -Eq '__KODEX_[A-Z0-9_]+__' "$routes" || fail 'management route render contains placeholders'
kubectl apply --dry-run=client --validate=false -f "$routes" >/dev/null

render_monitoring_values="$temporary_directory/monitoring-values.yaml"
GRAFANA_ORIGIN="https://$grafana_host" yq '
  (.. | select(tag == "!!str")) |= sub("__KODEX_GRAFANA_ORIGIN__"; strenv(GRAFANA_ORIGIN))
' "$script_directory/kube-prometheus-stack-values.yaml" >"$render_monitoring_values"

render_oauth_values() {
  local surface=$1 host=$2 role=$3 issuer=$4 output=$5
  local cookie_name="_kodex_${surface//-/_}_oauth2" tls_secret
  case "$surface" in
    control-center) tls_secret=staff-control-center-public-tls ;;
    grafana) tls_secret=kodex-grafana-public-tls ;;
    headlamp) tls_secret=kodex-headlamp-public-tls ;;
    *) fail 'unsupported OAuth2 surface' ;;
  esac
  OAUTH2_SECRET="oauth2-$surface" OAUTH2_COOKIE_NAME="$cookie_name" OIDC_ISSUER="$issuer" \
  OIDC_CONNECT_IP="$oidc_connect_ip" OIDC_HOST="$oidc_host" \
  SURFACE_HOST="$host" SURFACE_ORIGIN="https://$host" ALLOWED_ROLE="$role" \
  INGRESS_CLASS="$ingress_class" SURFACE_TLS_SECRET="$tls_secret" yq '
    .fullnameOverride = strenv(OAUTH2_SECRET) |
    (.. | select(tag == "!!str")) |= (
      sub("__KODEX_OAUTH2_SECRET__"; strenv(OAUTH2_SECRET)) |
      sub("__KODEX_OAUTH2_COOKIE_NAME__"; strenv(OAUTH2_COOKIE_NAME)) |
      sub("__KODEX_OIDC_ISSUER__"; strenv(OIDC_ISSUER)) |
      sub("__KODEX_OIDC_CONNECT_IP__"; strenv(OIDC_CONNECT_IP)) |
      sub("__KODEX_OIDC_HOST__"; strenv(OIDC_HOST)) |
      sub("__KODEX_SURFACE_HOST__"; strenv(SURFACE_HOST)) |
      sub("__KODEX_SURFACE_ORIGIN__"; strenv(SURFACE_ORIGIN)) |
      sub("__KODEX_ALLOWED_ROLE__"; strenv(ALLOWED_ROLE)) |
      sub("__KODEX_INGRESS_CLASS__"; strenv(INGRESS_CLASS)) |
      sub("__KODEX_SURFACE_TLS_SECRET__"; strenv(SURFACE_TLS_SECRET))
    )
  ' "$script_directory/oauth2-proxy-values.yaml" >"$output"
}

if [[ "$mode" == preflight || "$mode" == reconcile ]]; then
  helm template kodex-monitoring "$monitoring_chart" --namespace observability --values "$render_monitoring_values" >/dev/null
  for binding in \
    "control-center|kodex-system|$control_center_host|kodex-owner|$oidc_issuer" \
    "grafana|observability|$grafana_host|kodex-owner|$oidc_issuer" \
    "headlamp|platform-admin|$headlamp_host|admin|$oidc_origin/realms/master"; do
    IFS='|' read -r surface namespace host role issuer <<<"$binding"
    values="$temporary_directory/oauth-$surface.yaml"
    render_oauth_values "$surface" "$host" "$role" "$issuer" "$values"
    helm template "oauth2-$surface" "$oauth2_chart" --namespace "$namespace" --values "$values" >/dev/null
  done
  helm template kodex-headlamp "$headlamp_chart" --namespace platform-admin --values "$script_directory/headlamp-values.yaml" >/dev/null
  if [[ "$mode" == preflight ]]; then
    printf 'Management surfaces preflight completed\n'
    exit 0
  fi
fi

kubectl apply --server-side --field-manager=kodex-management -f "$script_directory/namespaces.yaml" >/dev/null
if [[ "$mode" == apply-monitoring || "$mode" == reconcile ]]; then
  kubectl -n observability get secret grafana-admin >/dev/null 2>&1 || fail 'Grafana admin Secret is absent'
  recover_interrupted_helm_release kodex-monitoring observability
  helm upgrade --install kodex-monitoring "$monitoring_chart" --namespace observability \
    --values "$render_monitoring_values" --atomic --wait --timeout 20m
fi

if [[ "$mode" == apply-surfaces || "$mode" == reconcile ]]; then
  for binding in control-center:kodex-system grafana:observability headlamp:platform-admin; do
    surface=${binding%%:*}; namespace=${binding#*:}
    kubectl -n "$namespace" get secret "oauth2-$surface" >/dev/null 2>&1 || fail "OAuth2 Secret is absent: $surface"
  done
  recover_interrupted_helm_release kodex-headlamp platform-admin
  helm upgrade --install kodex-headlamp "$headlamp_chart" --namespace platform-admin \
    --values "$script_directory/headlamp-values.yaml" --atomic --wait --timeout 10m
  # Новые OAuth2 Proxy должны получить точный путь к issuer до OIDC discovery
  # на старте; Helm не сможет завершить rollout под прежней egress policy.
  kubectl apply --server-side --field-manager=kodex-management -f "$routes" >/dev/null
  for binding in \
    "control-center|kodex-system|$control_center_host|kodex-owner|$oidc_issuer" \
    "grafana|observability|$grafana_host|kodex-owner|$oidc_issuer" \
    "headlamp|platform-admin|$headlamp_host|admin|$oidc_origin/realms/master"; do
    IFS='|' read -r surface namespace host role issuer <<<"$binding"
    values="$temporary_directory/oauth-$surface.yaml"
    render_oauth_values "$surface" "$host" "$role" "$issuer" "$values"
    recover_interrupted_helm_release "oauth2-$surface" "$namespace"
    helm upgrade --install "oauth2-$surface" "$oauth2_chart" --namespace "$namespace" \
      --values "$values" --atomic --wait --timeout 10m
  done
fi

if [[ "$mode" == readback || "$mode" == reconcile ]]; then
  for binding in \
    oauth2-control-center:kodex-system:kodex-owner \
    oauth2-grafana:observability:kodex-owner \
    oauth2-headlamp:platform-admin:admin; do
    IFS=: read -r deployment namespace role <<<"$binding"
    kubectl -n "$namespace" rollout status "deployment/$deployment" --timeout=3m >/dev/null || fail "OAuth2 Proxy rollout failed: $deployment"
    kubectl -n "$namespace" get deployment "$deployment" -o json | jq -e \
      --arg role "--allowed-role=$role" --arg oidc_host "$oidc_host" --arg oidc_ip "$oidc_connect_ip" '
      any(.spec.template.spec.containers[]; .name == "oauth2-proxy" and (.args | index($role)) != null) and
      .spec.template.spec.hostAliases == [{ip:$oidc_ip, hostnames:[$oidc_host]}]
    ' >/dev/null || fail "OAuth2 Proxy role gate mismatch: $deployment"
  done
  for binding in \
    oauth2-control-center-exact-paths:kodex-system \
    oauth2-grafana-exact-paths:observability \
    oauth2-headlamp-exact-paths:platform-admin; do
    IFS=: read -r policy namespace <<<"$binding"
    kubectl -n "$namespace" get networkpolicy "$policy" -o json | jq -e \
      --argjson target_port "$oidc_target_port" '
      any(.spec.egress[];
        .ports == [{protocol:"TCP",port:$target_port}] and
        .to == [{
          namespaceSelector:{matchLabels:{"kubernetes.io/metadata.name":"identity"}},
          podSelector:{matchLabels:{
            "app.kubernetes.io/name":"sso",
            "app.kubernetes.io/component":"identity-provider"
          }}
        }]
      )
    ' >/dev/null || fail "OAuth2 Proxy OIDC egress mismatch: $policy"
  done
  kubectl -n identity get networkpolicy sso-oauth2-proxy-ingress -o json | jq -e \
    --argjson target_port "$oidc_target_port" '
    .spec.podSelector.matchLabels == {
      "app.kubernetes.io/name":"sso",
      "app.kubernetes.io/component":"identity-provider"
    } and
    .spec.policyTypes == ["Ingress"] and
    .spec.ingress == [{
      from:[
        {
          namespaceSelector:{matchLabels:{"kubernetes.io/metadata.name":"kodex-system"}},
          podSelector:{matchLabels:{"app.kubernetes.io/instance":"oauth2-control-center"}}
        },
        {
          namespaceSelector:{matchLabels:{"kubernetes.io/metadata.name":"observability"}},
          podSelector:{matchLabels:{"app.kubernetes.io/instance":"oauth2-grafana"}}
        },
        {
          namespaceSelector:{matchLabels:{"kubernetes.io/metadata.name":"platform-admin"}},
          podSelector:{matchLabels:{"app.kubernetes.io/instance":"oauth2-headlamp"}}
        }
      ],
      ports:[{protocol:"TCP",port:$target_port}]
    }]
  ' >/dev/null || fail 'Keycloak OAuth2 Proxy ingress mismatch'
  kubectl -n platform-admin rollout status deployment/kodex-headlamp --timeout=3m >/dev/null || fail 'Headlamp rollout failed'
  kubectl get clusterrolebinding kodex-headlamp-admin -o json | jq -e '
    .metadata.name == "kodex-headlamp-admin" and
    .roleRef == {
      apiGroup: "rbac.authorization.k8s.io",
      kind: "ClusterRole",
      name: "cluster-admin"
    } and
    .subjects == [{
      kind: "ServiceAccount",
      name: "kodex-headlamp",
      namespace: "platform-admin"
    }]
  ' >/dev/null || fail 'Headlamp cluster-admin binding mismatch'
  kubectl -n observability rollout status statefulset/kodex-monitoring-grafana --timeout=3m >/dev/null || fail 'Grafana rollout failed'
  kubectl -n observability get prometheus -o json | jq -e '
    (.items | length) == 1 and (.items[0].status.availableReplicas // 0) >= 1
  ' >/dev/null || fail 'Prometheus readback failed'
  kubectl -n observability get alertmanager -o json | jq -e '
    (.items | length) == 1 and (.items[0].status.availableReplicas // 0) >= 1
  ' >/dev/null || fail 'Alertmanager readback failed'
  for binding in kodex-grafana:observability:"$grafana_host" kodex-headlamp:platform-admin:"$headlamp_host"; do
    IFS=: read -r ingress namespace host <<<"$binding"
    [[ "$(kubectl -n "$namespace" get ingress "$ingress" -o jsonpath='{.spec.rules[0].host}')" == "$host" ]] || fail "surface Ingress mismatch: $ingress"
    kubectl -n "$namespace" get ingress "$ingress" -o json | jq -e '
      .metadata.annotations["traefik.ingress.kubernetes.io/router.middlewares"] | test("oauth2-.+-chain@kubernetescrd$")
    ' >/dev/null || fail "surface OAuth2 middleware is absent: $ingress"
  done
  kubectl -n kodex-system get ingress staff-control-center -o json | jq -e '
    .metadata.annotations["traefik.ingress.kubernetes.io/router.middlewares"] ==
      "kodex-system-oauth2-control-center-chain@kubernetescrd"
  ' >/dev/null || fail 'Control Center OAuth2 middleware is absent'
  kubectl -n kodex-system get ingress staff-control-center-api -o json | jq -e '
    .metadata.annotations["traefik.ingress.kubernetes.io/router.middlewares"] ==
      "kodex-system-oauth2-control-center-auth@kubernetescrd" and
    .metadata.annotations["traefik.ingress.kubernetes.io/router.priority"] == "200" and
    .spec.rules[0].http.paths == [{
      path:"/api/v1",
      pathType:"Prefix",
      backend:{service:{name:"staff-control-center",port:{name:"https"}}}
    }]
  ' >/dev/null || fail 'Control Center API must preserve application authorization responses'
  kubectl -n kodex-system get service staff-control-center -o json | jq -e '
    .metadata.annotations["traefik.ingress.kubernetes.io/service.serverstransport"] ==
      "kodex-system-staff-control-center@kubernetescrd"
  ' >/dev/null || fail 'Control Center Service does not select its TLS ServersTransport'
fi

printf 'Management surfaces bootstrap completed: %s\n' "$mode"
