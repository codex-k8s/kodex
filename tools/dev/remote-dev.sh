#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex remote development failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 host-preflight|host-apply|host-readback|up|status|smoke|e2e|down" \
    '  [--env-file <private-path>] [--resource-prefix <slug>]' \
    '  [--run-timeout-ms <milliseconds>] [--expected-sha <40-hex-commit>]' >&2
}

command_name=${1:-}
[[ -n "$command_name" ]] || { usage; exit 1; }
shift
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
env_file="$repository_root/.kodex-remote-env"
resource_prefix=""
run_timeout_ms=""
expected_sha=""
while (($# > 0)); do
  case "$1" in
    --env-file) env_file=${2:-}; shift 2 ;;
    --resource-prefix) resource_prefix=${2:-}; shift 2 ;;
    --run-timeout-ms) run_timeout_ms=${2:-}; shift 2 ;;
    --expected-sha) expected_sha=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
case "$command_name" in
  host-preflight|host-apply|host-readback|up|status|smoke|e2e|down) ;;
  *) usage; fail 'command is invalid' ;;
esac

# shellcheck source=tools/install/load-env.sh
source "$repository_root/tools/install/load-env.sh"
kodex_load_env "$env_file" || exit 1
KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES=${KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES:-}
kodex_require_env KODEX_REMOTE_SERVER_PUBLIC_IP KODEX_REMOTE_CONTROL_HOST \
  KODEX_REMOTE_OIDC_HOST KODEX_REMOTE_REGISTRY_HOST \
  KODEX_REMOTE_PROMOTED_PULL_HOST KODEX_REMOTE_ACME_EMAIL \
  KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES || exit 1
KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES=${KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES:-}
KODEX_REMOTE_GRAFANA_HOST=${KODEX_REMOTE_GRAFANA_HOST:-grafana.kodex.works}
KODEX_REMOTE_HEADLAMP_HOST=${KODEX_REMOTE_HEADLAMP_HOST:-headlamp.kodex.works}
state_directory=${KODEX_REMOTE_STATE_DIRECTORY:-/srv/kodex-dev/state}
context=${KODEX_REMOTE_KUBE_CONTEXT:-default}
cluster_marker=/var/lib/kodex-dev/cluster-identity.json
legacy_kubeconfig="$HOME/.kube/kodex-dev-remote"
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
  fail 'remote state directory is invalid'

temporary_kubeconfig_directory=""
cleanup() {
  if [[ -n "$temporary_kubeconfig_directory" ]]; then
    rm -rf -- "$temporary_kubeconfig_directory"
  fi
}
trap cleanup EXIT

create_temporary_kubeconfig() {
  [[ -z "$temporary_kubeconfig_directory" ]] || return
  temporary_kubeconfig_directory=$(mktemp -d)
  chmod 0700 "$temporary_kubeconfig_directory"
  kubeconfig="$temporary_kubeconfig_directory/kubeconfig"
  # Перенаправление намеренно создаёт приватный файл от имени текущего оператора.
  # shellcheck disable=SC2024
  sudo -n cat /etc/rancher/k3s/k3s.yaml >"$kubeconfig" ||
    fail 'temporary operator kubeconfig creation failed'
  chmod 0600 "$kubeconfig"
  export KUBECONFIG="$kubeconfig"
  [[ "$(kubectl config current-context)" == "$context" ]] ||
    fail 'temporary operator kubeconfig context mismatch'
}

