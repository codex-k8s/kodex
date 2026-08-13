#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'Direct production SSO bootstrap failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --context <exact-context> --mode apply|readback --oidc-ca-file <path> --public-ipv4 <address> [--external-material-file <path>] [--owner-username <name>] [--owner-email <email>]\n' "$0" >&2
}

context=""
mode=""
oidc_ca_file=""
external_material_file=""
public_ipv4=""
owner_username="lepehovsv"
owner_email="lepehovsv@gmail.com"
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --oidc-ca-file) oidc_ca_file="${2:-}"; shift 2 ;;
    --external-material-file) external_material_file="${2:-}"; shift 2 ;;
    --public-ipv4) public_ipv4="${2:-}"; shift 2 ;;
    --owner-username) owner_username="${2:-}"; shift 2 ;;
    --owner-email) owner_email="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail "exact Kubernetes context is required"
case "$mode" in apply|readback) ;; *) fail "mode must be apply or readback" ;; esac
[[ -r "$oidc_ca_file" ]] || fail "OIDC CA file is required"
[[ "$public_ipv4" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || fail "public IPv4 address is invalid"
IFS=. read -r ipv4_a ipv4_b ipv4_c ipv4_d <<<"$public_ipv4"
for octet in "$ipv4_a" "$ipv4_b" "$ipv4_c" "$ipv4_d"; do
  ((10#$octet <= 255)) || fail "public IPv4 address is invalid"
done
[[ "$owner_username" =~ ^[a-z0-9][a-z0-9._-]{2,63}$ ]] || fail "owner username is invalid"
[[ "$owner_email" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]] || fail "owner email is invalid"
for command_name in curl jq kubectl openssl sha256sum yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail "Kubernetes context mismatch"
openssl x509 -in "$oidc_ca_file" -noout -checkend 2592000 >/dev/null 2>&1 || fail "OIDC CA expires too soon"

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT HUP INT TERM

render="$temporary_directory/sso.yaml"
kubectl kustomize "$script_directory" >"$render"
oidc_egress="$temporary_directory/control-api-oidc-egress.yaml"
PUBLIC_OIDC_CIDR="$public_ipv4/32" yq '
  .spec.egress[0].to[0].ipBlock.cidr = strenv(PUBLIC_OIDC_CIDR)
' "$script_directory/control-api-oidc-egress.yaml" >"$oidc_egress"
kubectl apply --dry-run=client --validate=false -f "$render" >/dev/null
kubectl apply --dry-run=client --validate=false -f "$oidc_egress" >/dev/null

validate_bootstrap_secret() {
  kubectl --context "$context" -n identity get secret keycloak-bootstrap -o json |
    jq -e '
      (.data | keys | sort) == [
        "admin-password", "admin-username", "database-password", "organization-id",
        "owner-email", "owner-initial-password", "owner-username"
      ] and all(.data[]; type == "string" and length > 0)
    ' >/dev/null || fail "Keycloak bootstrap Secret is invalid"
}

update_oidc_ca() {
  kubectl --context "$context" -n mattercodex-system create configmap mattercodex-oidc-ca \
    --from-file="ca.pem=$oidc_ca_file" --dry-run=client -o yaml |
    kubectl --context "$context" apply --server-side --force-conflicts \
      --field-manager=mattercodex-sso-bootstrap -f - >/dev/null

  if [[ -n "$external_material_file" ]]; then
    [[ -f "$external_material_file" && ! -L "$external_material_file" ]] ||
      fail "external material file is invalid"
    mode_bits=$(stat -c '%a' "$external_material_file")
    (((8#$mode_bits & 0077) == 0)) || fail "external material file permissions are too broad"
    ca_value=$(<"$oidc_ca_file")
    OIDC_CA_VALUE="$ca_value" yq eval-all '
      (select(.kind == "ConfigMap" and .metadata.namespace == "mattercodex-system" and
        .metadata.name == "mattercodex-oidc-ca").data."ca.pem") = strenv(OIDC_CA_VALUE)
    ' "$external_material_file" >"$temporary_directory/external-material.yaml"
    count=$(yq -o=json eval-all '.' "$temporary_directory/external-material.yaml" | jq -s '[.[] | select(.kind == "ConfigMap" and .metadata.namespace == "mattercodex-system" and .metadata.name == "mattercodex-oidc-ca")] | length')
    [[ "$count" == 1 ]] || fail "external material OIDC CA binding is absent or duplicated"
    install -m 0600 "$temporary_directory/external-material.yaml" "$external_material_file"
  fi
}

if [[ "$mode" == apply ]]; then
  kubectl --context "$context" apply --server-side --field-manager=mattercodex-sso-bootstrap \
    -f "$script_directory/namespace.yaml" >/dev/null

  if ! kubectl --context "$context" -n identity get secret keycloak-bootstrap >/dev/null 2>&1; then
    secret_directory="$temporary_directory/bootstrap-secret"
    mkdir -p "$secret_directory"
    printf '%s' mattercodex-admin >"$secret_directory/admin-username"
    openssl rand -base64 48 | tr -d '\n' >"$secret_directory/admin-password"
    openssl rand -base64 48 | tr -d '\n' >"$secret_directory/database-password"
    printf '%s' "$owner_username" >"$secret_directory/owner-username"
    printf '%s' "$owner_email" >"$secret_directory/owner-email"
    openssl rand -base64 48 | tr -d '\n' >"$secret_directory/owner-initial-password"
    cat /proc/sys/kernel/random/uuid | tr -d '\n' >"$secret_directory/organization-id"
    kubectl --context "$context" -n identity create secret generic keycloak-bootstrap \
      --from-file="$secret_directory" --dry-run=client -o yaml |
      kubectl --context "$context" apply --server-side \
        --field-manager=mattercodex-sso-bootstrap -f - >/dev/null
  fi
  validate_bootstrap_secret

  kubectl --context "$context" apply --server-side --field-manager=mattercodex-sso-bootstrap \
    -f "$render" >/dev/null
  kubectl --context "$context" apply --server-side --field-manager=mattercodex-sso-bootstrap \
    -f "$oidc_egress" >/dev/null
  kubectl --context "$context" -n identity wait --for=condition=Ready certificate/sso-public-tls --timeout=5m >/dev/null
  kubectl --context "$context" -n identity rollout status statefulset/keycloak-postgresql --timeout=5m >/dev/null
  kubectl --context "$context" -n identity rollout status deployment/sso --timeout=8m >/dev/null
  update_oidc_ca
fi

validate_bootstrap_secret
for policy in control-api-gateway-public-oidc-egress control-plane-public-oidc-egress; do
  [[ "$(kubectl --context "$context" -n mattercodex-system get networkpolicy "$policy" -o jsonpath='{.spec.egress[0].to[0].ipBlock.cidr}')" == "$public_ipv4/32" ]] ||
    fail "$policy OIDC egress readback mismatch"
done
kubectl --context "$context" -n identity get certificate sso-public-tls -o json |
  jq -e 'any(.status.conditions[]?; .type == "Ready" and .status == "True")' >/dev/null ||
  fail "SSO public certificate is not Ready"
kubectl --context "$context" -n identity get statefulset keycloak-postgresql -o json |
  jq -e '(.status.readyReplicas // 0) == 1' >/dev/null || fail "Keycloak PostgreSQL is not Ready"
kubectl --context "$context" -n identity get deployment sso -o json |
  jq -e '(.status.readyReplicas // 0) == 1 and (.status.availableReplicas // 0) == 1' >/dev/null ||
  fail "Keycloak is not Ready"
discovery=$(curl --fail --silent --show-error --max-time 10 \
  https://sso.kodex.works/realms/mattercodex/.well-known/openid-configuration)
jwks_uri=$(jq -er 'select(.issuer == "https://sso.kodex.works/realms/mattercodex") | .jwks_uri' <<<"$discovery")
[[ "$jwks_uri" == "https://sso.kodex.works/realms/mattercodex/protocol/openid-connect/certs" ]] ||
  fail "OIDC discovery readback mismatch"
curl --fail --silent --show-error --max-time 10 "$jwks_uri" |
  jq -e '.keys | type == "array" and length > 0 and all(.[]; .kty == "RSA" and (.kid | type == "string" and length > 0))' >/dev/null ||
  fail "OIDC JWKS readback mismatch"
printf 'Direct production SSO %s completed\n' "$mode"
