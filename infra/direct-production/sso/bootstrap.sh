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

validate_admin_client_secret() {
  kubectl --context "$context" -n identity get secret keycloak-admin-client -o json |
    jq -e '
      (.data | keys | sort) == ["client-id", "client-secret"] and
      (.data["client-id"] | @base64d | test("^[a-z0-9][a-z0-9-]{2,63}$")) and
      (.data["client-secret"] | @base64d | length >= 32)
    ' >/dev/null || fail "Keycloak admin client Secret is invalid"
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

write_keycloak_admin_curl_config() {
  local token_response=$1 keycloak_admin_token
  keycloak_admin_token=$(jq -er '.access_token | select(type == "string" and length > 0)' "$token_response") ||
    fail "Keycloak admin token is invalid"
  printf 'silent\nshow-error\nfail\nconnect-timeout = 5\nmax-time = 15\nheader = "Authorization: Bearer %s"\n' \
    "$keycloak_admin_token" >"$temporary_directory/keycloak-admin-curl.conf"
  unset keycloak_admin_token
}

wait_keycloak_public_endpoint() {
  local attempt
  for attempt in {1..30}; do
    if curl --fail --silent --show-error --connect-timeout 3 --max-time 5 \
      https://sso.kodex.works/realms/master/.well-known/openid-configuration >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done
  fail "Keycloak public endpoint is not ready"
}

keycloak_bootstrap_admin_login() {
  local secret_json admin_username admin_password token_request token_response
  secret_json=$(kubectl --context "$context" -n identity get secret keycloak-bootstrap -o json)
  admin_username=$(jq -er '.data["admin-username"] | @base64d' <<<"$secret_json")
  admin_password=$(jq -er '.data["admin-password"] | @base64d' <<<"$secret_json")
  token_request="$temporary_directory/keycloak-bootstrap-admin-token-request"
  token_response="$temporary_directory/keycloak-bootstrap-admin-token-response"
  jq -jrn --arg username "$admin_username" --arg password "$admin_password" '
    "client_id=admin-cli&grant_type=password&username=\($username | @uri)&password=\($password | @uri)"
  ' >"$token_request"
  unset admin_password secret_json
  curl --fail --silent --show-error --connect-timeout 5 --max-time 15 \
    --retry 5 --retry-delay 1 --retry-all-errors \
    --header 'Content-Type: application/x-www-form-urlencoded' \
    --data-binary "@$token_request" \
    https://sso.kodex.works/realms/master/protocol/openid-connect/token >"$token_response" ||
    fail "Keycloak bootstrap admin authentication failed"
  write_keycloak_admin_curl_config "$token_response"
  unset admin_username
}

keycloak_service_admin_login() {
  local secret_json client_id client_secret token_request token_response
  validate_admin_client_secret
  secret_json=$(kubectl --context "$context" -n identity get secret keycloak-admin-client -o json)
  client_id=$(jq -er '.data["client-id"] | @base64d' <<<"$secret_json")
  client_secret=$(jq -er '.data["client-secret"] | @base64d' <<<"$secret_json")
  token_request="$temporary_directory/keycloak-service-admin-token-request"
  token_response="$temporary_directory/keycloak-service-admin-token-response"
  jq -jrn --arg client_id "$client_id" --arg client_secret "$client_secret" '
    "grant_type=client_credentials&client_id=\($client_id | @uri)&client_secret=\($client_secret | @uri)"
  ' >"$token_request"
  unset client_secret secret_json
  curl --fail --silent --show-error --connect-timeout 5 --max-time 15 \
    --retry 5 --retry-delay 1 --retry-all-errors \
    --header 'Content-Type: application/x-www-form-urlencoded' \
    --data-binary "@$token_request" \
    https://sso.kodex.works/realms/master/protocol/openid-connect/token >"$token_response" ||
    fail "Keycloak service admin authentication failed"
  write_keycloak_admin_curl_config "$token_response"
  unset client_id
}

keycloak_admin_api() {
  local realm=$1 method=$2 path=$3 body=${4:-}
  local arguments=(--config "$temporary_directory/keycloak-admin-curl.conf" --request "$method")
  if [[ -n "$body" ]]; then
    arguments+=(--header 'Content-Type: application/json' --data-binary "@$body")
  fi
  curl "${arguments[@]}" "https://sso.kodex.works/admin/realms/$realm$path"
}

keycloak_admin_client_id() {
  local clients
  clients=$(keycloak_admin_api master GET '/clients?clientId=mattercodex-sso-bootstrap') ||
    fail "read Keycloak admin client"
  jq -er '
    select(type == "array" and length == 1) | .[0] |
    select(
      .clientId == "mattercodex-sso-bootstrap" and
      .enabled == true and
      .publicClient == false and
      .bearerOnly == false and
      .standardFlowEnabled == false and
      .directAccessGrantsEnabled == false and
      .serviceAccountsEnabled == true
    ) | .id
  ' <<<"$clients" || fail "Keycloak admin client readback mismatch"
}

readback_keycloak_admin_client() {
  local client_id service_account service_account_id role_mappings
  client_id=$(keycloak_admin_client_id)
  service_account=$(keycloak_admin_api master GET "/clients/$client_id/service-account-user") ||
    fail "read Keycloak admin service account"
  service_account_id=$(jq -er '.id | select(type == "string" and length > 0)' <<<"$service_account") ||
    fail "Keycloak admin service account readback mismatch"
  role_mappings=$(keycloak_admin_api master GET "/users/$service_account_id/role-mappings/realm") ||
    fail "read Keycloak admin role mappings"
  jq -e 'any(.[]; .name == "admin" and .composite == true)' <<<"$role_mappings" >/dev/null ||
    fail "Keycloak admin service account role readback mismatch"
}

retire_temporary_bootstrap_admin() {
  local admin_username encoded_username users count temporary user_id
  admin_username=$(kubectl --context "$context" -n identity get secret keycloak-bootstrap \
    -o jsonpath='{.data.admin-username}' | base64 -d)
  encoded_username=$(jq -rn --arg value "$admin_username" '$value | @uri')
  users=$(keycloak_admin_api master GET "/users?username=$encoded_username&exact=true") ||
    fail "read temporary Keycloak bootstrap admin"
  count=$(jq 'length' <<<"$users")
  case "$count" in
    0) return ;;
    1) ;;
    *) fail "temporary Keycloak bootstrap admin is duplicated" ;;
  esac
  temporary=$(jq -r '.[0].attributes.is_temporary_admin == ["true"]' <<<"$users")
  [[ "$temporary" == true ]] || fail "refuse to delete non-temporary Keycloak bootstrap admin"
  user_id=$(jq -er '.[0].id' <<<"$users")
  keycloak_admin_api master DELETE "/users/$user_id" >/dev/null ||
    fail "delete temporary Keycloak bootstrap admin"
}

