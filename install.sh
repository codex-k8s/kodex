#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex installation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 [--env-file <path>] [--components <comma-list|all>] [--non-interactive]" \
    'Components: host,cert-manager,identity,trust,management,registry,arc,secrets,platform' >&2
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
env_file="$repository_root/.kodex-env"
components=""
non_interactive=false
while (($# > 0)); do
  case "$1" in
    --env-file) env_file="${2:-}"; shift 2 ;;
    --components) components="${2:-}"; shift 2 ;;
    --non-interactive) non_interactive=true; shift ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

# shellcheck source=tools/install/load-env.sh
source "$repository_root/tools/install/load-env.sh"
kodex_load_env "$env_file" || exit 1
kodex_require_env KODEX_INSTALL_MODE KODEX_NAMESPACE KODEX_KUBECONFIG \
  KODEX_KUBE_CONTEXT || exit 1
[[ "$KODEX_INSTALL_MODE" == bare-metal || "$KODEX_INSTALL_MODE" == existing-kubernetes ]] ||
  fail 'KODEX_INSTALL_MODE must be bare-metal or existing-kubernetes'
[[ "$KODEX_NAMESPACE" == kodex-system ]] ||
  fail 'the current release supports the isolated kodex-system namespace only'
KODEX_INGRESS_SERVICE_NAME=${KODEX_INGRESS_SERVICE_NAME:-${KODEX_INGRESS_POD_NAME:-}}
KODEX_PUBLIC_TLS_MODE=${KODEX_PUBLIC_TLS_MODE:-enabled}
KODEX_DISABLE_OBSERVABILITY=${KODEX_DISABLE_OBSERVABILITY:-true}
export KODEX_INGRESS_SERVICE_NAME KODEX_PUBLIC_TLS_MODE KODEX_DISABLE_OBSERVABILITY
[[ "$KODEX_PUBLIC_TLS_MODE" == deferred || "$KODEX_PUBLIC_TLS_MODE" == enabled ]] ||
  fail 'KODEX_PUBLIC_TLS_MODE must be deferred or enabled'
[[ "$KODEX_DISABLE_OBSERVABILITY" == true || "$KODEX_DISABLE_OBSERVABILITY" == false ]] ||
  fail 'KODEX_DISABLE_OBSERVABILITY must be true or false'
if [[ "$KODEX_DISABLE_OBSERVABILITY" == false && -z "${KODEX_SENTRY_DSN:-}" ]]; then
  fail 'KODEX_SENTRY_DSN is required when external observability exporters are enabled'
fi

all_components=(host cert-manager identity trust management registry arc secrets platform)
if [[ -z "$components" ]]; then
  if [[ -t 0 && "$non_interactive" == false ]]; then
    selected=()
    for component in "${all_components[@]}"; do
      default=yes
      [[ "$KODEX_INSTALL_MODE" == bare-metal || "$component" != host ]] || default=no
      prompt='Y/n'
      [[ "$default" == no ]] && prompt='y/N'
      read -r -p "Install $component? [$prompt] " answer
      answer=${answer:-$default}
      [[ "$answer" =~ ^([Yy]|yes)$ ]] && selected+=("$component")
    done
    components=$(IFS=,; printf '%s' "${selected[*]}")
  elif [[ "$KODEX_INSTALL_MODE" == bare-metal ]]; then
    components=all
  else
    components=cert-manager,identity,trust,management,registry,arc,secrets,platform
  fi
fi
[[ -n "$components" ]] || fail 'no installation components were selected'
[[ "$components" != all ]] || components=$(IFS=,; printf '%s' "${all_components[*]}")
declare -A selected_components=()
IFS=',' read -r -a requested_components <<<"$components"
for component in "${requested_components[@]}"; do
  [[ " ${all_components[*]} " == *" $component "* ]] || fail "unknown component: $component"
  selected_components[$component]=true
