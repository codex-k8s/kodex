#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Management surfaces test failed: %s\n' "$*" >&2; exit 1; }
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
bootstrap="$repository_root/infra/management-surfaces/bootstrap.sh"
routes="$repository_root/infra/management-surfaces/routes.yaml"
values="$repository_root/infra/management-surfaces/oauth2-proxy-values.yaml"
lock="$repository_root/infra/management-surfaces/charts.lock.json"
identity="$repository_root/infra/identity/keycloak.yaml"
keycloak_bootstrap="$repository_root/tools/deploy/configure-keycloak.sh"
headlamp_values="$repository_root/infra/management-surfaces/headlamp-values.yaml"
monitoring_values="$repository_root/infra/management-surfaces/kube-prometheus-stack-values.yaml"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

download_chart() {
  local name=$1 chart repository version expected_sha directory archive
  chart=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .chart' "$lock")
  repository=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .repository' "$lock")
  version=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .version' "$lock")
  expected_sha=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .sha256' "$lock")
  directory="$temporary_directory/charts/$name"
  mkdir -p "$directory"
  helm pull "$chart" --repo "$repository" --version "$version" --destination "$directory" >/dev/null
  archive=$(find "$directory" -maxdepth 1 -type f -name '*.tgz' -print -quit)
  [[ -n "$archive" ]] || fail "chart archive is absent: $name"
  printf '%s  %s\n' "$expected_sha" "$archive" | sha256sum --check --status ||
    fail "chart digest mismatch: $name"
  printf '%s\n' "$archive"
}

validate_headlamp_render() {
  yq -o=json -I=0 '.' "$1" | jq -s -e '
    [.[] | select(.kind == "ClusterRoleBinding" and
      .metadata.name == "kodex-headlamp-admin")] as $bindings |
    ($bindings | length) == 1 and
    $bindings[0].roleRef == {
      apiGroup: "rbac.authorization.k8s.io",
      kind: "ClusterRole",
      name: "cluster-admin"
    } and
    $bindings[0].subjects == [{
      kind: "ServiceAccount",
      name: "kodex-headlamp",
      namespace: "platform-admin"
    }]
  ' >/dev/null
}

validate_grafana_render() {
  yq -o=json -I=0 '.' "$1" | jq -s -e '
    [.[] | select(
      (.kind == "Deployment" or .kind == "StatefulSet") and
      .metadata.namespace == "observability" and
      .metadata.name == "kodex-monitoring-grafana"
    )] as $workloads |
    ($workloads | length) == 1 and $workloads[0].kind == "StatefulSet"
  ' >/dev/null
}

bash -n "$bootstrap"
bash -n "$keycloak_bootstrap"
(
  rollback_arguments=""
  helm() {
    case "$1" in
      status) printf '{"info":{"status":"pending-upgrade"}}\n' ;;
      history)
        printf '[{"revision":1,"status":"superseded"},{"revision":2,"status":"deployed"},{"revision":3,"status":"pending-upgrade"}]\n'
        ;;
      rollback) rollback_arguments="$*" ;;
      *) return 1 ;;
    esac
  }
  source <(sed -n '/^recover_interrupted_helm_release() {$/,/^}$/p' "$bootstrap")
  recover_interrupted_helm_release oauth2-control-center kodex-system
  [[ "$rollback_arguments" == 'rollback oauth2-control-center 2 --namespace kodex-system --wait --timeout 10m' ]]
) || fail 'interrupted Helm release recovery contract is invalid'
routes_apply_line=$(grep -n 'kubectl apply --server-side --field-manager=kodex-management -f "$routes"' \
  "$bootstrap" | cut -d: -f1)
oauth2_upgrade_line=$(grep -n 'helm upgrade --install "oauth2-$surface"' "$bootstrap" | cut -d: -f1)
[[ -n "$routes_apply_line" && -n "$oauth2_upgrade_line" &&
  "$routes_apply_line" -lt "$oauth2_upgrade_line" ]] ||
  fail 'OAuth2 routes and NetworkPolicy are not applied before proxy rollout'
