#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local cluster bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --context <exact-context> --mode apply|readback --state-directory <path>\n' "$0" >&2
}

context=""
mode=""
state_directory=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'exact Kubernetes context is required'
case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
  fail 'state directory must be an exact safe absolute path'
for command_name in curl helm jq kubectl openssl sha256sum yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
lock_file="$script_directory/components.lock.json"
jq -e '
  .schemaVersion == 1 and (.charts | length) == 1 and
  .charts[0].name == "traefik" and
  (.charts[0].version | test("^[0-9]+\\.[0-9]+\\.[0-9]+$")) and
  (.charts[0].sha256 | test("^[a-f0-9]{64}$"))
' "$lock_file" >/dev/null || fail 'development component lock is invalid'

download_chart() {
  local name=$1 directory=$2 repository chart version expected_sha archive
  repository=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .repository' "$lock_file")
  chart=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .chart' "$lock_file")
  version=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .version' "$lock_file")
  expected_sha=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .sha256' "$lock_file")
  helm repo add "kodex-local-$name" "$repository" --force-update >/dev/null
  helm pull "kodex-local-$name/$chart" --version "$version" --destination "$directory" >/dev/null
  archive=$(find "$directory" -maxdepth 1 -type f -name "$chart-*.tgz" -print -quit)
  [[ -n "$archive" ]] || fail "chart archive is absent: $name"
  printf '%s  %s\n' "$expected_sha" "$archive" | sha256sum --check --status ||
    fail "chart digest mismatch: $name"
  printf '%s' "$archive"
}

install_cert_manager() {
  local temporary_directory archive version repository expected_sha
  temporary_directory=$(mktemp -d)
  version=$(jq -er '.charts[] | select(.name == "cert-manager") | .version' \
    "$repository_root/tools/install/components.lock.json")
  repository=$(jq -er '.charts[] | select(.name == "cert-manager") | .repository' \
    "$repository_root/tools/install/components.lock.json")
  expected_sha=$(jq -er '.charts[] | select(.name == "cert-manager") | .sha256' \
    "$repository_root/tools/install/components.lock.json")
  helm pull "$repository" --version "$version" --destination "$temporary_directory" >/dev/null
  archive=$(find "$temporary_directory" -maxdepth 1 -type f -name '*.tgz' -print -quit)
  [[ -n "$archive" ]] || fail 'cert-manager chart archive is absent'
  printf '%s  %s\n' "$expected_sha" "$archive" | sha256sum --check --status ||
    fail 'cert-manager chart digest mismatch'
  kubectl create namespace cert-manager --dry-run=client -o yaml |
    kubectl apply --server-side --field-manager=kodex-local-dev -f - >/dev/null
  helm upgrade --install cert-manager "$archive" --namespace cert-manager \
    --set crds.enabled=true --rollback-on-failure --wait --timeout 10m
}

install_traefik() {
  local temporary_directory archive values
  temporary_directory=$(mktemp -d)
  archive=$(download_chart traefik "$temporary_directory")
  values="$temporary_directory/values.yaml"
  cat >"$values" <<'EOF'
fullnameOverride: traefik
deployment:
  replicas: 1
providers:
  kubernetesCRD:
    enabled: true
  kubernetesIngress:
    enabled: true
    publishedService:
      enabled: true
ingressClass:
  enabled: true
  isDefaultClass: false
service:
  type: LoadBalancer
ports:
  web:
    port: 8000
    hostPort: 80
    expose:
      default: true
    exposedPort: 80
    protocol: TCP
  websecure:
    port: 8443
    hostPort: 443
    expose:
      default: true
    exposedPort: 443
    protocol: TCP
EOF
  helm upgrade --install kodex-local-traefik "$archive" --namespace kube-system \
    --values "$values" --rollback-on-failure --wait --timeout 10m
}

apply_local_issuer() {
  kubectl apply --server-side --field-manager=kodex-local-dev -f - >/dev/null <<'EOF'
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: kodex-local-bootstrap
  namespace: cert-manager
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: kodex-local-ca
  namespace: cert-manager
spec:
  isCA: true
  commonName: Kodex Local Development CA
  secretName: kodex-local-ca
  duration: 87600h
  renewBefore: 720h
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: kodex-local-bootstrap
    kind: Issuer
    group: cert-manager.io
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: kodex-local
spec:
  ca:
    secretName: kodex-local-ca
EOF
  kubectl -n cert-manager wait --for=condition=Ready certificate/kodex-local-ca --timeout=5m >/dev/null ||
    fail 'local CA certificate is not ready'
  for attempt in $(seq 1 60); do
    kubectl get clusterissuer kodex-local -o json 2>/dev/null | jq -e '
      any(.status.conditions[]?; .type == "Ready" and .status == "True")
    ' >/dev/null && break
    ((attempt < 60)) || fail 'local ClusterIssuer is not ready'
    sleep 2
  done
  install -d -m 0700 "$state_directory"
  kubectl -n cert-manager get secret kodex-local-ca -o jsonpath='{.data.ca\.crt}' |
    base64 -d >"$state_directory/kodex-local-ca.crt"
  chmod 0600 "$state_directory/kodex-local-ca.crt"
  openssl x509 -in "$state_directory/kodex-local-ca.crt" -noout -checkend 86400 >/dev/null ||
    fail 'local CA export is invalid'
}

trust_browser_ca() {
  command -v certutil >/dev/null 2>&1 || return 0
  [[ -d "$HOME/.pki/nssdb" ]] || return 0
  certutil -D -d "sql:$HOME/.pki/nssdb" -n 'Kodex Local Development CA' >/dev/null 2>&1 || true
  certutil -A -d "sql:$HOME/.pki/nssdb" -n 'Kodex Local Development CA' \
    -t 'C,,' -i "$state_directory/kodex-local-ca.crt" || fail 'browser CA trust update failed'
}

if [[ "$mode" == apply ]]; then
  install_cert_manager
  "$repository_root/infra/service-infrastructure/bootstrap.sh" \
    --context "$context" --mode apply-controllers
  install_traefik
  apply_local_issuer
  trust_browser_ca
fi

for deployment in cert-manager cert-manager-cainjector cert-manager-webhook; do
  kubectl -n cert-manager rollout status "deployment/$deployment" --timeout=3m >/dev/null ||
    fail "cert-manager deployment is unavailable: $deployment"
done
kubectl -n kube-system rollout status deployment/traefik --timeout=3m >/dev/null ||
  fail 'Traefik deployment is unavailable'
kubectl get ingressclass traefik >/dev/null || fail 'Traefik IngressClass is absent'
kubectl get clusterissuer kodex-local -o json | jq -e '
  any(.status.conditions[]?; .type == "Ready" and .status == "True")
' >/dev/null || fail 'local ClusterIssuer readback failed'
kubectl -n kube-system get service traefik -o json | jq -e '
  .spec.type == "LoadBalancer" and
  ([.spec.ports[] | select(.port == 80 or .port == 443) | .port] | sort) == [80,443]
' >/dev/null || fail 'Traefik public ports are invalid'

printf 'Kodex local cluster bootstrap completed: %s\n' "$mode"
