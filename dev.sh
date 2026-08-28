#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local development failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 up|status|smoke|e2e|down [--kubeconfig <path>] [--context <name>]" \
    "       $0 e2e [--resource-prefix <slug>] [--run-timeout-ms <milliseconds>]" \
    "       $0 provider-authorize|provider-import|provider-list [provider options]" \
    '  [--state-directory <path>]' >&2
}

command_name=${1:-}
[[ -n "$command_name" ]] || { usage; exit 1; }
shift
case "$command_name" in
  provider-authorize)
    exec "$(dirname -- "${BASH_SOURCE[0]}")/tools/dev/provider-account.sh" authorize "$@"
    ;;
  provider-import)
    exec "$(dirname -- "${BASH_SOURCE[0]}")/tools/dev/provider-account.sh" import "$@"
    ;;
  provider-list)
    exec "$(dirname -- "${BASH_SOURCE[0]}")/tools/dev/provider-account.sh" list "$@"
    ;;
esac
kubeconfig=${KODEX_DEV_KUBECONFIG:-/home/s/.kube/radar-dev-local}
context=${KODEX_DEV_KUBE_CONTEXT:-radar-dev-local}
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
state_directory="$repository_root/.kodex-dev"
resource_prefix="local-e2e-$(date -u +%Y%m%d%H%M%S)"
run_timeout_ms=900000
while (($# > 0)); do
  case "$1" in
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --resource-prefix) resource_prefix=${2:-}; shift 2 ;;
    --run-timeout-ms) run_timeout_ms=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
case "$command_name" in up|status|smoke|e2e|down) ;; *) usage; fail 'command is invalid' ;; esac
if [[ "$command_name" == e2e ]]; then
  [[ "$resource_prefix" =~ ^[a-z0-9]([a-z0-9-]{2,38}[a-z0-9])$ ]] ||
    fail 'E2E resource prefix must be a lowercase 4-40 character slug'
  [[ "$run_timeout_ms" =~ ^[0-9]+$ && "$run_timeout_ms" -ge 60000 && "$run_timeout_ms" -le 1800000 ]] ||
    fail 'E2E run timeout must be between 60000 and 1800000 milliseconds'
fi
[[ -f "$kubeconfig" && -r "$kubeconfig" ]] || fail 'Kubernetes configuration is absent'
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
  fail 'state directory must be an exact safe absolute path'
export KUBECONFIG=$kubeconfig
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'
[[ "$context" != *prod* && "$context" != *production* ]] || fail 'production context is forbidden'

if [[ "$command_name" == down ]]; then
  for namespace in kodex-system identity; do
    kubectl get namespace "$namespace" >/dev/null 2>&1 || continue
    kubectl delete namespace "$namespace" --wait=false >/dev/null
    deadline=$((SECONDS + 600))
    while kubectl get namespace "$namespace" >/dev/null 2>&1; do
      ((SECONDS < deadline)) || fail "namespace deletion timed out: $namespace"
      sleep 1
    done
  done
  printf 'Kodex local application namespaces removed; shared cluster controllers retained\n'
  exit 0
fi

