#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex Teleport bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode preflight|apply|readback" \
    '  --host <exact-DNS> --ingress-class <name> --cluster-issuer <name>' \
    '  --allowed-ipv4-addresses <comma-list> [--allowed-ipv6-addresses <comma-list>]' \
    '  [--kube-cluster-name <name>]' \
    '  [--github-client-id-file <path> --github-client-secret-file <path>' \
    '   --github-organization <name> --github-team <slug>]' >&2
}

context=""
mode=""
host=""
ingress_class=""
cluster_issuer=""
allowed_ipv4_addresses=""
allowed_ipv6_addresses=""
kube_cluster_name=kodex-dev
github_client_id_file=""
github_client_secret_file=""
github_organization=""
github_team=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    --host) host=${2:-}; shift 2 ;;
    --ingress-class) ingress_class=${2:-}; shift 2 ;;
    --cluster-issuer) cluster_issuer=${2:-}; shift 2 ;;
    --allowed-ipv4-addresses) allowed_ipv4_addresses=${2:-}; shift 2 ;;
    --allowed-ipv6-addresses) allowed_ipv6_addresses=${2:-}; shift 2 ;;
    --kube-cluster-name) kube_cluster_name=${2:-}; shift 2 ;;
    --github-client-id-file) github_client_id_file=${2:-}; shift 2 ;;
    --github-client-secret-file) github_client_secret_file=${2:-}; shift 2 ;;
    --github-organization) github_organization=${2:-}; shift 2 ;;
    --github-team) github_team=${2:-}; shift 2 ;;
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
[[ "$kube_cluster_name" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
  fail 'Teleport Kubernetes cluster name is invalid'
for command_name in curl helm jq kubectl sha256sum yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
lock_file="$repository_root/tools/install/components.lock.json"
version=$(jq -er '.charts[] | select(.name == "teleport-cluster") | .version' "$lock_file")
repository=$(jq -er '.charts[] | select(.name == "teleport-cluster") | .repository' "$lock_file")
chart=$(jq -er '.charts[] | select(.name == "teleport-cluster") | .chart' "$lock_file")
expected_sha=$(jq -er '.charts[] | select(.name == "teleport-cluster") | .sha256' "$lock_file")
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && "$expected_sha" =~ ^[a-f0-9]{64}$ ]] ||
  fail 'Teleport chart lock is invalid'

temporary_directory=$(mktemp -d)
cleanup() { rm -rf -- "$temporary_directory"; }
trap cleanup EXIT
helm pull "$chart" --repo "$repository" --version "$version" \
  --destination "$temporary_directory" >/dev/null
archive="$temporary_directory/$chart-$version.tgz"
[[ -f "$archive" ]] || fail 'Teleport chart archive is absent'
printf '%s  %s\n' "$expected_sha" "$archive" | sha256sum --check --status ||
  fail 'Teleport chart digest mismatch'

values_file="$temporary_directory/values.yaml"
HOST="$host" KUBE_CLUSTER_NAME="$kube_cluster_name" yq -n -o=yaml '
  .chartMode = "standalone" |
  .clusterName = strenv(HOST) |
  .kubeClusterName = strenv(KUBE_CLUSTER_NAME) |
  .proxyProtocol = "off" |
  .proxyProtocol style="double" |
  .proxyListenerMode = "multiplex" |
  .publicAddr = [strenv(HOST) + ":443"] |
  .service.type = "ClusterIP" |
  .ingress.enabled = true |
  .ingress.suppressAutomaticWildcards = true |
  .ingress.spec.ingressClassName = "traefik" |
  .annotations.ingress."traefik.ingress.kubernetes.io/service.serversscheme" = "https" |
  .tls.existingSecretName = "teleport-tls" |
  .authentication.type = "github" |
  .authentication.connectorName = "github" |
  .authentication.localAuth = false |
  .authentication.secondFactors = ["sso"] |
  .persistence.enabled = true |
  .persistence.volumeSize = "10Gi" |
  .resources.requests.cpu = "250m" |
  .resources.requests.memory = "512Mi" |
  .podSecurityPolicy.enabled = false
' >"$values_file"
INGRESS_CLASS="$ingress_class" yq -i \
  '.ingress.spec.ingressClassName = strenv(INGRESS_CLASS)' "$values_file"

helm template teleport "$archive" --namespace teleport --values "$values_file" >/dev/null ||
  fail 'Teleport chart render failed'
"$repository_root/tools/dev/preflight-public-hosts.sh" --hosts "$host" \
  --allowed-ipv4-addresses "$allowed_ipv4_addresses" \
  --allowed-ipv6-addresses "$allowed_ipv6_addresses"
if [[ "$mode" == preflight ]]; then
  printf 'Kodex Teleport preflight completed\n'
  exit 0
fi

