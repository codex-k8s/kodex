#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local provider account failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage:' \
    '  provider-account.sh authorize --account-key <key> --name <name> --mode device|api-key' \
    '  provider-account.sh import --account-key <key> --name <name> --auth-file <path>' \
    '  provider-account.sh list' \
    'Options: --kubeconfig <path> --context <name> --state-directory <path>' >&2
}

command_name=${1:-}
[[ -n "$command_name" ]] || { usage; exit 1; }
shift

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
kubeconfig=${KODEX_DEV_KUBECONFIG:-/home/s/.kube/kodex-dev-local}
context=${KODEX_DEV_KUBE_CONTEXT:-default}
state_directory="$repository_root/.kodex-dev"
account_key=""
account_name=""
auth_mode=""
auth_file=""
while (($# > 0)); do
  case "$1" in
    --account-key) account_key=${2:-}; shift 2 ;;
    --name) account_name=${2:-}; shift 2 ;;
    --mode) auth_mode=${2:-}; shift 2 ;;
    --auth-file) auth_file=${2:-}; shift 2 ;;
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$command_name" in authorize|import|list) ;; *) usage; fail 'command is invalid' ;; esac
for command in codex install jq kubectl mktemp realpath sha256sum stat; do
  command -v "$command" >/dev/null 2>&1 || fail "$command is required"
done
[[ -f "$kubeconfig" && -r "$kubeconfig" ]] || fail 'Kubernetes configuration is absent'
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
  fail 'state directory must be an exact safe absolute path'
export KUBECONFIG=$kubeconfig
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
[[ "$context" != *prod* && "$context" != *production* ]] || fail 'production context is forbidden'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'

if [[ "$command_name" == list ]]; then
  kubectl -n kodex-system exec kodex-postgresql-0 -- psql -U postgres -d control_plane \
    -P pager=off -c "SELECT account.stable_key, account.name, account.state, account.enabled, revision.revision_number FROM control_plane.provider_accounts account LEFT JOIN control_plane.provider_credential_revisions revision ON revision.id = account.current_credential_revision_id ORDER BY account.created_at"
  exit 0
fi

