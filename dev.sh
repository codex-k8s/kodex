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
    '  [--state-directory <path>] [--cluster-marker <root-owned-path>]' \
    '  [--profile web-only|web-with-mattermost]' \
    '  [--expected-sha <40-hex-commit>]' >&2
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
cluster_marker=""
expected_sha=""
requested_profile=""
while (($# > 0)); do
  case "$1" in
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --resource-prefix) resource_prefix=${2:-}; shift 2 ;;
    --run-timeout-ms) run_timeout_ms=${2:-}; shift 2 ;;
    --cluster-marker) cluster_marker=${2:-}; shift 2 ;;
    --expected-sha) expected_sha=${2:-}; shift 2 ;;
    --profile) requested_profile=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
case "$command_name" in up|status|smoke|e2e|down) ;; *) usage; fail 'command is invalid' ;; esac
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
  fail 'state directory must be an exact safe absolute path'
case "$requested_profile" in ''|web-only|web-with-mattermost) ;; *) fail 'deployment profile is invalid' ;; esac
deployment_profile=${requested_profile:-web-only}
if [[ "$command_name" != down ]]; then
  deployment_profile=$("$repository_root/tools/dev/resolve-local-profile.sh" \
    "$requested_profile" "$state_directory/render.yaml")
fi
if [[ "${KODEX_DEV_TLS_MODE:-local-ca}" == public-acme ]]; then
  [[ -n "$cluster_marker" ]] || fail 'public development requires a disposable cluster marker'
  [[ -n "$expected_sha" ]] || fail 'public development requires an expected source SHA'
fi
if [[ "$command_name" == e2e ]]; then
  [[ "$resource_prefix" =~ ^[a-z0-9]([a-z0-9-]{2,38}[a-z0-9])$ ]] ||
    fail 'E2E resource prefix must be a lowercase 4-40 character slug'
  [[ "$run_timeout_ms" =~ ^[0-9]+$ && "$run_timeout_ms" -ge 60000 && "$run_timeout_ms" -le 1800000 ]] ||
    fail 'E2E run timeout must be between 60000 and 1800000 milliseconds'
fi
[[ -f "$kubeconfig" && -r "$kubeconfig" ]] || fail 'Kubernetes configuration is absent'
export KUBECONFIG=$kubeconfig
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'
[[ "$context" != *prod* && "$context" != *production* ]] || fail 'production context is forbidden'

