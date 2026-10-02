#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local cluster bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode apply|readback --state-directory <path>" \
    '  [--tls-mode local-ca|public-acme] [--acme-email <email>]' \
    '  [--ingress-class <name>] [--cluster-issuer <name>]' \
    '  [--cert-manager-mode managed|existing] [--ingress-mode managed|existing]' \
    '  [--provider-sandbox-mode managed|existing|disabled]' \
    '  [--local-ca-certificate-file <path> --local-ca-private-key-file <path>]' \
    '  [--trust-store browser|system-and-browser]' >&2
}

context=""
mode=""
state_directory=""
tls_mode=local-ca
acme_email=""
ingress_class=traefik
cluster_issuer=kodex-local
cert_manager_mode=managed
ingress_mode=managed
provider_sandbox_mode=managed
local_ca_certificate_file=""
local_ca_private_key_file=""
trust_store=browser
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --tls-mode) tls_mode=${2:-}; shift 2 ;;
    --acme-email) acme_email=${2:-}; shift 2 ;;
    --ingress-class) ingress_class=${2:-}; shift 2 ;;
    --cluster-issuer) cluster_issuer=${2:-}; shift 2 ;;
    --cert-manager-mode) cert_manager_mode=${2:-}; shift 2 ;;
    --ingress-mode) ingress_mode=${2:-}; shift 2 ;;
    --provider-sandbox-mode) provider_sandbox_mode=${2:-}; shift 2 ;;
    --local-ca-certificate-file) local_ca_certificate_file=${2:-}; shift 2 ;;
    --local-ca-private-key-file) local_ca_private_key_file=${2:-}; shift 2 ;;
    --trust-store) trust_store=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'exact Kubernetes context is required'