[[ "$account_key" =~ ^[a-z][a-z0-9_-]{1,95}$ ]] || fail 'account key is invalid'
[[ -n "$account_name" && ${#account_name} -le 160 && "$account_name" != *$'\n'* && "$account_name" != *$'\r'* ]] ||
  fail 'account name is invalid'

account_home="$state_directory/provider-accounts/$account_key"
if [[ "$command_name" == authorize ]]; then
  case "$auth_mode" in device|api-key) ;; *) fail 'authorization mode is invalid' ;; esac
  install -d -m 0700 "$account_home"
  if [[ "$auth_mode" == device ]]; then
    CODEX_HOME="$account_home" codex login --device-auth
  else
    [[ ! -t 0 ]] || fail 'API key must be provided through standard input'
    CODEX_HOME="$account_home" codex login --with-api-key
  fi
  auth_file="$account_home/auth.json"
fi

[[ "$auth_file" == /* && -f "$auth_file" && ! -L "$auth_file" ]] ||
  fail 'authorization file must be an absolute regular non-symlink file'
[[ "$(stat -c '%u' "$auth_file")" == "$(id -u)" ]] || fail 'authorization file owner is invalid'
permissions=$(stat -c '%a' "$auth_file")
(( (8#$permissions & 8#077) == 0 )) || fail 'authorization file must be private'
[[ "$(stat -c '%s' "$auth_file")" -le 1048576 ]] || fail 'authorization file is too large'
jq -e 'type == "object" and length > 0' "$auth_file" >/dev/null || fail 'authorization JSON is invalid'
CODEX_HOME="$(dirname -- "$auth_file")" codex login status >/dev/null ||
  fail 'Codex does not recognize the authorization file'

install -d -m 0700 "$account_home"
canonical_auth_file="$account_home/auth.json"
if [[ "$(realpath -e -- "$auth_file")" != "$canonical_auth_file" ]]; then
  temporary_auth_file=$(mktemp "$account_home/.auth.json.XXXXXX")
  install -m 0600 "$auth_file" "$temporary_auth_file"
  mv -f -- "$temporary_auth_file" "$canonical_auth_file"
fi
auth_file="$canonical_auth_file"

digest=$(sha256sum "$auth_file" | awk '{print $1}')
key_digest=$(printf '%s' "$account_key" | sha256sum | awk '{print $1}')
account_slug=${account_key:0:16}
secret_name="runtime-provider-openai-${account_slug}-${key_digest:0:6}-${digest:0:12}"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
chmod 0700 "$temporary_directory"
printf '%s\n' "$digest" >"$temporary_directory/auth.sha256"
chmod 0600 "$temporary_directory/auth.sha256"

if secret_json=$(kubectl -n kodex-system get "secret/$secret_name" -o json 2>/dev/null); then
  existing_digest=$(jq -jr '.data["auth.json"] | @base64d' <<<"$secret_json" | sha256sum | awk '{print $1}')
  jq -e --arg account_key "$account_key" '
    .immutable == true and .type == "Opaque" and
    .metadata.annotations["kodex.dev/provider-account-key"] == $account_key
  ' <<<"$secret_json" >/dev/null || fail 'existing provider Secret contract is invalid'
  [[ "$existing_digest" == "$digest" ]] || fail 'existing immutable provider Secret digest differs'
else
  manifest="$temporary_directory/provider-secret.json"
  kubectl -n kodex-system create secret generic "$secret_name" \
    --from-file=auth.json="$auth_file" \
    --from-file=auth.sha256="$temporary_directory/auth.sha256" \
    --dry-run=client -o json | jq --arg account_key "$account_key" '
      .immutable = true |
      .metadata.labels = {
        "app.kubernetes.io/part-of":"kodex",
        "app.kubernetes.io/managed-by":"kodex-local-dev"
      } |
      .metadata.annotations = {"kodex.dev/provider-account-key":$account_key}
    ' >"$manifest"
  kubectl create --field-manager=kodex-local-dev -f "$manifest" >/dev/null
fi

secret_uid=$(kubectl -n kodex-system get "secret/$secret_name" -o jsonpath='{.metadata.uid}')
secret_resource_version=$(kubectl -n kodex-system get "secret/$secret_name" -o jsonpath='{.metadata.resourceVersion}')
account_ref="pacc_${key_digest:0:24}"
credential_digest=$(printf '%s\n%s\n%s\n' "$account_key" "$secret_uid" "$secret_resource_version" | sha256sum | awk '{print $1}')
credential_ref="pcr_${credential_digest:0:24}"

readback=$(kubectl -n kodex-system exec -i kodex-postgresql-0 -- \
  psql -qAt -U postgres -d control_plane -P pager=off \
  -v account_ref="$account_ref" \
  -v stable_key="$account_key" \
  -v account_name="$account_name" \
  -v credential_ref="$credential_ref" \
  -v secret_name="$secret_name" \
  -v secret_uid="$secret_uid" \
  -v secret_resource_version="$secret_resource_version" \
  -v content_sha256="$digest" \
  <"$repository_root/tools/dev/reconcile-provider-account.sql")
IFS='|' read -r readback_key readback_revision readback_secret <<<"$readback"
[[ "$readback_key" == "$account_key" && "$readback_revision" =~ ^[1-9][0-9]*$ &&
  "$readback_secret" == "$secret_name" ]] || fail 'provider account database readback failed'

if [[ "$account_key" == default-openai-codex ]]; then
  kubectl -n kodex-system create configmap runtime-provider-openai-default-metadata \
    --from-literal=secretName="$secret_name" \
    --from-literal=secretUID="$secret_uid" \
    --from-literal=secretResourceVersion="$secret_resource_version" \
    --from-literal=contentSHA256="$digest" \
    --dry-run=client -o json | jq --arg account_key "$account_key" '
      .metadata.labels = {
        "app.kubernetes.io/part-of":"kodex",
        "app.kubernetes.io/managed-by":"kodex-local-dev"
      } |
      .metadata.annotations = {"kodex.dev/provider-account-key":$account_key}
    ' | kubectl apply --server-side --force-conflicts \
      --field-manager=kodex-local-dev -f - >/dev/null
fi

metadata_file="$account_home/account.json"
temporary_metadata=$(mktemp "$account_home/.account.json.XXXXXX")
jq -n --arg account_key "$account_key" --arg account_name "$account_name" '
  {version:1, accountKey:$account_key, name:$account_name}
' >"$temporary_metadata"
chmod 0600 "$temporary_metadata"
mv -f -- "$temporary_metadata" "$metadata_file"

printf 'Kodex provider account reconciled: %s revision %s\n' "$account_key" "$readback_revision"