capture_cluster_identity() {
  local config_json ca_data ca_file cluster_uid api_endpoint ca_sha256
  cluster_uid=$(kubectl get namespace kube-system -o jsonpath='{.metadata.uid}') ||
    fail 'Kubernetes cluster UID readback failed'
  [[ "$cluster_uid" =~ ^[A-Za-z0-9-]{16,128}$ ]] || fail 'Kubernetes cluster UID is invalid'
  config_json=$(kubectl config view --minify --raw -o json) ||
    fail 'Kubernetes cluster configuration readback failed'
  api_endpoint=$(jq -er '
    .clusters | select(length == 1) | .[0].cluster.server |
    select(type == "string" and test("^https://[^[:space:]]+$"))
  ' <<<"$config_json") || fail 'Kubernetes API endpoint is invalid'
  ca_data=$(jq -r '.clusters[0].cluster["certificate-authority-data"] // ""' \
    <<<"$config_json")
  if [[ -n "$ca_data" ]]; then
    ca_sha256=$(printf '%s' "$ca_data" | base64 --decode 2>/dev/null | sha256sum |
      awk '{print $1}') || fail 'Kubernetes CA data is invalid'
  else
    ca_file=$(jq -er '
      .clusters[0].cluster["certificate-authority"] |
      select(type == "string" and startswith("/"))
    ' <<<"$config_json") || fail 'Kubernetes CA reference is absent'
    [[ -f "$ca_file" && ! -L "$ca_file" ]] || fail 'Kubernetes CA file is invalid'
    ca_sha256=$(sha256sum -- "$ca_file" | awk '{print $1}')
  fi
  [[ "$ca_sha256" =~ ^[a-f0-9]{64}$ ]] || fail 'Kubernetes CA digest is invalid'
  jq -cn --arg cluster_uid "$cluster_uid" --arg api_endpoint "$api_endpoint" \
    --arg ca_sha256 "$ca_sha256" '{version:1,clusterUID:$cluster_uid,
      apiEndpoint:$api_endpoint,caSHA256:$ca_sha256}'
}

verify_disposable_cluster_marker() {
  local marker_stat marker_json current_json
  [[ "$cluster_marker" == /var/lib/kodex-dev/cluster-identity.json ]] ||
    fail 'disposable cluster marker path is invalid'
  if ! sudo -n test -f "$cluster_marker" || sudo -n test -L "$cluster_marker"; then
    fail 'disposable cluster marker is absent or unsafe'
  fi
  marker_stat=$(sudo -n stat -c '%u:%g:%a' -- "$cluster_marker") ||
    fail 'disposable cluster marker metadata readback failed'
  [[ "$marker_stat" == 0:0:600 ]] || fail 'disposable cluster marker ownership or mode is invalid'
  marker_json=$(sudo -n cat -- "$cluster_marker") ||
    fail 'disposable cluster marker readback failed'
  jq -e '
    .version == 1 and
    (.clusterUID | type == "string" and test("^[A-Za-z0-9-]{16,128}$")) and
    (.apiEndpoint | type == "string" and test("^https://[^[:space:]]+$")) and
    (.caSHA256 | type == "string" and test("^[a-f0-9]{64}$"))
  ' <<<"$marker_json" >/dev/null || fail 'disposable cluster marker is invalid'
  current_json=$(capture_cluster_identity)
  jq -e --argjson current "$current_json" '
    .clusterUID == $current.clusterUID and
    .apiEndpoint == $current.apiEndpoint and
    .caSHA256 == $current.caSHA256
  ' <<<"$marker_json" >/dev/null || fail 'Kubernetes cluster identity does not match the disposable marker'
}

if [[ -n "$expected_sha" ]]; then
  [[ "$expected_sha" =~ ^[a-f0-9]{40}$ ]] || fail 'expected source SHA is invalid'
  [[ "$(git -C "$repository_root" rev-parse HEAD)" == "$expected_sha" ]] ||
    fail 'source HEAD does not match the expected SHA'
fi
if [[ -n "$cluster_marker" ]]; then
  verify_disposable_cluster_marker
fi

if [[ "$command_name" == down ]]; then
  [[ "${KODEX_DEV_CONFIRM_DOWN:-}" == \
    I_UNDERSTAND_THIS_REMOVES_KODEX_FROM_THE_BOUND_DISPOSABLE_CLUSTER ]] ||
    fail 'down requires the exact disposable environment confirmation'
  local_admission_resources=(
    internal-rpc-authority-restore-anchor-forward-only
    internal-rpc-authority-restore-pitr-cluster-owner
    kodex-image-admission-controller-jobs
    kodex-image-admission-controller-workspaces
    runtime-execution-network-policy
    runtime-execution-rbac
    runtime-execution-service-account
    runtime-execution-ticket-exact-projection
    runtime-revision-exact-configmap-projection
    runtime-role-pod-exact-secret-projection
  )
  kubectl delete validatingadmissionpolicybindings.admissionregistration.k8s.io \
    "${local_admission_resources[@]}" --ignore-not-found --wait=true --timeout=2m >/dev/null
  kubectl delete validatingadmissionpolicies.admissionregistration.k8s.io \
    "${local_admission_resources[@]}" --ignore-not-found --wait=true --timeout=2m >/dev/null
  for namespace in kodex-runtime kodex-system identity kodex-trust; do
    kubectl get namespace "$namespace" >/dev/null 2>&1 || continue
    kubectl delete namespace "$namespace" --wait=false >/dev/null
    deadline=$((SECONDS + 600))
    while kubectl get namespace "$namespace" >/dev/null 2>&1; do
      ((SECONDS < deadline)) || fail "namespace deletion timed out: $namespace"
      sleep 1
    done
  done
  rm -f -- "$state_directory/render.yaml" "$state_directory/authority-source-state.json"
  printf 'Kodex local application namespaces removed; shared cluster controllers retained\n'
  exit 0
fi

api_endpoint_mode=readback
[[ "$command_name" == up ]] && api_endpoint_mode=apply
"$repository_root/tools/dev/configure-local-api-endpoint.sh" \
  --context "$context" --mode "$api_endpoint_mode"

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
    printf 'BASE_TREE\0%s\0' "$(git rev-parse 'HEAD^{tree}')"
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

live_workloads_match=false
verify_live_workload_source() {
  local render=$1 encoded workload kind namespace name live expected_projection actual_projection
  local projection
  projection='
    def kodex_annotations:
      (. // {} | with_entries(select(.key | startswith("kodex.dev/"))));
    def mounts:
      [(. // [])[] | {
        name,
        mountPath,
        readOnly: (.readOnly // false),
        subPath: (.subPath // ""),
        subPathExpr: (.subPathExpr // "")
      }] | sort_by(.name, .mountPath, .subPath, .subPathExpr);
    def containers:
      [(. // [])[] | {
        name,
        image,
        volumeMounts: (.volumeMounts | mounts)
      }] | sort_by(.name);
    {
      kind,
      namespace: (.metadata.namespace // "default"),
      name: .metadata.name,
      workloadAnnotations: (.metadata.annotations | kodex_annotations),
      templateAnnotations: (.spec.template.metadata.annotations | kodex_annotations),
      hostPaths: ([.spec.template.spec.volumes[]? |
        select(.hostPath != null) |
        {name, path: .hostPath.path, type: (.hostPath.type // "")}]
        | sort_by(.name, .path)),
      initContainers: (.spec.template.spec.initContainers | containers),
      containers: (.spec.template.spec.containers | containers)
    }
  '
  while IFS= read -r encoded; do
    workload=$(base64 --decode <<<"$encoded") || fail 'rendered workload projection is invalid'
    kind=$(jq -er '.kind | select(. == "Deployment" or . == "StatefulSet" or . == "DaemonSet")' <<<"$workload") ||
      fail 'rendered workload kind is invalid'
    namespace=$(jq -er '.metadata.namespace // "default"' <<<"$workload") ||
      fail 'rendered workload namespace is invalid'
    name=$(jq -er '.metadata.name | select(type == "string" and length > 0)' <<<"$workload") ||
      fail 'rendered workload name is invalid'
    live=$(kubectl -n "$namespace" get "$kind" "$name" -o json) ||
      fail "live workload is absent: $kind $namespace/$name"
    jq -e '.metadata.generation > 0 and .status.observedGeneration == .metadata.generation' <<<"$live" >/dev/null ||
      fail "live workload generation is not observed: $kind $namespace/$name"
    expected_projection=$(jq -cS "$projection" <<<"$workload") ||
      fail 'rendered workload source projection failed'
    actual_projection=$(jq -cS "$projection" <<<"$live") ||
      fail 'live workload source projection failed'
    [[ "$actual_projection" == "$expected_projection" ]] ||
      fail "live workload source projection differs from render: $kind $namespace/$name"
  done < <(yq -o=json -I=0 '.' "$render" | jq -sr '
    [.[] | select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "DaemonSet") |
      @base64] | .[]
  ')
  live_workloads_match=true
}

record_source_provenance_evidence() {
  local evidence_file=$1 evidence_command=$2 render_provenance current_revision
  local current_fingerprint source_dirty rendered_revision rendered_fingerprint rendered_dirty
  local render_matches sha_attested temporary_evidence
  [[ -f "$state_directory/render.yaml" && ! -L "$state_directory/render.yaml" ]] ||
    fail 'rendered source provenance is absent'
  render_provenance=$(yq -o=json -I=0 '
    select(.kind == "ConfigMap" and .metadata.namespace == "kodex-system" and
      .metadata.name == "kodex-dev-source-provenance")
  ' "$state_directory/render.yaml" | jq -sc '
    select(length == 1) | .[0].data |
    select((.sourceRevision | type == "string" and test("^[a-f0-9]{40}$")) and
      (.sourceContentSHA256 | type == "string" and test("^[a-f0-9]{64}$")) and
      (.sourceDirty == "true" or .sourceDirty == "false"))
  ') || fail 'rendered source provenance is invalid'
  [[ -n "$render_provenance" ]] || fail 'rendered source provenance is invalid'
  rendered_revision=$(jq -r '.sourceRevision' <<<"$render_provenance")
  rendered_fingerprint=$(jq -r '.sourceContentSHA256' <<<"$render_provenance")
  rendered_dirty=$(jq -r '.sourceDirty' <<<"$render_provenance")
  [[ "$(jq -r '.deploymentProfile // "web-only"' <<<"$render_provenance")" == "$deployment_profile" ]] ||
    fail 'rendered deployment profile does not match the selected profile'
  current_revision=$(git -C "$repository_root" rev-parse HEAD)
  current_fingerprint=$(calculate_local_source_fingerprint)
  [[ "$current_fingerprint" =~ ^[a-f0-9]{64}$ ]] || fail 'source content fingerprint is invalid'
  [[ "$rendered_revision" == "$current_revision" ]] ||
    fail 'rendered source revision does not match the current HEAD'
  if [[ -n "$expected_sha" && "$current_revision" != "$expected_sha" ]]; then
    fail 'source HEAD does not match the expected SHA'
  fi
  source_dirty=false
  [[ -z "$(git -C "$repository_root" status --porcelain --untracked-files=all)" ]] ||
    source_dirty=true
  render_matches=false
  [[ "$rendered_fingerprint" == "$current_fingerprint" ]] && render_matches=true
  sha_attested=false
  if [[ "$rendered_dirty" == false && "$source_dirty" == false && "$render_matches" == true ]]; then
    sha_attested=true
  fi
  install -d -m 0700 "$(dirname -- "$evidence_file")"
  temporary_evidence=$(mktemp "$(dirname -- "$evidence_file")/.source-provenance.XXXXXX")
  jq -n --arg command "$evidence_command" \
    --arg deployment_profile "$deployment_profile" \
    --arg expected_sha "$expected_sha" --arg head_sha "$current_revision" \
    --arg rendered_sha "$rendered_revision" \
    --arg rendered_content_sha256 "$rendered_fingerprint" \
    --arg current_content_sha256 "$current_fingerprint" \
    --argjson rendered_dirty "$rendered_dirty" --argjson dirty "$source_dirty" \
    --argjson render_matches "$render_matches" \
    --argjson live_workloads_match "$live_workloads_match" \
    --argjson sha_attested "$sha_attested" '
      {
        version: 1,
        command: $command,
        deploymentProfile: $deployment_profile,
        expectedSHA: (if $expected_sha == "" then null else $expected_sha end),
        headSHA: $head_sha,
        renderedSHA: $rendered_sha,
        renderedContentSHA256: $rendered_content_sha256,
        renderedDirty: $rendered_dirty,
        currentContentSHA256: $current_content_sha256,
        dirty: $dirty,
        renderContentMatches: $render_matches,
        liveWorkloadsMatch: $live_workloads_match,
        shaAttested: $sha_attested
      }
    ' >"$temporary_evidence"
  chmod 0600 "$temporary_evidence"
  mv -- "$temporary_evidence" "$evidence_file"
  printf 'Source HEAD: %s\nSource content SHA-256: %s\nSource SHA attested: %s\n' \
    "$current_revision" "$current_fingerprint" "$sha_attested"
}

require_exact_source_attestation() {
  local evidence_file=$1
  jq -e '
    .shaAttested == true and
    .renderContentMatches == true and
    .liveWorkloadsMatch == true and
    .renderedDirty == false and
    .dirty == false
  ' "$evidence_file" >/dev/null ||
    fail 'exact source SHA attestation is required for acceptance E2E'
}

resolve_local_authority_source_revision() {
  local current_revision source_fingerprint state_file state_revision state_fingerprint
  current_revision=$(read_authority_snapshot_revision)
  source_fingerprint=$(calculate_local_source_fingerprint)
  [[ "$source_fingerprint" =~ ^[a-f0-9]{64}$ ]] ||
    fail 'local source fingerprint is invalid'
  source_fingerprint=$(printf '%s\0%s\0' "$source_fingerprint" "$deployment_profile" | sha256sum | awk '{print $1}')
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
public_host=${KODEX_DEV_PUBLIC_HOST:-control.$dns_suffix}
oidc_host=${KODEX_DEV_OIDC_HOST:-sso.$dns_suffix}
grafana_host=${KODEX_DEV_GRAFANA_HOST:-grafana.$dns_suffix}
headlamp_host=${KODEX_DEV_HEADLAMP_HOST:-headlamp.$dns_suffix}
registry_host=${KODEX_DEV_REGISTRY_HOST:-registry.$dns_suffix}
promoted_pull_host=${KODEX_DEV_PROMOTED_PULL_HOST:-pull.$dns_suffix}
for host in "$public_host" "$oidc_host" "$grafana_host" "$headlamp_host" \
  "$registry_host" "$promoted_pull_host"; do
  [[ "$host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$host" == *.* ]] ||
    fail 'development public host is invalid'
done
tls_mode=${KODEX_DEV_TLS_MODE:-local-ca}
case "$tls_mode" in local-ca|public-acme) ;; *) fail 'development TLS mode is invalid' ;; esac
ingress_class=${KODEX_DEV_INGRESS_CLASS:-traefik}
cluster_issuer=${KODEX_DEV_CLUSTER_ISSUER:-kodex-local}
acme_email=${KODEX_DEV_ACME_EMAIL:-}
oidc_ca_file="$state_directory/kodex-local-ca.crt"
node_extra_ca_file="$state_directory/kodex-local-ca.crt"
provider_apparmor_profile=${KODEX_DEV_PROVIDER_APPARMOR_PROFILE:-}
[[ -z "$provider_apparmor_profile" || "$provider_apparmor_profile" == kodex-provider-runtime ]] ||
  fail 'KODEX_DEV_PROVIDER_APPARMOR_PROFILE is not approved'
if [[ "$tls_mode" == public-acme ]]; then
  [[ "$cluster_issuer" == letsencrypt-production ]] ||
    fail 'public development TLS requires letsencrypt-production'
  [[ "$acme_email" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]] ||
    fail 'ACME email is required for public development TLS'
  oidc_ca_file=/etc/ssl/certs/ca-certificates.crt
  node_extra_ca_file=""
fi
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
  --mode "$cluster_mode" --state-directory "$state_directory" \
  --tls-mode "$tls_mode" --acme-email "$acme_email" \
  --ingress-class "$ingress_class" --cluster-issuer "$cluster_issuer"

if [[ "$command_name" == up && "$tls_mode" == public-acme ]]; then
  "$repository_root/tools/dev/preflight-public-hosts.sh" \
    --hosts "${KODEX_DEV_PUBLIC_TLS_HOSTS:-$public_host,$oidc_host}" \
    --allowed-ipv4-addresses "${KODEX_DEV_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES:-}" \
    --allowed-ipv6-addresses "${KODEX_DEV_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES:-}" \
    --dns-timeout-seconds "${KODEX_DEV_PUBLIC_TLS_DNS_TIMEOUT_SECONDS:-10}" \
    --http-timeout-seconds "${KODEX_DEV_PUBLIC_TLS_HTTP_TIMEOUT_SECONDS:-10}" \
    --context "$context" --backend-address "${KODEX_DEV_KUBERNETES_API_ADDRESS:-10.254.254.1}"
fi

if [[ "$command_name" == status || "$command_name" == smoke || "$command_name" == e2e ]]; then
  source_evidence="$state_directory/source-provenance-$command_name.json"
  e2e_start_head=""
  e2e_start_fingerprint=""
  if [[ "$command_name" == e2e ]]; then
    source_evidence="$state_directory/e2e/$resource_prefix-source-provenance.json"
  fi
  record_source_provenance_evidence "$source_evidence" "$command_name"
  if [[ "$command_name" == e2e ]]; then
    e2e_start_head=$(jq -r '.headSHA' "$source_evidence")
    e2e_start_fingerprint=$(jq -r '.currentContentSHA256' "$source_evidence")
    "$repository_root/tools/dev/build-local-session-archive.sh" \
      --source-root "$repository_root" --state-directory "$state_directory"
    "$repository_root/tools/dev/build-local-stt.sh" \
      --source-root "$repository_root" --state-directory "$state_directory"
  fi
  "$repository_root/tools/dev/deploy-local.sh" --context "$context" --mode readback \
    --render "$state_directory/render.yaml" --state-directory "$state_directory" \
    --tls-mode "$tls_mode"
  verify_live_workload_source "$state_directory/render.yaml"
  record_source_provenance_evidence "$source_evidence" "$command_name"
  if [[ "$command_name" == e2e ]]; then
    require_exact_source_attestation "$source_evidence"
  fi
  if [[ "$command_name" == status ]]; then
    printf 'Control Center: https://%s\nCredentials: %s\n' "$public_host" "$credentials_file"
    exit 0
  fi
  frontend_directory="$repository_root/services/staff/control-center"
  install -d -m 0700 "$state_directory/e2e"
  if [[ ! -x "$frontend_directory/node_modules/.bin/playwright" ]]; then
    npm --prefix "$frontend_directory" ci
  fi
  "$repository_root/tools/dev/prepare-playwright-browser.sh" \
    --frontend-directory "$frontend_directory"
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
    NODE_EXTRA_CA_CERTS="$node_extra_ca_file" \
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
      KODEX_E2E_PRIVATE_OUTPUT_DIR="$state_directory/e2e/$resource_prefix-playwright" \
      KODEX_E2E_EXPECTED_SHA="$e2e_start_head" \
      KODEX_E2E_RUN_TIMEOUT_MS="$run_timeout_ms" \
      KODEX_E2E_KUBECONFIG="$kubeconfig" \
      KODEX_E2E_KUBE_CONTEXT="$context" \
      KODEX_E2E_REPOSITORY_ROOT="$repository_root" \
      KODEX_E2E_STATE_DIRECTORY="$state_directory" \
      NODE_EXTRA_CA_CERTS="$node_extra_ca_file" \
      npm --prefix "$frontend_directory" run test:e2e:discovery; then
      fail 'local browser E2E failed'
    fi
    record_source_provenance_evidence "$source_evidence" "$command_name"
    require_exact_source_attestation "$source_evidence"
    [[ "$(jq -r '.headSHA' "$source_evidence")" == "$e2e_start_head" &&
      "$(jq -r '.currentContentSHA256' "$source_evidence")" == "$e2e_start_fingerprint" ]] ||
      fail 'source content changed while E2E was running'
    temporary_source_evidence=$(mktemp "$state_directory/e2e/.source-provenance.XXXXXX")
    jq '.stableDuringCommand = true' "$source_evidence" >"$temporary_source_evidence"
    chmod 0600 "$temporary_source_evidence"
    mv -- "$temporary_source_evidence" "$source_evidence"
    jq -e --arg expected_sha "$e2e_start_head" '
      .version == 1 and .status == "passed" and
      .sourceSHA == $expected_sha and
      (.results | length) > 0 and all(.results[]; .status == "passed") and
      (.visualEvidence | length) == 6 and
      ([.visualEvidence[].name] | unique | length) == 6 and
      ([.visualEvidence[].viewport] | sort | unique) == ["1440x900", "1920x1080"] and
      all(.visualEvidence[];
        .bytes > 0 and (.sha256 | test("^[a-f0-9]{64}$")) and
        .sourceSHA == $expected_sha and
        (.name | test("^visual-(1440x900|1920x1080)-[a-z0-9-]+$")))
    ' "$report" >/dev/null || fail 'local browser E2E report is not fully successful'
    chmod 0600 "$run_state" "$report"
    "$repository_root/tools/dev/verify-discovery-readback.sh" \
      --context "$context" --kubeconfig "$kubeconfig" --state "$run_state" \
      --expect-account default-openai-codex \
      --expect-account openai-codex-account-2
    printf 'Kodex local full E2E completed: %s\nReport: %s\nSource evidence: %s\n' \
      "$resource_prefix" "$report" "$source_evidence"
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
  --oidc-host "$oidc_host" --ingress-class "$ingress_class" --cluster-issuer "$cluster_issuer" \
  --ingress-namespace kube-system --ingress-pod-name traefik
kubectl label namespace identity app.kubernetes.io/part-of=kodex kodex.dev/capability=identity \
  kodex.dev/environment=staging kodex.dev/local-profile=hot-reload --overwrite >/dev/null
if [[ "$tls_mode" == local-ca ]]; then
  kubectl -n identity patch serverstransport sso-public --type=merge \
    -p '{"spec":{"rootCAsSecrets":["sso-public-tls"]}}' >/dev/null
else
  kubectl -n identity patch serverstransport sso-public --type=merge \
    -p '{"spec":{"rootCAsSecrets":null}}' >/dev/null
fi
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
  --oidc-ca-file "$oidc_ca_file" \
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
"$repository_root/tools/dev/build-local-stt.sh" \
  --source-root "$repository_root" --state-directory "$state_directory"
stt_hot_reload_image=$(<"$state_directory/stt-hot-reload-image")
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
  --profile "$deployment_profile" \
  --cache-root "$state_directory/cache" --output "$state_directory/render.yaml" \
  --public-host "$public_host" --oidc-host "$oidc_host" \
  --ingress-class "$ingress_class" --cluster-issuer "$cluster_issuer" \
  --tls-mode "$tls_mode" \
  --kubernetes-service-cidr "$api_service_ip/32" \
  --kubernetes-endpoint-cidr "$api_endpoint_ip/32" \
  --kubernetes-endpoint-port "$api_endpoint_port" \
  --runner-image "$runner_image" \
  --session-archive-image "$session_archive_image" \
  --stt-hot-reload-image "$stt_hot_reload_image" \
  --backup-controller-image "$backup_controller_image" \
  --promoted-pull-host "$promoted_pull_host" \
  --role-image-builder-image "$role_image_builder_image" \
  --image-admission-image "$image_admission_image" \
  --image-admission-tools-image "$image_admission_tools_image" \
  --authority-image "$authority_image" \
  --provider-apparmor-profile "$provider_apparmor_profile" \
  --authority-source-revision "$authority_source_revision" \
  --role-image-input-manifest-digest "$role_image_input_manifest_digest" \
  --role-image-input-payload-sha256 "$role_image_input_payload_sha256" \
  --role-image-input-source-sha256 "$role_image_input_source_sha256"
record_source_provenance_evidence "$state_directory/source-provenance-up.json" up
"$repository_root/tools/dev/deploy-local.sh" --context "$context" --mode apply \
  --render "$state_directory/render.yaml" --state-directory "$state_directory" \
  --tls-mode "$tls_mode"
commit_local_authority_source_state

management_surface_arguments=(
  --context "$context"
  --oidc-issuer "https://$oidc_host/realms/kodex"
  --oidc-connect-address sso.identity.svc.cluster.local:443
  --oidc-target-port 8443
  --control-center-host "$public_host"
  --grafana-host "$grafana_host"
  --headlamp-host "$headlamp_host"
  --ingress-class "$ingress_class"
  --cluster-issuer "$cluster_issuer"
  --ingress-namespace kube-system
  --ingress-pod-name traefik
  --kubernetes-api-service-cidr "$api_service_ip/32"
  --kubernetes-api-endpoint-cidrs "$api_endpoint_ip/32"
  --kubernetes-api-endpoint-ports "$api_endpoint_port"
)
"$repository_root/infra/management-surfaces/bootstrap.sh" \
  --mode reconcile "${management_surface_arguments[@]}"

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
    --render "$state_directory/render.yaml" --state-directory "$state_directory" \
    --tls-mode "$tls_mode"
fi

"$repository_root/tools/deploy/configure-keycloak.sh" --context "$context" --mode readback \
  "${keycloak_origin_arguments[@]}"

printf '%s\n' \
  'Kodex local development is ready' \
  "Control Center: https://$public_host" \
  "Credentials: $credentials_file" \
  "Rendered manifest: $state_directory/render.yaml"
