#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local development failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 up|status|smoke|e2e|full-e2e|down [--kubeconfig <path>] [--context <name>]" \
    "       $0 e2e [--resource-prefix <slug>] [--run-timeout-ms <milliseconds>]" \
    "       $0 full-e2e [--check] [--skip-build] [--resource-prefix <slug>]" \
    "         [--target <test-make-target>]..." \
    "       $0 provider-authorize|provider-import|provider-list [provider options]" \
    '  [--state-directory <path>]' >&2
}

command_name=${1:-}
[[ -n "$command_name" ]] || { usage; exit 1; }
shift
case "$command_name" in
  full-e2e)
    exec "$(dirname -- "${BASH_SOURCE[0]}")/tools/dev/full-local-e2e.sh" "$@"
    ;;
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
kubeconfig=${KODEX_DEV_KUBECONFIG:-"$HOME/.kube/kodex-dev-local"}
context=${KODEX_DEV_KUBE_CONTEXT:-default}
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
  for namespace in kodex-runtime kodex-system identity kodex-trust; do
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

read_authority_snapshot_revision() {
  local encoded compact payload revision
  if ! encoded=$(kubectl -n kodex-system get secret/internal-rpc-authority-snapshot \
    -o jsonpath='{.data.snapshot\.jws}' 2>/dev/null); then
    printf '0\n'
    return
  fi
  if [[ -z "$encoded" ]]; then
    # A seed Secret can survive an interrupted first apply before the publisher
    # materializes revision 1. Treat only that empty seed as uninitialized.
    printf '0\n'
    return
  fi
  compact=$(printf '%s' "$encoded" | base64 --decode 2>/dev/null) ||
    fail 'local authority snapshot encoding is invalid'
  IFS=. read -r _ payload _ <<<"$compact"
  [[ -n "$payload" ]] || fail 'local authority snapshot payload is absent'
  case $((${#payload} % 4)) in
    0) ;;
    2) payload="${payload}==" ;;
    3) payload="${payload}=" ;;
    *) fail 'local authority snapshot payload encoding is invalid' ;;
  esac
  revision=$(printf '%s' "$payload" | tr '_-' '/+' | base64 --decode 2>/dev/null |
    jq -er '
      .source_revision |
      select(type == "number" and . >= 1 and . <= 9007199254740991 and floor == .)
    ') || fail 'local authority snapshot source revision is invalid'
  printf '%s\n' "$revision"
}

calculate_local_source_fingerprint() {
  (
    cd -- "$repository_root"
    printf 'HEAD\0%s\0' "$(git rev-parse HEAD)"
    git diff --no-ext-diff --binary HEAD --
    while IFS= read -r -d '' path; do
      printf 'UNTRACKED\0%s\0' "$path"
      if [[ -L "$path" ]]; then
        printf 'SYMLINK\0%s\0' "$(readlink -- "$path")"
      elif [[ -f "$path" ]]; then
        sha256sum -- "$path"
      else
        printf 'OTHER\0'
      fi
    done < <(git ls-files --others --exclude-standard -z)
  ) | sha256sum | awk '{print $1}'
}