case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
case "$tls_mode" in local-ca|public-acme) ;; *) fail 'TLS mode is invalid' ;; esac
case "$cert_manager_mode" in managed|existing) ;; *) fail 'cert-manager mode is invalid' ;; esac
case "$ingress_mode" in managed|existing) ;; *) fail 'ingress mode is invalid' ;; esac
case "$provider_sandbox_mode" in managed|existing|disabled) ;; *) fail 'provider sandbox mode is invalid' ;; esac
case "$trust_store" in browser|system-and-browser) ;; *) fail 'trust store mode is invalid' ;; esac
[[ "$ingress_class" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
  fail 'ingress class is invalid'
[[ "$cluster_issuer" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
  fail 'cluster issuer is invalid'
if [[ "$tls_mode" == public-acme ]]; then
  [[ "$acme_email" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]] ||
    fail 'ACME email is required in public TLS mode'
  [[ "$cluster_issuer" == letsencrypt-production ]] ||
    fail 'public TLS mode requires the supported letsencrypt-production issuer'
fi
if [[ "$tls_mode" == local-ca && "$cluster_issuer" != kodex-local ]]; then
  fail 'local CA mode requires the kodex-local ClusterIssuer'
fi
if [[ -n "$local_ca_certificate_file" || -n "$local_ca_private_key_file" ]]; then
  [[ "$tls_mode" == local-ca ]] || fail 'local CA files require local-ca TLS mode'
  [[ -n "$local_ca_certificate_file" && -n "$local_ca_private_key_file" ]] ||
    fail 'local CA certificate and private key must be provided together'
  [[ "$local_ca_certificate_file" == /* && -f "$local_ca_certificate_file" &&
    ! -L "$local_ca_certificate_file" ]] || fail 'local CA certificate file is invalid'
  [[ "$local_ca_private_key_file" == /* && -f "$local_ca_private_key_file" &&
    ! -L "$local_ca_private_key_file" ]] || fail 'local CA private key file is invalid'
  key_mode=$(stat -c '%a' "$local_ca_private_key_file")
  (((8#$key_mode & 0077) == 0)) || fail 'local CA private key permissions are too broad'
  openssl x509 -in "$local_ca_certificate_file" -noout -checkend 86400 >/dev/null ||
    fail 'local CA certificate is expired or invalid'
  openssl x509 -in "$local_ca_certificate_file" -noout -text |
    grep -Fq 'CA:TRUE' || fail 'local CA certificate is not a CA'
  certificate_public_key=$(openssl x509 -in "$local_ca_certificate_file" -pubkey -noout |
    openssl pkey -pubin -outform DER 2>/dev/null | sha256sum | awk '{print $1}')
  private_public_key=$(openssl pkey -in "$local_ca_private_key_file" -pubout -outform DER 2>/dev/null |
    sha256sum | awk '{print $1}')
  [[ -n "$certificate_public_key" && "$certificate_public_key" == "$private_public_key" ]] ||
    fail 'local CA certificate and private key do not match'
fi
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
  fail 'state directory must be an exact safe absolute path'
for command_name in curl helm jq kubectl openssl sha256sum yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
if [[ "$tls_mode" == local-ca ]]; then
  command -v certutil >/dev/null 2>&1 || fail 'certutil is required'
fi
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
lock_file="$script_directory/components.lock.json"
jq -e '
  .schemaVersion == 1 and (.charts | length) == 1 and
  .charts[0].name == "traefik" and
  (.charts[0].version | test("^[0-9]+\\.[0-9]+\\.[0-9]+$")) and
  (.charts[0].sha256 | test("^[a-f0-9]{64}$")) and
  (.images | length) == 2 and
  ([.images[].name] | sort) == ["aws-cli", "seaweedfs"] and
  any(.images[];
    .name == "seaweedfs" and .version == "4.41" and
    (.reference | test("^docker\\.io/chrislusf/seaweedfs@sha256:[a-f0-9]{64}$"))) and
  any(.images[];
    .name == "aws-cli" and .version == "2.36.34" and
    (.reference | test("^docker\\.io/amazon/aws-cli@sha256:[a-f0-9]{64}$")))
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
    expose:
      default: true
    exposedPort: 80
    protocol: TCP
  websecure:
    port: 8443
    expose:
      default: true
    exposedPort: 443
    protocol: TCP
EOF
  helm upgrade --install kodex-local-traefik "$archive" --namespace kube-system \
    --values "$values" --rollback-on-failure --wait --timeout 10m
}

apply_local_issuer() {
  if [[ -n "$local_ca_certificate_file" ]]; then
    kubectl create namespace cert-manager --dry-run=client -o yaml |
      kubectl apply --server-side --field-manager=kodex-local-dev -f - >/dev/null
    kubectl -n cert-manager create secret generic kodex-local-ca \
      --from-file=tls.crt="$local_ca_certificate_file" \
      --from-file=ca.crt="$local_ca_certificate_file" \
      --from-file=tls.key="$local_ca_private_key_file" \
      --type=kubernetes.io/tls --dry-run=client -o yaml |
      kubectl apply --server-side --force-conflicts \
        --field-manager=kodex-local-dev -f - >/dev/null
    kubectl apply --server-side --field-manager=kodex-local-dev -f - >/dev/null <<'EOF'
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: kodex-local
spec:
  ca:
    secretName: kodex-local-ca
EOF
  else
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
  fi
  for attempt in $(seq 1 60); do
    kubectl get clusterissuer kodex-local -o json 2>/dev/null | jq -e '
      any(.status.conditions[]?; .type == "Ready" and .status == "True")
    ' >/dev/null && break
    ((attempt < 60)) || fail 'local ClusterIssuer is not ready'
    sleep 2
  done
  install -d -m 0700 "$state_directory"
  if [[ -n "$local_ca_certificate_file" ]]; then
    install -m 0600 "$local_ca_certificate_file" "$state_directory/kodex-local-ca.crt"
  else
    kubectl -n cert-manager get secret kodex-local-ca -o jsonpath='{.data.ca\.crt}' |
      base64 -d >"$state_directory/kodex-local-ca.crt"
  fi
  chmod 0600 "$state_directory/kodex-local-ca.crt"
  openssl x509 -in "$state_directory/kodex-local-ca.crt" -noout -checkend 86400 >/dev/null ||
    fail 'local CA export is invalid'
}

trust_system_ca() {
  local target=/usr/local/share/ca-certificates/kodex-local-development-ca.crt
  sudo -n install -m 0644 "$state_directory/kodex-local-ca.crt" "$target" ||
    fail 'system CA installation failed'
  sudo -n update-ca-certificates >/dev/null || fail 'system CA trust update failed'
  openssl verify -CAfile /etc/ssl/certs/ca-certificates.crt \
    "$state_directory/kodex-local-ca.crt" >/dev/null || fail 'system CA trust readback failed'
}

trust_browser_ca() {
  local nss_database="sql:$HOME/.pki/nssdb"
  local desired_nickname='Kodex Local Development CA'
  local source_fingerprint existing_fingerprint line nickname trusted_nickname=""
  install -d -m 0700 "$HOME/.pki/nssdb"
  if [[ ! -f "$HOME/.pki/nssdb/cert9.db" ]]; then
    certutil -N --empty-password -d "$nss_database" >/dev/null ||
      fail 'browser NSS database initialization failed'
  fi

  source_fingerprint=$(openssl x509 -in "$state_directory/kodex-local-ca.crt" \
    -noout -fingerprint -sha256) || fail 'browser CA fingerprint calculation failed'
  while IFS= read -r line; do
    [[ "$line" =~ ^(.+[^[:space:]])[[:space:]]+([[:alpha:],]+)[[:space:]]*$ ]] || continue
    nickname=${BASH_REMATCH[1]}
    existing_fingerprint=$(certutil -L -d "$nss_database" -n "$nickname" -a 2>/dev/null |
      openssl x509 -noout -fingerprint -sha256 2>/dev/null) || continue
    if [[ "$existing_fingerprint" == "$source_fingerprint" ]]; then
      trusted_nickname=$nickname
      break
    fi
  done < <(certutil -L -d "$nss_database")
  if [[ -z "$trusted_nickname" ]]; then
    certutil -D -d "$nss_database" -n "$desired_nickname" >/dev/null 2>&1 || true
    certutil -A -d "$nss_database" -n "$desired_nickname" \
      -t 'C,,' -i "$state_directory/kodex-local-ca.crt" || fail 'browser CA trust update failed'
    trusted_nickname=$desired_nickname
  else
    certutil -M -d "$nss_database" -n "$trusted_nickname" -t 'C,,' ||
      fail 'browser CA trust update failed'
  fi
  certutil -L -d "$nss_database" -n "$trusted_nickname" -a 2>/dev/null |
    openssl x509 -noout -fingerprint -sha256 |
    grep -Fqx "$source_fingerprint" ||
    fail 'browser CA trust readback failed'
}

if [[ "$mode" == apply ]]; then
  case "$provider_sandbox_mode" in
    managed) sudo -n "$repository_root/tools/dev/configure-provider-sandbox.sh" --mode apply ;;
    existing) sudo -n "$repository_root/tools/dev/configure-provider-sandbox.sh" --mode readback ;;
    disabled) ;;
  esac
  if [[ "$tls_mode" == local-ca ]]; then
    [[ "$cert_manager_mode" == existing ]] || install_cert_manager
  else
    if [[ "$cert_manager_mode" == managed ]]; then
      "$repository_root/tools/install/bootstrap-cert-manager.sh" \
        --context "$context" --mode apply --acme-email "$acme_email" \
        --ingress-class "$ingress_class"
    fi
  fi
  "$repository_root/infra/service-infrastructure/bootstrap.sh" \
    --context "$context" --mode apply-controllers
  [[ "$ingress_mode" == existing ]] || install_traefik
  if [[ "$tls_mode" == local-ca ]]; then
    apply_local_issuer
    trust_browser_ca
    [[ "$trust_store" == system-and-browser ]] && trust_system_ca
  fi
fi

if [[ "$provider_sandbox_mode" != disabled ]]; then
  sudo -n "$repository_root/tools/dev/configure-provider-sandbox.sh" --mode readback
fi

for deployment in cert-manager cert-manager-cainjector cert-manager-webhook; do
  kubectl -n cert-manager rollout status "deployment/$deployment" --timeout=3m >/dev/null ||
    fail "cert-manager deployment is unavailable: $deployment"
done
kubectl get ingressclass "$ingress_class" >/dev/null || fail 'selected IngressClass is absent'
ingress_controller=$(kubectl get ingressclass "$ingress_class" -o jsonpath='{.spec.controller}')
[[ -n "$ingress_controller" ]] || fail 'selected IngressClass controller is absent'
if [[ "$ingress_class" == traefik ]]; then
  kubectl -n kube-system rollout status deployment/traefik --timeout=3m >/dev/null ||
    fail 'Traefik deployment is unavailable'
fi
kubectl get clusterissuer "$cluster_issuer" -o json | jq -e '
  any(.status.conditions[]?; .type == "Ready" and .status == "True")
' >/dev/null || fail 'development ClusterIssuer readback failed'
if [[ "$ingress_class" == traefik ]]; then
  kubectl -n kube-system get service traefik -o json | jq -e '
    .spec.type == "LoadBalancer" and
    ([.spec.ports[] | select(.port == 80 or .port == 443) | .port] | sort) == [80,443]
  ' >/dev/null || fail 'Traefik public ports are invalid'
fi

printf 'Kodex development cluster bootstrap completed: mode=%s tls=%s cert-manager=%s ingress=%s sandbox=%s\n' \
  "$mode" "$tls_mode" "$cert_manager_mode" "$ingress_mode" "$provider_sandbox_mode"