rg -q -- '-s ssoSessionIdleTimeout=28800 -s ssoSessionMaxLifespan=43200' "$keycloak_bootstrap" ||
  fail 'realm SSO lifetime contract is absent'
rg -q -- '-s duplicateEmailsAllowed=false -s verifyEmail=false -s accessTokenLifespan=300' "$keycloak_bootstrap" ||
  fail 'realm access token lifetime is not fixed at 300 seconds'
rg -q '"access\.token\.lifespan":"3600"' "$keycloak_bootstrap" ||
  fail 'Control Center access token lifetime override is absent'
rg -q '\.attributes\."access\.token\.lifespan" == "3600"' "$keycloak_bootstrap" ||
  fail 'Control Center access token lifetime readback is absent'
jq -e '
  .schemaVersion == 1 and (.charts | length) == 3 and
  ([.charts[].name] | sort) == ["headlamp","kube-prometheus-stack","oauth2-proxy"] and
  all(.charts[]; (.sha256 | test("^[a-f0-9]{64}$")))
' "$lock" >/dev/null || fail 'management chart lock is invalid'

headlamp_chart=$(download_chart headlamp)
monitoring_chart=$(download_chart kube-prometheus-stack)
oauth2_chart=$(download_chart oauth2-proxy)
headlamp_render="$temporary_directory/headlamp.yaml"
monitoring_render="$temporary_directory/monitoring.yaml"
rendered_monitoring_values="$temporary_directory/monitoring-values.yaml"
helm template kodex-headlamp "$headlamp_chart" --namespace platform-admin \
  --values "$headlamp_values" >"$headlamp_render"
GRAFANA_ORIGIN=https://grafana.example.test yq '
  (.. | select(tag == "!!str")) |=
    sub("__KODEX_GRAFANA_ORIGIN__"; strenv(GRAFANA_ORIGIN))
' "$monitoring_values" >"$rendered_monitoring_values"
helm template kodex-monitoring "$monitoring_chart" --namespace observability \
  --values "$rendered_monitoring_values" >"$monitoring_render"
validate_headlamp_render "$headlamp_render" || fail 'Headlamp exact binding render is invalid'
validate_grafana_render "$monitoring_render" || fail 'Grafana exact StatefulSet render is invalid'

mkdir -p "$temporary_directory/bin"
cat >"$temporary_directory/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == run ]]
EOF
cat >"$temporary_directory/bin/helm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == pull ]]
chart=${2:-}
destination=""
while (($# > 0)); do
  case "$1" in
    --destination) destination=${2:-}; shift 2 ;;
    *) shift ;;
  esac
done
case "$chart" in
  headlamp) source=${FIXTURE_HEADLAMP_CHART:?} ;;
  kube-prometheus-stack) source=${FIXTURE_MONITORING_CHART:?} ;;
  oauth2-proxy) source=${FIXTURE_OAUTH2_CHART:?} ;;
  *) exit 1 ;;
esac
[[ -n "$destination" ]]
cp -- "$source" "$destination/$(basename -- "$source")"
EOF
cat >"$temporary_directory/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
arguments=" $* "
if [[ "$*" == 'config current-context' ]]; then
  printf 'fixture-context\n'
  exit 0
fi
if [[ "$arguments" == *' apply '* ]]; then
  manifest=""
  while (($# > 0)); do
    case "$1" in
      -f) manifest=${2:-}; shift 2 ;;
      *) shift ;;
    esac
  done
  if [[ -f "$manifest" ]] && grep -q 'oauth2-control-center-exact-paths' "$manifest"; then
    yq -e '
      select(.kind == "NetworkPolicy" and .metadata.name == "oauth2-control-center-exact-paths") |
      .spec.egress[].ports[] | select(.port == 8443 and (.port | tag) == "!!int")
    ' "$manifest" >/dev/null
    yq -e '
      select(.kind == "NetworkPolicy" and .metadata.name == "sso-oauth2-proxy-ingress") |
      .spec.ingress[].ports[] | select(.port == 8443 and (.port | tag) == "!!int")
    ' "$manifest" >/dev/null
  fi
  exit 0
