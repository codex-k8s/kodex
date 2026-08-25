#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Fresh install secret materialization failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --ca-certificate-file <path> --ca-private-key-file <path>" \
    '  --postgresql-password-file <path> --nats-material-directory <path>' \
    '  --control-api-material-directory <path> --oidc-ca-file <path>' \
    '  --provider-auth-file <path>' >&2
}

expected_context=""
ca_certificate_file=""
ca_private_key_file=""
postgresql_password_file=""
nats_material_directory=""
control_api_material_directory=""
oidc_ca_file=""
provider_auth_file=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --ca-certificate-file) ca_certificate_file="${2:-}"; shift 2 ;;
    --ca-private-key-file) ca_private_key_file="${2:-}"; shift 2 ;;
    --postgresql-password-file) postgresql_password_file="${2:-}"; shift 2 ;;
    --nats-material-directory) nats_material_directory="${2:-}"; shift 2 ;;
    --control-api-material-directory) control_api_material_directory="${2:-}"; shift 2 ;;
    --oidc-ca-file) oidc_ca_file="${2:-}"; shift 2 ;;
    --provider-auth-file) provider_auth_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail 'exact context is required'
for command_name in awk grep kubectl openssl sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail 'current Kubernetes context mismatch'

for input_file in "$ca_certificate_file" "$ca_private_key_file" "$postgresql_password_file" \
  "$oidc_ca_file" "$provider_auth_file" \
  "$control_api_material_directory/session-current.hex" \
  "$control_api_material_directory/session-previous.hex" \
  "$control_api_material_directory/lease-signing.key"; do
  [[ -f "$input_file" && -r "$input_file" && ! -L "$input_file" ]] || fail 'required material file is invalid'
done
for key in operator.jwt system-account.public system-account.jwt account.public account.jwt; do
  [[ -f "$nats_material_directory/$key" && -r "$nats_material_directory/$key" && ! -L "$nats_material_directory/$key" ]] ||
    fail "required NATS material is invalid: $key"
done

validate_nats_jwt() {
  local file_path=$1
  [[ $(awk 'END {print NR}' "$file_path") -eq 1 ]] &&
    grep -Eq '^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$' "$file_path"
}

validate_nats_account_nkey() {
  local file_path=$1
  [[ $(awk 'END {print NR}' "$file_path") -eq 1 ]] &&
    grep -Eq '^A[A-Z2-7]{55}$' "$file_path"
}

for key in operator.jwt system-account.jwt account.jwt; do
  validate_nats_jwt "$nats_material_directory/$key" || fail "NATS JWT is not canonical: $key"
done
for key in system-account.public account.public; do
  validate_nats_account_nkey "$nats_material_directory/$key" ||
    fail "NATS account nkey is not canonical: $key"
done

openssl x509 -in "$ca_certificate_file" -noout -checkend 86400 >/dev/null || fail 'installation CA is invalid or expires too soon'
openssl pkey -in "$ca_private_key_file" -noout -check >/dev/null 2>&1 || fail 'installation CA private key is invalid'
openssl x509 -in "$oidc_ca_file" -noout -checkend 3600 >/dev/null || fail 'OIDC CA is invalid or expires too soon'
[[ $(wc -c <"$postgresql_password_file") -ge 32 ]] || fail 'PostgreSQL bootstrap password is too short'
[[ $(wc -c <"$provider_auth_file") -ge 32 ]] || fail 'provider authorization material is too short'

kubectl create namespace kodex-system --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl create namespace kodex-trust --dry-run=client -o yaml | kubectl apply -f - >/dev/null

create_secret() {
  local namespace=$1
  local name=$2
  shift 2
  kubectl -n "$namespace" create secret generic "$name" "$@" --dry-run=client -o yaml |
    kubectl apply -f - >/dev/null
}

create_secret kodex-system kodex-installation-ca \
  --from-file=tls.crt="$ca_certificate_file" --from-file=tls.key="$ca_private_key_file"
create_secret kodex-trust kodex-installation-ca \
  --from-file=tls.crt="$ca_certificate_file"
create_secret kodex-trust kodex-vault-ca-source \
  --from-file=ca.crt="$ca_certificate_file"
create_secret kodex-system control-api-gateway-vault-ca \
  --from-file=ca.crt="$ca_certificate_file"
create_secret kodex-system internal-rpc-authority-restore-controller-vault-ca \
  --from-file=ca.crt="$ca_certificate_file"
create_secret kodex-system kodex-postgresql-bootstrap \
  --from-file=password="$postgresql_password_file"
create_secret kodex-system kodex-nats-credentials \
  --from-file=operator.jwt="$nats_material_directory/operator.jwt" \
  --from-file=system-account.public="$nats_material_directory/system-account.public" \
  --from-file=system-account.jwt="$nats_material_directory/system-account.jwt" \
  --from-file=account.public="$nats_material_directory/account.public" \
  --from-file=account.jwt="$nats_material_directory/account.jwt"
create_secret kodex-system kodex-sentry --from-literal=dsn=
create_secret kodex-system internal-rpc-authority-sentry --from-literal=dsn=
create_secret kodex-system kodex-integration-credentials --from-literal=empty=
create_secret kodex-system runtime-provider-openai-default-r1 \
  --from-file=auth.json="$provider_auth_file"

kubectl -n kodex-system create configmap kodex-oidc-ca \
  --from-file=ca.pem="$oidc_ca_file" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n kodex-system create configmap kodex-internal-ca \
  --from-file=ca.pem="$ca_certificate_file" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n kodex-system create configmap kodex-otel-ca \
  --from-file=ca.pem="$ca_certificate_file" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n kodex-system create configmap internal-rpc-authority-otel-ca \
  --from-file=ca.pem="$ca_certificate_file" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

provider_uid=$(kubectl -n kodex-system get secret runtime-provider-openai-default-r1 -o jsonpath='{.metadata.uid}')
provider_resource_version=$(kubectl -n kodex-system get secret runtime-provider-openai-default-r1 -o jsonpath='{.metadata.resourceVersion}')
provider_sha256=$(sha256sum "$provider_auth_file" | awk '{print $1}')
kubectl -n kodex-system create configmap runtime-provider-openai-default-metadata \
  --from-literal=secretName=runtime-provider-openai-default-r1 \
  --from-literal=secretUID="$provider_uid" \
  --from-literal=secretResourceVersion="$provider_resource_version" \
  --from-literal=contentSHA256="$provider_sha256" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
kubectl apply -f "$repository_root/infra/service-infrastructure/vault-bootstrap.yaml" >/dev/null
kubectl -n kodex-system wait --for=condition=Ready certificate/kodex-vault-server --timeout=180s >/dev/null
kubectl -n kodex-system wait --for=condition=Ready certificate/kodex-control-api-bootstrap --timeout=180s >/dev/null

for secret_name in kodex-installation-ca kodex-postgresql-bootstrap kodex-nats-credentials \
  kodex-vault-server-tls control-api-gateway-vault-ca runtime-provider-openai-default-r1 \
  internal-rpc-authority-restore-controller-vault-ca kodex-sentry; do
  kubectl -n kodex-system get secret "$secret_name" >/dev/null || fail "secret readback failed: $secret_name"
done
printf 'Fresh install secret materialization completed\n'
