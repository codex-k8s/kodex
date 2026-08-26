#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Release render failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --lock <release-lock.json> --lock-sha256 <64-hex> --output <render.yaml>" \
    '  --public-host <dns> --public-origin <https://dns>' \
    '  [--control-tls-recovery-host <different-dns>]' \
    '  --oidc-issuer <https-url> --oidc-jwks-url <https-url>' \
    '  --oidc-connect-address <host:port> --oidc-tls-server-name <dns>' \
    '  --promoted-pull-host <dns>' \
    '  --kubernetes-api-service-cidr <ipv4/32|ipv6/128>' \
    '  --kubernetes-api-endpoint-cidrs <host-cidr[,host-cidr...]>' \
    '  --kubernetes-api-endpoint-ports <port[,port...]>' \
    '  --ingress-class <name> --cluster-issuer <name>' \
    '  --ingress-namespace <name> --ingress-pod-name <label-value>' \
    '  --oidc-namespace <name> --oidc-pod-name <label-value>' \
    '  --oidc-pod-component <label-value> --oidc-target-port <port>' \
    '  [--disable-observability]' \
    '  [--profile <web-only|web-with-mattermost>]' \
    '  [--mattermost-host <dns>; required for web-with-mattermost]' >&2
}

lock_file=""
lock_sha256=""
output=""
public_host=""
public_origin=""
control_tls_recovery_host=""
oidc_issuer=""
oidc_jwks_url=""
oidc_connect_address=""
oidc_tls_server_name=""
promoted_pull_host=""
kubernetes_api_service_cidr=""
kubernetes_api_endpoint_cidrs=""
kubernetes_api_endpoint_ports=""
ingress_class=""
cluster_issuer=""
ingress_namespace=""
ingress_pod_name=""
oidc_namespace=""
oidc_pod_name=""
oidc_pod_component=""
oidc_target_port=""
disable_observability=false
mattermost_host=""
profile="web-only"

while (($# > 0)); do
  case "$1" in
    --lock) lock_file="${2:-}"; shift 2 ;;
    --lock-sha256) lock_sha256="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --public-host) public_host="${2:-}"; shift 2 ;;
    --public-origin) public_origin="${2:-}"; shift 2 ;;
    --control-tls-recovery-host) control_tls_recovery_host="${2:-}"; shift 2 ;;
    --oidc-issuer) oidc_issuer="${2:-}"; shift 2 ;;
    --oidc-jwks-url) oidc_jwks_url="${2:-}"; shift 2 ;;
    --oidc-connect-address) oidc_connect_address="${2:-}"; shift 2 ;;
    --oidc-tls-server-name) oidc_tls_server_name="${2:-}"; shift 2 ;;
    --promoted-pull-host) promoted_pull_host="${2:-}"; shift 2 ;;
    --kubernetes-api-service-cidr) kubernetes_api_service_cidr="${2:-}"; shift 2 ;;
    --kubernetes-api-endpoint-cidrs) kubernetes_api_endpoint_cidrs="${2:-}"; shift 2 ;;
    --kubernetes-api-endpoint-ports) kubernetes_api_endpoint_ports="${2:-}"; shift 2 ;;
    --ingress-class) ingress_class="${2:-}"; shift 2 ;;
    --cluster-issuer) cluster_issuer="${2:-}"; shift 2 ;;
    --ingress-namespace) ingress_namespace="${2:-}"; shift 2 ;;
    --ingress-pod-name) ingress_pod_name="${2:-}"; shift 2 ;;
    --oidc-namespace) oidc_namespace="${2:-}"; shift 2 ;;
    --oidc-pod-name) oidc_pod_name="${2:-}"; shift 2 ;;
    --oidc-pod-component) oidc_pod_component="${2:-}"; shift 2 ;;
    --oidc-target-port) oidc_target_port="${2:-}"; shift 2 ;;
    --disable-observability) disable_observability=true; shift ;;
    --mattermost-host) mattermost_host="${2:-}"; shift 2 ;;
    --profile) profile="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -r "$lock_file" ]] || fail 'release lock is not readable'
[[ "$profile" == "web-only" || "$profile" == "web-with-mattermost" ]] || fail 'release profile is invalid'
[[ "$lock_sha256" =~ ^[a-f0-9]{64}$ && "$lock_sha256" != 0000000000000000000000000000000000000000000000000000000000000000 ]] ||
  fail 'release lock SHA-256 is invalid'