fi
if [[ "$arguments" == *' get service sso -o json '* ]]; then
  printf '{"spec":{"clusterIP":"10.43.99.185","ports":[{"port":443}]}}\n'
  exit 0
fi
if [[ "$arguments" =~ \ rollout\ status\ deployment/(oauth2-control-center|oauth2-grafana|oauth2-headlamp|kodex-headlamp)\  ]]; then
  exit 0
fi
if [[ "$arguments" == *' get deployment oauth2-'*' -o json '* ]]; then
  role=kodex-owner
  [[ "$arguments" != *' get deployment oauth2-headlamp '* ]] || role=admin
  printf '{"spec":{"template":{"spec":{"hostAliases":[{"ip":"10.43.99.185","hostnames":["sso.example.test"]}],"containers":[{"name":"oauth2-proxy","args":["--allowed-role=%s"]}]}}}}\n' "$role"
  exit 0
fi
if [[ "$arguments" == *' get networkpolicy oauth2-'*'-exact-paths -o json '* ]]; then
  printf '%s\n' '{"spec":{"egress":[{"ports":[{"protocol":"UDP","port":53}]},{"ports":[{"protocol":"TCP","port":8443}],"to":[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"identity"}},"podSelector":{"matchLabels":{"app.kubernetes.io/name":"sso","app.kubernetes.io/component":"identity-provider"}}}]}]}}'
  exit 0
fi
if [[ "$arguments" == *' get networkpolicy sso-oauth2-proxy-ingress -o json '* ]]; then
  printf '%s\n' '{"spec":{"podSelector":{"matchLabels":{"app.kubernetes.io/name":"sso","app.kubernetes.io/component":"identity-provider"}},"policyTypes":["Ingress"],"ingress":[{"from":[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"kodex-system"}},"podSelector":{"matchLabels":{"app.kubernetes.io/instance":"oauth2-control-center"}}},{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"observability"}},"podSelector":{"matchLabels":{"app.kubernetes.io/instance":"oauth2-grafana"}}},{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"platform-admin"}},"podSelector":{"matchLabels":{"app.kubernetes.io/instance":"oauth2-headlamp"}}}],"ports":[{"protocol":"TCP","port":8443}]}]}}'
  exit 0
fi
if [[ "$arguments" == *' get clusterrolebinding kodex-headlamp-admin -o json '* ]]; then
  [[ "${FAKE_HEADLAMP_BINDING_NAME:?}" == kodex-headlamp-admin ]] || exit 1
  printf '{"metadata":{"name":"kodex-headlamp-admin"},"roleRef":{"apiGroup":"rbac.authorization.k8s.io","kind":"ClusterRole","name":"%s"},"subjects":[{"kind":"ServiceAccount","name":"kodex-headlamp","namespace":"platform-admin"}]}\n' \
    "${FAKE_HEADLAMP_BINDING_ROLE:?}"
  exit 0
fi
if [[ "$arguments" == *' rollout status statefulset/kodex-monitoring-grafana '* ]]; then
  [[ "${FAKE_GRAFANA_KIND:?}" == StatefulSet ]]
  exit
fi
if [[ "$arguments" == *' rollout status deployment/kodex-monitoring-grafana '* ]]; then
  [[ "${FAKE_GRAFANA_KIND:?}" == Deployment ]]
  exit
fi
if [[ "$arguments" == *' get prometheus -o json '* ||
  "$arguments" == *' get alertmanager -o json '* ]]; then
  printf '{"items":[{"status":{"availableReplicas":1}}]}\n'
  exit 0
fi
if [[ "$arguments" == *' get ingress kodex-grafana -o jsonpath='* ]]; then
  printf 'grafana.example.test'
  exit 0
fi
if [[ "$arguments" == *' get ingress kodex-headlamp -o jsonpath='* ]]; then
  printf 'headlamp.example.test'
  exit 0
fi
if [[ "$arguments" == *' get ingress kodex-grafana -o json '* ]]; then
  printf '{"metadata":{"annotations":{"traefik.ingress.kubernetes.io/router.middlewares":"observability-oauth2-grafana-chain@kubernetescrd"}}}\n'
  exit 0
