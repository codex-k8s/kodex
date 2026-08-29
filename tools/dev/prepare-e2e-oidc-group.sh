#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local OIDC E2E fixture failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --state-directory <path>" \
    '  [--namespace identity] [--deployment sso] [--realm kodex]' \
    '  [--group kodex-e2e-restricted] [--admin-secret keycloak-admin-client]' \
    '  [--identities-configmap keycloak-identities]' >&2
}

expected_context=""
state_directory=""
namespace=identity
deployment=sso
realm=kodex
group_name=kodex-e2e-restricted
admin_secret=keycloak-admin-client
identities_configmap=keycloak-identities
while (($# > 0)); do
  case "$1" in
    --context) expected_context=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --namespace) namespace=${2:-}; shift 2 ;;
    --deployment) deployment=${2:-}; shift 2 ;;
    --realm) realm=${2:-}; shift 2 ;;
    --group) group_name=${2:-}; shift 2 ;;
    --admin-secret) admin_secret=${2:-}; shift 2 ;;
    --identities-configmap) identities_configmap=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail 'exact context is required'
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" &&
  -d "$state_directory" && ! -L "$state_directory" ]] ||
  fail 'state directory must be an exact existing safe absolute path'
[[ "${KODEX_E2E_CONFIRM_DISPOSABLE:-}" == I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION ]] ||
  fail 'disposable installation confirmation is required'
[[ "${expected_context,,}" != *prod* && "${expected_context,,}" != *production* ]] ||
  fail 'production context is forbidden'
for resource_name in "$namespace" "$deployment" "$realm" "$group_name" "$admin_secret" \
  "$identities_configmap"; do
  [[ "$resource_name" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
    fail 'resource name is invalid'
done
for command_name in base64 flock jq kubectl; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] ||
  fail 'current Kubernetes context mismatch'
kubectl -n "$namespace" rollout status "deployment/$deployment" --timeout=300s >/dev/null ||
  fail 'Keycloak deployment is unavailable'
kubectl get namespace "$namespace" -o json | jq -e '
  .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
  .metadata.labels["kodex.dev/local-profile"] == "hot-reload"
' >/dev/null || fail 'identity namespace is not an exact local profile'
exec 9>"$state_directory/e2e-oidc-group.lock"
flock -w 60 9 || fail 'another OIDC E2E fixture reconciliation is active'

temporary_directory=$(mktemp -d)
chmod 0700 "$temporary_directory"
cleanup() {
  rm -rf -- "$temporary_directory"
  kubectl -n "$namespace" exec "deployment/$deployment" -- sh -ec \
    'rm -f /tmp/kodex-e2e-kcadm.config' >/dev/null 2>&1 || true
}
trap cleanup EXIT

read_secret_key() {
  local secret_name=$1 key=$2 output_file=$3
  kubectl -n "$namespace" get secret "$secret_name" \
    -o "jsonpath={.data['${key//./\\.}']}" | base64 -d >"$output_file"
  [[ -s "$output_file" && ! -L "$output_file" ]] ||
    fail "Keycloak secret key is absent: $secret_name/$key"
  chmod 0600 "$output_file"
}

read_secret_key "$admin_secret" client-id "$temporary_directory/admin-client-id"
read_secret_key "$admin_secret" client-secret "$temporary_directory/admin-client-secret"
kubectl -n "$namespace" get configmap "$identities_configmap" \
  -o jsonpath='{.data.owner-username}' >"$temporary_directory/owner-username"
[[ -s "$temporary_directory/owner-username" && ! -L "$temporary_directory/owner-username" ]] ||
  fail 'owner username is absent'
chmod 0600 "$temporary_directory/owner-username"

admin_client_id=$(<"$temporary_directory/admin-client-id")
admin_client_secret=$(<"$temporary_directory/admin-client-secret")
owner_username=$(<"$temporary_directory/owner-username")
for value in "$admin_client_id" "$admin_client_secret" "$owner_username"; do
  [[ -n "$value" && "$value" != *$'\n'* && "$value" != *$'\r'* ]] ||
    fail 'Keycloak fixture input must be a non-empty single line'
done

keycloak_request() {
  printf '%s\n%s\n' "$admin_client_id" "$admin_client_secret" |
    kubectl -n "$namespace" exec -i "deployment/$deployment" -- sh -ec '
      IFS= read -r client_id
      IFS= read -r client_secret
      config=/tmp/kodex-e2e-kcadm.config
      command=/opt/keycloak/bin/kcadm.sh
      "$command" config credentials --config "$config" --server http://127.0.0.1:8080 \
        --realm master --client "$client_id" --secret "$client_secret" >/dev/null 2>&1
      exec "$command" "$@" --config "$config"
    ' sh "$@"
}

owner_id=$(keycloak_request get users -r "$realm" -q "username=$owner_username" |
  jq -er --arg username "$owner_username" '
    [.[] | select(.username == $username)] |
    if length == 1 then .[0].id else error("owner identity is ambiguous") end
  ') || fail 'owner identity is unavailable'

groups_json=$(keycloak_request get groups -r "$realm" -q "search=$group_name" -q exact=true -q max=2) ||
  fail 'OIDC groups are unavailable'
group_count=$(jq -r --arg group_name "$group_name" \
  '[.[] | select(.name == $group_name and .path == ("/" + $group_name))] | length' <<<"$groups_json")
case "$group_count" in
  0)
    keycloak_request create groups -r "$realm" -s "name=$group_name" >/dev/null ||
      fail 'OIDC E2E group creation failed'
    groups_json=$(keycloak_request get groups -r "$realm" -q "search=$group_name" -q exact=true -q max=2) ||
      fail 'created OIDC group readback failed'
    ;;
  1) ;;
  *) fail 'OIDC E2E group is ambiguous' ;;
esac
group_id=$(jq -er --arg group_name "$group_name" '
  [.[] | select(.name == $group_name and .path == ("/" + $group_name))] |
  if length == 1 then .[0].id else error("group identity is ambiguous") end
' <<<"$groups_json") || fail 'OIDC E2E group is unavailable after reconciliation'

memberships=$(keycloak_request get "users/$owner_id/groups" -r "$realm") ||
  fail 'owner group membership readback failed'
if ! jq -e --arg group_id "$group_id" 'any(.[]; .id == $group_id)' <<<"$memberships" >/dev/null; then
  keycloak_request update "users/$owner_id/groups/$group_id" -r "$realm" -n >/dev/null ||
    fail 'owner group membership update failed'
fi
keycloak_request get "users/$owner_id/groups" -r "$realm" |
  jq -e --arg group_id "$group_id" 'any(.[]; .id == $group_id)' >/dev/null ||
  fail 'owner group membership verification failed'

printf 'Kodex local OIDC E2E group is ready: %s\n' "$group_name"
