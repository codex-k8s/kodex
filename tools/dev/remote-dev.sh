#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex remote development failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 host-preflight|host-apply|up|status|smoke|e2e|down|teleport" \
    '  [--env-file <private-path>] [--resource-prefix <slug>]' \
    '  [--run-timeout-ms <milliseconds>]' >&2
}

command_name=${1:-}
[[ -n "$command_name" ]] || { usage; exit 1; }
shift
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
env_file="$repository_root/.kodex-remote-env"
resource_prefix=""
run_timeout_ms=""
while (($# > 0)); do
  case "$1" in
    --env-file) env_file=${2:-}; shift 2 ;;
    --resource-prefix) resource_prefix=${2:-}; shift 2 ;;
    --run-timeout-ms) run_timeout_ms=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
case "$command_name" in
  host-preflight|host-apply|up|status|smoke|e2e|down|teleport) ;;
  *) usage; fail 'command is invalid' ;;
esac

# shellcheck source=tools/install/load-env.sh
source "$repository_root/tools/install/load-env.sh"
kodex_load_env "$env_file" || exit 1
KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES=${KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES:-}
kodex_require_env KODEX_REMOTE_SERVER_PUBLIC_IP KODEX_REMOTE_CONTROL_HOST \
  KODEX_REMOTE_OIDC_HOST KODEX_REMOTE_TELEPORT_HOST KODEX_REMOTE_REGISTRY_HOST \
  KODEX_REMOTE_PROMOTED_PULL_HOST KODEX_REMOTE_ACME_EMAIL \
  KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES || exit 1
KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES=${KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES:-}
KODEX_REMOTE_GRAFANA_HOST=${KODEX_REMOTE_GRAFANA_HOST:-grafana.kodex.works}
KODEX_REMOTE_HEADLAMP_HOST=${KODEX_REMOTE_HEADLAMP_HOST:-headlamp.kodex.works}
KODEX_REMOTE_GITHUB_ORGANIZATION=${KODEX_REMOTE_GITHUB_ORGANIZATION:-codex-k8s}
KODEX_REMOTE_GITHUB_TEAM=${KODEX_REMOTE_GITHUB_TEAM:-kodex-teleport-admins}
state_directory=${KODEX_REMOTE_STATE_DIRECTORY:-/srv/kodex-dev/state}
kubeconfig=${KODEX_REMOTE_KUBECONFIG:-$HOME/.kube/kodex-dev-remote}
context=${KODEX_REMOTE_KUBE_CONTEXT:-default}
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
  fail 'remote state directory is invalid'
[[ "$kubeconfig" == /* ]] || fail 'remote kubeconfig path must be absolute'

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
    /srv/kodex-dev "$state_directory" "$(dirname -- "$kubeconfig")"
  sudo -n install -m 0600 -o "$(id -un)" -g "$primary_group" \
    /etc/rancher/k3s/k3s.yaml "$kubeconfig"
  KUBECONFIG="$kubeconfig" kubectl get --raw=/readyz >/dev/null ||
    fail 'operator kubeconfig readback failed'
  printf 'Kodex remote host preparation completed; start a new SSH session before up\n'
  exit 0
fi

[[ -f "$kubeconfig" && -r "$kubeconfig" ]] || fail 'remote operator kubeconfig is absent'
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
export KODEX_DEV_PUBLIC_TLS_HOSTS="$KODEX_REMOTE_CONTROL_HOST,$KODEX_REMOTE_OIDC_HOST,$KODEX_REMOTE_TELEPORT_HOST"
export KODEX_DEV_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES="$KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES"
export KODEX_DEV_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES="$KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES"

teleport_arguments=(
  --context "$context" --host "$KODEX_REMOTE_TELEPORT_HOST"
  --ingress-class traefik --cluster-issuer letsencrypt-production
  --allowed-ipv4-addresses "$KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES"
  --allowed-ipv6-addresses "$KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES"
  --github-organization "$KODEX_REMOTE_GITHUB_ORGANIZATION"
  --github-team "$KODEX_REMOTE_GITHUB_TEAM"
)
apply_teleport() {
  kodex_require_env KODEX_REMOTE_TELEPORT_GITHUB_CLIENT_ID \
    KODEX_REMOTE_TELEPORT_GITHUB_CLIENT_SECRET || exit 1
  install -d -m 0700 "$state_directory/inputs"
  client_id_file="$state_directory/inputs/teleport-github-client-id"
  client_secret_file="$state_directory/inputs/teleport-github-client-secret"
  printf '%s' "$KODEX_REMOTE_TELEPORT_GITHUB_CLIENT_ID" >"$client_id_file"
  printf '%s' "$KODEX_REMOTE_TELEPORT_GITHUB_CLIENT_SECRET" >"$client_secret_file"
  chmod 0600 "$client_id_file" "$client_secret_file"
  "$repository_root/infra/teleport/bootstrap.sh" --mode apply \
    "${teleport_arguments[@]}" --github-client-id-file "$client_id_file" \
    --github-client-secret-file "$client_secret_file"
}

if [[ "$command_name" == teleport ]]; then
  apply_teleport
  exit 0
fi

case "$command_name" in
  up)
    id -nG | tr ' ' '\n' | grep -Fxq docker ||
      fail 'current session does not include the docker group; reconnect over SSH'
    docker info >/dev/null 2>&1 || fail 'Docker daemon is unavailable to the operator'
    "$repository_root/dev.sh" up --kubeconfig "$kubeconfig" --context "$context" \
      --state-directory "$state_directory"
    if [[ -n "${KODEX_REMOTE_TELEPORT_GITHUB_CLIENT_ID:-}" &&
      -n "${KODEX_REMOTE_TELEPORT_GITHUB_CLIENT_SECRET:-}" ]]; then
      apply_teleport
    else
      printf 'Kodex Teleport is pending dedicated GitHub OAuth credentials\n'
    fi
    ;;
  status|smoke|down)
    "$repository_root/dev.sh" "$command_name" --kubeconfig "$kubeconfig" \
      --context "$context" --state-directory "$state_directory"
    ;;
  e2e)
    e2e_arguments=(--kubeconfig "$kubeconfig" --context "$context" \
      --state-directory "$state_directory")
    [[ -z "$resource_prefix" ]] || e2e_arguments+=(--resource-prefix "$resource_prefix")
    [[ -z "$run_timeout_ms" ]] || e2e_arguments+=(--run-timeout-ms "$run_timeout_ms")
    "$repository_root/dev.sh" e2e "${e2e_arguments[@]}"
    ;;
esac
