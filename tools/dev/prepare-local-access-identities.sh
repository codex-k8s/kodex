#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local access identities failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --state-directory <path>" \
    '  [--namespace identity] [--deployment sso] [--realm kodex]' \
    '  [--admin-secret keycloak-admin-client]'
}

expected_context=""
state_directory=""
namespace=identity
deployment=sso
realm=kodex
admin_secret=keycloak-admin-client
while (($# > 0)); do
  case "$1" in
    --context) expected_context=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --namespace) namespace=${2:-}; shift 2 ;;
    --deployment) deployment=${2:-}; shift 2 ;;
    --realm) realm=${2:-}; shift 2 ;;
    --admin-secret) admin_secret=${2:-}; shift 2 ;;
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
for resource_name in "$namespace" "$deployment" "$realm" "$admin_secret"; do
  [[ "$resource_name" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
    fail 'resource name is invalid'
done
for command_name in base64 flock jq kubectl openssl; do
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

exec 9>"$state_directory/access-identities.lock"
flock -w 60 9 || fail 'another access identity reconciliation is active'

temporary_directory=$(mktemp -d)
chmod 0700 "$temporary_directory"
credentials_file="$state_directory/access-identities.env"
cleanup() {
  rm -rf -- "$temporary_directory"
  kubectl -n "$namespace" exec -c keycloak "deployment/$deployment" -- sh -ec \
    'rm -f /tmp/kodex-access-kcadm.config' >/dev/null 2>&1 || true
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
admin_client_id=$(<"$temporary_directory/admin-client-id")
admin_client_secret=$(<"$temporary_directory/admin-client-secret")
for value in "$admin_client_id" "$admin_client_secret"; do
  [[ -n "$value" && "$value" != *$'\n'* && "$value" != *$'\r'* ]] ||
    fail 'Keycloak administrator input must be a non-empty single line'
done

keycloak_request() {
  printf '%s\n%s\n' "$admin_client_id" "$admin_client_secret" |
    kubectl -n "$namespace" exec -i -c keycloak "deployment/$deployment" -- sh -ec '
      IFS= read -r client_id
      IFS= read -r client_secret
      config=/tmp/kodex-access-kcadm.config
      command=/opt/keycloak/bin/kcadm.sh
      "$command" config credentials --config "$config" --server http://127.0.0.1:8080 \
        --realm master --client "$client_id" --secret "$client_secret" >/dev/null 2>&1
      exec "$command" "$@" --config "$config"
    ' sh "$@"
}

if [[ -e "$credentials_file" ]]; then
  [[ -f "$credentials_file" && ! -L "$credentials_file" &&
    $((8#$(stat -c '%a' "$credentials_file") & 8#077)) == 0 ]] ||
    fail 'access identity credentials file is unsafe'
  # shellcheck disable=SC1090
  source "$credentials_file"
else
  umask 077
  access_password=$(openssl rand -base64 36 | tr -d '\n')
  {
    printf 'KODEX_ACCESS_ADMIN_USERNAME=%q\n' kodex-debug-admin
    printf 'KODEX_ACCESS_OPERATOR_USERNAME=%q\n' kodex-debug-operator
    printf 'KODEX_ACCESS_AUDITOR_USERNAME=%q\n' kodex-debug-auditor
    printf 'KODEX_ACCESS_TEST_PASSWORD=%q\n' "$access_password"
  } >"$credentials_file"
  unset access_password
  # shellcheck disable=SC1090
  source "$credentials_file"
fi

for variable_name in KODEX_ACCESS_ADMIN_USERNAME KODEX_ACCESS_OPERATOR_USERNAME \
  KODEX_ACCESS_AUDITOR_USERNAME KODEX_ACCESS_TEST_PASSWORD; do
  value=${!variable_name:-}
  [[ -n "$value" && "$value" != *$'\n'* && "$value" != *$'\r'* ]] ||
    fail 'access identity credential is invalid'
done

reconcile_group() {
  local group_name=$1 groups_json group_count
  groups_json=$(keycloak_request get groups -r "$realm" -q "search=$group_name" -q exact=true -q max=2) ||
    fail 'OIDC groups are unavailable'
  group_count=$(jq -r --arg group_name "$group_name" \
    '[.[] | select(.name == $group_name and .path == ("/" + $group_name))] | length' <<<"$groups_json")
  case "$group_count" in
    0) keycloak_request create groups -r "$realm" -s "name=$group_name" >/dev/null ||
         fail 'OIDC group creation failed' ;;
    1) ;;
    *) fail 'OIDC group is ambiguous' ;;
  esac
  keycloak_request get groups -r "$realm" -q "search=$group_name" -q exact=true -q max=2 |
    jq -er --arg group_name "$group_name" '
      [.[] | select(.name == $group_name and .path == ("/" + $group_name))] |
      if length == 1 then .[0].id else error("group identity is unavailable") end
    '
}

reconcile_user() {
  local username=$1 suffix=$2 group_id=${3:-} users_json user_count user_id
  users_json=$(keycloak_request get users -r "$realm" -q "username=$username" -q exact=true -q max=2) ||
    fail 'Keycloak users are unavailable'
  user_count=$(jq -r --arg username "$username" '[.[] | select(.username == $username)] | length' <<<"$users_json")
  case "$user_count" in
    0)
      keycloak_request create users -r "$realm" -s "username=$username" -s enabled=true \
        -s "email=$username@example.invalid" -s emailVerified=true -s firstName=Kodex \
        -s "lastName=$suffix" -s 'requiredActions=[]' >/dev/null ||
        fail 'Keycloak user creation failed'
      ;;
    1) ;;
    *) fail 'Keycloak user is ambiguous' ;;
  esac
  user_id=$(keycloak_request get users -r "$realm" -q "username=$username" -q exact=true -q max=2 |
    jq -er --arg username "$username" '
      [.[] | select(.username == $username)] |
      if length == 1 then .[0].id else error("user identity is unavailable") end
    ') || fail 'Keycloak user readback failed'
  keycloak_request update "users/$user_id" -r "$realm" -s enabled=true \
    -s "email=$username@example.invalid" -s emailVerified=true -s firstName=Kodex \
    -s "lastName=$suffix" -s 'requiredActions=[]' >/dev/null ||
    fail 'Keycloak user update failed'
  printf '%s\n%s\n%s\n' "$admin_client_id" "$admin_client_secret" "$KODEX_ACCESS_TEST_PASSWORD" |
    kubectl -n "$namespace" exec -i -c keycloak "deployment/$deployment" -- sh -ec '
      IFS= read -r client_id
      IFS= read -r client_secret
      IFS= read -r user_password
      config=/tmp/kodex-access-kcadm.config
      command=/opt/keycloak/bin/kcadm.sh
      "$command" config credentials --config "$config" --server http://127.0.0.1:8080 \
        --realm master --client "$client_id" --secret "$client_secret" >/dev/null 2>&1
      "$command" set-password --config "$config" -r "$1" --username "$2" \
        --new-password "$user_password" >/dev/null
    ' sh "$realm" "$username" || fail 'Keycloak user password update failed'
  if [[ -n "$group_id" ]]; then
    keycloak_request update "users/$user_id/groups/$group_id" -r "$realm" -n >/dev/null ||
      fail 'Keycloak group membership update failed'
    keycloak_request get "users/$user_id/groups" -r "$realm" |
      jq -e --arg group_id "$group_id" 'any(.[]; .id == $group_id)' >/dev/null ||
      fail 'Keycloak group membership readback failed'
  fi
}

operators_group_id=$(reconcile_group kodex-debug-operators)
auditors_group_id=$(reconcile_group kodex-debug-auditors)
reconcile_user "$KODEX_ACCESS_ADMIN_USERNAME" Administrator
reconcile_user "$KODEX_ACCESS_OPERATOR_USERNAME" Operator "$operators_group_id"
reconcile_user "$KODEX_ACCESS_AUDITOR_USERNAME" Auditor "$auditors_group_id"

printf '%s\n' 'Kodex local access identities are ready: 3 users, 2 groups'
