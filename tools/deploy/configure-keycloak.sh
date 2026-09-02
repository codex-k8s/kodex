#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Keycloak bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode apply|readback|retire-initial-passwords" \
    '  --public-origin <https-origin> --grafana-origin <https-origin>' \
    '  --headlamp-origin <https-origin>' \
    '  [--namespace identity] [--deployment sso]' \
    '  [--realm kodex] [--admin-secret keycloak-admin-client]' \
    '  [--bootstrap-secret keycloak-bootstrap]' \
    '  [--identities-configmap keycloak-identities]' \
    '  [--initial-password-secret keycloak-initial-passwords]' >&2
}

expected_context=""
mode=""
public_origin=""
grafana_origin=""
headlamp_origin=""
namespace=identity
deployment=sso
realm=kodex
admin_secret=keycloak-admin-client
bootstrap_secret=keycloak-bootstrap
identities_configmap=keycloak-identities
initial_password_secret=keycloak-initial-passwords
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --public-origin) public_origin="${2:-}"; shift 2 ;;
    --grafana-origin) grafana_origin="${2:-}"; shift 2 ;;
    --headlamp-origin) headlamp_origin="${2:-}"; shift 2 ;;
    --namespace) namespace="${2:-}"; shift 2 ;;
    --deployment) deployment="${2:-}"; shift 2 ;;
    --realm) realm="${2:-}"; shift 2 ;;
    --admin-secret) admin_secret="${2:-}"; shift 2 ;;
    --bootstrap-secret) bootstrap_secret="${2:-}"; shift 2 ;;
    --identities-configmap) identities_configmap="${2:-}"; shift 2 ;;
    --initial-password-secret) initial_password_secret="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail 'exact context is required'