done
component_selected() { [[ "${selected_components[$1]:-}" == true ]]; }
any_component_selected() {
  local component
  for component in "$@"; do
    component_selected "$component" && return 0
  done
  return 1
}
material_directory=${KODEX_MATERIAL_DIRECTORY:-$repository_root/.kodex-material}
[[ "$material_directory" == /* ]] || material_directory="$repository_root/$material_directory"
if component_selected host && [[ "$KODEX_INSTALL_MODE" != bare-metal ]]; then
  fail 'host component is available only in bare-metal mode'
fi
component_selected management && ! component_selected identity &&
  fail 'management requires identity in the same installation run'
component_selected arc && ! component_selected registry &&
  fail 'the bundled ARC profile requires the bundled registry component'

if component_selected host; then
  kodex_require_env KODEX_SERVER_PUBLIC_IP || exit 1
fi
if component_selected cert-manager; then
  kodex_require_env KODEX_ACME_EMAIL KODEX_INGRESS_CLASS || exit 1
fi
if component_selected identity; then
  kodex_require_env KODEX_OIDC_HOST KODEX_CONTROL_HOST KODEX_GRAFANA_HOST \
    KODEX_HEADLAMP_HOST KODEX_INGRESS_CLASS KODEX_CLUSTER_ISSUER \
    KODEX_INGRESS_NAMESPACE KODEX_INGRESS_POD_NAME \
    KODEX_KEYCLOAK_ADMIN_USERNAME KODEX_KEYCLOAK_ADMIN_INITIAL_PASSWORD \
    KODEX_OWNER_USERNAME KODEX_OWNER_EMAIL KODEX_OWNER_INITIAL_PASSWORD || exit 1
fi
if component_selected management; then
  kodex_require_env KODEX_CONTROL_HOST KODEX_GRAFANA_HOST KODEX_HEADLAMP_HOST \
    KODEX_PUBLIC_IPV4_CIDR KODEX_INGRESS_CLASS KODEX_CLUSTER_ISSUER \
    KODEX_INGRESS_NAMESPACE KODEX_INGRESS_POD_NAME || exit 1
fi
if any_component_selected identity management registry arc secrets platform; then
  kodex_require_env KODEX_REGISTRY_HOST KODEX_PROMOTED_PULL_HOST || exit 1
fi
if component_selected arc; then
  kodex_require_env KODEX_GITHUB_ARC_TOKEN KODEX_GITHUB_OWNER_PAT \
    KODEX_INGRESS_NAMESPACE KODEX_INGRESS_POD_NAME KODEX_INGRESS_SERVICE_NAME || exit 1
fi
if component_selected platform; then
  kodex_require_env KODEX_GITHUB_OWNER_PAT KODEX_CONTROL_HOST KODEX_OIDC_HOST \
    KODEX_OIDC_CONNECT_ADDRESS KODEX_OIDC_TLS_SERVER_NAME \
    KODEX_OIDC_NAMESPACE KODEX_OIDC_POD_NAME KODEX_OIDC_POD_COMPONENT \
    KODEX_OIDC_TARGET_PORT KODEX_INGRESS_CLASS KODEX_CLUSTER_ISSUER \
    KODEX_INGRESS_NAMESPACE KODEX_INGRESS_POD_NAME || exit 1
  if [[ "$KODEX_PUBLIC_TLS_MODE" == enabled &&
    -z "${KODEX_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES:-}" &&
    -z "${KODEX_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES:-}" ]]; then
    fail 'at least one public TLS allowed address is required in enabled mode'
  fi
fi
if [[ -n "${KODEX_CONTROL_TLS_RECOVERY_HOST:-}" ]]; then
  [[ "$KODEX_CONTROL_TLS_RECOVERY_HOST" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ &&
    "$KODEX_CONTROL_TLS_RECOVERY_HOST" == *.* ]] ||
    fail 'KODEX_CONTROL_TLS_RECOVERY_HOST must be an exact lowercase DNS name'
  [[ "$KODEX_CONTROL_TLS_RECOVERY_HOST" != "$KODEX_CONTROL_HOST" ]] ||
    fail 'KODEX_CONTROL_TLS_RECOVERY_HOST must differ from KODEX_CONTROL_HOST'
fi
if component_selected secrets; then
  [[ -n "${KODEX_OPENAI_AUTH_JSON_B64:-}" || -n "${KODEX_OPENAI_AUTH_JSON_FILE:-}" ]] ||
    fail 'KODEX_OPENAI_AUTH_JSON_B64 or KODEX_OPENAI_AUTH_JSON_FILE is required'
fi
if ! component_selected registry && any_component_selected secrets platform &&
  [[ ! -e "$material_directory" ]]; then
  kodex_require_env KODEX_RELEASE_REGISTRY_USERNAME KODEX_RELEASE_REGISTRY_PASSWORD || exit 1
fi
if [[ "${KODEX_ENABLE_EXTERNAL_S3:-false}" == true ]]; then
  kodex_require_env KODEX_S3_ENDPOINT KODEX_S3_REGION KODEX_S3_BUCKET \
    KODEX_S3_ACCESS_KEY KODEX_S3_SECRET_KEY || exit 1
elif [[ "${KODEX_ENABLE_EXTERNAL_S3:-false}" != false ]]; then
  fail 'KODEX_ENABLE_EXTERNAL_S3 must be true or false'
fi

if component_selected host; then
  "$repository_root/tools/install/prepare-host.sh" --mode apply \
    --server-public-ip "$KODEX_SERVER_PUBLIC_IP" \
    --server-public-ipv6-address "${KODEX_SERVER_PUBLIC_IPV6_ADDRESS:-}"
fi

export KUBECONFIG=$KODEX_KUBECONFIG
[[ -f "$KUBECONFIG" && -r "$KUBECONFIG" ]] || fail 'Kubernetes configuration is absent'
kubectl config use-context "$KODEX_KUBE_CONTEXT" >/dev/null
[[ "$(kubectl config current-context)" == "$KODEX_KUBE_CONTEXT" ]] || fail 'Kubernetes context mismatch'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'

installer_temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$installer_temporary_directory"' EXIT
ensure_core_material() {
  if [[ ! -e "$material_directory" ]]; then
    local -a arguments=(
      --output-directory "$material_directory" \
      --release-registry-host "$KODEX_REGISTRY_HOST" \
      --promoted-pull-host "$KODEX_PROMOTED_PULL_HOST"
    )
    if ! component_selected registry; then
      printf '%s' "$KODEX_RELEASE_REGISTRY_USERNAME" \
        >"$installer_temporary_directory/registry-username"
      printf '%s' "$KODEX_RELEASE_REGISTRY_PASSWORD" \
        >"$installer_temporary_directory/registry-password"
      chmod 0600 "$installer_temporary_directory/registry-username" \
        "$installer_temporary_directory/registry-password"
      arguments+=(
        --release-registry-username-file "$installer_temporary_directory/registry-username"
        --release-registry-password-file "$installer_temporary_directory/registry-password"
      )
    fi
    "$repository_root/tools/install/generate-material.sh" "${arguments[@]}"
  fi
  [[ -d "$material_directory/projections" && -d "$material_directory/registry" ]] ||
    fail 'installation material directory is incomplete'
  jq -e --arg host "$KODEX_REGISTRY_HOST" '
    (.auths | keys) == [$host] and (.auths[$host].auth | type == "string" and length > 0)
  ' "$material_directory/registry/release-source/dockerconfig.json" >/dev/null ||
    fail 'installation material targets another release registry'
  "$repository_root/tools/install/reconcile-pull-docker-config.sh" \
    --material-directory "$material_directory" \
    --promoted-pull-host "$KODEX_PROMOTED_PULL_HOST"
  chmod 0700 "$material_directory"
  install -d -m 0700 "$material_directory/inputs" "$material_directory/github"
}

write_env_input() {
  local key=$1 output=$2 value=${!1:-}
  [[ -n "$value" ]] || fail "$key is required"
  printf '%s' "$value" >"$output"
  chmod 0600 "$output"
}

if any_component_selected identity management registry arc secrets platform; then
  ensure_core_material
fi
if any_component_selected arc platform; then
  write_env_input KODEX_GITHUB_OWNER_PAT "$material_directory/inputs/github-owner-pat"
fi
if component_selected arc; then
  write_env_input KODEX_GITHUB_ARC_TOKEN "$material_directory/inputs/github-arc-token"
fi

provider_auth_file="$material_directory/inputs/openai-auth.json"
if component_selected secrets && [[ -n "${KODEX_OPENAI_AUTH_JSON_FILE:-}" ]]; then
  [[ -f "$KODEX_OPENAI_AUTH_JSON_FILE" && -s "$KODEX_OPENAI_AUTH_JSON_FILE" ]] ||
    fail 'KODEX_OPENAI_AUTH_JSON_FILE is invalid'
  install -m 0600 "$KODEX_OPENAI_AUTH_JSON_FILE" "$provider_auth_file"
elif component_selected secrets && [[ -n "${KODEX_OPENAI_AUTH_JSON_B64:-}" ]]; then
  printf '%s' "$KODEX_OPENAI_AUTH_JSON_B64" | base64 -d >"$provider_auth_file" ||
    fail 'KODEX_OPENAI_AUTH_JSON_B64 is invalid'
  chmod 0600 "$provider_auth_file"
fi
if component_selected secrets; then
  jq -e 'type == "object" and length > 0' "$provider_auth_file" >/dev/null ||
    fail 'OpenAI authorization JSON is invalid'
fi

if component_selected cert-manager; then
  "$repository_root/tools/install/bootstrap-cert-manager.sh" \
    --context "$KODEX_KUBE_CONTEXT" --mode apply \
    --acme-email "$KODEX_ACME_EMAIL" --ingress-class "$KODEX_INGRESS_CLASS" \
    --acme-server "${KODEX_ACME_SERVER:-https://acme-v02.api.letsencrypt.org/directory}"
fi

if component_selected identity || component_selected management; then
  if [[ ! -d "$material_directory/identity" && ! -d "$material_directory/management" ]]; then
    for binding in \
      KODEX_KEYCLOAK_ADMIN_USERNAME:admin-username \
      KODEX_KEYCLOAK_ADMIN_INITIAL_PASSWORD:admin-password \
      KODEX_OWNER_USERNAME:owner-username \
      KODEX_OWNER_EMAIL:owner-email \
      KODEX_OWNER_INITIAL_PASSWORD:owner-password; do
      key=${binding%%:*}; name=${binding#*:}
      write_env_input "$key" "$material_directory/inputs/$name"
    done
    "$repository_root/tools/deploy/generate-identity-material.sh" \
      --material-directory "$material_directory" \
      --admin-username-file "$material_directory/inputs/admin-username" \
      --admin-initial-password-file "$material_directory/inputs/admin-password" \
      --owner-username-file "$material_directory/inputs/owner-username" \
      --owner-email-file "$material_directory/inputs/owner-email" \
      --owner-initial-password-file "$material_directory/inputs/owner-password"
  fi
  [[ -d "$material_directory/identity" && -d "$material_directory/management" ]] ||
    fail 'identity installation material is incomplete'
fi

if component_selected identity; then
  "$repository_root/tools/deploy/materialize-identity-secrets.sh" \
    --context "$KODEX_KUBE_CONTEXT" --material-directory "$material_directory"
  "$repository_root/infra/identity/bootstrap.sh" --context "$KODEX_KUBE_CONTEXT" \
    --mode apply --oidc-host "$KODEX_OIDC_HOST" \
    --ingress-class "$KODEX_INGRESS_CLASS" --cluster-issuer "$KODEX_CLUSTER_ISSUER" \
    --ingress-namespace "$KODEX_INGRESS_NAMESPACE" --ingress-pod-name "$KODEX_INGRESS_POD_NAME"
  "$repository_root/tools/deploy/configure-keycloak.sh" --context "$KODEX_KUBE_CONTEXT" \
    --mode apply --public-origin "https://$KODEX_CONTROL_HOST" \
    --grafana-origin "https://$KODEX_GRAFANA_HOST" \
    --headlamp-origin "https://$KODEX_HEADLAMP_HOST"
  "$repository_root/tools/deploy/configure-keycloak.sh" --context "$KODEX_KUBE_CONTEXT" \
    --mode readback --public-origin "https://$KODEX_CONTROL_HOST" \
    --grafana-origin "https://$KODEX_GRAFANA_HOST" \
    --headlamp-origin "https://$KODEX_HEADLAMP_HOST"
fi

if component_selected trust; then
  "$repository_root/infra/service-infrastructure/bootstrap.sh" \
    --context "$KODEX_KUBE_CONTEXT" --mode apply-controllers
  "$repository_root/infra/service-infrastructure/bootstrap.sh" \
    --context "$KODEX_KUBE_CONTEXT" --mode readback
fi

api_service_ip=$(kubectl -n default get service kubernetes -o json | jq -er '.spec.clusterIP')
api_endpoint_cidrs=$(kubectl -n default get endpoints kubernetes -o json | jq -er '
  [.subsets[]?.addresses[]?.ip | . + "/32"] | unique | sort | select(length > 0) | join(",")
')
api_endpoint_ports=$(kubectl -n default get endpoints kubernetes -o json | jq -er '
  [.subsets[]?.ports[]?.port | tostring] | unique | sort | select(length > 0) | join(",")
')

if component_selected management; then
  kodex_require_env KODEX_PUBLIC_IPV4_CIDR || exit 1
  "$repository_root/infra/management-surfaces/bootstrap.sh" \
    --context "$KODEX_KUBE_CONTEXT" --mode apply-monitoring \
    --oidc-issuer "https://$KODEX_OIDC_HOST/realms/kodex" \
    --control-center-host "$KODEX_CONTROL_HOST" --grafana-host "$KODEX_GRAFANA_HOST" \
    --headlamp-host "$KODEX_HEADLAMP_HOST" --public-ipv4-cidr "$KODEX_PUBLIC_IPV4_CIDR" \
    --ingress-class "$KODEX_INGRESS_CLASS" --cluster-issuer "$KODEX_CLUSTER_ISSUER" \
    --ingress-namespace "$KODEX_INGRESS_NAMESPACE" --ingress-pod-name "$KODEX_INGRESS_POD_NAME" \
    --kubernetes-api-service-cidr "$api_service_ip/32" \
    --kubernetes-api-endpoint-cidrs "$api_endpoint_cidrs" \
    --kubernetes-api-endpoint-ports "$api_endpoint_ports"
  "$repository_root/infra/management-surfaces/bootstrap.sh" \
    --context "$KODEX_KUBE_CONTEXT" --mode apply-surfaces \
    --oidc-issuer "https://$KODEX_OIDC_HOST/realms/kodex" \
    --control-center-host "$KODEX_CONTROL_HOST" --grafana-host "$KODEX_GRAFANA_HOST" \
    --headlamp-host "$KODEX_HEADLAMP_HOST" --public-ipv4-cidr "$KODEX_PUBLIC_IPV4_CIDR" \
    --ingress-class "$KODEX_INGRESS_CLASS" --cluster-issuer "$KODEX_CLUSTER_ISSUER" \
    --ingress-namespace "$KODEX_INGRESS_NAMESPACE" --ingress-pod-name "$KODEX_INGRESS_POD_NAME" \
    --kubernetes-api-service-cidr "$api_service_ip/32" \
    --kubernetes-api-endpoint-cidrs "$api_endpoint_cidrs" \
    --kubernetes-api-endpoint-ports "$api_endpoint_ports"
fi

registry_docker_config="$material_directory/github/release-registry-dockerconfig.json"
if any_component_selected arc platform; then
  install -m 0600 "$material_directory/registry/release-source/dockerconfig.json" \
    "$registry_docker_config"
fi
if component_selected registry; then
  kubectl create namespace kodex-infra --dry-run=client -o yaml |
    kubectl apply --server-side --field-manager=kodex-install -f - >/dev/null
  "$repository_root/infra/bootstrap-registry/bootstrap.sh" \
    --context "$KODEX_KUBE_CONTEXT" --mode apply \
    --registry-host "$KODEX_REGISTRY_HOST" --ingress-class "$KODEX_INGRESS_CLASS" \
    --cluster-issuer "$KODEX_CLUSTER_ISSUER" \
    --username-file "$material_directory/registry/release-source/username" \
    --password-file "$material_directory/registry/release-source/password" \
    --docker-config-output "$registry_docker_config"
  chmod 0600 "$registry_docker_config"
  "$repository_root/infra/bootstrap-registry/bootstrap.sh" \
    --context "$KODEX_KUBE_CONTEXT" --mode readback \
    --registry-host "$KODEX_REGISTRY_HOST" --ingress-class "$KODEX_INGRESS_CLASS" \
    --cluster-issuer "$KODEX_CLUSTER_ISSUER" \
    --username-file "$material_directory/registry/release-source/username" \
    --password-file "$material_directory/registry/release-source/password"
  if [[ "$KODEX_INSTALL_MODE" == bare-metal ]]; then
    "$repository_root/tools/install/configure-node-registry.sh" --mode apply \
      --registry-host "$KODEX_REGISTRY_HOST" \
      --username-file "$material_directory/registry/release-source/username" \
      --password-file "$material_directory/registry/release-source/password" \
      --promoted-pull-host "$KODEX_PROMOTED_PULL_HOST" \
      --promoted-pull-username-file "$material_directory/registry/pull/username" \
      --promoted-pull-password-file "$material_directory/registry/pull/password"
  fi
fi

if component_selected secrets; then
  "$repository_root/tools/install/materialize-secrets.sh" \
    --context "$KODEX_KUBE_CONTEXT" --material-directory "$material_directory" \
    --oidc-ca-file /etc/ssl/certs/ca-certificates.crt \
    --provider-auth-file "$provider_auth_file"
  if [[ -n "${KODEX_SENTRY_DSN:-}" ]]; then
    for sentry_secret in kodex-sentry internal-rpc-authority-sentry; do
      kubectl -n kodex-system create secret generic "$sentry_secret" \
        --from-literal="dsn=$KODEX_SENTRY_DSN" --dry-run=client -o yaml |
        kubectl apply --server-side --force-conflicts \
          --field-manager=kodex-install -f - >/dev/null
    done
  fi
  if [[ "${KODEX_ENABLE_EXTERNAL_S3:-false}" == true ]]; then
    kubectl -n kodex-system create secret generic kodex-external-s3 \
      --from-literal=endpoint="$KODEX_S3_ENDPOINT" \
      --from-literal=region="$KODEX_S3_REGION" \
      --from-literal=bucket="$KODEX_S3_BUCKET" \
      --from-literal=access-key="$KODEX_S3_ACCESS_KEY" \
      --from-literal=secret-key="$KODEX_S3_SECRET_KEY" \
      --dry-run=client -o yaml |
      kubectl apply --server-side --force-conflicts \
        --field-manager=kodex-install -f - >/dev/null
  fi
fi

workflow_sha_file="$material_directory/github/workflow-sha"
owner_actor_id_file="$material_directory/github/owner-actor-id"
if any_component_selected arc platform; then
  printf '%s' "$(git -C "$repository_root" rev-parse HEAD)" >"$workflow_sha_file"
  chmod 0600 "$workflow_sha_file"
  [[ -s "$registry_docker_config" ]] || fail 'release registry Docker configuration is absent'
  "$repository_root/tools/install/configure-github.sh" \
    --context "$KODEX_KUBE_CONTEXT" --mode apply \
    --owner-pat-file "$material_directory/inputs/github-owner-pat" \
    --workflow-sha-file "$workflow_sha_file" \
    --registry-docker-config-file "$registry_docker_config" \
    --owner-actor-id-file "$owner_actor_id_file"
  "$repository_root/tools/install/configure-github.sh" \
    --context "$KODEX_KUBE_CONTEXT" --mode readback \
    --owner-pat-file "$material_directory/inputs/github-owner-pat" \
    --workflow-sha-file "$workflow_sha_file" \
    --registry-docker-config-file "$registry_docker_config" \
    --owner-actor-id-file "$owner_actor_id_file"
fi

if component_selected arc; then
  "$repository_root/infra/arc/bootstrap.sh" --context "$KODEX_KUBE_CONTEXT" \
    --mode apply --github-pat-file "$material_directory/inputs/github-arc-token" \
    --registry-namespace kodex-infra --release-registry-host "$KODEX_REGISTRY_HOST" \
    --ingress-namespace "$KODEX_INGRESS_NAMESPACE" \
    --ingress-pod-name "$KODEX_INGRESS_POD_NAME" \
    --ingress-service-name "$KODEX_INGRESS_SERVICE_NAME" \
    --workflow-sha-file "$workflow_sha_file" \
    --build-owner-actor-id-file "$owner_actor_id_file" \
    --deploy-owner-actor-id-file "$owner_actor_id_file"
fi

if component_selected platform; then
  "$repository_root/tools/install/release-platform.sh" --context "$KODEX_KUBE_CONTEXT" \
    --owner-pat-file "$material_directory/inputs/github-owner-pat" \
    --workflow-sha-file "$workflow_sha_file" --profile web-only \
    --public-tls-mode "$KODEX_PUBLIC_TLS_MODE"
fi

if component_selected management; then
  "$repository_root/infra/management-surfaces/bootstrap.sh" \
    --context "$KODEX_KUBE_CONTEXT" --mode readback \
    --oidc-issuer "https://$KODEX_OIDC_HOST/realms/kodex" \
    --control-center-host "$KODEX_CONTROL_HOST" --grafana-host "$KODEX_GRAFANA_HOST" \
    --headlamp-host "$KODEX_HEADLAMP_HOST" --public-ipv4-cidr "$KODEX_PUBLIC_IPV4_CIDR" \
    --ingress-class "$KODEX_INGRESS_CLASS" --cluster-issuer "$KODEX_CLUSTER_ISSUER" \
    --ingress-namespace "$KODEX_INGRESS_NAMESPACE" --ingress-pod-name "$KODEX_INGRESS_POD_NAME" \
    --kubernetes-api-service-cidr "$api_service_ip/32" \
    --kubernetes-api-endpoint-cidrs "$api_endpoint_cidrs" \
    --kubernetes-api-endpoint-ports "$api_endpoint_ports"
fi

printf 'Kodex installation completed: mode=%s components=%s\n' \
  "$KODEX_INSTALL_MODE" "$components"