fi
if [[ "$arguments" == *' get ingress kodex-headlamp -o json '* ]]; then
  printf '{"metadata":{"annotations":{"traefik.ingress.kubernetes.io/router.middlewares":"platform-admin-oauth2-headlamp-chain@kubernetescrd"}}}\n'
  exit 0
fi
if [[ "$arguments" == *' get ingress staff-control-center -o json '* ]]; then
  printf '{"metadata":{"annotations":{"traefik.ingress.kubernetes.io/router.middlewares":"kodex-system-oauth2-control-center-chain@kubernetescrd"}}}\n'
  exit 0
fi
if [[ "$arguments" == *' get service staff-control-center -o json '* ]]; then
  printf '{"metadata":{"annotations":{"traefik.ingress.kubernetes.io/service.serverstransport":"kodex-system-staff-control-center@kubernetescrd"}}}\n'
  exit 0
fi
printf 'Unexpected kubectl fixture call: %s\n' "$*" >&2
exit 1
EOF
chmod 0700 "$temporary_directory/bin/go" "$temporary_directory/bin/helm" \
  "$temporary_directory/bin/kubectl"

readback_arguments=(
  --context fixture-context
  --mode readback
  --oidc-issuer https://sso.example.test/realms/kodex
  --oidc-connect-address sso.identity.svc.cluster.local:443
  --oidc-target-port 8443
  --control-center-host control.example.test
  --grafana-host grafana.example.test
  --headlamp-host headlamp.example.test
  --ingress-class traefik
  --cluster-issuer fixture-issuer
  --ingress-namespace kube-system
  --ingress-pod-name traefik
  --kubernetes-api-service-cidr 10.43.0.1/32
  --kubernetes-api-endpoint-cidrs 192.0.2.20/32
  --kubernetes-api-endpoint-ports 6443
)
run_readback() {
  local binding_name=$1 binding_role=$2 grafana_kind=$3
  PATH="$temporary_directory/bin:$PATH" \
    FIXTURE_HEADLAMP_CHART="$headlamp_chart" \
    FIXTURE_MONITORING_CHART="$monitoring_chart" \
    FIXTURE_OAUTH2_CHART="$oauth2_chart" \
    FAKE_HEADLAMP_BINDING_NAME="$binding_name" \
    FAKE_HEADLAMP_BINDING_ROLE="$binding_role" \
    FAKE_GRAFANA_KIND="$grafana_kind" \
    "$bootstrap" "${readback_arguments[@]}"
}
expect_readback_rejected() {
  local binding_name=$1 binding_role=$2 grafana_kind=$3 expected_error=$4 label=$5
  if run_readback "$binding_name" "$binding_role" "$grafana_kind" \
    >"$temporary_directory/$label.out" 2>"$temporary_directory/$label.err"; then
    fail "management readback accepted $label"
  fi
  grep -Fq "$expected_error" "$temporary_directory/$label.err" ||
    fail "management readback rejected $label for an unexpected reason"
}

run_readback kodex-headlamp-admin cluster-admin StatefulSet >/dev/null ||
  fail 'management readback rejected the exact pinned chart resources'
expect_readback_rejected kodex-headlamp cluster-admin StatefulSet \
  'Headlamp cluster-admin binding mismatch' wrong-binding-name
expect_readback_rejected kodex-headlamp-admin view StatefulSet \
  'Headlamp cluster-admin binding mismatch' wrong-binding-role
expect_readback_rejected kodex-headlamp-admin cluster-admin Deployment \
  'Grafana rollout failed' wrong-grafana-kind

for surface in control-center grafana headlamp; do
  rg -q "oauth2-$surface" "$bootstrap" || fail "OAuth2 surface is absent: $surface"
  MIDDLEWARE_NAME="oauth2-$surface-errors" yq -e '
    select(.kind == "Middleware" and .metadata.name == strenv(MIDDLEWARE_NAME)) |
    .spec.errors.statusRewrites."401" == 302
  ' "$routes" >/dev/null || fail "OAuth2 browser redirect is absent: $surface"