create_keycloak_admin_client() {
  local existing client_id client_secret_file client_id_file client_body
  local service_account service_account_id admin_role role_body
  existing=$(keycloak_admin_api master GET '/clients?clientId=mattercodex-sso-bootstrap') ||
    fail "read existing Keycloak admin client"
  case "$(jq 'length' <<<"$existing")" in
    0) ;;
    1)
      client_id=$(jq -er '.[0].id' <<<"$existing")
      keycloak_admin_api master DELETE "/clients/$client_id" >/dev/null ||
        fail "delete incomplete Keycloak admin client"
      ;;
    *) fail "Keycloak admin client is duplicated" ;;
  esac

  client_secret_file="$temporary_directory/keycloak-admin-client-secret"
  client_id_file="$temporary_directory/keycloak-admin-client-id"
  client_body="$temporary_directory/keycloak-admin-client.json"
  printf '%s' mattercodex-sso-bootstrap >"$client_id_file"
  openssl rand -hex 32 | tr -d '\n' >"$client_secret_file"
  jq -n --rawfile secret "$client_secret_file" '{
    clientId: "mattercodex-sso-bootstrap",
    name: "MatterCodex SSO bootstrap automation",
    enabled: true,
    protocol: "openid-connect",
    publicClient: false,
    bearerOnly: false,
    standardFlowEnabled: false,
    directAccessGrantsEnabled: false,
    serviceAccountsEnabled: true,
    clientAuthenticatorType: "client-secret",
    secret: $secret
  }' >"$client_body"
  keycloak_admin_api master POST '/clients' "$client_body" >/dev/null ||
    fail "create Keycloak admin client"

  client_id=$(keycloak_admin_client_id)
  service_account=$(keycloak_admin_api master GET "/clients/$client_id/service-account-user") ||
    fail "read new Keycloak admin service account"
  service_account_id=$(jq -er '.id' <<<"$service_account") ||
    fail "new Keycloak admin service account is invalid"
  admin_role=$(keycloak_admin_api master GET '/roles/admin') ||
    fail "read Keycloak master admin role"
  role_body="$temporary_directory/keycloak-admin-role.json"
  jq -s '.' <<<"$admin_role" >"$role_body"
  keycloak_admin_api master POST "/users/$service_account_id/role-mappings/realm" "$role_body" >/dev/null ||
    fail "assign Keycloak master admin role"

  kubectl --context "$context" -n identity create secret generic keycloak-admin-client \
    --from-file="client-id=$client_id_file" --from-file="client-secret=$client_secret_file" \
    --dry-run=client -o yaml |
    kubectl --context "$context" apply --server-side \
      --field-manager=mattercodex-sso-bootstrap -f - >/dev/null
  keycloak_service_admin_login
  readback_keycloak_admin_client
  retire_temporary_bootstrap_admin
}

