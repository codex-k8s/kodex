#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Identity bootstrap failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --context <exact-context> --mode preflight|apply|readback --oidc-host <dns> --ingress-class <name> --cluster-issuer <name> --ingress-namespace <name> --ingress-pod-name <label>\n' "$0" >&2
}

context=""
mode=""
oidc_host=""
ingress_class=""
cluster_issuer=""
ingress_namespace=""
ingress_pod_name=""
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --oidc-host) oidc_host="${2:-}"; shift 2 ;;
    --ingress-class) ingress_class="${2:-}"; shift 2 ;;
    --cluster-issuer) cluster_issuer="${2:-}"; shift 2 ;;
    --ingress-namespace) ingress_namespace="${2:-}"; shift 2 ;;
    --ingress-pod-name) ingress_pod_name="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'exact context is required'
case "$mode" in preflight|apply|readback) ;; *) fail 'mode is invalid' ;; esac
for value in "$oidc_host" "$ingress_class" "$cluster_issuer" "$ingress_namespace" "$ingress_pod_name"; do
  [[ "$value" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail 'deployment parameter is invalid'
done
[[ "$oidc_host" == *.* ]] || fail 'OIDC host must be a DNS name'
for command_name in jq kubectl yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'current Kubernetes context mismatch'
kubectl get namespace cert-manager >/dev/null 2>&1 || fail 'cert-manager namespace is absent'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
render="$temporary_directory/identity.yaml"
kubectl kustomize "$script_directory" >"$render"
OIDC_HOST="$oidc_host" INGRESS_CLASS="$ingress_class" CLUSTER_ISSUER="$cluster_issuer" \
INGRESS_NAMESPACE="$ingress_namespace" INGRESS_POD_NAME="$ingress_pod_name" yq -i '
  (.. | select(tag == "!!str")) |= (
    sub("__MATTERCODEX_OIDC_HOST__"; strenv(OIDC_HOST)) |
    sub("__MATTERCODEX_INGRESS_CLASS__"; strenv(INGRESS_CLASS)) |
    sub("__MATTERCODEX_CLUSTER_ISSUER__"; strenv(CLUSTER_ISSUER)) |
    sub("__MATTERCODEX_INGRESS_NAMESPACE__"; strenv(INGRESS_NAMESPACE)) |
    sub("__MATTERCODEX_INGRESS_POD_NAME__"; strenv(INGRESS_POD_NAME))
  )
' "$render"
! grep -Eq '__MATTERCODEX_[A-Z0-9_]+__|sha256:0{64}' "$render" || fail 'identity render contains placeholders'
kubectl apply --dry-run=client --validate=false -f "$render" >/dev/null

validate_secret_shape() {
  kubectl -n identity get secret keycloak-bootstrap -o json | jq -e '
    (.data | keys | sort) == [
      "admin-password", "admin-username", "database-password", "organization-id"
    ] and all(.data[]; type == "string" and length > 0)
  ' >/dev/null || fail 'Keycloak bootstrap Secret is invalid'
  kubectl -n identity get configmap keycloak-identities -o json | jq -e '
    (.data | keys | sort) == ["admin-username", "owner-email", "owner-username"] and
    all(.data[]; type == "string" and length > 0)
  ' >/dev/null || fail 'Keycloak identity ConfigMap is invalid'
  kubectl -n identity get secret keycloak-admin-client -o json | jq -e '
    (.data | keys | sort) == ["client-id", "client-secret"] and
    all(.data[]; type == "string" and length > 0)
  ' >/dev/null || fail 'Keycloak admin client Secret is invalid'
  kubectl -n identity get secret keycloak-database-ca -o json | jq -e '
    (.data | keys | sort) == ["tls.crt", "tls.key"] and
    all(.data[]; type == "string" and length > 0)
  ' >/dev/null || fail 'Keycloak database CA Secret is invalid'
}

if [[ "$mode" == preflight ]]; then
  printf 'Identity preflight completed\n'
  exit 0
fi

validate_secret_shape
if [[ "$mode" == apply ]]; then
  kubectl apply --server-side --field-manager=mattercodex-identity -f "$render" >/dev/null
  kubectl -n identity wait --for=condition=Ready certificate/sso-public-tls --timeout=5m >/dev/null ||
    fail 'Keycloak public certificate is not ready'
  kubectl -n identity wait --for=condition=Ready certificate/keycloak-postgresql-tls --timeout=5m >/dev/null ||
    fail 'Keycloak PostgreSQL certificate is not ready'
  database_tls_revision=$(kubectl -n identity get secret keycloak-postgresql-tls \
    -o jsonpath='{.metadata.resourceVersion}')
  [[ "$database_tls_revision" =~ ^[1-9][0-9]*$ ]] || fail 'Keycloak PostgreSQL TLS revision is invalid'
  kubectl -n identity patch statefulset keycloak-postgresql --type=merge \
    -p "{\"spec\":{\"template\":{\"metadata\":{\"annotations\":{\"mattercodex.io/database-tls-revision\":\"$database_tls_revision\"}}}}}" >/dev/null
  kubectl -n identity rollout status statefulset/keycloak-postgresql --timeout=10m >/dev/null ||
    fail 'Keycloak PostgreSQL rollout failed'
  kubectl -n identity rollout status deployment/sso --timeout=10m >/dev/null ||
    fail 'Keycloak rollout failed'
fi

kubectl -n identity get deployment sso -o json | jq -e \
  --arg image 'quay.io/keycloak/keycloak:26.7.1@sha256:f1f1f01e472c8a78df40d8f2a49a925274eda4d3d80d5f6edbb5c880ee3c01c6' '
    .spec.template.spec.containers[] | select(.name == "keycloak") |
    .image == $image and
    any(.env[]; .name == "KC_HTTP_HOST" and .value == "0.0.0.0") and
    any(.env[]; .name == "KC_HTTP_MANAGEMENT_HOST" and .value == "0.0.0.0") and
    any(.env[]; .name == "KC_DB_URL" and
      (.value | contains("sslmode=verify-full&sslrootcert=")))
  ' >/dev/null || fail 'Keycloak image readback mismatch'
kubectl -n identity get service sso -o json | jq -e '
  . as $service |
  ([$service.spec.ports[] | .name] | sort) == ["https", "management"] and
  all($service.spec.ports[]; .port != 8080 and .targetPort != 8080 and .targetPort != "http")
' >/dev/null || fail 'Keycloak Service exposes the reconciliation HTTP listener'
kubectl -n identity get networkpolicy sso-exact-paths -o json | jq -e '
  all(.spec.ingress[]?.ports[]?; .port != 8080 and .port != "http")
' >/dev/null || fail 'Keycloak NetworkPolicy exposes the reconciliation HTTP listener'
[[ "$(kubectl -n identity get ingress sso -o jsonpath='{.spec.rules[0].host}')" == "$oidc_host" ]] ||
  fail 'Keycloak Ingress host readback mismatch'
[[ "$(kubectl -n identity get ingress sso -o jsonpath='{.spec.rules[0].http.paths[0].backend.service.port.name}')" == https ]] ||
  fail 'Keycloak Ingress does not use the TLS backend'
kubectl -n identity exec keycloak-postgresql-0 -- sh -ec '
  export PGPASSWORD=$POSTGRES_PASSWORD PGSSLMODE=verify-full
  export PGSSLROOTCERT=/var/run/secrets/mattercodex/postgresql/ca.crt
  export PGHOST=keycloak-postgresql.identity.svc.cluster.local PGHOSTADDR=127.0.0.1
  psql -U keycloak -d keycloak -Atc \
    "select ssl from pg_stat_ssl where pid = pg_backend_pid()"
' | grep -Fxq t || fail 'Keycloak PostgreSQL TLS readback failed'
database_tls_revision=$(kubectl -n identity get secret keycloak-postgresql-tls \
  -o jsonpath='{.metadata.resourceVersion}')
[[ "$(kubectl -n identity get statefulset keycloak-postgresql \
  -o jsonpath='{.spec.template.metadata.annotations.mattercodex\.io/database-tls-revision}')" == "$database_tls_revision" ]] ||
  fail 'Keycloak PostgreSQL TLS rollout revision mismatch'
printf 'Identity bootstrap completed: %s\n' "$mode"