done
for ingress in kodex-grafana kodex-headlamp; do
  INGRESS_NAME="$ingress" yq -e \
    'select(.kind == "Ingress" and .metadata.name == strenv(INGRESS_NAME))' "$routes" >/dev/null ||
    fail "management Ingress is absent: $ingress"
done
kubectl kustomize "$repository_root/deploy/k8s/profiles/web-only" | yq -e \
  'select(.kind == "Ingress" and .metadata.name == "staff-control-center")' >/dev/null ||
  fail 'Control Center Ingress is absent from the platform release'
kubectl kustomize "$repository_root/deploy/k8s/profiles/web-only" | yq -e '
  select(.kind == "Service" and .metadata.name == "staff-control-center") |
  .metadata.annotations["traefik.ingress.kubernetes.io/service.serverstransport"] ==
    "kodex-system-staff-control-center@kubernetescrd"
' >/dev/null || fail 'Control Center Service does not select its TLS ServersTransport'
if kubectl kustomize "$repository_root/deploy/k8s/profiles/web-only" | yq -e '
  select(.kind == "Ingress" and .metadata.name == "staff-control-center") |
  .metadata.annotations | has("traefik.ingress.kubernetes.io/service.serverstransport")
' >/dev/null 2>&1; then
  fail 'Control Center ServersTransport annotation is attached to Ingress instead of Service'
fi
yq -e '.extraArgs."allowed-role" == "__KODEX_ALLOWED_ROLE__"' "$values" >/dev/null ||
  fail 'OAuth2 Proxy role gate is absent'
yq -e '
  (.hostAliases | length) == 1 and
  .hostAliases[0].ip == "__KODEX_OIDC_CONNECT_IP__" and
  (.hostAliases[0].hostnames | length) == 1 and
  .hostAliases[0].hostnames[0] == "__KODEX_OIDC_HOST__"
' "$values" >/dev/null || fail 'OAuth2 Proxy internal OIDC host alias is absent'
for policy in \
  oauth2-control-center-exact-paths \
  oauth2-grafana-exact-paths \
  oauth2-headlamp-exact-paths; do
  POLICY_NAME="$policy" yq -e '
    select(.kind == "NetworkPolicy" and .metadata.name == strenv(POLICY_NAME)) |
    .spec.egress[] |
    select(
      (.ports | length) == 1 and
      .ports[0].protocol == "TCP" and
      .ports[0].port == "__KODEX_OIDC_TARGET_PORT__" and
      (.to | length) == 1 and
      .to[0].namespaceSelector.matchLabels["kubernetes.io/metadata.name"] == "identity" and
      .to[0].podSelector.matchLabels["app.kubernetes.io/name"] == "sso" and
      .to[0].podSelector.matchLabels["app.kubernetes.io/component"] == "identity-provider"
    )
  ' "$routes" >/dev/null || fail "OAuth2 Proxy exact Keycloak egress is absent: $policy"
done
POLICY_NAME=sso-oauth2-proxy-ingress yq -e '
  select(.kind == "NetworkPolicy" and .metadata.name == strenv(POLICY_NAME)) |
  (.spec.ingress[0].from | length) == 3 and
  .spec.ingress[0].ports[0].port == "__KODEX_OIDC_TARGET_PORT__"
' "$routes" >/dev/null || fail 'Keycloak exact OAuth2 Proxy ingress is absent'
yq -e '
  select(.kind == "Service" and .metadata.name == "sso") |
  .metadata.annotations["traefik.ingress.kubernetes.io/service.serverstransport"] ==
    "identity-sso-public@kubernetescrd"
' "$identity" >/dev/null || fail 'SSO Service does not select its TLS ServersTransport'
yq -e '
  select(.kind == "ServersTransport" and .metadata.name == "sso-public") |
  .spec.serverName == "__KODEX_OIDC_HOST__" and .spec.insecureSkipVerify == false
' "$identity" >/dev/null || fail 'SSO ServersTransport does not enforce exact TLS identity'
if rg -qi 'vault' "$repository_root/infra/management-surfaces"; then
  fail 'retired Vault management surface remains active'
fi
printf 'Management surfaces test completed\n'