[[ -n "$output" ]] || fail 'output path is required'
[[ "$public_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$public_host" == *.* ]] || fail 'public host is invalid'
[[ "$public_origin" == "https://$public_host" ]] || fail 'public origin must be the exact HTTPS public host'
if [[ -n "$control_tls_recovery_host" ]]; then
  [[ "$control_tls_recovery_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ &&
    "$control_tls_recovery_host" == *.* ]] ||
    fail 'Control Center TLS recovery host is invalid'
  [[ "$control_tls_recovery_host" != "$public_host" ]] ||
    fail 'Control Center TLS recovery host must differ from the public host'
fi
[[ "$oidc_issuer" =~ ^https://[a-zA-Z0-9._:-]+(/[^[:space:]]*)?$ ]] || fail 'OIDC issuer is invalid'
[[ "$oidc_jwks_url" =~ ^https://[a-zA-Z0-9._:-]+(/[^[:space:]]*)?$ ]] || fail 'OIDC JWKS URL is invalid'
[[ "$oidc_connect_address" =~ ^[a-zA-Z0-9._-]+:[1-9][0-9]{0,4}$ ]] || fail 'OIDC connect address is invalid'
[[ "$oidc_tls_server_name" =~ ^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$ ]] ||
  fail 'OIDC TLS server name is invalid'
[[ "$promoted_pull_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$promoted_pull_host" == *.* ]] ||
  fail 'promoted pull host is invalid'
for dns_label in "$ingress_class" "$cluster_issuer" "$ingress_namespace" "$ingress_pod_name" \
  "$oidc_namespace" "$oidc_pod_name" "$oidc_pod_component"; do
  [[ "$dns_label" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail 'deployment selector is invalid'
done
[[ "$oidc_target_port" =~ ^[1-9][0-9]{0,4}$ && "$oidc_target_port" -le 65535 ]] ||
  fail 'OIDC target port is invalid'
if [[ "$profile" == "web-with-mattermost" ]]; then
  [[ "$mattermost_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$mattermost_host" == *.* ]] ||
    fail 'Mattermost host is required and must be an exact lowercase DNS name'
elif [[ -n "$mattermost_host" ]]; then
  fail 'Mattermost host is forbidden for the web-only profile'
fi

for command_name in go kubectl yq jq sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
go run "$repository_root/tools/release/validate-host-cidr.go" "$kubernetes_api_service_cidr" >/dev/null ||
  fail 'Kubernetes API Service CIDR is invalid'

[[ -n "$kubernetes_api_endpoint_cidrs" &&
  ! "$kubernetes_api_endpoint_cidrs" =~ [[:space:]] &&
  "$kubernetes_api_endpoint_cidrs" != ,* &&
  "$kubernetes_api_endpoint_cidrs" != *, &&
  "$kubernetes_api_endpoint_cidrs" != *,,* ]] ||
  fail 'Kubernetes API endpoint CIDRs must be a nonempty comma-separated list without whitespace'
IFS=',' read -r -a api_endpoint_cidrs <<<"$kubernetes_api_endpoint_cidrs"
((${#api_endpoint_cidrs[@]} >= 1 && ${#api_endpoint_cidrs[@]} <= 16)) ||
  fail 'Kubernetes API endpoint CIDRs must contain between one and 16 values'
declare -A seen_api_endpoint_cidrs=()
for cidr in "${api_endpoint_cidrs[@]}"; do
  go run "$repository_root/tools/release/validate-host-cidr.go" "$cidr" >/dev/null ||
    fail "Kubernetes API endpoint CIDR is invalid: $cidr"
  [[ "$cidr" != "$kubernetes_api_service_cidr" ]] ||
    fail 'Kubernetes API endpoint CIDRs must not repeat the Service CIDR'
  [[ -z "${seen_api_endpoint_cidrs[$cidr]:-}" ]] ||
    fail "Kubernetes API endpoint CIDR is duplicated: $cidr"
  seen_api_endpoint_cidrs[$cidr]=true
done

[[ -n "$kubernetes_api_endpoint_ports" &&
  ! "$kubernetes_api_endpoint_ports" =~ [[:space:]] &&
  "$kubernetes_api_endpoint_ports" != ,* &&
  "$kubernetes_api_endpoint_ports" != *, &&
  "$kubernetes_api_endpoint_ports" != *,,* ]] ||
  fail 'Kubernetes API endpoint ports must be a nonempty comma-separated list without whitespace'
IFS=',' read -r -a api_endpoint_ports <<<"$kubernetes_api_endpoint_ports"
((${#api_endpoint_ports[@]} >= 1 && ${#api_endpoint_ports[@]} <= 8)) ||
  fail 'Kubernetes API endpoint ports must contain between one and eight values'
declare -A seen_api_endpoint_ports=()
for port in "${api_endpoint_ports[@]}"; do
  [[ "$port" =~ ^[1-9][0-9]{0,4}$ ]] ||
    fail "Kubernetes API endpoint port is invalid: $port"
  ((10#$port <= 65535)) ||
    fail "Kubernetes API endpoint port is invalid: $port"
  [[ -z "${seen_api_endpoint_ports[$port]:-}" ]] ||
    fail "Kubernetes API endpoint port is duplicated: $port"
  seen_api_endpoint_ports[$port]=true
done

source_sha=$(jq -er '.source_sha' "$lock_file")
"$script_directory/validate-release-lock.sh" \
  --lock "$lock_file" --source-sha "$source_sha" --sha256 "$lock_sha256" --profile "$profile" >/dev/null

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
rendered="$temporary_directory/$profile.yaml"
kubectl kustomize "$repository_root/deploy/k8s/profiles/$profile" >"$rendered"

while IFS=$'\t' read -r component pull_ref; do
  COMPONENT="$component" PULL_REF="$pull_ref" yq -i '
    (.. | select(tag == "!!str")) |= sub(
      "[A-Za-z0-9._:/-]+/" + strenv(COMPONENT) + "@sha256:[a-f0-9]{64}";
      strenv(PULL_REF)
    )
  ' "$rendered"
done < <(jq -r '.images[] | [.component,.pull_ref] | @tsv' "$lock_file")

registry_push=$(jq -er '.registry.push' "$lock_file")
node_pull=$(jq -er '.registry.node_pull' "$lock_file")
repository_prefix=$(jq -er '.registry.repository_prefix' "$lock_file")
release_source_registry=${registry_push%%/*}
release_source_hostname=${release_source_registry%:443}
agent_runner_ref=$(jq -er '.images[] | select(.component == "agent-runner") | .pull_ref' "$lock_file")
agent_runner_digest=$(jq -er '.images[] | select(.component == "agent-runner") | .digest' "$lock_file")
control_plane_digest=$(jq -er '.images[] | select(.component == "control-plane") | .digest' "$lock_file")
role_base_documents_digest=$(jq -er '.images[] | select(.component == "role-base-documents") | .digest' "$lock_file")
frontend_digest=$(jq -er '.images[] | select(.component == "dockerfile") | .digest' "$lock_file")
role_input_manifest_digest=$(jq -er '.role_image_input.manifest_digest' "$lock_file")
role_input_payload_sha256=$(jq -er '.role_image_input.payload_sha256' "$lock_file")
role_input_source_sha256=$(jq -er '.role_image_input.source_sha256' "$lock_file")
authority_ref=$(jq -er '.images[] | select(.component == "internal-rpc-authority") | .pull_ref' "$lock_file")
admission_ref=$(jq -er '.images[] | select(.component == "image-admission") | .pull_ref' "$lock_file")
admission_tools_ref=$(jq -er '.external_images[] | select(.component == "admission-tools") | .pull_ref' "$lock_file")
admission_tools_digest=$(jq -er '.external_images[] | select(.component == "admission-tools") | .digest' "$lock_file")
frontend_sha256=${frontend_digest#sha256:}
oidc_origin=$(printf '%s\n' "$oidc_issuer" | sed -E 's#^(https://[^/]+).*$#\1#')

PUBLIC_HOST="$public_host" \
PUBLIC_ORIGIN="$public_origin" \
OIDC_ISSUER="$oidc_issuer" \
OIDC_JWKS_URL="$oidc_jwks_url" \
OIDC_CONNECT_ADDRESS="$oidc_connect_address" \
OIDC_TLS_SERVER_NAME="$oidc_tls_server_name" \
OIDC_ORIGIN="$oidc_origin" \
PULL_REGISTRY_HOST="$promoted_pull_host" \
KUBERNETES_API_SERVICE_CIDR="$kubernetes_api_service_cidr" yq -i '
  (.. | select(tag == "!!str")) |= (
    sub("__KODEX_PUBLIC_HOST__"; strenv(PUBLIC_HOST)) |
    sub("__KODEX_PUBLIC_ORIGIN__"; strenv(PUBLIC_ORIGIN)) |
    sub("__KODEX_OIDC_ISSUER__"; strenv(OIDC_ISSUER)) |
    sub("__KODEX_OIDC_JWKS_URL__"; strenv(OIDC_JWKS_URL)) |
    sub("__KODEX_OIDC_CONNECT_ADDRESS__"; strenv(OIDC_CONNECT_ADDRESS)) |
    sub("__KODEX_OIDC_TLS_SERVER_NAME__"; strenv(OIDC_TLS_SERVER_NAME)) |
    sub("__KODEX_OIDC_ORIGIN__"; strenv(OIDC_ORIGIN)) |
    sub("__KODEX_KUBERNETES_API_SERVICE_CIDR__"; strenv(KUBERNETES_API_SERVICE_CIDR)) |
    sub("registry-pull\\.invalid"; strenv(PULL_REGISTRY_HOST))
  )
' "$rendered"

if [[ -n "$control_tls_recovery_host" ]]; then
  CONTROL_TLS_RECOVERY_HOST="$control_tls_recovery_host" yq -i '
    with(select(.kind == "Certificate" and
      .metadata.name == "staff-control-center-public");
      .spec.dnsNames += [strenv(CONTROL_TLS_RECOVERY_HOST)])
  ' "$rendered"
fi
expected_control_tls_names=1
[[ -z "$control_tls_recovery_host" ]] || expected_control_tls_names=2
EXPECTED_CONTROL_TLS_NAMES="$expected_control_tls_names" \
PUBLIC_HOST="$public_host" RECOVERY_HOST="$control_tls_recovery_host" yq -e '
  select(.kind == "Certificate" and
    .metadata.name == "staff-control-center-public") |
  (.spec.dnsNames | length) == (strenv(EXPECTED_CONTROL_TLS_NAMES) | tonumber) and
  (.spec.dnsNames[0] == strenv(PUBLIC_HOST)) and
  (strenv(RECOVERY_HOST) == "" or .spec.dnsNames[1] == strenv(RECOVERY_HOST))
' "$rendered" >/dev/null || fail 'Control Center TLS DNS names are invalid'

api_endpoint_destinations=$(printf '%s\n' "${api_endpoint_cidrs[@]}" |
  jq -Rsc 'split("\n") | map(select(length > 0) | {ipBlock:{cidr:.}})')
api_endpoint_tcp_ports=$(printf '%s\n' "${api_endpoint_ports[@]}" |
  jq -Rsc 'split("\n") | map(select(length > 0) | {protocol:"TCP",port:tonumber})')
api_endpoint_rule=$(jq -cn \
  --argjson to "$api_endpoint_destinations" \
  --argjson ports "$api_endpoint_tcp_ports" \
  '{to:$to,ports:$ports}')
api_client_policy_count=$(yq -o=json '
  select(.kind == "NetworkPolicy" and (
    .metadata.name == "kodex-image-admission-controller-exact-paths" or
    .metadata.name == "runtime-controller-exact-paths" or
    .metadata.name == "internal-rpc-authority-publisher-exact-paths" or
    .metadata.name == "internal-rpc-authority-restore-controller-exact-paths" or
    .metadata.name == "internal-rpc-authority-restore-jobs-exact-paths" or
    .metadata.name == "internal-rpc-authority-restore-pitr-telemetry"
  )) | .metadata.name
' "$rendered" | jq -s 'length')
[[ "$api_client_policy_count" == "6" ]] ||
  fail 'release profile must contain exactly six Kubernetes API client policies'
KUBERNETES_API_ENDPOINT_RULE="$api_endpoint_rule" yq -i '
  with(select(.kind == "NetworkPolicy" and (
    .metadata.name == "kodex-image-admission-controller-exact-paths" or
    .metadata.name == "runtime-controller-exact-paths" or
    .metadata.name == "internal-rpc-authority-publisher-exact-paths" or
    .metadata.name == "internal-rpc-authority-restore-controller-exact-paths" or
    .metadata.name == "internal-rpc-authority-restore-jobs-exact-paths" or
    .metadata.name == "internal-rpc-authority-restore-pitr-telemetry"
  ));
    .spec.egress += [(strenv(KUBERNETES_API_ENDPOINT_RULE) | from_json)]
  )
' "$rendered"

if [[ "$disable_observability" == true ]]; then
  yq -i '
    with(select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "DaemonSet" or .kind == "Job");
      .spec.template.spec.containers[] |= (
        .env = ((.env // []) | map(select(.name != "OTEL_SDK_DISABLED")) +
          [{"name":"OTEL_SDK_DISABLED","value":"true"}])
      )
    ) |
    with(select(.kind == "CronJob");
      .spec.jobTemplate.spec.template.spec.containers[] |= (
        .env = ((.env // []) | map(select(.name != "OTEL_SDK_DISABLED")) +
          [{"name":"OTEL_SDK_DISABLED","value":"true"}])
      )
    )
  ' "$rendered"
fi

INGRESS_CLASS="$ingress_class" \
CLUSTER_ISSUER="$cluster_issuer" \
INGRESS_NAMESPACE="$ingress_namespace" \
INGRESS_POD_NAME="$ingress_pod_name" \
OIDC_NAMESPACE="$oidc_namespace" \
OIDC_POD_NAME="$oidc_pod_name" \
OIDC_POD_COMPONENT="$oidc_pod_component" \
OIDC_TARGET_PORT="$oidc_target_port" yq -i '
  (.. | select(tag == "!!str")) |= (
    sub("__KODEX_INGRESS_CLASS__"; strenv(INGRESS_CLASS)) |
    sub("__KODEX_CLUSTER_ISSUER__"; strenv(CLUSTER_ISSUER)) |
    sub("__KODEX_INGRESS_NAMESPACE__"; strenv(INGRESS_NAMESPACE)) |
    sub("__KODEX_INGRESS_POD_NAME__"; strenv(INGRESS_POD_NAME)) |
    sub("__KODEX_OIDC_NAMESPACE__"; strenv(OIDC_NAMESPACE)) |
    sub("__KODEX_OIDC_POD_NAME__"; strenv(OIDC_POD_NAME)) |
    sub("__KODEX_OIDC_POD_COMPONENT__"; strenv(OIDC_POD_COMPONENT))
  ) |
  with(select(.kind == "NetworkPolicy" and (
      .metadata.name == "control-api-gateway-exact-runtime-paths" or
      .metadata.name == "control-plane-exact-runtime-paths"
    ));
    (.spec.egress[] |
      select(.to[]?.namespaceSelector.matchLabels."kubernetes.io/metadata.name" == strenv(OIDC_NAMESPACE)) |
      .ports[0].port) = (strenv(OIDC_TARGET_PORT) | tonumber)
  )
' "$rendered"

if [[ "$profile" == "web-with-mattermost" ]]; then
  MATTERMOST_HOST="$mattermost_host" yq -i '
    with(select(.kind == "ConfigMap" and .metadata.name == "interaction-gateway-runtime");
      .data.INTERACTION_GATEWAY_ALLOWED_HOSTS = strenv(MATTERMOST_HOST)
    )
  ' "$rendered"
fi

LOCK_DIGEST="$lock_sha256" \
REGISTRY_PUSH="$registry_push" \
NODE_PULL="$node_pull" \
REPOSITORY_PREFIX="$repository_prefix" \
PULL_REGISTRY_HOST="$promoted_pull_host" \
AGENT_RUNNER_REF="$agent_runner_ref" \
AGENT_RUNNER_DIGEST="$agent_runner_digest" \
CONTROL_PLANE_DIGEST="$control_plane_digest" \
AUTHORITY_REF="$authority_ref" \
ADMISSION_REF="$admission_ref" \
ADMISSION_TOOLS_REF="$admission_tools_ref" \
ADMISSION_TOOLS_DIGEST="$admission_tools_digest" \
SOURCE_SHA="$source_sha" \
FRONTEND_SHA256="$frontend_sha256" yq -i '
  (.. | select(tag == "!!str")) |= sub(
    "[A-Za-z0-9._:/-]+/image-admission-tools@sha256:[a-f0-9]{64}";
    strenv(ADMISSION_TOOLS_REF)
  ) |
  with(select(.kind == "ConfigMap" and .metadata.name == "kodex-image-admission-policy");
    .metadata.annotations."kodex.dev/admission-tools-sha256" = strenv(ADMISSION_TOOLS_DIGEST) |
    .data.orchestrationRevision = strenv(SOURCE_SHA) |
    .data.toolsImage = strenv(ADMISSION_TOOLS_REF) |
    .data.admissionImage = strenv(ADMISSION_REF) |
    .data.authorityImage = strenv(AUTHORITY_REF) |
    .data.promotedPullRepository = (strenv(NODE_PULL) + "/" + strenv(REPOSITORY_PREFIX) + "/roles") |
    .data.pullRegistryHost = strenv(PULL_REGISTRY_HOST) |
    .data.pullCredentialGeneration = "1" |
    .data.nodeReadbackImage = (strenv(PULL_REGISTRY_HOST) + "/" + strenv(REPOSITORY_PREFIX) + "/agent-runner@" + strenv(AGENT_RUNNER_DIGEST)) |
    .data.roleImageInputRepository = "kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/role-image-inputs" |
    .data.policyRevision = "1" |
    .data.policySHA256 = strenv(LOCK_DIGEST) |
    .data.trustedRoleBaseRepository = "kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/agent-runner" |
    .data.trustedRoleBaseDigest = strenv(AGENT_RUNNER_DIGEST) |
    .data.builderSHA256 = "8c2ce26a3722e0cf4514fad4cfcd0e0f0f16214219ca7b73f3e1fcef74640ac4" |
    .data.frontendSHA256 = strenv(FRONTEND_SHA256) |
    .data.toolchainSHA256 = strenv(LOCK_DIGEST) |
    .data.roleRuntimeContractRevision = "1" |
    .data.roleRuntimeContractSHA256 = strenv(LOCK_DIGEST)
  ) |
  with(select(.kind == "ConfigMap" and .metadata.name == "role-image-builder-runtime");
    .data.ROLE_IMAGE_BUILDER_EXPECTED_TOOLCHAIN_SHA256 = strenv(LOCK_DIGEST)
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "control-plane");
    .spec.template.metadata.annotations."kodex.dev/agent-runtime-image-digest" = strenv(AGENT_RUNNER_DIGEST)
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "kodex-image-registry-pull");
    .spec.template.metadata.annotations."kodex.dev/pull-credential-generation" = "1" |
    (.spec.template.spec.containers[] |
      select(.name == "certificate-guard").env[] |
      select(.name == "READBACK_IMAGE").value) =
        (strenv(PULL_REGISTRY_HOST) + "/" + strenv(REPOSITORY_PREFIX) +
          "/control-plane@" + strenv(CONTROL_PLANE_DIGEST))
  )
' "$rendered"

admission_policy_json=$(yq -o=json -I=0 '
  select(.kind == "ConfigMap" and .metadata.name == "kodex-image-admission-policy") |
  .data
' "$rendered")
[[ -n "$admission_policy_json" && "$admission_policy_json" != "null" ]] ||
  fail 'image admission policy projection is absent'
ADMISSION_POLICY_JSON="$admission_policy_json" yq -i '
  with(select(
    .apiVersion == "supplychain.kodex.dev/v1alpha1" and
    .kind == "ImageAdmissionPolicyParameters" and
    .metadata.name == "kodex-image-admission-policy"
  );
    .spec = (strenv(ADMISSION_POLICY_JSON) | from_json)
  )
' "$rendered"

role_environment_catalog=$(jq -cn \
  --arg source_revision "$source_sha" \
  --arg source_sha256 "$role_input_source_sha256" \
  --arg manifest_digest "$role_input_manifest_digest" \
  --arg payload_sha256 "$role_input_payload_sha256" \
  --arg agent_runner_digest "$agent_runner_digest" \
  --arg documents_digest "$role_base_documents_digest" '
  {schemaVersion:1,
   context:{sourceRef:"urn:kodex:release-source",sourceRevision:$source_revision,
     sourceSha256:$source_sha256,
     contextRef:("oci://kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/role-image-inputs@" + $manifest_digest),
     contextSha256:$payload_sha256},
   environments:[
     {key:"standard",nameMessageKey:"role-environments.standard.name",
      descriptionMessageKey:"role-environments.standard.description",unavailableMessageKey:"",
      softwareMessageKeys:["role-environments.software.base"],recommended:true,available:true,
      customInstallationAllowed:false,
      baseImageReference:"kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/agent-runner",
      baseImageDigest:$agent_runner_digest,platforms:[{os:"linux",architecture:"amd64",variant:""}],packages:[],tools:[]},
     {key:"documents",nameMessageKey:"role-environments.documents.name",
      descriptionMessageKey:"role-environments.documents.description",unavailableMessageKey:"",
      softwareMessageKeys:["role-environments.software.base","role-environments.software.pdf",
        "role-environments.software.ocr","role-environments.software.office"],recommended:false,available:true,
      customInstallationAllowed:false,
      baseImageReference:"kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/role-base-documents",
      baseImageDigest:$documents_digest,platforms:[{os:"linux",architecture:"amd64",variant:""}],packages:[],tools:[]}]}')
ROLE_ENVIRONMENT_CATALOG="$role_environment_catalog" yq -i '
  with(select(.kind == "ConfigMap" and .metadata.name == "kodex-role-environments");
    .data."catalog.json" = strenv(ROLE_ENVIRONMENT_CATALOG)
  )
' "$rendered"

agent_runner_source_ref="$registry_push/$repository_prefix/agent-runner@$agent_runner_digest"
control_plane_source_ref="$registry_push/$repository_prefix/control-plane@$control_plane_digest"
role_base_documents_source_ref="$registry_push/$repository_prefix/role-base-documents@$role_base_documents_digest"
role_image_input_source_ref="$registry_push/$repository_prefix/role-image-inputs@$role_input_manifest_digest"
dockerfile_source_ref="$registry_push/$repository_prefix/dockerfile@$frontend_digest"
RELEASE_SOURCE_REGISTRY="$release_source_registry" \
SOURCE_SHA="$source_sha" \
CONTROL_PLANE_SOURCE_REF="$control_plane_source_ref" \
CONTROL_PLANE_DIGEST="$control_plane_digest" \
DOCKERFILE_SOURCE_REF="$dockerfile_source_ref" \
DOCKERFILE_DIGEST="$frontend_digest" \
AGENT_RUNNER_SOURCE_REF="$agent_runner_source_ref" \
AGENT_RUNNER_DIGEST="$agent_runner_digest" \
ROLE_BASE_DOCUMENTS_SOURCE_REF="$role_base_documents_source_ref" \
ROLE_BASE_DOCUMENTS_DIGEST="$role_base_documents_digest" \
ROLE_IMAGE_INPUT_SOURCE_REF="$role_image_input_source_ref" \
ROLE_IMAGE_INPUT_MANIFEST_DIGEST="$role_input_manifest_digest" yq -i '
  with(select(.kind == "Job" and .metadata.name == "release-artifact-materializer");
    (.spec.template.spec.containers[0].env[] | select(.name == "RELEASE_SOURCE_REGISTRY").value) = strenv(RELEASE_SOURCE_REGISTRY) |
    (.spec.template.spec.containers[0].env[] | select(.name == "RELEASE_SOURCE_SHA").value) = strenv(SOURCE_SHA) |
    (.spec.template.spec.containers[0].env[] | select(.name == "CONTROL_PLANE_SOURCE_REF").value) = strenv(CONTROL_PLANE_SOURCE_REF) |
    (.spec.template.spec.containers[0].env[] | select(.name == "CONTROL_PLANE_DIGEST").value) = strenv(CONTROL_PLANE_DIGEST) |
    (.spec.template.spec.containers[0].env[] | select(.name == "DOCKERFILE_SOURCE_REF").value) = strenv(DOCKERFILE_SOURCE_REF) |
    (.spec.template.spec.containers[0].env[] | select(.name == "DOCKERFILE_DIGEST").value) = strenv(DOCKERFILE_DIGEST) |
    (.spec.template.spec.containers[0].env[] | select(.name == "AGENT_RUNNER_SOURCE_REF").value) = strenv(AGENT_RUNNER_SOURCE_REF) |
    (.spec.template.spec.containers[0].env[] | select(.name == "AGENT_RUNNER_DIGEST").value) = strenv(AGENT_RUNNER_DIGEST) |
    (.spec.template.spec.containers[0].env[] | select(.name == "ROLE_BASE_DOCUMENTS_SOURCE_REF").value) = strenv(ROLE_BASE_DOCUMENTS_SOURCE_REF) |
    (.spec.template.spec.containers[0].env[] | select(.name == "ROLE_BASE_DOCUMENTS_DIGEST").value) = strenv(ROLE_BASE_DOCUMENTS_DIGEST) |
    (.spec.template.spec.containers[0].env[] | select(.name == "ROLE_IMAGE_INPUT_SOURCE_REF").value) = strenv(ROLE_IMAGE_INPUT_SOURCE_REF) |
    (.spec.template.spec.containers[0].env[] | select(.name == "ROLE_IMAGE_INPUT_MANIFEST_DIGEST").value) = strenv(ROLE_IMAGE_INPUT_MANIFEST_DIGEST)
  )
' "$rendered"

egress_revision="release-${source_sha:0:12}"
egress_policy=$(yq -r 'select(.kind == "ConfigMap" and (.metadata.name | test("^egress-gateway-policy-"))) | .data."policy.json"' "$rendered" |
  jq -cS --arg revision "$egress_revision" --arg hostname "$release_source_hostname" --arg mattermost "$mattermost_host" '
    .metadata.revision = $revision |
    .spec.destinations = ((.spec.destinations + [{hostname:$hostname,port:443}] +
      (if $mattermost == "" then [] else [{hostname:$mattermost,port:443}] end)) |
      unique_by([.hostname,.port]) | sort_by(.hostname,.port))
  ')
egress_policy_file="$temporary_directory/egress-policy.json"
printf '%s' "$egress_policy" >"$egress_policy_file"
egress_policy_digest=$(
  cd -- "$repository_root/services/external/egress-gateway"
  GOWORK=off go run ./cmd/policy-digest "$egress_policy_file"
)
egress_policy_name="egress-gateway-policy-${egress_policy_digest:0:12}"
EGRESS_POLICY="$egress_policy" EGRESS_REVISION="$egress_revision" EGRESS_DIGEST="$egress_policy_digest" \
EGRESS_POLICY_NAME="$egress_policy_name" yq -i '
  with(select(.kind == "ConfigMap" and (.metadata.name | test("^egress-gateway-policy-")));
    .metadata.name = strenv(EGRESS_POLICY_NAME) |
    .data."policy.json" = strenv(EGRESS_POLICY)
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "egress-gateway");
    .spec.template.metadata.annotations."kodex.dev/egress-policy-sha256" = strenv(EGRESS_DIGEST) |
    (.spec.template.spec.volumes[] | select(.name == "policy").configMap.name) = strenv(EGRESS_POLICY_NAME) |
    (.spec.template.spec.containers[] | select(.name == "egress-gateway").env[] |
      select(.name == "EGRESS_GATEWAY_EXPECTED_POLICY_REVISION").value) = strenv(EGRESS_REVISION) |
    (.spec.template.spec.containers[] | select(.name == "egress-gateway").env[] |
      select(.name == "EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST").value) = strenv(EGRESS_DIGEST)
  )
' "$rendered"

# Kustomize versions bundled with kubectl may classify newer cluster-scoped
# APIs as namespaced. Canonicalize their identity before duplicate detection
# and server-side apply.
yq -i '
  with(select(
    .kind == "Namespace" or
    .kind == "CustomResourceDefinition" or
    .kind == "ClusterRole" or
    .kind == "ClusterRoleBinding" or
    .kind == "ValidatingAdmissionPolicy" or
    .kind == "ValidatingAdmissionPolicyBinding" or
    .kind == "ValidatingWebhookConfiguration" or
    .kind == "MutatingWebhookConfiguration" or
    .kind == "ClusterIssuer" or
    .kind == "Bundle"
  ); del(.metadata.namespace))
' "$rendered"

# Canonicalize duplicate resources before validation. The web-only aggregate may
# include a shared ConfigMap through more than one component base, but the bytes
# must be identical.
yq -o=json '.' "$rendered" | jq -sc '
  map(select(.kind != null)) as $all |
  ($all | group_by([.apiVersion,.kind,(.metadata.namespace // ""),.metadata.name]) |
    map(select((map(tojson) | unique | length) > 1))) as $conflicts |
  if ($conflicts | length) > 0 then error("conflicting resource identities")
  else $all | unique_by([.apiVersion,.kind,(.metadata.namespace // ""),.metadata.name])[] end
' | yq -p=json -P >"$output"

if rg -n 'sha256:0{64}' "$output" >/dev/null; then
  fail 'render contains a zero image digest'
fi
if rg -n '__KODEX_[A-Z0-9_]+__|admission-tools\.invalid|registry-pull\.invalid|https://control\.invalid|control-api\.kodex\.local.*Ingress' "$output" >/dev/null; then
  fail 'render contains an unresolved deployment placeholder'
fi
if rg -n '\$\{[A-Z][A-Z0-9_]*IMAGE[A-Z0-9_]*\}' "$output" >/dev/null; then
  fail 'render contains an unresolved image variable'
fi

allowed_images="$temporary_directory/allowed-images.txt"
jq -r '.images[].pull_ref,.external_images[].pull_ref' "$lock_file" >"$allowed_images"
yq -N -r '.. | select(has("image")) | .image' "$output" | while IFS= read -r image_ref; do
  case "$image_ref" in
    */"$repository_prefix"/*)
      grep -Fx -- "$image_ref" "$allowed_images" >/dev/null ||
        fail "internal image is outside the release lock: $image_ref"
      ;;
  esac
  [[ "$image_ref" == *@sha256:* ]] || fail "mutable image reference is forbidden: $image_ref"
done

printf 'Web-only render created: %s\n' "$output"