resolve_local_authority_source_revision() {
  local current_revision source_fingerprint state_file state_revision state_fingerprint
  current_revision=$(read_authority_snapshot_revision)
  source_fingerprint=$(calculate_local_source_fingerprint)
  [[ "$source_fingerprint" =~ ^[a-f0-9]{64}$ ]] ||
    fail 'local source fingerprint is invalid'
  state_file="$state_directory/authority-source-state.json"
  state_revision=0
  state_fingerprint=""
  if [[ -e "$state_file" ]]; then
    [[ -f "$state_file" && ! -L "$state_file" &&
      "$(stat -c '%u' "$state_file")" == "$(id -u)" &&
      $((8#$(stat -c '%a' "$state_file") & 8#077)) == 0 ]] ||
      fail 'local authority source state is unsafe'
    state_revision=$(jq -er '
      select(.version == 1) | .sourceRevision |
      select(type == "number" and . >= 1 and . <= 9007199254740991 and floor == .)
    ' "$state_file") || fail 'local authority source state revision is invalid'
    state_fingerprint=$(jq -er '
      .sourceFingerprint | select(type == "string" and test("^[a-f0-9]{64}$"))
    ' "$state_file") || fail 'local authority source state fingerprint is invalid'
  fi
  if ((current_revision == 0)); then
    authority_source_revision=1
  elif ((state_revision == current_revision)) &&
    [[ "$state_fingerprint" == "$source_fingerprint" ]]; then
    authority_source_revision=$current_revision
  else
    ((current_revision < 9007199254740991)) ||
      fail 'local authority source revision is exhausted'
    authority_source_revision=$((current_revision + 1))
  fi
  authority_source_fingerprint=$source_fingerprint
}

commit_local_authority_source_state() {
  local state_file temporary_state source_sha
  state_file="$state_directory/authority-source-state.json"
  temporary_state=$(mktemp "$state_directory/.authority-source-state.XXXXXX")
  source_sha=$(git -C "$repository_root" rev-parse HEAD)
  jq -n --argjson source_revision "$authority_source_revision" \
    --arg source_fingerprint "$authority_source_fingerprint" \
    --arg source_sha "$source_sha" '
      {
        version: 1,
        sourceRevision: $source_revision,
        sourceFingerprint: $source_fingerprint,
        sourceSHA: $source_sha
      }
    ' >"$temporary_state"
  chmod 0600 "$temporary_state"
  mv -- "$temporary_state" "$state_file"
}

endpoint_ip=${KODEX_DEV_ENDPOINT_IP:-127.0.0.1}
[[ "$endpoint_ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
  fail 'KODEX_DEV_ENDPOINT_IP must use IPv4'
if [[ "$endpoint_ip" != 127.0.0.1 ]]; then
  ip -4 -o address show | awk '{print $4}' | cut -d/ -f1 | grep -Fxq "$endpoint_ip" ||
    fail 'KODEX_DEV_ENDPOINT_IP is not assigned to this host'
fi
dns_suffix=${endpoint_ip//./.}.nip.io
public_host="control.$dns_suffix"
oidc_host="sso.$dns_suffix"
grafana_host="grafana.$dns_suffix"
headlamp_host="headlamp.$dns_suffix"
registry_host="registry.$dns_suffix"
promoted_pull_host="pull.$dns_suffix"
keycloak_origin_arguments=(
  --public-origin "https://$public_host"
  --grafana-origin "https://$grafana_host"
  --headlamp-origin "https://$headlamp_host"
)

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
  if [[ "$command_name" == e2e ]]; then
    "$repository_root/tools/dev/build-local-session-archive.sh" \
      --source-root "$repository_root" --state-directory "$state_directory"
  fi
  "$repository_root/tools/dev/deploy-local.sh" --context "$context" --mode readback \
    --render "$state_directory/render.yaml" --state-directory "$state_directory"
  if [[ "$command_name" == status ]]; then
    printf 'Control Center: https://%s\nCredentials: %s\n' "$public_host" "$credentials_file"
    exit 0
  fi
  frontend_directory="$repository_root/services/staff/control-center"
  install -d -m 0700 "$state_directory/e2e"
  if [[ ! -x "$frontend_directory/node_modules/.bin/playwright" ]]; then
    npm --prefix "$frontend_directory" ci
  fi
  if [[ "$command_name" == smoke || "$command_name" == e2e ]]; then
    KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
      "$repository_root/tools/dev/prepare-e2e-oidc-group.sh" --context "$context" \
      --state-directory "$state_directory"
  fi
  if ! KODEX_E2E_BASE_URL="https://$public_host" \
    KODEX_E2E_OWNER_USERNAME="$KODEX_LOCAL_OWNER_USERNAME" \
    KODEX_E2E_OWNER_PASSWORD="$KODEX_LOCAL_OWNER_PASSWORD" \
    KODEX_E2E_STORAGE_STATE="$state_directory/e2e/owner.json" \
    KODEX_E2E_RBAC_GROUP=kodex-e2e-restricted \
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
      KODEX_E2E_RBAC_GROUP=kodex-e2e-restricted \
      KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
      KODEX_E2E_RESOURCE_PREFIX="$resource_prefix" \
      KODEX_E2E_RUN_STATE="$run_state" \
      KODEX_E2E_DISCOVERY_REPORT="$report" \
      KODEX_E2E_RUN_TIMEOUT_MS="$run_timeout_ms" \
      KODEX_E2E_KUBECONFIG="$kubeconfig" \
      KODEX_E2E_KUBE_CONTEXT="$context" \
      KODEX_E2E_REPOSITORY_ROOT="$repository_root" \
      KODEX_E2E_STATE_DIRECTORY="$state_directory" \
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

material_directory="$state_directory/material"
material_action=$("$repository_root/tools/dev/reconcile-local-material.sh" --context "$context" \
  --state-directory "$state_directory" --mode reconcile)
printf 'Kodex local material action: %s\n' "$material_action"

kubectl create namespace kodex-system --dry-run=client -o yaml |
  kubectl apply --server-side --field-manager=kodex-local-dev -f - >/dev/null
kubectl label namespace kodex-system app.kubernetes.io/part-of=kodex \
  kodex.dev/environment=staging kodex.dev/local-profile=hot-reload --overwrite >/dev/null

if [[ ! -d "$material_directory" ]]; then
  registry_username="$state_directory/inputs/registry-username"
  registry_password="$state_directory/inputs/registry-password"
  printf '%s' local-dev >"$registry_username"
  openssl rand -hex 32 >"$registry_password"
  chmod 0600 "$registry_username" "$registry_password"
  "$repository_root/tools/install/generate-material.sh" \
    --output-directory "$material_directory" \
    --release-registry-host "$registry_host" \
    --promoted-pull-host "$promoted_pull_host" \
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
kubectl label namespace identity app.kubernetes.io/part-of=kodex kodex.dev/capability=identity \
  kodex.dev/environment=staging kodex.dev/local-profile=hot-reload --overwrite >/dev/null
kubectl -n identity patch serverstransport sso-public --type=merge \
  -p '{"spec":{"rootCAsSecrets":["sso-public-tls"]}}' >/dev/null
"$repository_root/tools/deploy/configure-keycloak.sh" --context "$context" --mode apply \
  "${keycloak_origin_arguments[@]}"

"$repository_root/tools/install/materialize-nats-runtime-users.sh" \
  --context "$context" --material-directory "$material_directory"
default_provider_auth="$state_directory/provider-accounts/default-openai-codex/auth.json"
provider_auth=${KODEX_DEV_PROVIDER_AUTH_FILE:-$default_provider_auth}
[[ "$provider_auth" == /* && -f "$provider_auth" && ! -L "$provider_auth" ]] ||
  fail 'provider authorization is absent; set KODEX_DEV_PROVIDER_AUTH_FILE to a private Codex auth.json'
[[ "$(stat -c '%u' "$provider_auth")" == "$(id -u)" &&
  $((8#$(stat -c '%a' "$provider_auth") & 8#077)) == 0 ]] ||
  fail 'provider authorization must be owned by the current user and private'
[[ "$(stat -c '%s' "$provider_auth")" -le 1048576 ]] ||
  fail 'provider authorization exceeds the supported size'
provider_validation_home=$(mktemp -d "$state_directory/.provider-validation.XXXXXX")
chmod 0700 "$provider_validation_home"
install -m 0600 "$provider_auth" "$provider_validation_home/auth.json"
if ! CODEX_HOME="$provider_validation_home" HOME="$provider_validation_home" \
  codex login status >/dev/null 2>&1; then
  rm -rf -- "$provider_validation_home"
  fail 'Codex does not recognize the provider authorization file'
fi
rm -rf -- "$provider_validation_home"
"$repository_root/tools/install/materialize-secrets.sh" --context "$context" \
  --material-directory "$material_directory" \
  --oidc-ca-file "$state_directory/kodex-local-ca.crt" \
  --provider-auth-file "$provider_auth"
"$repository_root/tools/dev/reconcile-local-material.sh" --context "$context" \
  --state-directory "$state_directory" --mode commit >/dev/null

"$repository_root/tools/dev/configure-local-node-registry.sh" --mode apply \
  --context "$context" --material-directory "$material_directory" \
  --promoted-pull-host "$promoted_pull_host"

"$repository_root/tools/dev/build-local-runner.sh" \
  --source-root "$repository_root" --state-directory "$state_directory"
runner_image=$(<"$state_directory/agent-runner-image")
"$repository_root/tools/dev/build-local-session-archive.sh" \
  --source-root "$repository_root" --state-directory "$state_directory"
session_archive_image=$(<"$state_directory/session-archive-image")
"$repository_root/tools/dev/build-local-backup-controller.sh" \
  --source-root "$repository_root" --state-directory "$state_directory"
backup_controller_image=$(<"$state_directory/backup-controller-image")
"$repository_root/tools/dev/build-local-image-supply-chain.sh" \
  --source-root "$repository_root" --state-directory "$state_directory"
role_image_builder_image=$(<"$state_directory/role-image-builder-image")
image_admission_image=$(<"$state_directory/image-admission-image")
image_admission_tools_image=$(<"$state_directory/image-admission-tools-image")
authority_image=$(<"$state_directory/internal-rpc-authority-image")
role_image_input_manifest_digest=$(jq -er '.manifestDigest' "$state_directory/role-image-input.json")
role_image_input_payload_sha256=$(jq -er '.payloadSha256' "$state_directory/role-image-input.json")
role_image_input_source_sha256=$(jq -er '.sourceSha256' "$state_directory/role-image-input.json")

resolve_local_authority_source_revision

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
  --backup-controller-image "$backup_controller_image" \
  --promoted-pull-host "$promoted_pull_host" \
  --role-image-builder-image "$role_image_builder_image" \
  --image-admission-image "$image_admission_image" \
  --image-admission-tools-image "$image_admission_tools_image" \
  --authority-image "$authority_image" \
  --authority-source-revision "$authority_source_revision" \
  --role-image-input-manifest-digest "$role_image_input_manifest_digest" \
  --role-image-input-payload-sha256 "$role_image_input_payload_sha256" \
  --role-image-input-source-sha256 "$role_image_input_source_sha256"
"$repository_root/tools/dev/deploy-local.sh" --context "$context" --mode apply \
  --render "$state_directory/render.yaml" --state-directory "$state_directory"
commit_local_authority_source_state

provider_metadata=("$state_directory"/provider-accounts/*/account.json)
restored_provider_accounts=0
for metadata_file in "${provider_metadata[@]}"; do
  [[ -e "$metadata_file" ]] || continue
  [[ -f "$metadata_file" && ! -L "$metadata_file" &&
    "$(stat -c '%u' "$metadata_file")" == "$(id -u)" &&
    $((8#$(stat -c '%a' "$metadata_file") & 8#077)) == 0 ]] ||
    fail 'provider account metadata is unsafe'
  account_key=$(jq -er '
    select(.version == 1 and (.accountKey | type == "string") and
      (.name | type == "string" and length > 0 and length <= 160)) |
    .accountKey
  ' "$metadata_file") || fail 'provider account metadata is invalid'
  account_name=$(jq -er '.name' "$metadata_file") || fail 'provider account name is invalid'
  [[ "$account_key" == "$(basename -- "$(dirname -- "$metadata_file")")" ]] ||
    fail 'provider account metadata directory binding is invalid'
  account_auth_file="$(dirname -- "$metadata_file")/auth.json"
  if [[ "$account_key" == default-openai-codex ]] &&
    [[ "$(realpath -e -- "$account_auth_file")" == "$(realpath -e -- "$provider_auth")" ]]; then
    continue
  fi
  "$repository_root/tools/dev/provider-account.sh" import \
    --kubeconfig "$kubeconfig" --context "$context" --state-directory "$state_directory" \
    --account-key "$account_key" --name "$account_name" \
    --auth-file "$account_auth_file"
  restored_provider_accounts=$((restored_provider_accounts + 1))
done
if ((restored_provider_accounts > 0)); then
  "$repository_root/tools/dev/deploy-local.sh" --context "$context" --mode readback \
    --render "$state_directory/render.yaml" --state-directory "$state_directory"
fi

"$repository_root/tools/deploy/configure-keycloak.sh" --context "$context" --mode readback \
  "${keycloak_origin_arguments[@]}"

printf '%s\n' \
  'Kodex local development is ready' \
  "Control Center: https://$public_host" \
  "Credentials: $credentials_file" \
  "Rendered manifest: $state_directory/render.yaml"