capture_cluster_identity() {
  local config_json ca_data cluster_uid api_endpoint ca_sha256
  cluster_uid=$(kubectl get namespace kube-system -o jsonpath='{.metadata.uid}') ||
    fail 'Kubernetes cluster UID readback failed'
  [[ "$cluster_uid" =~ ^[A-Za-z0-9-]{16,128}$ ]] || fail 'Kubernetes cluster UID is invalid'
  config_json=$(kubectl config view --minify --raw -o json) ||
    fail 'Kubernetes cluster configuration readback failed'
  api_endpoint=$(jq -er '
    .clusters | select(length == 1) | .[0].cluster.server |
    select(type == "string" and test("^https://[^[:space:]]+$"))
  ' <<<"$config_json") || fail 'Kubernetes API endpoint is invalid'
  ca_data=$(jq -er '
    .clusters[0].cluster["certificate-authority-data"] |
    select(type == "string" and length > 0)
  ' <<<"$config_json") || fail 'embedded Kubernetes CA data is absent'
  ca_sha256=$(printf '%s' "$ca_data" | base64 --decode 2>/dev/null | sha256sum |
    awk '{print $1}') || fail 'Kubernetes CA data is invalid'
  [[ "$ca_sha256" =~ ^[a-f0-9]{64}$ ]] || fail 'Kubernetes CA digest is invalid'
  jq -cn --arg cluster_uid "$cluster_uid" --arg api_endpoint "$api_endpoint" \
    --arg ca_sha256 "$ca_sha256" '{version:1,clusterUID:$cluster_uid,
      apiEndpoint:$api_endpoint,caSHA256:$ca_sha256}'
}

read_cluster_marker() {
  local marker_stat marker_json
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
  printf '%s\n' "$marker_json"
}

verify_cluster_marker() {
  local marker_json current_json
  marker_json=$(read_cluster_marker)
  current_json=$(capture_cluster_identity)
  jq -e --argjson current "$current_json" '
    .clusterUID == $current.clusterUID and
    .apiEndpoint == $current.apiEndpoint and
    .caSHA256 == $current.caSHA256
  ' <<<"$marker_json" >/dev/null || fail 'Kubernetes cluster identity does not match the disposable marker'
}

create_cluster_marker() {
  local current_json temporary_marker
  current_json=$(capture_cluster_identity)
  if sudo -n test -e "$cluster_marker"; then
    verify_cluster_marker
    return
  fi
  temporary_marker=$(mktemp)
  printf '%s\n' "$current_json" >"$temporary_marker"
  chmod 0600 "$temporary_marker"
  sudo -n install -d -m 0700 -o root -g root "$(dirname -- "$cluster_marker")"
  sudo -n install -m 0600 -o root -g root "$temporary_marker" "$cluster_marker"
  rm -f -- "$temporary_marker"
  verify_cluster_marker
}

validate_source_checkout() {
  local expected_origin actual_origin actual_sha
  [[ "$expected_sha" =~ ^[a-f0-9]{40}$ ]] || fail 'expected source SHA is required and must use 40 lowercase hex characters'
  expected_origin=${KODEX_REMOTE_EXPECTED_ORIGIN_URL:-https://github.com/codex-k8s/kodex.git}
  case "$expected_origin" in
    https://github.com/codex-k8s/kodex.git|git@github.com:codex-k8s/kodex.git) ;;
    *) fail 'expected origin URL is not an approved Kodex repository URL' ;;
  esac
  actual_origin=$(git -C "$repository_root" remote get-url origin) ||
    fail 'source origin URL readback failed'
  [[ "$actual_origin" == "$expected_origin" ]] || fail 'source origin URL does not match the expected repository'
  actual_sha=$(git -C "$repository_root" rev-parse HEAD)
  [[ "$actual_sha" == "$expected_sha" ]] || fail 'source HEAD does not match the expected SHA'
  case "$command_name" in
    host-preflight|host-apply|host-readback|up)
      [[ -z "$(git -C "$repository_root" status --porcelain --untracked-files=all)" ]] ||
        fail 'initial remote deployment requires a clean source checkout'
      ;;
  esac
  printf 'Expected source SHA: %s\n' "$expected_sha"
}

validate_source_checkout

host_arguments=(
  --server-public-ip "$KODEX_REMOTE_SERVER_PUBLIC_IP"
  --server-public-ipv6-address "${KODEX_REMOTE_SERVER_PUBLIC_IPV6_ADDRESS:-}"
)
if [[ "$command_name" == host-preflight ]]; then
  sudo -n "$repository_root/tools/install/prepare-host.sh" --mode preflight \
    "${host_arguments[@]}"
  exit 0