install -d -m 0700 "$state_directory" "$state_directory/cache" "$state_directory/inputs"
node_ip=${KODEX_DEV_NODE_IP:-}
if [[ -n "$node_ip" ]]; then
  [[ "$node_ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'KODEX_DEV_NODE_IP must use IPv4'
  kubectl get nodes -o json | jq -e --arg node_ip "$node_ip" '
    [.items[].status.addresses[] | select(.type == "InternalIP" and .address == $node_ip)] |
    length == 1
  ' >/dev/null || fail 'KODEX_DEV_NODE_IP is not an exact local node InternalIP'
else
  node_ip=$(kubectl get nodes -o json | jq -er '
  if (.items | length) != 1 then error("one local node is required") else
    [.items[0].status.addresses[] |
      select(.type == "InternalIP" and (.address | test("^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$"))) |
      .address] |
    if length != 1 then error("one IPv4 InternalIP is required") else .[0] end
  end
') || fail 'local node IPv4 address is ambiguous'
fi
[[ "$node_ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'local node must use IPv4'
dns_suffix=${node_ip//./.}.nip.io
public_host="control.$dns_suffix"
oidc_host="sso.$dns_suffix"
grafana_host="grafana.$dns_suffix"
headlamp_host="headlamp.$dns_suffix"

credentials_file="$state_directory/credentials.env"
if [[ ! -e "$credentials_file" ]]; then
  umask 077
  cat >"$credentials_file" <<EOF
KODEX_LOCAL_ADMIN_USERNAME=admin
KODEX_LOCAL_ADMIN_PASSWORD=$(openssl rand -hex 32)
KODEX_LOCAL_OWNER_USERNAME=owner
KODEX_LOCAL_OWNER_EMAIL=owner@kodex.local
KODEX_LOCAL_OWNER_PASSWORD=$(openssl rand -hex 32)
EOF
fi
[[ -f "$credentials_file" && ! -L "$credentials_file" ]] || fail 'credentials file is invalid'
chmod 0600 "$credentials_file"
# shellcheck disable=SC1090
source "$credentials_file"

cluster_mode=readback
[[ "$command_name" == up ]] && cluster_mode=apply
"$repository_root/tools/dev/bootstrap-cluster.sh" --context "$context" \
  --mode "$cluster_mode" --state-directory "$state_directory"

if [[ "$command_name" == status || "$command_name" == smoke || "$command_name" == e2e ]]; then
  "$repository_root/tools/dev/deploy-local.sh" --context "$context" --mode readback \
    --render "$state_directory/render.yaml"
  if [[ "$command_name" == status ]]; then
    printf 'Control Center: https://%s\nCredentials: %s\n' "$public_host" "$credentials_file"
    exit 0
  fi
  frontend_directory="$repository_root/services/staff/control-center"
  install -d -m 0700 "$state_directory/e2e"
  if [[ ! -x "$frontend_directory/node_modules/.bin/playwright" ]]; then
    npm --prefix "$frontend_directory" ci
  fi
  if ! KODEX_E2E_BASE_URL="https://$public_host" \
    KODEX_E2E_OWNER_USERNAME="$KODEX_LOCAL_OWNER_USERNAME" \
    KODEX_E2E_OWNER_PASSWORD="$KODEX_LOCAL_OWNER_PASSWORD" \
    KODEX_E2E_STORAGE_STATE="$state_directory/e2e/owner.json" \
    KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
    NODE_EXTRA_CA_CERTS="$state_directory/kodex-local-ca.crt" \
    npm --prefix "$frontend_directory" run test:e2e:local; then
    fail 'local browser smoke failed'
  fi
  if [[ "$command_name" == e2e ]]; then
    run_state="$state_directory/e2e/$resource_prefix-state.json"
    report="$state_directory/e2e/$resource_prefix-report.json"
    [[ ! -e "$run_state" && ! -e "$report" ]] ||
      fail 'E2E state or report already exists for this resource prefix'
    if ! KODEX_E2E_BASE_URL="https://$public_host" \
      KODEX_E2E_STORAGE_STATE="$state_directory/e2e/owner.json" \
      KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
      KODEX_E2E_RESOURCE_PREFIX="$resource_prefix" \
      KODEX_E2E_RUN_STATE="$run_state" \
      KODEX_E2E_DISCOVERY_REPORT="$report" \
      KODEX_E2E_RUN_TIMEOUT_MS="$run_timeout_ms" \
      NODE_EXTRA_CA_CERTS="$state_directory/kodex-local-ca.crt" \
      npm --prefix "$frontend_directory" run test:e2e:discovery; then
      fail 'local browser E2E failed'
    fi
    jq -e '
      .version == 1 and .status == "passed" and
      (.results | length) > 0 and all(.results[]; .status == "passed")
    ' "$report" >/dev/null || fail 'local browser E2E report is not fully successful'
    chmod 0600 "$run_state" "$report"
    "$repository_root/tools/dev/verify-discovery-readback.sh" \
      --context "$context" --kubeconfig "$kubeconfig" --state "$run_state" \
      --expect-account default-openai-codex \
      --expect-account openai-codex-account-2
    printf 'Kodex local full E2E completed: %s\nReport: %s\n' "$resource_prefix" "$report"
    exit 0
  fi
  printf 'Kodex local browser smoke completed\n'
  exit 0
fi

kubectl create namespace kodex-system --dry-run=client -o yaml |
  kubectl apply --server-side --field-manager=kodex-local-dev -f - >/dev/null
kubectl label namespace kodex-system app.kubernetes.io/part-of=kodex \
  kodex.dev/environment=staging kodex.dev/local-profile=hot-reload --overwrite >/dev/null

material_directory="$state_directory/material"
if [[ ! -d "$material_directory" ]]; then
  registry_username="$state_directory/inputs/registry-username"
  registry_password="$state_directory/inputs/registry-password"
  printf '%s' local-dev >"$registry_username"
  openssl rand -hex 32 >"$registry_password"
  chmod 0600 "$registry_username" "$registry_password"
  "$repository_root/tools/install/generate-material.sh" \
    --output-directory "$material_directory" \
    --release-registry-host registry.local.kodex \
    --promoted-pull-host pull.local.kodex \
    --release-registry-username-file "$registry_username" \
    --release-registry-password-file "$registry_password"
fi

if [[ ! -d "$material_directory/identity" ]]; then
  for input in admin-username admin-password owner-username owner-email owner-password; do
    : >"$state_directory/inputs/$input"
  done
  printf '%s' "$KODEX_LOCAL_ADMIN_USERNAME" >"$state_directory/inputs/admin-username"
  printf '%s' "$KODEX_LOCAL_ADMIN_PASSWORD" >"$state_directory/inputs/admin-password"
  printf '%s' "$KODEX_LOCAL_OWNER_USERNAME" >"$state_directory/inputs/owner-username"
  printf '%s' "$KODEX_LOCAL_OWNER_EMAIL" >"$state_directory/inputs/owner-email"
  printf '%s' "$KODEX_LOCAL_OWNER_PASSWORD" >"$state_directory/inputs/owner-password"
  chmod 0600 "$state_directory/inputs"/*
  "$repository_root/tools/deploy/generate-identity-material.sh" \
    --material-directory "$material_directory" \
    --admin-username-file "$state_directory/inputs/admin-username" \
    --admin-initial-password-file "$state_directory/inputs/admin-password" \
    --owner-username-file "$state_directory/inputs/owner-username" \
    --owner-email-file "$state_directory/inputs/owner-email" \
    --owner-initial-password-file "$state_directory/inputs/owner-password"
fi

"$repository_root/tools/deploy/materialize-identity-secrets.sh" \
  --context "$context" --material-directory "$material_directory"
"$repository_root/infra/identity/bootstrap.sh" --context "$context" --mode apply \
  --oidc-host "$oidc_host" --ingress-class traefik --cluster-issuer kodex-local \
  --ingress-namespace kube-system --ingress-pod-name traefik
kubectl -n identity patch serverstransport sso-public --type=merge \
  -p '{"spec":{"rootCAsSecrets":["sso-public-tls"]}}' >/dev/null
"$repository_root/tools/deploy/configure-keycloak.sh" --context "$context" --mode apply \
  --public-origin "https://$public_host" --grafana-origin "https://$grafana_host" \
  --headlamp-origin "https://$headlamp_host"

"$repository_root/tools/install/materialize-nats-runtime-users.sh" \
  --context "$context" --material-directory "$material_directory"
provider_auth=${KODEX_DEV_PROVIDER_AUTH_FILE:-"$state_directory/inputs/openai-auth.json"}
if [[ -n "${KODEX_DEV_PROVIDER_AUTH_FILE:-}" ]]; then
  [[ "$provider_auth" == /* && -f "$provider_auth" && ! -L "$provider_auth" ]] ||
    fail 'KODEX_DEV_PROVIDER_AUTH_FILE must be an absolute regular non-symlink file'
  [[ "$(stat -c '%u' "$provider_auth")" == "$(id -u)" &&
    $((8#$(stat -c '%a' "$provider_auth") & 8#077)) == 0 ]] ||
    fail 'KODEX_DEV_PROVIDER_AUTH_FILE must be owned by the current user and private'
  [[ "$(stat -c '%s' "$provider_auth")" -le 1048576 ]] ||
    fail 'KODEX_DEV_PROVIDER_AUTH_FILE exceeds the supported size'
elif [[ ! -e "$provider_auth" ]]; then
  printf '%s\n' '{"auth_mode":"local-development","access_token":"not-configured"}' >"$provider_auth"
  chmod 0600 "$provider_auth"
fi
"$repository_root/tools/install/materialize-secrets.sh" --context "$context" \
  --material-directory "$material_directory" \
  --oidc-ca-file "$state_directory/kodex-local-ca.crt" \
  --provider-auth-file "$provider_auth"

"$repository_root/tools/dev/build-local-runner.sh" \
  --source-root "$repository_root" --state-directory "$state_directory"
runner_image=$(<"$state_directory/agent-runner-image")
"$repository_root/tools/dev/build-local-session-archive.sh" \
  --source-root "$repository_root" --state-directory "$state_directory"
session_archive_image=$(<"$state_directory/session-archive-image")
"$repository_root/tools/dev/build-local-backup-controller.sh" \
  --source-root "$repository_root" --state-directory "$state_directory"
backup_controller_image=$(<"$state_directory/backup-controller-image")

api_service_ip=$(kubectl -n default get service kubernetes -o jsonpath='{.spec.clusterIP}')
api_endpoint_slices=$(kubectl -n default get endpointslice \
  -l kubernetes.io/service-name=kubernetes -o json)
api_endpoint_ip=$(jq -er '
  [.items[] |
    select(.addressType == "IPv4") |
    .endpoints[] |
    select(.conditions.ready != false) |
    .addresses[] |
    select(test("^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$"))] |
  unique |
  if length != 1 then error("one ready Kubernetes API IPv4 endpoint is required") else .[0] end
' <<<"$api_endpoint_slices") || fail 'Kubernetes API endpoint address is ambiguous'
api_endpoint_port=$(jq -er '
  [.items[].ports[] |
    select(.protocol == "TCP" and .port != null) |
    .port] |
  unique |
  if length != 1 then error("one Kubernetes API TCP port is required") else .[0] end
' <<<"$api_endpoint_slices") || fail 'Kubernetes API endpoint port is ambiguous'
"$repository_root/tools/dev/render-local.sh" --source-root "$repository_root" \
  --cache-root "$state_directory/cache" --output "$state_directory/render.yaml" \
  --public-host "$public_host" --oidc-host "$oidc_host" \
  --kubernetes-service-cidr "$api_service_ip/32" \
  --kubernetes-endpoint-cidr "$api_endpoint_ip/32" \
  --kubernetes-endpoint-port "$api_endpoint_port" \
  --runner-image "$runner_image" \
  --session-archive-image "$session_archive_image" \
  --backup-controller-image "$backup_controller_image"
"$repository_root/tools/dev/deploy-local.sh" --context "$context" --mode apply \
  --render "$state_directory/render.yaml"

"$repository_root/tools/deploy/configure-keycloak.sh" --context "$context" --mode readback \
  --public-origin "https://$public_host" --grafana-origin "https://$grafana_host" \
  --headlamp-origin "https://$headlamp_host"

printf '%s\n' \
  'Kodex local development is ready' \
  "Control Center: https://$public_host" \
  "Credentials: $credentials_file" \
  "Rendered manifest: $state_directory/render.yaml"
