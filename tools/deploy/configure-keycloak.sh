#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Keycloak bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode apply|readback" \
    '  --public-origin <https-origin> [--namespace identity] [--deployment sso]' \
    '  [--realm mattercodex] [--admin-secret keycloak-admin-client]' \
    '  [--bootstrap-secret keycloak-bootstrap]' >&2
}

expected_context=""
mode=""
public_origin=""
namespace=identity
deployment=sso
realm=mattercodex
admin_secret=keycloak-admin-client
bootstrap_secret=keycloak-bootstrap
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --public-origin) public_origin="${2:-}"; shift 2 ;;
    --namespace) namespace="${2:-}"; shift 2 ;;
    --deployment) deployment="${2:-}"; shift 2 ;;
    --realm) realm="${2:-}"; shift 2 ;;
    --admin-secret) admin_secret="${2:-}"; shift 2 ;;
    --bootstrap-secret) bootstrap_secret="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail 'exact context is required'
[[ "$mode" == apply || "$mode" == readback ]] || fail 'mode is invalid'
[[ "$public_origin" =~ ^https://[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail 'public origin is invalid'
for resource_name in "$namespace" "$deployment" "$realm" "$admin_secret" "$bootstrap_secret"; do
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
trap 'rm -rf -- "$temporary_directory"' EXIT
chmod 0700 "$temporary_directory"

read_secret_key() {
  local secret_name=$1 key=$2 output_file=$3
  kubectl -n "$namespace" get secret "$secret_name" -o "jsonpath={.data['${key//./\\.}']}" |
    base64 -d >"$output_file"
  [[ -s "$output_file" && ! -L "$output_file" ]] || fail "Keycloak secret key is absent: $secret_name/$key"
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
read_secret_key "$bootstrap_secret" organization-id "$temporary_directory/organization-id"
read_secret_key "$bootstrap_secret" owner-username "$temporary_directory/owner-username"
read_secret_key "$bootstrap_secret" owner-email "$temporary_directory/owner-email"
read_secret_key "$bootstrap_secret" owner-initial-password "$temporary_directory/owner-password"

admin_client_id=$(read_single_line_secret "$temporary_directory/admin-client-id" 'Keycloak admin client ID')
admin_client_secret=$(read_single_line_secret "$temporary_directory/admin-client-secret" 'Keycloak admin client secret')
organization_id=$(read_single_line_secret "$temporary_directory/organization-id" 'bootstrap organization ID')
owner_username=$(read_single_line_secret "$temporary_directory/owner-username" 'bootstrap owner username')
owner_email=$(read_single_line_secret "$temporary_directory/owner-email" 'bootstrap owner email')
owner_password=$(read_single_line_secret "$temporary_directory/owner-password" 'bootstrap owner password')
[[ "$organization_id" =~ ^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$ ]] ||
  fail 'organization ID is invalid'
[[ "$owner_username" =~ ^[a-zA-Z0-9._@-]{3,128}$ ]] || fail 'owner username is invalid'
[[ "$owner_email" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]] || fail 'owner email is invalid'
[[ ${#owner_password} -ge 16 ]] || fail 'owner password is too short'

keycloak_request() {
  printf '%s\n%s\n' "$admin_client_id" "$admin_client_secret" |
    kubectl -n "$namespace" exec -i "deployment/$deployment" -- sh -ec '
      IFS= read -r client_id
      IFS= read -r client_secret
      config=/tmp/mattercodex-kcadm.config
      command=/opt/keycloak/bin/kcadm.sh
      "$command" config credentials --config "$config" --server http://127.0.0.1:8080 \
        --realm master --client "$client_id" --secret "$client_secret" >/dev/null 2>&1
      exec "$command" "$@" --config "$config"
    ' sh "$@"
}

find_client_id() {
  local client_id=$1
  keycloak_request get clients -r "$realm" -q "clientId=$client_id" |
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

replace_mapper() {
  local client_uuid=$1 mapper_name=$2 mapper_type=$3 config_json=$4
  keycloak_request create "clients/$client_uuid/protocol-mappers/models" -r "$realm" \
    -s "name=$mapper_name" -s protocol=openid-connect -s "protocolMapper=$mapper_type" \
    -s "config=$config_json" >/dev/null
}

if [[ "$mode" == apply ]]; then
  if ! keycloak_request get "realms/$realm" >/dev/null 2>&1; then
    keycloak_request create realms -s "realm=$realm" -s enabled=true >/dev/null
  fi
  keycloak_request update "realms/$realm" \
    -s enabled=true -s sslRequired=external -s registrationAllowed=false \
    -s resetPasswordAllowed=true -s rememberMe=true -s loginWithEmailAllowed=true \
    -s duplicateEmailsAllowed=false -s verifyEmail=false -s accessTokenLifespan=300 \
    -s ssoSessionIdleTimeout=28800 -s ssoSessionMaxLifespan=43200 >/dev/null

  if ! keycloak_request get "roles/mattercodex-owner" -r "$realm" >/dev/null 2>&1; then
    keycloak_request create roles -r "$realm" -s name=mattercodex-owner \
      -s 'description=MatterCodex platform owner' >/dev/null
  fi

  if ! find_client_id mattercodex-control-api >/dev/null 2>&1; then
    keycloak_request create clients -r "$realm" -s clientId=mattercodex-control-api >/dev/null
  fi
  control_api_id=$(find_client_id mattercodex-control-api)
  keycloak_request update "clients/$control_api_id" -r "$realm" \
    -s enabled=true -s publicClient=false -s bearerOnly=true -s protocol=openid-connect \
    -s standardFlowEnabled=false -s implicitFlowEnabled=false \
    -s directAccessGrantsEnabled=false -s serviceAccountsEnabled=false >/dev/null

  if ! find_client_id mattercodex-control-center >/dev/null 2>&1; then
    keycloak_request create clients -r "$realm" -s clientId=mattercodex-control-center >/dev/null
  fi
  control_center_id=$(find_client_id mattercodex-control-center)
  client_attributes=$(jq -cn --arg logout "$public_origin/*" '{
    "pkce.code.challenge.method":"S256",
    "post.logout.redirect.uris":$logout
  }')
  keycloak_request update "clients/$control_center_id" -r "$realm" \
    -s enabled=true -s publicClient=true -s bearerOnly=false -s protocol=openid-connect \
    -s standardFlowEnabled=true -s implicitFlowEnabled=false \
    -s directAccessGrantsEnabled=false -s serviceAccountsEnabled=false \
    -s "redirectUris=[\"$public_origin/auth/callback\"]" \
    -s "webOrigins=[\"$public_origin\"]" -s "attributes=$client_attributes" >/dev/null

  if ! find_scope_id mattercodex.owner >/dev/null 2>&1; then
    keycloak_request create client-scopes -r "$realm" -s name=mattercodex.owner \
      -s protocol=openid-connect \
      -s 'attributes={"include.in.token.scope":"true","display.on.consent.screen":"false"}' >/dev/null
  fi
  owner_scope_id=$(find_scope_id mattercodex.owner)
  keycloak_request update "client-scopes/$owner_scope_id" -r "$realm" \
    -s name=mattercodex.owner -s protocol=openid-connect \
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
        .name == "control-api-audience" or .name == "realm roles" or
        .name == "mattercodex-organization-id" or .name == "mattercodex-session-revision" or
        .name == "mattercodex-control-api-audience" or .name == "mattercodex-realm-roles") |
      .id
    ')
  replace_mapper "$control_center_id" mattercodex-organization-id oidc-hardcoded-claim-mapper \
    "$(jq -cn --arg value "$organization_id" '{
      "claim.name":"organization_id","claim.value":$value,"jsonType.label":"String",
      "access.token.claim":"true","id.token.claim":"true","userinfo.token.claim":"true",
      "introspection.token.claim":"true"
    }')"
  replace_mapper "$control_center_id" mattercodex-session-revision oidc-hardcoded-claim-mapper \
    '{"claim.name":"session_revision","claim.value":"1","jsonType.label":"long","access.token.claim":"true","id.token.claim":"true","userinfo.token.claim":"true","introspection.token.claim":"true"}'
  replace_mapper "$control_center_id" mattercodex-control-api-audience oidc-audience-mapper \
    '{"included.client.audience":"mattercodex-control-api","access.token.claim":"true","id.token.claim":"false","userinfo.token.claim":"false","introspection.token.claim":"true"}'
  replace_mapper "$control_center_id" mattercodex-realm-roles oidc-usermodel-realm-role-mapper \
    '{"claim.name":"realm_access.roles","jsonType.label":"String","multivalued":"true","access.token.claim":"true","id.token.claim":"true","userinfo.token.claim":"true","introspection.token.claim":"true"}'

  owner_count=$(keycloak_request get users -r "$realm" -q "username=$owner_username" |
    jq -r --arg username "$owner_username" '[.[] | select(.username == $username)] | length')
  if [[ "$owner_count" == 0 ]]; then
    keycloak_request create users -r "$realm" -s "username=$owner_username" -s enabled=true \
      -s "email=$owner_email" -s emailVerified=true >/dev/null
    printf '%s\n%s\n%s\n' "$admin_client_id" "$admin_client_secret" "$owner_password" |
      kubectl -n "$namespace" exec -i "deployment/$deployment" -- sh -ec '
        IFS= read -r client_id
        IFS= read -r client_secret
        IFS= read -r owner_password
        config=/tmp/mattercodex-kcadm.config
        command=/opt/keycloak/bin/kcadm.sh
        "$command" config credentials --config "$config" --server http://127.0.0.1:8080 \
          --realm master --client "$client_id" --secret "$client_secret" >/dev/null 2>&1
        "$command" set-password --config "$config" -r "$1" --username "$2" \
          --new-password "$owner_password" >/dev/null
      ' sh "$realm" "$owner_username"
  elif [[ "$owner_count" != 1 ]]; then
    fail 'owner identity is ambiguous'
  fi
  keycloak_request update "users/$(keycloak_request get users -r "$realm" -q "username=$owner_username" |
    jq -er --arg username "$owner_username" '.[] | select(.username == $username) | .id')" \
    -r "$realm" -s enabled=true -s "email=$owner_email" -s emailVerified=true >/dev/null
  keycloak_request add-roles -r "$realm" --uusername "$owner_username" \
    --rolename mattercodex-owner >/dev/null
fi

realm_json=$(keycloak_request get "realms/$realm")
jq -e '.enabled == true and .registrationAllowed == false and .accessTokenLifespan == 300' \
  <<<"$realm_json" >/dev/null || fail 'realm readback failed'
control_center_id=$(find_client_id mattercodex-control-center)
client_json=$(keycloak_request get "clients/$control_center_id" -r "$realm")
jq -e --arg origin "$public_origin" '
  .enabled == true and .publicClient == true and .standardFlowEnabled == true and
  .implicitFlowEnabled == false and .directAccessGrantsEnabled == false and
  .attributes."pkce.code.challenge.method" == "S256" and
  .redirectUris == [($origin + "/auth/callback")] and .webOrigins == [$origin] and
  (.optionalClientScopes | index("mattercodex.owner") != null)
' <<<"$client_json" >/dev/null || fail 'Control Center OIDC client readback failed'

mapper_json=$(keycloak_request get "clients/$control_center_id/protocol-mappers/models" -r "$realm")
jq -e --arg organization_id "$organization_id" '
  def mapper($name): [.[] | select(.name == $name)] | if length == 1 then .[0] else null end;
  mapper("mattercodex-organization-id").config."claim.value" == $organization_id and
  mapper("mattercodex-session-revision").config."claim.value" == "1" and
  mapper("mattercodex-control-api-audience").config."included.client.audience" == "mattercodex-control-api" and
  mapper("mattercodex-realm-roles").config."claim.name" == "realm_access.roles"
' <<<"$mapper_json" >/dev/null || fail 'OIDC claim mapper readback failed'

owner_id=$(keycloak_request get users -r "$realm" -q "username=$owner_username" |
  jq -er --arg username "$owner_username" '
    [ .[] | select(.username == $username and .enabled == true) ] |
    if length == 1 then .[0].id else error("owner identity readback failed") end
  ')
keycloak_request get "users/$owner_id/role-mappings/realm" -r "$realm" |
  jq -e 'any(.[]; .name == "mattercodex-owner")' >/dev/null || fail 'owner role readback failed'

printf 'Keycloak bootstrap completed: %s\n' "$mode"
