#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Identity secret materialization failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --context <exact-context> --material-directory <owner-material-directory>\n' "$0" >&2
}

context=""
material_directory=""
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
[[ -n "$context" ]] || fail 'exact context is required'
[[ -d "$material_directory/identity" && -d "$material_directory/management" && ! -L "$material_directory" ]] ||
  fail 'identity material directory is invalid'
for command_name in cmp grep jq kubectl openssl stat; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'current Kubernetes context mismatch'

require_file() {
  local path=$1 mode
  [[ -f "$path" && -s "$path" && ! -L "$path" ]] || fail 'identity material file is invalid'
  mode=$(stat -c '%a' "$path")
  (((8#$mode & 0077) == 0)) || fail 'identity material permissions are too broad'
}
for path in "$material_directory"/identity/* "$material_directory"/management/*/*; do require_file "$path"; done
openssl x509 -checkend 86400 -noout -in "$material_directory/identity/database-ca.crt" >/dev/null 2>&1 ||
  fail 'Keycloak database CA certificate is invalid or expiring'
openssl verify -CAfile "$material_directory/identity/database-ca.crt" \
  "$material_directory/identity/database-ca.crt" >/dev/null 2>&1 ||
  fail 'Keycloak database CA is not self-verifying'
openssl x509 -in "$material_directory/identity/database-ca.crt" -noout -ext basicConstraints 2>/dev/null |
  grep -Fq 'CA:TRUE' || fail 'Keycloak database CA certificate is not a CA'
cmp \
  <(openssl x509 -in "$material_directory/identity/database-ca.crt" -pubkey -noout 2>/dev/null) \
  <(openssl pkey -in "$material_directory/identity/database-ca.key" -pubout 2>/dev/null) >/dev/null ||
  fail 'Keycloak database CA private key does not match the certificate'

kubectl create namespace identity --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl create namespace kodex-system --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl create namespace platform-admin --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl create namespace observability --dry-run=client -o yaml | kubectl apply -f - >/dev/null

create_secret() {
  local namespace=$1 name=$2
  shift 2
  kubectl -n "$namespace" create secret generic "$name" "$@" --dry-run=client -o yaml |
    kubectl apply --server-side --field-manager=kodex-identity-material -f - >/dev/null
}

create_configmap() {
  local namespace=$1 name=$2
  shift 2
  kubectl -n "$namespace" create configmap "$name" "$@" --dry-run=client -o yaml |
    kubectl apply --server-side --field-manager=kodex-identity-material -f - >/dev/null
}

identity="$material_directory/identity"
create_secret identity keycloak-bootstrap \
  --from-file=admin-username="$identity/bootstrap-admin-username" \
  --from-file=admin-password="$identity/bootstrap-admin-password" \
  --from-file=database-password="$identity/database-password" \
  --from-file=organization-id="$identity/organization-id"
create_configmap identity keycloak-identities \
  --from-file=admin-username="$identity/admin-username" \
  --from-file=owner-username="$identity/owner-username" \
  --from-file=owner-email="$identity/owner-email"
create_secret identity keycloak-initial-passwords \
  --from-file=admin-initial-password="$identity/admin-initial-password" \
  --from-file=owner-initial-password="$identity/owner-initial-password"
create_secret identity keycloak-admin-client \
  --from-file=client-id="$identity/admin-client-id" \
  --from-file=client-secret="$identity/admin-client-secret"
create_secret identity keycloak-database-ca \
  --from-file=tls.crt="$identity/database-ca.crt" \
  --from-file=tls.key="$identity/database-ca.key"

for binding in \
  control-center:kodex-system \
  grafana:observability \
  headlamp:platform-admin; do
  surface=${binding%%:*}
  namespace=${binding#*:}
  directory="$material_directory/management/oauth2-$surface"
  [[ "$(wc -c <"$directory/cookie-secret")" -eq 32 ]] ||
    fail "OAuth2 Proxy cookie Secret must contain exactly 32 bytes: $surface"
  create_secret "$namespace" "oauth2-$surface" \
    --from-file=client-id="$directory/client-id" \
    --from-file=client-secret="$directory/client-secret" \
    --from-file=cookie-secret="$directory/cookie-secret"
done
create_secret observability grafana-admin \
  --from-file=admin-user="$material_directory/management/grafana-admin/admin-user" \
  --from-file=admin-password="$material_directory/management/grafana-admin/admin-password"

kubectl -n identity get configmap keycloak-identities -o json | jq -e '
  (.data | keys | sort) == ["admin-username", "owner-email", "owner-username"] and
  all(.data[]; type == "string" and length > 0)
' >/dev/null || fail 'Keycloak identity ConfigMap readback failed'
kubectl -n identity get secret keycloak-initial-passwords -o json | jq -e '
  (.data | keys | sort) == ["admin-initial-password", "owner-initial-password"] and
  all(.data[]; type == "string" and length > 0)
' >/dev/null || fail 'Keycloak initial password Secret readback failed'

for binding in \
  oauth2-control-center:kodex-system \
  oauth2-grafana:observability \
  oauth2-headlamp:platform-admin; do
  secret=${binding%%:*}
  namespace=${binding#*:}
  kubectl -n "$namespace" get secret "$secret" -o json | jq -e '
    (.data | keys | sort) == ["client-id", "client-secret", "cookie-secret"] and
    all(.data[]; type == "string" and length > 0) and
    (.data["cookie-secret"] | @base64d | length) == 32
  ' >/dev/null || fail "OAuth2 Proxy Secret readback failed: $secret"
done
kubectl -n observability get secret grafana-admin -o json | jq -e '
  (.data | keys | sort) == ["admin-password", "admin-user"] and
  all(.data[]; type == "string" and length > 0)
' >/dev/null || fail 'Grafana administrator Secret readback failed'
printf 'Identity secrets materialized\n'