ensure_keycloak_admin_client() {
  wait_keycloak_public_endpoint
  if kubectl --context "$context" -n identity get secret keycloak-admin-client >/dev/null 2>&1; then
    keycloak_service_admin_login
    readback_keycloak_admin_client
    return
  fi
  [[ "$mode" == apply ]] || fail "Keycloak admin client Secret is absent; run the documented recovery procedure"
  keycloak_bootstrap_admin_login
  create_keycloak_admin_client
}

control_center_client_id() {
  local clients
  clients=$(keycloak_admin_api mattercodex GET '/clients?clientId=mattercodex-control-center') ||
    fail "read Control Center client"
  jq -er '
    select(type == "array" and length == 1) | .[0] |
    select(.clientId == "mattercodex-control-center") | .id
  ' <<<"$clients" || fail "Control Center client readback mismatch"
}

reconcile_control_center_mappers() {
  local client_id mapper_name desired current mapper_id desired_with_id
  client_id=$(control_center_client_id)
  current=$(keycloak_admin_api mattercodex GET "/clients/$client_id/protocol-mappers/models") ||
    fail "read Control Center protocol mappers"
  for mapper_name in sub 'realm roles'; do
    desired="$temporary_directory/mapper-${mapper_name// /-}.json"
    jq -e --arg name "$mapper_name" '
      .clients[] | select(.clientId == "mattercodex-control-center") |
      .protocolMappers[] | select(.name == $name)
    ' "$script_directory/mattercodex-realm.json" >"$desired" ||
      fail "desired $mapper_name mapper is absent"
    count=$(jq --arg name "$mapper_name" '[.[] | select(.name == $name)] | length' <<<"$current")
    case "$count" in
      0)
        keycloak_admin_api mattercodex POST "/clients/$client_id/protocol-mappers/models" "$desired" >/dev/null ||
          fail "create $mapper_name mapper"
        ;;
      1)
        mapper_id=$(jq -er --arg name "$mapper_name" '.[] | select(.name == $name) | .id' <<<"$current")
        desired_with_id="$temporary_directory/mapper-${mapper_name// /-}-with-id.json"
        jq --arg id "$mapper_id" '. + {id: $id}' "$desired" >"$desired_with_id"
        keycloak_admin_api mattercodex PUT "/clients/$client_id/protocol-mappers/models/$mapper_id" "$desired_with_id" >/dev/null ||
          fail "update $mapper_name mapper"
        ;;
      *) fail "$mapper_name mapper is duplicated" ;;
    esac
  done
}

readback_control_center_mappers() {
  local client_id mappers
  client_id=$(control_center_client_id)
  mappers=$(keycloak_admin_api mattercodex GET "/clients/$client_id/protocol-mappers/models") ||
    fail "read Control Center protocol mappers"
  jq -e '
    ([.[] | select(
      .name == "sub" and
      .protocol == "openid-connect" and
      .protocolMapper == "oidc-sub-mapper" and
      .config["access.token.claim"] == "true" and
      .config["introspection.token.claim"] == "true"
    )] | length == 1) and
    ([.[] | select(
      .name == "realm roles" and
      .protocol == "openid-connect" and
      .protocolMapper == "oidc-usermodel-realm-role-mapper" and
      .config["claim.name"] == "realm_access.roles" and
      .config["multivalued"] == "true" and
      .config["access.token.claim"] == "true" and
      .config["introspection.token.claim"] == "true"
    )] | length == 1)
  ' <<<"$mappers" >/dev/null || fail "Control Center protocol mapper readback mismatch"
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
  ensure_keycloak_admin_client
  reconcile_control_center_mappers
  update_oidc_ca
fi

validate_bootstrap_secret
if [[ "$mode" == readback ]]; then
  ensure_keycloak_admin_client
fi
readback_keycloak_admin_client
readback_control_center_mappers
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
