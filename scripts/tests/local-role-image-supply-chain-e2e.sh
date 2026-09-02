#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local RoleImage supply-chain E2E failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage: local-role-image-supply-chain-e2e.sh --context <exact-context>' \
    '  --kubeconfig <path> --state-directory <path> --resource-prefix <slug>' \
    '  [--timeout-seconds <seconds>]' >&2
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
context=""
kubeconfig=""
state_directory=""
resource_prefix=""
timeout_seconds=1200
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --resource-prefix) resource_prefix=${2:-}; shift 2 ;;
    --timeout-seconds) timeout_seconds=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "${KODEX_E2E_CONFIRM_DISPOSABLE:-}" == I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION ]] ||
  fail 'explicit disposable-installation confirmation is required'
[[ -n "$context" && "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'exact non-production context is required'
[[ -f "$kubeconfig" && -r "$kubeconfig" && ! -L "$kubeconfig" ]] ||
  fail 'Kubernetes configuration is absent or unsafe'
[[ "$state_directory" == /* && -d "$state_directory" && ! -L "$state_directory" ]] ||
  fail 'state directory is invalid'
[[ "$resource_prefix" =~ ^[a-z0-9]([a-z0-9-]{2,38}[a-z0-9])$ ]] ||
  fail 'resource prefix is invalid'
[[ "$timeout_seconds" =~ ^[0-9]+$ && "$timeout_seconds" -ge 60 && "$timeout_seconds" -le 1800 ]] ||
  fail 'timeout must be between 60 and 1800 seconds'
for command_name in jq kubectl node npm stat; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

export KUBECONFIG=$kubeconfig
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'
namespace_json=$(kubectl get namespace/kodex-system -o json)
jq -e '
  .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
  .metadata.labels["kodex.dev/local-profile"] == "hot-reload"
' <<<"$namespace_json" >/dev/null || fail 'Kodex namespace is not the disposable local profile'

frontend_directory="$repository_root/services/staff/control-center"
storage_state="$state_directory/e2e/owner.json"
owner_username_file="$state_directory/inputs/owner-username"
owner_password_file="$state_directory/inputs/owner-password"
[[ -f "$owner_username_file" && -r "$owner_username_file" && ! -L "$owner_username_file" ]] ||
  fail 'local owner username is absent or unsafe'
[[ -f "$owner_password_file" && -r "$owner_password_file" && ! -L "$owner_password_file" ]] ||
  fail 'local owner password is absent or unsafe'
install -d -m 0700 "$state_directory/e2e"
state="$state_directory/e2e/$resource_prefix-role-image.json"
[[ ! -e "$state" && ! -L "$state" ]] || fail 'RoleImage E2E state already exists'

tls_mode=${KODEX_DEV_TLS_MODE:-local-ca}
case "$tls_mode" in
  local-ca)
    base_url=https://control.127.0.0.1.nip.io/
    ca_file="$state_directory/kodex-local-ca.crt"
    [[ -f "$ca_file" && ! -L "$ca_file" ]] || fail 'local HTTPS CA is absent'
    ;;
  public-acme)
    public_host=${KODEX_DEV_PUBLIC_HOST:-}
    [[ "$public_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ &&
      "$public_host" == *.* && "$public_host" != *..* ]] ||
      fail 'public-acme RoleImage E2E host is invalid'
    base_url="https://$public_host/"
    ca_file=""
    ;;
  *) fail 'RoleImage E2E TLS mode is invalid' ;;
esac
KODEX_E2E_BASE_URL="${base_url%/}" \
  KODEX_E2E_OWNER_USERNAME="$(<"$owner_username_file")" \
  KODEX_E2E_OWNER_PASSWORD="$(<"$owner_password_file")" \
  KODEX_E2E_STORAGE_STATE="$storage_state" \
  KODEX_E2E_RBAC_GROUP=kodex-e2e-restricted \
  KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
  NODE_EXTRA_CA_CERTS="$ca_file" \
  npm --prefix "$frontend_directory" run test:e2e:auth
[[ -f "$storage_state" && ! -L "$storage_state" ]] ||
  fail 'authenticated owner storage state was not created'

api_storage_state=$(mktemp "$state_directory/e2e/$resource_prefix-owner-api.XXXXXX.json")
cleanup() {
  rm -f -- "$api_storage_state"
}
trap cleanup EXIT
KODEX_E2E_BASE_URL="${base_url%/}" \
  KODEX_E2E_STORAGE_STATE="$storage_state" \
  KODEX_E2E_API_STORAGE_STATE="$api_storage_state" \
  KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
  NODE_EXTRA_CA_CERTS="$ca_file" \
  npm --prefix "$frontend_directory" run test:e2e:api-session

common_environment=(
  KODEX_ROLE_IMAGE_E2E_BASE_URL="$base_url"
  KODEX_ROLE_IMAGE_E2E_STORAGE_STATE="$api_storage_state"
  KODEX_ROLE_IMAGE_E2E_STATE="$state"
  KODEX_ROLE_IMAGE_E2E_PREFIX="$resource_prefix"
  KODEX_ROLE_IMAGE_E2E_TIMEOUT_MS="$((timeout_seconds * 1000))"
  NODE_EXTRA_CA_CERTS="$ca_file"
)
env "${common_environment[@]}" node "$repository_root/tools/dev/local-role-image-supply-chain-e2e.mjs" prepare

before_pods=$(kubectl -n kodex-runtime get pods -l runtime.kodex.dev/managed=true -o json |
  jq -c '[.items[].metadata.uid]')
env "${common_environment[@]}" node "$repository_root/tools/dev/local-role-image-supply-chain-e2e.mjs" launch

promoted_reference=$(jq -er '.promotedReference | select(test("@sha256:[a-f0-9]{64}$"))' "$state")
manifest_digest=$(jq -er '.manifestDigest | select(test("^sha256:[a-f0-9]{64}$"))' "$state")
deadline=$((SECONDS + timeout_seconds))
pod_json=""
while ((SECONDS < deadline)); do
  pods=$(kubectl -n kodex-runtime get pods -l runtime.kodex.dev/managed=true -o json)
  pod_json=$(jq -c --argjson before "$before_pods" --arg image "$promoted_reference" --arg digest "$manifest_digest" '
    [.items[] |
      select((.metadata.uid as $uid | $before | index($uid)) == null) |
      select(
        ([.spec.initContainers[]?,.spec.containers[]?] |
          length == 3 and all(.; .image == $image)) and
        ([.status.initContainerStatuses[]?,.status.containerStatuses[]?] |
          length == 3 and all(.; (.imageID // "") | endswith("@" + $digest)))
      )] |
    sort_by(.metadata.creationTimestamp) | last // empty
  ' <<<"$pods")
  [[ -z "$pod_json" ]] || break
  sleep 1
done
if [[ -z "$pod_json" ]]; then
  kubectl -n kodex-runtime get pods -l runtime.kodex.dev/managed=true \
    -o custom-columns=NAME:.metadata.name,PHASE:.status.phase --no-headers >&2 || true
  fail 'runtime Pod exact image and imageID readback timed out'
fi

pod_name=$(jq -er '.metadata.name' <<<"$pod_json")
pod_uid=$(jq -er '.metadata.uid' <<<"$pod_json")
temporary_state=$(mktemp "$state.XXXXXX")
jq --arg pod_name "$pod_name" --arg pod_uid "$pod_uid" \
  '.status="passed" | .runtimePod={name:$pod_name,uid:$pod_uid} | .finishedAt=(now | todateiso8601)' \
  "$state" >"$temporary_state"
chmod 0600 "$temporary_state"
mv -- "$temporary_state" "$state"

printf 'Kodex local RoleImage supply-chain E2E passed: %s\n' "$resource_prefix"