fi
if [[ "$command_name" == host-apply ]]; then
  sudo -n "$repository_root/tools/install/prepare-host.sh" --mode apply \
    "${host_arguments[@]}"
  primary_group=$(id -gn)
  sudo -n install -d -m 0750 -o "$(id -un)" -g "$primary_group" \
    /srv/kodex-dev "$state_directory"
  rm -f -- "$legacy_kubeconfig"
  create_temporary_kubeconfig
  kubectl get --raw=/readyz >/dev/null ||
    fail 'operator kubeconfig readback failed'
  create_cluster_marker
  printf 'Kodex remote host preparation completed; start a new SSH session before up\n'
  exit 0
fi
if [[ "$command_name" == host-readback ]]; then
  sudo -n "$repository_root/tools/install/prepare-host.sh" --mode readback \
    "${host_arguments[@]}"
  id -nG | tr ' ' '\n' | grep -Fxq docker ||
    fail 'operator is not a member of the docker group'
  docker info >/dev/null 2>&1 || fail 'Docker daemon is unavailable to the operator'
  create_temporary_kubeconfig
  kubectl get --raw=/readyz >/dev/null ||
    fail 'operator kubeconfig readback failed'
  verify_cluster_marker
  printf 'Kodex remote host readback completed\n'
  exit 0
fi

create_temporary_kubeconfig
verify_cluster_marker
export KODEX_DEV_KUBECONFIG="$kubeconfig"
export KODEX_DEV_KUBE_CONTEXT="$context"
export KODEX_DEV_TLS_MODE=public-acme
export KODEX_DEV_INGRESS_CLASS=traefik
export KODEX_DEV_CLUSTER_ISSUER=letsencrypt-production
export KODEX_DEV_ACME_EMAIL="$KODEX_REMOTE_ACME_EMAIL"
export KODEX_DEV_PUBLIC_HOST="$KODEX_REMOTE_CONTROL_HOST"
export KODEX_DEV_OIDC_HOST="$KODEX_REMOTE_OIDC_HOST"
export KODEX_DEV_GRAFANA_HOST="$KODEX_REMOTE_GRAFANA_HOST"
export KODEX_DEV_HEADLAMP_HOST="$KODEX_REMOTE_HEADLAMP_HOST"
export KODEX_DEV_REGISTRY_HOST="$KODEX_REMOTE_REGISTRY_HOST"
export KODEX_DEV_PROMOTED_PULL_HOST="$KODEX_REMOTE_PROMOTED_PULL_HOST"
export KODEX_DEV_PUBLIC_TLS_HOSTS="$KODEX_REMOTE_CONTROL_HOST,$KODEX_REMOTE_OIDC_HOST"
export KODEX_DEV_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES="$KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES"
export KODEX_DEV_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES="$KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES"

case "$command_name" in
  up)
    id -nG | tr ' ' '\n' | grep -Fxq docker ||
      fail 'current session does not include the docker group; reconnect over SSH'
    docker info >/dev/null 2>&1 || fail 'Docker daemon is unavailable to the operator'
    "$repository_root/dev.sh" up --kubeconfig "$kubeconfig" --context "$context" \
      --state-directory "$state_directory" --cluster-marker "$cluster_marker" \
      --expected-sha "$expected_sha"
    ;;
  status|smoke|down)
    "$repository_root/dev.sh" "$command_name" --kubeconfig "$kubeconfig" \
      --context "$context" --state-directory "$state_directory" \
      --cluster-marker "$cluster_marker" --expected-sha "$expected_sha"
    ;;
  e2e)
    e2e_arguments=(--kubeconfig "$kubeconfig" --context "$context" \
      --state-directory "$state_directory" --cluster-marker "$cluster_marker" \
      --expected-sha "$expected_sha")
    [[ -z "$resource_prefix" ]] || e2e_arguments+=(--resource-prefix "$resource_prefix")
    [[ -z "$run_timeout_ms" ]] || e2e_arguments+=(--run-timeout-ms "$run_timeout_ms")
    KODEX_E2E_BASE_HOST_RESOLUTION=loopback \
      "$repository_root/dev.sh" e2e "${e2e_arguments[@]}"
    ;;
esac