case "$mode" in apply|readback|retire-initial-passwords) ;; *) fail 'mode is invalid' ;; esac
[[ "$public_origin" =~ ^https://[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail 'public origin is invalid'
for management_origin in "$grafana_origin" "$headlamp_origin"; do
  [[ "$management_origin" =~ ^https://[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail 'management origin is invalid'
done
[[ "$public_origin" != "$grafana_origin" && "$public_origin" != "$headlamp_origin" &&
  "$grafana_origin" != "$headlamp_origin" ]] ||
  fail 'management origins must be unique'
for resource_name in "$namespace" "$deployment" "$realm" "$admin_secret" "$bootstrap_secret" \
  "$identities_configmap" "$initial_password_secret"; do
  [[ "$resource_name" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail 'resource name is invalid'
done
for command_name in base64 jq kubectl stat; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail 'current Kubernetes context mismatch'
kubectl -n "$namespace" get deployment "$deployment" >/dev/null 2>&1 || fail 'Keycloak deployment is absent'
kubectl -n "$namespace" rollout status "deployment/$deployment" --timeout=300s >/dev/null ||
  fail 'Keycloak deployment is unavailable'

temporary_directory=$(mktemp -d)
chmod 0700 "$temporary_directory"
cleanup() {
  rm -rf -- "$temporary_directory"
  kubectl -n "$namespace" exec "deployment/$deployment" -- sh -ec \
    'rm -f /tmp/kodex-kcadm.config /tmp/kodex-kcadm-bootstrap.config' \
    >/dev/null 2>&1 || true
}
trap cleanup EXIT

read_secret_key() {
  local secret_name=$1 key=$2 output_file=$3
  kubectl -n "$namespace" get secret "$secret_name" -o "jsonpath={.data['${key//./\\.}']}" |
    base64 -d >"$output_file"
  [[ -s "$output_file" && ! -L "$output_file" ]] || fail "Keycloak secret key is absent: $secret_name/$key"
  chmod 0600 "$output_file"
}

read_config_key() {
  local configmap_name=$1 key=$2 output_file=$3
  kubectl -n "$namespace" get configmap "$configmap_name" -o "jsonpath={.data['${key//./\\.}']}" >"$output_file"
  [[ -s "$output_file" && ! -L "$output_file" ]] || fail "Keycloak config key is absent: $configmap_name/$key"
  chmod 0600 "$output_file"
}

read_single_line_secret() {
  local file_path=$1 label=$2 value
  [[ -f "$file_path" && -s "$file_path" && ! -L "$file_path" ]] || fail "$label is absent"
  value=$(<"$file_path")
  [[ -n "$value" && "$value" != *$'\n'* && "$value" != *$'\r'* ]] ||
    fail "$label must be a non-empty single line"
  printf '%s' "$value"
}

read_secret_key "$admin_secret" client-id "$temporary_directory/admin-client-id"
read_secret_key "$admin_secret" client-secret "$temporary_directory/admin-client-secret"
read_secret_key "$bootstrap_secret" admin-username "$temporary_directory/bootstrap-admin-username"
read_secret_key "$bootstrap_secret" admin-password "$temporary_directory/bootstrap-admin-password"
read_secret_key "$bootstrap_secret" organization-id "$temporary_directory/organization-id"
read_config_key "$identities_configmap" admin-username "$temporary_directory/admin-username"
read_config_key "$identities_configmap" owner-username "$temporary_directory/owner-username"
read_config_key "$identities_configmap" owner-email "$temporary_directory/owner-email"

admin_client_id=$(read_single_line_secret "$temporary_directory/admin-client-id" 'Keycloak admin client ID')
admin_client_secret=$(read_single_line_secret "$temporary_directory/admin-client-secret" 'Keycloak admin client secret')
bootstrap_admin_username=$(read_single_line_secret "$temporary_directory/bootstrap-admin-username" 'Keycloak administrator username')
bootstrap_admin_password=$(read_single_line_secret "$temporary_directory/bootstrap-admin-password" 'Keycloak administrator password')
admin_username=$(read_single_line_secret "$temporary_directory/admin-username" 'permanent Keycloak administrator username')
organization_id=$(read_single_line_secret "$temporary_directory/organization-id" 'bootstrap organization ID')
owner_username=$(read_single_line_secret "$temporary_directory/owner-username" 'bootstrap owner username')
owner_email=$(read_single_line_secret "$temporary_directory/owner-email" 'bootstrap owner email')
[[ "$organization_id" =~ ^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$ ]] ||
  fail 'organization ID is invalid'
[[ "$bootstrap_admin_username" =~ ^[a-zA-Z0-9._@-]{3,128}$ ]] || fail 'Keycloak administrator username is invalid'
[[ ${#bootstrap_admin_password} -ge 20 ]] || fail 'Keycloak administrator password is too short'
[[ "$admin_username" =~ ^[a-zA-Z0-9._@-]{3,128}$ ]] || fail 'permanent Keycloak administrator username is invalid'
[[ "$admin_username" != "$bootstrap_admin_username" ]] || fail 'permanent and bootstrap administrators must differ'
[[ "$owner_username" =~ ^[a-zA-Z0-9._@-]{3,128}$ ]] || fail 'owner username is invalid'
[[ "$owner_username" != "$admin_username" && "$owner_username" != "$bootstrap_admin_username" ]] ||
  fail 'owner and administrator usernames must differ'
[[ "$owner_email" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]] || fail 'owner email is invalid'

read_initial_password() {
  local key=$1 label=$2
  local path="$temporary_directory/$key"
  read_secret_key "$initial_password_secret" "$key" "$path"
  local value
  value=$(read_single_line_secret "$path" "$label")
  [[ ${#value} -ge 20 ]] || fail "$label is too short"
  printf '%s' "$value"
}

keycloak_request() {
  printf '%s\n%s\n' "$admin_client_id" "$admin_client_secret" |
    kubectl -n "$namespace" exec -i "deployment/$deployment" -- sh -ec '
      IFS= read -r client_id
      IFS= read -r client_secret
      config=/tmp/kodex-kcadm.config
      command=/opt/keycloak/bin/kcadm.sh
      "$command" config credentials --config "$config" --server http://127.0.0.1:8080 \
        --realm master --client "$client_id" --secret "$client_secret" >/dev/null 2>&1
      exec "$command" "$@" --config "$config"
    ' sh "$@"
}

keycloak_bootstrap_request() {
  local bootstrap_username bootstrap_password
  bootstrap_username=$(kubectl -n "$namespace" get secret "$bootstrap_secret" -o jsonpath='{.data.admin-username}' | base64 -d)
  bootstrap_password=$(kubectl -n "$namespace" get secret "$bootstrap_secret" -o jsonpath='{.data.admin-password}' | base64 -d)
  printf '%s\n%s\n' "$bootstrap_username" "$bootstrap_password" |
    kubectl -n "$namespace" exec -i "deployment/$deployment" -- sh -ec '
      IFS= read -r username
      IFS= read -r password
      config=/tmp/kodex-kcadm-bootstrap.config
      command=/opt/keycloak/bin/kcadm.sh
      "$command" config credentials --config "$config" --server http://127.0.0.1:8080 \
        --realm master --user "$username" --password "$password" >/dev/null 2>&1
      exec "$command" "$@" --config "$config"
    ' sh "$@"
  unset bootstrap_password
}

ensure_admin_client() {
  local existing client_uuid service_account_id
  if keycloak_request get realms >/dev/null 2>&1; then
    return
  fi
  [[ "$mode" == apply ]] || fail 'Keycloak admin service client is unavailable'
  existing=$(keycloak_bootstrap_request get clients -r master -q "clientId=$admin_client_id") ||
    fail 'Keycloak bootstrap admin authentication failed'
  case "$(jq 'length' <<<"$existing")" in
    0)
      keycloak_bootstrap_request create clients -r master \
        -s "clientId=$admin_client_id" -s enabled=true -s publicClient=false \
        -s bearerOnly=false -s standardFlowEnabled=false -s directAccessGrantsEnabled=false \
        -s serviceAccountsEnabled=true -s "secret=$admin_client_secret" >/dev/null
      ;;
    1) ;;
    *) fail 'Keycloak admin service client is duplicated' ;;
  esac
  client_uuid=$(keycloak_bootstrap_request get clients -r master -q "clientId=$admin_client_id" |
    jq -er --arg client_id "$admin_client_id" '[.[] | select(.clientId == $client_id)] | if length == 1 then .[0].id else error("admin client mismatch") end')
  keycloak_bootstrap_request update "clients/$client_uuid" -r master \
    -s enabled=true -s publicClient=false -s bearerOnly=false -s standardFlowEnabled=false \
    -s directAccessGrantsEnabled=false -s serviceAccountsEnabled=true -s "secret=$admin_client_secret" >/dev/null
  service_account_id=$(keycloak_bootstrap_request get "clients/$client_uuid/service-account-user" -r master | jq -er '.id')
  kubectl -n "$namespace" exec "deployment/$deployment" -- sh -ec '
    exec /opt/keycloak/bin/kcadm.sh add-roles --config /tmp/kodex-kcadm-bootstrap.config \
      -r master --uid "$1" --rolename admin
  ' sh "$service_account_id" >/dev/null
  keycloak_request get realms >/dev/null 2>&1 || fail 'Keycloak admin service client activation failed'
}

find_client_id() {
  local client_id=$1 client_realm=${2:-$realm}
  keycloak_request get clients -r "$client_realm" -q "clientId=$client_id" |
    jq -er --arg client_id "$client_id" '
      [ .[] | select(.clientId == $client_id) ] |
      if length == 1 then .[0].id else error("client identity is ambiguous") end
    '
}

find_scope_id() {
  local scope_name=$1
  keycloak_request get client-scopes -r "$realm" |
    jq -er --arg scope_name "$scope_name" '
      [ .[] | select(.name == $scope_name) ] |
      if length == 1 then .[0].id else error("client scope identity is ambiguous") end
    '
}

reconcile_mapper() {
  local client_uuid=$1 mapper_name=$2 mapper_type=$3 config_json=$4 client_realm=${5:-$realm}
  local mapper_json mapper_count mapper_id
  config_json=$(jq -ceS 'if type == "object" then . else error("mapper config must be an object") end' \
    <<<"$config_json") || fail "Keycloak protocol mapper config is invalid: $mapper_name"
  mapper_json=$(keycloak_request get "clients/$client_uuid/protocol-mappers/models" -r "$client_realm")
  mapper_count=$(jq -er --arg mapper_name "$mapper_name" \
    '[.[] | select(.name == $mapper_name)] | length' <<<"$mapper_json") ||
    fail "Keycloak protocol mapper list is invalid: $mapper_name"
  case "$mapper_count" in
    0)
      keycloak_request create "clients/$client_uuid/protocol-mappers/models" -r "$client_realm" \
        -s "name=$mapper_name" -s protocol=openid-connect -s "protocolMapper=$mapper_type" \
        -s "config=$config_json" >/dev/null
      ;;
    1)
      mapper_id=$(jq -er --arg mapper_name "$mapper_name" '
        [.[] | select(.name == $mapper_name)] | .[0].id |
        select(type == "string" and length > 0)
      ' <<<"$mapper_json") || fail "Keycloak protocol mapper ID is invalid: $mapper_name"
      keycloak_request update "clients/$client_uuid/protocol-mappers/models/$mapper_id" \
        -r "$client_realm" -s "name=$mapper_name" -s protocol=openid-connect \
        -s "protocolMapper=$mapper_type" -s "config=$config_json" >/dev/null
      ;;
    *) fail "Keycloak protocol mapper is duplicated: $mapper_name" ;;
  esac
}

require_mapper_exact() {
  local mapper_json=$1 mapper_name=$2 mapper_type=$3 config_json=$4
  config_json=$(jq -ceS 'if type == "object" then . else error("mapper config must be an object") end' \
    <<<"$config_json") || return 1
  jq -e --arg mapper_name "$mapper_name" --arg mapper_type "$mapper_type" \
    --argjson config "$config_json" '
      [.[] | select(.name == $mapper_name)] |
      length == 1 and .[0].protocol == "openid-connect" and
      .[0].protocolMapper == $mapper_type and .[0].config == $config
    ' <<<"$mapper_json" >/dev/null
}

read_management_client_secret() {
  local client_id=$1 namespace_name=$2 secret_name=$3
  local secret_json actual_client_id client_secret
  secret_json=$(kubectl -n "$namespace_name" get secret "$secret_name" -o json) ||
    fail "management OIDC Secret is absent: $namespace_name/$secret_name"
  actual_client_id=$(jq -er '.data["client-id"] | @base64d' <<<"$secret_json") ||
    fail "management OIDC client ID is absent: $secret_name"
  client_secret=$(jq -er '.data["client-secret"] | @base64d' <<<"$secret_json") ||
    fail "management OIDC client secret is absent: $secret_name"
  [[ "$actual_client_id" == "$client_id" && ${#client_secret} -ge 32 ]] ||
    fail "management OIDC Secret is invalid: $secret_name"
  printf '%s' "$client_secret"
}

reconcile_confidential_client() {
  local client_id=$1 origin=$2 namespace_name=$3 secret_name=$4
  local client_realm=${5:-$realm} client_uuid client_secret attributes
  client_secret=$(read_management_client_secret "$client_id" "$namespace_name" "$secret_name")
  if ! find_client_id "$client_id" "$client_realm" >/dev/null 2>&1; then
    keycloak_request create clients -r "$client_realm" -s "clientId=$client_id" >/dev/null
  fi
  client_uuid=$(find_client_id "$client_id" "$client_realm")
  attributes=$(jq -cn --arg logout "$origin/*" '{
    "pkce.code.challenge.method":"S256",
    "post.logout.redirect.uris":$logout
  }')
  keycloak_request update "clients/$client_uuid" -r "$client_realm" \
    -s enabled=true -s publicClient=false -s bearerOnly=false -s protocol=openid-connect \
    -s standardFlowEnabled=true -s implicitFlowEnabled=false \
    -s directAccessGrantsEnabled=false -s serviceAccountsEnabled=false \
    -s "redirectUris=[\"$origin/oauth2/callback\"]" \
    -s "webOrigins=[\"$origin\"]" -s "attributes=$attributes" -s "secret=$client_secret" >/dev/null
  unset client_secret
  reconcile_mapper "$client_uuid" kodex-client-audience oidc-audience-mapper \
    "$(jq -cn --arg audience "$client_id" '{
      "included.client.audience":$audience,"access.token.claim":"true",
      "id.token.claim":"true","userinfo.token.claim":"false","introspection.token.claim":"true"
    }')" "$client_realm"
  reconcile_mapper "$client_uuid" kodex-realm-roles oidc-usermodel-realm-role-mapper \
    '{"claim.name":"realm_access.roles","jsonType.label":"String","multivalued":"true","access.token.claim":"true","id.token.claim":"true","userinfo.token.claim":"true","introspection.token.claim":"true"}' \
    "$client_realm"
}

readback_confidential_client() {
  local client_id=$1 origin=$2 redirect=$3 client_realm=${4:-$realm}
  local client_uuid client_json mapper_json audience_config roles_config
  client_uuid=$(find_client_id "$client_id" "$client_realm")
  client_json=$(keycloak_request get "clients/$client_uuid" -r "$client_realm")
  jq -e --arg origin "$origin" --arg redirect "$redirect" '
    .enabled == true and .publicClient == false and .standardFlowEnabled == true and
    .implicitFlowEnabled == false and .directAccessGrantsEnabled == false and
    .serviceAccountsEnabled == false and .redirectUris == [$redirect] and .webOrigins == [$origin]
  ' <<<"$client_json" >/dev/null || fail "management OIDC client readback failed: $client_id"
  mapper_json=$(keycloak_request get "clients/$client_uuid/protocol-mappers/models" -r "$client_realm")
  audience_config=$(jq -cn --arg audience "$client_id" '{
    "included.client.audience":$audience,"access.token.claim":"true",
    "id.token.claim":"true","userinfo.token.claim":"false","introspection.token.claim":"true"
  }')
  roles_config='{"claim.name":"realm_access.roles","jsonType.label":"String","multivalued":"true","access.token.claim":"true","id.token.claim":"true","userinfo.token.claim":"true","introspection.token.claim":"true"}'
  require_mapper_exact "$mapper_json" kodex-client-audience oidc-audience-mapper \
    "$audience_config" || fail "management OIDC mapper readback failed: $client_id"
  require_mapper_exact "$mapper_json" kodex-realm-roles oidc-usermodel-realm-role-mapper \
    "$roles_config" || fail "management OIDC mapper readback failed: $client_id"
}

ensure_admin_client

if [[ "$mode" == apply ]]; then
  admin_count=$(keycloak_request get users -r master -q "username=$admin_username" |
    jq -r --arg username "$admin_username" '[.[] | select(.username == $username)] | length')
  if [[ "$admin_count" == 0 ]]; then
    admin_initial_password=$(read_initial_password admin-initial-password 'permanent administrator initial password')
    keycloak_request create users -r master -s "username=$admin_username" -s enabled=true \
      -s "firstName=$admin_username" -s lastName=Administrator -s 'requiredActions=[]' >/dev/null
    printf '%s\n%s\n%s\n' "$admin_client_id" "$admin_client_secret" "$admin_initial_password" |
      kubectl -n "$namespace" exec -i "deployment/$deployment" -- sh -ec '
        IFS= read -r client_id
        IFS= read -r client_secret
        IFS= read -r administrator_password
        config=/tmp/kodex-kcadm.config
        command=/opt/keycloak/bin/kcadm.sh
        "$command" config credentials --config "$config" --server http://127.0.0.1:8080 \
          --realm master --client "$client_id" --secret "$client_secret" >/dev/null 2>&1
        "$command" set-password --config "$config" -r master --username "$1" \
          --new-password "$administrator_password" >/dev/null
      ' sh "$admin_username"
    unset admin_initial_password
  elif [[ "$admin_count" != 1 ]]; then
    fail 'Keycloak administrator identity is ambiguous'
  fi
  keycloak_request update "users/$(keycloak_request get users -r master -q "username=$admin_username" |
    jq -er --arg username "$admin_username" '.[] | select(.username == $username) | .id')" \
    -r master -s enabled=true -s "firstName=$admin_username" -s lastName=Administrator \
    -s 'requiredActions=[]' >/dev/null
  keycloak_request add-roles -r master --uusername "$admin_username" --rolename admin >/dev/null

  bootstrap_count=$(keycloak_request get users -r master -q "username=$bootstrap_admin_username" |
    jq -r --arg username "$bootstrap_admin_username" '[.[] | select(.username == $username)] | length')
  case "$bootstrap_count" in
    0) ;;
    1)
      bootstrap_id=$(keycloak_request get users -r master -q "username=$bootstrap_admin_username" |
        jq -er --arg username "$bootstrap_admin_username" '.[] | select(.username == $username) | .id')
      keycloak_request delete "users/$bootstrap_id" -r master >/dev/null
      ;;
    *) fail 'temporary Keycloak bootstrap administrator is ambiguous' ;;
  esac
fi

if [[ "$mode" == apply ]]; then
  if ! keycloak_request get "realms/$realm" >/dev/null 2>&1; then
    keycloak_request create realms -s "realm=$realm" -s enabled=true >/dev/null
  fi
  keycloak_request update "realms/$realm" \
    -s enabled=true -s sslRequired=external -s registrationAllowed=false \
    -s resetPasswordAllowed=true -s rememberMe=true -s loginWithEmailAllowed=true \
    -s duplicateEmailsAllowed=false -s verifyEmail=false -s accessTokenLifespan=300 \
    -s ssoSessionIdleTimeout=28800 -s ssoSessionMaxLifespan=43200 >/dev/null

  if ! keycloak_request get "roles/kodex-owner" -r "$realm" >/dev/null 2>&1; then
    keycloak_request create roles -r "$realm" -s name=kodex-owner \
      -s 'description=Kodex platform owner' >/dev/null
  fi
  if ! find_client_id kodex-control-api >/dev/null 2>&1; then
    keycloak_request create clients -r "$realm" -s clientId=kodex-control-api >/dev/null
  fi
  control_api_id=$(find_client_id kodex-control-api)
  keycloak_request update "clients/$control_api_id" -r "$realm" \
    -s enabled=true -s publicClient=false -s bearerOnly=true -s protocol=openid-connect \
    -s standardFlowEnabled=false -s implicitFlowEnabled=false \
    -s directAccessGrantsEnabled=false -s serviceAccountsEnabled=false >/dev/null

  if ! find_client_id kodex-control-center >/dev/null 2>&1; then
    keycloak_request create clients -r "$realm" -s clientId=kodex-control-center >/dev/null
  fi
  control_center_id=$(find_client_id kodex-control-center)
  client_attributes=$(jq -cn --arg logout "$public_origin/*" '{
    "pkce.code.challenge.method":"S256",
    "post.logout.redirect.uris":$logout,
    "access.token.lifespan":"3600"
  }')
  keycloak_request update "clients/$control_center_id" -r "$realm" \
    -s enabled=true -s publicClient=true -s bearerOnly=false -s protocol=openid-connect \
    -s standardFlowEnabled=true -s implicitFlowEnabled=false \
    -s directAccessGrantsEnabled=false -s serviceAccountsEnabled=false \
    -s "redirectUris=[\"$public_origin/auth/callback\"]" \
    -s "webOrigins=[\"$public_origin\"]" -s "attributes=$client_attributes" >/dev/null

  if ! find_scope_id kodex.owner >/dev/null 2>&1; then
    keycloak_request create client-scopes -r "$realm" -s name=kodex.owner \
      -s protocol=openid-connect \
      -s 'attributes={"include.in.token.scope":"true","display.on.consent.screen":"false"}' >/dev/null
  fi
  owner_scope_id=$(find_scope_id kodex.owner)
  keycloak_request update "client-scopes/$owner_scope_id" -r "$realm" \
    -s name=kodex.owner -s protocol=openid-connect \
    -s 'attributes={"include.in.token.scope":"true","display.on.consent.screen":"false"}' >/dev/null
  keycloak_request update "clients/$control_center_id/optional-client-scopes/$owner_scope_id" \
    -r "$realm" -n >/dev/null

  while IFS= read -r mapper_id; do
    [[ -n "$mapper_id" ]] || continue
    keycloak_request delete "clients/$control_center_id/protocol-mappers/models/$mapper_id" \
      -r "$realm" >/dev/null
  done < <(keycloak_request get "clients/$control_center_id/protocol-mappers/models" -r "$realm" |
    jq -r '
      .[] | select(.name == "organization-id" or .name == "session-revision" or
        .name == "control-api-audience" or .name == "realm roles") |
      .id
    ')
  reconcile_mapper "$control_center_id" kodex-organization-id oidc-hardcoded-claim-mapper \
    "$(jq -cn --arg value "$organization_id" '{
      "claim.name":"organization_id","claim.value":$value,"jsonType.label":"String",
      "access.token.claim":"true","id.token.claim":"true","userinfo.token.claim":"true",
      "introspection.token.claim":"true"
    }')"
  reconcile_mapper "$control_center_id" kodex-session-revision oidc-hardcoded-claim-mapper \
    '{"claim.name":"session_revision","claim.value":"1","jsonType.label":"long","access.token.claim":"true","id.token.claim":"true","userinfo.token.claim":"true","introspection.token.claim":"true"}'
  reconcile_mapper "$control_center_id" kodex-control-api-audience oidc-audience-mapper \
    '{"included.client.audience":"kodex-control-api","access.token.claim":"true","id.token.claim":"false","userinfo.token.claim":"false","introspection.token.claim":"true"}'
  reconcile_mapper "$control_center_id" kodex-realm-roles oidc-usermodel-realm-role-mapper \
    '{"claim.name":"realm_access.roles","jsonType.label":"String","multivalued":"true","access.token.claim":"true","id.token.claim":"true","userinfo.token.claim":"true","introspection.token.claim":"true"}'
  reconcile_mapper "$control_center_id" kodex-groups oidc-group-membership-mapper \
    '{"claim.name":"groups","full.path":"false","multivalued":"true","access.token.claim":"true","id.token.claim":"true","userinfo.token.claim":"true","introspection.token.claim":"true"}'

  owner_count=$(keycloak_request get users -r "$realm" -q "username=$owner_username" |
    jq -r --arg username "$owner_username" '[.[] | select(.username == $username)] | length')
  if [[ "$owner_count" == 0 ]]; then
    owner_password=$(read_initial_password owner-initial-password 'owner initial password')
    keycloak_request create users -r "$realm" -s "username=$owner_username" -s enabled=true \
      -s "email=$owner_email" -s emailVerified=true -s "firstName=$owner_username" \
      -s lastName=Owner -s 'requiredActions=[]' >/dev/null
    printf '%s\n%s\n%s\n' "$admin_client_id" "$admin_client_secret" "$owner_password" |
      kubectl -n "$namespace" exec -i "deployment/$deployment" -- sh -ec '
        IFS= read -r client_id
        IFS= read -r client_secret
        IFS= read -r owner_password
        config=/tmp/kodex-kcadm.config
        command=/opt/keycloak/bin/kcadm.sh
        "$command" config credentials --config "$config" --server http://127.0.0.1:8080 \
          --realm master --client "$client_id" --secret "$client_secret" >/dev/null 2>&1
        "$command" set-password --config "$config" -r "$1" --username "$2" \
          --new-password "$owner_password" >/dev/null
      ' sh "$realm" "$owner_username"
    unset owner_password
  elif [[ "$owner_count" != 1 ]]; then
    fail 'owner identity is ambiguous'
  fi
  keycloak_request update "users/$(keycloak_request get users -r "$realm" -q "username=$owner_username" |
    jq -er --arg username "$owner_username" '.[] | select(.username == $username) | .id')" \
    -r "$realm" -s enabled=true -s "email=$owner_email" -s emailVerified=true \
    -s "firstName=$owner_username" -s lastName=Owner -s 'requiredActions=[]' >/dev/null
  keycloak_request add-roles -r "$realm" --uusername "$owner_username" \
    --rolename kodex-owner >/dev/null

  reconcile_confidential_client kodex-control-center-proxy "$public_origin" \
    kodex-system oauth2-control-center
  reconcile_confidential_client kodex-grafana-proxy "$grafana_origin" \
    observability oauth2-grafana
  reconcile_confidential_client kodex-headlamp-proxy "$headlamp_origin" \
    platform-admin oauth2-headlamp master
fi

realm_json=$(keycloak_request get "realms/$realm")
jq -e '.enabled == true and .registrationAllowed == false and .accessTokenLifespan == 300' \
  <<<"$realm_json" >/dev/null || fail 'realm readback failed'
control_center_id=$(find_client_id kodex-control-center)
client_json=$(keycloak_request get "clients/$control_center_id" -r "$realm")
jq -e --arg origin "$public_origin" '
  .enabled == true and .publicClient == true and .standardFlowEnabled == true and
  .implicitFlowEnabled == false and .directAccessGrantsEnabled == false and
  .attributes."pkce.code.challenge.method" == "S256" and
  .attributes."access.token.lifespan" == "3600" and
  .redirectUris == [($origin + "/auth/callback")] and .webOrigins == [$origin] and
  (.optionalClientScopes | index("kodex.owner") != null)
' <<<"$client_json" >/dev/null || fail 'Control Center OIDC client readback failed'

mapper_json=$(keycloak_request get "clients/$control_center_id/protocol-mappers/models" -r "$realm")
organization_mapper_config=$(jq -cn --arg value "$organization_id" '{
  "claim.name":"organization_id","claim.value":$value,"jsonType.label":"String",
  "access.token.claim":"true","id.token.claim":"true","userinfo.token.claim":"true",
  "introspection.token.claim":"true"
}')
session_mapper_config='{"claim.name":"session_revision","claim.value":"1","jsonType.label":"long","access.token.claim":"true","id.token.claim":"true","userinfo.token.claim":"true","introspection.token.claim":"true"}'
audience_mapper_config='{"included.client.audience":"kodex-control-api","access.token.claim":"true","id.token.claim":"false","userinfo.token.claim":"false","introspection.token.claim":"true"}'
roles_mapper_config='{"claim.name":"realm_access.roles","jsonType.label":"String","multivalued":"true","access.token.claim":"true","id.token.claim":"true","userinfo.token.claim":"true","introspection.token.claim":"true"}'
groups_mapper_config='{"claim.name":"groups","full.path":"false","multivalued":"true","access.token.claim":"true","id.token.claim":"true","userinfo.token.claim":"true","introspection.token.claim":"true"}'
require_mapper_exact "$mapper_json" kodex-organization-id oidc-hardcoded-claim-mapper \
  "$organization_mapper_config" || fail 'OIDC claim mapper readback failed'
require_mapper_exact "$mapper_json" kodex-session-revision oidc-hardcoded-claim-mapper \
  "$session_mapper_config" || fail 'OIDC claim mapper readback failed'
require_mapper_exact "$mapper_json" kodex-control-api-audience oidc-audience-mapper \
  "$audience_mapper_config" || fail 'OIDC claim mapper readback failed'
require_mapper_exact "$mapper_json" kodex-realm-roles oidc-usermodel-realm-role-mapper \
  "$roles_mapper_config" || fail 'OIDC claim mapper readback failed'
require_mapper_exact "$mapper_json" kodex-groups oidc-group-membership-mapper \
  "$groups_mapper_config" || fail 'OIDC claim mapper readback failed'

owner_id=$(keycloak_request get users -r "$realm" -q "username=$owner_username" |
  jq -er --arg username "$owner_username" '
    [ .[] | select(.username == $username and .enabled == true and
      (.firstName | length) > 0 and (.lastName | length) > 0 and
      ((.requiredActions // []) | length) == 0) ] |
    if length == 1 then .[0].id else error("owner identity readback failed") end
  ')
keycloak_request get "users/$owner_id/role-mappings/realm" -r "$realm" |
  jq -e 'any(.[]; .name == "kodex-owner")' >/dev/null || fail 'owner role readback failed'

readback_confidential_client kodex-control-center-proxy "$public_origin" "$public_origin/oauth2/callback"
readback_confidential_client kodex-grafana-proxy "$grafana_origin" "$grafana_origin/oauth2/callback"
readback_confidential_client kodex-headlamp-proxy "$headlamp_origin" "$headlamp_origin/oauth2/callback" master

administrator_id=$(keycloak_request get users -r master -q "username=$admin_username" |
  jq -er --arg username "$admin_username" '
    [.[] | select(.username == $username and .enabled == true and
      (.firstName | length) > 0 and (.lastName | length) > 0 and
      ((.requiredActions // []) | length) == 0)] |
    if length == 1 then .[0].id else error("Keycloak administrator readback failed") end
  ')
keycloak_request get "users/$administrator_id/role-mappings/realm" -r master |
  jq -e 'any(.[]; .name == "admin")' >/dev/null || fail 'Keycloak administrator role readback failed'
bootstrap_count=$(keycloak_request get users -r master -q "username=$bootstrap_admin_username" |
  jq -r --arg username "$bootstrap_admin_username" '[.[] | select(.username == $username)] | length')
[[ "$bootstrap_count" == 0 ]] || fail 'temporary Keycloak bootstrap administrator still exists'

if [[ "$mode" == retire-initial-passwords ]]; then
  kubectl -n "$namespace" delete secret "$initial_password_secret" --ignore-not-found >/dev/null
  ! kubectl -n "$namespace" get secret "$initial_password_secret" >/dev/null 2>&1 ||
    fail 'Keycloak initial password Secret still exists'
fi

unset bootstrap_admin_password admin_client_secret
printf 'Keycloak bootstrap completed: %s\n' "$mode"