if [[ "$mode" == apply ]]; then
  for credential_file in "$github_client_id_file" "$github_client_secret_file"; do
    [[ "$credential_file" == /* && -f "$credential_file" && -s "$credential_file" &&
      ! -L "$credential_file" && $((8#$(stat -c '%a' "$credential_file") & 8#077)) == 0 ]] ||
      fail 'GitHub OAuth credential file is invalid or not private'
  done
  [[ "$github_organization" =~ ^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$ ]] ||
    fail 'GitHub organization is invalid'
  [[ "$github_team" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] ||
    fail 'GitHub team slug is invalid'

  kubectl create namespace teleport --dry-run=client -o yaml |
    kubectl apply --server-side --field-manager=kodex-teleport -f - >/dev/null
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
  ' | kubectl apply --server-side --field-manager=kodex-teleport -f - >/dev/null
  kubectl -n teleport wait --for=condition=Ready certificate/teleport-tls --timeout=10m >/dev/null ||
    fail 'Teleport public certificate is not ready'

  helm upgrade --install teleport "$archive" --namespace teleport \
    --values "$values_file" --atomic --wait --timeout 15m

  client_id=$(<"$github_client_id_file")
  client_secret=$(<"$github_client_secret_file")
  role_file="$temporary_directory/kodex-k8s-admin.json"
  connector_file="$temporary_directory/github.json"
  jq -n '{
    kind:"role",version:"v7",metadata:{name:"kodex-k8s-admin"},
    spec:{allow:{kubernetes_labels:{"*":"*"},kubernetes_groups:["system:masters"]}}
  }' >"$role_file"
  jq -n --arg client_id "$client_id" --arg client_secret "$client_secret" \
    --arg redirect_url "https://$host/v1/webapi/github/callback" \
    --arg organization "$github_organization" --arg team "$github_team" '{
      kind:"github",version:"v3",metadata:{name:"github"},
      spec:{display:"GitHub",client_id:$client_id,client_secret:$client_secret,
        redirect_url:$redirect_url,teams_to_logins:null,
        teams_to_roles:[{organization:$organization,team:$team,roles:["kodex-k8s-admin"]}]}
    }' >"$connector_file"
  chmod 0600 "$role_file" "$connector_file"
  kubectl -n teleport exec deployment/teleport-auth -- tctl create -f - \
    <"$role_file" >/dev/null
  kubectl -n teleport exec deployment/teleport-auth -- tctl create -f - \
    <"$connector_file" >/dev/null
  unset client_id client_secret
fi

for deployment in teleport-auth teleport-proxy; do
  kubectl -n teleport rollout status "deployment/$deployment" --timeout=5m >/dev/null ||
    fail "Teleport deployment is unavailable: $deployment"
done
kubectl -n teleport get certificate teleport-tls -o json | jq -e '
  any(.status.conditions[]?; .type == "Ready" and .status == "True")
' >/dev/null || fail 'Teleport certificate readback failed'
kubectl -n teleport get service teleport -o json | jq -e '
  .spec.type == "ClusterIP" and
  ([.spec.ports[] | select(.name == "tls" and .port == 443)] | length) == 1
' >/dev/null || fail 'Teleport proxy Service readback failed'
kubectl -n teleport get ingress teleport-proxy -o json | jq -e --arg host "$host" \
  --arg ingress_class "$ingress_class" '
    .spec.ingressClassName == $ingress_class and
    .spec.rules[0].host == $host and .spec.tls[0].secretName == "teleport-tls" and
    .metadata.annotations["traefik.ingress.kubernetes.io/service.serversscheme"] == "https"
  ' >/dev/null || fail 'Teleport Ingress readback failed'
kubectl -n teleport exec deployment/teleport-auth -- \
  tctl get role/kodex-k8s-admin -o json | jq -e '
    length == 1 and .[0].spec.allow.kubernetes_groups == ["system:masters"]
  ' >/dev/null || fail 'Teleport Kubernetes administrator role readback failed'
kubectl -n teleport exec deployment/teleport-auth -- tctl get github/github -o json | jq -e \
  --arg host "$host" --arg organization "$github_organization" --arg team "$github_team" '
    length == 1 and
    .[0].spec.redirect_url == ("https://" + $host + "/v1/webapi/github/callback") and
    any(.[0].spec.teams_to_roles[]?;
      .organization == $organization and .team == $team and
      (.roles | index("kodex-k8s-admin") != null))
  ' >/dev/null || fail 'Teleport GitHub connector readback failed'
curl --proto '=https' --tlsv1.2 --fail --silent --show-error \
  "https://$host/webapi/ping" | jq -e '.auth.type == "github"' >/dev/null ||
  fail 'Teleport public GitHub authentication endpoint readback failed'
printf 'Kodex Teleport bootstrap completed: %s\n' "$mode"
