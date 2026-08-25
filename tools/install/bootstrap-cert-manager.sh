#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex cert-manager bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode preflight|apply|readback" \
    '  --acme-email <email> --ingress-class <name> [--acme-server <url>]' >&2
}

context=""
mode=""
acme_email=""
ingress_class=""
acme_server=https://acme-v02.api.letsencrypt.org/directory
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --acme-email) acme_email="${2:-}"; shift 2 ;;
    --ingress-class) ingress_class="${2:-}"; shift 2 ;;
    --acme-server) acme_server="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$mode" in preflight|apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ -n "$context" ]] || fail 'exact Kubernetes context is required'
[[ "$acme_email" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]] ||
  fail 'ACME email is invalid'
[[ "$ingress_class" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
  fail 'ingress class is invalid'
[[ "$acme_server" =~ ^https://[^[:space:]]+$ ]] || fail 'ACME server is invalid'
for command_name in helm jq kubectl sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] ||
  fail 'current Kubernetes context mismatch'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
lock_file="$script_directory/components.lock.json"
version=$(jq -er '.charts[] | select(.name == "cert-manager") | .version' "$lock_file")
repository=$(jq -er '.charts[] | select(.name == "cert-manager") | .repository' "$lock_file")
expected_sha=$(jq -er '.charts[] | select(.name == "cert-manager") | .sha256' "$lock_file")
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
helm pull "$repository" --version "$version" --destination "$temporary_directory" >/dev/null
archive=$(find "$temporary_directory" -maxdepth 1 -type f -name '*.tgz' -print -quit)
[[ -n "$archive" ]] || fail 'cert-manager chart archive is absent'
printf '%s  %s\n' "$expected_sha" "$archive" | sha256sum --check --status ||
  fail 'cert-manager chart digest mismatch'

if [[ "$mode" == preflight ]]; then
  helm template cert-manager "$archive" --namespace cert-manager \
    --set crds.enabled=true >/dev/null
  printf 'Kodex cert-manager preflight completed\n'
  exit 0
fi

if [[ "$mode" == apply ]]; then
  kubectl create namespace cert-manager --dry-run=client -o yaml |
    kubectl apply --server-side --field-manager=kodex-install -f - >/dev/null
  helm upgrade --install cert-manager "$archive" --namespace cert-manager \
    --set crds.enabled=true --atomic --wait --timeout 10m
  jq -n --arg email "$acme_email" --arg server "$acme_server" --arg ingress "$ingress_class" '{
    apiVersion:"cert-manager.io/v1",kind:"ClusterIssuer",
    metadata:{name:"letsencrypt-production"},
    spec:{acme:{email:$email,server:$server,
      privateKeySecretRef:{name:"letsencrypt-production-account-key"},
      solvers:[{http01:{ingress:{class:$ingress}}}]}}
  }' | kubectl apply --server-side --field-manager=kodex-install -f - >/dev/null
fi

for deployment in cert-manager cert-manager-cainjector cert-manager-webhook; do
  kubectl -n cert-manager rollout status "deployment/$deployment" --timeout=5m >/dev/null ||
    fail "cert-manager deployment is unavailable: $deployment"
done
for attempt in $(seq 1 60); do
  if kubectl get clusterissuer letsencrypt-production -o json 2>/dev/null | jq -e '
    any(.status.conditions[]?; .type == "Ready" and .status == "True")
  ' >/dev/null; then
    break
  fi
  ((attempt < 60)) || fail 'ACME ClusterIssuer is not ready'
  sleep 2
done
printf 'Kodex cert-manager bootstrap completed: %s\n' "$mode"
